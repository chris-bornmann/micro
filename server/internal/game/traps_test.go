package game

import (
	"slices"
	"strings"
	"testing"

	"micro/server/internal/mapdata"
	"micro/server/internal/rules"
)

// trapRoom is a Player at (7, 7) in the one-room test level, with a trap of
// template id just to the right at (8, 7).
func trapRoom(t *testing.T, id string) (*GameState, *rules.Entity) {
	t.Helper()
	trap := testItem(t, id, 8, 7)
	state := newTestGameState(t, itemsTestMap(), testPlayer(t, 7, 7, 1000), nil, []*rules.Entity{trap})
	state.LevelConfig = testLevelConfig()
	state.Levels = make([]*levelState, maxLevels)
	state.Levels[0] = state.levelState
	revealAroundPlayer(state)
	return state, trap
}

func TestATrapDoorDropsYouToTheNextLevel(t *testing.T) {
	state, _ := trapRoom(t, "trap_door")
	msg := mustKey(t, state, "Right")
	if !strings.HasPrefix(msg, "A trap door opens beneath you! | You fall to level 2.") {
		t.Fatalf("got %q", msg)
	}
	if state.CurrentLevel != 2 || state.Levels[1] != state.levelState {
		t.Fatalf("expected to be on a stored level 2, on %d", state.CurrentLevel)
	}
	p := state.Player
	// Anywhere walkable — a room or a corridor.
	if !slices.Contains(state.Map.WalkableTiles(), mapdata.Point{X: p.X, Y: p.Y}) {
		t.Errorf("landed off the floor at (%d,%d)", p.X, p.Y)
	}
	if m := state.monsterAt(p.X, p.Y); m != nil {
		t.Errorf("landed on %s", m.ID)
	}
}

func TestATrapIsDrawnOnlyOnceFound(t *testing.T) {
	state, trap := trapRoom(t, "teleport_trap")
	if drawn(t, buildFrame(state, 0, false, false), 8, 7) == '^' {
		t.Error("a hidden trap must look like floor")
	}
	if !slices.Equal(lookAt(state, 8, 7), []string{"There is nothing here."}) {
		t.Error("look must not list a hidden trap")
	}
	if ch, color := cellAt(t, buildFrame(state, 0, true, false), 8, 7); ch != '^' || color != "#c94fd6" {
		t.Errorf("-reveal-hidden should draw it in magenta: %q %s", ch, color)
	}

	trap.RemoveTrait(trap.FindTrait("hidden")) // found
	if ch, color := cellAt(t, buildFrame(state, 0, false, false), 8, 7); ch != '^' || color != hex(trap.T.Color) {
		t.Errorf("a found trap should be drawn as itself: %q %s", ch, color)
	}
	if got := lookAt(state, 8, 7); !slices.Equal(got, []string{"A) teleport trap"}) {
		t.Errorf("look: %v", got)
	}
}

func TestTrueSightShowsHiddenTraps(t *testing.T) {
	state, trap := trapRoom(t, "pit")
	state.Player.Equipped = map[rules.Slot]*rules.Entity{rules.SlotRightHand: testItem(t, "ring_of_true_sight", 0, 0)}
	if ch, color := cellAt(t, buildFrame(state, 0, false, false), 8, 7); ch != '^' || color != hex(trap.T.Color) {
		t.Errorf("true sight should show the hidden pit: %q %s", ch, color)
	}
}

// lookAt is the look view as if the Player stood on (x, y).
func lookAt(state *GameState, x, y int) []string {
	p := state.Player
	px, py := p.X, p.Y
	p.X, p.Y = x, y
	defer func() { p.X, p.Y = px, py }()
	return lookHereLines(state)
}

func TestTrapsKeepToFreeTiles(t *testing.T) {
	if testing.Short() {
		t.Skip("seed sweep")
	}
	traps := 0
	for seed := int64(1); seed <= 20; seed++ {
		for level := 1; level <= 10; level++ {
			cfg := testLevelConfig()
			cfg.Seed = seed
			ls, err := populateLevel(testEngine(t), cfg, level, map[string]bool{})
			if err != nil {
				t.Fatal(err)
			}
			taken := map[[2]int]string{}
			for _, m := range ls.Monsters {
				taken[[2]int{m.X, m.Y}] = m.ID
			}
			for _, it := range ls.FloorItems {
				if it.T.Category != rules.CategoryTrap && it.T.Category != rules.CategoryStairs {
					continue
				}
				k := [2]int{it.X, it.Y}
				if other, ok := taken[k]; ok {
					t.Fatalf("seed %d level %d: %s shares %v with %s", seed, level, it.ID, k, other)
				}
				taken[k] = it.ID
				if it.T.Category == rules.CategoryTrap {
					traps++
				}
			}
		}
	}
	if traps == 0 {
		t.Error("no traps in 200 levels")
	}
}

func TestATrapDoorAlwaysWaitsOnLevelFive(t *testing.T) {
	for seed := int64(1); seed <= 20; seed++ {
		cfg := testLevelConfig()
		cfg.Seed = seed
		ls, _ := populateLevel(testEngine(t), cfg, 5, map[string]bool{})
		found := false
		for _, it := range ls.FloorItems {
			found = found || it.T.ID == "trap_door"
		}
		if !found {
			t.Errorf("seed %d: no trap door on level 5", seed)
		}
	}
}

func TestThePlayerNeverStartsOnATrap(t *testing.T) {
	for seed := int64(1); seed <= 40; seed++ {
		cfg := testLevelConfig()
		cfg.Seed = seed
		g, err := NewGame(cfg, false, false, false)
		if err != nil {
			t.Fatal(err)
		}
		if trapAt(g.state.FloorItems, g.state.Player.X, g.state.Player.Y) {
			t.Errorf("seed %d: the Player starts on a trap", seed)
		}
	}
}
