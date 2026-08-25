package gctx2d

import "testing"

// BenchmarkDynamicEntities measures the CPU-side path work used by vector-rendered mobs.
func BenchmarkDynamicEntities(b *testing.B) {
	var ctx *Context = &Context{
		width:       1920,
		height:      1080,
		transform:   uiIdentityMatrix(),
		vertices:    make([]vertex, 0, 256000),
		batches:     make([]batch, 0, 16),
		path:        make([]pathCommand, 0, 64),
		fillStyle:   ColorWhite,
		strokeStyle: ColorBlack,
		lineWidth:   4,
		lineCap:     LineCapRound,
		lineJoin:    LineJoinRound,
	}

	b.ReportAllocs()

	for b.Loop() {
		ctx.vertices = ctx.vertices[:0]
		ctx.batches = ctx.batches[:0]
		for entity := range 1000 {
			var x float32 = float32(entity%40) * 45
			var y float32 = float32(entity/40) * 45
			ctx.BeginPath()
			ctx.Arc(x, y, 18, 0, 6.2831855, false)
			ctx.Fill()
			ctx.Stroke()
		}
	}
}
