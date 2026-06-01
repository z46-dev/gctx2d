package gimage

import (
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
)
