package gctx2d_test

import (
	"testing"

	"github.com/z46-dev/gctx2d"
)

func TestDefaultFont(t *testing.T) {
	var fontName = gctx2d.DefaultFontName()
	if fontName == "" {
		t.Fatal("DefaultFontName returned an empty string")
	}

	if _, err := gctx2d.LoadFontByName(fontName); err != nil {
		t.Fatalf("LoadFontByName(%q) failed: %v", fontName, err)
	}

	t.Logf("Default font: %s", fontName)
}
