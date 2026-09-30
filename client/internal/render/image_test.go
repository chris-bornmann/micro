package render

import (
	"image"
	"image/color"
	"testing"

	"micromap/client/internal/screen"
)

// gridOf builds a screen.Grid of the given size filled with blanks, then
// applies each edit. Tests construct grids directly rather than generating a
// level: after the split this package has no idea what a level is, and these
// tests are about pixels, not geometry.
func gridOf(w, h int, edits ...func(g screen.Grid)) screen.Grid {
	g := screen.Grid{Width: w, Height: h, Cells: make([][]screen.Cell, h)}
	for y := range g.Cells {
		g.Cells[y] = make([]screen.Cell, w)
		for x := range g.Cells[y] {
			g.Cells[y][x] = screen.Cell{Ch: ' '}
		}
	}
	for _, e := range edits {
		e(g)
	}
	return g
}

func at(x, y int, c screen.Cell) func(screen.Grid) {
	return func(g screen.Grid) { g.Cells[y][x] = c }
}

// cellHasColor reports whether any pixel in img's (cellX,cellY) cell (of
// size cellSize) exactly matches want. Anti-aliased glyph edges blend
// toward the black background rather than landing on the exact color, so
// callers should use a cellSize generous enough that a stroke's interior
// pixels reach full coverage (32 reliably does; the app's own smaller
// default of 16 doesn't always, which is fine for legibility but not for
// an exact-match assertion).
func cellHasColor(img *image.RGBA, cellX, cellY, cellSize int, want color.RGBA) bool {
	x0, y0 := cellX*cellSize, cellY*cellSize
	for dy := 0; dy < cellSize; dy++ {
		for dx := 0; dx < cellSize; dx++ {
			if img.RGBAAt(x0+dx, y0+dy) == want {
				return true
			}
		}
	}
	return false
}

func TestImageBackgroundIsBlack(t *testing.T) {
	img := Image(gridOf(10, 10), 16)

	if got := img.RGBAAt(0, 0); got.R != 0 || got.G != 0 || got.B != 0 {
		// Every cell is blank — no glyph at all — so (0,0) should be the
		// untouched black background.
		t.Errorf("expected (0,0) to be black background on an empty grid, got %v", got)
	}
}

func TestImageSizeIsGridTimesCellSize(t *testing.T) {
	img := Image(gridOf(10, 6), 16)

	if got, want := img.Bounds().Dx(), 10*16; got != want {
		t.Errorf("width: got %d, want %d", got, want)
	}
	if got, want := img.Bounds().Dy(), 6*16; got != want {
		t.Errorf("height: got %d, want %d", got, want)
	}
}

func TestImagePaintsAGlyphInItsOwnColor(t *testing.T) {
	g := gridOf(10, 10, at(3, 2, screen.Cell{Ch: '#', R: 0x61, G: 0x61, B: 0x61}))

	img := Image(g, 32)

	want := color.RGBA{R: 0x61, G: 0x61, B: 0x61, A: 0xff}
	if !cellHasColor(img, 3, 2, 32, want) {
		t.Errorf("expected some pixel in the (3,2) cell painted %v, found none", want)
	}
}

func TestImagePaintsEachCellInItsOwnColor(t *testing.T) {
	g := gridOf(10, 10,
		at(1, 1, screen.Cell{Ch: '.', R: 0x50, G: 0x50, B: 0x66}),
		at(3, 4, screen.Cell{Ch: '@', R: 255, G: 255, B: 255}),
	)

	img := Image(g, 32)

	floor := color.RGBA{R: 0x50, G: 0x50, B: 0x66, A: 0xff}
	if !cellHasColor(img, 1, 1, 32, floor) {
		t.Errorf("expected the floor cell painted %v, found none", floor)
	}
	player := color.RGBA{R: 255, G: 255, B: 255, A: 0xff}
	if !cellHasColor(img, 3, 4, 32, player) {
		t.Errorf("expected the object cell painted %v, found none", player)
	}
	// The object's color must not have bled into the floor cell.
	if cellHasColor(img, 1, 1, 32, player) {
		t.Error("object color appears in the floor cell; cells are not independently colored")
	}
}

// TestImageDrawsNothingForABlankCell pins the one case where a colored cell
// still paints nothing: a space has no glyph, whatever color it carries.
func TestImageDrawsNothingForABlankCell(t *testing.T) {
	g := gridOf(4, 4, at(1, 1, screen.Cell{Ch: ' ', R: 255, G: 255, B: 255}))

	img := Image(g, 16)

	white := color.RGBA{R: 255, G: 255, B: 255, A: 0xff}
	if cellHasColor(img, 1, 1, 16, white) {
		t.Error("expected a space to paint nothing even when colored")
	}
}

// TestImageToleratesAZeroCellSize guards the clamp: a cell size below 1
// would otherwise produce a zero-width image and a division-free but empty
// canvas, and this is reachable from a client flag.
func TestImageToleratesAZeroCellSize(t *testing.T) {
	img := Image(gridOf(4, 4, at(1, 1, screen.Cell{Ch: '@', R: 255})), 0)

	if img.Bounds().Dx() != 4 || img.Bounds().Dy() != 4 {
		t.Errorf("expected cellSize clamped to 1 giving a 4x4 image, got %v", img.Bounds())
	}
}
