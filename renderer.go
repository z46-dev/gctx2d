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
	LineCapButt LineCap = iota
	LineCapRound
	LineCapSquare
)

const (
	LineJoinMiter LineJoin = iota
	LineJoinRound
	LineJoinBevel
)

// Clip modes control how a new clipping path is combined with the active clip.
const (
	ClipModeIntersect ClipMode = iota
	ClipModeDifference
	ClipModeReplace
)

const (
	clipOperationNone clipOperation = iota
	clipOperationIncrement
	clipOperationDecrement
)

const stencilFormat = gputypes.TextureFormatStencil8

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
		lineCap:      LineCapButt,
		lineJoin:     LineJoinMiter,
		miterLimit:   10,
		globalAlpha:  1,
		shadowColor:  ColorTransparent,
		transform:    uiIdentityMatrix(),
		imageEffect:  ImageEffectNone,
		stateStack:   make([]drawingState, 0, 32),
		path:         make([]pathCommand, 0, 128),
		clips:        make([]*clipEntry, 0, 16),
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

	if r.clipIncrementPipeline != nil {
		r.clipIncrementPipeline.Release()
		r.clipIncrementPipeline = nil
	}

	if r.clipDecrementPipeline != nil {
		r.clipDecrementPipeline.Release()
		r.clipDecrementPipeline = nil
	}

	r.releaseStencilAttachment()

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

	drawStencil := stencilState(wgpu.StencilOperationKeep)
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
						{ShaderLocation: 4, Offset: 32, Format: gputypes.VertexFormatFloat32x2},
						{ShaderLocation: 5, Offset: 40, Format: gputypes.VertexFormatFloat32x2},
						{ShaderLocation: 6, Offset: 48, Format: gputypes.VertexFormatFloat32x4},
						{ShaderLocation: 7, Offset: 64, Format: gputypes.VertexFormatFloat32x4},
						{ShaderLocation: 8, Offset: 80, Format: gputypes.VertexFormatFloat32},
						{ShaderLocation: 9, Offset: 84, Format: gputypes.VertexFormatFloat32},
						{ShaderLocation: 10, Offset: 88, Format: gputypes.VertexFormatFloat32},
						{ShaderLocation: 11, Offset: 92, Format: gputypes.VertexFormatFloat32},
						{ShaderLocation: 12, Offset: 96, Format: gputypes.VertexFormatFloat32},
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
		DepthStencil: &drawStencil,
	}); err != nil {
		err = fmt.Errorf("create UI render pipeline: %w", err)
		return
	}

	var clipShader *wgpu.ShaderModule
	if clipShader, err = r.device.CreateShaderModule(&wgpu.ShaderModuleDescriptor{Label: "gctx2d Clip Shader", WGSL: clipWGSL}); err != nil {
		err = fmt.Errorf("create clip shader module: %w", err)
		return
	}
	defer clipShader.Release()

	if r.clipIncrementPipeline, err = r.createClipPipeline(clipShader, "gctx2d Clip Increment Pipeline", wgpu.StencilOperationIncrementClamp); err != nil {
		return
	}
	if r.clipDecrementPipeline, err = r.createClipPipeline(clipShader, "gctx2d Clip Decrement Pipeline", wgpu.StencilOperationDecrementClamp); err != nil {
		return
	}

	return
}

func stencilState(passOperation wgpu.StencilOperation) wgpu.DepthStencilState {
	face := wgpu.StencilFaceState{
		Compare:     gputypes.CompareFunctionEqual,
		FailOp:      wgpu.StencilOperationKeep,
		DepthFailOp: wgpu.StencilOperationKeep,
		PassOp:      passOperation,
	}
	return wgpu.DepthStencilState{
		Format:           stencilFormat,
		DepthCompare:     gputypes.CompareFunctionAlways,
		StencilFront:     face,
		StencilBack:      face,
		StencilReadMask:  0xff,
		StencilWriteMask: 0xff,
	}
}

