package gctx2d

import (
	"math"
	"testing"
)

func newPathTestContext() *Context {
	return &Context{
		width:       800,
		height:      600,
		transform:   uiIdentityMatrix(),
		fillStyle:   ColorWhite,
		strokeStyle: ColorBlack,
		lineWidth:   4,
	}
}

func TestStandaloneCircleUsesSDFFastPath(t *testing.T) {
	var ctx *Context = newPathTestContext()
	ctx.BeginPath()
	ctx.Arc(100, 100, 25, 0, float32(math.Pi)*2, false)
	ctx.Fill()
	ctx.Stroke()

	if len(ctx.vertices) != 12 {
		t.Fatalf("standalone filled and stroked circle generated %d vertices, want 12", len(ctx.vertices))
	}
}

func TestTransformedCircleStrokeScalesOnce(t *testing.T) {
	var ctx *Context = newPathTestContext()
	ctx.SetObjectTransform(100, 100, 20, 0)
	ctx.SetLineWidth(0.5)
	ctx.BeginPath()
	ctx.Arc(0, 0, 1, 0, float32(math.Pi)*2, false)
	ctx.Stroke()

	if len(ctx.vertices) != 6 {
		t.Fatalf("transformed circle stroke generated %d vertices, want 6", len(ctx.vertices))
	}

	if ctx.vertices[0].StrokeWidth != 10 {
		t.Fatalf("transformed stroke width is %v, want 10", ctx.vertices[0].StrokeWidth)
	}
}

func TestExtendedCirclePathUsesGeneralPathRenderer(t *testing.T) {
	var ctx *Context = newPathTestContext()
	ctx.BeginPath()
	ctx.Arc(100, 100, 25, 0, float32(math.Pi)*2, false)
	ctx.LineTo(150, 100)
	ctx.Fill()

	if len(ctx.vertices) <= 6 {
		t.Fatalf("extended circle path incorrectly used the six-vertex circle fast path")
	}
}

func TestPartialArcUsesGeneralPathRenderer(t *testing.T) {
	var ctx *Context = newPathTestContext()
	ctx.BeginPath()
	ctx.Arc(100, 100, 25, 0, float32(math.Pi), false)
	ctx.Stroke()

	if len(ctx.vertices) <= 6 {
		t.Fatalf("partial arc incorrectly used the six-vertex circle fast path")
	}
}
