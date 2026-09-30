package main

import (
	"encoding/json"
	"fmt"
	"math/rand"

	"map_api/item"
	"map_api/mapdata"
	"map_api/mapgen"
	"map_api/stairs"
	"map_api/vision"
)

// maxLevels is the deepest a Player can go — level maxLevels has stairs
// up but no stairs down (see populateLevel), so there's nowhere further
// to descend.
const maxLevels = 25

// populateLevel generates a brand-new Map for levelNum and spawns its
// monsters/items/stairs onto it — the same sequence main() used to
// build inline for the one level that used to exist, now reusable for
// every depth a Player reaches. Never touches the Player; callers
// decide where to place them afterward (see positionPlayerAtStairs).
//
// spawnedOnce records, per monster Kind, whether that kind has ever
// spawned on any level reached so far this run (see
// GameState.SpawnedOnce) — mutated in place as a side effect (a map is
// a reference type), so callers just pass state.SpawnedOnce directly
// and see it updated without needing a return value for it.
func populateLevel(cfg mapgen.Config, levelNum int, spawnedOnce map[string]bool) (*levelState, error) {
	if cfg.Seed != 0 {
		// level 1 = cfg.Seed+0 (unchanged from before multi-level play),
		// every deeper level offset so it's a distinct, reproducible map
		// for a given -seed rather than colliding with level 1's.
		cfg.Seed += int64(levelNum - 1)
	}
	m, err := generateLevel(cfg)
	if err != nil {
		return nil, err
	}
	ls := &levelState{Map: m, Visited: newVisibility()}

	// Seeded off the level's own map seed, at the next free offset after
	// stairs (m.Seed+10/+11) — drives every monster kind's spawn-count
	// roll, in monsterKinds' fixed order, so the whole roster is
	// reproducible for a given -seed the same way each individual
	// monster's own placement already is.
	spawnRNG := rand.New(rand.NewSource(m.Seed + 12))
	for _, kind := range monsterKinds {
		svc := monsterServices[kind]
		count := svc.SpawnCount(levelNum, spawnedOnce[kind], spawnRNG)
		for i := 0; i < count; i++ {
			init := svc.Init
			if kind == "goblin" && i == 0 {
				// Only the first goblin on a level carries the one
				// cursed starting sword; see initGoblinArmed.
				init = initGoblinArmed
			}
			raw, err := init(m, i)
			if err != nil {
				return nil, err
			}
			ls.Monsters = append(ls.Monsters, MonsterState{Kind: kind, State: raw})
		}
		if count > 0 {
			spawnedOnce[kind] = true
		}
	}

	for _, initItem := range []func(*mapdata.Map) ([]byte, error){
		initPotion, initInvisibilityPotion, initWeapon, initTorch, initScroll, initArmor, initFlask,
	} {
		raw, err := initItem(m)
		if err != nil {
			return nil, err
		}
		ls.FloorItems = append(ls.FloorItems, raw)
	}

	stairsUpState, err := initStairsUp(m)
	if err != nil {
		return nil, err
	}
	ls.FloorItems = append(ls.FloorItems, stairsUpState)

	if levelNum != maxLevels {
		stairsDownState, err := initStairsDown(m)
		if err != nil {
			return nil, err
		}
		ls.FloorItems = append(ls.FloorItems, stairsDownState)
	}

	return ls, nil
}

// positionPlayerAtStairs relocates the Player — HP/Inventory/Attributes/
// etc. all untouched — to wherever the given stairs Kind sits on the
// CURRENT level. Used on arrival: appearing at the down-stairs when the
// Player just came up, or the up-stairs when they just came down, so
// they land exactly where they'd expect and pressing the key they
// arrived on takes them right back. Reuses relocateItem directly — it's
// a generic X/Y field overwrite on any JSON blob, not actually
// item-specific despite its name and doc comment.
func positionPlayerAtStairs(state *GameState, kind stairs.Kind) error {
	for _, raw := range state.FloorItems {
		it, err := itemInfo(raw)
		if err != nil {
			return err
		}
		if it.Category != item.CategoryStairs {
			continue
		}
		gotKind, err := stairs.KindOf(raw)
		if err != nil {
			return err
		}
		if gotKind != kind {
			continue
		}
		relocated, err := relocateItem(state.PlayerState, it.X, it.Y)
		if err != nil {
			return err
		}
		state.PlayerState = relocated
		return nil
	}
	return fmt.Errorf("hack: no stairs %q found on level %d to place the Player at", kind, state.CurrentLevel)
}

// killPlayer marks the Player Dead via the same generic-field-edit
// technique relocateItem uses for X/Y, reusing handleKey's existing
// playerObj.Dead freeze/"Game Over" path. Used when the Player climbs
// the stairs up from level 1 and leaves the dungeon — mechanically the
// same terminal state as dying in combat, just a different one-time
// message.
func killPlayer(state *GameState) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(state.PlayerState, &fields); err != nil {
		return fmt.Errorf("parse player: %w", err)
	}
	deadJSON, err := json.Marshal(true)
	if err != nil {
		return err
	}
	fields["dead"] = deadJSON
	out, err := json.Marshal(fields)
	if err != nil {
		return fmt.Errorf("marshal player: %w", err)
	}
	state.PlayerState = out
	return nil
}

// applyStairsTransition moves the Player between levels: repoints
// state's embedded *levelState at the target depth's levelState
// (generating and storing it first if this is the first time that
// depth has been reached), then places the Player at the stairs
// leading back the way they came. Going up from level 1 (or an
// unset/zero CurrentLevel, e.g. an older test fixture that predates
// multi-level play) ends the game instead — there is no level 0.
func applyStairsTransition(state *GameState, kind stairs.Kind) (string, error) {
	current := state.CurrentLevel
	if current == 0 {
		current = 1
	}
	if kind == stairs.KindUp && current == 1 {
		if err := killPlayer(state); err != nil {
			return "", err
		}
		return "You climb the stairs up and leave the dungeon behind. Game Over.", nil
	}
	if state.Levels == nil {
		state.Levels = make([]*levelState, maxLevels)
	}

	target, arriveVia, verb := current+1, stairs.KindUp, "descend"
	if kind == stairs.KindUp {
		target, arriveVia, verb = current-1, stairs.KindDown, "ascend"
	}

	if state.Levels[target-1] == nil {
		if state.SpawnedOnce == nil {
			state.SpawnedOnce = map[string]bool{}
		}
		ls, err := populateLevel(state.LevelConfig, target, state.SpawnedOnce)
		if err != nil {
			return "", err
		}
		state.Levels[target-1] = ls
	}
	// Repoint at the target level's own levelState — nothing to copy in
	// either direction, since this *is* the target level's live data
	// from here on, the same object stored in state.Levels.
	state.levelState = state.Levels[target-1]
	state.CurrentLevel = target

	if err := positionPlayerAtStairs(state, arriveVia); err != nil {
		return "", err
	}
	playerObj, err := playerObject(state.PlayerState)
	if err != nil {
		return "", err
	}
	state.Visited.reveal(state.Map, playerObj.X, playerObj.Y)
	litTiles, _, _ := vision.CurrentlyLit(*state.Map, playerObj.X, playerObj.Y, playerObj.Lit)
	state.Visited.rememberLitTiles(litTiles)

	return fmt.Sprintf("You %s to level %d.", verb, target), nil
}
