package gctx2d

import (
	"fmt"
	"math"
	"strings"
	"unsafe"

	"github.com/gogpu/gputypes"
	"github.com/gogpu/wgpu"
	"github.com/z46-dev/gctx2d/gimage"
)

const (
	ctxKindRoundedRect = 0
	ctxKindText        = 1
	ctxKindImage       = 2
)

const (
	ImageEffectNone ImageEffect = iota
	ImageEffectElectric
)

const (
	pathCommandMoveTo pathCommandKind = iota
	pathCommandLineTo
	pathCommandClose
)

var vertexStride = uint64(unsafe.Sizeof(vertex{}))

// NewContext creates a new UI renderer with the specified default font.
// The fontPath should point to a .ttf/.otf file on the user's system, and fontSize is the default pixel size for rendering text.
// If fontPath is empty, it will attempt to find a common system font automatically.
func NewContext(device *wgpu.Device, targetFormat gputypes.TextureFormat) (renderer *Context, err error) {
	if device == nil {
		err = fmt.Errorf("nil wgpu device")
		return
	}

	var defaultFont *FontHandle
	if defaultFont, err = LoadFontByName(DefaultFontName()); err != nil {
		return
	}

	renderer = &Context{
		device:       device,
		targetFormat: targetFormat,
		imageGroups:  make(map[*gimage.Image]*wgpu.BindGroup),
		fontFaces:    make(map[fontFaceKey]*fontFace),
		defaultFont:  defaultFont,
		vertices:     make([]vertex, 0, 1024),
		batches:      make([]batch, 0, 128),
		fillStyle:    ColorWhite,
		strokeStyle:  ColorWhite,
		lineWidth:    1,
		transform:    uiIdentityMatrix(),
		imageEffect:  ImageEffectNone,
		stateStack:   make([]drawingState, 0, 32),
		path:         make([]pathCommand, 0, 128),
	}

	if err = renderer.createFontSampler(); err != nil {
		renderer.Release()
		renderer = nil
		return
	}

	if err = renderer.createPipeline(targetFormat); err != nil {
		renderer.Release()
		renderer = nil
		return
	}

	if err = renderer.SetFont(16, nil); err != nil {
		renderer.Release()
		renderer = nil
		return
	}

	return
}

// Release frees all GPU resources associated with the UI renderer. It is safe to call multiple times.
func (r *Context) Release() {
	if r == nil {
		return
	}

	for _, sub := range r.pending {
		if sub.cmd != nil {
			sub.cmd.Release()
		}
	}

	r.pending = nil

	for _, group := range r.imageGroups {
		if group != nil {
			group.Release()
		}
	}

	r.imageGroups = nil

	if r.vertexBuffer != nil {
		r.vertexBuffer.Release()
		r.vertexBuffer = nil
	}

	if r.pipeline != nil {
		r.pipeline.Release()
		r.pipeline = nil
	}

	if r.pipelineLayout != nil {
		r.pipelineLayout.Release()
		r.pipelineLayout = nil
	}

	for _, face := range r.fontFaces {
		if face != nil {
			face.Release()
		}
	}

	r.fontFaces = nil
	r.currentFont = nil
	r.defaultFont = nil

	if r.textureLayout != nil {
		r.textureLayout.Release()
		r.textureLayout = nil
	}

	if r.fontSampler != nil {
		r.fontSampler.Release()
		r.fontSampler = nil
	}
}

// createFontSampler creates the sampler shared by all font atlas textures.
func (r *Context) createFontSampler() (err error) {
	if r.fontSampler, err = r.device.CreateSampler(&wgpu.SamplerDescriptor{
		Label:        "Conquest UI Font Atlas Sampler",
		AddressModeU: gputypes.AddressModeClampToEdge,
		AddressModeV: gputypes.AddressModeClampToEdge,
		AddressModeW: gputypes.AddressModeClampToEdge,
		MagFilter:    gputypes.FilterModeLinear,
		MinFilter:    gputypes.FilterModeLinear,
		MipmapFilter: gputypes.FilterModeNearest,
		LodMinClamp:  0,
		LodMaxClamp:  32,
	}); err != nil {
		err = fmt.Errorf("create UI font atlas sampler: %w", err)
	}

	return
}

// Release frees the GPU resources associated with a cached font face.
func (f *fontFace) Release() {
	if f == nil {
		return
	}

	if f.group != nil {
		f.group.Release()
		f.group = nil
	}

	if f.view != nil {
		f.view.Release()
		f.view = nil
	}

	if f.texture != nil {
		f.texture.Release()
		f.texture = nil
	}

	f.atlas = nil
}

// SetFont selects the active font face used by FillText and StrokeText.
// Passing a nil handle selects the context's default font.
// An optional weight selects a cached synthetic weight variant. If omitted, FontWeightRegular is used.
func (r *Context) SetFont(size float64, handle *FontHandle, weights ...FontWeight) (err error) {
	if r == nil {
		err = fmt.Errorf("nil gctx2d context")
		return
	}

	if handle == nil {
		handle = r.defaultFont
	}

	if handle == nil {
		err = fmt.Errorf("nil font handle")
		return
	}

	if size <= 0 {
		err = fmt.Errorf("font size must be positive")
		return
	}

	weight := fontWeightFromArgs(weights)

	if r.textureLayout == nil || r.fontSampler == nil {
		err = fmt.Errorf("font resources are not initialized")
		return
	}

	if r.fontFaces == nil {
		r.fontFaces = make(map[fontFaceKey]*fontFace)
	}

	var key fontFaceKey = fontFaceKey{handle: handle, size: size, weight: weight}
	if face := r.fontFaces[key]; face != nil {
		r.currentFont = face
		return
	}

	var atlas *fontAtlas
	if atlas, err = buildFontAtlas(handle, size, weight); err != nil {
		return
	}

	face := &fontFace{
		key:   key,
		atlas: atlas,
	}

	defer func() {
		if err != nil {
			face.Release()
		}
	}()

	if face.texture, err = r.device.CreateTexture(&wgpu.TextureDescriptor{
		Label: "Conquest UI Font Atlas",
		Size: wgpu.Extent3D{
			Width:              uint32(atlas.Width),
			Height:             uint32(atlas.Height),
			DepthOrArrayLayers: 1,
		},
		MipLevelCount: 1,
		SampleCount:   1,
		Dimension:     gputypes.TextureDimension2D,
		Format:        gputypes.TextureFormatRGBA8Unorm,
		Usage:         gputypes.TextureUsageTextureBinding | gputypes.TextureUsageCopyDst,
	}); err != nil {
		err = fmt.Errorf("create UI font atlas texture: %w", err)
		return
	}

	if err = r.device.Queue().WriteTexture(
		&wgpu.ImageCopyTexture{
			Texture:  face.texture,
			MipLevel: 0,
			Origin:   wgpu.Origin3D{X: 0, Y: 0, Z: 0},
			Aspect:   gputypes.TextureAspectAll,
		},
		atlas.Pixels,
		&wgpu.ImageDataLayout{
			Offset:       0,
			BytesPerRow:  uint32(atlas.Width * 4),
			RowsPerImage: uint32(atlas.Height),
		},
		&wgpu.Extent3D{
			Width:              uint32(atlas.Width),
			Height:             uint32(atlas.Height),
			DepthOrArrayLayers: 1,
		},
	); err != nil {
		err = fmt.Errorf("upload UI font atlas texture: %w", err)
		return
	}

	if face.view, err = r.device.CreateTextureView(face.texture, nil); err != nil {
		err = fmt.Errorf("create UI font atlas texture view: %w", err)
		return
	}

	if face.group, err = r.device.CreateBindGroup(&wgpu.BindGroupDescriptor{
		Label:  "Conquest UI Font Atlas Bind Group",
		Layout: r.textureLayout,
		Entries: []wgpu.BindGroupEntry{
			{Binding: 0, Sampler: r.fontSampler},
			{Binding: 1, TextureView: face.view},
		},
	}); err != nil {
		err = fmt.Errorf("create UI font atlas bind group: %w", err)
		return
	}

	r.fontFaces[key] = face
	r.currentFont = face
	return
}

