package grid

import (
	"strings"
	"testing"

	"micro/server/internal/mapdata"
	"micro/server/internal/overlay"
)

func TestBuildPaintsObjectsOnTop(t *testing.T) {
	m := &mapdata.Map{
		Width:  10,
		Height: 10,
		Rooms:  []mapdata.Room{{ID: 0, X: 2, Y: 2, W: 5, H: 5}},
	}

	r := m.Rooms[0]
	x, y := r.CenterX(), r.CenterY()

	objects := []overlay.Object{
		{ID: "goblin:1", X: x, Y: y, Glyph: 'g', Color: overlay.Color{G: 200}, Layer: 0},
		{ID: "player:1", X: x, Y: y, Glyph: '@', Color: overlay.Color{R: 255, G: 255, B: 255}, Layer: 1},
	}

	g := Build(m, Options{Objects: objects})

	got := g.Cells[y][x]
	if got.Ch != '@' {
		t.Errorf("expected higher-layer object '@' to win at (%d,%d), got %q", x, y, got.Ch)
	}
	wantColor := overlay.Color{R: 255, G: 255, B: 255}
	if got.Color != wantColor {
		t.Errorf("expected color %v at (%d,%d), got %v", wantColor, x, y, got.Color)
	}
}

func TestBuildSkipsOutOfBoundsObjects(t *testing.T) {
	m := &mapdata.Map{
		Width:  10,
		Height: 10,
		Rooms:  []mapdata.Room{{ID: 0, X: 2, Y: 2, W: 5, H: 5}},
	}

	// Must not panic even though this object is well outside the map.
	objects := []overlay.Object{
		{ID: "ghost:1", X: m.Width + 100, Y: m.Height + 100, Glyph: '?'},
	}
	Build(m, Options{Objects: objects})
}

func TestBuildPaintsLitTilesAsDarkFloorOutsideAnyRoom(t *testing.T) {
	m := &mapdata.Map{Width: 10, Height: 10} // no rooms/corridors at all

	g := Build(m, Options{LitTiles: []mapdata.Point{{X: 3, Y: 4}, {X: 4, Y: 4}}})

	for _, p := range []mapdata.Point{{X: 3, Y: 4}, {X: 4, Y: 4}} {
		got := g.Cells[p.Y][p.X]
		if got.Ch != '.' || got.Color != colorFloorDark {
			t.Errorf("expected LitTiles entry %v painted as dark floor ('.',%v), got (%q,%v)", p, colorFloorDark, got.Ch, got.Color)
		}
	}
	// An untouched cell with no room nearby should stay blank — walls are
	// only drawn around a room's own perimeter, not across the whole map.
	if got := g.Cells[0][0]; got.Ch != ' ' {
		t.Errorf("expected (0,0) to remain blank with no rooms on the map, got %q", got.Ch)
	}
}

func TestBuildPaintsWallsOnlyAroundARoomsPerimeter(t *testing.T) {
	m := &mapdata.Map{
		Width: 10, Height: 10,
		Rooms: []mapdata.Room{{ID: 0, X: 3, Y: 3, W: 2, H: 2}},
	}

	g := Build(m, Options{})

	// Immediately outside the room's rectangle: wall.
	if got := g.Cells[2][3]; got.Ch != '#' || got.Color != colorWall {
		t.Errorf("expected (3,2) just above the room to be wall, got (%q,%v)", got.Ch, got.Color)
	}
	// Two tiles away from the room: still blank, not wall.
	if got := g.Cells[0][0]; got.Ch != ' ' {
		t.Errorf("expected (0,0), far from the room, to remain blank, got %q", got.Ch)
	}
}

func TestBuildLitTilesDrawUnderObjects(t *testing.T) {
	m := &mapdata.Map{Width: 10, Height: 10}

	g := Build(m, Options{
		LitTiles: []mapdata.Point{{X: 3, Y: 4}},
		Objects:  []overlay.Object{{ID: "player:1", X: 3, Y: 4, Glyph: '@', Color: overlay.Color{R: 255, G: 255, B: 255}}},
	})

	got := g.Cells[4][3]
	if got.Ch != '@' {
		t.Errorf("expected the object glyph to draw on top of a lit tile, got %q", got.Ch)
	}
}

