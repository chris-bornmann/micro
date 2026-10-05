package render

import (
	"testing"

	"micro/client/internal/screen"
)

func TestBrightenOneIsIdentity(t *testing.T) {
	c := screen.Cell{Ch: '.', R: 0x3c, G: 0x3c, B: 0x4c}
	g := gridOf(2, 2, at(1, 1, c))

	if got := Brighten(g, 1).At(1, 1); got != c {
		t.Errorf("amount 1 changed the cell: got %+v, want %+v", got, c)
	}
}

func TestBrightenLiftsDarkColorsMoreThanBrightOnes(t *testing.T) {
	g := gridOf(2, 1,
		at(0, 0, screen.Cell{Ch: '.', R: 60, G: 60, B: 60}),
		at(1, 0, screen.Cell{Ch: '@', R: 220, G: 220, B: 220}),
	)

	out := Brighten(g, 1.6)
	dark, bright := out.At(0, 0), out.At(1, 0)

	if dark.R <= 60 || bright.R <= 220 {
		t.Fatalf("expected both colors brighter, got dark %d bright %d", dark.R, bright.R)
	}
	if int(dark.R)-60 <= int(bright.R)-220 {
		t.Errorf("expected the dark color lifted more: dark +%d, bright +%d", int(dark.R)-60, int(bright.R)-220)
	}
	if dark.R >= bright.R {
		t.Errorf("brightening reordered colors: dark %d is no longer below bright %d", dark.R, bright.R)
	}
}

// TestBrightenKeepsTheEndpoints pins black as black (the blank background
// and a blank cell's zero color) and white as white (no clipping past it).
func TestBrightenKeepsTheEndpoints(t *testing.T) {
	g := gridOf(2, 1,
		at(0, 0, screen.Cell{Ch: '#'}),
		at(1, 0, screen.Cell{Ch: '@', R: 255, G: 255, B: 255}),
	)

	out := Brighten(g, 2)

	if c := out.At(0, 0); c.R != 0 || c.G != 0 || c.B != 0 {
		t.Errorf("black moved: got %+v", c)
	}
	if c := out.At(1, 0); c.R != 255 || c.G != 255 || c.B != 255 {
		t.Errorf("white moved: got %+v", c)
	}
}

func TestBrightenDoesNotModifyItsInput(t *testing.T) {
	c := screen.Cell{Ch: '.', R: 60, G: 60, B: 60}
	g := gridOf(1, 1, at(0, 0, c))

	Brighten(g, 1.6)

	if got := g.At(0, 0); got != c {
		t.Errorf("input grid was modified: got %+v, want %+v", got, c)
	}
}