// createPipeline compiles the UI shader and creates the render pipeline for drawing UI elements.
func (r *Context) createPipeline(targetFormat gputypes.TextureFormat) (err error) {
	var shader *wgpu.ShaderModule
	if shader, err = r.device.CreateShaderModule(&wgpu.ShaderModuleDescriptor{
		Label: "Conquest UI Shader",
		WGSL:  ctxWGSL,
	}); err != nil {
		err = fmt.Errorf("create UI shader module: %w", err)
		return
	}

	defer shader.Release()

	if r.textureLayout, err = r.device.CreateBindGroupLayout(&wgpu.BindGroupLayoutDescriptor{
		Label: "Conquest UI Atlas Layout",
		Entries: []gputypes.BindGroupLayoutEntry{
			{
				Binding:    0,
				Visibility: gputypes.ShaderStageFragment,
				Sampler: &gputypes.SamplerBindingLayout{
					Type: gputypes.SamplerBindingTypeFiltering,
				},
			},
			{
				Binding:    1,
				Visibility: gputypes.ShaderStageFragment,
				Texture: &gputypes.TextureBindingLayout{
					SampleType:    gputypes.TextureSampleTypeFloat,
					ViewDimension: gputypes.TextureViewDimension2D,
					Multisampled:  false,
				},
			},
		},
	}); err != nil {
		err = fmt.Errorf("create UI atlas bind group layout: %w", err)
		return
	}

	if r.pipelineLayout, err = r.device.CreatePipelineLayout(&wgpu.PipelineLayoutDescriptor{
		Label:            "Conquest UI Pipeline Layout",
		BindGroupLayouts: []*wgpu.BindGroupLayout{r.textureLayout},
	}); err != nil {
		err = fmt.Errorf("create UI pipeline layout: %w", err)
		return
	}

	var blend gputypes.BlendState = gputypes.BlendState{
		Color: gputypes.BlendComponent{
			Operation: gputypes.BlendOperationAdd,
			SrcFactor: gputypes.BlendFactorSrcAlpha,
			DstFactor: gputypes.BlendFactorOneMinusSrcAlpha,
		},
		Alpha: gputypes.BlendComponent{
			Operation: gputypes.BlendOperationAdd,
			SrcFactor: gputypes.BlendFactorOne,
			DstFactor: gputypes.BlendFactorOneMinusSrcAlpha,
		},
	}

	if r.pipeline, err = r.device.CreateRenderPipeline(&wgpu.RenderPipelineDescriptor{
		Label:  "Conquest UI Pipeline",
		Layout: r.pipelineLayout,
		Vertex: wgpu.VertexState{
			Module:     shader,
			EntryPoint: "vs_main",
			Buffers: []gputypes.VertexBufferLayout{
				{
					ArrayStride: vertexStride,
					StepMode:    gputypes.VertexStepModeVertex,
					Attributes: []gputypes.VertexAttribute{
						{ShaderLocation: 0, Offset: 0, Format: gputypes.VertexFormatFloat32x2},
						{ShaderLocation: 1, Offset: 8, Format: gputypes.VertexFormatFloat32x2},
						{ShaderLocation: 2, Offset: 16, Format: gputypes.VertexFormatFloat32x2},
						{ShaderLocation: 3, Offset: 24, Format: gputypes.VertexFormatFloat32x2},
						{ShaderLocation: 4, Offset: 32, Format: gputypes.VertexFormatFloat32x4},
						{ShaderLocation: 5, Offset: 48, Format: gputypes.VertexFormatFloat32},
						{ShaderLocation: 6, Offset: 52, Format: gputypes.VertexFormatFloat32},
						{ShaderLocation: 7, Offset: 56, Format: gputypes.VertexFormatFloat32},
						{ShaderLocation: 8, Offset: 60, Format: gputypes.VertexFormatFloat32},
					},
				},
			},
		},
		Primitive: gputypes.PrimitiveState{
			Topology: gputypes.PrimitiveTopologyTriangleList,
			CullMode: gputypes.CullModeNone,
		},
		Fragment: &wgpu.FragmentState{
			Module:     shader,
			EntryPoint: "fs_main",
			Targets: []gputypes.ColorTargetState{
				{
					Format:    targetFormat,
					Blend:     &blend,
					WriteMask: gputypes.ColorWriteMaskAll,
				},
			},
		},
	}); err != nil {
		err = fmt.Errorf("create UI render pipeline: %w", err)
	}

	return
}

// Begin starts a new UI frame with the specified dimensions. It must be called before issuing any draw calls.
func (r *Context) Begin(width, height int) {
	r.width = width
	r.height = height
	r.vertices = r.vertices[:0]
	r.batches = r.batches[:0]
	r.path = r.path[:0]
	r.transform = uiIdentityMatrix()
	r.imageEffect = ImageEffectNone
	r.currentImageShader = nil
	r.effectTime = 0
	r.stateStack = r.stateStack[:0]
}

// SetFillStyle sets the color used by Fill.
func (r *Context) SetFillStyle(color Color) {
	r.fillStyle = color
}

// SetStrokeStyle sets the color used by Stroke.
func (r *Context) SetStrokeStyle(color Color) {
	r.strokeStyle = color
}

