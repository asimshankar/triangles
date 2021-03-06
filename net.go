package main

import (
	"crypto/md5"
	"fmt"
	"github.com/asimshankar/triangles/spec"
	"runtime"
	"time"
	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/discovery"
	"v.io/v23/options"
	"v.io/v23/rpc"
	"v.io/v23/security"
	discutil "v.io/x/ref/lib/discovery"

	_ "v.io/x/ref/runtime/factories/roaming"
)

var interfaceName = spec.ScreenDesc.PkgPath

type NetworkChannels struct {
	// When the network setup is complete, the Color to be used is written
	// to the channel. If the network setup fails, an error is written
	// do the channel.
	// Exactly one item is written to the channel before it is closed.
	Ready <-chan interface{}
	// Clients read NewLeftScreen to get a channel on which they can send
	// triangles to the screen on the left.
	NewLeftScreen <-chan (chan<- *spec.Triangle)
	// Clients read NewRightScreen to get a channel on which they can send
	// triangles to the screen on the right.
	NewRightScreen <-chan (chan<- *spec.Triangle)
	// Invitations is where clients can read invitations received to join
	// another screen on their right (our left). The response to the invitation
	// is sent by writing to Invitation.Response.
	Invitations <-chan Invitation
}

func SetupNetwork(chMyScreen chan<- *spec.Triangle) NetworkChannels {
	var (
		ready          = make(chan interface{})
		newLeftScreen  = make(chan chan<- *spec.Triangle)
		newRightScreen = make(chan chan<- *spec.Triangle)
		invites        = make(chan Invitation)
		nm             = &networkManager{
			myScreen:   chMyScreen,
			inviteRPCs: make(chan Invitation),
		}
		ret = NetworkChannels{
			Ready:          ready,
			NewLeftScreen:  newLeftScreen,
			NewRightScreen: newRightScreen,
			Invitations:    invites,
		}
	)
	go nm.run(ready, newLeftScreen, newRightScreen, invites)
	return ret
}

type networkManager struct {
	myScreen   chan<- *spec.Triangle
	inviteRPCs chan Invitation
}

func (nm *networkManager) run(ready chan<- interface{}, newLeftScreen, newRightScreen chan<- chan<- *spec.Triangle, newInvite chan<- Invitation) {
	defer close(nm.myScreen)
	defer close(newLeftScreen)
	defer close(newRightScreen)
	notifyReady := func(result interface{}) {
		ready <- result
		close(ready)
		ready = nil
	}
	ctx, shutdown := v23.Init()
	defer shutdown()
	ctx, server, err := v23.WithNewServer(ctx, "", spec.ScreenServer(nm), security.AllowEveryone())
	if err != nil {
		notifyReady(err)
		return
	}
	disc, err := v23.NewDiscovery(ctx)
	if err != nil {
		notifyReady(err)
		return
	}
	// Select a color based on some unique identifier of the process, the PublicKey serves as one.
	notifyReady(selectColor(v23.GetPrincipal(ctx).PublicKey()))
	var (
		left     = remoteScreen{myScreen: nm.myScreen, notify: newLeftScreen}
		right    = remoteScreen{myScreen: nm.myScreen, notify: newRightScreen}
		accepted = make(chan string) // Names of remote screens that accepted an invitation
		seek     = make(chan bool)   // Send false to stop seeking invitations from others, true otherwise

		pendingInviterName        string
		pendingInviteUserResponse <-chan error
		pendingInviteRPCResponse  chan<- error
	)
	seekInvites(ctx, disc, server, seek)
	go sendInvites(ctx, disc, accepted)
	for {
		select {
		case invitation := <-nm.inviteRPCs:
			if left.Active() {
				invitation.Response <- fmt.Errorf("thanks for the invite but I'm already engaged with a previous invitation")
				break
			}
			// Defer the response to the user interface.
			ch := make(chan error)
			pendingInviterName = invitation.Name
			pendingInviteRPCResponse = invitation.Response
			pendingInviteUserResponse = ch
			invitation.Response = ch
			newInvite <- invitation
		case err := <-pendingInviteUserResponse:
			pendingInviteRPCResponse <- err
			if err == nil {
				ctx.Infof("Activating left screen %q", pendingInviterName)
				left.Activate(ctx, pendingInviterName)
				seek <- false
			}
			pendingInviterName = ""
			pendingInviteUserResponse = nil
			pendingInviteRPCResponse = nil
		case <-left.Lost():
			ctx.Infof("Deactivating left screen")
			left.Deactivate()
			seek <- true
		case invitee := <-accepted:
			ctx.Infof("Activating right screen %q", invitee)
			right.Activate(ctx, invitee)
		case <-right.Lost():
			ctx.Infof("Deactivating right screen")
			right.Deactivate()
			go sendInvites(ctx, disc, accepted)
		case <-ctx.Done():
			return
		}
	}
}

