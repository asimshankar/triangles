package main

import (
	"bytes"
	"crypto/rand"
	"fmt"
	"github.com/asimshankar/triangles/spec"
	"runtime"
	"sync"
	"time"
	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/discovery"
	"v.io/v23/options"
	"v.io/v23/rpc"
	"v.io/v23/security"

	_ "v.io/x/ref/runtime/factories/generic"
)

var interfaceName = spec.ScreenDesc.PkgPath

type NetworkChannels struct {
	// Clients read NewLeftScreen to get a channel on which they can send
	// triangles to the screen on the left.
	NewLeftScreen <-chan (chan<- *spec.Triangle)
	// Clients read NewRightScreen to get a channel on which they can send
	// triangles to the screen on the right.
	NewRightScreen <-chan (chan<- *spec.Triangle)
	// Ready is closed when the network is setup correctly, or an error is
	// sent if network setup failed.
	Ready <-chan error
}

func SetupNetwork(chMyScreen chan<- *spec.Triangle) NetworkChannels {
	var (
		ready          = make(chan error)
		newLeftScreen  = make(chan chan<- *spec.Triangle)
		newRightScreen = make(chan chan<- *spec.Triangle)
		nm             = &networkManager{
			myUuid:         make([]byte, 16),
			chInvited:      make(chan bool),
			myScreen:       chMyScreen,
			newLeftScreen:  newLeftScreen,
			newRightScreen: newRightScreen,
		}
		ret = NetworkChannels{
			NewLeftScreen:  newLeftScreen,
			NewRightScreen: newRightScreen,
			Ready:          ready,
		}
	)
	if _, err := rand.Read(nm.myUuid); err != nil {
		go func() { ready <- err }()
		return ret
	}
	go func() {
		preV23Init()
		ctx, shutdown := v23.Init()
		defer shutdown()
		ctx, server, err := v23.WithNewServer(ctx, "", spec.ScreenServer(nm), security.AllowEveryone())
		if err != nil {
			ready <- err
			return
		}
		go seekInvites(ctx, server, nm.myUuid, nm.chInvited)
		go nm.sendInvites(ctx)
		close(ready)
		<-ctx.Done()
		close(nm.newLeftScreen)
		close(nm.newRightScreen)
	}()
	return ret
}

type networkManager struct {
	myUuid    []byte
	lock      sync.Mutex
	invited   bool      // GUARDED_BY(lock)
	chInvited chan bool // True sent when an invite is accepted, false sent otherwise

	myScreen       chan<- *spec.Triangle
	newLeftScreen  chan<- chan<- *spec.Triangle
	newRightScreen chan<- chan<- *spec.Triangle
}

func (nm *networkManager) Invite(ctx *context.T, call spec.ScreenInviteServerCall) error {
	chLeftScreen, err := nm.acceptInvitation()
	if err != nil {
		return err
	}
	defer nm.exitInvitation()
	blessings, rejected := security.RemoteBlessingNames(ctx, call.Security())
	ctx.Infof("Accepted invitation from %v (rejected blessings: %v)", blessings, rejected)
	chError := make(chan error, 2) // Buffered so that we don't have to worry about the two goroutines blocking
	go stream2channel(call.RecvStream(), nm.myScreen, -2, chError)
	go channel2stream(chLeftScreen, call.SendStream(), nm.myScreen, chError)
	return <-chError
}

func (nm *networkManager) acceptInvitation() (<-chan *spec.Triangle, error) {
	nm.lock.Lock()
	defer nm.lock.Unlock()
	if nm.invited {
		return nil, fmt.Errorf("thanks for the invite, but I'm already engaged with a previous invitation")
	}
	ch := make(chan *spec.Triangle)
	nm.invited = true
	nm.newLeftScreen <- ch
	nm.chInvited <- true
	return ch, nil
}

func (nm *networkManager) exitInvitation() {
	nm.lock.Lock()
	defer nm.lock.Unlock()
	if nm.invited {
		nm.invited = false
		nm.newLeftScreen <- nil
		nm.chInvited <- false
	}
}

