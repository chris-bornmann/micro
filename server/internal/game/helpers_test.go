package game

import (
	"testing"

	"micro/server/internal/mapdata"
	"micro/server/internal/mapgen"
	"micro/server/internal/rules"
	"micro/server/internal/rules/rulestest"
)

// testEngine is the engine every game runs on, with the real content —
// a copy of it on which every melee blow lands, so tests about what a
// blow does are not at the mercy of the to-hit roll.
func testEngine(t *testing.T) *rules.Engine {
	t.Helper()
	eng, err := defaultEngine()
	if err != nil {
		t.Fatalf("load content: %v", err)
	}
	cp := *eng
	cp.HitRoll = rulestest.AlwaysHit
	return &cp
}

// newTestGameState builds a single-level GameState for a test — the
// fixture counterpart to NewGame. monsters and floor may be nil.
func newTestGameState(t *testing.T, m *mapdata.Map, player *rules.Entity, monsters, floor []*rules.Entity) *GameState {
	t.Helper()
	ls := &levelState{Map: m, Monsters: monsters, FloorItems: floor, Visited: newVisibility()}
	return &GameState{eng: testEngine(t), Player: player, CurrentLevel: 1, Levels: []*levelState{ls}, levelState: ls}
}

// itemsTestMap is a single room big enough to place a player and an item
// without any corridor/movement concerns.
func itemsTestMap() *mapdata.Map { return rulestest.Room() }

// testPlayer is a Player at (x, y) with hp HP and all-zero attributes.
func testPlayer(t *testing.T, x, y, hp int) *rules.Entity {
	t.Helper()
	return rulestest.New(t, testEngine(t), "player", x, y, hp)
}

// testMonster is a monster of template id at (x, y) with hp HP, all-zero
// attributes and nothing equipped.
func testMonster(t *testing.T, id string, x, y, hp int) *rules.Entity {
	t.Helper()
	m := rulestest.New(t, testEngine(t), id, x, y, hp)
	m.Equipped = nil
	return m
}

// testItem is an item of template id lying at (x, y).
func testItem(t *testing.T, id string, x, y int) *rules.Entity {
	t.Helper()
	return rulestest.New(t, testEngine(t), id, x, y, 0)
}

// carrying puts items in p's inventory and returns p.
func carrying(p *rules.Entity, items ...*rules.Entity) *rules.Entity {
	p.Inventory = append(p.Inventory, items...)
	return p
}

// deadGoblin is a monster already dead, for tests that want a monster
// list without a monster taking turns.
func deadGoblin(t *testing.T) *rules.Entity {
	t.Helper()
	g := testMonster(t, "goblin", 0, 0, 0)
	g.Dead = true
	return g
}

func testLevelConfig() mapgen.Config {
	cfg := mapgen.DefaultConfig()
	cfg.Width, cfg.Height, cfg.NumRooms = 30, 30, 3
	cfg.Seed = 99
	return cfg
}

// newLevel1TestState is a real generated level 1 with a 15-HP Player
// standing on its down-stairs.
func newLevel1TestState(t *testing.T) *GameState {
	t.Helper()
	cfg := testLevelConfig()
	spawnedOnce := map[string]bool{}
	ls, err := populateLevel(testEngine(t), cfg, 1, spawnedOnce)
	if err != nil {
		t.Fatalf("populateLevel: %v", err)
	}
	state := &GameState{
		eng:          testEngine(t),
		Player:       testPlayer(t, 0, 0, 15),
		CurrentLevel: 1,
		Levels:       make([]*levelState, maxLevels),
		LevelConfig:  cfg,
		SpawnedOnce:  spawnedOnce,
		levelState:   ls,
	}
	state.Levels[0] = ls
	if err := positionPlayerAtStairs(state, "down"); err != nil {
		t.Fatalf("positionPlayerAtStairs: %v", err)
	}
	return state
}

// mustKey is handleKey for a test that expects no error.
func mustKey(t *testing.T, state *GameState, key string) string {
	t.Helper()
	msg, err := handleKey(state, key)
	if err != nil {
		t.Fatalf("handleKey(%q): %v", key, err)
	}
	return msg
}

func names(items []*rules.Entity) []string {
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = it.Name
	}
	return out
}
