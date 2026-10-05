package game

import (
	"fmt"
	"math/rand"

	"micro/server/internal/levels"
	"micro/server/internal/mapdata"
	"micro/server/internal/mapgen"
	"micro/server/internal/rules"
	"micro/server/internal/vision"
)

// maxLevels is the deepest a Player can go — level maxLevels has stairs
// up but no stairs down (see populateLevel), so there's nowhere further
// to descend.
const maxLevels = rules.DeepestLevel

// populateLevel generates a brand-new Map for levelNum and spawns its
// monsters, items and stairs onto it. It never touches the Player;
// callers decide where to place them afterward (see
// positionPlayerAtStairs).
//
// What spawns is entirely the content's say: every monster template rolls
// its own count by its spawn model, every item template with a fixed
// spawn places that many, and the stairs templates are placed by
// direction. Templates are visited sorted by ID, so a given -seed always
// produces the same level.
//
// No two monsters start on the same tile, and none starts on a staircase,
// where the Player arrives: the stairs are placed first, and each monster
// keeps drawing cells from its own seeded stream until it finds a free
// one. Traps then go on cells still free of all of those, and of each
// other.
//
// spawnedOnce records, per monster template, whether it has ever spawned
// on any level reached so far this run (see GameState.SpawnedOnce), and
// is updated in place.
func populateLevel(eng *rules.Engine, cfg mapgen.Config, levelNum int, spawnedOnce map[string]bool) (*levelState, error) {
	return makeLevel(eng, cfg, levelNum, spawnedOnce, nil)
}

// makeLevel is populateLevel for a level that may be special: built from
// spec (a hand-drawn level, a maze) rather than by the usual generator,
// with whatever spec places put where it says, then the random monsters,
// items and traps its Populate asks for. Stairs it does not place go
// anywhere, as on an ordinary level.
func makeLevel(eng *rules.Engine, cfg mapgen.Config, levelNum int, spawnedOnce map[string]bool, spec *rules.LevelSpec) (*levelState, error) {
	if cfg.Seed != 0 {
		// Level 1 uses cfg.Seed itself; every deeper level is offset so it
		// is a distinct, reproducible map for a given -seed.
		cfg.Seed += int64(levelNum - 1)
	}
	var m *mapdata.Map
	var layout *levels.Layout
	var err error
	if spec != nil {
		layout, err = levels.Build(spec, cfg)
		if layout != nil {
			m = layout.Map
		}
	} else {
		m, err = mapgen.Generate(cfg)
	}
	if err != nil {
		return nil, err
	}
	if len(m.Rooms) == 0 {
		return nil, fmt.Errorf("level %d has no rooms to spawn in", levelNum)
	}
	if m.Seed == 0 {
		m.Seed = rand.Int63() // a drawn level in a game with no -seed
	}
	ls := &levelState{Map: m, Visited: newVisibility()}

	// Nothing placed here shares a tile: the stairs keep apart, monsters
	// keep off the stairs and each other, and traps off all of them.
	taken := map[mapdata.Point]bool{}
	free := func(x, y int) bool { return !taken[mapdata.Point{X: x, Y: y}] }

	placedStairs := map[string]bool{}
	if layout != nil {
		placeRNG := rand.New(rand.NewSource(m.Seed + 15))
		for _, p := range layout.Placed {
			t := eng.Cat.Get(p.ID)
			if t.Stairs == "down" && levelNum == maxLevels {
				continue
			}
			if t.Category.IsCreature() {
				if t.Unique && spawnedOnce[t.ID] {
					continue // the one and only is already about
				}
				ls.Monsters = append(ls.Monsters, eng.Create(t, p.X, p.Y, placeRNG, nil))
				spawnedOnce[t.ID] = true
			} else {
				ls.FloorItems = append(ls.FloorItems, rules.New(t, p.X, p.Y, placeRNG))
			}
			if t.Stairs != "" {
				placedStairs[t.Stairs] = true
			}
			taken[mapdata.Point{X: p.X, Y: p.Y}] = true
		}
	}
	pop := rules.PopulateAll
	if layout != nil {
		pop = layout.Populate
	}

	for _, t := range eng.Cat.ByCategory(rules.CategoryStairs) {
		if t.Stairs == "down" && levelNum == maxLevels || placedStairs[t.Stairs] {
			continue
		}
		s := eng.SpawnWhere(t, m, 0, nil, free)
		taken[mapdata.Point{X: s.X, Y: s.Y}] = true
		ls.FloorItems = append(ls.FloorItems, s)
	}

	// One seeded stream drives every monster's spawn-count roll, in
	// template order, so the whole roster is reproducible.
	countRNG := rand.New(rand.NewSource(m.Seed + 12))
	for _, t := range eng.Cat.ByCategory(rules.CategoryMonster) {
		if !pop.Monsters {
			break
		}
		count := rules.SpawnCount(t, levelNum, spawnedOnce[t.ID], countRNG)
		for i := 0; i < count; i++ {
			withEquipment := func(es rules.EquipSpec) bool {
				return es.LimitPerLevel == 0 || i < es.LimitPerLevel
			}
			mon := eng.SpawnWhere(t, m, i, withEquipment, free)
			taken[mapdata.Point{X: mon.X, Y: mon.Y}] = true
			ls.Monsters = append(ls.Monsters, mon)
		}
		if count > 0 {
			spawnedOnce[t.ID] = true
		}
	}

	// Traps: on free room cells — off the stairs, the monsters and each
	// other — so far as the level has room.
	trapRNG := rand.New(rand.NewSource(m.Seed + 13))
	for _, t := range eng.Cat.ByCategory(rules.CategoryTrap) {
		if !pop.Traps {
			break
		}
		for i := 0; i < rules.SpawnCount(t, levelNum, false, trapRNG); i++ {
			trap := eng.SpawnWhere(t, m, i, nil, free)
			taken[mapdata.Point{X: trap.X, Y: trap.Y}] = true
			ls.FloorItems = append(ls.FloorItems, trap)
		}
	}

	for _, t := range eng.Cat.All() {
		if !pop.Items || t.Category.IsCreature() || t.Spawn.Model != rules.SpawnFixed {
			continue
		}
		for i := 0; i < rules.SpawnCount(t, levelNum, false, nil); i++ {
			ls.FloorItems = append(ls.FloorItems, eng.Spawn(t, m, i, nil))
		}
	}

	return ls, nil
}

