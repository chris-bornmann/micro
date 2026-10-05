package render

import (
	"math"

	"micro/client/internal/screen"
)

// Brighten returns a copy of g with every cell's color lifted by a gamma
// curve: each channel v becomes 255 * (v/255)^(1/amount). amount 1 leaves
// colors exactly as the server sent them; larger values brighten.
//
// A curve rather than a flat multiplier because the colors that are hard to
// see are the dark ones — terrain is deliberately dimmed server-side, and
// remembered floor sits only a little above black. A curve lifts those a lot
// and bright colors barely at all, so nothing clips to white, black stays
// black, and the relative order of colors (lit floor brighter than
// remembered floor) survives. PROTOCOL.md permits exactly this kind of
// restyling: a client may substitute colors, it just cannot learn what they
// mean.
func Brighten(g screen.Grid, amount float64) screen.Grid {
	if amount <= 0 || amount == 1 {
		return g
	}

	var lut [256]uint8
	for v := range lut {
		lut[v] = uint8(math.Round(255 * math.Pow(float64(v)/255, 1/amount)))
	}

	out := screen.Grid{Width: g.Width, Height: g.Height, Cells: make([][]screen.Cell, len(g.Cells))}
	for y, row := range g.Cells {
		out.Cells[y] = make([]screen.Cell, len(row))
		for x, c := range row {
			out.Cells[y][x] = screen.Cell{Ch: c.Ch, R: lut[c.R], G: lut[c.G], B: lut[c.B]}
		}
	}
	return out
}
