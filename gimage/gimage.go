package gimage

import (
	"fmt"
	"image"
	"image/draw"
	"os"
	"path/filepath"
	"strings"
	"sync"

	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"

	"github.com/gogpu/gputypes"
	"github.com/gogpu/wgpu"
)

const uploadChunkBytes = 16 * 1024 * 1024

var (
	cacheLock sync.Mutex
	images    map[string]*Image              = make(map[string]*Image)
	samplers  map[*wgpu.Device]*wgpu.Sampler = make(map[*wgpu.Device]*wgpu.Sampler)
)

// LoadImage loads an image from filePath and caches it by normalized file name.
// The device is passed explicitly so this package does not need package-level initialization.
func LoadImage(device *wgpu.Device, filePath string) (img *Image, err error) {
	if device == nil {
		err = fmt.Errorf("nil wgpu device")
		return
	}

	if filePath, err = clean(filePath); err != nil {
		return
	}

	cacheLock.Lock()
	defer cacheLock.Unlock()

	if images != nil {
		if img = images[filePath]; img != nil {
			return
		}
	}

	var file *os.File
	// #nosec G304 -- LoadImage intentionally accepts a caller-selected image path.
	if file, err = os.Open(filePath); err != nil {
		err = fmt.Errorf("open image %q: %w", filePath, err)
		return
	}

	defer file.Close()

	var decoded image.Image
	if decoded, _, err = image.Decode(file); err != nil {
		err = fmt.Errorf("decode image %q: %w", filePath, err)
		return
	}

	var sampler *wgpu.Sampler
	if sampler, err = samplerForDevice(device); err != nil {
		return
	}

	if img, err = imageFromGoImage(device, sampler, assetName(filePath), filePath, decoded); err != nil {
		return
	}

	images[filePath] = img
	return
}

// FromGoImage creates a new Image from a standard Go image.Image.
// The device is passed explicitly so this package does not need package-level initialization.
func FromGoImage(device *wgpu.Device, name string, stdImg image.Image) (img *Image, err error) {
	if device == nil {
		err = fmt.Errorf("nil wgpu device")
		return
	}

	var sampler *wgpu.Sampler
	if sampler, err = samplerForDevice(device); err != nil {
		return
	}

	if img, err = imageFromGoImage(device, sampler, name, name, stdImg); err != nil {
		return
	}

	return
}

// Flush ensures that all pending texture uploads are completed by submitting an empty command buffer and polling the device queue.
// This is necessary after uploading textures to ensure they are available for rendering, especially when uploads are done in chunks.
func Flush(device *wgpu.Device) (err error) {
	if device == nil {
		err = fmt.Errorf("nil wgpu device")
		return
	}

	var encoder *wgpu.CommandEncoder
	if encoder, err = device.CreateCommandEncoder(&wgpu.CommandEncoderDescriptor{Label: "gctx2d Image Upload Flush"}); err != nil {
		err = fmt.Errorf("create image upload flush encoder: %w", err)
		return
	}

	var commands *wgpu.CommandBuffer
	if commands, err = encoder.Finish(); err != nil {
		err = fmt.Errorf("finish image upload flush commands: %w", err)
		return
	}

	defer commands.Release()
	if _, err = device.Queue().Submit(commands); err != nil {
		err = fmt.Errorf("submit image upload flush: %w", err)
		return
	}

	device.Queue().Poll()
	return
}

// Release releases all cached images and samplers, freeing their GPU resources and clearing the caches to prevent reuse.
// This should be called when the application is shutting down or when the GPU device is being reset to avoid resource leaks.
func Release() {
	cacheLock.Lock()
	defer cacheLock.Unlock()

	for _, img := range images {
		img.Release()
	}

	for _, sampler := range samplers {
		if sampler != nil {
			sampler.Release()
		}
	}

	images = nil
	samplers = nil
}

// samplerForDevice returns a sampler for the given device, creating and caching it if necessary to avoid redundant samplers for the same device.
func samplerForDevice(device *wgpu.Device) (sampler *wgpu.Sampler, err error) {
	if device == nil {
		err = fmt.Errorf("nil wgpu device")
		return
	}

	if samplers == nil {
		samplers = make(map[*wgpu.Device]*wgpu.Sampler)
	}

	if sampler = samplers[device]; sampler != nil {
		return
	}

	if sampler, err = device.CreateSampler(&wgpu.SamplerDescriptor{
		Label:        "gctx2d Image Sampler",
		AddressModeU: gputypes.AddressModeClampToEdge,
		AddressModeV: gputypes.AddressModeClampToEdge,
		AddressModeW: gputypes.AddressModeClampToEdge,
		MagFilter:    gputypes.FilterModeLinear,
		MinFilter:    gputypes.FilterModeLinear,
		MipmapFilter: gputypes.FilterModeNearest,
		LodMinClamp:  0,
		LodMaxClamp:  32,
	}); err != nil {
		err = fmt.Errorf("create image sampler: %w", err)
		return
	}

	samplers[device] = sampler
	return
}

