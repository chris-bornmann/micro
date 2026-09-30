package vision

import (
	"testing"

	"micro/server/internal/mapdata"
)

// visionTestMap: one Dark room (interior x:5-9, y:5-9) with a corridor
// exiting its east wall, connected to a lit room (interior x:16-20).
func visionTestMap() mapdata.Map {
	darkRoom := mapdata.Room{ID: 0, X: 5, Y: 5, W: 5, H: 5, Dark: true}
	litRoom := mapdata.Room{ID: 1, X: 16, Y: 5, W: 5, H: 5}
	corridor := mapdata.Corridor{
		ID: 0, RoomA: 0, RoomB: 1,
		DoorA: mapdata.Door{X: 10, Y: 7},
		DoorB: mapdata.Door{X: 15, Y: 7},
		Path: []mapdata.Point{
			{X: 10, Y: 7}, {X: 11, Y: 7}, {X: 12, Y: 7}, {X: 13, Y: 7}, {X: 14, Y: 7}, {X: 15, Y: 7},
		},
	}
	return mapdata.Map{
		Width: 30, Height: 20,
		Rooms:     []mapdata.Room{darkRoom, litRoom},
		Corridors: []mapdata.Corridor{corridor},
	}
}

func TestCurrentlyLitFalseInLitRoom(t *testing.T) {
	m := visionTestMap()

	tiles, _, ok := CurrentlyLit(m, 18, 7, false) // inside the lit room
	if ok {
		t.Errorf("expected ok=false in a lit room, got tiles=%v", tiles)
	}
}

func TestCurrentlyLitFalseInCorridor(t *testing.T) {
	m := visionTestMap()

	tiles, _, ok := CurrentlyLit(m, 12, 7, false) // mid-corridor
	if ok {
		t.Errorf("expected ok=false in a corridor, got tiles=%v", tiles)
	}
}

func TestCurrentlyLitFalseInEmptySpace(t *testing.T) {
	m := visionTestMap()

	if _, _, ok := CurrentlyLit(m, 0, 0, false); ok {
		t.Error("expected ok=false in unrecognized empty space")
	}
}

func TestCurrentlyLitReturnsNineTilesInRoomInterior(t *testing.T) {
	m := visionTestMap()

	tiles, room, ok := CurrentlyLit(m, 7, 7, false) // well inside the dark room
	if !ok {
		t.Fatal("expected ok=true inside a dark room")
	}
	if room.ID != 0 {
		t.Errorf("expected room ID 0, got %d", room.ID)
	}
	if len(tiles) != 9 {
		t.Fatalf("expected 9 lit tiles, got %d: %v", len(tiles), tiles)
	}
	want := map[mapdata.Point]bool{}
	for dy := -1; dy <= 1; dy++ {
		for dx := -1; dx <= 1; dx++ {
			want[mapdata.Point{X: 7 + dx, Y: 7 + dy}] = true
		}
	}
	for _, p := range tiles {
		if !want[p] {
			t.Errorf("unexpected lit tile %v", p)
		}
	}
}

func TestCurrentlyLitExcludesTilesOutsideTheDarkRoom(t *testing.T) {
	m := visionTestMap()

	// (9,7) is the dark room's easternmost interior column (room spans
	// x:5-9); its east neighbor (10,7) is the corridor door, not part of
	// the room, and should never be reported as a lit floor tile.
	tiles, _, ok := CurrentlyLit(m, 9, 7, false)
	if !ok {
		t.Fatal("expected ok=true at (9,7), still inside the dark room")
	}
	for _, p := range tiles {
		if p.X == 10 {
			t.Errorf("expected the corridor door at x=10 excluded from lit tiles, got %v in %v", p, tiles)
		}
	}
	// Also confirm every returned tile is still actually inside room 0.
	for _, p := range tiles {
		r, ok := m.RoomAt(p.X, p.Y)
		if !ok || r.ID != 0 {
			t.Errorf("expected lit tile %v to belong to room 0, got room=%+v ok=%v", p, r, ok)
		}
	}
}

