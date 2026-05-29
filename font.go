package gctx2d

import (
	"fmt"
	"image"
	"image/draw"
	"os"

	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

const (
	atlasWidth  = 1024
	atlasHeight = 1024
	atlasPad    = 2
)

// findSystemFont attempts to locate a common system font on the user's machine.
func FindSystemFont() (fontPath string) {
	var (
		err        error
		candidates []string = []string{
			// Windows
			"C:\\Windows\\Fonts\\arial.ttf",
			"C:\\Windows\\Fonts\\calibri.ttf",
			"C:\\Windows\\Fonts\\segoeui.ttf",
			// macOS
			"/Library/Fonts/Arial.ttf",
			"/System/Library/Fonts/Supplemental/Arial.ttf",
			"/System/Library/Fonts/Supplemental/Courier New.ttf",
			"/System/Library/Fonts/Supplemental/Times New Roman.ttf",
			"/System/Library/Fonts/Monaco.ttf",
			// Linux
			"/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf",
			"/usr/share/fonts/TTF/DejaVuSans.ttf",
			"/usr/share/fonts/liberation/LiberationSans-Regular.ttf",
		}
	)

	for _, fontPath = range candidates {
		if _, err = os.Stat(fontPath); err == nil {
			return
		}
	}

	fontPath = ""
	return
}

// buildFontAtlas creates a font atlas for the specified font and size.
func buildFontAtlas(fontPath string, size float64) (atlas *fontAtlas, err error) {
	var fontBytes []byte
	if fontBytes, err = os.ReadFile(fontPath); err != nil {
		err = fmt.Errorf("read font %q: %w", fontPath, err)
		return
	}

	var parsed *opentype.Font
	if parsed, err = opentype.Parse(fontBytes); err != nil {
		err = fmt.Errorf("parse font %q: %w", fontPath, err)
		return
	}

	var face font.Face
	if face, err = opentype.NewFace(parsed, &opentype.FaceOptions{
		Size:    size,
		DPI:     72,
		Hinting: font.HintingFull,
	}); err != nil {
		err = fmt.Errorf("create font face: %w", err)
		return
	}

	defer face.Close()

	var dst *image.RGBA = image.NewRGBA(image.Rect(0, 0, atlasWidth, atlasHeight))
	draw.Draw(dst, dst.Bounds(), image.Transparent, image.Point{}, draw.Src)

	var (
		metrics    font.Metrics = face.Metrics()
		lineHeight int          = metrics.Height.Ceil()
	)

	if lineHeight <= 0 {
		lineHeight = int(size * 1.35)
	}

	atlas = &fontAtlas{
		Width:      atlasWidth,
		Height:     atlasHeight,
		Pixels:     dst.Pix,
		Glyphs:     make(map[rune]glyph, 128),
		LineHeight: float32(lineHeight),
	}

	var (
		x, y, rowH int          = atlasPad, atlasPad, 0
		drawer     *font.Drawer = &font.Drawer{
			Dst:  dst,
			Src:  image.White,
			Face: face,
		}
	)

	for r := rune(32); r <= rune(126); r++ {
		var (
			s               string = string(r)
			bounds, advance        = font.BoundString(face, s)
			glyphW, glyphH  int    = max(1, (bounds.Max.X - bounds.Min.X).Ceil()), max(1, (bounds.Max.Y - bounds.Min.Y).Ceil())
			cellW, cellH    int    = glyphW + atlasPad*2, glyphH + atlasPad*2
		)

		if x+cellW >= atlasWidth {
			x = atlasPad
			y += rowH + atlasPad
			rowH = 0
		}

		if y+cellH >= atlasHeight {
			err = fmt.Errorf("font atlas overflow at rune %q", r)
			return
		}

		var gx, gy int = x + atlasPad, y + atlasPad

		if r != ' ' {
			drawer.Dot = fixed.Point26_6{
				X: fixed.I(gx) - bounds.Min.X,
				Y: fixed.I(gy) - bounds.Min.Y,
			}

			drawer.DrawString(s)
		}

		atlas.Glyphs[r] = glyph{
			X:        float32(gx),
			Y:        float32(gy),
			W:        float32(glyphW),
			H:        float32(glyphH),
			U0:       float32(gx) / float32(atlasWidth),
			V0:       float32(gy) / float32(atlasHeight),
			U1:       float32(gx+glyphW) / float32(atlasWidth),
			V1:       float32(gy+glyphH) / float32(atlasHeight),
			Advance:  float32(advance.Ceil()),
			BearingX: float32(bounds.Min.X.Floor()),
			BearingY: float32(bounds.Min.Y.Floor()),
		}

		x += cellW
		if cellH > rowH {
			rowH = cellH
		}
	}

	return
}
