package gctx2d

import (
	"math"
	"slices"
	"testing"

	"github.com/gogpu/wgpu"
)

func TestStateSettersAndBeginReset(t *testing.T) {
	ctx := &Context{
		transform:    Matrix{A: 2, D: 2, E: 10, F: 20},
		imageEffect:  ImageEffectElectric,
		effectTime:   12.5,
		stateStack:   []drawingState{{}},
		path:         []pathCommand{{kind: pathCommandMoveTo, p: Point{X: 1, Y: 2}}},
		vertices:     make([]vertex, 3),
		batches:      make([]batch, 2),
		lineWidth:    5,
		lineCap:      LineCapRound,
		lineJoin:     LineJoinBevel,
		miterLimit:   4,
		globalAlpha:  0.5,
		shadowBlur:   3,
		textAlign:    TextAlignRight,
		textBaseline: TextBaselineBottom,
		usesStencil:  true,
	}

	ctx.SetLineWidth(0)
	if ctx.lineWidth != 1 {
		t.Fatalf("expected line width clamp to 1, got %v", ctx.lineWidth)
	}

	ctx.SetLineCap(LineCap(99))
	if ctx.lineCap != LineCapButt {
		t.Fatalf("expected invalid line cap to clamp to butt, got %v", ctx.lineCap)
	}

	ctx.SetLineJoin(LineJoin(99))
	if ctx.lineJoin != LineJoinMiter {
		t.Fatalf("expected invalid line join to clamp to miter, got %v", ctx.lineJoin)
	}

	ctx.SetMiterLimit(0.2)
	if ctx.miterLimit != 1 {
		t.Fatalf("expected miter limit clamp to 1, got %v", ctx.miterLimit)
	}

	ctx.SetGlobalAlpha(-1)
	if ctx.globalAlpha != 0 {
		t.Fatalf("expected global alpha clamp to 0, got %v", ctx.globalAlpha)
	}

	ctx.SetGlobalAlpha(2)
	if ctx.globalAlpha != 1 {
		t.Fatalf("expected global alpha clamp to 1, got %v", ctx.globalAlpha)
	}

	ctx.SetShadowBlur(-5)
	if ctx.shadowBlur != 0 {
		t.Fatalf("expected shadow blur clamp to 0, got %v", ctx.shadowBlur)
	}

	ctx.SetTextAlign(TextAlign(99))
	if ctx.textAlign != TextAlignLeft {
		t.Fatalf("expected invalid text align to clamp to left, got %v", ctx.textAlign)
	}

	ctx.SetTextBaseline(TextBaseline(99))
	if ctx.textBaseline != TextBaselineAlphabetic {
		t.Fatalf("expected invalid text baseline to clamp to alphabetic, got %v", ctx.textBaseline)
	}

	ctx.Begin(320, 240)
	if ctx.width != 320 || ctx.height != 240 {
		t.Fatalf("unexpected frame size: %dx%d", ctx.width, ctx.height)
	}
	if len(ctx.vertices) != 0 || len(ctx.batches) != 0 || len(ctx.path) != 0 || len(ctx.stateStack) != 0 {
		t.Fatal("Begin should reset frame-local slices")
	}
	if ctx.transform != uiIdentityMatrix() {
		t.Fatalf("Begin should reset transform, got %#v", ctx.transform)
	}
	if ctx.imageEffect != ImageEffectNone || ctx.currentImageShader != nil || ctx.effectTime != 0 {
		t.Fatal("Begin should reset effect state")
	}
	if ctx.usesStencil {
		t.Fatal("Begin should leave frames without clips on the non-stencil render path")
	}
}

