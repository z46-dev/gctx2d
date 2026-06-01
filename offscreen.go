package gctx2d

import (
	"fmt"

	"github.com/gogpu/gputypes"
	"github.com/gogpu/wgpu"
	"github.com/z46-dev/gctx2d/gimage"
)

const DefaultOffscreenFormat gputypes.TextureFormat = gputypes.TextureFormatRGBA8Unorm

type OffscreenCanvas struct {
	Width  int
	Height int

	device       *wgpu.Device
	targetFormat gputypes.TextureFormat
	texture      *wgpu.Texture
	view         *wgpu.TextureView
	sampler      *wgpu.Sampler
	context      *Context
}

// NewOffscreenCanvas creates a GPU-backed render target that can vend a frame-ready 2D context.
// If no format is supplied, RGBA8Unorm is used.
func NewOffscreenCanvas(device *wgpu.Device, width, height int, formats ...gputypes.TextureFormat) (canvas *OffscreenCanvas, err error) {
	if device == nil {
		err = fmt.Errorf("nil wgpu device")
		return
	}

	width, height = max(width, 1), max(height, 1)

	targetFormat := DefaultOffscreenFormat
	if len(formats) > 0 {
		targetFormat = formats[0]
	}

	canvas = &OffscreenCanvas{
		Width:        width,
		Height:       height,
		device:       device,
		targetFormat: targetFormat,
	}

	if canvas.texture, err = device.CreateTexture(&wgpu.TextureDescriptor{
		Label: "gctx2d Offscreen Canvas",
		Size: wgpu.Extent3D{
			Width:              uint32(width),
			Height:             uint32(height),
			DepthOrArrayLayers: 1,
		},
		MipLevelCount: 1,
		SampleCount:   1,
		Dimension:     gputypes.TextureDimension2D,
		Format:        targetFormat,
		Usage:         gputypes.TextureUsageRenderAttachment | gputypes.TextureUsageTextureBinding | gputypes.TextureUsageCopySrc,
	}); err != nil {
		err = fmt.Errorf("create offscreen texture: %w", err)
		canvas.Release()
		canvas = nil
		return
	}

	if canvas.view, err = device.CreateTextureView(canvas.texture, nil); err != nil {
		err = fmt.Errorf("create offscreen texture view: %w", err)
		canvas.Release()
		canvas = nil
		return
	}

	if canvas.sampler, err = device.CreateSampler(&wgpu.SamplerDescriptor{
		Label:        "gctx2d Offscreen Canvas Sampler",
		AddressModeU: gputypes.AddressModeClampToEdge,
		AddressModeV: gputypes.AddressModeClampToEdge,
		AddressModeW: gputypes.AddressModeClampToEdge,
		MagFilter:    gputypes.FilterModeLinear,
		MinFilter:    gputypes.FilterModeLinear,
		MipmapFilter: gputypes.FilterModeNearest,
		LodMinClamp:  0,
		LodMaxClamp:  32,
	}); err != nil {
		err = fmt.Errorf("create offscreen sampler: %w", err)
		canvas.Release()
		canvas = nil
		return
	}

	return
}

// GetContext returns the canvas's drawing context and starts a new frame sized to the canvas.
func (c *OffscreenCanvas) GetContext() (ctx *Context, err error) {
	if c == nil {
		err = fmt.Errorf("nil offscreen canvas")
		return
	}

	if c.context == nil {
		if c.context, err = NewContext(c.device, c.targetFormat); err != nil {
			err = fmt.Errorf("create offscreen context: %w", err)
			return
		}
	}

	c.context.Begin(c.Width, c.Height)
	ctx = c.context
	return
}

// Flush submits the current offscreen frame to the canvas texture.
func (c *OffscreenCanvas) Flush(clearColor *gputypes.Color) (err error) {
	if c == nil {
		err = fmt.Errorf("nil offscreen canvas")
		return
	}

	if c.context == nil {
		if c.context, err = NewContext(c.device, c.targetFormat); err != nil {
			err = fmt.Errorf("create offscreen context: %w", err)
			return
		}

		c.context.Begin(c.Width, c.Height)
	}

	if c.view == nil {
		err = fmt.Errorf("offscreen canvas has no texture view")
		return
	}

	err = c.context.Flush(c.view, clearColor)
	return
}

func (c *OffscreenCanvas) Texture() *wgpu.Texture {
	if c == nil {
		return nil
	}

	return c.texture
}

func (c *OffscreenCanvas) View() *wgpu.TextureView {
	if c == nil {
		return nil
	}

	return c.view
}

func (c *OffscreenCanvas) Sampler() *wgpu.Sampler {
	if c == nil {
		return nil
	}

	return c.sampler
}

func (c *OffscreenCanvas) Format() gputypes.TextureFormat {
	if c == nil {
		return DefaultOffscreenFormat
	}

	return c.targetFormat
}

// Image returns a gimage.Image view over the offscreen canvas texture so it can be used with DrawImage.
// The returned image shares the canvas's GPU resources and must not be released independently.
func (c *OffscreenCanvas) Image(name string) *gimage.Image {
	if c == nil || c.texture == nil || c.view == nil || c.sampler == nil {
		return nil
	}

	return &gimage.Image{
		Name:    name,
		Path:    name,
		Width:   c.Width,
		Height:  c.Height,
		Format:  c.targetFormat,
		Texture: c.texture,
		View:    c.view,
		Sampler: c.sampler,
	}
}

func (c *OffscreenCanvas) Release() {
	if c == nil {
		return
	}

	if c.context != nil {
		c.context.Release()
		c.context = nil
	}

	if c.view != nil {
		c.view.Release()
		c.view = nil
	}

	if c.texture != nil {
		c.texture.Release()
		c.texture = nil
	}

	if c.sampler != nil {
		c.sampler.Release()
		c.sampler = nil
	}
}
