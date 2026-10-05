package game

import (
	"strings"
	"testing"

	"micro/server/internal/rules"
	"micro/server/internal/rules/rulestest"
)

// awarenessScene is a Player in room A of a two-room map with a goblin
// out of sight in room B, which the Player has never seen, and an
// invisible phantom right beside them.
func awarenessScene(t *testing.T) (*GameState, *rules.Entity, *rules.Entity) {
	t.Helper()
	g := testMonster(t, "goblin", 32, 7, 10)
	ph := testMonster(t, "phantom", 8, 7, 10)
	state := newTestGameState(t, rulestest.TwoRooms(), testPlayer(t, 7, 7, 100), []*rules.Entity{g, ph}, nil)
	revealAroundPlayer(state)
	return state, g, ph
}

func drawn(t *testing.T, f Frame, x, y int) rune {
	t.Helper()
	ch, err := glyphAt(f, x, y)
	if err != nil {
		t.Fatal(err)
	}
	return ch
}

func TestMonsterVisionShowsEveryMonsterButNoMoreOfTheMap(t *testing.T) {
	state, g, ph := awarenessScene(t)

	before := buildFrame(state, 0, false, false)
	if drawn(t, before, g.X, g.Y) == 'g' || drawn(t, before, ph.X, ph.Y) == 'p' {
		t.Fatal("without monster vision, neither the distant goblin nor the invisible phantom shows")
	}

	state.Player.AddTrait(&rules.TraitInstance{Trait: "monster_vision", Turns: 5})
	after := buildFrame(state, 0, false, false)
	if drawn(t, after, g.X, g.Y) != 'g' {
		t.Error("the goblin in an unexplored room should show")
	}
	ch, color := cellAt(t, after, ph.X, ph.Y)
	if ch != 'p' || color != hex(ph.T.Color) {
		t.Errorf("the invisible phantom should show in its own color, got %q in %s", ch, color)
	}

	// Nothing else about room B appears: every cell that differs between
	// the two frames is a monster.
	for y, row := range after.Rows {
		was := []rune(before.Rows[y])
		for x, ch := range []rune(row) {
			if ch != was[x] && !(x == g.X && y == g.Y) && !(x == ph.X && y == ph.Y) {
				t.Errorf("(%d,%d) changed from %q to %q", x, y, was[x], ch)
			}
		}
	}
}

func TestMonsterVisionFromAWornItemLastsWhileWorn(t *testing.T) {
	state, g, _ := awarenessScene(t)
	ring := testItem(t, "shoes_of_sneaking", 0, 0)
	ring.Traits = []*rules.TraitInstance{{Trait: "monster_vision"}}
	state.Player.Equipped = map[rules.Slot]*rules.Entity{rules.SlotFeet: ring}

	for i := 0; i < 50; i++ {
		mustKey(t, state, ".")
	}
	if drawn(t, buildFrame(state, 0, false, false), g.X, g.Y) != 'g' {
		t.Error("worn, it never wears off")
	}
}

// hiddenDoorScene is TwoRooms with its room-A door hidden, the Player
// inside room A, and an invisible phantom beside them.
func hiddenDoorScene(t *testing.T) (*GameState, *rules.Entity) {
	t.Helper()
	m := rulestest.TwoRooms()
	m.Corridors[0].DoorA.Hidden = true
	ph := testMonster(t, "phantom", 13, 9, 10)
	state := newTestGameState(t, m, testPlayer(t, 12, 9, 100), []*rules.Entity{ph}, nil)
	revealAroundPlayer(state)
	return state, ph
}

