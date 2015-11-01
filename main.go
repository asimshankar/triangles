// An app that draws and moves triangles on the screen and pushes them on to
// other screens.
//
// See https://github.com/asimshankar/triangles
package main

import (
	"fmt"
	"github.com/asimshankar/triangles/spec"
	"golang.org/x/mobile/app"
	"golang.org/x/mobile/event/lifecycle"
	"golang.org/x/mobile/event/paint"
	"golang.org/x/mobile/event/size"
	"golang.org/x/mobile/event/touch"
	"golang.org/x/mobile/gl"
	"log"
	"time"
)

func main() {
	app.Main(func(a app.App) {
		var (
			myGL    *GL
			sz      size.Event
			touches = make(map[touch.Sequence]*touchEvents) // Active touch events
			scene   = Scene{}
			debug   *GLDebug

			chMyScreen      = make(chan *spec.Triangle) // New triangles to draw on my screen
			leftScreen      = newOtherScreen(nil, chMyScreen)
			rightScreen     = newOtherScreen(nil, chMyScreen)
			networkChannels = SetupNetwork(chMyScreen)

			spawnTriangle = func(x float32) {
				c := scene.TopBanner
				scene.Triangles = append(scene.Triangles, &spec.Triangle{
					X: x, Y: 1, R: c.R, G: c.G, B: c.B})
			}

			invitationActive       bool
			invitation             Invitation
			invitationTicker       *time.Ticker
			invitationBannerTicker <-chan time.Time

			clearInvitation = func() {
				invitationActive = false
				invitation = Invitation{}
				invitationTicker.Stop()
				invitationBannerTicker = nil
				scene.LeftBanner = nil
			}
		)
		for {
			select {
			case ready := <-networkChannels.Ready:
				switch v := ready.(type) {
				case error:
					log.Panic(v)
				case Color:
					scene.TopBanner = v
					spawnTriangle(0)
				default:
					log.Panicf("Unexpected type from the Ready channel: %T (%v)", ready, ready)
				}
				networkChannels.Ready = nil // To stop this select clause from being hit again.
			case inv := <-networkChannels.Invitations:
				invitationActive = true
				invitation = inv
				invitationTicker = time.NewTicker(time.Second)
				invitationBannerTicker = invitationTicker.C
				log.Printf("Notifying user of invitation from %v", inv.Name)
			case <-invitationBannerTicker:
				// Flash the banner
				if scene.LeftBanner == nil {
					scene.LeftBanner = &invitation.Color
					break
				}
				scene.LeftBanner = nil
			case <-invitation.Withdrawn:
				log.Printf("Invitation from %v withdrawn", invitation.Name)
				clearInvitation()
			case ch := <-networkChannels.NewLeftScreen:
				leftScreen.close()
				leftScreen = newOtherScreen(ch, chMyScreen)
			case ch := <-networkChannels.NewRightScreen:
				rightScreen.close()
				rightScreen = newOtherScreen(ch, chMyScreen)
			case t := <-chMyScreen:
				scene.Triangles = append(scene.Triangles, t)
			case e := <-a.Events():
				switch e := a.Filter(e).(type) {
				case lifecycle.Event:
					switch e.Crosses(lifecycle.StageVisible) {
					case lifecycle.CrossOn:
						glctx, _ := e.DrawContext.(gl.Context)
						var err error
						if myGL, err = NewGL(glctx); err != nil {
							log.Panic(err)
						}
						debug = NewGLDebug(glctx)
						a.Send(paint.Event{})
					case lifecycle.CrossOff:
						if exitOnLifecycleCrossOff() {
							return
						}
						myGL.Release()
						debug.Release()
						myGL = nil
					}
				case paint.Event:
					if e.External {
						continue
					}
					var mine, left, right []*spec.Triangle
					for _, t := range scene.Triangles {
						moveTriangle(t)
						switch {
						case t.X <= -1:
							left = append(left, t)
						case t.X >= 1:
							right = append(right, t)
						default:
							mine = append(mine, t)
						}
					}
					if len(left) > 0 {
						go leftScreen.send(left)
					}
					if len(right) > 0 {
						go rightScreen.send(right)
					}
					scene.Triangles = mine
					myGL.Paint(scene)
					debug.Paint(sz)
					a.Publish()
					a.Send(paint.Event{})
				case size.Event:
					sz = e
				case touch.Event:
					switch e.Type {
					case touch.TypeBegin:
						touches[e.Sequence] = &touchEvents{Start: e}
					case touch.TypeMove:
						touches[e.Sequence].Count++
					case touch.TypeEnd:
						tch := touches[e.Sequence]
						delete(touches, e.Sequence)
						x, y := touch2coords(tch.Start, sz)
						if invitationActive && x < bannerWidth {
							// Touched in the left invitation banner:
							// Swipe = reject, Tap = accept.
							if x, y := (e.X - tch.Start.X), (e.Y - tch.Start.Y); x*x+y*y > 0 {
								log.Printf("Swiped (%d, %d) pixels, rejecting invitation from %q", x, y, invitation.Name)
								invitation.Response <- fmt.Errorf("user rejected")
							} else {
								log.Printf("Accepting invitation from %q", invitation.Name)
								invitation.Response <- nil
							}
							clearInvitation()
							break
						}
						if y < bannerWidth {
							// Touched top banner, spawn a new triangle
							spawnTriangle(x)
							break
						}
						if c := tch.Count; c > maxTouchCount {
							log.Printf("Ignoring long touch (%d > %d)", c, maxTouchCount)
							break
						}
						// Find the closest triangle to the touch start and adjust its velocity.
						var (
							// Normalize the touch coordinates to the triangle coordinates ([-1,1])
							closestT      *spec.Triangle
							minDistanceSq float32
						)
						for idx, t := range scene.Triangles {
							if d := (x-t.X)*(x-t.X) + (y-t.Y)*(y-t.Y); d < minDistanceSq || idx == 0 {
								minDistanceSq = d
								closestT = t
							}
						}
						if closestT != nil {
							closestT.Dx += (e.X - tch.Start.X) / float32(sz.WidthPx)
							closestT.Dy += (e.Y - tch.Start.Y) / float32(sz.HeightPx)
						}
					}
				}
			}
		}
	})
}