func TestDrawingStateSaveRestore(t *testing.T) {
	ctx := &Context{
		fillStyle:    ColorRed,
		strokeStyle:  ColorBlue,
		lineWidth:    3,
		lineCap:      LineCapSquare,
		lineJoin:     LineJoinRound,
		miterLimit:   7,
		globalAlpha:  0.4,
		shadowColor:  Color{R: 0.1, G: 0.2, B: 0.3, A: 0.5},
		shadowBlur:   6,
		textAlign:    TextAlignCenter,
		textBaseline: TextBaselineMiddle,
		transform:    Matrix{A: 1, D: 1, E: 10, F: 20},
		imageEffect:  ImageEffectElectric,
		effectTime:   9,
		stateStack:   make([]drawingState, 0, 1),
	}

	ctx.Save()
	ctx.SetFillStyle(ColorWhite)
	ctx.SetStrokeStyle(ColorBlack)
	ctx.SetLineWidth(1)
	ctx.SetLineCap(LineCapButt)
	ctx.SetLineJoin(LineJoinMiter)
	ctx.SetMiterLimit(1)
	ctx.SetGlobalAlpha(1)
	ctx.SetShadowColor(ColorTransparent)
	ctx.SetShadowBlur(0)
	ctx.SetTextAlign(TextAlignLeft)
	ctx.SetTextBaseline(TextBaselineTop)
	ctx.SetTransform(2, 0, 0, 2, 0, 0)
	ctx.SetImageEffect(ImageEffectNone)
	ctx.SetEffectTime(0)
	ctx.Restore()

	if ctx.fillStyle != ColorRed || ctx.strokeStyle != ColorBlue || ctx.lineWidth != 3 {
		t.Fatal("basic drawing state did not restore")
	}
	if ctx.lineCap != LineCapSquare || ctx.lineJoin != LineJoinRound || ctx.miterLimit != 7 {
		t.Fatal("stroke state did not restore")
	}
	if ctx.globalAlpha != 0.4 || ctx.shadowColor != (Color{R: 0.1, G: 0.2, B: 0.3, A: 0.5}) || ctx.shadowBlur != 6 {
		t.Fatal("alpha/shadow state did not restore")
	}
	if ctx.textAlign != TextAlignCenter || ctx.textBaseline != TextBaselineMiddle {
		t.Fatal("text state did not restore")
	}
	if ctx.transform != (Matrix{A: 1, D: 1, E: 10, F: 20}) || ctx.imageEffect != ImageEffectElectric || ctx.effectTime != 9 {
		t.Fatal("transform/effect state did not restore")
	}

	ctx.Restore()
}

func TestTextMeasurementAndPlacementUseKerning(t *testing.T) {
	atlas := &fontAtlas{
		Glyphs: map[rune]glyph{
			'a': {W: 2, H: 4, Advance: 10},
			't': {W: 2, H: 4, Advance: 8},
			'?': {W: 2, H: 4, Advance: 6},
		},
		Kerning: map[[2]rune]float32{
			{'a', 't'}: -2,
			{'t', '?'}: 1,
		},
	}
	ctx := &Context{
		width:     100,
		height:    100,
		transform: uiIdentityMatrix(),
	}

	if got := ctx.measureTextLine("at", atlas); got != 16 {
		t.Fatalf("expected kerned width 16, got %v", got)
	}
	if got := ctx.measureTextLine("até", atlas); got != 23 {
		t.Fatalf("expected fallback glyph to participate in kerning, got width %v", got)
	}

	var (
		allocations float64
		metrics     TextLineMetrics
		positions   []float32
	)
	ctx.currentFont = &fontFace{atlas: atlas}
	metrics, positions = ctx.MeasureTextLine("at", make([]float32, 0, 3))
	if metrics.Width != 16 || metrics.Ascent != atlas.Ascent || metrics.Descent != atlas.Descent || metrics.LineHeight != atlas.LineHeight {
		t.Fatalf("unexpected public text metrics: %+v", metrics)
	}
	if !slices.Equal(positions, []float32{0, 8, 16}) {
		t.Fatalf("unexpected kerned caret positions: %v", positions)
	}
	allocations = testing.AllocsPerRun(100, func() {
		metrics, positions = ctx.MeasureTextLine("at", positions[:0])
	})
	if allocations != 0 {
		t.Fatalf("MeasureTextLine allocated %v times with reusable storage", allocations)
	}
	ctx.SetTextKerning(false)
	metrics, positions = ctx.MeasureTextLine("at", positions[:0])
	if metrics.Width != 18 || !slices.Equal(positions, []float32{0, 10, 18}) {
		t.Fatalf("un-kerned measurement produced width=%v positions=%v", metrics.Width, positions)
	}
	ctx.SetTextKerning(true)

	ctx.appendTextLine(&fontFace{}, atlas, "at", 0, 20, ColorWhite)
	if len(ctx.vertices) != 12 {
		t.Fatalf("expected two glyph quads, got %d vertices", len(ctx.vertices))
	}

	secondGlyphX, _ := ctx.pixelToClip(8, 20)
	if got := ctx.vertices[6].Position[0]; got != secondGlyphX {
		t.Fatalf("expected second glyph x to include kerning, got %v, want %v", got, secondGlyphX)
	}
}