type remoteScreen struct {
	lost <-chan error // State changed by activate/deactivate
	// State fixed at construction time
	myScreen chan<- *spec.Triangle
	notify   chan<- chan<- *spec.Triangle
}

func (s *remoteScreen) Active() bool       { return s.lost != nil }
func (s *remoteScreen) Lost() <-chan error { return s.lost }
func (s *remoteScreen) Activate(ctx *context.T, name string) {
	errch := make(chan error)
	s.lost = errch
	ch := make(chan *spec.Triangle)
	go channel2rpc(ctx, ch, name, errch, s.myScreen)
	s.notify <- ch
}
func (s *remoteScreen) Deactivate() {
	s.lost = nil
	s.notify <- nil
}

type Invitation struct {
	Name      string
	Color     Color
	Response  chan<- error
	Withdrawn <-chan struct{} // Close if the invitation has been withdrawn
}

func (nm *networkManager) Invite(ctx *context.T, call rpc.ServerCall) error {
	inviter := call.RemoteEndpoint().Name()
	response := make(chan error)
	nm.inviteRPCs <- Invitation{
		Name:      inviter,
		Color:     selectColor(call.Security().RemoteBlessings().PublicKey()),
		Response:  response,
		Withdrawn: ctx.Done(),
	}
	if err := <-response; err != nil {
		return err
	}
	blessings, rejected := security.RemoteBlessingNames(ctx, call.Security())
	ctx.Infof("Accepted invitation from %v@%v (rejected blessings: %v)", blessings, inviter, rejected)
	return nil
}

func (nm *networkManager) Give(ctx *context.T, call rpc.ServerCall, t spec.Triangle) error {
	if ctx.V(3) {
		blessings, rejected := security.RemoteBlessingNames(ctx, call.Security())
		ctx.Infof("Took a triangle from %v@%v (rejected blessings: %v)", blessings, call.RemoteEndpoint().Name(), rejected)
	}
	// Transform from sender's coordinates to our coordinates.
	// The assumption is that if the triangle was to the left of the
	// sender's coordinate system, then it will appear on our right and
	// vice-versa.
	switch {
	case t.X < -1:
		t.X += 2
	case t.X > 1:
		t.X -= 2
	}
	nm.myScreen <- &t
	return nil
}

func sendInvites(ctx *context.T, disc discovery.T, notify chan<- string) {
	ctx.Infof("Scanning for peers to invite")
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	updates, err := disc.Scan(ctx, fmt.Sprintf("v.InterfaceName=%q", interfaceName))
	if err != nil {
		ctx.Panic(err)
	}
	for u := range updates {
		if u.IsLost() {
			continue
		}
		ctx.Infof("Sending invitations to %+v", u.Addresses())
		if addr := sendOneInvite(ctx, u.Addresses()); len(addr) > 0 {
			notify <- addr
			go func() {
				for range updates {
				}
			}()
			return
		}
	}
	ctx.Infof("Stopped scanning for peers to invite without finding one")
}

