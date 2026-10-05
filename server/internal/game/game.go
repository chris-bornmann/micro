// Central game state: the coordinator's own types.
package game

import (
	"micro/server/internal/mapdata"
	"micro/server/internal/mapgen"
	"micro/server/internal/rules"
)

// GameState is the coordinator's central state: the Player, who persists
// across every level, plus whichever level they're currently on. A
// level's own content — Map, Monsters, FloorItems, Visited, Oily — lives
// in exactly one place, levelState, embedded here so every state.Map/
// state.Monsters/state.FloorItems read and write site works exactly as if
// they were plain GameState fields. There is no separate "current level"
// copy anywhere else: CurrentLevel/Levels are the only place level
// identity/storage exists, and a transition between them (see
// applyStairsTransition in levels.go) is exactly "repoint the embedded
// *levelState at a different Levels entry" — never a copy in either
// direction, so the live level and the stored level can never drift out
// of sync with each other.
type GameState struct {
	// eng is the rules engine every action goes through.
	eng *rules.Engine

	Player *rules.Entity
	// PendingSelection is non-nil while the game is waiting on a follow-up
	// keypress to complete a pick up, use, equip, drop or throw (see
	// resolvePendingSelection). nil means no selection is in progress.
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
	// SpawnedOnce records, per monster template ID, whether that monster
	// has ever spawned on any level reached so far this run. A
	// single-shot monster's "have I appeared yet" spans every level, so
	// it lives here rather than in a levelState.
	SpawnedOnce map[string]bool
	// Roster holds unique characters who left a level alive — escaped,
	// teleported away — with everything they carried and remembered. Each
	// may turn up again on a deeper level (see bringBack).
	Roster []rosterEntry
	// Special is which special level, if any, each depth is this game (see
	// assignSpecial): settled at the start, built when first reached.
	Special map[int]*rules.LevelSpec
	// Identity is which item types the Player does not recognize yet, and
	// how each looks this game (see rules.Identity). Nil, as in most
	// tests, recognizes everything.
	Identity *rules.Identity

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
// every level.
type levelState struct {
	Map *mapdata.Map
	// Monsters is every monster on this level, dead ones included: a dead
	// monster keeps its own state (see rules.Ctx.Kill), and every loop
	// over Monsters skips it.
	Monsters []*rules.Entity
	// FloorItems is everything lying on this level's floor: items, and
	// fixed features such as stairs and traps (which are never picked
	// up). Picking an item up moves it into the Player's inventory; an
	// item is never in two places at once.
	FloorItems []*rules.Entity
	Visited    Visibility
	// Oily is the set of floor tiles currently coated in spilled oil —
	// dynamic runtime state, not part of the map's own generation output,
	// the same way Visited tracks fog of war alongside it.
	Oily map[mapdata.Point]bool
}

// world is the current level as the rules engine sees it. It shares the
// level's own slices and oil map, so whatever the engine changes, it
// changes here.
func (state *GameState) world() *rules.World {
	if state.Oily == nil {
		state.Oily = map[mapdata.Point]bool{}
	}
	return &rules.World{
		Map: state.Map, Player: state.Player, Monsters: state.Monsters, Oily: state.Oily,
		Floor: state.FloorItems, Depth: max(state.CurrentLevel, 1),
		Visible: state.playerSees(),
		// The turn being played is the one after the last completed.
		Turn:     state.Turn + 1,
		Summon:   state.summon,
		Identity: state.Identity,
	}
}

// ctx starts a rules resolution against the current level.
func (state *GameState) ctx() *rules.Ctx { return state.eng.NewCtx(state.world()) }

// liveMonsters returns every monster on the current level that is not
// dead.
func (state *GameState) liveMonsters() []*rules.Entity { return state.world().LiveMonsters() }

// monsterAt returns the live monster standing on (x, y), if any.
func (state *GameState) monsterAt(x, y int) *rules.Entity {
	for _, m := range state.liveMonsters() {
		if m.X == x && m.Y == y {
			return m
		}
	}
	return nil
}

// floorItemsAt returns every floor item at (x, y), in FloorItems order.
func (state *GameState) floorItemsAt(x, y int) []*rules.Entity {
	var out []*rules.Entity
	for _, it := range state.FloorItems {
		if it.X == x && it.Y == y {
			out = append(out, it)
		}
	}
	return out
}

// removeFloorItem takes item off the floor, reporting whether it was there.
func (state *GameState) removeFloorItem(item *rules.Entity) bool {
	for i, it := range state.FloorItems {
		if it == item {
			state.FloorItems = append(state.FloorItems[:i], state.FloorItems[i+1:]...)
			return true
		}
	}
	return false
}

// pendingAction is which pipeline a resolved pendingSelection completes.
type pendingAction string

const (
	pendingEquip  pendingAction = "equip"
	pendingUse    pendingAction = "use"
	pendingDrop   pendingAction = "drop"
	pendingPickup pendingAction = "pickup"
	pendingThrow  pendingAction = "throw"
	// pendingAnswer is an item, already chosen, whose use asks a question
	// first (see rules.Asker): the next key is the typed answer.
	pendingAnswer pendingAction = "answer"
	// pendingUseOn is an item, already chosen, whose use asks which of the
	// Player's other items to use it on: Candidates are those, by letter.
	pendingUseOn pendingAction = "use_on"
)

// pendingSelection tracks a request that's waiting on a follow-up
// keypress because more than one item could match (or, for drop, because
// drop always asks). Cleared as soon as it's resolved (a valid letter, or
// — for a throw's second stage — a valid direction) or abandoned
// (anything else).
type pendingSelection struct {
	Action pendingAction
	// Candidates is what this selection is choosing among, in the order
	// candidateLetters assigns letters to them.
	Candidates []*rules.Entity
	// Item is set once the item is chosen and the game is waiting on
	// something else: a throw's direction, or the answer to Question.
	Item *rules.Entity
	// Question is what using Item asks, for pendingAnswer.
	Question string
}

// trapAt reports whether a trap lies on (x, y) among items.
func trapAt(items []*rules.Entity, x, y int) bool {
	for _, it := range items {
		if it.X == x && it.Y == y && it.T.Category == rules.CategoryTrap {
			return true
		}
	}
	return false
}

// noticed reports whether the Player knows about a thing on the floor: it
// is not hidden, or they can see what is hidden (see_invisible).
func (state *GameState) noticed(it *rules.Entity) bool {
	reg := state.eng.Reg
	return !reg.Has(it, "hidden") || reg.Has(state.Player, "see_invisible")
}

// playerSees returns what rules.World.Visible asks: whether the Player can
// see a tile right now. It is the same set of tiles the frame draws
// movable things on (see currentlyVisibleTiles), worked out once, when
// first asked.
func (state *GameState) playerSees() func(x, y int) bool {
	var seen map[mapdata.Point]bool
	return func(x, y int) bool {
		if seen == nil {
			seen = map[mapdata.Point]bool{}
			if state.Player != nil && state.Map != nil {
				for _, p := range currentlyVisibleTiles(state) {
					seen[p] = true
				}
			}
		}
		return seen[mapdata.Point{X: x, Y: y}]
	}
}

// rosterEntry is a unique character off the map, and the level it left.
type rosterEntry struct {
	Who  *rules.Entity
	Left int
}