func TestCurrentlyLitAtRoomCornerReturnsFewerTiles(t *testing.T) {
	m := visionTestMap()

	// (5,5) is the dark room's northwest interior corner; its 8
	// neighbors mostly fall outside the room (wall/empty space), so only
	// the tiles still inside room 0 should come back.
	tiles, _, ok := CurrentlyLit(m, 5, 5, false)
	if !ok {
		t.Fatal("expected ok=true at the room's corner")
	}
	if len(tiles) >= 9 {
		t.Errorf("expected fewer than 9 tiles at a corner, got %d: %v", len(tiles), tiles)
	}
	for _, p := range tiles {
		if p.X < 5 || p.X >= 10 || p.Y < 5 || p.Y >= 10 {
			t.Errorf("expected every lit tile inside the room's bounds, got %v", p)
		}
	}
}

func TestCurrentlyLitWithTorchReturnsEveryRoomTile(t *testing.T) {
	m := visionTestMap()

	// Even standing at the room's corner (which only lights a handful of
	// tiles at radius 1), a lit torch should reveal the whole 5x5 room.
	tiles, room, ok := CurrentlyLit(m, 5, 5, true)
	if !ok {
		t.Fatal("expected ok=true inside a dark room with a torch lit")
	}
	if room.ID != 0 {
		t.Errorf("expected room ID 0, got %d", room.ID)
	}
	if len(tiles) != 25 {
		t.Fatalf("expected all 25 tiles of the 5x5 dark room, got %d: %v", len(tiles), tiles)
	}
	for _, p := range tiles {
		if p.X < 5 || p.X >= 10 || p.Y < 5 || p.Y >= 10 {
			t.Errorf("expected every lit tile inside the room's bounds, got %v", p)
		}
	}
}

func TestCurrentlyLitTorchLitButNotInDarkRoomStillFalse(t *testing.T) {
	m := visionTestMap()

	if _, _, ok := CurrentlyLit(m, 18, 7, true); ok {
		t.Error("expected ok=false in a lit room even with a torch lit — nothing to restrict, so nothing to reveal")
	}
	if _, _, ok := CurrentlyLit(m, 12, 7, true); ok {
		t.Error("expected ok=false in a corridor even with a torch lit")
	}
}

func TestCurrentlyVisibleReturnsWholeRoomInALitRoom(t *testing.T) {
	m := visionTestMap()

	tiles := CurrentlyVisible(m, 18, 7, false) // inside the lit room (interior 16-20, 5-9)
	want := mapdata.Room{ID: 1, X: 16, Y: 5, W: 5, H: 5}.Tiles()
	if len(tiles) != len(want) {
		t.Fatalf("expected the whole 5x5 lit room (%d tiles), got %d: %v", len(want), len(tiles), tiles)
	}
	wantSet := map[mapdata.Point]bool{}
	for _, p := range want {
		wantSet[p] = true
	}
	for _, p := range tiles {
		if !wantSet[p] {
			t.Errorf("unexpected visible tile %v outside the lit room", p)
		}
	}
}

func TestCurrentlyVisibleReturnsWholeCorridorPath(t *testing.T) {
	m := visionTestMap()

	tiles := CurrentlyVisible(m, 12, 7, false) // mid-corridor
	want := []mapdata.Point{
		{X: 10, Y: 7}, {X: 11, Y: 7}, {X: 12, Y: 7}, {X: 13, Y: 7}, {X: 14, Y: 7}, {X: 15, Y: 7},
	}
	if len(tiles) != len(want) {
		t.Fatalf("expected the whole corridor path (%d tiles), got %d: %v", len(want), len(tiles), tiles)
	}
	wantSet := map[mapdata.Point]bool{}
	for _, p := range want {
		wantSet[p] = true
	}
	for _, p := range tiles {
		if !wantSet[p] {
			t.Errorf("unexpected visible tile %v outside the corridor path", p)
		}
	}
}

