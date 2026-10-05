package mapdata

import "testing"

// geometryTestMap mirrors the hand-built map used in the player package's
// tests: a 5x5 room (interior x:5-9, y:5-9) with a corridor exiting its
// east wall at (10,7) and running to (12,7).
func geometryTestMap() Map {
	room := Room{ID: 0, X: 5, Y: 5, W: 5, H: 5}
	corridor := Corridor{
		ID:    0,
		RoomA: 0,
		RoomB: 1,
		DoorA: Door{X: 10, Y: 7},
		DoorB: Door{X: 12, Y: 7},
		Path: []Point{
			{X: 10, Y: 7}, {X: 11, Y: 7}, {X: 12, Y: 7},
		},
	}
	return Map{Width: 20, Height: 20, Rooms: []Room{room}, Corridors: []Corridor{corridor}}
}

func TestRoomAt(t *testing.T) {
	m := geometryTestMap()

	if r, ok := m.RoomAt(7, 7); !ok || r.ID != 0 {
		t.Errorf("RoomAt(7,7) = %+v, %v; want room 0, true", r, ok)
	}
	if _, ok := m.RoomAt(10, 7); ok {
		t.Error("RoomAt(10,7) should be false (that's a corridor/door point, not inside the room)")
	}
	if _, ok := m.RoomAt(0, 0); ok {
		t.Error("RoomAt(0,0) should be false (empty space)")
	}
}

func TestRoomsAt(t *testing.T) {
	m := geometryTestMap()

	if rooms := m.RoomsAt(7, 7); !rooms[0] || len(rooms) != 1 {
		t.Errorf("RoomsAt(7,7) = %v, want just room 0 (inside its rectangle)", rooms)
	}
	if rooms := m.RoomsAt(10, 7); !rooms[0] || len(rooms) != 1 {
		t.Errorf("RoomsAt(10,7) = %v, want just room 0 (its door threshold, an open doorway)", rooms)
	}
	if rooms := m.RoomsAt(12, 7); !rooms[1] || len(rooms) != 1 {
		t.Errorf("RoomsAt(12,7) = %v, want just room 1 (the far door's threshold)", rooms)
	}
	if rooms := m.RoomsAt(11, 7); len(rooms) != 0 {
		t.Errorf("RoomsAt(11,7) = %v, want empty (mid-corridor, not a room or a threshold)", rooms)
	}
	if rooms := m.RoomsAt(0, 0); len(rooms) != 0 {
		t.Errorf("RoomsAt(0,0) = %v, want empty (empty space)", rooms)
	}
}

func TestCorridorAt(t *testing.T) {
	m := geometryTestMap()

	if c, ok := m.CorridorAt(11, 7); !ok || c.ID != 0 {
		t.Errorf("CorridorAt(11,7) = %+v, %v; want corridor 0, true", c, ok)
	}
	if c, ok := m.CorridorAt(10, 7); !ok || c.ID != 0 {
		t.Errorf("CorridorAt(10,7) (a door endpoint) = %+v, %v; want corridor 0, true", c, ok)
	}
	if _, ok := m.CorridorAt(7, 7); ok {
		t.Error("CorridorAt(7,7) should be false (that's inside the room, not the corridor)")
	}
}

func TestTilesWithinRadiusReturnsMooreNeighborhoodPlusCenter(t *testing.T) {
	m := geometryTestMap()

	tiles := m.TilesWithinRadius(7, 7, 1)
	if len(tiles) != 9 {
		t.Fatalf("expected 9 tiles (3x3 block), got %d: %v", len(tiles), tiles)
	}

	want := map[Point]bool{}
	for dy := -1; dy <= 1; dy++ {
		for dx := -1; dx <= 1; dx++ {
			want[Point{X: 7 + dx, Y: 7 + dy}] = true
		}
	}
	for _, p := range tiles {
		if !want[p] {
			t.Errorf("unexpected tile %v in radius-1 result", p)
		}
		delete(want, p)
	}
	if len(want) != 0 {
		t.Errorf("missing expected tiles: %v", want)
	}
}

