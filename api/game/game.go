// Central game state: the coordinator's own types. No behaviour beyond
// the one conversion helper OilyTiles needs to be JSON-representable.
package game

import (
	"encoding/json"

	"map_api/mapdata"
	"map_api/mapgen"
)

// GameState is the coordinator's central state: the Player, who persists
// across every level, plus whichever level they're currently on. A
// level's own content — Map, Monsters, FloorItems, Visited, OilyTiles —
// lives in exactly one place, levelState, embedded here so every
// state.Map/state.Monsters/state.FloorItems/state.Visited/
// state.OilyTiles read AND write site works exactly as if they were
// still plain GameState fields (Go promotes an embedded pointer's
// fields for both reads and assignment). There is no separate "current
// level" copy anywhere else: CurrentLevel/Levels are the only place
// level identity/storage exists, and a transition between them (see
// applyStairsTransition in levels.go) is exactly "repoint the embedded
// *levelState at a different Levels entry" — never a copy in either
// direction, so the live level and the stored level can never drift
// out of sync with each other.
type GameState struct {
	// PlayerState is exactly the bytes player last returned as its own
	// state — stored and forwarded verbatim. hack never parses it beyond
	// the generic overlay.Object subset (playerObject, for rendering and
	// building the world snapshot each service is fed); whatever private
	// state it keeps for itself is its own business.
	PlayerState []byte
	// PendingSelection is non-nil while hack is waiting on a follow-up
	// letter keypress to complete an equip/quaff that had 2+ matching
	// candidates (see equipItem/useItem, resolvePendingSelection). nil
	// means no selection is in progress — handleKey's normal key switch
	// applies.
	PendingSelection *pendingSelection

	// CurrentLevel is the 1-indexed depth the Player is currently on.
	CurrentLevel int
	// Levels holds every level's own *levelState, indexed by depth-1
	// (Levels[0] is level 1) — nil until that depth has actually been
	// visited (see populateLevel in levels.go). The entry for
	// CurrentLevel is always the same pointer as the embedded
	// *levelState below.
	Levels []*levelState
	// LevelConfig is the mapgen.Config (Width/Height/room settings from
	// flags) used to generate every level this run — level N+1 reuses it
	// with Seed offset by depth, so a given -seed stays deterministic
	// across the whole run, not just level 1.
	LevelConfig mapgen.Config
	// SpawnedOnce tracks, per monster Kind, whether that kind has EVER
	// spawned on any level reached so far this run — unlike Levels
	// (per-depth), this persists across the whole game, since a
	// single-shot monster's "have I appeared yet" question spans every
	// level, not just the current one (see populateLevel). hack is the
	// only thing that can answer this — a monster package has no memory
	// of its own between Init calls, same as every other exported
	// function it has.
	SpawnedOnce map[string]bool

	// Turn counts turns the Player has actually consumed. It is incremented
	// in exactly one place, finishTurn, because that is the one place a turn
	// completes — every branch that spends a turn ends by calling it, and
	// every branch that does not (a picker opening, a picker cancelled, a
	// command refused, a read-only view) never reaches it. Counting anywhere
	// else means counting keypresses, which is a different and less useful
	// number.
	Turn int

	*levelState
}

// levelState is one level's full content: everything about a level
// except the Player, who carries their own HP/Inventory/position across
// every level. See GameState's own doc comment for why this is embedded
// rather than duplicated as GameState's own fields.
type levelState struct {
	Map *mapdata.Map
	// Monsters is every live-or-dead monster currently on this level,
	// each entry's own State exactly the bytes that monster's service
	// last returned — same "store whatever they returned, verbatim,
	// parse only the generic subset" invariant as PlayerState, just
	// plural and polymorphic (Kind routes to the right service in
	// monsterServices; see hack/monsters.go).
	Monsters []MonsterState
	// FloorItems is every item currently sitting on this level's floor
	// (not yet picked up), each entry exactly what that item's Init last
	// returned — same "store whatever they returned, verbatim, parse only
	// the generic subset" invariant as PlayerState, just plural since
	// there can be many floor items. Once an item is picked up, its
	// entry moves from here into the Player's own Inventory (via
	// player.PickUp) — hack never holds an item's state in two places at
	// once.
	FloorItems []json.RawMessage
	Visited    Visibility
	// OilyTiles is the set of floor tiles currently coated in spilled oil
	// (e.g. from a shattered flask of oil) — dynamic runtime state, not
	// part of the map's own static generation output (mapdata.Map), the
	// same way Visited tracks dynamic fog-of-war state alongside it.
	// Nothing consumes this yet. Lazily initialized on first write
	// (resolveFlaskThrow) rather than at every levelState construction
	// site, since it's nil-safe to read and most games never spill any
	// oil.
	OilyTiles map[mapdata.Point]bool
}

// oilyTilesSlice converts state.OilyTiles (a map, for O(1) writes from
// resolveFlaskThrow) into the []mapdata.Point shape world.AugmentedMap
// actually carries — encoding/json can't marshal a struct-keyed map, the
// same reason Visibility.rememberedTiles() does this same conversion for
// render.Options.LitTiles.
func (state *GameState) oilyTilesSlice() []mapdata.Point {
	tiles := make([]mapdata.Point, 0, len(state.OilyTiles))
	for p := range state.OilyTiles {
		tiles = append(tiles, p)
	}
	return tiles
}

// pendingAction is which pipeline a resolved pendingSelection completes.
type pendingAction string

const (
	pendingEquip  pendingAction = "equip"
	pendingQuaff  pendingAction = "quaff"
	pendingDrop   pendingAction = "drop"
	pendingPickup pendingAction = "pickup"
	pendingThrow  pendingAction = "throw"
)

// pendingSelection tracks a wield/quaff/drop/throw request that's waiting
// on a follow-up keypress because 1+ (drop) or 2+ (wield/quaff/throw)
// candidates matched. Cleared as soon as it's resolved (a valid letter,
// or — for a throw's second stage — a valid direction) or abandoned
// (anything else).
type pendingSelection struct {
	Action pendingAction
	// Candidates is what this selection is choosing among, in the same
	// order candidateLetters assigned letters to when the picker was
	// opened — Candidates[i] corresponds to candidateLetters(len(Candidates))[i].
	Candidates []carriedItem
	// Item is set only once a pendingThrow's item has been chosen (via
	// the same candidateLetters mechanism as every other picker, or
	// automatically when there was only one candidate) — hack is now
	// waiting on a follow-up DIRECTION keypress instead of a letter. nil
	// for every other pendingAction, and for a throw whose item hasn't
	// been picked yet.
	Item *carriedItem
}

// carriedItem pairs an item's raw JSON with whether it's the Player's
// currently-wielded item, currently-lit torch, or a currently-worn item
// — the unit both the "i" view and the drop picker display, so a
// carried weapon, burning torch, or worn item is annotated identically
// in both places (see pickerLines).
type carriedItem struct {
	Raw       json.RawMessage
	IsWielded bool
	IsLit     bool
	IsWorn    bool
}
