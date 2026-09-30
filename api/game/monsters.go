package game

import (
	"encoding/json"
	"fmt"
	"math/rand"

	"map_api/goblin"
	"map_api/item"
	"map_api/mapdata"
	"map_api/mummy"
	"map_api/overlay"
	"map_api/weapon"
)

// MonsterState is one live-or-dead monster's own opaque state plus
// which service owns it. hack never parses State itself beyond the
// generic overlay.Object subset (monsterObject); Kind is purely a
// routing key into monsterServices.
type MonsterState struct {
	Kind  string          `json:"kind"`
	State json.RawMessage `json:"state"`
}

// monsterService is hack's own adapter over one monster package's
// exported functions — not a Go interface goblin/mummy implement (they
// stay fully independent packages, unaware of each other or of any
// shared interface, so either can be extracted into its own process
// later with no change to itself). hack builds one adapter per kind and
// dispatches by Kind, the same way a real coordinator would route by
// service name once these are network calls instead of function calls.
type monsterService struct {
	DisplayName string // "Goblin", "Mummy" — for status messages
	// Init spawns one bare instance of this kind on m — for goblin this
	// is the unarmed variant; populateLevel (levels.go) special-cases
	// the first goblin spawned on a level to use initGoblinArmed
	// instead, so only one "cursed starting sword" ever exists per
	// level regardless of how many goblins SpawnCount rolls. spawnIndex
	// is this instance's position (0, 1, 2, ...) among however many of
	// this kind populateLevel spawns on the same level — each
	// implementation folds it into its own rng seed alongside m.Seed so
	// multiple instances on one level don't all roll the identical
	// spawn position/stats (see goblin.spawn's doc comment).
	Init func(m *mapdata.Map, spawnIndex int) ([]byte, error)
	// SpawnCount decides how many of this kind spawn on dungeonLevel —
	// see spawn.GenericSpawnCount/spawn.SingleShotShouldSpawn. Each
	// package owns its own tuning; hack only ever calls this one
	// function, never learning whether a kind is common or single-shot.
	SpawnCount func(dungeonLevel int, alreadySpawned bool, rng *rand.Rand) int
	Action     func(selfJSON, augmentedMapJSON []byte) ([]byte, error)
	Defend     func(selfJSON, attackJSON []byte) ([]byte, error)
	Tick       func(selfJSON []byte) ([]byte, error)
	Corpse     func(selfJSON []byte) ([]byte, error)
	// Status returns a status.Field list describing the monster's own
	// current condition (see player.Status). Not called anywhere in hack
	// yet — no monster-inspection UI exists — but registered here so a
	// future debug picker can call it generically by Kind instead of
	// needing its own per-kind branching.
	Status func(selfJSON []byte) ([]byte, error)
}

// monsterKinds lists every registered monster Kind in a fixed order —
// populateLevel iterates this (not monsterServices directly) when
// rolling spawn counts, since Go's map iteration order is randomized
// and level content needs to stay reproducible for a given -seed.
var monsterKinds = []string{"goblin", "mummy"}

var monsterServices = map[string]monsterService{
	"goblin": {
		DisplayName: "Goblin",
		Init:        initGoblin,
		SpawnCount:  goblin.SpawnCount,
		Action:      goblin.Action,
		Defend:      goblin.Defend,
		Tick:        goblin.Tick,
		Corpse:      goblin.Corpse,
		Status:      goblin.Status,
	},
	"mummy": {
		DisplayName: "Mummy",
		Init:        initMummy,
		SpawnCount:  mummy.SpawnCount,
		Action:      mummy.Action,
		Defend:      mummy.Defend,
		Tick:        mummy.Tick,
		Corpse:      mummy.Corpse,
		Status:      mummy.Status,
	},
}

// corpseResult mirrors the {"self":...,"item":...,"wielded":...} shape
// every monster's own Corpse function returns — decoded generically
// here rather than importing each package's own CorpseResult type, the
// same way defendEnvelope/tickEnvelope already avoid depending on
// producer-specific types. Wielded is nil for any monster that doesn't
// wield anything (e.g. a mummy).
type corpseResult struct {
	Self    json.RawMessage   `json:"self"`
	Item    json.RawMessage   `json:"item"`
	Wielded *item.WeaponStats `json:"wielded,omitempty"`
}

// monsterObject decodes just the generic overlay.Object subset out of a
// monster's raw state — works for any monster kind, since every one
// embeds overlay.Object the same way player.person/goblin.monster/
// mummy.creature do.
func monsterObject(state []byte) (overlay.Object, error) {
	var o overlay.Object
	if err := json.Unmarshal(state, &o); err != nil {
		return overlay.Object{}, fmt.Errorf("parse monster: %w", err)
	}
	return o, nil
}