// SetLineWidth sets the stroke width used by Stroke.
func (r *Context) SetLineWidth(width float32) {
	if width <= 0 {
		width = 1
	}

	r.lineWidth = width
}

// SetTextAlign sets the horizontal anchor used by FillText and StrokeText.
func (r *Context) SetTextAlign(align TextAlign) {
	switch align {
	case TextAlignLeft, TextAlignCenter, TextAlignRight:
		r.textAlign = align
	default:
		r.textAlign = TextAlignLeft
	}
}

// SetTextBaseline sets the vertical anchor used by FillText and StrokeText.
func (r *Context) SetTextBaseline(baseline TextBaseline) {
	switch baseline {
	case TextBaselineAlphabetic, TextBaselineTop, TextBaselineMiddle, TextBaselineBottom:
		r.textBaseline = baseline
	default:
		r.textBaseline = TextBaselineAlphabetic
	}
}

// SetImageEffect sets the image shader effect used by subsequent DrawImage calls.
func (r *Context) SetImageEffect(effect ImageEffect) {
	r.imageEffect = effect
}

// ResetImageEffect restores the default built-in image effect.
func (r *Context) ResetImageEffect() {
	r.imageEffect = ImageEffectNone
}

// SetImageShader sets a custom image shader used by subsequent DrawImage calls.
func (r *Context) SetImageShader(shader ImageShaderBinding) {
	r.currentImageShader = shader
}

// ResetImageShader restores the default image shader for subsequent DrawImage calls.
func (r *Context) ResetImageShader() {
	r.currentImageShader = nil
}

// SetEffectTime sets the time value used by animated image effects.
func (r *Context) SetEffectTime(seconds float32) {
	r.effectTime = seconds
}

func (r *Context) drawingState() drawingState {
	return drawingState{
		fillStyle:          r.fillStyle,
		strokeStyle:        r.strokeStyle,
		lineWidth:          r.lineWidth,
		textAlign:          r.textAlign,
		textBaseline:       r.textBaseline,
		transform:          r.transform,
		imageEffect:        r.imageEffect,
		effectTime:         r.effectTime,
		currentImageShader: r.currentImageShader,
		currentFont:        r.currentFont,
	}
}

func (r *Context) restoreDrawingState(state drawingState) {
	r.fillStyle = state.fillStyle
	r.strokeStyle = state.strokeStyle
	r.lineWidth = state.lineWidth
	r.textAlign = state.textAlign
	r.textBaseline = state.textBaseline
	r.transform = state.transform
	r.imageEffect = state.imageEffect
	r.effectTime = state.effectTime
	r.currentImageShader = state.currentImageShader
	r.currentFont = state.currentFont
}

// Save pushes the current drawing state onto the stack.
func (r *Context) Save() {
	r.stateStack = append(r.stateStack, r.drawingState())
}

// Restore pops the most recently saved drawing state from the stack.
func (r *Context) Restore() {
	if len(r.stateStack) == 0 {
		return
	}

	last := len(r.stateStack) - 1
	state := r.stateStack[last]
	r.stateStack = r.stateStack[:last]
	r.restoreDrawingState(state)
}

// GetTransform returns the current transform matrix.
func (r *Context) GetTransform() Matrix {
	return r.transform
}

// ResetTransform restores the current canvas-style transform to the identity matrix.
func (r *Context) ResetTransform() {
	r.transform = uiIdentityMatrix()
}

// SetTransform replaces the current canvas-style transform with the specified affine matrix.
// The coefficients follow the HTML canvas convention: x' = a*x + c*y + e, y' = b*x + d*y + f.
func (r *Context) SetTransform(a, b, c, d, e, f float32) {
	r.transform = Matrix{A: a, B: b, C: c, D: d, E: e, F: f}
}

// Transform post-multiplies the current canvas-style transform by the specified affine matrix.
func (r *Context) Transform(a, b, c, d, e, f float32) {
	r.transform = r.transform.Mul(Matrix{A: a, B: b, C: c, D: d, E: e, F: f})
}

// SetMatrixTransform replaces the current canvas-style transform with the provided matrix.
func (r *Context) SetMatrixTransform(transform Matrix) {
	r.transform = transform
}

// TransformMatrix post-multiplies the current canvas-style transform by the provided matrix.
func (r *Context) TransformMatrix(transform Matrix) {
	r.transform = r.transform.Mul(transform)
}

// SetObjectTransform composes an object-local translation, uniform scale, and rotation with the current transform.
// Rotation is in radians.
func (r *Context) SetObjectTransform(x, y, size, rotation float32) {
	r.TransformMatrix(NewUniformTransformMatrix(x, y, size, rotation))
}

// SetTransformPolite is kept as a compatibility alias for SetObjectTransform.
func (r *Context) SetTransformPolite(x, y, size, rotation float32) {
	r.SetObjectTransform(x, y, size, rotation)
}

// BeginPath clears the current canvas-style path.
func (r *Context) BeginPath() {
	r.path = r.path[:0]
}

// MoveTo starts a new canvas-style subpath.
func (r *Context) MoveTo(x, y float32) {
	var point Point = r.transformPoint(x, y)
	r.path = append(r.path, pathCommand{
		kind: pathCommandMoveTo,
		p:    point,
	})
}

// LineTo appends a straight line to the current canvas-style path.
func (r *Context) LineTo(x, y float32) {
	if len(r.path) == 0 || r.lastPathCommandKind() == pathCommandClose {
		r.MoveTo(x, y)
		return
	}

	var point Point = r.transformPoint(x, y)
	r.path = append(r.path, pathCommand{
		kind: pathCommandLineTo,
		p:    point,
	})
}

// ClosePath closes the current canvas-style subpath.
func (r *Context) ClosePath() {
	if len(r.path) == 0 || r.lastPathCommandKind() == pathCommandClose {
		return
	}

	r.path = append(r.path, pathCommand{kind: pathCommandClose})
}

// Rect appends a closed rectangle subpath. Call Fill or Stroke to draw it.
func (r *Context) Rect(x, y, w, h float32) {
	if w == 0 || h == 0 {
		return
	}

	r.MoveTo(x, y)
	r.LineTo(x+w, y)
	r.LineTo(x+w, y+h)
	r.LineTo(x, y+h)
	r.ClosePath()
}

