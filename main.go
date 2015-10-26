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
	"math/rand"
)

func main() {
	app.Main(func(a app.App) {
		var (
			myGL    *GL
			sz      size.Event
			touches = make(map[touch.Sequence]*touchEvents) // Active touch events
			scene   = Scene{}

			chMyScreen      = make(chan *spec.Triangle) // New triangles to draw on my screen
			leftScreen      = newOtherScreen(nil, chMyScreen)
			rightScreen     = newOtherScreen(nil, chMyScreen)
			networkChannels = SetupNetwork(chMyScreen)

			myR, myG, myB = randomColor()
			spawnTriangle = func() {
				scene.Triangles = append(scene.Triangles, &spec.Triangle{R: myR, G: myG, B: myB})
			}
		)
		spawnTriangle()
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
						if c := tch.Count; c > maxTouchCount {
							log.Printf("Ignoring long touch (%d > %d)", c, maxTouchCount)
							break
						}
						// Find the closest triangle to the touch start and adjust its velocity.
						var (
							// Normalize the touch coordinates to the triangle coordinates ([-1,1])
							x, y          = touch2coords(tch.Start, sz)
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

func randomColor() (r, g, b float32) {
	random := func() float32 {
		// Pick a number between [30, 255] and normalize it to [0, 1]
		return float32(rand.Intn(255-29)+30) / 255.0
	}
	return random(), random(), random()
}

const (
	maxTouchCount     = 30
	gravity           = 0.1
	timeBetweenPaints = 0.1
)
