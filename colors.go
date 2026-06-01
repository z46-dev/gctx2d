package gctx2d

// Color represents a color in RGBA format, where each component is a float32 in the range [0, 1].
func RGBA(r, g, b, a float32) Color {
	return Color{R: r, G: g, B: b, A: a}
}

var (
	ColorTransparent = RGBA(0, 0, 0, 0)
	ColorWhite       = RGBA(1, 1, 1, 1)
	ColorBlack       = RGBA(0, 0, 0, 1)
	ColorRed         = RGBA(1, 0, 0, 1)
	ColorGreen       = RGBA(0, 1, 0, 1)
	ColorBlue        = RGBA(0, 0, 1, 1)
	ColorYellow      = RGBA(1, 1, 0, 1)
	ColorCyan        = RGBA(0, 1, 1, 1)
	ColorMagenta     = RGBA(1, 0, 1, 1)
	ColorOrange      = RGBA(1, 0.5, 0, 1)
	ColorPurple      = RGBA(0.5, 0, 0.5, 1)
	ColorPink        = RGBA(1, 0.75, 0.8, 1)
	ColorGray        = RGBA(0.5, 0.5, 0.5, 1)
)