func TestMatrixHelpers(t *testing.T) {
	id := IdentityMatrix()
	if id != (Matrix{A: 1, D: 1}) {
		t.Fatalf("unexpected identity matrix: %#v", id)
	}

	m := NewTransformMatrix(10, 20, 2, 3, float32(math.Pi/2))
	p := m.Apply(1, 0)
	if !approx32(p.X, 10) || !approx32(p.Y, 22) {
		t.Fatalf("unexpected transformed point: %#v", p)
	}

	u := NewUniformTransformMatrix(5, 6, 2, 0)
	if u != (Matrix{A: 2, D: 2, E: 5, F: 6}) {
		t.Fatalf("unexpected uniform transform: %#v", u)
	}

	composed := Matrix{A: 1, D: 1, E: 3, F: 4}.Mul(Matrix{A: 2, D: 2})
	got := composed.Apply(1, 1)
	if got != (Point{X: 5, Y: 6}) {
		t.Fatalf("unexpected composed point: %#v", got)
	}
}

func TestTransformMutatorsAndPixelToClip(t *testing.T) {
	ctx := &Context{transform: uiIdentityMatrix(), width: 200, height: 100}

	ctx.SetTransform(1, 0, 0, 1, 10, 20)
	if ctx.GetTransform() != (Matrix{A: 1, D: 1, E: 10, F: 20}) {
		t.Fatalf("unexpected transform after SetTransform: %#v", ctx.GetTransform())
	}

	ctx.Transform(2, 0, 0, 2, 0, 0)
	p := ctx.transformPoint(5, 5)
	if p != (Point{X: 20, Y: 30}) {
		t.Fatalf("unexpected transformed point: %#v", p)
	}

	ctx.ResetTransform()
	ctx.SetObjectTransform(8, 9, 2, 0)
	if pt := ctx.transformPoint(1, 1); pt != (Point{X: 10, Y: 11}) {
		t.Fatalf("unexpected object transform point: %#v", pt)
	}

	ctx.ResetTransform()
	ctx.SetTransformPolite(1, 2, 3, 0)
	if pt := ctx.transformPoint(1, 1); pt != (Point{X: 4, Y: 5}) {
		t.Fatalf("unexpected polite transform point: %#v", pt)
	}

	clipX, clipY := ctx.pixelToClip(100, 50)
	if !approx32(clipX, 0) || !approx32(clipY, 0) {
		t.Fatalf("unexpected clip coords: %v %v", clipX, clipY)
	}
}

func TestFlattenPathAndCurrentPoint(t *testing.T) {
	ctx := &Context{transform: uiIdentityMatrix()}
	ctx.MoveTo(0, 0)
	ctx.LineTo(10, 0)
	ctx.ClosePath()
	ctx.MoveTo(5, 5)
	ctx.LineTo(8, 9)

	if kind := ctx.lastPathCommandKind(); kind != pathCommandLineTo {
		t.Fatalf("unexpected last path kind: %v", kind)
	}

	if pt, ok := ctx.currentPathPoint(); !ok || pt != (Point{X: 8, Y: 9}) {
		t.Fatalf("unexpected current path point: %#v ok=%v", pt, ok)
	}

	subpaths := ctx.flattenPath()
	if len(subpaths) != 2 {
		t.Fatalf("expected 2 subpaths, got %d", len(subpaths))
	}
	if !subpaths[0].closed || subpaths[1].closed {
		t.Fatal("unexpected closed flags in flattened path")
	}

	ctx.ClosePath()
	if _, ok := ctx.currentPathPoint(); ok {
		t.Fatal("expected no current path point after close")
	}
}

