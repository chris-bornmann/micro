package main

import (
	"strings"
	"testing"

	"map_api/item"
	"map_api/stairs"
)

func TestPopulateLevelSpawnsExpectedItemsAndStairs(t *testing.T) {
	ls, err := populateLevel(testLevelConfig(), 1, map[string]bool{})
	if err != nil {
		t.Fatalf("populateLevel: %v", err)
	}

	wantCategories := []item.Category{
		item.CategoryPotion, item.CategoryPotion, // healing + invisibility
		item.CategoryWeapon, item.CategoryTorch, item.CategoryScroll,
		item.CategoryArmor, item.CategoryFlask, item.CategoryStairs, item.CategoryStairs,
	}
	if len(ls.FloorItems) != len(wantCategories) {
		t.Fatalf("expected %d floor items, got %d", len(wantCategories), len(ls.FloorItems))
	}

	up, down := false, false
	for _, raw := range ls.FloorItems {
		it, err := itemInfo(raw)
		if err != nil {
			t.Fatalf("itemInfo: %v", err)
		}
		if it.Category != item.CategoryStairs {
			continue
		}
		kind, err := stairs.KindOf(raw)
		if err != nil {
			t.Fatalf("stairs.KindOf: %v", err)
		}
		if kind == stairs.KindUp {
			up = true
		}
		if kind == stairs.KindDown {
			down = true
		}
	}
	if !up || !down {
		t.Errorf("expected both stairs up and down on level 1, got up=%v down=%v", up, down)
	}
}

func TestPopulateLevelNeverSpawnsMummyBeforeItsEarliestLevel(t *testing.T) {
	spawnedOnce := map[string]bool{}
	for level := 1; level < 3; level++ {
		cfg := testLevelConfig()
		cfg.Seed += int64(level) // vary the map/rng across levels, like a real run would
		ls, err := populateLevel(cfg, level, spawnedOnce)
		if err != nil {
			t.Fatalf("populateLevel(level %d): %v", level, err)
		}
		for _, m := range ls.Monsters {
			if m.Kind == "mummy" {
				t.Errorf("expected no mummy before its earliestLevel, found one on level %d", level)
			}
		}
	}
}

func TestPopulateLevelMarksSpawnedOnceWhenAMummyAppears(t *testing.T) {
	spawnedOnce := map[string]bool{}
	// mummy.spawnForceByLevel guarantees an appearance if it hasn't
	// already happened — use that level so this test isn't flaky.
	ls, err := populateLevel(testLevelConfig(), 8, spawnedOnce)
	if err != nil {
		t.Fatalf("populateLevel: %v", err)
	}
	found := false
	for _, m := range ls.Monsters {
		if m.Kind == "mummy" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected a guaranteed mummy at the forceByLevel depth")
	}
	if !spawnedOnce["mummy"] {
		t.Error("expected spawnedOnce[\"mummy\"] to be set true after a mummy actually spawned")
	}
}

func TestPopulateLevelNeverSpawnsAnotherMummyOnceSpawnedOnce(t *testing.T) {
	spawnedOnce := map[string]bool{"mummy": true}
	// Even at the guaranteed forceByLevel depth, an already-true
	// spawnedOnce must suppress any further appearance.
	ls, err := populateLevel(testLevelConfig(), 8, spawnedOnce)
	if err != nil {
		t.Fatalf("populateLevel: %v", err)
	}
	for _, m := range ls.Monsters {
		if m.Kind == "mummy" {
			t.Error("expected no further mummy once spawnedOnce[\"mummy\"] was already true")
		}
	}
}

func TestPopulateLevelGoblinCountVariesAcrossLevels(t *testing.T) {
	counts := map[int]bool{}
	for seed := int64(1); seed <= 40; seed++ {
		cfg := testLevelConfig()
		cfg.Seed = seed
		ls, err := populateLevel(cfg, 1, map[string]bool{})
		if err != nil {
			t.Fatalf("populateLevel: %v", err)
		}
		n := 0
		for _, m := range ls.Monsters {
			if m.Kind == "goblin" {
				n++
			}
		}
		counts[n] = true
	}
	if len(counts) < 2 {
		t.Errorf("expected goblin count to vary across different seeds/levels, always got the same set %+v", counts)
	}
}

func TestPopulateLevelOmitsStairsDownOnTheLastLevel(t *testing.T) {
	ls, err := populateLevel(testLevelConfig(), maxLevels, map[string]bool{})
	if err != nil {
		t.Fatalf("populateLevel: %v", err)
	}

	up, down := false, false
	for _, raw := range ls.FloorItems {
		it, err := itemInfo(raw)
		if err != nil {
			t.Fatalf("itemInfo: %v", err)
		}
		if it.Category != item.CategoryStairs {
			continue
		}
		kind, err := stairs.KindOf(raw)
		if err != nil {
			t.Fatalf("stairs.KindOf: %v", err)
		}
		if kind == stairs.KindUp {
			up = true
		}
		if kind == stairs.KindDown {
			down = true
		}
	}
	if !up {
		t.Error("expected stairs up even on the last level")
	}
	if down {
		t.Error("expected no stairs down on the last level")
	}
}

