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
		globalAlpha        float32
		shadowColor        Color
		shadowBlur         float32
		textAlign          TextAlign
		textBaseline       TextBaseline
		transform          Matrix
		imageEffect        ImageEffect
		effectTime         float32
		currentImageShader ImageShaderBinding
		currentFont        *fontFace
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

	batch struct {
		pipeline *wgpu.RenderPipeline
		group    *wgpu.BindGroup
		uniform  *wgpu.BindGroup
		start    uint32
		count    uint32
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

	ImageShaderProgram[T any] struct {
		device          *wgpu.Device
		pipeline        *wgpu.RenderPipeline
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

		pipeline       *wgpu.RenderPipeline
		pipelineLayout *wgpu.PipelineLayout
		textureLayout  *wgpu.BindGroupLayout
		imageGroups    map[*gimage.Image]*wgpu.BindGroup

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
		globalAlpha        float32
		shadowColor        Color
		shadowBlur         float32
		textAlign          TextAlign
		textBaseline       TextBaseline
		transform          Matrix
		imageEffect        ImageEffect
		effectTime         float32
		currentImageShader ImageShaderBinding
		stateStack         []drawingState
		path               []pathCommand

		pending []submission
	}
)