type touchEvents struct {
	Start touch.Event // Starting event
	Count int         // Number of moves before the end event
}

type otherScreen struct {
	chTriangles chan<- *spec.Triangle
	chLost      chan struct{}
	chSelf      chan<- *spec.Triangle
}

func newOtherScreen(other, self chan<- *spec.Triangle) *otherScreen {
	return &otherScreen{
		chTriangles: other,
		chLost:      make(chan struct{}),
		chSelf:      self,
	}
}

func (s *otherScreen) close() {
	close(s.chLost)
}

func (s *otherScreen) send(triangles []*spec.Triangle) {
	if s.chTriangles == nil {
		for _, t := range triangles {
			returnTriangle(t, s.chSelf)
		}
		return
	}
	for i, t := range triangles {
		select {
		case <-s.chLost:
			// Lost the other screen, so reflect the remaining triangles back onto my screen.
			for i < len(triangles) {
				returnTriangle(t, s.chSelf)
				i++
			}
			return
		case s.chTriangles <- t:
		}
	}
}

// touch2coords transforms coordinates from the touch.Event coordinate system
// to the GL and Triangles coordinate system.
func touch2coords(t touch.Event, sz size.Event) (x, y float32) {
	return 2*t.X/float32(sz.WidthPx) - 1, 2*t.Y/float32(sz.HeightPx) - 1
}

func moveTriangle(t *spec.Triangle) {
	t.X = t.X + t.Dx*timeBetweenPaints
	t.Y = t.Y + (t.Dy-gravity)*timeBetweenPaints
	if t.Y <= -1 {
		t.Dy = -1 * t.Dy
		t.Y = -1
	} else if t.Y >= 1 {
		t.Dy = -1 * t.Dy
		t.Y = 1
	}
}

func returnTriangle(t *spec.Triangle, myScreen chan<- *spec.Triangle) {
	t.Dx = -1 * t.Dx
	moveTriangle(t)
	myScreen <- t
}

const (
	maxTouchCount            = 30
	acceptInvitationDuration = time.Second
	gravity                  = 0.1
	timeBetweenPaints        = 0.1
)
