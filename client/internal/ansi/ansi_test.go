package ansi

import (
	"strings"
	"testing"

	"micro/client/internal/screen"
)

func gridOf(w, h int, edits ...func(screen.Grid)) screen.Grid {
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

func TestRenderEmitsOneLinePerRow(t *testing.T) {
	out := Render(gridOf(3, 4))

	if got := strings.Count(out, "\n"); got != 4 {
		t.Errorf("got %d newlines, want 4", got)
	}
}

// TestRenderLeavesBlanksUncolored keeps a frame's escape-sequence volume
// proportional to what is actually drawn: on a mostly-unexplored level the
// overwhelming majority of cells are blank.
func TestRenderLeavesBlanksUncolored(t *testing.T) {
	out := Render(gridOf(3, 2))

	if strings.Contains(out, "\x1b") {
		t.Errorf("expected no escape sequences for an all-blank grid, got %q", out)
	}
	if out != "   \n   \n" {
		t.Errorf("got %q, want three spaces per row", out)
	}
}

func TestRenderColorsAGlyphAndResetsAfterIt(t *testing.T) {
	out := Render(gridOf(1, 1, at(0, 0, screen.Cell{Ch: '@', R: 255, G: 255, B: 255})))

	// White maps to the top of the 6x6x6 cube: 16 + 36*5 + 6*5 + 5 = 231.
	if want := "\x1b[38;5;231m@\x1b[0m\n"; out != want {
		t.Errorf("got %q, want %q", out, want)
	}
}

// TestRenderResetsPerCell means a truncated or interleaved write can never
// leave the terminal stuck in a color.
func TestRenderResetsPerCell(t *testing.T) {
	out := Render(gridOf(2, 1,
		at(0, 0, screen.Cell{Ch: '#', R: 0x82, G: 0x82, B: 0x82}),
		at(1, 0, screen.Cell{Ch: '@', R: 255, G: 255, B: 255}),
	))

	if got, want := strings.Count(out, "\x1b[0m"), 2; got != want {
		t.Errorf("got %d resets, want %d (one per colored cell)", got, want)
	}
}

func TestTo256MapsTheColorCubeCorners(t *testing.T) {
	cases := []struct {
		name string
		cell screen.Cell
		want int
	}{
		{"black", screen.Cell{}, 16},
		{"white", screen.Cell{R: 255, G: 255, B: 255}, 231},
		{"red", screen.Cell{R: 255}, 16 + 36*5},
		{"green", screen.Cell{G: 255}, 16 + 6*5},
		{"blue", screen.Cell{B: 255}, 16 + 5},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := to256(tc.cell); got != tc.want {
				t.Errorf("got %d, want %d", got, tc.want)
			}
		})
	}
}

func TestRenderTreatsAZeroRuneAsBlank(t *testing.T) {
	g := screen.Grid{Width: 1, Height: 1, Cells: [][]screen.Cell{{{Ch: 0, R: 255}}}}

	if got := Render(g); got != " \n" {
		t.Errorf("got %q, want a single space", got)
	}
}
