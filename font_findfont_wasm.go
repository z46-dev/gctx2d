//go:build js && wasm

package gctx2d

func DefaultFontName() (name string) {
	name = "embedded:fallback"
	return
}

func LoadFontByName(name string) (handle *FontHandle, err error) {
	handle, err = loadDefaultFont()
	return
}

func isRegularFontVariant(normalizedName string) bool {
	return normalizedName == "embedded:fallback"
}
