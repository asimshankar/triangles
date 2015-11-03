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
			myGL             *GL
			sz               size.Event
			touches          = make(map[touch.Sequence]*touchEvents) // Active touch events
			touchedTriangles = make(map[*spec.Triangle]struct{})     // Triangles currently being touched
			scene            = Scene{}
			debug            *GLDebug

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
					// Handle any collisions between triangles.
					for i, t1 := range scene.Triangles {
						for j := i + 1; j < len(scene.Triangles); j++ {
							t2 := scene.Triangles[j]
							if dx, dy := (t1.X - t2.X), (t1.Y - t2.Y); dx*dx+dy*dy < triangleSide*triangleSide {
								t1.Dx, t2.Dx = t2.Dx, t1.Dx
								t1.Dy, t2.Dy = t2.Dy, t1.Dy
							}
						}
					}
					for _, t := range scene.Triangles {
						if _, touched := touchedTriangles[t]; !touched {
							// Only move a triangle if it is not currently being manipulated by the user.
							moveTriangle(t)
						}
						switch {
						case t.X < -1:
							left = append(left, t)
						case t.X > 1:
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
						var (
							touchedT *spec.Triangle
							x, y     = touch2coords(e, sz)
						)
						for _, t := range scene.Triangles {
							if dx, dy := (x - t.X), (y - t.Y); dx*dx+dy*dy < triangleSide*triangleSide {
								log.Printf("Triangle %+v touched by user", t)
								touchedT = t
								touchedTriangles[t] = struct{}{}
								break
							}
						}
						touches[e.Sequence] = &touchEvents{Start: e, Triangle: touchedT}
					case touch.TypeMove:
						tch := touches[e.Sequence]
						if tch.Triangle != nil {
							tch.Triangle.X, tch.Triangle.Y = touch2coords(e, sz)
						}
					case touch.TypeEnd:
						tch := touches[e.Sequence]
						delete(touches, e.Sequence)
						x, y := touch2coords(tch.Start, sz)
						if t := tch.Triangle; t != nil {
							// Set triangle velocity based on movement from the original position.
							t.X, t.Y = touch2coords(e, sz)
							t.Dx = (e.X - tch.Start.X) / float32(sz.WidthPx)
							t.Dy = (e.Y - tch.Start.Y) / float32(sz.HeightPx)
							delete(touchedTriangles, t)
							break
						}
						if invitationActive && x < bannerWidth {
							// Touched in the left invitation banner:
							// Swipe = reject, Tap = accept.
							var swipeThreshold = float32(sz.WidthPx) / 2
							if dx, dy := (e.X - tch.Start.X), (e.Y - tch.Start.Y); dx*dx+dy*dy > swipeThreshold*swipeThreshold {
								log.Printf("Swiped (%v, %v) pixels, rejecting invitation from %q", dx, dy, invitation.Name)
								invitation.Response <- fmt.Errorf("user rejected")
							} else {
								log.Printf("Accepting invitation from %q", invitation.Name)
								invitation.Response <- nil
							}
							clearInvitation()
							break
						}
						if y >= 1-bannerWidth {
							// Touched top banner, spawn a new triangle
							log.Printf("Top banner touched, spawning new triangle (Y=%v, threshold=%v)", y, -1+bannerWidth)
							spawnTriangle(x)
							break
						}
					}
				}
			}
		}
	})
}

type touchEvents struct {
	Start    touch.Event    // Where the touch event began
	Triangle *spec.Triangle // The triangle being manipulated by touch, if any
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
//
// Pixel coordinates <--> GL coordinates;
//            (0, 0) <--> (-1, 1)  // bottom left
//        (W/2, H/2) <--> (0, 0)
//            (W, H) <--> (1, -1)  // top right
func touch2coords(t touch.Event, sz size.Event) (x, y float32) {
	return 2*t.X/float32(sz.WidthPx) - 1, 1 - 2*t.Y/float32(sz.HeightPx)
}

func moveTriangle(t *spec.Triangle) {
	t.Dy = t.Dy - gravity
	t.X = t.X + t.Dx*timeBetweenPaints
	t.Y = t.Y + t.Dy*timeBetweenPaints
	if t.Y <= -1 {
		t.Dy = -1 * t.Dy
		t.Y = -1
	} else if maxY := 1 - triangleCenterHeight; t.Y >= maxY {
		t.Dy = -1 * t.Dy
		t.Y = maxY
	}
}

func returnTriangle(t *spec.Triangle, myScreen chan<- *spec.Triangle) {
	t.Dx = -1 * t.Dx
	moveTriangle(t)
	myScreen <- t
}

const (
	acceptInvitationDuration = time.Second
	gravity                  = 0.001
	timeBetweenPaints        = 0.1
)
