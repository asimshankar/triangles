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
					myGL.Paint(myTriangles)
					a.Publish()
					a.Send(paint.Event{})
				}
			}
		}
	})
}

func exitOnLifecycleCrossOff() bool { return runtime.GOOS != "android" }
