package main

import (
	"encoding/binary"
	"github.com/asimshankar/triangles/spec"
	"golang.org/x/mobile/exp/f32"
	"golang.org/x/mobile/exp/gl/glutil"
	"golang.org/x/mobile/gl"
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
	ctx.BindBuffer(gl.ARRAY_BUFFER, g.buf)
	ctx.BufferData(gl.ARRAY_BUFFER, triangleData, gl.STATIC_DRAW)
	return g, nil
}

func (g *GL) Release() {
	if g == nil {
		return
	}
	g.ctx.DeleteProgram(g.program)
	g.ctx.DeleteBuffer(g.buf)
}

func (g *GL) Paint(triangles []*spec.Triangle) {
	if g == nil {
		return
	}
	g.ctx.ClearColor(0, 0, 0, 1)
	g.ctx.Clear(gl.COLOR_BUFFER_BIT)

	g.ctx.UseProgram(g.program)

	g.ctx.EnableVertexAttribArray(g.position)
	g.ctx.VertexAttribPointer(g.position, coordsPerVertex, gl.FLOAT, false, 0, 0)
	for _, t := range triangles {
		g.ctx.Uniform4f(g.color, t.R, t.G, t.B, 1)
		g.ctx.Uniform2f(g.offset, t.X, t.Y)
		g.ctx.DrawArrays(gl.TRIANGLES, 0, vertexCount)
	}
	g.ctx.DisableVertexAttribArray(g.position)
}

const (
	vertexShader = `#version 100
uniform vec2 offset;

attribute vec4 position;
void main() {
	// offset comes in with x/y values between 0 and 1.
	// position bounds are -1 to 1.
	vec4 offset4 = vec4(2.0*offset.x-1.0, 1.0-2.0*offset.y, 0, 0);
	gl_Position = position + offset4;
}`

	fragmentShader = `#version 100
precision mediump float;
uniform vec4 color;
void main() {
	gl_FragColor = color;
}`

	coordsPerVertex = 3
	vertexCount     = 3
	triangleSize    = 0.4 // In OpenGL coordinates where the full screen in of size 2 [-1, 1]
)

var (
	triangleData = f32.Bytes(binary.LittleEndian,
		0.0, triangleSize, 0.0, // top left
		0.0, 0.0, 0.0, // bottom left
		triangleSize, 0.0, 0.0, // bottom right
	)
)
