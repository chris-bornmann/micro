package render

import (
	"image"
	"image/color"
	"testing"

	"map_api/mapdata"
	"map_api/mapgen"
	"map_api/overlay"
)

func TestGridPaintsObjectsOnTop(t *testing.T) {
	cfg := mapgen.DefaultConfig()
	cfg.Seed = 99
	cfg.NumRooms = 3
	m, err := mapgen.Generate(cfg)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(m.Rooms) == 0 {
		t.Fatal("expected at least one room")
	}

	r := m.Rooms[0]
	x, y := r.CenterX(), r.CenterY()

	objects := []overlay.Object{
		{ID: "goblin:1", X: x, Y: y, Glyph: 'g', Color: overlay.Color{G: 200}, Layer: 0},
		{ID: "player:1", X: x, Y: y, Glyph: '@', Color: overlay.Color{R: 255, G: 255, B: 255}, Layer: 1},
	}

	g := grid(m, Options{Objects: objects})

	got := g[y][x]
	if got.ch != '@' {
		t.Errorf("expected higher-layer object '@' to win at (%d,%d), got %q", x, y, got.ch)
	}
	wantColor := overlay.Color{R: 255, G: 255, B: 255}
	if got.color != wantColor {
		t.Errorf("expected color %v at (%d,%d), got %v", wantColor, x, y, got.color)
	}
}

func TestGridSkipsOutOfBoundsObjects(t *testing.T) {
	cfg := mapgen.DefaultConfig()
	cfg.Seed = 7
	cfg.NumRooms = 2
	m, err := mapgen.Generate(cfg)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	// Must not panic even though this object is well outside the map.
	objects := []overlay.Object{
		{ID: "ghost:1", X: m.Width + 100, Y: m.Height + 100, Glyph: '?'},
	}
	grid(m, Options{Objects: objects})
}

func TestGridPaintsLitTilesAsDarkFloorOutsideAnyRoom(t *testing.T) {
	m := &mapdata.Map{Width: 10, Height: 10} // no rooms/corridors at all

	g := grid(m, Options{LitTiles: []mapdata.Point{{X: 3, Y: 4}, {X: 4, Y: 4}}})

	for _, p := range []mapdata.Point{{X: 3, Y: 4}, {X: 4, Y: 4}} {
		got := g[p.Y][p.X]
		if got.ch != '.' || got.color != colorFloorDark {
			t.Errorf("expected LitTiles entry %v painted as dark floor ('.',%v), got (%q,%v)", p, colorFloorDark, got.ch, got.color)
		}
	}
	// An untouched cell with no room nearby should stay blank — walls are
	// only drawn around a room's own perimeter, not across the whole map.
	if got := g[0][0]; got.ch != ' ' {
		t.Errorf("expected (0,0) to remain blank with no rooms on the map, got %q", got.ch)
	}
}

func TestGridPaintsWallsOnlyAroundARoomsPerimeter(t *testing.T) {
	m := &mapdata.Map{
		Width: 10, Height: 10,
		Rooms: []mapdata.Room{{ID: 0, X: 3, Y: 3, W: 2, H: 2}},
	}

	g := grid(m, Options{})

	// Immediately outside the room's rectangle: wall.
	if got := g[2][3]; got.ch != '#' || got.color != colorWall {
		t.Errorf("expected (3,2) just above the room to be wall, got (%q,%v)", got.ch, got.color)
	}
	// Two tiles away from the room: still blank, not wall.
	if got := g[0][0]; got.ch != ' ' {
		t.Errorf("expected (0,0), far from the room, to remain blank, got %q", got.ch)
	}
}

func TestGridLitTilesDrawUnderObjects(t *testing.T) {
	m := &mapdata.Map{Width: 10, Height: 10}

	g := grid(m, Options{
		LitTiles: []mapdata.Point{{X: 3, Y: 4}},
		Objects:  []overlay.Object{{ID: "player:1", X: 3, Y: 4, Glyph: '@', Color: overlay.Color{R: 255, G: 255, B: 255}}},
	})

	got := g[4][3]
	if got.ch != '@' {
		t.Errorf("expected the object glyph to draw on top of a lit tile, got %q", got.ch)
	}
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
	m := &mapdata.Map{Width: 10, Height: 10}

	img := Image(m, Options{}, 16)

	if got := img.RGBAAt(0, 0); got == (color.RGBA{}) || got.R != 0 || got.G != 0 || got.B != 0 {
		// With no rooms on the map, (0,0) is a blank cell — no glyph at
		// all — so it should be the untouched black background.
		t.Errorf("expected (0,0) to be black background with no rooms on the map, got %v", got)
	}
}

func TestImagePaintsWallGlyphInWallColor(t *testing.T) {
	m := &mapdata.Map{
		Width: 10, Height: 10,
		Rooms: []mapdata.Room{{ID: 0, X: 3, Y: 3, W: 2, H: 2}},
	}

	img := Image(m, Options{}, 32)

	want := color.RGBA{R: colorWall.R, G: colorWall.G, B: colorWall.B, A: 0xff}
	if !cellHasColor(img, 3, 2, 32, want) { // just above the room
		t.Errorf("expected some pixel in the (3,2) wall cell painted %v, found none", want)
	}
}

func TestImagePaintsLitTilesAsDarkFloorOutsideAnyRoom(t *testing.T) {
	m := &mapdata.Map{Width: 10, Height: 10}

	img := Image(m, Options{LitTiles: []mapdata.Point{{X: 3, Y: 4}}}, 16)

	want := color.RGBA{R: colorFloorDark.R, G: colorFloorDark.G, B: colorFloorDark.B, A: 0xff}
	if !cellHasColor(img, 3, 4, 16, want) {
		t.Errorf("expected some pixel in the LitTiles cell painted as dark floor %v, found none", want)
	}
}

func TestImagePaintsObjectGlyphInObjectColor(t *testing.T) {
	m := &mapdata.Map{Width: 10, Height: 10}

	img := Image(m, Options{
		Objects: []overlay.Object{{ID: "player:1", X: 3, Y: 4, Glyph: '@', Color: overlay.Color{R: 255, G: 255, B: 255}}},
	}, 32)

	want := color.RGBA{R: 255, G: 255, B: 255, A: 0xff}
	if !cellHasColor(img, 3, 4, 32, want) {
		t.Errorf("expected some pixel in the object's cell painted %v, found none", want)
	}
}
