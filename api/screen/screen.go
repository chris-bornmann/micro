// Package screen is the client's side of the character grid: decoding what
// the server sent into something paintable.
//
// It is deliberately the client's own type set, not a copy of the server's.
// The two sides share no code — they agree on a JSON shape, documented in
// PROTOCOL.md, and each declares what it needs of it. So this package holds
// only what a renderer reads: a character, a color, and the dimensions. It
// has no imports.
package screen

import (
	"fmt"
	"strconv"
)

// Cell is one character and the color to draw it in. A space (or a zero Ch)
// means nothing is there and nothing should be drawn.
type Cell struct {
	Ch      rune
	R, G, B uint8
}

// Grid is one decoded frame, Height rows of Width cells.
type Grid struct {
	Width, Height int
	Cells         [][]Cell
}

// At returns the cell at (x,y), or a blank cell if it is out of bounds, so
// a renderer can walk whatever region it likes without bounds-checking.
func (g Grid) At(x, y int) Cell {
	if y < 0 || y >= len(g.Cells) || x < 0 || x >= len(g.Cells[y]) {
		return Cell{Ch: ' '}
	}
	return g.Cells[y][x]
}

// Decode turns a frame's wire form into a Grid.
//
// rows carries the glyphs (one string per row), colors a palette index per
// cell, and palette the "#rrggbb" values those index into. Every input is
// treated as untrusted: a short row, a row of the wrong length, an index
// outside the palette or a malformed color is an error rather than a panic
// or a silently mangled screen, because this is parsing a network response.
//
// Rows are decoded as runes, not bytes, so a multi-byte glyph occupies one
// cell rather than shifting everything after it.
func Decode(width, height int, rows []string, colors [][]int, palette []string) (Grid, error) {
	if width < 0 || height < 0 {
		return Grid{}, fmt.Errorf("screen: negative dimensions %dx%d", width, height)
	}
	if len(rows) != height {
		return Grid{}, fmt.Errorf("screen: got %d rows, want %d", len(rows), height)
	}
	if len(colors) != height {
		return Grid{}, fmt.Errorf("screen: got %d color rows, want %d", len(colors), height)
	}

	rgb := make([][3]uint8, len(palette))
	for i, p := range palette {
		c, err := parseHex(p)
		if err != nil {
			return Grid{}, fmt.Errorf("screen: palette[%d]: %w", i, err)
		}
		rgb[i] = c
	}

	g := Grid{Width: width, Height: height, Cells: make([][]Cell, height)}
	for y := 0; y < height; y++ {
		runes := []rune(rows[y])
		if len(runes) != width {
			return Grid{}, fmt.Errorf("screen: row %d has %d cells, want %d", y, len(runes), width)
		}
		if len(colors[y]) != width {
			return Grid{}, fmt.Errorf("screen: color row %d has %d cells, want %d", y, len(colors[y]), width)
		}
		g.Cells[y] = make([]Cell, width)
		for x := 0; x < width; x++ {
			i := colors[y][x]
			if i < 0 || i >= len(rgb) {
				return Grid{}, fmt.Errorf("screen: cell (%d,%d): palette index %d out of range (%d entries)", x, y, i, len(rgb))
			}
			g.Cells[y][x] = Cell{Ch: runes[x], R: rgb[i][0], G: rgb[i][1], B: rgb[i][2]}
		}
	}

	return g, nil
}

// parseHex parses "#rrggbb".
func parseHex(s string) ([3]uint8, error) {
	if len(s) != 7 || s[0] != '#' {
		return [3]uint8{}, fmt.Errorf("malformed color %q, want #rrggbb", s)
	}
	var out [3]uint8
	for i := 0; i < 3; i++ {
		v, err := strconv.ParseUint(s[1+i*2:3+i*2], 16, 8)
		if err != nil {
			return [3]uint8{}, fmt.Errorf("malformed color %q: %w", s, err)
		}
		out[i] = uint8(v)
	}
	return out, nil
}
