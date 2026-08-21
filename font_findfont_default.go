//go:build !js || !wasm

package gctx2d

import (
	"fmt"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/flopp/go-findfont"
)

func DefaultFontName() (name string) {
	var (
		fonts              []string = findfont.List()
		bestChoicesInOrder          = []string{
			"Arial",
			"Cantarell",
			"DejaVu Sans",
			"FreeSans",
			"Helvetica",
			"Liberation Sans",
			"Noto Sans",
			"Adwaita Sans",
			"Droid Sans",
		}
	)

	if len(fonts) == 0 {
		return
	}

	var preferredMatch string
	var sansFallback string
	var firstFont string

	for _, fontPath := range fonts {
		var fontName string = filepath.Base(fontPath)
		fontName = strings.TrimSuffix(fontName, filepath.Ext(fontName))
		if firstFont == "" {
			firstFont = fontName
		}

		var normalizedFontName string = normalizeFontLookupName(fontName)
		var isRegular bool = isRegularFontVariant(normalizedFontName)

		for _, baseName := range bestChoicesInOrder {
			var normalizedChoice string = normalizeFontLookupName(baseName)
			if normalizedFontName == normalizedChoice || strings.HasPrefix(normalizedFontName, normalizedChoice) {
				if isRegular {
					return fontName
				}
				if preferredMatch == "" {
					preferredMatch = fontName
				}
				break
			}
		}

		if sansFallback == "" && strings.Contains(normalizedFontName, "sans") && isRegular {
			sansFallback = fontName
		}
	}

	if preferredMatch != "" {
		name = preferredMatch
		return
	}

	if sansFallback != "" {
		name = sansFallback
		return
	}

	if firstFont != "" {
		name = firstFont
		return
	}

	return
}

func isRegularFontVariant(normalizedName string) bool {
	var regularMarkers []string = []string{"regular", "roman", "book", "normal"}
	for _, marker := range regularMarkers {
		if strings.Contains(normalizedName, marker) {
			return true
		}
	}

	var nonRegularMarkers []string = []string{"bold", "italic", "oblique", "light", "medium", "semibold", "extrabold", "black", "thin"}
	return !slices.ContainsFunc(nonRegularMarkers, func(marker string) bool {
		return strings.Contains(normalizedName, marker)
	})
}

func LoadFontByName(name string) (handle *FontHandle, err error) {
	if strings.TrimSpace(name) == "" {
		err = fmt.Errorf("font name cannot be empty")
		return
	}

	var ext string = path.Ext(name)
	if ext != "" && !strings.EqualFold(ext, ".ttf") {
		err = fmt.Errorf("unsupported font file extension %q", ext)
		return
	}

	var fontPath string
	if fontPath, err = findfont.Find(fmt.Sprintf("%s.ttf", name)); err != nil {
		err = fmt.Errorf("find font %q: %w", name, err)
		return
	}

	handle, err = LoadFont(fontPath)
	return
}