func TestArcAndCurveHelpers(t *testing.T) {
	ctx := &Context{transform: uiIdentityMatrix()}

	arc := ctx.arcPoints(10, 20, 5, 0, float32(math.Pi/2), false)
	if len(arc) < 12 {
		t.Fatalf("expected many arc points, got %d", len(arc))
	}
	if !approx32(arc[0].X, 15) || !approx32(arc[0].Y, 20) {
		t.Fatalf("unexpected first arc point: %#v", arc[0])
	}

	ccw := ctx.ellipsePoints(0, 0, 4, 2, 0, 0, float32(math.Pi/2), true)
	if len(ccw) < 12 {
		t.Fatalf("expected many ccw ellipse points, got %d", len(ccw))
	}
	if ccw[0] == ccw[len(ccw)-1] {
		t.Fatal("expected counter-clockwise ellipse sweep to traverse distinct endpoints")
	}

	full := ctx.ellipsePoints(0, 0, 2, 2, 0, 0, float32(math.Pi*4), false)
	if len(full) < 13 {
		t.Fatalf("expected full ellipse approximation, got %d", len(full))
	}

	q := ctx.quadraticCurvePoints(Point{0, 0}, Point{10, 10}, Point{20, 0})
	if len(q) < 13 || q[0] != (Point{0, 0}) || q[len(q)-1] != (Point{20, 0}) {
		t.Fatal("quadratic curve approximation endpoints are wrong")
	}

	c := ctx.cubicCurvePoints(Point{0, 0}, Point{10, 20}, Point{20, 20}, Point{30, 0})
	if len(c) < 13 || c[0] != (Point{0, 0}) || c[len(c)-1] != (Point{30, 0}) {
		t.Fatal("cubic curve approximation endpoints are wrong")
	}

	if steps := curveSubdivisionSteps([]Point{{0, 0}, {1, 1}}); steps != 12 {
		t.Fatalf("expected minimum subdivision steps of 12, got %d", steps)
	}
	if steps := curveSubdivisionSteps([]Point{{0, 0}, {5000, 0}}); steps != 192 {
		t.Fatalf("expected subdivision clamp to 192, got %d", steps)
	}
}

func TestPathBuilders(t *testing.T) {
	ctx := &Context{transform: uiIdentityMatrix()}

	ctx.BeginPath()
	ctx.Rect(0, 0, 10, 20)
	if len(ctx.flattenPath()) != 1 {
		t.Fatal("expected rect to produce one subpath")
	}

	ctx.BeginPath()
	ctx.RoundedRect(10, 20, -30, -40, 50)
	if len(ctx.path) == 0 {
		t.Fatal("expected rounded rect path commands")
	}

	ctx.BeginPath()
	ctx.Circle(10, 10, 0)
	if len(ctx.path) != 0 {
		t.Fatal("zero-radius circle should not append path commands")
	}

	ctx.BeginPath()
	ctx.Polygon([]Point{{0, 0}, {10, 0}, {10, 10}})
	if subpaths := ctx.flattenPath(); len(subpaths) != 1 || !subpaths[0].closed {
		t.Fatal("polygon should create one closed subpath")
	}

	ctx.BeginPath()
	ctx.Polyline([]Point{{0, 0}, {10, 0}, {10, 10}})
	if subpaths := ctx.flattenPath(); len(subpaths) != 1 || subpaths[0].closed {
		t.Fatal("polyline should create one open subpath")
	}

	ctx.BeginPath()
	ctx.LineTo(5, 6)
	if len(ctx.path) == 0 || ctx.path[0].kind != pathCommandMoveTo {
		t.Fatal("LineTo with empty path should start with MoveTo")
	}

	ctx.BeginPath()
	ctx.Arc(0, 0, 0, 0, 1, false)
	ctx.Ellipse(0, 0, 0, 1, 0, 0, 1, false)
	if len(ctx.path) != 0 {
		t.Fatal("zero-radius arc/ellipse should not append commands")
	}
}

