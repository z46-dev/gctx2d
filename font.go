package gctx2d

import (
	"fmt"
	"image"
	"image/draw"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"

	"github.com/flopp/go-findfont"
)

const (
	atlasWidth  = 1024
	atlasHeight = 1024
	atlasPad    = 2
)

const (
	FontWeightThin       FontWeight = 100
	FontWeightExtraLight FontWeight = 200
	FontWeightLight      FontWeight = 300
	FontWeightRegular    FontWeight = 400
	FontWeightNormal                = FontWeightRegular
	FontWeightMedium     FontWeight = 500
	FontWeightSemiBold   FontWeight = 600
	FontWeightBold       FontWeight = 700
	FontWeightExtraBold  FontWeight = 800
	FontWeightBlack      FontWeight = 900
)

const (
	TextAlignLeft TextAlign = iota
	TextAlignCenter
	TextAlignRight
)

const (
	TextBaselineAlphabetic TextBaseline = iota
	TextBaselineTop
	TextBaselineMiddle
	TextBaselineBottom
)

var (
	loadedFontsLock sync.Mutex
	loadedFonts     map[string]*FontHandle = make(map[string]*FontHandle)
)

// FontHandle is an opaque handle returned by LoadFont and consumed by Context.SetFont.
// A nil *FontHandle passed to SetFont selects the context's default font.
type FontHandle struct {
	path   string
	parsed *opentype.Font
}

// Path returns the normalized file path that was loaded for this font handle.
func (h *FontHandle) Path() string {
	if h == nil {
		return ""
	}

	return h.path
}

func DefaultFontName() (name string) {
	var (
		fonts              []string = findfont.List()
		bestChoicesInOrder          = []string{
			"Arial",
			"DejaVu Sans",
			"FreeSans",
			"Helvetica",
			"Liberation Sans",
			"Noto Sans",
		}
	)

	// Transform bestChoicesInOrder to also have lowercase and no space variants (_, -, and nothing) for more robust matching.
	var extendedBestChoices []string
	for _, baseName := range bestChoicesInOrder {
		extendedBestChoices = append(extendedBestChoices,
			baseName,
			strings.ToLower(baseName),
			strings.ReplaceAll(baseName, " ", ""),
			strings.ReplaceAll(baseName, " ", "_"),
			strings.ReplaceAll(baseName, " ", "-"),
		)
	}

	for _, fontPath := range fonts {
		var fontName string = filepath.Base(fontPath)
		fontName = strings.TrimSuffix(fontName, filepath.Ext(fontName))

		if slices.Contains(extendedBestChoices, fontName) {
			name = fontName
			return
		}
	}

	return
}

// LoadFont loads and parses a TrueType/OpenType font file and returns a reusable handle.
// Loaded handles are cached by normalized file path, so repeated calls for the same file
// are cheap and return the same handle. Passing an empty path loads FindSystemFont().
func LoadFont(filePath string) (handle *FontHandle, err error) {
	if strings.TrimSpace(filePath) == "" {
		err = fmt.Errorf("font file path cannot be empty")
		return
	}

	if filePath, err = cleanFontPath(filePath); err != nil {
		return
	}

	loadedFontsLock.Lock()
	defer loadedFontsLock.Unlock()

	if loadedFonts == nil {
		loadedFonts = make(map[string]*FontHandle)
	}

	if handle = loadedFonts[filePath]; handle != nil {
		return
	}

	var fontBytes []byte
	if fontBytes, err = os.ReadFile(filePath); err != nil {
		err = fmt.Errorf("read font %q: %w", filePath, err)
		return
	}

	var parsed *opentype.Font
	if parsed, err = opentype.Parse(fontBytes); err != nil {
		err = fmt.Errorf("parse font %q: %w", filePath, err)
		return
	}

	handle = &FontHandle{
		path:   filePath,
		parsed: parsed,
	}

	loadedFonts[filePath] = handle
	return
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

	var filepath string
	if filepath, err = findfont.Find(fmt.Sprintf("%s.ttf", name)); err != nil {
		err = fmt.Errorf("find font %q: %w", name, err)
		return
	}

	handle, err = LoadFont(filepath)
	return
}