// imageFromGoImage creates a GPU texture from a standard Go image.Image and returns an Image struct containing the texture and its metadata.
func imageFromGoImage(device *wgpu.Device, sampler *wgpu.Sampler, name, source string, img image.Image) (asset *Image, err error) {
	var bounds image.Rectangle = img.Bounds()
	if bounds.Empty() {
		err = fmt.Errorf("image %q is empty", source)
		return
	}

	var rgba *image.NRGBA = image.NewNRGBA(image.Rect(0, 0, bounds.Dx(), bounds.Dy()))
	draw.Draw(rgba, rgba.Bounds(), img, bounds.Min, draw.Src)

	var texture *wgpu.Texture
	if texture, err = device.CreateTexture(&wgpu.TextureDescriptor{
		Label: source,
		Size: wgpu.Extent3D{
			Width:              uint32(rgba.Bounds().Dx()),
			Height:             uint32(rgba.Bounds().Dy()),
			DepthOrArrayLayers: 1,
		},
		MipLevelCount: 1,
		SampleCount:   1,
		Dimension:     gputypes.TextureDimension2D,
		Format:        gputypes.TextureFormatRGBA8Unorm,
		Usage:         gputypes.TextureUsageTextureBinding | gputypes.TextureUsageCopyDst,
	}); err != nil {
		err = fmt.Errorf("create image texture %q: %w", source, err)
		return
	}

	if err = uploadTexture(device, source, texture, rgba); err != nil {
		texture.Release()
		return
	}

	var view *wgpu.TextureView
	if view, err = device.CreateTextureView(texture, nil); err != nil {
		texture.Release()
		err = fmt.Errorf("create image texture view %q: %w", source, err)
		return
	}

	asset = &Image{
		Name:    name,
		Path:    source,
		Width:   rgba.Bounds().Dx(),
		Height:  rgba.Bounds().Dy(),
		Format:  gputypes.TextureFormatRGBA8Unorm,
		Texture: texture,
		View:    view,
		Sampler: sampler,
	}

	return
}

// uploadTexture uploads the pixel data from the given RGBA image to the specified GPU texture in chunks to manage memory usage.
func uploadTexture(device *wgpu.Device, name string, texture *wgpu.Texture, rgba *image.NRGBA) (err error) {
	var (
		bounds         image.Rectangle = rgba.Bounds()
		width, height  int             = bounds.Dx(), bounds.Dy()
		rowBytes       int             = width * 4
		bytesPerRow    int             = (rowBytes + 255) &^ 255
		chunkRows      int             = min(max(1, uploadChunkBytes/bytesPerRow), height)
		flushEachChunk bool            = bytesPerRow*height > uploadChunkBytes
		chunk          []byte          = make([]byte, bytesPerRow*chunkRows)
	)

	for y := 0; y < height; y += chunkRows {
		var (
			rows int    = min(chunkRows, height-y)
			data []byte = chunk[:bytesPerRow*rows]
		)

		if bytesPerRow != rowBytes {
			clear(data)
		}

		for row := range rows {
			var srcY int = y + row
			copy(data[row*bytesPerRow:row*bytesPerRow+rowBytes], rgba.Pix[srcY*rgba.Stride:srcY*rgba.Stride+rowBytes])
		}

		if err = device.Queue().WriteTexture(
			&wgpu.ImageCopyTexture{
				Texture: texture,
				Origin:  wgpu.Origin3D{Y: uint32(y)},
				Aspect:  gputypes.TextureAspectAll,
			},
			data,
			&wgpu.ImageDataLayout{
				BytesPerRow:  uint32(bytesPerRow),
				RowsPerImage: uint32(rows),
			},
			&wgpu.Extent3D{
				Width:              uint32(width),
				Height:             uint32(rows),
				DepthOrArrayLayers: 1,
			},
		); err != nil {
			err = fmt.Errorf("upload image %q rows %d-%d: %w", name, y, y+rows-1, err)
			return
		}

		if flushEachChunk {
			if err = Flush(device); err != nil {
				err = fmt.Errorf("flush image upload %q rows %d-%d: %w", name, y, y+rows-1, err)
				return
			}
		}
	}

	return
}

// Release frees the GPU resources associated with the image and clears its fields to prevent reuse.
func (i *Image) Release() {
	if i == nil {
		return
	}

	if i.View != nil {
		i.View.Release()
	}

	if i.Texture != nil {
		i.Texture.Release()
	}

	*i = Image{}
}

// clean normalizes the file path and checks for invalid paths that could lead to security issues.
func clean(name string) (cleaned string, err error) {
	cleaned = filepath.Clean(strings.TrimSpace(name))
	if cleaned == "." || cleaned == string(filepath.Separator) || strings.HasPrefix(cleaned, "..") {
		err = fmt.Errorf("invalid image path %q", cleaned)
	}

	return
}

// assetName extracts the base name of the file without extension to use as the image's name.
func assetName(name string) string {
	return strings.TrimSuffix(filepath.Base(name), filepath.Ext(name))
}
