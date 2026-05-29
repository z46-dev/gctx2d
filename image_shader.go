package gctx2d

import (
	"fmt"
	"unsafe"

	"github.com/gogpu/gputypes"
	"github.com/gogpu/wgpu"
)

// NewImageShaderProgram compiles a full WGSL image shader program that can be shared by many shader instances.
// The WGSL source is used exactly as provided. It must define compatible vs_main and fs_main entry points.
// Group 0 is reserved for the image sampler/texture. Group 1 binding 0 is the typed uniform block T.
func NewImageShaderProgram[T any](ctx *Context, desc ImageShaderDescriptor) (program *ImageShaderProgram[T], err error) {
	if ctx == nil || ctx.device == nil {
		err = fmt.Errorf("nil gctx2d context")
		return
	}

	var (
		uniforms    T
		uniformSize uint64 = uint64(unsafe.Sizeof(uniforms))
	)

	if uniformSize == 0 {
		uniformSize = 16
	}

	program = &ImageShaderProgram[T]{
		device:          ctx.device,
		uniformByteSize: uniformSize,
	}

	defer func() {
		if err != nil && program != nil {
			program.Release()
			program = nil
		}
	}()

	if program.uniformLayout, err = ctx.device.CreateBindGroupLayout(&wgpu.BindGroupLayoutDescriptor{
		Label: desc.Label + " Uniform Layout",
		Entries: []gputypes.BindGroupLayoutEntry{{
			Binding:    0,
			Visibility: gputypes.ShaderStageFragment,
			Buffer: &gputypes.BufferBindingLayout{
				Type:             gputypes.BufferBindingTypeUniform,
				HasDynamicOffset: false,
				MinBindingSize:   uniformSize,
			},
		}},
	}); err != nil {
		err = fmt.Errorf("create image shader uniform layout: %w", err)
		return
	}

	if program.pipelineLayout, err = ctx.device.CreatePipelineLayout(&wgpu.PipelineLayoutDescriptor{
		Label:            desc.Label + " Pipeline Layout",
		BindGroupLayouts: []*wgpu.BindGroupLayout{ctx.textureLayout, program.uniformLayout},
	}); err != nil {
		err = fmt.Errorf("create image shader pipeline layout: %w", err)
		return
	}

	var module *wgpu.ShaderModule
	if module, err = ctx.device.CreateShaderModule(&wgpu.ShaderModuleDescriptor{
		Label: desc.Label,
		WGSL:  desc.WGSL,
	}); err != nil {
		err = fmt.Errorf("create image shader module: %w", err)
		return
	}

	defer module.Release()

	var blend = gputypes.BlendState{
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

	if program.pipeline, err = ctx.device.CreateRenderPipeline(&wgpu.RenderPipelineDescriptor{
		Label:  desc.Label + " Pipeline",
		Layout: program.pipelineLayout,
		Vertex: wgpu.VertexState{
			Module:     module,
			EntryPoint: "vs_main",
			Buffers: []gputypes.VertexBufferLayout{{
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
			}},
		},
		Primitive: gputypes.PrimitiveState{
			Topology: gputypes.PrimitiveTopologyTriangleList,
			CullMode: gputypes.CullModeNone,
		},
		Fragment: &wgpu.FragmentState{
			Module:     module,
			EntryPoint: "fs_main",
			Targets: []gputypes.ColorTargetState{{
				Format:    ctx.targetFormat,
				Blend:     &blend,
				WriteMask: gputypes.ColorWriteMaskAll,
			}},
		},
	}); err != nil {
		err = fmt.Errorf("create image shader pipeline: %w", err)
	}

	return
}

// NewImageShader compiles a shader program and creates one typed shader instance.
// Prefer NewImageShaderProgram when many objects share the same WGSL.
func NewImageShader[T any](ctx *Context, desc ImageShaderDescriptor, uniforms T) (shader *ImageShader[T], err error) {
	var program *ImageShaderProgram[T]
	if program, err = NewImageShaderProgram[T](ctx, desc); err != nil {
		return
	}

	if shader, err = program.NewInstance(uniforms); err != nil {
		program.Release()
		return
	}

	return
}

// Release frees the GPU resources associated with the shader program. It does not affect any shader instances, but they will no longer be usable.
func (p *ImageShaderProgram[T]) Release() {
	if p == nil {
		return
	}

	if p.pipeline != nil {
		p.pipeline.Release()
		p.pipeline = nil
	}
	if p.pipelineLayout != nil {
		p.pipelineLayout.Release()
		p.pipelineLayout = nil
	}
	if p.uniformLayout != nil {
		p.uniformLayout.Release()
		p.uniformLayout = nil
	}
}

// NewInstance creates a new shader instance with the given uniform values. The instance shares the program's pipeline and layout, but has its own uniform buffer and bind group.
func (p *ImageShaderProgram[T]) NewInstance(uniforms T) (shader *ImageShader[T], err error) {
	if p == nil || p.device == nil || p.uniformLayout == nil {
		err = fmt.Errorf("nil image shader program")
		return
	}

	shader = &ImageShader[T]{program: p}
	defer func() {
		if err != nil && shader != nil {
			shader.Release()
			shader = nil
		}
	}()

	if shader.uniformBuffer, err = p.device.CreateBuffer(&wgpu.BufferDescriptor{
		Label: "gctx2d Image Shader Uniform Buffer",
		Size:  p.uniformByteSize,
		Usage: gputypes.BufferUsageUniform | gputypes.BufferUsageCopyDst,
	}); err != nil {
		err = fmt.Errorf("create image shader uniform buffer: %w", err)
		return
	}

	if shader.uniformBindGroup, err = p.device.CreateBindGroup(&wgpu.BindGroupDescriptor{
		Label:  "gctx2d Image Shader Uniform Bind Group",
		Layout: p.uniformLayout,
		Entries: []wgpu.BindGroupEntry{{
			Binding: 0,
			Buffer:  shader.uniformBuffer,
			Size:    p.uniformByteSize,
		}},
	}); err != nil {
		err = fmt.Errorf("create image shader uniform bind group: %w", err)
		return
	}

	err = shader.SetUniforms(uniforms)
	return
}

// Release frees the GPU resources associated with the shader instance. It does not affect the program, but the instance will no longer be usable.
func (s *ImageShader[T]) Release() {
	if s == nil {
		return
	}

	if s.uniformBindGroup != nil {
		s.uniformBindGroup.Release()
		s.uniformBindGroup = nil
	}
	if s.uniformBuffer != nil {
		s.uniformBuffer.Release()
		s.uniformBuffer = nil
	}

	s.program = nil
}

// SetUniforms updates the uniform buffer with new values. The shader instance must have been created with a compatible uniform type T.
func (s *ImageShader[T]) SetUniforms(uniforms T) (err error) {
	if s == nil || s.program == nil || s.program.device == nil || s.uniformBuffer == nil {
		err = fmt.Errorf("nil image shader")
		return
	}

	s.uniforms = uniforms
	var data []byte = unsafe.Slice((*byte)(unsafe.Pointer(&s.uniforms)), int(s.program.uniformByteSize))
	if err = s.program.device.Queue().WriteBuffer(s.uniformBuffer, 0, data); err != nil {
		err = fmt.Errorf("upload image shader uniforms: %w", err)
	}

	return
}

func (s *ImageShader[T]) imagePipeline() *wgpu.RenderPipeline {
	if s == nil || s.program == nil {
		return nil
	}

	return s.program.pipeline
}

func (s *ImageShader[T]) imageUniformBindGroup() *wgpu.BindGroup {
	if s == nil {
		return nil
	}

	return s.uniformBindGroup
}