// buildFontAtlas creates a font atlas for the specified loaded font, size, and weight.
func buildFontAtlas(handle *FontHandle, size float64, weight FontWeight) (atlas *fontAtlas, err error) {
	if handle == nil || handle.parsed == nil {
		err = fmt.Errorf("nil font handle")
		return
	}

	if size <= 0 {
		err = fmt.Errorf("font size must be positive")
		return
	}

	weight = normalizeFontWeight(weight)

	var face font.Face
	if face, err = opentype.NewFace(handle.parsed, &opentype.FaceOptions{
		Size:    size,
		DPI:     72,
		Hinting: font.HintingFull,
	}); err != nil {
		err = fmt.Errorf("create font face %q at %.2fpx: %w", handle.path, size, err)
		return
	}

	defer face.Close()

	var dst *image.RGBA = image.NewRGBA(image.Rect(0, 0, atlasWidth, atlasHeight))
	draw.Draw(dst, dst.Bounds(), image.Transparent, image.Point{}, draw.Src)

	var (
		metrics    font.Metrics = face.Metrics()
		ascent     int          = metrics.Ascent.Ceil()
		descent    int          = metrics.Descent.Ceil()
		lineHeight int          = metrics.Height.Ceil()
	)

	if ascent <= 0 {
		ascent = int(size * 0.8)
	}

	if descent <= 0 {
		descent = max(1, int(size*0.2))
	}

	if lineHeight <= 0 {
		lineHeight = int(size * 1.35)
	}

	atlas = &fontAtlas{
		Width:      atlasWidth,
		Height:     atlasHeight,
		Pixels:     dst.Pix,
		Glyphs:     make(map[rune]glyph, 128),
		LineHeight: float32(lineHeight),
		Ascent:     float32(ascent),
		Descent:    float32(descent),
	}

	var (
		x, y, rowH int          = atlasPad, atlasPad, 0
		drawer     *font.Drawer = &font.Drawer{
			Dst:  dst,
			Src:  image.White,
			Face: face,
		}
		xOffsets []int = syntheticFontWeightOffsets(weight)
		extraW   int   = max(0, len(xOffsets)-1)
	)

	for r := rune(32); r <= rune(126); r++ {
		var (
			s               string = string(r)
			bounds, advance        = font.BoundString(face, s)
			glyphW, glyphH  int    = max(1, (bounds.Max.X-bounds.Min.X).Ceil()+extraW), max(1, (bounds.Max.Y - bounds.Min.Y).Ceil())
			cellW, cellH    int    = glyphW + atlasPad*2, glyphH + atlasPad*2
		)

		if x+cellW >= atlasWidth {
			x = atlasPad
			y += rowH + atlasPad
			rowH = 0
		}

		if y+cellH >= atlasHeight {
			err = fmt.Errorf("font atlas overflow at rune %q for %q at %.2fpx weight %d", r, handle.path, size, weight)
			return
		}

		var gx, gy int = x + atlasPad, y + atlasPad

		if r != ' ' {
			for _, xOffset := range xOffsets {
				drawer.Dot = fixed.Point26_6{
					X: fixed.I(gx+xOffset) - bounds.Min.X,
					Y: fixed.I(gy) - bounds.Min.Y,
				}

				drawer.DrawString(s)
			}
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
			Advance:  float32(advance.Ceil() + extraW),
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

func normalizeFontWeight(weight FontWeight) FontWeight {
	if weight == 0 {
		return FontWeightRegular
	}

	if weight < FontWeightThin {
		return FontWeightThin
	}

	if weight > FontWeightBlack {
		return FontWeightBlack
	}

	return FontWeight(((int(weight) + 50) / 100) * 100)
}

func fontWeightFromArgs(weights []FontWeight) FontWeight {
	if len(weights) == 0 {
		return FontWeightRegular
	}

	return normalizeFontWeight(weights[0])
}

// syntheticFontWeightOffsets implements simple synthetic emboldening for cached atlas faces.
// Lighter-than-regular weights render with the base face because this renderer only has one font file per handle.
func syntheticFontWeightOffsets(weight FontWeight) []int {
	weight = normalizeFontWeight(weight)

	switch {
	case weight <= FontWeightRegular:
		return []int{0}
	case weight <= FontWeightSemiBold:
		return []int{0, 1}
	case weight <= FontWeightExtraBold:
		return []int{0, 1, 2}
	default:
		return []int{0, 1, 2, 3}
	}
}

func cleanFontPath(filePath string) (cleaned string, err error) {
	cleaned = filepath.Clean(strings.TrimSpace(filePath))
	if cleaned == "" || cleaned == "." || cleaned == string(filepath.Separator) {
		err = fmt.Errorf("invalid font path %q", filePath)
		return
	}

	return
}
