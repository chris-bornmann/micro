package game

import (
	"strings"
	"testing"

	"micro/server/internal/rules"
	"micro/server/internal/rules/rulestest"
)

// reading is a game on TwoRooms with the Player in room A at (6,9),
// carrying only a scroll of summon monster, and the given monsters. The
// scroll is read, so the game is waiting on the answer.
func reading(t *testing.T, monsters ...*rules.Entity) (*GameState, *rules.Entity) {
	t.Helper()
	p := testPlayer(t, 6, 9, 1000)
	scroll := testItem(t, "summon_scroll", 0, 0)
	carrying(p, scroll)
	state := newTestGameState(t, rulestest.TwoRooms(), p, monsters, nil)
	mustKey(t, state, "A")
	return state, scroll
}

// inRoomA reports whether e stands in room A, where the Player is.
func inRoomA(e *rules.Entity) bool {
	return e.X >= 5 && e.X <= 14 && e.Y >= 5 && e.Y <= 14
}

func live(state *GameState, id string) []*rules.Entity {
	var out []*rules.Entity
	for _, m := range state.liveMonsters() {
		if m.T.ID == id {
			out = append(out, m)
		}
	}
	return out
}

func TestReadingASummonScrollAsksForACharacter(t *testing.T) {
	state, _ := reading(t)
	g := &Game{state: state}
	pr := g.pendingPrompt()
	if pr == nil || pr.Kind != PromptCharacter || pr.Title != "Scroll of summon monster" || len(pr.Lines) != 1 {
		t.Fatalf("prompt %+v", pr)
	}
	if state.Turn != 0 {
		t.Error("asking costs no turn")
	}
}

func TestSummoningAnOrdinaryMonsterMakesANewOne(t *testing.T) {
	state, _ := reading(t)
	msg := mustKey(t, state, "g")
	goblins := live(state, "goblin")
	if len(goblins) != 1 || !inRoomA(goblins[0]) {
		t.Fatalf("want one goblin in the Player's room, got %d", len(goblins))
	}
	if !strings.HasPrefix(msg, "You read the scroll of summon monster and a goblin appears!") {
		t.Errorf("message %q", msg)
	}
	if len(state.Player.Inventory) != 0 || state.Turn != 1 {
		t.Errorf("the scroll should be used up (%v) and a turn spent (%d)", state.Player.Inventory, state.Turn)
	}
}

func TestEscapeKeepsTheScroll(t *testing.T) {
	state, scroll := reading(t)
	mustKey(t, state, "Escape")
	if state.PendingSelection != nil || len(state.Player.Inventory) != 1 || state.Player.Inventory[0] != scroll || state.Turn != 0 {
		t.Error("Escape should cancel, keeping the scroll and the turn")
	}
}

// TestALetterNoMonsterHasSummonsNothing — and case matters: there is a
// goblin, g, but no G.
func TestALetterNoMonsterHasSummonsNothing(t *testing.T) {
	for _, key := range []string{"G", "#"} {
		state, _ := reading(t)
		msg := mustKey(t, state, key)
		if len(state.liveMonsters()) != 0 || !strings.Contains(msg, "nothing answers your call") || len(state.Player.Inventory) != 0 {
			t.Errorf("%q: monsters %d, %q", key, len(state.liveMonsters()), msg)
		}
	}
}

// TestTheThiefComesFromAcrossTheLevel: the one Thief, not a second, with
// what he stole.
func TestTheThiefComesFromAcrossTheLevel(t *testing.T) {
	thief := testMonster(t, "thief", 32, 7, 20) // in room B
	loot := testItem(t, "torch", 0, 0)
	thief.Inventory = append(thief.Inventory, loot)
	state, _ := reading(t, thief)
	msg := mustKey(t, state, "T")
	thieves := live(state, "thief")
	if len(thieves) != 1 || thieves[0] != thief || !inRoomA(thief) || thief.Inventory[0] != loot {
		t.Fatalf("want the same Thief, with his loot, in room A: %d thieves, at (%d,%d)", len(thieves), thief.X, thief.Y)
	}
	if !strings.Contains(msg, "the Thief appears!") {
		t.Errorf("message %q", msg)
	}
}

func TestTheThiefComesFromAnotherLevel(t *testing.T) {
	thief := testMonster(t, "thief", 32, 7, 20)
	state, _ := reading(t)
	other := &levelState{Map: rulestest.TwoRooms(), Monsters: []*rules.Entity{thief}, Visited: newVisibility()}
	state.Levels = append(state.Levels, other)
	mustKey(t, state, "T")
	if len(other.Monsters) != 0 || len(live(state, "thief")) != 1 || !inRoomA(thief) {
		t.Errorf("he should have left level 2 for the Player's room: level 2 has %d", len(other.Monsters))
	}
}

func TestTheThiefComesBackFromTheRoster(t *testing.T) {
	thief := testMonster(t, "thief", 32, 7, 20)
	thief.Mind.Attitude = rules.Afraid // ran off with something
	state, _ := reading(t, thief)
	depart(state, thief)
	mustKey(t, state, "T")
	if thief.Mind.Attitude != rules.Neutral {
		t.Errorf("summoned after escaping, he should be calm, is %q", thief.Mind.Attitude)
	}
	if len(state.Roster) != 0 || len(live(state, "thief")) != 1 || !inRoomA(thief) {
		t.Errorf("roster %d, thieves %d", len(state.Roster), len(live(state, "thief")))
	}
}

func TestADeadThiefCannotBeSummoned(t *testing.T) {
	thief := testMonster(t, "thief", 32, 7, 20)
	thief.Dead = true
	state, _ := reading(t, thief)
	msg := mustKey(t, state, "T")
	if len(live(state, "thief")) != 0 || !strings.Contains(msg, "can no longer answer") {
		t.Errorf("thieves %d, %q", len(live(state, "thief")), msg)
	}
}

// TestAThiefNotYetMetIsMadeAndNeverSpawnsAgain: summoned before he ever
// appeared, he is made now — and counts as having appeared.
func TestAThiefNotYetMetIsMadeAndNeverSpawnsAgain(t *testing.T) {
	state, _ := reading(t)
	mustKey(t, state, "T")
	if len(live(state, "thief")) != 1 || !state.SpawnedOnce["thief"] {
		t.Errorf("thieves %d, spawned once %v", len(live(state, "thief")), state.SpawnedOnce["thief"])
	}
}

// TestSummonedIntoACorridor: out of any room, the monster lands on the
// free tile nearest the Player.
func TestSummonedIntoACorridor(t *testing.T) {
	state, _ := reading(t)
	state.Player.X, state.Player.Y = 20, 9
	mustKey(t, state, "g")
	g := live(state, "goblin")
	if len(g) != 1 || abs(g[0].X-20)+abs(g[0].Y-9) != 1 {
		t.Errorf("want a goblin beside the Player in the corridor, got %v", g)
	}
}