func TestBatchAndShadowHelpers(t *testing.T) {
	ctx := &Context{
		batches:     make([]batch, 0, 4),
		globalAlpha: 0.5,
		shadowColor: Color{R: 1, G: 0.5, B: 0.25, A: 0.8},
		shadowBlur:  6,
		currentFont: &fontFace{group: nil},
		fontFaces:   map[fontFaceKey]*fontFace{},
		imageEffect: ImageEffectElectric,
		transform:   uiIdentityMatrix(),
	}

	ctx.appendBatch(nil, nil, nil, 0, 3)
	if len(ctx.batches) != 0 {
		t.Fatal("appendBatch should skip nil groups")
	}

	group := &wgpu.BindGroup{}
	// cannot instantiate real group safely; use nil-guard path only
	_ = group

	if !ctx.hasShadow() {
		t.Fatal("expected shadow to be enabled")
	}
	if got := ctx.applyGlobalAlpha(Color{A: 1}); got.A != 0.5 {
		t.Fatalf("unexpected global alpha result: %#v", got)
	}
	if got := ctx.effectiveShadowColor(); !approx32(got.A, 0.4) {
		t.Fatalf("unexpected effective shadow alpha: %#v", got)
	}
	if got := ctx.effectiveShadowBlurForQuads(); got != 6 {
		t.Fatalf("unexpected effective shadow blur: %v", got)
	}
	if pad := quadShadowPadding(3); pad != 8 {
		t.Fatalf("unexpected shadow padding: %v", pad)
	}

	samples := shapeShadowSamples(6)
	if len(samples) == 0 {
		t.Fatal("expected shadow samples")
	}
	var sum float32
	for _, sample := range samples {
		sum += sample.weight
	}
	if !approx32(sum, 1) {
		t.Fatalf("expected normalized shadow weights, got %v", sum)
	}

	ctx.currentImageShader = stubImageShader{}
	if got := ctx.effectiveShadowColorForQuads(); got != ColorTransparent {
		t.Fatalf("expected transparent quad shadow color with custom image shader, got %#v", got)
	}
	if got := ctx.effectiveShadowBlurForQuads(); got != 0 {
		t.Fatalf("expected quad shadow blur disabled with custom image shader, got %v", got)
	}
}

func TestLineIntersectionAndBatchMerging(t *testing.T) {
	pt, ok := lineIntersection(Point{0, 0}, Point{10, 10}, Point{0, 10}, Point{10, 0})
	if !ok || !approx32(pt.X, 5) || !approx32(pt.Y, 5) {
		t.Fatalf("unexpected line intersection: %#v ok=%v", pt, ok)
	}

	if _, ok := lineIntersection(Point{0, 0}, Point{10, 0}, Point{0, 1}, Point{10, 1}); ok {
		t.Fatal("expected parallel lines to have no intersection")
	}

	ctx := &Context{batches: make([]batch, 0, 4)}
	g := &wgpu.BindGroup{}
	ctx.appendBatch(nil, g, nil, 0, 3)
	ctx.appendBatch(nil, g, nil, 3, 3)
	if len(ctx.batches) != 1 || ctx.batches[0].count != 6 {
		t.Fatalf("expected adjacent batches to merge, got %#v", ctx.batches)
	}
}

func TestOffscreenCanvasNilAndAccessors(t *testing.T) {
	var c *OffscreenCanvas
	if tex := c.Texture(); tex != nil {
		t.Fatal("nil canvas Texture should return nil")
	}
	if view := c.View(); view != nil {
		t.Fatal("nil canvas View should return nil")
	}
	if sampler := c.Sampler(); sampler != nil {
		t.Fatal("nil canvas Sampler should return nil")
	}
	if format := c.Format(); format != DefaultOffscreenFormat {
		t.Fatalf("unexpected default format: %v", format)
	}
	if img := c.Image("x"); img != nil {
		t.Fatal("nil canvas Image should return nil")
	}
	if err := c.Flush(nil); err == nil {
		t.Fatal("expected nil canvas Flush to fail")
	}

	canvas := &OffscreenCanvas{Width: 12, Height: 34, targetFormat: DefaultOffscreenFormat}
	if img := canvas.Image("foo"); img != nil {
		t.Fatal("expected Image to be nil without GPU resources")
	}
	canvas.Release()
}

type stubImageShader struct{}

func (stubImageShader) imagePipeline() *wgpu.RenderPipeline    { return nil }
func (stubImageShader) imageUniformBindGroup() *wgpu.BindGroup { return nil }

func approx32(a, b float32) bool {
	return float32(math.Abs(float64(a-b))) < 0.001
}
