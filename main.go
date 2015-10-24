// An app that draws and moves triangles on the screen and pushes them on to
// other screens.
//
// See https://github.com/asimshankar/triangles
package main

import (
	"github.com/asimshankar/triangles/spec"
	"golang.org/x/mobile/app"
	"golang.org/x/mobile/event/lifecycle"
	"golang.org/x/mobile/event/paint"
	"golang.org/x/mobile/event/size"
	"golang.org/x/mobile/event/touch"
	"golang.org/x/mobile/gl"
	"log"
)

func main() {
	app.Main(func(a app.App) {
		var (
			myGL        *GL
			myTriangles = []*spec.Triangle{
				&spec.Triangle{B: 1},
			}
			sz size.Event

			touchCount int // Number of touch events before the touch stopped
			touchStart touch.Event

			chMyScreen      = make(chan *spec.Triangle) // New triangles to draw on my screen
			leftScreen      = newOtherScreen(nil, chMyScreen)
			rightScreen     = newOtherScreen(nil, chMyScreen)
			networkChannels = SetupNetwork(chMyScreen)
		)
		for {
			select {
			case err := <-networkChannels.Ready:
				if err != nil {
					log.Panic(err)
				}
				networkChannels.Ready = nil // To stop this select clause from being hit.
			case ch := <-networkChannels.NewLeftScreen:
				leftScreen.close()
				leftScreen = newOtherScreen(ch, chMyScreen)
			case ch := <-networkChannels.NewRightScreen:
				rightScreen.close()
				rightScreen = newOtherScreen(ch, chMyScreen)
			case t := <-chMyScreen:
				myTriangles = append(myTriangles, t)
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
						a.Send(paint.Event{})
					case lifecycle.CrossOff:
						if exitOnLifecycleCrossOff() {
							return
						}
						myGL.Release()
						myGL = nil
					}
				case paint.Event:
					if e.External {
						continue
					}
					var mine, left, right []*spec.Triangle
					for _, t := range myTriangles {
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
					myTriangles = mine
					myGL.Paint(myTriangles)
					a.Publish()
					a.Send(paint.Event{})
				case size.Event:
					sz = e
				case touch.Event:
					switch e.Type {
					case touch.TypeBegin:
						touchCount = 1
						touchStart = e
					case touch.TypeMove:
						touchCount++
					case touch.TypeEnd:
						if touchCount > maxTouchCount {
							log.Printf("Ignoring long touch (%d > %d)", touchCount, maxTouchCount)
							touchCount = 0
							break
						}
						touchCount = 0
						// Find the closest triangle to the touch start and adjust its velocity.
						var (
							// Normalize the touch coordinates to the triangle coordinates ([-1,1])
							x, y          = touch2coords(touchStart, sz)
							closestT      *spec.Triangle
							minDistanceSq float32
						)
						for idx, t := range myTriangles {
							if d := (x-t.X)*(x-t.X) + (y-t.Y)*(y-t.Y); d < minDistanceSq || idx == 0 {
								minDistanceSq = d
								closestT = t
							}
						}
						if closestT != nil {
							closestT.Dx += (e.X - touchStart.X) / float32(sz.WidthPx)
							closestT.Dy += (e.Y - touchStart.Y) / float32(sz.HeightPx)
						}
					}
				}
			}
		}
	})
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
	} else if t.Y >= 1-triangleHeight {
		t.Dy = -1 * t.Dy
		t.Y = 1 - triangleHeight
	}
}

func returnTriangle(t *spec.Triangle, myScreen chan<- *spec.Triangle) {
	t.Dx = -1 * t.Dx
	moveTriangle(t)
	myScreen <- t
}

const (
	maxTouchCount     = 30
	gravity           = 0.1
	timeBetweenPaints = 0.1
)