// liveMonsterObjects returns the overlay.Object for every monster in
// state.Monsters that isn't Dead.
func liveMonsterObjects(state *GameState) ([]overlay.Object, error) {
	var objs []overlay.Object
	for _, ms := range state.Monsters {
		obj, err := monsterObject(ms.State)
		if err != nil {
			return nil, err
		}
		if !obj.Dead {
			objs = append(objs, obj)
		}
	}
	return objs, nil
}

// monsterIndexAt returns the index into state.Monsters of the live
// monster occupying (x,y), if any — used to route an attack (found by
// position, the only thing the attacker's own Result reports) to the
// right monster.
func monsterIndexAt(state *GameState, x, y int) (int, bool) {
	for i, ms := range state.Monsters {
		obj, err := monsterObject(ms.State)
		if err != nil || obj.Dead {
			continue
		}
		if obj.X == x && obj.Y == y {
			return i, true
		}
	}
	return -1, false
}

// attackMonster sends attackJSON (a JSON-encoded action.Result, as
// produced by attackResult-style logic in whichever package built it)
// to state.Monsters[idx]'s own Defend (resolved via its Kind), applies
// the outcome, and finalizes death if it killed the monster. Shared by
// the default (melee) attack path and a thrown item's hit path, so both
// go through Defend/corpse creation identically regardless of which
// monster kind was hit or what kind of attack produced attackJSON.
func attackMonster(state *GameState, idx int, attackJSON []byte) ([]string, error) {
	ms := &state.Monsters[idx]
	svc, ok := monsterServices[ms.Kind]
	if !ok {
		return nil, fmt.Errorf("hack: no monster service registered for kind %q", ms.Kind)
	}
	dRespJSON, err := svc.Defend(ms.State, attackJSON)
	if err != nil {
		return nil, fmt.Errorf("%s defend: %w", ms.Kind, err)
	}
	var dresp defendEnvelope
	if err := json.Unmarshal(dRespJSON, &dresp); err != nil {
		return nil, fmt.Errorf("parse %s defend response: %w", ms.Kind, err)
	}
	ms.State = dresp.Self
	messages := []string{defendMessage(svc.DisplayName, dresp.Outcome)}

	if err := finalizeMonsterDeathIfAny(state, idx); err != nil {
		return nil, err
	}
	return messages, nil
}

// finalizeMonsterDeathIfAny checks whether state.Monsters[idx] (whose
// State was just updated) has newly died, and if so builds its corpse
// and drops its wielded weapon (if any) on the floor at its death tile
// — shared by every path that can reduce a monster's HP to 0 (melee, a
// thrown item via attackMonster, and burn-tick damage via its own
// Tick), so all three trigger identical death handling regardless of
// monster kind. A no-op if the monster isn't dead. Callers are
// responsible for only invoking this once per death (i.e. only when the
// monster was alive immediately before the HP-reducing event just
// applied), so a corpse is never built twice for the same death.
func finalizeMonsterDeathIfAny(state *GameState, idx int) error {
	ms := &state.Monsters[idx]
	obj, err := monsterObject(ms.State)
	if err != nil {
		return err
	}
	if !obj.Dead {
		return nil
	}
	svc, ok := monsterServices[ms.Kind]
	if !ok {
		return fmt.Errorf("hack: no monster service registered for kind %q", ms.Kind)
	}
	corpseRaw, err := svc.Corpse(ms.State)
	if err != nil {
		return err
	}
	var cr corpseResult
	if err := json.Unmarshal(corpseRaw, &cr); err != nil {
		return fmt.Errorf("parse corpse result: %w", err)
	}
	ms.State = cr.Self
	state.FloorItems = append(state.FloorItems, cr.Item)
	if cr.Wielded != nil {
		weaponRaw, err := weapon.FromStats(*cr.Wielded)
		if err != nil {
			return err
		}
		relocated, err := relocateItem(weaponRaw, obj.X, obj.Y)
		if err != nil {
			return err
		}
		state.FloorItems = append(state.FloorItems, relocated)
	}
	return nil
}

// monsterBurnMessage returns a short status string for a monster's own
// burn-tick outcome, mirroring tickMessage's burn-damage lines but in
// the third person with a caller-supplied display name, matching
// defendMessage's phrasing style. Returns "" when the monster isn't
// currently burning.
func monsterBurnMessage(who string, outcome item.TickOutcome) string {
	if outcome.BurnDamage <= 0 {
		return ""
	}
	if outcome.BurnedOut {
		return fmt.Sprintf("%s takes %d fire damage and the flames die out.", who, outcome.BurnDamage)
	}
	return fmt.Sprintf("%s takes %d fire damage from burning.", who, outcome.BurnDamage)
}