func TestTilesWithinRadiusZeroReturnsOnlyCenter(t *testing.T) {
	m := geometryTestMap()

	tiles := m.TilesWithinRadius(7, 7, 0)
	if len(tiles) != 1 || tiles[0] != (Point{X: 7, Y: 7}) {
		t.Errorf("expected exactly [(7,7)], got %v", tiles)
	}
}

func TestTilesWithinRadiusTwoReturnsFiveByFiveBlock(t *testing.T) {
	m := geometryTestMap()

	tiles := m.TilesWithinRadius(7, 7, 2)
	if len(tiles) != 25 {
		t.Errorf("expected 25 tiles (5x5 block), got %d", len(tiles))
	}
}

func TestRoomTilesReturnsEveryInteriorTile(t *testing.T) {
	r := Room{ID: 0, X: 5, Y: 5, W: 3, H: 2}

	tiles := r.Tiles()
	want := map[Point]bool{
		{X: 5, Y: 5}: true, {X: 6, Y: 5}: true, {X: 7, Y: 5}: true,
		{X: 5, Y: 6}: true, {X: 6, Y: 6}: true, {X: 7, Y: 6}: true,
	}
	if len(tiles) != len(want) {
		t.Fatalf("expected %d tiles, got %d: %v", len(want), len(tiles), tiles)
	}
	for _, p := range tiles {
		if !want[p] {
			t.Errorf("unexpected tile %v", p)
		}
		delete(want, p)
	}
	if len(want) != 0 {
		t.Errorf("missing expected tiles: %v", want)
	}
}

func TestWalkableTilesIncludesEveryRoomAndCorridorTile(t *testing.T) {
	m := geometryTestMap()

	tiles := m.WalkableTiles()
	// The room is 5x5 = 25 tiles; the corridor's Path has 3 points.
	if len(tiles) != 28 {
		t.Fatalf("expected 25 room tiles + 3 corridor tiles = 28, got %d", len(tiles))
	}

	got := map[Point]bool{}
	for _, p := range tiles {
		got[p] = true
	}
	for _, wantPoint := range []Point{{X: 7, Y: 7}, {X: 10, Y: 7}, {X: 11, Y: 7}, {X: 12, Y: 7}} {
		if !got[wantPoint] {
			t.Errorf("expected %v among walkable tiles", wantPoint)
		}
	}
}

func TestRoomByIDFindsMatchingRoom(t *testing.T) {
	m := geometryTestMap()

	r, ok := m.RoomByID(0)
	if !ok {
		t.Fatal("expected room 0 to be found")
	}
	if r.X != 5 || r.Y != 5 || r.W != 5 || r.H != 5 {
		t.Errorf("expected room 0's geometry to match, got %+v", r)
	}
}

func TestRoomByIDFalseWhenMissing(t *testing.T) {
	m := geometryTestMap()

	if _, ok := m.RoomByID(99); ok {
		t.Error("expected ok=false for a room ID that doesn't exist")
	}
}

func TestRevealDoorAtUpdatesEveryCopy(t *testing.T) {
	m := Map{
		Corridors: []Corridor{
			{DoorA: Door{X: 3, Y: 4, Hidden: true}, DoorB: Door{X: 9, Y: 9}},
			{DoorA: Door{X: 1, Y: 1}, DoorB: Door{X: 3, Y: 4, Hidden: true}},
		},
		Doors: []Door{{X: 3, Y: 4, Hidden: true}, {X: 9, Y: 9}},
	}
	if !m.RevealDoorAt(3, 4) {
		t.Fatal("expected a hidden door to reveal")
	}
	if m.Corridors[0].DoorA.Hidden || m.Corridors[1].DoorB.Hidden || m.Doors[0].Hidden {
		t.Error("every copy of the door should be revealed")
	}
	if m.RevealDoorAt(3, 4) || m.RevealDoorAt(9, 9) {
		t.Error("nothing hidden is left there to reveal")
	}
}
