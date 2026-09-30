// Package ansi draws a decoded character grid as 8-bit-color terminal text.
//
// Client-side, like package render, and for the same reason: turning a color
// into an escape sequence is a property of the display, not of the game. It
// is a separate package from render so the terminal client need not link an
// embedded font it will never rasterize.
package ansi

import (
	"fmt"
	"strings"

	"micro/client/internal/screen"
)

// Render draws g as one string of ANSI-colored rows, newline-terminated,
// with the color reset after every cell so a truncated or interleaved write
// cannot leave the terminal in a colored state.
func Render(g screen.Grid) string {
	var b strings.Builder
	// Rough preallocation: each cell costs ~15 bytes of escape sequence.
	b.Grow(g.Width*g.Height*15 + g.Height)

	for y := 0; y < g.Height; y++ {
		for x := 0; x < g.Width; x++ {
			c := g.At(x, y)
			ch := c.Ch
			if ch == 0 {
				ch = ' '
			}
			if ch == ' ' {
				b.WriteByte(' ') // nothing to color
				continue
			}
			fmt.Fprintf(&b, "\x1b[38;5;%dm%c\x1b[0m", to256(c), ch)
		}
		b.WriteByte('\n')
	}

	return b.String()
}

// to256 maps a cell's RGB to the nearest xterm 256-color palette index,
// using the standard 6x6x6 color cube (indices 16-231).
func to256(c screen.Cell) int {
	to6 := func(v uint8) int { return int(v) * 5 / 255 }
	return 16 + 36*to6(c.R) + 6*to6(c.G) + to6(c.B)
}