// RoundedRect appends a closed rounded rectangle subpath. Call Fill or Stroke to draw it.
func (r *Context) RoundedRect(x, y, w, h, radius float32) {
	if w == 0 || h == 0 {
		return
	}

	if radius <= 0 {
		r.Rect(x, y, w, h)
		return
	}

	if w < 0 {
		x += w
		w = -w
	}

	if h < 0 {
		y += h
		h = -h
	}

	var rds float32 = radius
	if rds > w*0.5 {
		rds = w * 0.5
	}

	if rds > h*0.5 {
		rds = h * 0.5
	}

	var (
		x0 float32 = x
		y0 float32 = y
		x1 float32 = x + w
		y1 float32 = y + h
	)

	r.MoveTo(x0+rds, y0)
	r.LineTo(x1-rds, y0)
	r.arcPointsToPath(r.arcPoints(x1-rds, y0+rds, rds, -float32(math.Pi)*0.5, 0, false), true)
	r.LineTo(x1, y1-rds)
	r.arcPointsToPath(r.arcPoints(x1-rds, y1-rds, rds, 0, float32(math.Pi)*0.5, false), true)
	r.LineTo(x0+rds, y1)
	r.arcPointsToPath(r.arcPoints(x0+rds, y1-rds, rds, float32(math.Pi)*0.5, float32(math.Pi), false), true)
	r.LineTo(x0, y0+rds)
	r.arcPointsToPath(r.arcPoints(x0+rds, y0+rds, rds, float32(math.Pi), float32(math.Pi)*1.5, false), true)
	r.ClosePath()
}

// Polygon appends a closed polygon subpath. Call Fill or Stroke to draw it.
func (r *Context) Polygon(points []Point) {
	if len(points) == 0 {
		return
	}

	r.MoveTo(points[0].X, points[0].Y)
	for i := 1; i < len(points); i++ {
		r.LineTo(points[i].X, points[i].Y)
	}

	r.ClosePath()
}

// Polyline appends an open polyline subpath. Call Stroke to draw it.
func (r *Context) Polyline(points []Point) {
	if len(points) == 0 {
		return
	}

	r.MoveTo(points[0].X, points[0].Y)
	for i := 1; i < len(points); i++ {
		r.LineTo(points[i].X, points[i].Y)
	}
}

// Line appends an open line segment subpath. Call Stroke to draw it.
func (r *Context) Line(x0, y0, x1, y1 float32) {
	r.MoveTo(x0, y0)
	r.LineTo(x1, y1)
}

// Circle appends a closed circular subpath. Call Fill or Stroke to draw it.
func (r *Context) Circle(cx, cy, radius float32) {
	if radius <= 0 {
		return
	}

	r.Ellipse(cx, cy, radius, radius, 0, 0, float32(math.Pi)*2, false)
	r.ClosePath()
}

// Arc appends a circular arc to the current canvas-style path. Angles are in radians.
func (r *Context) Arc(cx, cy, radius, startAngle, endAngle float32, counterClockwise bool) {
	if radius <= 0 {
		return
	}

	r.arcPointsToPath(r.arcPoints(cx, cy, radius, startAngle, endAngle, counterClockwise), true)
}

// Ellipse appends an elliptical arc to the current canvas-style path. Angles are in radians.
func (r *Context) Ellipse(cx, cy, radiusX, radiusY, rotation, startAngle, endAngle float32, counterClockwise bool) {
	if radiusX <= 0 || radiusY <= 0 {
		return
	}

	r.arcPointsToPath(r.ellipsePoints(cx, cy, radiusX, radiusY, rotation, startAngle, endAngle, counterClockwise), true)
}

// QuadraticCurveTo appends a quadratic Bezier curve to the current canvas-style path.
func (r *Context) QuadraticCurveTo(cpx, cpy, x, y float32) {
	var (
		control Point = r.transformPoint(cpx, cpy)
		end     Point = r.transformPoint(x, y)
		start   Point
		ok      bool
	)

	if start, ok = r.currentPathPoint(); !ok {
		r.MoveTo(cpx, cpy)
		start = control
	}

	r.curvePointsToPath(r.quadraticCurvePoints(start, control, end))
}

// BezierCurveTo appends a cubic Bezier curve to the current canvas-style path.
func (r *Context) BezierCurveTo(cp1x, cp1y, cp2x, cp2y, x, y float32) {
	var (
		control1 Point = r.transformPoint(cp1x, cp1y)
		control2 Point = r.transformPoint(cp2x, cp2y)
		end      Point = r.transformPoint(x, y)
		start    Point
		ok       bool
	)

	if start, ok = r.currentPathPoint(); !ok {
		r.MoveTo(cp1x, cp1y)
		start = control1
	}

	r.curvePointsToPath(r.cubicCurvePoints(start, control1, control2, end))
}

// CubicCurveTo is a compatibility alias for BezierCurveTo.
func (r *Context) CubicCurveTo(cp1x, cp1y, cp2x, cp2y, x, y float32) {
	r.BezierCurveTo(cp1x, cp1y, cp2x, cp2y, x, y)
}

// Fill fills all current canvas-style subpaths with the current fill style. The current tessellator is a simple fan,
// so it is intended for convex subpaths.
func (r *Context) Fill() {
	for _, subpath := range r.flattenPath() {
		r.appendFilledPolygon(subpath.points, r.fillStyle)
	}
}

// Stroke strokes all current canvas-style subpaths with the current stroke style and line width.
func (r *Context) Stroke() {
	for _, subpath := range r.flattenPath() {
		r.appendStrokePath(subpath.points, subpath.closed, r.lineWidth, r.strokeStyle)
	}
}

// DrawImage draws an image at its natural size with its top-left corner at x/y.
func (r *Context) DrawImage(img *gimage.Image, x, y float32) {
	if img == nil {
		return
	}

	r.DrawImageBounds(img, x, y, float32(img.Width), float32(img.Height))
}

// DrawImageBounds draws an image stretched into the specified destination bounds.
func (r *Context) DrawImageBounds(img *gimage.Image, x, y, width, height float32) {
	if img == nil {
		return
	}

	r.DrawImageClipBounds(img, x, y, width, height, 0, 0, float32(img.Width), float32(img.Height))
}

// DrawImageClipBounds draws a clipped source rectangle into the specified destination bounds.
func (r *Context) DrawImageClipBounds(img *gimage.Image, x, y, width, height, x1, y1, x2, y2 float32) {
	r.appendImageClipBounds(img, r.transform, x, y, width, height, x1, y1, x2, y2)
}

// DrawImageBoundsTransformed draws an image using an object-local transform that is composed with the current context transform.
func (r *Context) DrawImageBoundsTransformed(img *gimage.Image, transform Matrix, x, y, width, height float32) {
	if img == nil {
		return
	}

	r.DrawImageClipBoundsTransformed(img, transform, x, y, width, height, 0, 0, float32(img.Width), float32(img.Height))
}

// DrawImageClipBoundsTransformed draws a clipped source rectangle using an object-local transform composed with the current context transform.
func (r *Context) DrawImageClipBoundsTransformed(img *gimage.Image, transform Matrix, x, y, width, height, x1, y1, x2, y2 float32) {
	if img == nil {
		return
	}

	r.appendImageClipBounds(img, r.transform.Mul(transform), x, y, width, height, x1, y1, x2, y2)
}

