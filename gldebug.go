package main

import (
	"golang.org/x/mobile/event/size"
	"golang.org/x/mobile/exp/app/debug"
	"golang.org/x/mobile/exp/gl/glutil"
	"golang.org/x/mobile/gl"
)

// GLDebug encapsulates state for drawing debug information.
type GLDebug struct {
	ctx    gl.Context
	images *glutil.Images
	fps    *debug.FPS
}

func NewGLDebug(ctx gl.Context) *GLDebug {
	if ctx == nil {
		return nil
	}
	images := glutil.NewImages(ctx)
	return &GLDebug{
		ctx:    ctx,
		images: images,
		fps:    debug.NewFPS(images),
	}
}

func (d *GLDebug) Release() {
	if d == nil {
		return
	}
	d.fps.Release()
	d.images.Release()
}

func (d *GLDebug) Paint(sz size.Event) {
	if d == nil {
		return
	}
	d.fps.Draw(sz)
}