// stairsAmong returns the stairs leading dir ("up" or "down") among items.
func stairsAmong(items []*rules.Entity, dir string) *rules.Entity {
	for _, it := range items {
		if it.T.Stairs == dir {
			return it
		}
	}
	return nil
}

// positionPlayerAtStairs moves the Player — nothing else about them
// changes — onto the current level's stairs leading dir. Used on arrival:
// appearing at the down-stairs when the Player just came up, or the
// up-stairs when they just came down, so they land exactly where they'd
// expect and pressing the key they arrived on takes them right back.
func positionPlayerAtStairs(state *GameState, dir string) error {
	s := stairsAmong(state.FloorItems, dir)
	if s == nil {
		return fmt.Errorf("no stairs %s on level %d to place the Player at", dir, state.CurrentLevel)
	}
	state.Player.X, state.Player.Y = s.X, s.Y
	// A monster may have wandered onto the stairs since the Player was
	// last here. Two creatures never share a tile, so it steps aside to
	// the nearest free one.
	w := state.world()
	for _, m := range w.LiveMonsters() {
		if m.X == s.X && m.Y == s.Y {
			if x, y, ok := w.NearestFreeTile(s.X, s.Y, nil); ok {
				m.X, m.Y = x, y
			}
		}
	}
	return nil
}

// applyStairsTransition moves the Player between levels: repoints
// state's embedded *levelState at the target depth's levelState
// (generating and storing it first if this is the first time that depth
// has been reached), then places the Player at the stairs leading back
// the way they came. Going up from level 1 ends the game instead — there
// is no level 0 — mechanically the same terminal state as dying.
func applyStairsTransition(state *GameState, dir string) (string, error) {
	current := max(state.CurrentLevel, 1)
	if dir == "up" && current == 1 {
		state.Player.Dead = true
		return "You climb the stairs up and leave the dungeon behind. Game Over.", nil
	}

	target, arriveVia, verb := current+1, "up", "descend"
	if dir == "up" {
		target, arriveVia, verb = current-1, "down", "ascend"
	}
	if err := enterLevel(state, target); err != nil {
		return "", err
	}
	if err := positionPlayerAtStairs(state, arriveVia); err != nil {
		return "", err
	}
	revealAroundPlayer(state)
	return fmt.Sprintf("You %s to level %d.", verb, target), nil
}

// fallToNextLevel drops the Player through a trap door to a random free
// tile on the level below — which is generated, if this is the first time
// anyone has reached it.
func fallToNextLevel(state *GameState) (string, error) {
	target := max(state.CurrentLevel, 1) + 1
	if err := enterLevel(state, target); err != nil {
		return "", err
	}
	w := state.world()
	if x, y, ok := w.RandomUnoccupiedTile(); ok {
		state.Player.X, state.Player.Y = x, y
	}
	revealAroundPlayer(state)
	return fmt.Sprintf("You fall to level %d.", target), nil
}