// hiddenDoorMap is a two-room map joined by one corridor whose DoorA is
// hidden, for the disguise tests below.
func hiddenDoorMap() *mapdata.Map {
	return &mapdata.Map{
		Width: 12, Height: 6,
		Rooms: []mapdata.Room{
			{ID: 0, X: 1, Y: 2, W: 2, H: 2},
			{ID: 1, X: 8, Y: 2, W: 2, H: 2},
		},
		Corridors: []mapdata.Corridor{{
			ID: 0, RoomA: 0, RoomB: 1,
			DoorA: mapdata.Door{X: 3, Y: 2, Hidden: true},
			DoorB: mapdata.Door{X: 7, Y: 2},
			Path: []mapdata.Point{
				{X: 3, Y: 2}, {X: 4, Y: 2}, {X: 5, Y: 2}, {X: 6, Y: 2}, {X: 7, Y: 2},
			},
		}},
	}
}

// TestBuildDisguisesAnUndiscoveredHiddenDoorAsWall is the core secrecy
// guarantee of rasterizing server-side. Before the split this decision was
// the client's: the whole map, hidden-door flags and all, was handed over
// along with a RevealHidden flag the client was trusted to leave off. Here
// the disguise happens before anything is serialized, so a hidden door must
// be byte-for-byte the wall cell beside it — not merely marked as one.
func TestBuildDisguisesAnUndiscoveredHiddenDoorAsWall(t *testing.T) {
	m := hiddenDoorMap()

	g := Build(m, Options{})

	door := g.Cells[2][3]
	if door.Ch != '#' || door.Color != colorWall {
		t.Errorf("expected the hidden door at (3,2) drawn as wall ('#',%v), got (%q,%v)", colorWall, door.Ch, door.Color)
	}
	// And identical to a wall cell that really is wall, so nothing
	// downstream can tell them apart.
	plain := g.Cells[1][3] // room 0's perimeter, directly above the door
	if door != plain {
		t.Errorf("hidden door cell %v differs from an ordinary wall cell %v; the disguise leaks", door, plain)
	}
}

func TestBuildRevealHiddenShowsTheDoorDistinctly(t *testing.T) {
	m := hiddenDoorMap()

	g := Build(m, Options{RevealHidden: true})

	got := g.Cells[2][3]
	if got.Ch != 'h' || got.Color != RevealColor {
		t.Errorf("expected the revealed hidden door drawn as ('h',%v), got (%q,%v)", RevealColor, got.Ch, got.Color)
	}
}

func TestBuildDrawsAnOrdinaryDoorAsADoor(t *testing.T) {
	m := hiddenDoorMap()

	g := Build(m, Options{})

	got := g.Cells[2][7] // DoorB, not hidden
	if got.Ch != '+' || got.Color != colorDoor {
		t.Errorf("expected the ordinary door drawn as ('+',%v), got (%q,%v)", colorDoor, got.Ch, got.Color)
	}
}

func TestEncodeShapeMatchesGridDimensions(t *testing.T) {
	m := &mapdata.Map{
		Width: 12, Height: 6,
		Rooms: []mapdata.Room{{ID: 0, X: 1, Y: 2, W: 2, H: 2}},
	}

	g := Build(m, Options{})
	rows, colors, palette := g.Encode()

	if len(rows) != 6 {
		t.Fatalf("got %d rows, want 6", len(rows))
	}
	if len(colors) != 6 {
		t.Fatalf("got %d color rows, want 6", len(colors))
	}
	for y := range rows {
		if n := len([]rune(rows[y])); n != 12 {
			t.Errorf("row %d has %d runes, want 12", y, n)
		}
		if n := len(colors[y]); n != 12 {
			t.Errorf("color row %d has %d entries, want 12", y, n)
		}
		for x, i := range colors[y] {
			if i < 0 || i >= len(palette) {
				t.Errorf("cell (%d,%d) palette index %d outside palette of %d", x, y, i, len(palette))
			}
		}
	}
}