func (r *Context) appendImageClipBounds(img *gimage.Image, transform Matrix, x, y, width, height, x1, y1, x2, y2 float32) {
	if img == nil || img.View == nil || img.Sampler == nil || width == 0 || height == 0 || r.width <= 0 || r.height <= 0 {
		return
	}

	group, err := r.imageGroup(img)
	if err != nil {
		return
	}

	var (
		pipeline *wgpu.RenderPipeline
		uniform  *wgpu.BindGroup
	)

	if r.currentImageShader != nil {
		pipeline = r.currentImageShader.imagePipeline()
		uniform = r.currentImageShader.imageUniformBindGroup()
	}

	var (
		p0 Point = transform.Apply(x, y)
		p1 Point = transform.Apply(x+width, y)
		p2 Point = transform.Apply(x+width, y+height)
		p3 Point = transform.Apply(x, y+height)

		u0 float32 = x1 / float32(img.Width)
		v0 float32 = y1 / float32(img.Height)
		u1 float32 = x2 / float32(img.Width)
		v1 float32 = y2 / float32(img.Height)

		positions [6]Point      = [6]Point{p0, p1, p2, p0, p2, p3}
		locals    [6][2]float32 = [6][2]float32{
			{-1, -1},
			{1, -1},
			{1, 1},
			{-1, -1},
			{1, 1},
			{-1, 1},
		}
		uvs [6][2]float32 = [6][2]float32{
			{u0, v0},
			{u1, v0},
			{u1, v1},
			{u0, v0},
			{u1, v1},
			{u0, v1},
		}
		start uint32  = uint32(len(r.vertices))
		kind  float32 = ctxKindImage + float32(r.imageEffect)
	)

	if r.currentImageShader != nil {
		kind = ctxKindImage
	}

	for i := range 6 {
		clipX, clipY := r.pixelToClip(positions[i].X, positions[i].Y)
		r.vertices = append(r.vertices, vertex{
			Position:   [2]float32{clipX, clipY},
			Local:      locals[i],
			HalfSize:   [2]float32{1, 1},
			UV:         uvs[i],
			Color:      [4]float32{1, 1, 1, 1},
			Kind:       kind,
			EffectTime: r.effectTime,
		})
	}

	r.appendBatch(pipeline, group, uniform, start, 6)
}

// FillText draws an ASCII-oriented text run with the current fill style.
// x/y are interpreted using the current text align and baseline settings.
// If no font has been selected with SetFont, this is a no-op.
func (r *Context) FillText(s string, x, y float32) {
	r.appendTextRun(s, x, y, r.fillStyle)
}

// StrokeText draws an ASCII-oriented text stroke with the current stroke style and line width.
// x/y are interpreted using the current text align and baseline settings.
// If no font has been selected with SetFont, this is a no-op.
func (r *Context) StrokeText(s string, x, y float32) {
	if r == nil || len(s) == 0 || r.currentFont == nil || r.lineWidth <= 0 {
		return
	}

	var radius float32 = float32(math.Ceil(float64(r.lineWidth)))
	if radius < 1 {
		radius = 1
	}

	var steps int = int(radius)
	for oy := -steps; oy <= steps; oy++ {
		for ox := -steps; ox <= steps; ox++ {
			if ox == 0 && oy == 0 {
				continue
			}

			dx, dy := float32(ox), float32(oy)
			if dx*dx+dy*dy > radius*radius {
				continue
			}

			r.appendTextRun(s, x+dx, y+dy, r.strokeStyle)
		}
	}
}

func (r *Context) appendTextRun(s string, x, y float32, color Color) {
	if r == nil || len(s) == 0 || r.currentFont == nil || r.currentFont.atlas == nil || r.currentFont.group == nil {
		return
	}

	var (
		face  *fontFace  = r.currentFont
		atlas *fontAtlas = face.atlas
		lines []string   = strings.Split(s, "\n")
	)

	if len(lines) == 0 {
		return
	}

	var baselineY float32 = r.resolveTextBaselineY(y, len(lines), atlas)

	for lineIndex, line := range lines {
		var (
			lineWidth float32 = r.measureTextLine(line, atlas)
			penX      float32 = r.resolveTextAlignX(x, lineWidth)
			penY      float32 = baselineY + float32(lineIndex)*atlas.LineHeight
		)

		r.appendTextLine(face, atlas, line, penX, penY, color)
	}
}

func (r *Context) appendTextLine(face *fontFace, atlas *fontAtlas, s string, penX, penY float32, color Color) {
	for _, ch := range s {
		var (
			g  glyph
			ok bool
		)

		if g, ok = atlas.Glyphs[ch]; !ok {
			if g, ok = atlas.Glyphs['?']; !ok {
				continue
			}
		}

		if ch != ' ' {
			var (
				x0, y0 float32 = penX + g.BearingX, penY + g.BearingY
				x1, y1 float32 = x0 + g.W, y0 + g.H
				p0     Point   = r.transformPoint(x0, y0)
				p1     Point   = r.transformPoint(x1, y0)
				p2     Point   = r.transformPoint(x1, y1)
				p3     Point   = r.transformPoint(x0, y1)
			)

			r.appendTextQuad(face, p0, p1, p2, p3, g, color)
		}

		penX += g.Advance
	}
}

func (r *Context) measureTextLine(s string, atlas *fontAtlas) (width float32) {
	if atlas == nil || len(s) == 0 {
		return
	}

	for _, ch := range s {
		var (
			g  glyph
			ok bool
		)

		if g, ok = atlas.Glyphs[ch]; !ok {
			if g, ok = atlas.Glyphs['?']; !ok {
				continue
			}
		}

		width += g.Advance
	}

	return
}

func (r *Context) resolveTextAlignX(x, lineWidth float32) float32 {
	switch r.textAlign {
	case TextAlignCenter:
		return x - lineWidth*0.5
	case TextAlignRight:
		return x - lineWidth
	default:
		return x
	}
}

func (r *Context) resolveTextBaselineY(y float32, lineCount int, atlas *fontAtlas) float32 {
	if atlas == nil {
		return y
	}

	if lineCount < 1 {
		lineCount = 1
	}

	var lineSpan float32 = atlas.LineHeight * float32(lineCount-1)

	switch r.textBaseline {
	case TextBaselineTop:
		return y + atlas.Ascent
	case TextBaselineMiddle:
		return y - lineSpan*0.5 + (atlas.Ascent-atlas.Descent)*0.5
	case TextBaselineBottom:
		return y - lineSpan - atlas.Descent
	default:
		return y
	}
}