func (nm *networkManager) sendInvites(ctx *context.T) {
	var cancelLastInvite func()
	for {
		if cancelLastInvite != nil {
			cancelLastInvite()
			cancelLastInvite = nil
		}
		var (
			rightScreen         spec.ScreenInviteClientCall
			ctxScan, cancelScan = context.WithCancel(ctx)
			updates, err        = v23.GetDiscovery(ctxScan).Scan(ctxScan, interfaceName)
		)
		if err != nil {
			ctx.Panic(err)
		}
		ctx.Infof("Scanning for peers to invite")
		for u := range updates {
			if rightScreen != nil {
				// Cancel the scan and drain all remaining updates
				cancelScan()
				go func() {
					for range updates {
					}
				}()
				break
			}
			if f, ok := u.Interface().(discovery.Found); ok && !bytes.Equal(f.Service.InstanceUuid, nm.myUuid) {
				// TODO: Something to think about: Do we want a
				// timeout for the establishment of the stream?
				//
				// Alternatively, could have tried connecting
				// to all addresses in parallel, but then given
				// the Invite server implementation (where the
				// method implementation will cancel the RPC if
				// multiple are pending), it might be possible
				// that the client and server will be unable to
				// agree on which RPC is the one to keep?
				//
				// For now, try serially, with a timeout for each invite
				// time between sending the Invites.
				ctx.Infof("Sending invitation to %+v", f.Service)
				rightScreen, cancelLastInvite = sendInvitesWithTimeout(ctx, time.Second, f.Service.Addrs) // ctx and not scanCtx because the latter will be canceled
			}
		}
		chRightScreen := make(chan *spec.Triangle)
		nm.newRightScreen <- chRightScreen
		chError := make(chan error, 2)
		go stream2channel(rightScreen.RecvStream(), nm.myScreen, +2, chError)
		go channel2stream(chRightScreen, rightScreen.SendStream(), nm.myScreen, chError)
		select {
		case <-ctx.Done():
			rightScreen.Finish()
			return
		case err := <-chError:
			if err != nil {
				ctx.Infof("Right screen has been lost: %v", err)
			}
			ctx.Infof("Right screen has gracefully terminated?: %v", rightScreen.Finish())
		}
	}
}

func sendInvitesWithTimeout(ctx *context.T, timeout time.Duration, addrs []string) (spec.ScreenInviteClientCall, func()) {
	var (
		chCall = make(chan spec.ScreenInviteClientCall)
		chErr  = make(chan error)
		invite = func(ctx *context.T, addr string) {
			ctx.Infof("Inviting %v", addr)
			call, err := spec.ScreenClient(addr).Invite(ctx, options.ServerAuthorizer{security.AllowEveryone()})
			if err != nil {
				chErr <- err
			}
			if call != nil {
				ctx.Infof("Invitation accepted by %v", addr)
				chCall <- call
			}
		}
	)
	for _, addr := range addrs {
		ctxInvite, cancel := context.WithCancel(ctx)
		go invite(ctxInvite, addr)
		select {
		case <-time.After(timeout):
			ctx.Infof("Invitation to %v timed out", addr)
			cancel()
		case err := <-chErr:
			ctx.Infof("Invitation to %v failed: %v", addr, err)
		case call := <-chCall:
			return call, cancel
		}
	}
	return nil, nil
}

func seekInvites(ctx *context.T, server rpc.Server, uuid []byte, updates <-chan bool) {
	var (
		// TODO: Thoughts on the discovery API
		// - Service should be mutable so that if the InstanceUuid is filled in
		//   then the client gets to know it
		// - Even InterfaceName should be somehow filled in automatically?
		service = discovery.Service{
			InstanceUuid:  uuid,
			InstanceName:  "triangles",
			InterfaceName: interfaceName,
			Attrs: discovery.Attributes{
				"OS": runtime.GOOS,
			},
		}
		cancel    func()
		chStopped <-chan struct{}
		start     = func() {
			// Set the service, update cancelCtx, cancel and chStopped
			endpoints := server.Status().Endpoints
			service.Addrs = make([]string, len(endpoints))
			for idx, ep := range endpoints {
				service.Addrs[idx] = ep.Name()
			}
			var err error
			var advCtx *context.T
			advCtx, cancel = context.WithCancel(ctx)
			chStopped, err = v23.GetDiscovery(ctx).Advertise(advCtx, service, nil)
			if err != nil {
				ctx.Info(err)
				return
			}
			ctx.Infof("Started advertising: %#v", service)
		}
		stop = func() {
			if chStopped == nil {
				return
			}
			cancel()
			<-chStopped
			ctx.Infof("Stopped advertising: %#v", service)
			chStopped = nil
		}
	)
	start()
	for shouldStop := range updates {
		if shouldStop {
			stop()
			continue
		}
		start()
	}
}

type triangleRecvStream interface {
	Advance() bool
	Value() spec.Triangle
	Err() error
}

type triangleSendStream interface {
	Send(spec.Triangle) error
}

func stream2channel(in triangleRecvStream, out chan<- *spec.Triangle, deltaX float32, errch chan<- error) {
	for in.Advance() {
		t := in.Value()
		t.X = t.X + deltaX // transform from in coordinates to out coordinates
		out <- &t
	}
	errch <- in.Err()
}

func channel2stream(in <-chan *spec.Triangle, out triangleSendStream, fallback chan<- *spec.Triangle, errch chan<- error) {
	for t := range in {
		if err := out.Send(*t); err != nil {
			errch <- err
			break
		}
	}
	// out is dead, reflect triangles back into in.
	for t := range in {
		t.Dx = -1 * t.Dx
		fallback <- t
	}
}
