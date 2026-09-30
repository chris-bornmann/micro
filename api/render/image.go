package render

import (
	"image"
	"image/color"
	"image/draw"
	"sync"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gomono"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"

	"map_api/mapdata"
)

// glyphFont is the embedded Go Mono TTF every cell's glyph is drawn
// with — a real scalable monospace font, rasterized fresh at whatever
// pixel size the cell needs, rather than a fixed-size bitmap font
// nearest-neighbor-scaled up or down (which distorted descenders like
// 'g' at cell sizes that don't divide evenly into the bitmap's own
// dimensions).
var glyphFont = sync.OnceValue(func() *opentype.Font {
	f, err := opentype.Parse(gomono.TTF)
	if err != nil {
		panic("render: parse embedded Go Mono font: " + err.Error())
	}
	return f
})

// faceCache holds one font.Face per cellSize seen so far — building a
// Face isn't free, but cellSize is effectively constant for the life of
// a run (set once from a flag), so a tiny cache avoids rebuilding it on
// every single frame.
var faceCache sync.Map // int(cellSize) -> font.Face

func glyphFaceForCellSize(cellSize int) font.Face {
	if f, ok := faceCache.Load(cellSize); ok {
		return f.(font.Face)
	}
	// 0.8x cellSize leaves a little breathing room in the cell so
	// ascenders/descenders don't touch neighboring rows.
	face, err := opentype.NewFace(glyphFont(), &opentype.FaceOptions{
		Size:    float64(cellSize) * 0.8,
		DPI:     72,
		Hinting: font.HintingFull,
	})
	if err != nil {
		panic("render: build glyph face: " + err.Error())
	}
	faceCache.Store(cellSize, face)
	return face
}

// Image rasterizes m onto a blank Width*cellSize x Height*cellSize canvas:
// pure black everywhere, then each cell's glyph (from the same grid() ANSI()
// itself draws from) painted in its own color on top — walls/floors/doors/
// corridors/objects all drawn as characters, not solid blocks, matching how
// ANSI() already renders a terminal view of the same map.
func Image(m *mapdata.Map, opts Options, cellSize int) *image.RGBA {
	if cellSize < 1 {
		cellSize = 1
	}
	g := grid(m, opts)

	img := image.NewRGBA(image.Rect(0, 0, m.Width*cellSize, m.Height*cellSize))
	draw.Draw(img, img.Bounds(), image.NewUniform(color.Black), image.Point{}, draw.Src)

	for y, row := range g {
		for x, c := range row {
			drawGlyph(img, x, y, cellSize, c.ch, color.RGBA{R: c.color.R, G: c.color.G, B: c.color.B, A: 0xff})
		}
	}

	return img
}

// drawGlyph draws ch, colored c, into dst's (cellX,cellY) cell of size
// cellSize — horizontally centered on its own advance width, and
// vertically centered using the face's ascent/descent, so glyphs of
// different heights (e.g. 'g' with a descender vs '#') sit consistently
// within the cell rather than all sharing one fixed baseline.
func drawGlyph(dst *image.RGBA, cellX, cellY, cellSize int, ch rune, c color.RGBA) {
	if ch == 0 || ch == ' ' {
		return
	}

	face := glyphFaceForCellSize(cellSize)
	metrics := face.Metrics()
	x0, y0 := cellX*cellSize, cellY*cellSize

	advance, _ := face.GlyphAdvance(ch)
	xOffset := (fixed.I(cellSize) - advance) / 2

	lineHeight := (metrics.Ascent + metrics.Descent).Ceil()
	yPad := (cellSize - lineHeight) / 2
	if yPad < 0 {
		yPad = 0
	}

	d := &font.Drawer{
		Dst:  dst,
		Src:  image.NewUniform(c),
		Face: face,
		Dot:  fixed.Point26_6{X: fixed.I(x0) + xOffset, Y: fixed.I(y0+yPad) + metrics.Ascent},
	}
	d.DrawString(string(ch))
}