// sendOneInvite sends invitations to all the addresses in addrs and returns the one that accepted it.
// All addrs are assumed to be equivalent and thus at most one Invite RPC will succeed.
//
// TODO: This is aiming to replicate what the RPC stack does for all the
// addresses a single name resolved to. Should all these addresses discovered
// somehow be encapsulated in a single object name?
func sendOneInvite(ctx *context.T, addrs []string) string {
	// Give at most 1 second for these connections to be made, if they
	// can't be made then consider the peer bad and ignore it.
	// TODO: Should these RPCs also use the "connection timeout" that might be implemented
	// as per proposal: https://docs.google.com/a/google.com/document/d/1prtxGhSR5TaL0lc_iDRC0Q6H1Drbg2T0x7MWVb_ZCSM/edit?usp=sharing
	ctx, cancel := context.WithTimeout(ctx, maxInvitationWaitTime)
	defer cancel()
	ch := make(chan string)
	for _, addr := range addrs {
		go func(addr string) {
			err := spec.ScreenClient(addr).Invite(ctx, options.ServerAuthorizer{security.AllowEveryone()})
			ctx.Infof("Invitation to %v sent, error: %v", addr, err)
			if err == nil {
				ch <- addr
				return
			}
			ch <- ""
		}(addr)
	}
	for i := range addrs {
		if ret := <-ch; len(ret) > 0 {
			// Drain the rest and return
			go func() {
				i++
				for i < len(addrs) {
					<-ch
				}
			}()
			return ret
		}
	}
	return ""
}

func seekInvites(ctx *context.T, disc discovery.T, server rpc.Server, updates <-chan bool) {
	var (
		ad = &discovery.Advertisement{
			InterfaceName: interfaceName,
			Attributes: discovery.Attributes{
				"OS": runtime.GOOS,
			},
		}
		cancel    func()
		chStopped <-chan struct{}
		start     = func() {
			// Start the advertisement, update cancelCtx, cancel and chStopped
			var err error
			var advCtx *context.T
			advCtx, cancel = context.WithCancel(ctx)
			if chStopped, err = discutil.AdvertiseServer(advCtx, disc, server, "", ad, nil); err != nil {
				cancel()
				ctx.Infof("Failed to advertise %#v: %v", *ad, err)
				return
			}
			ctx.Infof("Started advertising: %#v", *ad)
		}
		stop = func() {
			if chStopped == nil {
				return
			}
			cancel()
			<-chStopped
			ctx.Infof("Stopped advertising: %#v", *ad)
			chStopped = nil
		}
	)
	start()
	go func() {
		for shouldStart := range updates {
			if shouldStart {
				start()
				continue
			}
			stop()
		}
	}()
}

func channel2rpc(ctx *context.T, src <-chan *spec.Triangle, dst string, errch chan<- error, myScreen chan<- *spec.Triangle) {
	for t := range src {
		// This is an "interactive" game, if an RPC doesn't succeed in say
		ctxTimeout, cancel := context.WithTimeout(ctx, maxTriangleGiveTime)
		if err := spec.ScreenClient(dst).Give(ctxTimeout, *t, options.ServerAuthorizer{security.AllowEveryone()}); err != nil {
			cancel()
			returnTriangle(t, myScreen)
			ctx.Infof("%q.Give failed: %v, aborting connection with remote screen", dst, err)
			errch <- err
			break
		}
		cancel()
	}
	for t := range src {
		returnTriangle(t, myScreen)
	}
	ctx.VI(1).Infof("Exiting goroutine with connection to %q", dst)
}

func selectColor(key security.PublicKey) Color {
	var (
		bytes, _ = key.MarshalBinary()
		uid      = md5.Sum(bytes)
		pick     = func(idx int) float32 {
			//  Keep component between [30, 225] instead of [0,255]
			// to avoid white and black - and then normalize to [0, 1]
			return (30 + (float32(uid[idx])/255.0)*(225-30)) / 255
		}
	)
	// Consider md5 to have uniform randomness in all its bytes.
	// We're just selecting a color, no need to fret if it doesn't.
	return Color{R: pick(0), G: pick(7), B: pick(15)}
}

const (
	maxInvitationWaitTime = 30 * time.Second
	maxTriangleGiveTime   = time.Second / 2
)
