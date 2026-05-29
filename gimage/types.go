package gimage

import (
	stdimage "image"

	"github.com/gogpu/gputypes"
	"github.com/gogpu/wgpu"
)

type (
	Image struct {
		Name, Path    string
		Width, Height int
		Format        gputypes.TextureFormat
		Texture       *wgpu.Texture
		View          *wgpu.TextureView
		Sampler       *wgpu.Sampler
	}

	OffscreenCanvas struct {
		Image *stdimage.NRGBA
	}
)

// NewOffscreen creates a new offscreen canvas with the specified width and height.
// If either width or height is less than 1, it defaults to 1.
func NewOffscreen(width, height int) (canvas *OffscreenCanvas) {
	width, height = max(width, 1), max(height, 1)
	canvas = &OffscreenCanvas{
		Image: stdimage.NewNRGBA(stdimage.Rect(0, 0, width, height)),
	}

	return
}

// Bounds returns the bounds of the offscreen canvas's image.
func (c *OffscreenCanvas) Bounds() (rectangle stdimage.Rectangle) {
	if c != nil && c.Image != nil {
		rectangle = c.Image.Bounds()
	}

	return
}