// enterLevel makes target the current level, generating and storing it
// first if it has never been reached. It repoints state's embedded
// *levelState at the stored level — nothing is copied in either direction,
// since that *is* the level's live data from here on. Where the Player
// lands on it is the caller's business.
func enterLevel(state *GameState, target int) error {
	if state.Levels == nil {
		state.Levels = make([]*levelState, maxLevels)
	}
	if state.Levels[target-1] == nil {
		if state.SpawnedOnce == nil {
			state.SpawnedOnce = map[string]bool{}
		}
		ls, err := makeLevel(state.eng, state.LevelConfig, target, state.SpawnedOnce, state.Special[target])
		if err != nil {
			return err
		}
		bringBack(state, ls, target)
		state.Levels[target-1] = ls
	}
	state.levelState = state.Levels[target-1]
	state.CurrentLevel = target
	return nil
}

// revealAroundPlayer marks what the Player can see from where they stand
// as explored: the room or corridor they are in, and — in a dark room —
// the tiles their light (or the dark itself) lets them see.
func revealAroundPlayer(state *GameState) {
	p := state.Player
	torch := state.eng.Reg.Has(p, "light")
	state.Visited.reveal(state.Map, p.X, p.Y)
	lit, _, _ := vision.CurrentlyLit(*state.Map, p.X, p.Y, torch)
	state.Visited.rememberLitTiles(lit)
	state.Visited.seeDoors(state.Map, p.X, p.Y, torch)
}

// depart takes a unique character off the current level, alive, and keeps
// it in the roster with everything it carries and remembers. Having got
// away, it calms down at once: wherever it turns up next — a deeper level,
// or summoned — it is no longer afraid or running.
func depart(state *GameState, e *rules.Entity) {
	for i, m := range state.Monsters {
		if m == e {
			calmDown(e)
			state.Monsters = append(state.Monsters[:i], state.Monsters[i+1:]...)
			state.Roster = append(state.Roster, rosterEntry{Who: e, Left: max(state.CurrentLevel, 1)})
			return
		}
	}
}

// calmDown is a character who has escaped forgetting its fear: no longer
// afraid — back to how its template feels about the player — nor running
// anywhere, nor tracking the player it got away from. What it carries,
// and whether the player ever attacked it, it remembers.
func calmDown(e *rules.Entity) {
	m := &e.Mind
	m.Seen, m.Fleeing, m.FleeRoom, m.Aware = false, false, -1, false
	if m.Attitude == rules.Afraid {
		m.Attitude = e.T.Attitude
		if m.Attitude == "" {
			m.Attitude = rules.Hostile
		}
	}
}

// bringBack gives each rostered character a chance to turn up on ls, a
// level being made for the first time, if it is deeper than the one the
// character left. A returner lands on a free room cell, as calm as it was
// when it left (see depart).
func bringBack(state *GameState, ls *levelState, level int) {
	rng := rand.New(rand.NewSource(ls.Map.Seed + 14))
	var staying []rosterEntry
	for _, r := range state.Roster {
		if level <= r.Left || rng.Intn(100) >= r.Who.T.ReturnChance {
			staying = append(staying, r)
			continue
		}
		e := r.Who
		e.X, e.Y = freeRoomCell(ls, rng)
		ls.Monsters = append(ls.Monsters, e)
	}
	state.Roster = staying
}

// freeRoomCell is a random room cell on ls with no monster or fixed
// feature on it — falling back to any room cell on a level with no room.
func freeRoomCell(ls *levelState, rng *rand.Rand) (int, int) {
	taken := map[mapdata.Point]bool{}
	for _, m := range ls.Monsters {
		taken[mapdata.Point{X: m.X, Y: m.Y}] = true
	}
	for _, it := range ls.FloorItems {
		taken[mapdata.Point{X: it.X, Y: it.Y}] = true
	}
	x, y := rules.RandomRoomCell(ls.Map, rng)
	for tries := 0; taken[mapdata.Point{X: x, Y: y}] && tries < 1000; tries++ {
		x, y = rules.RandomRoomCell(ls.Map, rng)
	}
	return x, y
}

// assignSpecial settles, for a new game, which special level goes at
// which depth: each, if its chance comes up, at its depth — or a depth
// picked from its range, moving on to the next free one in the range if
// another special level is there already. A level whose range is full is
// left out. seed reproduces the choice; 0 means any.
func assignSpecial(specs []*rules.LevelSpec, seed int64) map[int]*rules.LevelSpec {
	if seed == 0 {
		seed = rand.Int63()
	}
	rng := rand.New(rand.NewSource(seed + 51))
	out := map[int]*rules.LevelSpec{}
	for _, s := range specs {
		if rng.Intn(100) >= s.Chance {
			continue
		}
		span := s.Depths.Max - s.Depths.Min + 1
		start := rng.Intn(span)
		for i := 0; i < span; i++ {
			d := s.Depths.Min + (start+i)%span
			if out[d] == nil {
				out[d] = s
				break
			}
		}
	}
	return out
}
