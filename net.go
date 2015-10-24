package main

import (
	"flag"
	"fmt"
	"github.com/asimshankar/triangles/spec"
	"sync"
	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/options"
	"v.io/v23/security"

	_ "v.io/x/ref/runtime/factories/generic"
)

var flagRightHack = flag.String("right", "", "Temporary flag to play around with invitations")

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
	go func() {
		preV23Init()
		ctx, shutdown := v23.Init()
		defer shutdown()
		ctx, server, err := v23.WithNewServer(ctx, "", spec.ScreenServer(nm), security.AllowEveryone())
		if err != nil {
			ready <- err
			return
		}
		go nm.sendInvites(ctx)
		for idx, ep := range server.Status().Endpoints {
			ctx.Infof("Server address #%d: %v", idx+1, ep.Name())
		}
		close(ready)
		<-ctx.Done()
		close(nm.newLeftScreen)
		close(nm.newRightScreen)
	}()
	return ret
}

type networkManager struct {
	lock    sync.Mutex
	invited bool // GUARDED_BY(lock)

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
	return ch, nil
}

func (nm *networkManager) exitInvitation() {
	nm.lock.Lock()
	defer nm.lock.Unlock()
	if nm.invited {
		nm.invited = false
		nm.newLeftScreen <- nil
	}
}

func (nm *networkManager) sendInvites(ctx *context.T) {
	if len(*flagRightHack) == 0 {
		return
	}
	for {
		nm.newRightScreen <- nil
		call, err := spec.ScreenClient(*flagRightHack).Invite(ctx, options.ServerAuthorizer{security.AllowEveryone()})
		if err != nil {
			ctx.Panic(err)
		}
		chRightScreen := make(chan *spec.Triangle)
		nm.newRightScreen <- chRightScreen
		chError := make(chan error, 2)
		go stream2channel(call.RecvStream(), nm.myScreen, +2, chError)
		go channel2stream(chRightScreen, call.SendStream(), nm.myScreen, chError)
		select {
		case <-ctx.Done():
			call.Finish()
			return
		case err := <-chError:
			if err != nil {
				ctx.Infof("Lost the right screen: %v", err)
			}
			ctx.Infof("Done with the right screen: %v", call.Finish())
		}
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
