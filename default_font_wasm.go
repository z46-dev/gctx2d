//go:build js && wasm

package gctx2d

import (
	"fmt"

	"golang.org/x/image/font/opentype"
)

// loadDefaultFont provides an embedded fallback font for browser builds.
func loadDefaultFont() (handle *FontHandle, err error) {
	var parsed *opentype.Font
	if parsed, err = opentype.Parse(fallbackFontBytes); err != nil {
		err = fmt.Errorf("parse embedded fallback font: %w", err)
		return
	}

	handle = &FontHandle{
		path:   "embedded:fallback_ubuntu_bold.ttf",
		parsed: parsed,
	}
	return
}
