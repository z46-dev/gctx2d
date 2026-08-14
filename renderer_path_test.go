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

func TestClipIntersectDifferenceAndRestore(t *testing.T) {
	ctx := &Context{
		width:      100,
		height:     80,
		transform:  uiIdentityMatrix(),
		vertices:   make([]vertex, 0, 32),
		batches:    make([]batch, 0, 8),
		clips:      make([]*clipEntry, 0, 4),
		stateStack: make([]drawingState, 0, 2),
	}

	ctx.Rect(0, 0, 60, 60)
	ctx.Clip()
	if !ctx.usesStencil {
		t.Fatal("Clip should enable the stencil render path for the frame")
	}
	if len(ctx.clips) != 1 || ctx.clips[0].mode != ClipModeIntersect {
		t.Fatalf("default Clip mode did not create an intersect clip: %#v", ctx.clips)
	}
	if len(ctx.batches) != 1 || ctx.batches[0].clipOperation != clipOperationIncrement || ctx.batches[0].stencilReference != 0 {
		t.Fatalf("unexpected intersect clip batch: %#v", ctx.batches)
	}

	ctx.Save()
	ctx.BeginPath()
	ctx.Rect(10, 10, 20, 20)
	ctx.Clip(ClipModeDifference)
	if len(ctx.clips) != 2 {
		t.Fatalf("clip depth=%d, want 2", len(ctx.clips))
	}
	if got := ctx.batches[len(ctx.batches)-2]; got.clipOperation != clipOperationIncrement || got.stencilReference != 1 || got.count != 6 {
		t.Fatalf("difference clip did not copy its parent region: %#v", got)
	}
	if got := ctx.batches[len(ctx.batches)-1]; got.clipOperation != clipOperationDecrement || got.stencilReference != 2 {
		t.Fatalf("difference clip did not punch out its path: %#v", got)
	}

	ctx.Restore()
	if len(ctx.clips) != 1 {
		t.Fatalf("restored clip depth=%d, want 1", len(ctx.clips))
	}
	if got := ctx.batches[len(ctx.batches)-1]; got.clipOperation != clipOperationDecrement || got.stencilReference != 2 || got.count != 6 {
		t.Fatalf("restoring difference clip did not restore its parent: %#v", got)
	}
}

func TestClipReplaceRestoresSavedClip(t *testing.T) {
	ctx := &Context{
		width:      100,
		height:     100,
		transform:  uiIdentityMatrix(),
		vertices:   make([]vertex, 0, 32),
		batches:    make([]batch, 0, 8),
		clips:      make([]*clipEntry, 0, 4),
		stateStack: make([]drawingState, 0, 1),
	}

	ctx.Rect(0, 0, 50, 50)
	ctx.Clip()
	original := ctx.clips[0]
	ctx.Save()

	ctx.BeginPath()
	ctx.Rect(50, 50, 25, 25)
	ctx.Clip(ClipModeReplace)
	if len(ctx.clips) != 1 || ctx.clips[0] == original {
		t.Fatal("replace clip should install an independent root clip")
	}
	if got := ctx.batches[len(ctx.batches)-2]; got.clipOperation != clipOperationDecrement || got.stencilReference != 1 {
		t.Fatalf("replace clip did not remove the previous clip: %#v", got)
	}

	ctx.Restore()
	if len(ctx.clips) != 1 || ctx.clips[0] != original {
		t.Fatal("Restore did not reconstruct the clip active at Save")
	}
	last := ctx.batches[len(ctx.batches)-1]
	if last.clipOperation != clipOperationIncrement || last.stencilReference != 0 {
		t.Fatalf("saved clip was not replayed from the root: %#v", last)
	}
}
