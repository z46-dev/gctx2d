package gctx2d

import (
	"github.com/gogpu/gputypes"
	"github.com/gogpu/wgpu"
	"github.com/z46-dev/gctx2d/gimage"
)

type (
	ImageEffect  int
	FontWeight   int
	TextAlign    int
	TextBaseline int
	LineCap      int
	LineJoin     int
	ClipMode     int

	Color struct {
		R float32
		G float32
		B float32
		A float32
	}

	Point struct {
		X float32
		Y float32
	}

	// TextLineMetrics describes a single line rendered with the active font.
	TextLineMetrics struct {
		Width, Ascent, Descent, LineHeight float32
	}

	Matrix struct {
		A float32
		B float32
		C float32
		D float32
		E float32
		F float32
	}

	drawingState struct {
		fillStyle          Color
		strokeStyle        Color
		lineWidth          float32
		lineCap            LineCap
		lineJoin           LineJoin
		miterLimit         float32
		globalAlpha        float32
		shadowColor        Color
		shadowBlur         float32
		textAlign          TextAlign
		textBaseline       TextBaseline
		disableTextKerning bool
		transform          Matrix
		imageEffect        ImageEffect
		effectTime         float32
		currentImageShader ImageShaderBinding
		currentFont        *fontFace
		clips              []*clipEntry
	}

	vertex struct {
		Position    [2]float32
		Local       [2]float32
		HalfSize    [2]float32
		UV          [2]float32
		UVMin       [2]float32
		UVMax       [2]float32
		Color       [4]float32
		ShadowColor [4]float32
		Radius      float32
		Kind        float32
		StrokeWidth float32
		EffectTime  float32
		ShadowBlur  float32
	}

	glyph struct {
		X float32
		Y float32
		W float32
		H float32

		U0 float32
		V0 float32
		U1 float32
		V1 float32

		Advance  float32
		BearingX float32
		BearingY float32
	}

	fontAtlas struct {
		Width      int
		Height     int
		Pixels     []byte
		Glyphs     map[rune]glyph
		Kerning    map[[2]rune]float32
		LineHeight float32
		Ascent     float32
		Descent    float32
	}

	fontFaceKey struct {
		handle *FontHandle
		size   float64
		weight FontWeight
	}

	fontFace struct {
		key     fontFaceKey
		atlas   *fontAtlas
		texture *wgpu.Texture
		view    *wgpu.TextureView
		group   *wgpu.BindGroup
	}

	pathCommandKind int

	pathCommand struct {
		kind pathCommandKind
		p    Point
	}

	pathSubpath struct {
		points []Point
		closed bool
	}

	strokeSegment struct {
		a      Point
		b      Point
		tx     float32
		ty     float32
		nx     float32
		ny     float32
		length float32
	}

	batch struct {
		pipeline         *wgpu.RenderPipeline
		group            *wgpu.BindGroup
		uniform          *wgpu.BindGroup
		start            uint32
		count            uint32
		stencilReference uint32
		clipOperation    clipOperation
	}

	clipOperation int

	vertexRange struct {
		start uint32
		count uint32
	}

	clipEntry struct {
		mode   ClipMode
		ranges []vertexRange
	}

	submission struct {
		index uint64
		cmd   *wgpu.CommandBuffer
	}

	ImageShaderDescriptor struct {
		Label string
		WGSL  string
	}

	ImageShaderBinding interface {
		imagePipeline() *wgpu.RenderPipeline
		imageUniformBindGroup() *wgpu.BindGroup
	}

	imageStencilShaderBinding interface {
		imageStencilPipeline() *wgpu.RenderPipeline
	}

	ImageShaderProgram[T any] struct {
		device          *wgpu.Device
		pipeline        *wgpu.RenderPipeline
		stencilPipeline *wgpu.RenderPipeline
		pipelineLayout  *wgpu.PipelineLayout
		uniformLayout   *wgpu.BindGroupLayout
		uniformByteSize uint64
	}

	ImageShader[T any] struct {
		program          *ImageShaderProgram[T]
		uniformBuffer    *wgpu.Buffer
		uniformBindGroup *wgpu.BindGroup
		uniforms         T
	}

	Context struct {
		device       *wgpu.Device
		targetFormat gputypes.TextureFormat

		pipeline              *wgpu.RenderPipeline
		stencilPipeline       *wgpu.RenderPipeline
		clipIncrementPipeline *wgpu.RenderPipeline
		clipDecrementPipeline *wgpu.RenderPipeline
		pipelineLayout        *wgpu.PipelineLayout
		textureLayout         *wgpu.BindGroupLayout
		imageGroups           map[*gimage.Image]*wgpu.BindGroup

		fontSampler *wgpu.Sampler
		defaultFont *FontHandle
		currentFont *fontFace
		fontFaces   map[fontFaceKey]*fontFace

		vertexBuffer   *wgpu.Buffer
		vertexCapacity int
		vertices       []vertex
		batches        []batch

		width  int
		height int

		fillStyle          Color
		strokeStyle        Color
		lineWidth          float32
		lineCap            LineCap
		lineJoin           LineJoin
		miterLimit         float32
		globalAlpha        float32
		shadowColor        Color
		shadowBlur         float32
		textAlign          TextAlign
		textBaseline       TextBaseline
		disableTextKerning bool
		transform          Matrix
		imageEffect        ImageEffect
		effectTime         float32
		currentImageShader ImageShaderBinding
		stateStack         []drawingState
		path               []pathCommand
		clips              []*clipEntry

		stencilTexture *wgpu.Texture
		stencilView    *wgpu.TextureView
		stencilWidth   int
		stencilHeight  int
		usesStencil    bool

		pending []submission
	}
)