// newLevel1TestState builds a real, populated level 1 (via populateLevel,
// so its stairs actually exist somewhere) wrapped in a GameState ready
// for applyStairsTransition, with the Player positioned exactly on
// level 1's own down-stairs — mirroring what a Player standing there
// mid-game would look like.
func newLevel1TestState(t *testing.T) *GameState {
	t.Helper()
	cfg := testLevelConfig()
	spawnedOnce := map[string]bool{}
	ls, err := populateLevel(cfg, 1, spawnedOnce)
	if err != nil {
		t.Fatalf("populateLevel: %v", err)
	}
	playerState := marshalPerson(t, testPerson{ID: "player:1", HP: 15, MaxHP: 15})
	state := &GameState{
		PlayerState:  playerState,
		CurrentLevel: 1,
		Levels:       make([]*levelState, maxLevels),
		LevelConfig:  cfg,
		SpawnedOnce:  spawnedOnce,
		levelState:   ls,
	}
	state.Levels[0] = ls
	if err := positionPlayerAtStairs(state, stairs.KindDown); err != nil {
		t.Fatalf("positionPlayerAtStairs: %v", err)
	}
	return state
}

func TestApplyStairsTransitionDescendsGeneratesAndStoresLevel2(t *testing.T) {
	state := newLevel1TestState(t)

	msg, err := handleKey(state, ">")
	if err != nil {
		t.Fatalf("handleKey(\">\"): %v", err)
	}
	if !strings.Contains(msg, "descend to level 2") {
		t.Errorf("expected a 'descend to level 2' message, got %q", msg)
	}
	if state.CurrentLevel != 2 {
		t.Fatalf("expected CurrentLevel 2, got %d", state.CurrentLevel)
	}
	if state.Levels[1] == nil {
		t.Fatal("expected level 2 to be stored in Levels[1]")
	}

	// The Player should be standing exactly on level 2's own up-stairs.
	playerObj, err := playerObject(state.PlayerState)
	if err != nil {
		t.Fatalf("playerObject: %v", err)
	}
	foundAtPlayer := false
	for _, raw := range state.FloorItems {
		it, err := itemInfo(raw)
		if err != nil {
			t.Fatalf("itemInfo: %v", err)
		}
		if it.Category != item.CategoryStairs || it.X != playerObj.X || it.Y != playerObj.Y {
			continue
		}
		kind, err := stairs.KindOf(raw)
		if err != nil {
			t.Fatalf("stairs.KindOf: %v", err)
		}
		if kind == stairs.KindUp {
			foundAtPlayer = true
		}
	}
	if !foundAtPlayer {
		t.Error("expected the Player to land exactly on level 2's up-stairs")
	}
}

func TestApplyStairsTransitionAscendRestoresTheExactStoredLevel(t *testing.T) {
	state := newLevel1TestState(t)
	level1 := state.levelState

	if _, err := handleKey(state, ">"); err != nil {
		t.Fatalf("handleKey(\">\"): %v", err)
	}
	if state.CurrentLevel != 2 {
		t.Fatalf("expected CurrentLevel 2 after descending, got %d", state.CurrentLevel)
	}

	msg, err := handleKey(state, "<")
	if err != nil {
		t.Fatalf("handleKey(\"<\"): %v", err)
	}
	if !strings.Contains(msg, "ascend to level 1") {
		t.Errorf("expected an 'ascend to level 1' message, got %q", msg)
	}
	if state.CurrentLevel != 1 {
		t.Fatalf("expected CurrentLevel 1 after ascending back, got %d", state.CurrentLevel)
	}
	if state.levelState != level1 {
		t.Error("expected ascending back to level 1 to restore the exact same stored levelState, not a new one")
	}
}

func TestApplyStairsTransitionRevisitingReturnsTheSameStoredLevel(t *testing.T) {
	state := newLevel1TestState(t)

	if _, err := handleKey(state, ">"); err != nil {
		t.Fatalf("handleKey(\">\") (1st descent): %v", err)
	}
	level2First := state.levelState

	if _, err := handleKey(state, "<"); err != nil {
		t.Fatalf("handleKey(\"<\"): %v", err)
	}
	if _, err := handleKey(state, ">"); err != nil {
		t.Fatalf("handleKey(\">\") (2nd descent): %v", err)
	}

	if state.levelState != level2First {
		t.Error("expected revisiting level 2 to return the exact same stored levelState, not regenerate it")
	}
}

func TestApplyStairsTransitionUpFromLevel1EndsTheGame(t *testing.T) {
	cfg := testLevelConfig()
	ls, err := populateLevel(cfg, 1, map[string]bool{})
	if err != nil {
		t.Fatalf("populateLevel: %v", err)
	}
	playerState := marshalPerson(t, testPerson{ID: "player:1", HP: 15, MaxHP: 15})
	state := &GameState{PlayerState: playerState, CurrentLevel: 1, Levels: make([]*levelState, maxLevels), LevelConfig: cfg, levelState: ls}
	state.Levels[0] = ls
	if err := positionPlayerAtStairs(state, stairs.KindUp); err != nil {
		t.Fatalf("positionPlayerAtStairs: %v", err)
	}

	msg, err := handleKey(state, "<")
	if err != nil {
		t.Fatalf("handleKey(\"<\"): %v", err)
	}
	if !strings.Contains(msg, "leave the dungeon") {
		t.Errorf("expected a 'leave the dungeon' message, got %q", msg)
	}
	got := unmarshalPerson(t, state.PlayerState)
	if !got.Dead {
		t.Error("expected the Player to be marked Dead after leaving the dungeon from level 1")
	}

	again, err := handleKey(state, ".")
	if err != nil {
		t.Fatalf("handleKey(\".\") after game over: %v", err)
	}
	if again != "Game Over" {
		t.Errorf("expected subsequent keys to return \"Game Over\", got %q", again)
	}
}
