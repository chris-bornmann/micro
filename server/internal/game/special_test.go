package game

import (
	"crypto/sha256"
	"fmt"
	"testing"

	"micro/server/internal/grid"
	"micro/server/internal/rules"
)

// ordinaryLevels pins ordinary levels — their map, and every monster and
// item on them — for three seeds and six depths, as they were before
// special levels came. Walls inside rooms and special levels must leave
// every other level exactly as it was. (Only a deliberate change to
// generation or content should change these.)
var ordinaryLevels = map[string]string{
	"1/1":  "f5ca1be19c940620",
	"1/2":  "88de2d7e9fbb215f",
	"1/3":  "76164975cb5fa053",
	"1/4":  "c9c87b688c536172",
	"1/5":  "f74afffbf943d3d5",
	"1/6":  "5eae38e978722ac0",
	"4/1":  "d2646f9661b3d8f0",
	"4/2":  "e91370da835d23df",
	"4/3":  "cbca0af190692fae",
	"4/4":  "bc21f4fa2356d02d",
	"4/5":  "548062ea0b219ee9",
	"4/6":  "c325f85bde5191dd",
	"99/1": "9b0a010acb121a68",
	"99/2": "38703fefe222d2fa",
	"99/3": "4c05afd339f00c4d",
	"99/4": "e745e97d387ee56c",
	"99/5": "eedc55fd21c343f5",
	"99/6": "0f792170053be1de",
}

func TestOrdinaryLevelsAreAsTheyWere(t *testing.T) {
	for _, seed := range []int64{1, 4, 99} {
		for level := 1; level <= 6; level++ {
			cfg := testLevelConfig()
			cfg.Seed = seed
			ls, err := populateLevel(testEngine(t), cfg, level, map[string]bool{})
			if err != nil {
				t.Fatal(err)
			}
			rows, _, _ := grid.Build(ls.Map, grid.Options{}).Encode()
			h := sha256.New()
			fmt.Fprint(h, rows)
			for _, m := range ls.Monsters {
				fmt.Fprint(h, m.T.ID, m.X, m.Y, m.HP, m.Attrs)
				for _, it := range m.Carried() {
					fmt.Fprint(h, it.T.ID)
				}
			}
			for _, it := range ls.FloorItems {
				fmt.Fprint(h, it.T.ID, it.X, it.Y)
			}
			key := fmt.Sprintf("%d/%d", seed, level)
			if got := fmt.Sprintf("%x", h.Sum(nil)[:8]); got != ordinaryLevels[key] {
				t.Errorf("seed %d, level %d changed: %s, was %s", seed, level, got, ordinaryLevels[key])
			}
		}
	}
}

func special(t *testing.T, name string) *rules.LevelSpec {
	t.Helper()
	for _, s := range testEngine(t).Cat.Levels {
		if s.Name == name {
			return s
		}
	}
	t.Fatalf("no special level %q", name)
	return nil
}

func TestSpecialLevelsAreSettledPerGame(t *testing.T) {
	specs := testEngine(t).Cat.Levels
	a, b := assignSpecial(specs, 7), assignSpecial(specs, 7)
	if fmt.Sprint(a) != fmt.Sprint(b) {
		t.Error("the same seed should settle the same special levels")
	}
	mazes := 0
	for seed := int64(1); seed <= 200; seed++ {
		got := assignSpecial(specs, seed)
		if got[10] == nil || got[10].Name != "throne_room" {
			t.Fatalf("seed %d: the throne room is always at depth 10", seed)
		}
		for d, s := range got {
			if s.Name == "maze" {
				mazes++
				if d < 4 || d > 8 {
					t.Errorf("seed %d: a maze at depth %d", seed, d)
				}
			}
		}
	}
	// chance 60.
	if mazes < 90 || mazes > 150 {
		t.Errorf("a maze in %d of 200 games", mazes)
	}
}

