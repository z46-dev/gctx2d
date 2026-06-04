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
