package game

import (
	"testing"

	"micromap/server/internal/mapdata"
	"micromap/server/internal/overlay"
	"micromap/server/internal/vision"
)

// visTestMap: two rooms connected by one corridor, plus an unrelated third
// room with its own corridor, so tests can confirm unrelated areas stay
// entirely absent from a filtered map.
func visTestMap() mapdata.Map {
	roomA := mapdata.Room{ID: 0, X: 5, Y: 5, W: 5, H: 5}  // interior 5-9
	roomB := mapdata.Room{ID: 1, X: 16, Y: 5, W: 5, H: 5} // interior 16-20
	corridor := mapdata.Corridor{
		ID: 0, RoomA: 0, RoomB: 1,
		DoorA: mapdata.Door{X: 10, Y: 7},
		DoorB: mapdata.Door{X: 15, Y: 7},
		Path: []mapdata.Point{
			{X: 10, Y: 7}, {X: 11, Y: 7}, {X: 12, Y: 7}, {X: 13, Y: 7}, {X: 14, Y: 7}, {X: 15, Y: 7},
		},
	}

	roomC := mapdata.Room{ID: 2, X: 30, Y: 30, W: 5, H: 5} // interior 30-34, unrelated
	corridorC := mapdata.Corridor{
		ID: 1, RoomA: 2, RoomB: 2,
		DoorA: mapdata.Door{X: 35, Y: 32},
		DoorB: mapdata.Door{X: 37, Y: 32},
		Path:  []mapdata.Point{{X: 35, Y: 32}, {X: 36, Y: 32}, {X: 37, Y: 32}},
	}

	return mapdata.Map{
		Width: 40, Height: 40,
		Rooms:     []mapdata.Room{roomA, roomB, roomC},
		Corridors: []mapdata.Corridor{corridor, corridorC},
	}
}

func hasRoom(m *mapdata.Map, id int) bool {
	for _, r := range m.Rooms {
		if r.ID == id {
			return true
		}
	}
	return false
}

func findCorridor(m *mapdata.Map, id int) (mapdata.Corridor, bool) {
	for _, c := range m.Corridors {
		if c.ID == id {
			return c, true
		}
	}
	return mapdata.Corridor{}, false
}

func TestRevealMarksContainingRoom(t *testing.T) {
	m := visTestMap()
	v := newVisibility()
	v.reveal(&m, 7, 7) // inside room A

	if !v.Rooms[0] {
		t.Error("expected room 0 to be marked visited")
	}
	if v.Rooms[1] || len(v.Corridors) != 0 {
		t.Error("expected nothing else to be marked visited yet")
	}
}

func TestRevealMarksContainingCorridor(t *testing.T) {
	m := visTestMap()
	v := newVisibility()
	v.reveal(&m, 12, 7) // on the corridor's path

	if !v.Corridors[0] {
		t.Error("expected corridor 0 to be marked visited")
	}
}

func TestVisibleMapShowsOnlyStartingRoomAndItsDoor(t *testing.T) {
	m := visTestMap()
	v := newVisibility()
	v.reveal(&m, 7, 7) // spawn inside room A, corridor not yet walked

	vis := v.visibleMap(&m)

	if !hasRoom(vis, 0) {
		t.Error("expected room A to be visible")
	}
	if hasRoom(vis, 1) {
		t.Error("room B should not be visible yet")
	}
	if hasRoom(vis, 2) {
		t.Error("unrelated room C should never be visible")
	}

	c, ok := findCorridor(vis, 0)
	if !ok {
		t.Fatal("expected a partial view of corridor 0 (room A's door)")
	}
	if len(c.Path) != 1 || c.Path[0] != (mapdata.Point{X: 10, Y: 7}) {
		t.Errorf("expected only room A's door (10,7) visible, got Path=%v", c.Path)
	}
	if c.DoorB.X >= 0 && c.DoorB.Y >= 0 {
		t.Errorf("expected room B's door to be suppressed (off-map sentinel), got %+v", c.DoorB)
	}

	if _, ok := findCorridor(vis, 1); ok {
		t.Error("unrelated corridor C should never be visible")
	}
}

func TestVisibleMapShowsFullCorridorOnceWalked(t *testing.T) {
	m := visTestMap()
	v := newVisibility()
	v.reveal(&m, 7, 7)  // room A
	v.reveal(&m, 12, 7) // walk into the corridor
	v.reveal(&m, 18, 7) // walk into room B

	vis := v.visibleMap(&m)

	if !hasRoom(vis, 0) || !hasRoom(vis, 1) {
		t.Error("expected both rooms A and B to be visible")
	}
	c, ok := findCorridor(vis, 0)
	if !ok {
		t.Fatal("expected corridor 0 to be visible")
	}
	if len(c.Path) != 6 {
		t.Errorf("expected the corridor's full path once visited, got %d points", len(c.Path))
	}
}

func TestRevealAtDoorAlsoRevealsTheRoomBeyondIt(t *testing.T) {
	m := visTestMap()
	v := newVisibility()
	v.reveal(&m, 7, 7)  // room A
	v.reveal(&m, 15, 7) // walk to the far door — the threshold of room B

	if !v.Corridors[0] {
		t.Error("expected corridor 0 to be marked visited")
	}
	if !v.Rooms[1] {
		t.Error("expected room B to be revealed just by standing at its door, before stepping inside it")
	}

	vis := v.visibleMap(&m)
	if !hasRoom(vis, 1) {
		t.Error("expected room B to appear in the visible map from its threshold alone")
	}
}

