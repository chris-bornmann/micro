package game

import (
	"testing"

	"micro/server/internal/mapdata"
	"micro/server/internal/overlay"
	"micro/server/internal/rules"
	"micro/server/internal/vision"
)

// darkTestMap: a Dark room (interior x:5-9, y:5-9) with a corridor exiting
// its east wall, connected to an ordinary lit room (interior x:16-20).
func darkTestMap() mapdata.Map {
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

func TestVisibleMapAlwaysExcludesDarkRoomButKeepsItMarkedVisited(t *testing.T) {
	m := darkTestMap()
	v := newVisibility()
	v.reveal(&m, 7, 7) // player standing in the dark room

	vis := v.visibleMap(&m)
	if hasRoom(vis, 0) {
		t.Error("expected the dark room excluded from the filtered map's full room list")
	}
	if !v.Rooms[0] {
		t.Error("expected the dark room to remain permanently marked visited despite being excluded from rendering")
	}
}

func TestVisibleMapExcludesDarkRoomEvenAfterPlayerLeavesIt(t *testing.T) {
	m := darkTestMap()
	v := newVisibility()
	v.reveal(&m, 7, 7)  // enter the dark room
	v.reveal(&m, 12, 7) // walk into the corridor
	v.reveal(&m, 18, 7) // now in the lit room, dark room behind us

	vis := v.visibleMap(&m)
	if hasRoom(vis, 0) {
		t.Error("expected the dark room to stay excluded from full-room rendering even after leaving it — this is the bug being fixed: it must never fall back to whole-room reveal")
	}
	if !hasRoom(vis, 1) {
		t.Error("expected the lit room to render in full")
	}
}

func TestVisibleMapIncludesLitRoomsNormally(t *testing.T) {
	m := darkTestMap()
	v := newVisibility()
	v.reveal(&m, 18, 7) // player standing in the lit room

	vis := v.visibleMap(&m)
	if !hasRoom(vis, 1) {
		t.Error("expected the lit room to render in full, unaffected by darkness")
	}
}

func TestFilterObjectsShowsOnlyObjectsWithinCurrentRadius(t *testing.T) {
	m := darkTestMap()
	v := newVisibility()
	v.reveal(&m, 7, 7)

	litTiles, _, _ := vision.CurrentlyLit(m, 7, 7, false)

	player := sighting{Object: overlay.Object{ID: "player:1", X: 7, Y: 7}}
	adjacentGoblin := sighting{Object: overlay.Object{ID: "goblin:1", X: 8, Y: 7}} // 1 tile away, within radius 1
	farGoblin := sighting{Object: overlay.Object{ID: "goblin:2", X: 5, Y: 9}}      // same room, out of radius

	visible := v.filterObjects(&m, []sighting{player, adjacentGoblin, farGoblin}, litTiles)

	seen := map[string]bool{}
	for _, o := range visible {
		seen[o.ID] = true
	}
	if len(visible) != 2 || !seen["player:1"] || !seen["goblin:1"] {
		t.Errorf("expected player:1 and goblin:1 visible, got %+v", visible)
	}
	if seen["goblin:2"] {
		t.Error("expected the far goblin (out of the dark radius) to be hidden")
	}
}

func TestFilterObjectsHidesObjectsOnRememberedButNotCurrentlyLitTiles(t *testing.T) {
	m := darkTestMap()
	v := newVisibility()
	v.reveal(&m, 6, 6)

	// Permanently remember (6,6)'s lit radius, as a turn would.
	litAt66, _, _ := vision.CurrentlyLit(m, 6, 6, false)
	v.rememberLitTiles(litAt66)

	// Now the Player has moved to (9,9), far from (6,6); (6,6) itself is
	// remembered (floor still shows there) but is NOT part of the
	// CURRENT frame's lit tiles, so an object sitting there must be
	// hidden even though the floor renders.
	litAt99, _, _ := vision.CurrentlyLit(m, 9, 9, false)
	monsterAtOldSpot := sighting{Object: overlay.Object{ID: "goblin:1", X: 6, Y: 6}}

	visible := v.filterObjects(&m, []sighting{monsterAtOldSpot}, litAt99)
	if len(visible) != 0 {
		t.Errorf("expected the object on a remembered-but-not-currently-lit tile hidden, got %+v", visible)
	}
}

func TestRememberLitTilesAccumulatesAcrossMultiplePositions(t *testing.T) {
	m := darkTestMap()
	v := newVisibility()

	lit1, _, _ := vision.CurrentlyLit(m, 6, 6, false)
	v.rememberLitTiles(lit1)
	lit2, _, _ := vision.CurrentlyLit(m, 8, 8, false)
	v.rememberLitTiles(lit2)

	remembered := map[mapdata.Point]bool{}
	for _, p := range v.rememberedTiles() {
		remembered[p] = true
	}
	for _, p := range lit1 {
		if !remembered[p] {
			t.Errorf("expected %v (from the first position) to still be remembered", p)
		}
	}
	for _, p := range lit2 {
		if !remembered[p] {
			t.Errorf("expected %v (from the second position) to be remembered", p)
		}
	}
}

func TestDarkRoomFloorStaysRememberedAfterPlayerLeavesButObjectsDoNot(t *testing.T) {
	m := darkTestMap()
	v := newVisibility()

	// Player starts at (6,6), near the northwest corner, and it gets
	// remembered (as a turn boundary would do).
	v.reveal(&m, 6, 6)
	lit1, _, _ := vision.CurrentlyLit(m, 6, 6, false)
	v.rememberLitTiles(lit1)

	// Walks the corridor and ends up in the lit room — well outside the
	// dark room's radius entirely.
	v.reveal(&m, 12, 7)
	v.reveal(&m, 18, 7)
	lit3, _, inDark3 := vision.CurrentlyLit(m, 18, 7, false)
	if inDark3 {
		t.Fatal("expected the lit room to not restrict vision")
	}

	// The floor at (6,6) must still be part of the permanent memory...
	remembered := map[mapdata.Point]bool{}
	for _, p := range v.rememberedTiles() {
		remembered[p] = true
	}
	if !remembered[(mapdata.Point{X: 6, Y: 6})] {
		t.Error("expected (6,6)'s floor to remain permanently remembered after leaving the dark room")
	}

	// ...but a monster sitting on that remembered tile must not show,
	// since the Player is no longer anywhere near it.
	monsterAtOldSpot := sighting{Object: overlay.Object{ID: "goblin:1", X: 6, Y: 6}}
	visible := v.filterObjects(&m, []sighting{monsterAtOldSpot}, lit3)
	if len(visible) != 0 {
		t.Errorf("expected the monster on the remembered tile hidden once the Player is far away, got %+v", visible)
	}
}

// shownDoors is where vis draws doors: every door endpoint on the map.
func shownDoors(vis *mapdata.Map) map[mapdata.Point]bool {
	out := map[mapdata.Point]bool{}
	for _, c := range vis.Corridors {
		for _, d := range []mapdata.Door{c.DoorA, c.DoorB} {
			if d != offMap {
				out[mapdata.Point{X: d.X, Y: d.Y}] = true
			}
		}
	}
	return out
}

// TestADarkRoomsDoorShowsOnlyOnceSeen: inside a dark room, its door at
// (10,7) stays unseen from across the room, shows once the Player is next
// to it, and stays shown after they move away.
func TestADarkRoomsDoorShowsOnlyOnceSeen(t *testing.T) {
	m := darkTestMap()
	v := newVisibility()
	door := mapdata.Point{X: 10, Y: 7}
	at := func(x, y int) {
		v.reveal(&m, x, y)
		v.seeDoors(&m, x, y, false)
	}

	at(6, 7)
	if shownDoors(v.visibleMap(&m))[door] {
		t.Fatal("from across a dark room, its door should not show")
	}
	at(9, 6) // diagonally next to it
	if !shownDoors(v.visibleMap(&m))[door] {
		t.Fatal("next to it, the door should show")
	}
	at(6, 7)
	if !shownDoors(v.visibleMap(&m))[door] {
		t.Error("a door once seen stays shown")
	}
}

func TestATorchShowsEveryDoorOfADarkRoom(t *testing.T) {
	m := darkTestMap()
	v := newVisibility()
	v.reveal(&m, 6, 7)
	v.seeDoors(&m, 6, 7, true)
	if !shownDoors(v.visibleMap(&m))[mapdata.Point{X: 10, Y: 7}] {
		t.Error("a torch lights the whole room, its doors too")
	}
}

// TestTrueSightShowsAHiddenDoorOnlyWhereThePlayerCanSee: the ring of true
// sight draws a dark room's hidden door as a door — but only once the
// Player's light reaches it, not on entering the room.
func TestTrueSightShowsAHiddenDoorOnlyWhereThePlayerCanSee(t *testing.T) {
	m := darkTestMap()
	m.Corridors[0].DoorA.Hidden = true
	p := testPlayer(t, 6, 7, 100)
	p.Equipped = map[rules.Slot]*rules.Entity{rules.SlotRightHand: testItem(t, "ring_of_true_sight", 0, 0)}
	state := newTestGameState(t, &m, p, nil, nil)

	glyphAtDoor := func() byte {
		revealAroundPlayer(state)
		return buildFrame(state, 0, false, false).Rows[7][10]
	}
	if g := glyphAtDoor(); g == '+' {
		t.Fatal("from across the dark room, the hidden door should not show")
	}
	p.X = 9
	if g := glyphAtDoor(); g != '+' {
		t.Errorf("next to it, true sight should show it as a door, got %q", g)
	}
}