func TestTheRingOfTrueSightShowsHiddenDoorsAndInvisibleMonsters(t *testing.T) {
	state, ph := hiddenDoorScene(t)
	f := buildFrame(state, 0, false, false)
	if drawn(t, f, 15, 9) != '#' || drawn(t, f, ph.X, ph.Y) == 'p' {
		t.Fatal("without the ring the door looks like wall and the phantom is unseen")
	}

	state.Player.Inventory = []*rules.Entity{testItem(t, "ring_of_true_sight", 0, 0)}
	if msg := mustKey(t, state, "E"); !strings.HasPrefix(msg, "You put the ring of true sight on your right hand.") {
		t.Fatalf("got %q", msg)
	}
	f = buildFrame(state, 0, false, false)
	if drawn(t, f, 15, 9) != '+' {
		t.Errorf("the hidden door should show as a door, got %q", drawn(t, f, 15, 9))
	}
	if ch, color := cellAt(t, f, ph.X, ph.Y); ch != 'p' || color != hex(ph.T.Color) {
		t.Errorf("the phantom should show in its own color, got %q in %s", ch, color)
	}
	if got := inventoryLines(state); got[0] != "A) ring of true sight (on right hand)" {
		t.Errorf("inventory %v", got)
	}

	// The cheat still wins: magenta.
	if drawn(t, buildFrame(state, 0, true, false), 15, 9) != 'h' {
		t.Error("-reveal-hidden should still draw hidden doors as h")
	}
}

// doorScene is TwoRooms with room A's door at (15,9) hidden, and the
// Player at (x, 9).
func doorScene(t *testing.T, x int) *GameState {
	t.Helper()
	m := rulestest.TwoRooms()
	m.Corridors[0].DoorA.Hidden = true
	state := newTestGameState(t, m, testPlayer(t, x, 9, 100), nil, nil)
	revealAroundPlayer(state)
	return state
}

func TestFindingAHiddenDoorFromTheRoomRevealsAndAnnouncesIt(t *testing.T) {
	state := doorScene(t, 14)
	if drawn(t, buildFrame(state, 0, false, false), 15, 9) != '#' {
		t.Fatal("the hidden door should look like wall to begin with")
	}
	if msg := mustKey(t, state, "Right"); msg != "You found a hidden door!" {
		t.Errorf("got %q", msg)
	}
	if state.Map.Corridors[0].DoorA.Hidden {
		t.Error("the door should no longer be hidden")
	}
	mustKey(t, state, "Left") // back into the room
	if drawn(t, buildFrame(state, 0, false, false), 15, 9) != '+' {
		t.Error("the door should now be drawn as a door, for good")
	}
	if msg := mustKey(t, state, "Right"); msg != "" {
		t.Errorf("a door is only found once, got %q", msg)
	}
}

func TestReachingAHiddenDoorFromTheCorridorRevealsItQuietly(t *testing.T) {
	state := doorScene(t, 16)
	if msg := mustKey(t, state, "Left"); msg != "" {
		t.Errorf("no surprise from the corridor side, got %q", msg)
	}
	if state.Map.Corridors[0].DoorA.Hidden {
		t.Error("the door should still be revealed")
	}
	// Walking back out through it from the room is no discovery now.
	mustKey(t, state, "Left")
	if msg := mustKey(t, state, "Right"); msg != "" {
		t.Errorf("got %q", msg)
	}
}

// TestVisibleMatchesTheFrame: a monster is drawn exactly where the engine
// is told the Player can see.
func TestVisibleMatchesTheFrame(t *testing.T) {
	g := testMonster(t, "goblin", 32, 7, 10)
	state := newTestGameState(t, rulestest.TwoRooms(), testPlayer(t, 7, 7, 100), []*rules.Entity{g}, nil)
	revealAroundPlayer(state)
	for _, pos := range [][2]int{{8, 7}, {32, 7}, {12, 12}} {
		g.X, g.Y = pos[0], pos[1]
		seen := drawn(t, buildFrame(state, 0, false, false), g.X, g.Y) == 'g'
		if seen != state.world().PlayerSees(g.X, g.Y) {
			t.Errorf("goblin at %v: drawn %v, but PlayerSees says %v", pos, seen, !seen)
		}
	}
}