func TestVisitedAreasStayVisibleAfterLeaving(t *testing.T) {
	m := visTestMap()
	v := newVisibility()
	v.reveal(&m, 7, 7)
	v.reveal(&m, 12, 7)
	v.reveal(&m, 18, 7) // now in room B

	// Simulate backtracking to room A — reveal is idempotent, nothing should
	// be removed by continuing to move through already-visited territory.
	v.reveal(&m, 7, 7)

	vis := v.visibleMap(&m)
	if !hasRoom(vis, 0) || !hasRoom(vis, 1) {
		t.Error("expected previously visited rooms to remain visible")
	}
	if c, ok := findCorridor(vis, 0); !ok || len(c.Path) != 6 {
		t.Error("expected the corridor to remain fully visible")
	}
}

// TestFilterObjectsHidesUnvisitedAndShowsVisitedFixedObjects covers a
// Fixed object's permanent, memory-based visibility (e.g. a future
// staircase) — once its room has ever been visited, it stays visible
// forever, exactly like the map's own geometry. See
// TestFilterObjectsHidesMovableObjectsOnceOutOfCurrentView for the very
// different (current-visibility-only) rule movable objects follow.
func TestFilterObjectsHidesUnvisitedAndShowsVisitedFixedObjects(t *testing.T) {
	m := visTestMap()
	v := newVisibility()
	v.reveal(&m, 7, 7) // only room A visited so far

	inA := overlay.Object{ID: "stairs:1", X: 7, Y: 7, Fixed: true}
	inB := overlay.Object{ID: "stairs:2", X: 18, Y: 7, Fixed: true} // room B, unvisited

	visible := v.filterObjects(&m, []overlay.Object{inA, inB}, nil)
	if len(visible) != 1 || visible[0].ID != "stairs:1" {
		t.Errorf("expected only the object in the visited room, got %+v", visible)
	}

	v.reveal(&m, 12, 7) // walk the corridor
	v.reveal(&m, 18, 7) // enter room B

	visible = v.filterObjects(&m, []overlay.Object{inA, inB}, nil)
	if len(visible) != 2 {
		t.Errorf("expected both objects visible once room B is revealed, got %+v", visible)
	}
}

// TestFilterObjectsShowsMovableObjectsFromDoorThreshold confirms a
// movable object is visible the instant the Player reaches a room's
// threshold, before fully stepping inside — mirroring the old memory-
// based behavior for room geometry, and letting a Sneaking Player peek
// into a room without a monster there ever becoming aware of them (see
// goblin.Action's own threshold-vs-Sneaking asymmetry, untouched by this
// package).
func TestFilterObjectsShowsMovableObjectsFromDoorThreshold(t *testing.T) {
	m := visTestMap()
	v := newVisibility()
	v.reveal(&m, 7, 7)  // visited room A
	v.reveal(&m, 12, 7) // and walked partway down the corridor

	inB := overlay.Object{ID: "goblin:1", X: 18, Y: 7} // inside room B (never entered yet)

	// Standing exactly on room B's door (15,7), not yet inside it.
	currentlyVisible := vision.CurrentlyVisible(m, 15, 7, false)
	visible := v.filterObjects(&m, []overlay.Object{inB}, currentlyVisible)
	if len(visible) != 1 || visible[0].ID != "goblin:1" {
		t.Errorf("expected the object in room B visible from its threshold alone, got %+v", visible)
	}
}

// TestFilterObjectsHidesMovableObjectsOnceOutOfCurrentView covers the
// new default (Fixed unset) rule for every item and intelligent entity
// today: visibility depends solely on the currentlyVisible set passed in
// this turn, never on whether the room was visited before. A room
// visited earlier but no longer occupied hides its movable objects even
// though the room itself stays revealed on the map.
func TestFilterObjectsHidesMovableObjectsOnceOutOfCurrentView(t *testing.T) {
	m := visTestMap()
	v := newVisibility()
	v.reveal(&m, 7, 7)  // visit room A
	v.reveal(&m, 12, 7) // walk the corridor
	v.reveal(&m, 18, 7) // visit room B; both rooms now permanently revealed

	inA := overlay.Object{ID: "goblin:1", X: 7, Y: 7} // movable (Fixed unset)
	inB := overlay.Object{ID: "goblin:2", X: 18, Y: 7}

	// Currently standing in room A only: room B's occupant must be
	// hidden, even though room B itself was visited a moment ago.
	roomATiles := mapdata.Room{ID: 0, X: 5, Y: 5, W: 5, H: 5}.Tiles()
	visible := v.filterObjects(&m, []overlay.Object{inA, inB}, roomATiles)
	if len(visible) != 1 || visible[0].ID != "goblin:1" {
		t.Errorf("expected only the object in the currently-viewed room, got %+v", visible)
	}

	// Now standing in room B instead: room A's occupant must be hidden,
	// symmetric with the above — memory of room A no longer matters.
	roomBTiles := mapdata.Room{ID: 1, X: 16, Y: 5, W: 5, H: 5}.Tiles()
	visible = v.filterObjects(&m, []overlay.Object{inA, inB}, roomBTiles)
	if len(visible) != 1 || visible[0].ID != "goblin:2" {
		t.Errorf("expected only the object in the currently-viewed room, got %+v", visible)
	}
}