// Flush submits the accumulated draw calls to the GPU and presents them on the specified target texture view.
func (r *Context) Flush(target *wgpu.TextureView, clearColor *gputypes.Color) (err error) {
	if target == nil {
		return
	}

	r.pollSubmissions()

	if len(r.vertices) == 0 && clearColor == nil {
		return
	}

	if len(r.vertices) > 0 {
		if err = r.ensureVertexCapacity(len(r.vertices)); err != nil {
			return
		}

		var (
			size  int    = int(vertexStride) * len(r.vertices)
			bytes []byte = unsafe.Slice((*byte)(unsafe.Pointer(&r.vertices[0])), size)
		)

		if err = r.device.Queue().WriteBuffer(r.vertexBuffer, 0, bytes); err != nil {
			err = fmt.Errorf("upload UI vertex buffer: %w", err)
			return
		}
	}

	var (
		loadOp  gputypes.LoadOp = gputypes.LoadOpLoad
		clear   gputypes.Color  = gputypes.ColorTransparent
		encoder *wgpu.CommandEncoder
	)

	if clearColor != nil {
		loadOp = gputypes.LoadOpClear
		clear = *clearColor
	}

	if encoder, err = r.device.CreateCommandEncoder(&wgpu.CommandEncoderDescriptor{
		Label: "Conquest UI Encoder",
	}); err != nil {
		err = fmt.Errorf("create UI command encoder: %w", err)
		return
	}

	var pass *wgpu.RenderPassEncoder
	if pass, err = encoder.BeginRenderPass(&wgpu.RenderPassDescriptor{
		ColorAttachments: []wgpu.RenderPassColorAttachment{
			{
				View:       target,
				LoadOp:     loadOp,
				StoreOp:    gputypes.StoreOpStore,
				ClearValue: clear,
			},
		},
	}); err != nil {
		err = fmt.Errorf("begin UI render pass: %w", err)
		return
	}

	if len(r.vertices) > 0 {
		pass.SetVertexBuffer(0, r.vertexBuffer, 0)

		var (
			currentPipeline *wgpu.RenderPipeline
			currentGroup    *wgpu.BindGroup
			currentUniform  *wgpu.BindGroup
		)

		for _, batch := range r.batches {
			if batch.group == nil || batch.count == 0 {
				continue
			}

			pipeline := batch.pipeline
			if pipeline == nil {
				pipeline = r.pipeline
			}

			if pipeline != currentPipeline {
				pass.SetPipeline(pipeline)
				currentPipeline = pipeline
				currentGroup = nil
				currentUniform = nil
			}

			if batch.group != currentGroup {
				pass.SetBindGroup(0, batch.group, nil)
				currentGroup = batch.group
			}

			if batch.uniform != nil && batch.uniform != currentUniform {
				pass.SetBindGroup(1, batch.uniform, nil)
				currentUniform = batch.uniform
			}

			pass.Draw(batch.count, 1, batch.start, 0)
		}
	}

	if err = pass.End(); err != nil {
		err = fmt.Errorf("end UI render pass: %w", err)
		return
	}

	var commands *wgpu.CommandBuffer
	if commands, err = encoder.Finish(); err != nil {
		err = fmt.Errorf("finish UI commands: %w", err)
		return
	}

	var index uint64
	if index, err = r.device.Queue().Submit(commands); err != nil {
		commands.Release()
		err = fmt.Errorf("submit UI commands: %w", err)
		return
	}

	r.pending = append(r.pending, submission{index: index, cmd: commands})
	r.pollSubmissions()
	return
}

// ensureVertexCapacity checks if the current vertex buffer can accommodate the specified number of vertices,
// and if not, it creates a new buffer with increased capacity.
func (r *Context) ensureVertexCapacity(vertexCount int) (err error) {
	if vertexCount <= r.vertexCapacity && r.vertexBuffer != nil {
		return
	}

	var capacity int = 1024
	for capacity < vertexCount {
		capacity *= 2
	}

	var buffer *wgpu.Buffer
	if buffer, err = r.device.CreateBuffer(&wgpu.BufferDescriptor{
		Label: "Conquest UI Vertex Buffer",
		Size:  uint64(capacity) * vertexStride,
		Usage: gputypes.BufferUsageVertex | gputypes.BufferUsageCopyDst,
	}); err != nil {
		err = fmt.Errorf("create UI vertex buffer: %w", err)
		return
	}

	if r.vertexBuffer != nil {
		r.vertexBuffer.Release()
	}

	r.vertexBuffer = buffer
	r.vertexCapacity = capacity
	return
}

// pollSubmissions checks the status of pending GPU submissions and releases any command buffers that have completed.
func (r *Context) pollSubmissions() {
	if len(r.pending) == 0 || r.device == nil {
		return
	}

	var (
		completed uint64       = r.device.Queue().Poll()
		kept      []submission = r.pending[:0]
	)

	for _, sub := range r.pending {
		if sub.index <= completed {
			if sub.cmd != nil {
				sub.cmd.Release()
			}

			continue
		}

		kept = append(kept, sub)
	}

	r.pending = kept
}

// appendFilledPolygon appends a simple fan triangulation. This is fast and fine for convex HUD shapes.
func (r *Context) appendFilledPolygon(points []Point, color Color) {
	if len(points) < 3 {
		return
	}

	var origin Point = points[0]
	for i := 1; i < len(points)-1; i++ {
		r.appendSolidTriangle(origin, points[i], points[i+1], color)
	}
}

// appendStrokePath appends a simple stroked polyline/polygon.
func (r *Context) appendStrokePath(points []Point, closed bool, lineWidth float32, color Color) {
	if len(points) < 2 {
		return
	}

	for i := 0; i < len(points)-1; i++ {
		r.appendStrokeSegment(points[i], points[i+1], lineWidth, color)
	}

	if closed {
		r.appendStrokeSegment(points[len(points)-1], points[0], lineWidth, color)
	}
}

