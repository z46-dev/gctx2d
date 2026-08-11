package gctx2d

import "testing"

func TestStrokePathSkipsZeroLengthSegmentsButStillBuildsGeometry(t *testing.T) {
	ctx := &Context{
		width:      128,
		height:     128,
		lineWidth:  8,
		lineCap:    LineCapRound,
		lineJoin:   LineJoinMiter,
		miterLimit: 10,
		vertices:   make([]vertex, 0, 64),
		batches:    make([]batch, 0, 8),
	}

	points := []Point{
		{X: 16, Y: 16},
		{X: 32, Y: 16},
		{X: 32, Y: 16},
		{X: 48, Y: 32},
		{X: 64, Y: 32},
	}

	ctx.appendStrokePath(points, false, ctx.lineWidth, ColorWhite)

	if len(ctx.vertices) == 0 {
		t.Fatal("expected stroke path to emit geometry")
	}
}

func TestAntialiasedShapeFastPathsEmitSDFQuads(t *testing.T) {
	var ctx *Context = &Context{
		width:       128,
		height:      128,
		transform:   uiIdentityMatrix(),
		fillStyle:   ColorWhite,
		strokeStyle: ColorWhite,
		lineWidth:   2,
		vertices:    make([]vertex, 0, 12),
		batches:     make([]batch, 0, 2),
	}

	ctx.FillCircle(20, 20, 8)
	ctx.StrokeRoundedRect(40, 10, 30, 20, 5)
	if len(ctx.vertices) != 12 {
		t.Fatalf("fast paths emitted vertices=%d, want 12", len(ctx.vertices))
	}
	for _, shapeVertex := range ctx.vertices {
		if shapeVertex.Kind != ctxKindRoundedRect {
			t.Fatalf("fast path emitted kind=%v, want SDF rounded rectangle", shapeVertex.Kind)
		}
	}
}
