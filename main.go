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
	"runtime"
)

func main() {
	app.Main(func(a app.App) {
		var (
			myGL        *GL
			myTriangles = []*spec.Triangle{
				&spec.Triangle{X: 0.5, Y: 0.5, B: 1.0},
			}
			sz size.Event

			touchCount int // Number of touch events before the touch stopped
			touchStart touch.Event
		)
		for {
			select {
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
					for _, t := range myTriangles {
						moveTriangle(t)
					}
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
							// Normalize the touch coordinates to the triangle coordinates ([0,1])
							x             = touchStart.X / float32(sz.WidthPx)
							y             = touchStart.Y / float32(sz.HeightPx)
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
							log.Printf("ASIM:", *closestT)
						}
					}
				}
			}
		}
	})
}

func exitOnLifecycleCrossOff() bool { return runtime.GOOS != "android" }

func moveTriangle(t *spec.Triangle) {
	t.X = t.X + t.Dx*timeBetweenPaints
	t.Y = t.Y + (t.Dy+gravity)*timeBetweenPaints
	if t.Y <= 0 {
		t.Dy = -1 * t.Dy
		t.Y = 0
	} else if t.Y >= 1 {
		t.Dy = -1 * t.Dy
		t.Y = 1
	}
}

const (
	maxTouchCount     = 30
	gravity           = 0.1
	timeBetweenPaints = 0.1
)
