package game

import (
	"testing"

	"micro/server/internal/rules"
)

func TestAUniqueCharacterAppearsOncePerRun(t *testing.T) {
	if testing.Short() {
		t.Skip("seed sweep")
	}
	for seed := int64(1); seed <= 10; seed++ {
		spawnedOnce := map[string]bool{}
		thieves := 0
		for level := 1; level <= 10; level++ {
			cfg := testLevelConfig()
			cfg.Seed = seed
			ls, err := populateLevel(testEngine(t), cfg, level, spawnedOnce)
			if err != nil {
				t.Fatal(err)
			}
			thieves += countKind(ls, "thief")
		}
		if thieves != 1 {
			t.Errorf("seed %d: %d thieves in ten levels, want exactly one", seed, thieves)
		}
	}
}

// TestADepartedThiefComesBackDeeperWithHisLoot: leaving the level puts
// him in the roster, carrying what he stole; a new, deeper level may
// bring him back.
func TestADepartedThiefComesBackDeeperWithHisLoot(t *testing.T) {
	state := newLevel1TestState(t)
	thief := testMonster(t, "thief", 0, 0, 20)
	always := *thief.T
	always.ReturnChance = 100
	thief.T = &always
	loot := testItem(t, "torch", 0, 0)
	thief.Inventory = append(thief.Inventory, loot)
	thief.Mind.Attitude = rules.Afraid
	state.Monsters = append(state.Monsters, thief)

	depart(state, thief)
	if state.monsterAt(thief.X, thief.Y) == thief || len(state.Roster) != 1 {
		t.Fatal("he should be off the level and in the roster")
	}
	if thief.Mind.Attitude != rules.Neutral || thief.Mind.Fleeing {
		t.Errorf("having escaped, he should have forgotten his fear: %q", thief.Mind.Attitude)
	}

	mustKey(t, state, ">") // level 2, made for the first time
	if len(state.Roster) != 0 || !containsEntity(state.Monsters, thief) {
		t.Fatal("he should be back, on level 2")
	}
	if thief.Inventory[len(thief.Inventory)-1] != loot || thief.Mind.Attitude != rules.Neutral {
		t.Errorf("he should still have the loot, and have calmed down: %q", thief.Mind.Attitude)
	}
	if _, ok := state.Map.RoomAt(thief.X, thief.Y); !ok {
		t.Errorf("he should be on a room cell, at (%d,%d)", thief.X, thief.Y)
	}
}

func TestADepartedCharacterNeverReturnsHigherUp(t *testing.T) {
	state := newLevel1TestState(t)
	mustKey(t, state, ">")
	thief := testMonster(t, "thief", 0, 0, 20)
	always := *thief.T
	always.ReturnChance = 100
	thief.T = &always
	state.Monsters = append(state.Monsters, thief)
	depart(state, thief) // left level 2
	ls, _ := populateLevel(testEngine(t), testLevelConfig(), 2, map[string]bool{})
	bringBack(state, ls, 2)
	if containsEntity(ls.Monsters, thief) {
		t.Error("only a deeper level brings him back")
	}
}

func containsEntity(list []*rules.Entity, e *rules.Entity) bool {
	for _, x := range list {
		if x == e {
			return true
		}
	}
	return false
}
