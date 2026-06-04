package gctx2d

import (
	"path/filepath"
	"testing"
)

func TestFontHandlePathAndNameNormalization(t *testing.T) {
	var nilHandle *FontHandle
	if nilHandle.Path() != "" {
		t.Fatal("nil FontHandle.Path should return empty string")
	}

	handle := &FontHandle{path: "C:\\Fonts\\Example.ttf"}
	if handle.Path() != "C:\\Fonts\\Example.ttf" {
		t.Fatalf("unexpected font path: %q", handle.Path())
	}

	if got := normalizeFontLookupName("DejaVu Sans-Bold_Italic"); got != "dejavusansbolditalic" {
		t.Fatalf("unexpected normalized font name: %q", got)
	}
}

func TestRegularVariantDetection(t *testing.T) {
	cases := []struct {
		name string
		want bool
	}{
		{name: "sourcesansregular", want: true},
		{name: "timesnewromanbook", want: true},
		{name: "robotobold", want: false},
		{name: "cantarellitalic", want: false},
	}

	for _, tc := range cases {
		if got := isRegularFontVariant(tc.name); got != tc.want {
			t.Fatalf("%q: expected %v, got %v", tc.name, tc.want, got)
		}
	}
}

func TestFontWeightHelpers(t *testing.T) {
	if got := normalizeFontWeight(0); got != FontWeightRegular {
		t.Fatalf("expected zero weight to normalize to regular, got %v", got)
	}
	if got := normalizeFontWeight(50); got != FontWeightThin {
		t.Fatalf("expected low weight clamp to thin, got %v", got)
	}
	if got := normalizeFontWeight(750); got != FontWeightExtraBold {
		t.Fatalf("expected 750 to normalize to 800, got %v", got)
	}
	if got := normalizeFontWeight(1000); got != FontWeightBlack {
		t.Fatalf("expected high weight clamp to black, got %v", got)
	}

	if got := fontWeightFromArgs(nil); got != FontWeightRegular {
		t.Fatalf("expected default arg weight to be regular, got %v", got)
	}
	if got := fontWeightFromArgs([]FontWeight{650}); got != FontWeightBold {
		t.Fatalf("expected arg weight to normalize to bold, got %v", got)
	}

	if got := syntheticFontWeightOffsets(FontWeightRegular); len(got) != 1 || got[0] != 0 {
		t.Fatalf("unexpected regular synthetic offsets: %#v", got)
	}
	if got := syntheticFontWeightOffsets(FontWeightSemiBold); len(got) != 2 {
		t.Fatalf("unexpected semibold synthetic offsets: %#v", got)
	}
	if got := syntheticFontWeightOffsets(FontWeightBlack); len(got) != 4 {
		t.Fatalf("unexpected black synthetic offsets: %#v", got)
	}
}

func TestCleanFontPath(t *testing.T) {
	cleaned, err := cleanFontPath("  .\\fonts\\example.ttf  ")
	if err != nil {
		t.Fatalf("cleanFontPath returned unexpected error: %v", err)
	}
	if cleaned != filepath.Clean(".\\fonts\\example.ttf") {
		t.Fatalf("unexpected cleaned path: %q", cleaned)
	}

	invalid := []string{"", "   ", ".", string(filepath.Separator)}
	for _, path := range invalid {
		if _, err := cleanFontPath(path); err == nil {
			t.Fatalf("expected invalid font path %q to fail", path)
		}
	}
}

func TestLoadFontByNameValidation(t *testing.T) {
	if _, err := LoadFontByName(""); err == nil {
		t.Fatal("expected empty font name to fail")
	}

	if _, err := LoadFontByName("font.otf"); err == nil {
		t.Fatal("expected unsupported font extension to fail")
	}
}

func TestLoadFontValidation(t *testing.T) {
	if _, err := LoadFont(""); err == nil {
		t.Fatal("expected empty font path to fail")
	}
}
