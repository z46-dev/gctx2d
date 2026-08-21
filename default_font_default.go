//go:build !js || !wasm

package gctx2d

// loadDefaultFont resolves a system font on native platforms.
func loadDefaultFont() (handle *FontHandle, err error) {
	handle, err = LoadFontByName(DefaultFontName())
	return
}