// TestEncodeDeduplicatesThePalette guards the reason colors are indices and
// not inline hex: a frame reuses a couple of dozen colors across every cell.
func TestEncodeDeduplicatesThePalette(t *testing.T) {
	m := &mapdata.Map{
		Width: 12, Height: 6,
		Rooms: []mapdata.Room{{ID: 0, X: 1, Y: 2, W: 2, H: 2}},
	}

	g := Build(m, Options{})
	_, _, palette := g.Encode()

	seen := map[string]bool{}
	for _, p := range palette {
		if seen[p] {
			t.Errorf("palette contains %q more than once: %v", p, palette)
		}
		seen[p] = true
		if len(p) != 7 || !strings.HasPrefix(p, "#") {
			t.Errorf("palette entry %q is not #rrggbb", p)
		}
	}
	// Blank, wall, lit floor — nowhere near one entry per cell.
	if len(palette) > 8 {
		t.Errorf("expected a handful of distinct colors for a one-room map, got %d: %v", len(palette), palette)
	}
}

// TestEncodeRowsAreHumanReadable is why the glyph layer is text rather than
// numbers: a frame should be legible straight out of curl.
func TestEncodeRowsAreHumanReadable(t *testing.T) {
	m := &mapdata.Map{
		Width: 6, Height: 5,
		Rooms: []mapdata.Room{{ID: 0, X: 2, Y: 2, W: 2, H: 2}},
	}

	rows, _, _ := Build(m, Options{}).Encode()

	want := []string{
		"      ",
		" #### ",
		" #..# ",
		" #..# ",
		" #### ",
	}
	for y := range want {
		if rows[y] != want[y] {
			t.Errorf("row %d: got %q, want %q", y, rows[y], want[y])
		}
	}
}

func TestEncodeBlankCellIsPaletteIndexZero(t *testing.T) {
	m := &mapdata.Map{Width: 4, Height: 3} // nothing on it at all

	rows, colors, palette := Build(m, Options{}).Encode()

	if palette[0] != "#000000" {
		t.Errorf("expected palette[0] to be the blank cell's black, got %q", palette[0])
	}
	for y := range rows {
		for x := range colors[y] {
			if colors[y][x] != 0 {
				t.Errorf("cell (%d,%d) on an empty map: got palette index %d, want 0", x, y, colors[y][x])
			}
		}
	}
}

// TestWallsInsideARoomAreDrawn: a maze's inner walls show as walls
// wherever its room is drawn whole — lit, or dark on the full-map view —
// and, seen by torchlight, tile by tile.
func TestWallsInsideARoomAreDrawn(t *testing.T) {
	for _, dark := range []bool{false, true} {
		m := &mapdata.Map{Width: 8, Height: 5, Rooms: []mapdata.Room{{ID: 0, X: 1, Y: 1, W: 5, H: 3, Dark: dark, Walled: true}},
			Walls: map[mapdata.Point]bool{{X: 3, Y: 2}: true}}
		if c := Build(m, Options{}).Cells[2][3]; c.Ch != '#' {
			t.Errorf("dark %v: the wall drawn as %q", dark, c.Ch)
		}
	}
	m := &mapdata.Map{Width: 8, Height: 5, Walls: map[mapdata.Point]bool{{X: 3, Y: 2}: true}}
	g := Build(m, Options{LitTiles: []mapdata.Point{{X: 2, Y: 2}, {X: 3, Y: 2}}})
	if g.Cells[2][2].Ch != '.' || g.Cells[2][3].Ch != '#' {
		t.Errorf("by torchlight: %q %q", g.Cells[2][2].Ch, g.Cells[2][3].Ch)
	}
}
