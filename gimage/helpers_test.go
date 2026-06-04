package gimage

import (
	"path/filepath"
	"testing"

	"github.com/gogpu/gputypes"
	"github.com/gogpu/wgpu"
)

func TestClean(t *testing.T) {
	cleaned, err := clean("  assets/example.png ")
	if err != nil {
		t.Fatalf("clean returned unexpected error: %v", err)
	}
	if cleaned != filepath.Clean("assets/example.png") {
		t.Fatalf("unexpected cleaned image path: %q", cleaned)
	}

	invalid := []string{"..", "..\\foo.png", "../foo.png", ".", string(filepath.Separator)}
	for _, name := range invalid {
		if _, err := clean(name); err == nil {
			t.Fatalf("expected invalid image path %q to fail", name)
		}
	}
}

func TestAssetName(t *testing.T) {
	if got := assetName("assets/icons/laser.blue.png"); got != "laser.blue" {
		t.Fatalf("unexpected asset name: %q", got)
	}
}

func TestImageReleaseAndPackageRelease(t *testing.T) {
	img := &Image{
		Name:   "x",
		Path:   "y",
		Width:  10,
		Height: 20,
		Format: gputypes.TextureFormatRGBA8Unorm,
	}
	img.Release()
	if *img != (Image{}) {
		t.Fatalf("expected released image to be zeroed, got %#v", img)
	}

	var nilImg *Image
	nilImg.Release()

	images = map[string]*Image{
		"a": {},
	}
	samplers = map[*wgpu.Device]*wgpu.Sampler{}
	Release()
	if images != nil || samplers != nil {
		t.Fatal("expected package Release to clear caches")
	}
}
