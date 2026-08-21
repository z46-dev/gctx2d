package gctx2d

import "testing"

func TestEffectiveStrokeWidthScalesWithTransform(t *testing.T) {
	ctx := &Context{
		lineWidth: 3,
		transform: Matrix{A: 2, D: 4},
	}

	if got := ctx.effectiveStrokeWidth(); got != 6 {
		t.Fatalf("effective stroke width = %v, want 6", got)
	}
}

func TestStrokeUsesScaledLineWidth(t *testing.T) {
	ctx := &Context{
		width:       128,
		height:      128,
		transform:   Matrix{A: 2, D: 2},
		strokeStyle: ColorWhite,
		lineWidth:   3,
		vertices:    make([]vertex, 0, 32),
		batches:     make([]batch, 0, 8),
	}

	ctx.BeginPath()
	ctx.MoveTo(10, 10)
	ctx.LineTo(20, 10)
	ctx.Stroke()

	if len(ctx.vertices) == 0 {
		t.Fatal("expected stroke geometry")
	}

	if got := ctx.vertices[0].HalfSize[1]; got != 3 {
		t.Fatalf("stroke half-width = %v, want 3", got)
	}
}
