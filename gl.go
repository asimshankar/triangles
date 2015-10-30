package main

import (
	"encoding/binary"
	"github.com/asimshankar/triangles/spec"
	"golang.org/x/mobile/exp/f32"
	"golang.org/x/mobile/exp/gl/glutil"
	"golang.org/x/mobile/gl"
	"math"
)

// GL encapsulates all the GL commands for drawing a set of spec.Triangles.
type GL struct {
	ctx      gl.Context
	program  gl.Program
	buf      gl.Buffer
	position gl.Attrib
	offset   gl.Uniform
	color    gl.Uniform
}

// NewGL returns a GL. ctx can be nil and the returned value can be nil, which
// is okay, a nil *GL functions correctly and doesn't crash.
func NewGL(ctx gl.Context) (*GL, error) {
	if ctx == nil {
		return nil, nil
	}
	program, err := glutil.CreateProgram(ctx, vertexShader, fragmentShader)
	if err != nil {
		return nil, err
	}
	g := &GL{
		ctx:      ctx,
		program:  program,
		buf:      ctx.CreateBuffer(),
		position: ctx.GetAttribLocation(program, "position"),
		color:    ctx.GetUniformLocation(program, "color"),
		offset:   ctx.GetUniformLocation(program, "offset"),
	}
	return g, nil
}

func (g *GL) Release() {
	if g == nil {
		return
	}
	g.ctx.DeleteProgram(g.program)
	g.ctx.DeleteBuffer(g.buf)
}

type Color struct {
	R, G, B float32
}

// Scene represents the state of the game to be painted on the screen.
type Scene struct {
	Triangles []*spec.Triangle

	// If non-nil, a banner will be draw along the right/left edge of the screen respectively.
	RightBanner *Color
	LeftBanner  *Color
}

func (g *GL) Paint(scn Scene) {
	if g == nil {
		return
	}
	g.ctx.ClearColor(0, 0, 0, 1)
	g.ctx.Clear(gl.COLOR_BUFFER_BIT)

	g.ctx.UseProgram(g.program)

	g.ctx.BindBuffer(gl.ARRAY_BUFFER, g.buf)
	g.ctx.BufferData(gl.ARRAY_BUFFER, triangleData, gl.STATIC_DRAW)
	g.ctx.EnableVertexAttribArray(g.position)
	g.ctx.VertexAttribPointer(g.position, coordsPerVertex, gl.FLOAT, false, 0, 0)
	for _, t := range scn.Triangles {
		g.ctx.Uniform4f(g.color, t.R, t.G, t.B, 1)
		g.ctx.Uniform2f(g.offset, t.X, t.Y)
		g.ctx.DrawArrays(gl.TRIANGLES, 0, vertexCount)
	}
	if c := scn.RightBanner; c != nil {
		g.ctx.BufferData(gl.ARRAY_BUFFER, rightBannerData, gl.STATIC_DRAW)
		g.ctx.Uniform4f(g.color, c.R, c.G, c.B, 1)
		g.ctx.Uniform2f(g.offset, 0, 0)
		g.ctx.DrawArrays(gl.TRIANGLE_FAN, 0, 4)
	}

	if c := scn.LeftBanner; c != nil {
		g.ctx.BufferData(gl.ARRAY_BUFFER, leftBannerData, gl.STATIC_DRAW)
		g.ctx.Uniform4f(g.color, c.R, c.G, c.B, 1)
		g.ctx.Uniform2f(g.offset, 0, 0)
		g.ctx.DrawArrays(gl.TRIANGLE_FAN, 0, 4)
	}

	g.ctx.DisableVertexAttribArray(g.position)
}

const (
	vertexShader = `#version 100
uniform vec2 offset;

attribute vec4 position;
void main() {
	vec4 offset4 = vec4(offset.x, offset.y, 0, 0);
	gl_Position = position + offset4;
}`

	fragmentShader = `#version 100
precision mediump float;
uniform vec4 color;
void main() {
	gl_FragColor = color;
}`

	coordsPerVertex         = 3
	vertexCount             = 3
	triangleSide    float32 = 0.4 // In OpenGL coordinates where the full screen in of size 2 [-1, 1]
	bannerWidth             = 0.1
)

var (
	triangleHeight = float32(math.Sqrt(3)) * triangleSide / 2
	triangleData   = f32.Bytes(binary.LittleEndian,
		-triangleSide/2, -triangleHeight/2, 0, // bottom left
		0, triangleHeight/2, 0, // top
		triangleSide/2, -triangleHeight/2, 0, // bottom right
	)
	rightBannerData = f32.Bytes(binary.LittleEndian,
		1-bannerWidth, 1, 0,
		1, 1, 0,
		1, -1, 0,
		1-bannerWidth, -1, 0,
	)
	leftBannerData = f32.Bytes(binary.LittleEndian,
		-1, 1, 0,
		-1+bannerWidth, 1, 0,
		-1+bannerWidth, -1, 0,
		-1, -1, 0,
	)
)