func (r *Context) createClipPipeline(shader *wgpu.ShaderModule, label string, operation wgpu.StencilOperation) (*wgpu.RenderPipeline, error) {
	state := stencilState(operation)
	pipeline, err := r.device.CreateRenderPipeline(&wgpu.RenderPipelineDescriptor{
		Label:  label,
		Layout: r.pipelineLayout,
		Vertex: wgpu.VertexState{
			Module: shader, EntryPoint: "vs_main",
			Buffers: []gputypes.VertexBufferLayout{{
				ArrayStride: vertexStride,
				StepMode:    gputypes.VertexStepModeVertex,
				Attributes:  []gputypes.VertexAttribute{{ShaderLocation: 0, Offset: 0, Format: gputypes.VertexFormatFloat32x2}},
			}},
		},
		Primitive:    gputypes.PrimitiveState{Topology: gputypes.PrimitiveTopologyTriangleList, CullMode: gputypes.CullModeNone},
		DepthStencil: &state,
	})
	if err != nil {
		return nil, fmt.Errorf("create clip pipeline: %w", err)
	}
	return pipeline, nil
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
	r.clips = r.clips[:0]
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

// SetLineCap sets the line cap style used by Stroke and StrokeText-like geometry.
func (r *Context) SetLineCap(cap LineCap) {
	switch cap {
	case LineCapButt, LineCapRound, LineCapSquare:
		r.lineCap = cap
	default:
		r.lineCap = LineCapButt
	}
}

// SetLineJoin sets the line join style used by Stroke.
func (r *Context) SetLineJoin(join LineJoin) {
	switch join {
	case LineJoinMiter, LineJoinRound, LineJoinBevel:
		r.lineJoin = join
	default:
		r.lineJoin = LineJoinMiter
	}
}

// SetMiterLimit sets the maximum allowed miter length ratio before falling back to a bevel join.
func (r *Context) SetMiterLimit(limit float32) {
	if limit < 1 {
		limit = 1
	}

	r.miterLimit = limit
}

// SetGlobalAlpha sets the alpha multiplier applied to subsequent draw calls.
func (r *Context) SetGlobalAlpha(alpha float32) {
	if alpha < 0 {
		alpha = 0
	} else if alpha > 1 {
		alpha = 1
	}

	r.globalAlpha = alpha
}

// SetShadowColor sets the color used by subsequent shadow blur passes.
func (r *Context) SetShadowColor(color Color) {
	r.shadowColor = color
}

// SetShadowBlur sets the blur radius used by subsequent shadow passes.
func (r *Context) SetShadowBlur(blur float32) {
	if blur < 0 {
		blur = 0
	}

	r.shadowBlur = blur
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

// SetTextKerning controls whether font kerning pairs affect text placement and measurement.
func (r *Context) SetTextKerning(enabled bool) {
	r.disableTextKerning = !enabled
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
		lineCap:            r.lineCap,
		lineJoin:           r.lineJoin,
		miterLimit:         r.miterLimit,
		globalAlpha:        r.globalAlpha,
		shadowColor:        r.shadowColor,
		shadowBlur:         r.shadowBlur,
		textAlign:          r.textAlign,
		textBaseline:       r.textBaseline,
		disableTextKerning: r.disableTextKerning,
		transform:          r.transform,
		imageEffect:        r.imageEffect,
		effectTime:         r.effectTime,
		currentImageShader: r.currentImageShader,
		currentFont:        r.currentFont,
		clips:              append([]*clipEntry(nil), r.clips...),
	}
}

func (r *Context) restoreDrawingState(state drawingState) {
	r.fillStyle = state.fillStyle
	r.strokeStyle = state.strokeStyle
	r.lineWidth = state.lineWidth
	r.lineCap = state.lineCap
	r.lineJoin = state.lineJoin
	r.miterLimit = state.miterLimit
	r.globalAlpha = state.globalAlpha
	r.shadowColor = state.shadowColor
	r.shadowBlur = state.shadowBlur
	r.textAlign = state.textAlign
	r.textBaseline = state.textBaseline
	r.disableTextKerning = state.disableTextKerning
	r.transform = state.transform
	r.imageEffect = state.imageEffect
	r.effectTime = state.effectTime
	r.currentImageShader = state.currentImageShader
	r.currentFont = state.currentFont
	r.transitionClips(state.clips)
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

// FillRoundedRect draws an antialiased rounded rectangle through the SDF fast path.
func (r *Context) FillRoundedRect(x, y, width, height, radius float32) {
	r.appendTransformedSDFRect(x, y, width, height, radius, 0, r.fillStyle)
}

// StrokeRoundedRect draws an antialiased rounded-rectangle outline through the SDF fast path.
func (r *Context) StrokeRoundedRect(x, y, width, height, radius float32) {
	if r.lineWidth > 0 {
		r.appendTransformedSDFRect(x, y, width, height, radius, r.lineWidth, r.strokeStyle)
	}
}

// FillCircle draws an antialiased circle through the SDF fast path.
func (r *Context) FillCircle(cx, cy, radius float32) {
	if radius > 0 {
		r.appendTransformedSDFRect(cx-radius, cy-radius, radius*2, radius*2, radius, 0, r.fillStyle)
	}
}

// StrokeCircle draws an antialiased circle outline through the SDF fast path.
func (r *Context) StrokeCircle(cx, cy, radius float32) {
	if radius > 0 && r.lineWidth > 0 {
		r.appendTransformedSDFRect(cx-radius, cy-radius, radius*2, radius*2, radius, r.lineWidth, r.strokeStyle)
	}
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
	subpaths := r.flattenPath()
	if r.hasShadow() {
		r.appendPathShadow(subpaths)
	}

	for _, subpath := range subpaths {
		r.appendFilledPolygon(subpath.points, r.fillStyle)
	}
}

// Clip combines the current path with the active clipping region. With no mode it intersects,
// matching the HTML canvas API. Difference punches the path out; Replace discards the active
// region before applying the path. Clip paths use the same convex-subpath tessellation as Fill.
func (r *Context) Clip(modes ...ClipMode) {
	mode := ClipModeIntersect
	if len(modes) > 0 {
		mode = modes[0]
	}
	if mode != ClipModeIntersect && mode != ClipModeDifference && mode != ClipModeReplace {
		mode = ClipModeIntersect
	}

	entry := &clipEntry{mode: mode}
	for _, subpath := range r.flattenPath() {
		if len(subpath.points) < 3 {
			continue
		}
		start := uint32(len(r.vertices))
		for i := 1; i+1 < len(subpath.points); i++ {
			r.appendClipVertex(subpath.points[0])
			r.appendClipVertex(subpath.points[i])
			r.appendClipVertex(subpath.points[i+1])
		}
		if count := uint32(len(r.vertices)) - start; count > 0 {
			entry.ranges = append(entry.ranges, vertexRange{start: start, count: count})
		}
	}

	if mode == ClipModeReplace {
		r.transitionClips(nil)
		entry.mode = ClipModeIntersect
	}
	if len(r.clips) >= 255 {
		return
	}
	r.pushClip(entry)
	r.clips = append(r.clips, entry)
}

func (r *Context) appendClipVertex(point Point) {
	x, y := r.pixelToClip(point.X, point.Y)
	r.vertices = append(r.vertices, vertex{Position: [2]float32{x, y}})
}

func (r *Context) transitionClips(target []*clipEntry) {
	common := 0
	for common < len(r.clips) && common < len(target) && r.clips[common] == target[common] {
		common++
	}
	for i := len(r.clips) - 1; i >= common; i-- {
		r.popClip(r.clips[i], uint32(i+1))
	}
	r.clips = append(r.clips[:0], target[:common]...)
	for i := common; i < len(target) && i < 255; i++ {
		r.pushClip(target[i])
		r.clips = append(r.clips, target[i])
	}
}

func (r *Context) pushClip(entry *clipEntry) {
	depth := uint32(len(r.clips))
	if entry.mode == ClipModeDifference {
		r.appendFullscreenClipBatch(clipOperationIncrement, depth)
		for _, span := range entry.ranges {
			r.appendClipBatch(clipOperationDecrement, depth+1, span)
		}
		return
	}
	for _, span := range entry.ranges {
		r.appendClipBatch(clipOperationIncrement, depth, span)
	}
}

func (r *Context) popClip(entry *clipEntry, depth uint32) {
	if entry.mode == ClipModeDifference {
		r.appendFullscreenClipBatch(clipOperationDecrement, depth)
		return
	}
	for _, span := range entry.ranges {
		r.appendClipBatch(clipOperationDecrement, depth, span)
	}
}

func (r *Context) appendFullscreenClipBatch(operation clipOperation, reference uint32) {
	start := uint32(len(r.vertices))
	for _, p := range [...]Point{{0, 0}, {float32(r.width), 0}, {float32(r.width), float32(r.height)}, {0, 0}, {float32(r.width), float32(r.height)}, {0, float32(r.height)}} {
		r.appendClipVertex(p)
	}
	r.appendClipBatch(operation, reference, vertexRange{start: start, count: 6})
}

func (r *Context) appendClipBatch(operation clipOperation, reference uint32, span vertexRange) {
	r.batches = append(r.batches, batch{start: span.start, count: span.count, stencilReference: reference, clipOperation: operation})
}

// Stroke strokes all current canvas-style subpaths with the current stroke style and line width.
func (r *Context) Stroke() {
	subpaths := r.flattenPath()
	if r.hasShadow() {
		r.appendStrokeShadow(subpaths)
	}

	for _, subpath := range subpaths {
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

	shadowBlur := r.effectiveShadowBlurForQuads()
	padding := quadShadowPadding(shadowBlur)
	paddedX, paddedY := x-padding, y-padding
	paddedWidth, paddedHeight := width+padding*2, height+padding*2

	var (
		p0 Point = transform.Apply(paddedX, paddedY)
		p1 Point = transform.Apply(paddedX+paddedWidth, paddedY)
		p2 Point = transform.Apply(paddedX+paddedWidth, paddedY+paddedHeight)
		p3 Point = transform.Apply(paddedX, paddedY+paddedHeight)

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
		start       uint32  = uint32(len(r.vertices))
		kind        float32 = ctxKindImage + float32(r.imageEffect)
		color       Color   = r.applyGlobalAlpha(ColorWhite)
		shadowColor Color   = r.effectiveShadowColorForQuads()
	)

	if r.currentImageShader != nil {
		shadowBlur = 0
		shadowColor = ColorTransparent
		kind = ctxKindImage
	}

	for i := range 6 {
		clipX, clipY := r.pixelToClip(positions[i].X, positions[i].Y)
		r.vertices = append(r.vertices, vertex{
			Position:    [2]float32{clipX, clipY},
			Local:       [2]float32{locals[i][0] * paddedWidth * 0.5, locals[i][1] * paddedHeight * 0.5},
			HalfSize:    [2]float32{width * 0.5, height * 0.5},
			UV:          [2]float32{u0, v0},
			UVMin:       [2]float32{u0, v0},
			UVMax:       [2]float32{u1, v1},
			Color:       [4]float32{color.R, color.G, color.B, color.A},
			ShadowColor: [4]float32{shadowColor.R, shadowColor.G, shadowColor.B, shadowColor.A},
			Radius:      0,
			Kind:        kind,
			StrokeWidth: 0,
			EffectTime:  r.effectTime,
			ShadowBlur:  shadowBlur,
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

// MeasureTextLine measures a single line with the active font and optionally writes exact caret positions.
// Pass a non-nil reusable slice to receive one position per rune boundary, beginning at zero.
func (r *Context) MeasureTextLine(s string, caretPositions []float32) (metrics TextLineMetrics, positions []float32) {
	if r == nil || r.currentFont == nil || r.currentFont.atlas == nil {
		return
	}

	var atlas *fontAtlas = r.currentFont.atlas
	metrics.Ascent = atlas.Ascent
	metrics.Descent = atlas.Descent
	metrics.LineHeight = atlas.LineHeight
	metrics.Width, positions = measureTextLineInto(s, atlas, caretPositions, !r.disableTextKerning)
	return
}

// StrokeText draws an ASCII-oriented text stroke with the current stroke style and line width.
// x/y are interpreted using the current text align and baseline settings.
// If no font has been selected with SetFont, this is a no-op.
func (r *Context) StrokeText(s string, x, y float32) {
	if r == nil || len(s) == 0 || r.currentFont == nil || r.lineWidth <= 0 {
		return
	}

	r.appendStrokeTextRun(s, x, y, r.strokeStyle)
}

func (r *Context) appendStrokeTextRun(s string, x, y float32, color Color) {
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

			r.appendTextRun(s, x+dx, y+dy, color)
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
	)
	if !strings.ContainsRune(s, '\n') {
		var baselineY float32 = r.resolveTextBaselineY(y, 1, atlas)
		r.appendTextLine(face, atlas, s, r.resolveTextAlignX(x, r.measureTextLine(s, atlas)), baselineY, color)
		return
	}

	var lines []string = strings.Split(s, "\n")
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
	var (
		previous    rune
		hasPrevious bool
	)

	for _, ch := range s {
		var (
			g         glyph
			glyphRune rune = ch
			ok        bool
		)

		if g, ok = atlas.Glyphs[ch]; !ok {
			glyphRune = '?'
			if g, ok = atlas.Glyphs[glyphRune]; !ok {
				continue
			}
		}

		if hasPrevious && !r.disableTextKerning {
			penX += atlas.Kerning[[2]rune{previous, glyphRune}]
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
		previous = glyphRune
		hasPrevious = true
	}
}

func (r *Context) measureTextLine(s string, atlas *fontAtlas) (width float32) {
	width, _ = measureTextLineInto(s, atlas, nil, !r.disableTextKerning)
	return
}

func measureTextLineInto(s string, atlas *fontAtlas, caretPositions []float32, kerning bool) (width float32, positions []float32) {
	var recordPositions bool = caretPositions != nil
	if recordPositions {
		positions = append(caretPositions[:0], 0)
	}
	if atlas == nil || len(s) == 0 {
		return
	}

	var (
		previous    rune
		hasPrevious bool
	)

	for _, ch := range s {
		var (
			g         glyph
			glyphRune rune = ch
			ok        bool
		)

		if g, ok = atlas.Glyphs[ch]; !ok {
			glyphRune = '?'
			if g, ok = atlas.Glyphs[glyphRune]; !ok {
				if recordPositions {
					positions = append(positions, width)
				}
				continue
			}
		}

		if hasPrevious && kerning {
			width += atlas.Kerning[[2]rune{previous, glyphRune}]
			if recordPositions {
				positions[len(positions)-1] = width
			}
		}

		width += g.Advance
		if recordPositions {
			positions = append(positions, width)
		}
		previous = glyphRune
		hasPrevious = true
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
		if err = r.ensureStencilAttachment(); err != nil {
			return
		}
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

	passDesc := &wgpu.RenderPassDescriptor{
		ColorAttachments: []wgpu.RenderPassColorAttachment{
			{
				View:       target,
				LoadOp:     loadOp,
				StoreOp:    gputypes.StoreOpStore,
				ClearValue: clear,
			},
		},
	}
	if len(r.vertices) > 0 {
		passDesc.DepthStencilAttachment = &wgpu.RenderPassDepthStencilAttachment{
			View:              r.stencilView,
			StencilLoadOp:     gputypes.LoadOpClear,
			StencilStoreOp:    gputypes.StoreOpDiscard,
			StencilClearValue: 0,
		}
	}

	var pass *wgpu.RenderPassEncoder
	if pass, err = encoder.BeginRenderPass(passDesc); err != nil {
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
			if batch.count == 0 || (batch.clipOperation == clipOperationNone && batch.group == nil) {
				continue
			}

			pipeline := batch.pipeline
			switch batch.clipOperation {
			case clipOperationIncrement:
				pipeline = r.clipIncrementPipeline
			case clipOperationDecrement:
				pipeline = r.clipDecrementPipeline
			case clipOperationNone:
				if pipeline == nil {
					pipeline = r.pipeline
				}
			}
			if pipeline == nil {
				continue
			}

			pass.SetStencilReference(batch.stencilReference)

			if pipeline != currentPipeline {
				pass.SetPipeline(pipeline)
				currentPipeline = pipeline
				currentGroup = nil
				currentUniform = nil
			}

			if batch.group != nil && batch.group != currentGroup {
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
		r.appendSolidTriangleNoShadow(origin, points[i], points[i+1], color)
	}
}

func (r *Context) appendFilledPolygonOffset(points []Point, offset Point, color Color) {
	if len(points) < 3 {
		return
	}

	origin := Point{X: points[0].X + offset.X, Y: points[0].Y + offset.Y}
	for i := 1; i < len(points)-1; i++ {
		a := Point{X: points[i].X + offset.X, Y: points[i].Y + offset.Y}
		b := Point{X: points[i+1].X + offset.X, Y: points[i+1].Y + offset.Y}
		r.appendSolidTriangleNoShadow(origin, a, b, color)
	}
}

// appendStrokePath appends a simple stroked polyline/polygon.
func (r *Context) appendStrokePath(points []Point, closed bool, lineWidth float32, color Color) {
	if len(points) < 2 {
		return
	}

	if lineWidth <= 0 {
		lineWidth = 1
	}

	halfWidth := lineWidth * 0.5
	segmentCount := len(points) - 1
	if closed {
		segmentCount = len(points)
	}

	segments := make([]strokeSegment, 0, segmentCount)
	for i := 0; i < segmentCount; i++ {
		a := points[i]
		b := points[(i+1)%len(points)]
		dx, dy := b.X-a.X, b.Y-a.Y
		lenSq := dx*dx + dy*dy
		if lenSq <= 0 {
			continue
		}

		length := float32(math.Sqrt(float64(lenSq)))
		tx, ty := dx/length, dy/length
		segments = append(segments, strokeSegment{
			a:      a,
			b:      b,
			tx:     tx,
			ty:     ty,
			nx:     -ty,
			ny:     tx,
			length: length,
		})
	}

	if len(segments) == 0 {
		return
	}

	for i, seg := range segments {
		startExtend := float32(0)
		endExtend := float32(0)
		if !closed && r.lineCap == LineCapSquare {
			if i == 0 {
				startExtend = halfWidth
			}
			if i == len(segments)-1 {
				endExtend = halfWidth
			}
		}

		r.appendStrokeBody(seg, lineWidth, color, startExtend, endExtend)
	}

	if closed {
		for i := 0; i < len(segments); i++ {
			prev := segments[(i-1+len(segments))%len(segments)]
			curr := segments[i]
			r.appendStrokeJoin(curr.a, prev, curr, halfWidth, color)
		}
		return
	}

	if r.lineCap == LineCapRound {
		r.appendStrokeCapCircle(points[0], halfWidth, color)
		r.appendStrokeCapCircle(points[len(points)-1], halfWidth, color)
	}

	for i := 1; i < len(segments); i++ {
		r.appendStrokeJoin(segments[i].a, segments[i-1], segments[i], halfWidth, color)
	}
}

func (r *Context) appendStrokePathOffset(points []Point, closed bool, lineWidth float32, color Color, offset Point) {
	if len(points) < 2 {
		return
	}

	shifted := make([]Point, len(points))
	for i, p := range points {
		shifted[i] = Point{X: p.X + offset.X, Y: p.Y + offset.Y}
	}

	r.appendStrokePath(shifted, closed, lineWidth, color)
}

func (r *Context) appendStrokeBody(seg strokeSegment, lineWidth float32, color Color, startExtend, endExtend float32) {
	halfWidth := lineWidth * 0.5
	halfLength := seg.length*0.5 + (startExtend+endExtend)*0.5
	centerShift := (endExtend - startExtend) * 0.5
	center := Point{
		X: (seg.a.X+seg.b.X)*0.5 + seg.tx*centerShift,
		Y: (seg.a.Y+seg.b.Y)*0.5 + seg.ty*centerShift,
	}

	r.appendOrientedSDFQuadNoShadow(center, seg.tx, seg.ty, seg.nx, seg.ny, halfLength, halfWidth, 0, 0, color)
}

func (r *Context) appendStrokeCapCircle(center Point, radius float32, color Color) {
	if radius <= 0 {
		return
	}

	r.appendAxisAlignedSDFQuadNoShadow(center, radius, radius, radius, 0, color)
}

func (r *Context) appendStrokeJoin(vertex Point, prev, next strokeSegment, halfWidth float32, color Color) {
	if halfWidth <= 0 {
		return
	}

	cross := prev.tx*next.ty - prev.ty*next.tx
	if math.Abs(float64(cross)) < 1e-4 {
		return
	}

	if r.lineJoin == LineJoinRound {
		r.appendStrokeCapCircle(vertex, halfWidth, color)
		return
	}

	outwardSign := float32(1)
	if cross < 0 {
		outwardSign = -1
	}

	outerPrev := Point{
		X: vertex.X + prev.nx*halfWidth*outwardSign,
		Y: vertex.Y + prev.ny*halfWidth*outwardSign,
	}
	outerNext := Point{
		X: vertex.X + next.nx*halfWidth*outwardSign,
		Y: vertex.Y + next.ny*halfWidth*outwardSign,
	}

	if r.lineJoin == LineJoinBevel {
		r.appendSolidTriangleNoShadow(vertex, outerPrev, outerNext, color)
		return
	}

	miterPoint, ok := lineIntersection(
		outerPrev, Point{X: outerPrev.X - prev.tx, Y: outerPrev.Y - prev.ty},
		outerNext, Point{X: outerNext.X + next.tx, Y: outerNext.Y + next.ty},
	)
	if !ok {
		r.appendSolidTriangleNoShadow(vertex, outerPrev, outerNext, color)
		return
	}

	miterDX, miterDY := miterPoint.X-vertex.X, miterPoint.Y-vertex.Y
	miterLength := float32(math.Sqrt(float64(miterDX*miterDX + miterDY*miterDY)))
	if miterLength > r.miterLimit*halfWidth {
		r.appendSolidTriangleNoShadow(vertex, outerPrev, outerNext, color)
		return
	}

	r.appendSolidTriangleNoShadow(vertex, outerPrev, miterPoint, color)
	r.appendSolidTriangleNoShadow(vertex, miterPoint, outerNext, color)
}

func lineIntersection(a0, a1, b0, b1 Point) (Point, bool) {
	adx, ady := a1.X-a0.X, a1.Y-a0.Y
	bdx, bdy := b1.X-b0.X, b1.Y-b0.Y
	denom := adx*bdy - ady*bdx
	if math.Abs(float64(denom)) < 1e-6 {
		return Point{}, false
	}

	dx, dy := b0.X-a0.X, b0.Y-a0.Y
	t := (dx*bdy - dy*bdx) / denom
	return Point{X: a0.X + adx*t, Y: a0.Y + ady*t}, true
}

// appendSolidTriangle appends a triangle that uses the normal solid/SDF branch with radius zero.
func (r *Context) appendSolidTriangle(a, b, c Point, color Color) {
	r.appendSolidTriangleNoShadow(a, b, c, color)
}

func (r *Context) appendSolidTriangleNoShadow(a, b, c Point, color Color) {
	start := uint32(len(r.vertices))
	r.appendSolidVertexNoShadow(a.X, a.Y, color)
	r.appendSolidVertexNoShadow(b.X, b.Y, color)
	r.appendSolidVertexNoShadow(c.X, c.Y, color)
	r.appendBatch(nil, r.activeTextureGroup(), nil, start, 3)
}

// appendSolidVertex appends one vertex for simple solid geometry.
func (r *Context) appendSolidVertex(x, y float32, color Color) {
	r.appendSDFVertex(x, y, 0, 0, 1, 1, 0, 0, color)
}

func (r *Context) appendSolidVertexNoShadow(x, y float32, color Color) {
	r.appendSDFVertexWithShadow(x, y, 0, 0, 1, 1, 0, 0, color, ColorTransparent, 0)
}

func (r *Context) appendAxisAlignedSDFQuadNoShadow(center Point, halfWidth, halfHeight, radius, strokeWidth float32, color Color) {
	r.appendOrientedSDFQuadNoShadow(center, 1, 0, 0, 1, halfWidth, halfHeight, radius, strokeWidth, color)
}

func (r *Context) appendTransformedSDFRect(x, y, width, height, radius, strokeWidth float32, color Color) {
	var (
		center                Point
		scaleX, scaleY, scale float32
	)
	if width == 0 || height == 0 {
		return
	}
	if width < 0 {
		x += width
		width = -width
	}
	if height < 0 {
		y += height
		height = -height
	}

	scaleX = float32(math.Hypot(float64(r.transform.A), float64(r.transform.B)))
	scaleY = float32(math.Hypot(float64(r.transform.C), float64(r.transform.D)))
	if scaleX == 0 || scaleY == 0 {
		return
	}
	scale = min(scaleX, scaleY)
	center = r.transform.Apply(x+width/2, y+height/2)
	r.appendOrientedSDFQuad(
		center,
		r.transform.A/scaleX,
		r.transform.B/scaleX,
		r.transform.C/scaleY,
		r.transform.D/scaleY,
		width*scaleX/2,
		height*scaleY/2,
		max(0, min(radius*scale, min(width*scaleX, height*scaleY)/2)),
		strokeWidth*scale,
		color,
	)
}

func (r *Context) appendOrientedSDFQuad(center Point, tx, ty, nx, ny, halfWidth, halfHeight, radius, strokeWidth float32, color Color) {
	var padding float32 = 2 + strokeWidth/2 + quadShadowPadding(r.shadowBlur)
	var (
		extentX float32 = halfWidth + padding
		extentY float32 = halfHeight + padding
		locals          = [6][2]float32{
			{-extentX, -extentY},
			{extentX, -extentY},
			{extentX, extentY},
			{-extentX, -extentY},
			{extentX, extentY},
			{-extentX, extentY},
		}
		start uint32 = uint32(len(r.vertices))
	)
	for _, local := range locals {
		r.appendSDFVertex(
			center.X+tx*local[0]+nx*local[1],
			center.Y+ty*local[0]+ny*local[1],
			local[0], local[1], halfWidth, halfHeight, radius, strokeWidth, color,
		)
	}

	r.appendBatch(nil, r.activeTextureGroup(), nil, start, 6)
}

func (r *Context) appendOrientedSDFQuadNoShadow(center Point, tx, ty, nx, ny, halfWidth, halfHeight, radius, strokeWidth float32, color Color) {
	padding := float32(2)
	if r.hasShadow() {
		padding += quadShadowPadding(r.shadowBlur)
	}

	extentX := halfWidth + padding
	extentY := halfHeight + padding
	locals := [6][2]float32{
		{-extentX, -extentY},
		{extentX, -extentY},
		{extentX, extentY},
		{-extentX, -extentY},
		{extentX, extentY},
		{-extentX, extentY},
	}

	start := uint32(len(r.vertices))
	for _, local := range locals {
		px := center.X + tx*local[0] + nx*local[1]
		py := center.Y + ty*local[0] + ny*local[1]
		r.appendSDFVertexWithShadow(px, py, local[0], local[1], halfWidth, halfHeight, radius, strokeWidth, color, ColorTransparent, 0)
	}

	r.appendBatch(nil, r.activeTextureGroup(), nil, start, 6)
}

// appendSDFVertex appends one vertex for distance-field evaluated geometry.
func (r *Context) appendSDFVertex(x, y, localX, localY, halfWidth, halfHeight, radius, strokeWidth float32, color Color) {
	r.appendSDFVertexWithShadow(x, y, localX, localY, halfWidth, halfHeight, radius, strokeWidth, color, r.effectiveShadowColor(), r.shadowBlur)
}

func (r *Context) appendSDFVertexWithShadow(x, y, localX, localY, halfWidth, halfHeight, radius, strokeWidth float32, color, shadowColor Color, shadowBlur float32) {
	color = r.applyGlobalAlpha(color)
	var clipX, clipY float32 = r.pixelToClip(x, y)
	r.vertices = append(r.vertices, vertex{
		Position:    [2]float32{clipX, clipY},
		Local:       [2]float32{localX, localY},
		HalfSize:    [2]float32{halfWidth, halfHeight},
		Color:       [4]float32{color.R, color.G, color.B, color.A},
		ShadowColor: [4]float32{shadowColor.R, shadowColor.G, shadowColor.B, shadowColor.A},
		Radius:      radius,
		Kind:        ctxKindRoundedRect,
		StrokeWidth: strokeWidth,
		ShadowBlur:  shadowBlur,
	})
}

// appendTextQuad adds a possibly transformed quad for a single glyph to the vertex buffer, using the glyph's atlas UVs and the specified color.
func (r *Context) appendTextQuad(face *fontFace, p0, p1, p2, p3 Point, g glyph, color Color) {
	shadowBlur := r.effectiveShadowBlurForQuads()
	padding := quadShadowPadding(shadowBlur)
	if padding > 0 {
		p0 = Point{X: p0.X - padding, Y: p0.Y - padding}
		p1 = Point{X: p1.X + padding, Y: p1.Y - padding}
		p2 = Point{X: p2.X + padding, Y: p2.Y + padding}
		p3 = Point{X: p3.X - padding, Y: p3.Y + padding}
	}

	start := uint32(len(r.vertices))
	var positions [6]Point = [6]Point{
		p0,
		p1,
		p2,
		p0,
		p2,
		p3,
	}

	color = r.applyGlobalAlpha(color)
	shadowColor := r.effectiveShadowColorForQuads()
	halfWidth := g.W * 0.5
	halfHeight := g.H * 0.5
	paddedHalfWidth := halfWidth + padding
	paddedHalfHeight := halfHeight + padding
	locals := [6][2]float32{
		{-paddedHalfWidth, -paddedHalfHeight},
		{paddedHalfWidth, -paddedHalfHeight},
		{paddedHalfWidth, paddedHalfHeight},
		{-paddedHalfWidth, -paddedHalfHeight},
		{paddedHalfWidth, paddedHalfHeight},
		{-paddedHalfWidth, paddedHalfHeight},
	}

	for i := range 6 {
		var clipX, clipY float32 = r.pixelToClip(positions[i].X, positions[i].Y)
		r.vertices = append(r.vertices, vertex{
			Position:    [2]float32{clipX, clipY},
			Local:       locals[i],
			HalfSize:    [2]float32{halfWidth, halfHeight},
			UV:          [2]float32{g.U0, g.V0},
			UVMin:       [2]float32{g.U0, g.V0},
			UVMax:       [2]float32{g.U1, g.V1},
			Color:       [4]float32{color.R, color.G, color.B, color.A},
			ShadowColor: [4]float32{shadowColor.R, shadowColor.G, shadowColor.B, shadowColor.A},
			Kind:        ctxKindText,
			ShadowBlur:  shadowBlur,
		})
	}

	r.appendBatch(nil, face.group, nil, start, 6)
}

func (r *Context) hasShadow() bool {
	return r != nil && r.shadowBlur > 0 && r.shadowColor.A > 0
}

func (r *Context) appendPathShadow(subpaths []pathSubpath) {
	for _, sample := range shapeShadowSamples(r.shadowBlur) {
		color := r.applyGlobalAlpha(r.shadowColor)
		color.A *= sample.weight
		if color.A <= 0 {
			continue
		}

		offset := Point{X: sample.dx, Y: sample.dy}
		for _, subpath := range subpaths {
			r.appendFilledPolygonOffset(subpath.points, offset, color)
		}
	}
}

func (r *Context) appendStrokeShadow(subpaths []pathSubpath) {
	for _, sample := range shapeShadowSamples(r.shadowBlur) {
		color := r.applyGlobalAlpha(r.shadowColor)
		color.A *= sample.weight
		if color.A <= 0 {
			continue
		}

		offset := Point{X: sample.dx, Y: sample.dy}
		for _, subpath := range subpaths {
			r.appendStrokePathOffset(subpath.points, subpath.closed, r.lineWidth, color, offset)
		}
	}
}

func (r *Context) applyGlobalAlpha(color Color) Color {
	color.A *= r.globalAlpha
	return color
}

func (r *Context) effectiveShadowColor() Color {
	if !r.hasShadow() {
		return ColorTransparent
	}

	return r.applyGlobalAlpha(r.shadowColor)
}

func (r *Context) effectiveShadowColorForQuads() Color {
	if r.currentImageShader != nil {
		return ColorTransparent
	}

	return r.effectiveShadowColor()
}

func (r *Context) effectiveShadowBlurForQuads() float32 {
	if r.currentImageShader != nil || !r.hasShadow() {
		return 0
	}

	return r.shadowBlur
}

func quadShadowPadding(blur float32) float32 {
	if blur <= 0 {
		return 0
	}

	return blur*2 + 2
}

type shadowSample struct {
	dx     float32
	dy     float32
	weight float32
}

func shapeShadowSamples(blur float32) []shadowSample {
	if blur <= 0 {
		return nil
	}

	outerCount := int(math.Ceil(float64(blur * 1.5)))
	if outerCount < 8 {
		outerCount = 8
	}
	if outerCount > 20 {
		outerCount = 20
	}

	innerCount := outerCount / 2
	samples := make([]shadowSample, 0, outerCount+innerCount)
	var totalWeight float32

	if blur > 1 {
		innerRadius := blur * 0.55
		for i := range innerCount {
			angle := float64(i) * 2 * math.Pi / float64(innerCount)
			samples = append(samples, shadowSample{
				dx:     float32(math.Cos(angle)) * innerRadius,
				dy:     float32(math.Sin(angle)) * innerRadius,
				weight: 1,
			})
			totalWeight += 1
		}
	}

	for i := range outerCount {
		angle := float64(i) * 2 * math.Pi / float64(outerCount)
		samples = append(samples, shadowSample{
			dx:     float32(math.Cos(angle)) * blur,
			dy:     float32(math.Sin(angle)) * blur,
			weight: 0.8,
		})
		totalWeight += 0.8
	}

	if totalWeight <= 0 {
		return nil
	}

	scale := 1 / totalWeight
	for i := range samples {
		samples[i].weight *= scale
	}

	return samples
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
		if last.clipOperation == clipOperationNone && last.stencilReference == uint32(len(r.clips)) && last.pipeline == pipeline && last.group == group && last.uniform == uniform && last.start+last.count == start {
			last.count += count
			return
		}
	}

	r.batches = append(r.batches, batch{pipeline: pipeline, group: group, uniform: uniform, start: start, count: count, stencilReference: uint32(len(r.clips))})
}

func (r *Context) ensureStencilAttachment() (err error) {
	if r.stencilTexture != nil && r.stencilView != nil && r.stencilWidth == r.width && r.stencilHeight == r.height {
		return nil
	}
	r.releaseStencilAttachment()
	if r.width <= 0 || r.height <= 0 {
		return fmt.Errorf("invalid clipping attachment size %dx%d", r.width, r.height)
	}
	if r.stencilTexture, err = r.device.CreateTexture(&wgpu.TextureDescriptor{
		Label:         "gctx2d Clip Stencil",
		Size:          wgpu.Extent3D{Width: uint32(r.width), Height: uint32(r.height), DepthOrArrayLayers: 1},
		MipLevelCount: 1,
		SampleCount:   1,
		Dimension:     gputypes.TextureDimension2D,
		Format:        stencilFormat,
		Usage:         gputypes.TextureUsageRenderAttachment,
	}); err != nil {
		return fmt.Errorf("create clip stencil texture: %w", err)
	}
	if r.stencilView, err = r.device.CreateTextureView(r.stencilTexture, nil); err != nil {
		r.releaseStencilAttachment()
		return fmt.Errorf("create clip stencil view: %w", err)
	}
	r.stencilWidth, r.stencilHeight = r.width, r.height
	return nil
}

func (r *Context) releaseStencilAttachment() {
	if r.stencilView != nil {
		r.stencilView.Release()
		r.stencilView = nil
	}
	if r.stencilTexture != nil {
		r.stencilTexture.Release()
		r.stencilTexture = nil
	}
	r.stencilWidth, r.stencilHeight = 0, 0
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