func TestCurrentlyVisibleShowsConnectedRoomFromItsDoorThreshold(t *testing.T) {
	m := visionTestMap()

	// (15,7) is DoorB, the threshold into the lit room — not itself
	// inside the room's rectangle (16-20), so RoomAt alone would miss it.
	// Standing exactly there should still reveal the whole room, plus the
	// corridor itself, matching Visibility.reveal's own threshold rule.
	tiles := CurrentlyVisible(m, 15, 7, false)
	want := map[mapdata.Point]bool{}
	for _, p := range (mapdata.Room{ID: 1, X: 16, Y: 5, W: 5, H: 5}).Tiles() {
		want[p] = true
	}
	for _, p := range []mapdata.Point{{X: 10, Y: 7}, {X: 11, Y: 7}, {X: 12, Y: 7}, {X: 13, Y: 7}, {X: 14, Y: 7}, {X: 15, Y: 7}} {
		want[p] = true
	}
	if len(tiles) != len(want) {
		t.Fatalf("expected the connected room plus the corridor (%d tiles), got %d: %v", len(want), len(tiles), tiles)
	}
	for _, p := range tiles {
		if !want[p] {
			t.Errorf("unexpected visible tile %v", p)
		}
	}
}

func TestCurrentlyVisibleFromDarkRoomThresholdExcludesTheDarkRoomInterior(t *testing.T) {
	m := visionTestMap()

	// (10,7) is DoorA, the threshold into the DARK room — a room's own
	// threshold reveals it wholesale everywhere else, but darkness is the
	// one exception: never revealed wholesale, even from its own door.
	tiles := CurrentlyVisible(m, 10, 7, false)
	want := map[mapdata.Point]bool{
		{X: 10, Y: 7}: true, {X: 11, Y: 7}: true, {X: 12, Y: 7}: true,
		{X: 13, Y: 7}: true, {X: 14, Y: 7}: true, {X: 15, Y: 7}: true,
	}
	if len(tiles) != len(want) {
		t.Fatalf("expected only the corridor path (%d tiles), got %d: %v", len(want), len(tiles), tiles)
	}
	for _, p := range tiles {
		if !want[p] {
			t.Errorf("unexpected visible tile %v — dark room interior should stay hidden from its own threshold", p)
		}
	}
}

func TestCurrentlyVisibleDefersToCurrentlyLitInADarkRoom(t *testing.T) {
	m := visionTestMap()

	tiles := CurrentlyVisible(m, 7, 7, false) // well inside the dark room
	wantTiles, _, ok := CurrentlyLit(m, 7, 7, false)
	if !ok {
		t.Fatal("expected CurrentlyLit ok=true inside the dark room")
	}
	if len(tiles) != len(wantTiles) {
		t.Fatalf("expected CurrentlyVisible to defer to CurrentlyLit's radius-limited set, got %d tiles, want %d", len(tiles), len(wantTiles))
	}

	// With a torch lit, CurrentlyLit lifts the radius restriction to the
	// whole room — CurrentlyVisible should follow suit.
	tilesTorch := CurrentlyVisible(m, 5, 5, true)
	if len(tilesTorch) != 25 {
		t.Fatalf("expected all 25 tiles of the dark room with a torch lit, got %d: %v", len(tilesTorch), tilesTorch)
	}
}

func TestCurrentlyVisibleFallsBackToJustThePointInUnrecognizedSpace(t *testing.T) {
	m := visionTestMap()

	tiles := CurrentlyVisible(m, 0, 0, false)
	if len(tiles) != 1 || tiles[0] != (mapdata.Point{X: 0, Y: 0}) {
		t.Errorf("expected just the single point (0,0) in unrecognized space, got %v", tiles)
	}
}
