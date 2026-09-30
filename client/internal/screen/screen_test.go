package screen

import "testing"

// Decode parses a network response, so every test here feeds it wire data
// written out by hand rather than produced by the server. A round-trip
// against the real encoder would be a better test of agreement but a worse
// test of this package: after the split the encoder is on the other side of
// the boundary, and this code has to be correct about bytes it did not
// create.

func TestDecodeBuildsTheGrid(t *testing.T) {
	g, err := Decode(3, 2,
		[]string{"#.@", "  +"},
		[][]int{{0, 1, 2}, {3, 3, 1}},
		[]string{"#828282", "#e8dfa4", "#ffffff", "#000000"},
	)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}

	if g.Width != 3 || g.Height != 2 {
		t.Errorf("got %dx%d, want 3x2", g.Width, g.Height)
	}
	if got := g.At(0, 0); got.Ch != '#' || got.R != 0x82 || got.G != 0x82 || got.B != 0x82 {
		t.Errorf("(0,0): got %v, want '#' in 828282", got)
	}
	if got := g.At(2, 0); got.Ch != '@' || got.R != 255 || got.G != 255 || got.B != 255 {
		t.Errorf("(2,0): got %v, want '@' in white", got)
	}
	if got := g.At(2, 1); got.Ch != '+' || got.G != 0xdf {
		t.Errorf("(2,1): got %v, want '+' in e8dfa4", got)
	}
}

// TestDecodeCountsRunesNotBytes is why rows are decoded as []rune: a
// multi-byte glyph must occupy one cell, not shift every cell after it.
func TestDecodeCountsRunesNotBytes(t *testing.T) {
	g, err := Decode(3, 1,
		[]string{"aéb"}, // 'é' is two bytes, one rune
		[][]int{{0, 0, 0}},
		[]string{"#ffffff"},
	)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got := g.At(1, 0).Ch; got != 'é' {
		t.Errorf("(1,0): got %q, want 'é'", got)
	}
	if got := g.At(2, 0).Ch; got != 'b' {
		t.Errorf("(2,0): got %q, want 'b' — a multi-byte glyph shifted the row", got)
	}
}

func TestAtIsSafeOutOfBounds(t *testing.T) {
	g, err := Decode(1, 1, []string{"#"}, [][]int{{0}}, []string{"#ffffff"})
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}

	for _, p := range [][2]int{{-1, 0}, {0, -1}, {1, 0}, {0, 1}, {99, 99}} {
		if got := g.At(p[0], p[1]); got.Ch != ' ' {
			t.Errorf("At(%d,%d): got %q, want a blank cell", p[0], p[1], got.Ch)
		}
	}
}

func TestDecodeRejectsMalformedInput(t *testing.T) {
	cases := []struct {
		name    string
		w, h    int
		rows    []string
		colors  [][]int
		palette []string
	}{
		{"too few rows", 2, 2, []string{"ab"}, [][]int{{0, 0}, {0, 0}}, []string{"#ffffff"}},
		{"too few color rows", 2, 2, []string{"ab", "cd"}, [][]int{{0, 0}}, []string{"#ffffff"}},
		{"short row", 3, 1, []string{"ab"}, [][]int{{0, 0, 0}}, []string{"#ffffff"}},
		{"long row", 2, 1, []string{"abc"}, [][]int{{0, 0}}, []string{"#ffffff"}},
		{"short color row", 2, 1, []string{"ab"}, [][]int{{0}}, []string{"#ffffff"}},
		{"index past palette", 1, 1, []string{"a"}, [][]int{{5}}, []string{"#ffffff"}},
		{"negative index", 1, 1, []string{"a"}, [][]int{{-1}}, []string{"#ffffff"}},
		{"empty palette", 1, 1, []string{"a"}, [][]int{{0}}, nil},
		{"color missing hash", 1, 1, []string{"a"}, [][]int{{0}}, []string{"ffffff"}},
		{"color too short", 1, 1, []string{"a"}, [][]int{{0}}, []string{"#fff"}},
		{"color not hex", 1, 1, []string{"a"}, [][]int{{0}}, []string{"#gggggg"}},
		{"negative height", 1, -1, nil, nil, []string{"#ffffff"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Decode(tc.w, tc.h, tc.rows, tc.colors, tc.palette); err == nil {
				t.Error("expected an error, got nil")
			}
		})
	}
}

func TestDecodeAcceptsAnEmptyGrid(t *testing.T) {
	g, err := Decode(0, 0, nil, nil, nil)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if g.Width != 0 || g.Height != 0 {
		t.Errorf("got %dx%d, want 0x0", g.Width, g.Height)
	}
}