// appendStrokeSegment appends one stroked line segment as an SDF capsule so the fragment shader
// can anti-alias the contour instead of relying on hard triangle edges.
func (r *Context) appendStrokeSegment(a, b Point, lineWidth float32, color Color) {
	if lineWidth <= 0 {
		lineWidth = 1
	}

	var (
		dx, dy float32 = b.X - a.X, b.Y - a.Y
		lenSq  float32 = dx*dx + dy*dy
	)

	if lenSq <= 0 {
		return
	}

	var (
		length     float32    = float32(math.Sqrt(float64(lenSq)))
		halfWidth  float32    = lineWidth * 0.5
		radius     float32    = halfWidth
		halfLength float32    = length * 0.5
		tx, ty     float32    = dx / length, dy / length
		nx, ny     float32    = -ty, tx
		center     Point      = Point{X: (a.X + b.X) * 0.5, Y: (a.Y + b.Y) * 0.5}
		halfSize   [2]float32 = [2]float32{halfLength + radius, radius}
		padding    float32    = 2
		extentX    float32    = halfSize[0] + padding
		extentY    float32    = halfSize[1] + padding
		locals                = [6][2]float32{
			{-extentX, -extentY},
			{extentX, -extentY},
			{extentX, extentY},
			{-extentX, -extentY},
			{extentX, extentY},
			{-extentX, extentY},
		}
	)

	start := uint32(len(r.vertices))
	for _, local := range locals {
		var px float32 = center.X + tx*local[0] + nx*local[1]
		var py float32 = center.Y + ty*local[0] + ny*local[1]
		r.appendSDFVertex(px, py, local[0], local[1], halfSize[0], halfSize[1], radius, 0, color)
	}

	r.appendBatch(nil, r.activeTextureGroup(), nil, start, 6)
}

// appendSolidTriangle appends a triangle that uses the normal solid/SDF branch with radius zero.
func (r *Context) appendSolidTriangle(a, b, c Point, color Color) {
	start := uint32(len(r.vertices))
	r.appendSolidVertex(a.X, a.Y, color)
	r.appendSolidVertex(b.X, b.Y, color)
	r.appendSolidVertex(c.X, c.Y, color)
	r.appendBatch(nil, r.activeTextureGroup(), nil, start, 3)
}

// appendSolidVertex appends one vertex for simple solid geometry.
func (r *Context) appendSolidVertex(x, y float32, color Color) {
	r.appendSDFVertex(x, y, 0, 0, 1, 1, 0, 0, color)
}

// appendSDFVertex appends one vertex for distance-field evaluated geometry.
func (r *Context) appendSDFVertex(x, y, localX, localY, halfWidth, halfHeight, radius, strokeWidth float32, color Color) {
	var clipX, clipY float32 = r.pixelToClip(x, y)
	r.vertices = append(r.vertices, vertex{
		Position:    [2]float32{clipX, clipY},
		Local:       [2]float32{localX, localY},
		HalfSize:    [2]float32{halfWidth, halfHeight},
		Color:       [4]float32{color.R, color.G, color.B, color.A},
		Radius:      radius,
		Kind:        ctxKindRoundedRect,
		StrokeWidth: strokeWidth,
	})
}

// appendTextQuad adds a possibly transformed quad for a single glyph to the vertex buffer, using the glyph's atlas UVs and the specified color.
func (r *Context) appendTextQuad(face *fontFace, p0, p1, p2, p3 Point, g glyph, color Color) {
	start := uint32(len(r.vertices))
	var positions [6]Point = [6]Point{
		p0,
		p1,
		p2,
		p0,
		p2,
		p3,
	}

	var uvs [6][2]float32 = [6][2]float32{
		{g.U0, g.V0},
		{g.U1, g.V0},
		{g.U1, g.V1},
		{g.U0, g.V0},
		{g.U1, g.V1},
		{g.U0, g.V1},
	}

	for i := range 6 {
		var clipX, clipY float32 = r.pixelToClip(positions[i].X, positions[i].Y)
		r.vertices = append(r.vertices, vertex{
			Position: [2]float32{clipX, clipY},
			UV:       uvs[i],
			Color:    [4]float32{color.R, color.G, color.B, color.A},
			Kind:     ctxKindText,
		})
	}

	r.appendBatch(nil, face.group, nil, start, 6)
}

func (r *Context) activeTextureGroup() *wgpu.BindGroup {
	if r.currentFont != nil && r.currentFont.group != nil {
		return r.currentFont.group
	}

	for _, face := range r.fontFaces {
		if face != nil && face.group != nil {
			return face.group
		}
	}

	return nil
}

