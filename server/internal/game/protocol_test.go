package game

import (
	"fmt"
	"regexp"
	"testing"
)

// The server must not import the client to test itself. It did briefly, by
// calling the client's decoder to prove a frame was decodable, and separating
// the modules caught it — which is the same inverted test dependency the
// mapdata and render packages had before the split.
//
// Asserting conformance to PROTOCOL.md directly is the better test anyway: it
// checks the frame against the document every client is written from, rather
// than against the behaviour of one particular client that happens to exist.

var hexColor = regexp.MustCompile(`^#[0-9a-f]{6}$`)

// assertFrameConformsToProtocol checks every structural invariant PROTOCOL.md
// states about a frame's grid. A client is entitled to rely on all of these,
// and its own decoder rejects a frame that breaks any.
func assertFrameConformsToProtocol(t *testing.T, f Frame) {
	t.Helper()

	if f.Width < 0 || f.Height < 0 {
		t.Errorf("negative dimensions %dx%d", f.Width, f.Height)
		return
	}
	if len(f.Rows) != f.Height {
		t.Errorf("got %d rows, want Height=%d", len(f.Rows), f.Height)
	}
	if len(f.Colors) != f.Height {
		t.Errorf("got %d color rows, want Height=%d", len(f.Colors), f.Height)
	}

	for i, c := range f.Palette {
		if !hexColor.MatchString(c) {
			t.Errorf("palette[%d] = %q, want #rrggbb", i, c)
		}
	}

	for y := 0; y < len(f.Rows) && y < len(f.Colors); y++ {
		// Runes, not bytes: a multi-byte glyph must occupy one cell.
		if n := len([]rune(f.Rows[y])); n != f.Width {
			t.Errorf("row %d has %d runes, want Width=%d", y, n, f.Width)
		}
		if n := len(f.Colors[y]); n != f.Width {
			t.Errorf("color row %d has %d entries, want Width=%d", y, n, f.Width)
		}
		for x, idx := range f.Colors[y] {
			if idx < 0 || idx >= len(f.Palette) {
				t.Errorf("cell (%d,%d): palette index %d outside a palette of %d",
					x, y, idx, len(f.Palette))
			}
		}
	}
}

// glyphAt returns the character at (x,y), or an error if it is out of bounds.
func glyphAt(f Frame, x, y int) (rune, error) {
	if y < 0 || y >= len(f.Rows) {
		return 0, fmt.Errorf("row %d out of range", y)
	}
	runes := []rune(f.Rows[y])
	if x < 0 || x >= len(runes) {
		return 0, fmt.Errorf("column %d out of range", x)
	}
	return runes[x], nil
}
