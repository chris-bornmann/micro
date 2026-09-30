// Calls into every other service. Each function here is JSON-in,
// JSON-out and touches no coordinator state, so each is independently
// the seam where that service could become a network call.
package main

import (
	"encoding/json"
	"fmt"

	"map_api/armor"
	"map_api/flask"
	"map_api/goblin"
	"map_api/mapdata"
	"map_api/mapgen"
	"map_api/mummy"
	"map_api/player"
	"map_api/potion"
	"map_api/scroll"
	"map_api/stairs"
	"map_api/torch"
	"map_api/weapon"
)

// generateLevel produces a new level. Today it just calls mapgen.Generate
// in-process; this is the seam where that becomes an HTTP call to a
// separate map-generation service later.
func generateLevel(cfg mapgen.Config) (*mapdata.Map, error) {
	return mapgen.Generate(cfg)
}

// initPlayer calls the Player service's Init and returns its raw response
// unmodified — the same JSON-in/JSON-out shape it will speak over HTTP once
// Player is a standalone service.
func initPlayer(m *mapdata.Map) ([]byte, error) {
	mapJSON, err := json.Marshal(m)
	if err != nil {
		return nil, fmt.Errorf("marshal map: %w", err)
	}
	return player.Init(mapJSON)
}

// initGoblin calls the Goblin service's plain Init — a bare, unarmed
// goblin. Registered as monsterServices["goblin"].Init, the default used
// for every goblin populateLevel spawns except the first on a level
// (see initGoblinArmed), so a count > 1 doesn't produce multiple copies
// of the one cursed starting sword. spawnIndex is threaded straight
// through to goblin.Init — see its own doc comment for why populateLevel
// must pass a distinct index per goblin spawned on the same level.
func initGoblin(m *mapdata.Map, spawnIndex int) ([]byte, error) {
	mapJSON, err := json.Marshal(m)
	if err != nil {
		return nil, fmt.Errorf("marshal map: %w", err)
	}
	return goblin.Init(mapJSON, spawnIndex)
}

// initGoblinArmed calls the Goblin service's InitArmed with
// weapon.GoblinStarter's stats — the coordinator-side seam that decides
// the first Goblin spawned on a level is pre-equipped with its cursed
// starting sword (see populateLevel).
func initGoblinArmed(m *mapdata.Map, spawnIndex int) ([]byte, error) {
	mapJSON, err := json.Marshal(m)
	if err != nil {
		return nil, fmt.Errorf("marshal map: %w", err)
	}
	statsJSON, err := json.Marshal(weapon.GoblinStarter())
	if err != nil {
		return nil, fmt.Errorf("marshal goblin starter weapon stats: %w", err)
	}
	return goblin.InitArmed(mapJSON, statsJSON, spawnIndex)
}

// initMummy calls the Mummy service's Init, the same way initPlayer
// calls Player's, and returns its raw response unmodified.
func initMummy(m *mapdata.Map, spawnIndex int) ([]byte, error) {
	mapJSON, err := json.Marshal(m)
	if err != nil {
		return nil, fmt.Errorf("marshal map: %w", err)
	}
	return mummy.Init(mapJSON, spawnIndex)
}

// initPotion calls the Potion service's Init, the same way initPlayer/
// initGoblin call theirs, and returns its raw response unmodified.
func initPotion(m *mapdata.Map) ([]byte, error) {
	mapJSON, err := json.Marshal(m)
	if err != nil {
		return nil, fmt.Errorf("marshal map: %w", err)
	}
	return potion.Init(mapJSON)
}

// initInvisibilityPotion calls the Potion service's InitInvisibility, the
// same way initPotion calls Init.
func initInvisibilityPotion(m *mapdata.Map) ([]byte, error) {
	mapJSON, err := json.Marshal(m)
	if err != nil {
		return nil, fmt.Errorf("marshal map: %w", err)
	}
	return potion.InitInvisibility(mapJSON)
}

// initWeapon calls the Weapon service's Init, the same way initPotion
// calls Potion's, and returns its raw response unmodified.
func initWeapon(m *mapdata.Map) ([]byte, error) {
	mapJSON, err := json.Marshal(m)
	if err != nil {
		return nil, fmt.Errorf("marshal map: %w", err)
	}
	return weapon.Init(mapJSON)
}

// initTorch calls the Torch service's Init, the same way initWeapon
// calls Weapon's, and returns its raw response unmodified.
func initTorch(m *mapdata.Map) ([]byte, error) {
	mapJSON, err := json.Marshal(m)
	if err != nil {
		return nil, fmt.Errorf("marshal map: %w", err)
	}
	return torch.Init(mapJSON)
}

// initScroll calls the Scroll service's InitTeleport, the same way
// initTorch calls Torch's, and returns its raw response unmodified.
func initScroll(m *mapdata.Map) ([]byte, error) {
	mapJSON, err := json.Marshal(m)
	if err != nil {
		return nil, fmt.Errorf("marshal map: %w", err)
	}
	return scroll.InitTeleport(mapJSON)
}

// initArmor calls the Armor service's InitShoesOfSneaking, the same way
// initScroll calls Scroll's, and returns its raw response unmodified.
func initArmor(m *mapdata.Map) ([]byte, error) {
	mapJSON, err := json.Marshal(m)
	if err != nil {
		return nil, fmt.Errorf("marshal map: %w", err)
	}
	return armor.InitShoesOfSneaking(mapJSON)
}

// initFlask calls the Flask service's Init, the same way initArmor
// calls Armor's, and returns its raw response unmodified.
func initFlask(m *mapdata.Map) ([]byte, error) {
	mapJSON, err := json.Marshal(m)
	if err != nil {
		return nil, fmt.Errorf("marshal map: %w", err)
	}
	return flask.Init(mapJSON)
}

// initStairsUp/initStairsDown call the Stairs service's InitUp/InitDown,
// the same way initFlask calls Flask's, and return their raw response
// unmodified.
func initStairsUp(m *mapdata.Map) ([]byte, error) {
	mapJSON, err := json.Marshal(m)
	if err != nil {
		return nil, fmt.Errorf("marshal map: %w", err)
	}
	return stairs.InitUp(mapJSON)
}
func initStairsDown(m *mapdata.Map) ([]byte, error) {
	mapJSON, err := json.Marshal(m)
	if err != nil {
		return nil, fmt.Errorf("marshal map: %w", err)
	}
	return stairs.InitDown(mapJSON)
}