func (r *Context) imageGroup(img *gimage.Image) (*wgpu.BindGroup, error) {
	if group, ok := r.imageGroups[img]; ok {
		return group, nil
	}

	group, err := r.device.CreateBindGroup(&wgpu.BindGroupDescriptor{
		Label:  "gctx2d Image Bind Group",
		Layout: r.textureLayout,
		Entries: []wgpu.BindGroupEntry{
			{Binding: 0, Sampler: img.Sampler},
			{Binding: 1, TextureView: img.View},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("create image bind group: %w", err)
	}

	r.imageGroups[img] = group
	return group, nil
}

func (r *Context) appendBatch(pipeline *wgpu.RenderPipeline, group, uniform *wgpu.BindGroup, start, count uint32) {
	if group == nil || count == 0 {
		return
	}

	if len(r.batches) > 0 {
		last := &r.batches[len(r.batches)-1]
		if last.pipeline == pipeline && last.group == group && last.uniform == uniform && last.start+last.count == start {
			last.count += count
			return
		}
	}

	r.batches = append(r.batches, batch{pipeline: pipeline, group: group, uniform: uniform, start: start, count: count})
}

// flattenPath converts the current canvas-style path into closed/open subpaths.
func (r *Context) flattenPath() (subpaths []pathSubpath) {
	var current []Point

	finish := func(closed bool) {
		if len(current) == 0 {
			return
		}

		points := make([]Point, len(current))
		copy(points, current)

		subpaths = append(subpaths, pathSubpath{
			points: points,
			closed: closed,
		})

		current = current[:0]
	}

	for _, cmd := range r.path {
		switch cmd.kind {
		case pathCommandMoveTo:
			finish(false)
			current = append(current, cmd.p)
		case pathCommandLineTo:
			if len(current) == 0 {
				current = append(current, cmd.p)
				continue
			}

			current = append(current, cmd.p)
		case pathCommandClose:
			finish(true)
		}
	}

	finish(false)
	return
}

// lastPathCommandKind returns the most recent path command kind.
func (r *Context) lastPathCommandKind() pathCommandKind {
	if len(r.path) == 0 {
		return pathCommandClose
	}

	return r.path[len(r.path)-1].kind
}

// currentPathPoint returns the latest point in the current subpath, if one exists.
func (r *Context) currentPathPoint() (Point, bool) {
	for i := len(r.path) - 1; i >= 0; i-- {
		cmd := r.path[i]
		switch cmd.kind {
		case pathCommandMoveTo, pathCommandLineTo:
			return cmd.p, true
		case pathCommandClose:
			return Point{}, false
		}
	}

	return Point{}, false
}

// arcPointsToPath appends a point approximation to the current path.
func (r *Context) arcPointsToPath(points []Point, connect bool) {
	if len(points) == 0 {
		return
	}

	if !connect || len(r.path) == 0 || r.lastPathCommandKind() == pathCommandClose {
		r.MoveTo(points[0].X, points[0].Y)
	} else {
		r.LineTo(points[0].X, points[0].Y)
	}

	for i := 1; i < len(points); i++ {
		r.LineTo(points[i].X, points[i].Y)
	}
}

// curvePointsToPath appends a flattened Bezier approximation, skipping the already-present start point.
func (r *Context) curvePointsToPath(points []Point) {
	if len(points) < 2 {
		return
	}

	for i := 1; i < len(points); i++ {
		r.path = append(r.path, pathCommand{
			kind: pathCommandLineTo,
			p:    points[i],
		})
	}
}

// arcPoints returns a polyline approximation for a circular arc.
func (r *Context) arcPoints(cx, cy, radius, startAngle, endAngle float32, counterClockwise bool) []Point {
	return r.ellipsePoints(cx, cy, radius, radius, 0, startAngle, endAngle, counterClockwise)
}

// ellipsePoints returns a polyline approximation for an elliptical arc.
func (r *Context) ellipsePoints(cx, cy, radiusX, radiusY, rotation, startAngle, endAngle float32, counterClockwise bool) (points []Point) {
	var (
		tau      float32 = float32(math.Pi) * 2
		rawSweep float32 = endAngle - startAngle
		sweep    float32 = rawSweep
	)

	if rawSweep == 0 {
		return
	}

	if float32(math.Abs(float64(rawSweep))) >= tau {
		if counterClockwise {
			sweep = -tau
		} else {
			sweep = tau
		}
	} else if counterClockwise {
		if sweep > 0 {
			sweep -= tau
		}
	} else if sweep < 0 {
		sweep += tau
	}

	var (
		absSweep float32 = float32(math.Abs(float64(sweep)))
		steps    int     = int(absSweep/(float32(math.Pi)/32)) + 1
	)

	if steps < 12 {
		steps = 12
	}

	if steps > 192 {
		steps = 192
	}

	var (
		cosRot float32 = float32(math.Cos(float64(rotation)))
		sinRot float32 = float32(math.Sin(float64(rotation)))
	)

	points = make([]Point, 0, steps+1)
	for i := 0; i <= steps; i++ {
		var (
			t     float32 = float32(i) / float32(steps)
			angle float32 = startAngle + sweep*t
			x     float32 = float32(math.Cos(float64(angle))) * radiusX
			y     float32 = float32(math.Sin(float64(angle))) * radiusY
		)

		points = append(points, Point{
			X: cx + x*cosRot - y*sinRot,
			Y: cy + x*sinRot + y*cosRot,
		})
	}

	return
}

// quadraticCurvePoints returns a polyline approximation for a quadratic Bezier curve.
func (r *Context) quadraticCurvePoints(p0, p1, p2 Point) (points []Point) {
	steps := curveSubdivisionSteps([]Point{p0, p1, p2})
	points = make([]Point, 0, steps+1)

	for i := 0; i <= steps; i++ {
		t := float32(i) / float32(steps)
		mt := 1 - t
		points = append(points, Point{
			X: mt*mt*p0.X + 2*mt*t*p1.X + t*t*p2.X,
			Y: mt*mt*p0.Y + 2*mt*t*p1.Y + t*t*p2.Y,
		})
	}

	return
}

// cubicCurvePoints returns a polyline approximation for a cubic Bezier curve.
func (r *Context) cubicCurvePoints(p0, p1, p2, p3 Point) (points []Point) {
	steps := curveSubdivisionSteps([]Point{p0, p1, p2, p3})
	points = make([]Point, 0, steps+1)

	for i := 0; i <= steps; i++ {
		t := float32(i) / float32(steps)
		mt := 1 - t
		points = append(points, Point{
			X: mt*mt*mt*p0.X + 3*mt*mt*t*p1.X + 3*mt*t*t*p2.X + t*t*t*p3.X,
			Y: mt*mt*mt*p0.Y + 3*mt*mt*t*p1.Y + 3*mt*t*t*p2.Y + t*t*t*p3.Y,
		})
	}

	return
}

func curveSubdivisionSteps(points []Point) int {
	var length float32
	for i := 1; i < len(points); i++ {
		dx := points[i].X - points[i-1].X
		dy := points[i].Y - points[i-1].Y
		length += float32(math.Sqrt(float64(dx*dx + dy*dy)))
	}

	steps := int(length/8) + 1
	if steps < 12 {
		return 12
	}

	if steps > 192 {
		return 192
	}

	return steps
}

// transformPoint applies the current canvas-style transform to a point.
func (r *Context) transformPoint(x, y float32) Point {
	return r.transform.Apply(x, y)
}

// uiIdentityMatrix returns the identity affine transform.
func uiIdentityMatrix() Matrix {
	return Matrix{A: 1, D: 1}
}

// IdentityMatrix returns an identity affine transform.
func IdentityMatrix() Matrix {
	return uiIdentityMatrix()
}

// NewTransformMatrix returns a translation/rotation/scale affine transform.
func NewTransformMatrix(x, y, scaleX, scaleY, rotation float32) Matrix {
	var (
		cosine float32 = float32(math.Cos(float64(rotation)))
		sine   float32 = float32(math.Sin(float64(rotation)))
	)

	return Matrix{
		A: cosine * scaleX,
		B: sine * scaleX,
		C: -sine * scaleY,
		D: cosine * scaleY,
		E: x,
		F: y,
	}
}

// NewUniformTransformMatrix returns a translation/rotation/uniform-scale affine transform.
func NewUniformTransformMatrix(x, y, scale, rotation float32) Matrix {
	return NewTransformMatrix(x, y, scale, scale, rotation)
}

// Mul composes two affine transforms using the HTML canvas convention current = current * next.
func (m Matrix) Mul(next Matrix) Matrix {
	return Matrix{
		A: m.A*next.A + m.C*next.B,
		B: m.B*next.A + m.D*next.B,
		C: m.A*next.C + m.C*next.D,
		D: m.B*next.C + m.D*next.D,
		E: m.A*next.E + m.C*next.F + m.E,
		F: m.B*next.E + m.D*next.F + m.F,
	}
}

// Apply transforms a point using the affine matrix.
func (m Matrix) Apply(x, y float32) Point {
	return Point{
		X: m.A*x + m.C*y + m.E,
		Y: m.B*x + m.D*y + m.F,
	}
}

// pixelToClip converts pixel coordinates to normalized device coordinates in the range [-1, 1], with (0, 0) at the top-left corner.
func (r *Context) pixelToClip(x, y float32) (clipX, clipY float32) {
	clipX, clipY = (x/float32(r.width))*2-1, 1-(y/float32(r.height))*2
	return
}