// TestTheThroneRoomHasOnlyWhatItPlaces: the King, his guards, the
// treasury, the stairs — and nothing at random.
func TestTheThroneRoomHasOnlyWhatItPlaces(t *testing.T) {
	spawnedOnce := map[string]bool{}
	ls, err := makeLevel(testEngine(t), testLevelConfig(), 10, spawnedOnce, special(t, "throne_room"))
	if err != nil {
		t.Fatal(err)
	}
	count := map[string]int{}
	for _, m := range ls.Monsters {
		count[m.T.ID]++
	}
	for _, it := range ls.FloorItems {
		count[it.T.ID]++
	}
	want := map[string]int{"king": 1, "royal_guard": 9, "stairs_up": 1, "stairs_down": 1, "healing_potion": 2,
		"knowledge_scroll": 1, "ring_of_regeneration": 1, "chain_mail": 1, "cursed_helm": 1}
	if fmt.Sprint(count) != fmt.Sprint(want) {
		t.Errorf("throne room holds %v, want %v", count, want)
	}
	if !spawnedOnce["king"] {
		t.Error("the King, placed, has appeared")
	}
	again, _ := makeLevel(testEngine(t), testLevelConfig(), 10, spawnedOnce, special(t, "throne_room"))
	for _, m := range again.Monsters {
		if m.T.ID == "king" {
			t.Error("there is only one King")
		}
	}
}

// TestAMazeIsPopulatedButNeverOnAWall.
func TestAMazeIsPopulatedButNeverOnAWall(t *testing.T) {
	for seed := int64(1); seed <= 5; seed++ {
		cfg := testLevelConfig()
		cfg.Seed = seed
		ls, err := makeLevel(testEngine(t), cfg, 6, map[string]bool{}, special(t, "maze"))
		if err != nil {
			t.Fatal(err)
		}
		if len(ls.Map.Rooms) != 1 || !ls.Map.Rooms[0].Walled {
			t.Fatalf("seed %d: a maze is one walled room", seed)
		}
		if len(ls.Monsters) == 0 || len(ls.FloorItems) < 5 {
			t.Errorf("seed %d: %d monsters, %d floor items", seed, len(ls.Monsters), len(ls.FloorItems))
		}
		stairs := 0
		for _, e := range append(append([]*rules.Entity{}, ls.Monsters...), ls.FloorItems...) {
			if ls.Map.IsWall(e.X, e.Y) {
				t.Errorf("seed %d: %s inside a wall at (%d,%d)", seed, e.T.ID, e.X, e.Y)
			}
			if e.T.Category == rules.CategoryStairs {
				stairs++
			}
		}
		if stairs != 2 {
			t.Errorf("seed %d: %d stairs", seed, stairs)
		}
	}
}

// TestGoingDownReachesTheSpecialLevel: a game with a maze at depth 2
// builds it when the Player first goes down, and lands them on its way
// in.
func TestGoingDownReachesTheSpecialLevel(t *testing.T) {
	state := newLevel1TestState(t)
	state.Special = map[int]*rules.LevelSpec{2: special(t, "maze")}
	down := stairsAmong(state.FloorItems, "down")
	state.Player.X, state.Player.Y = down.X, down.Y
	mustKey(t, state, ">")
	if state.CurrentLevel != 2 || len(state.Map.Rooms) != 1 || !state.Map.Rooms[0].Walled {
		t.Fatalf("level %d is not the maze", state.CurrentLevel)
	}
	if up := stairsAmong(state.FloorItems, "up"); up == nil || state.Player.X != up.X || state.Player.Y != up.Y {
		t.Error("the Player should arrive on the maze's way in")
	}
}

// TestADrawnLevelCanAlsoHaveRandomMonsters: the throne room asking for
// monsters by depth keeps its own placed ones and gains random ones — but
// no random items or traps, which it didn't ask for.
func TestADrawnLevelCanAlsoHaveRandomMonsters(t *testing.T) {
	spec := *special(t, "throne_room")
	spec.Populate = rules.Populate{Monsters: true}
	ls, err := makeLevel(testEngine(t), testLevelConfig(), 10, map[string]bool{}, &spec)
	if err != nil {
		t.Fatal(err)
	}
	placed, random := 0, 0
	for _, m := range ls.Monsters {
		if m.T.ID == "king" || m.T.ID == "royal_guard" {
			placed++
		} else {
			random++
		}
	}
	if placed != 10 || random == 0 {
		t.Errorf("%d placed, %d random monsters", placed, random)
	}
	if len(ls.FloorItems) != 8 { // the two stairs and the treasury's six, nothing else
		t.Errorf("%d floor items: no random items or traps were asked for", len(ls.FloorItems))
	}
}
