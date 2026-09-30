// Command hack is the game coordinator: it holds central game state and
// drives the other services. Today that's just generating a level and
// showing it in a window with basic keyboard input; future steps add
// monsters/treasure and a turn loop over sentient objects.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"math/rand"
	"os"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"

	"map_api/action"
	"map_api/armor"
	"map_api/flask"
	"map_api/goblin"
	"map_api/item"
	"map_api/mapdata"
	"map_api/mapgen"
	"map_api/mummy"
	"map_api/overlay"
	"map_api/player"
	"map_api/potion"
	"map_api/render"
	"map_api/scroll"
	"map_api/stairs"
	"map_api/status"
	"map_api/torch"
	"map_api/vision"
	"map_api/weapon"
	"map_api/world"
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

// playerObject decodes just the generic overlay.Object subset out of the
// Player's raw state — used only where hack needs a plain drawable/
// positioned object (rendering, building the world snapshot fed to other
// services).
func playerObject(state []byte) (overlay.Object, error) {
	var p overlay.Object
	if err := json.Unmarshal(state, &p); err != nil {
		return overlay.Object{}, fmt.Errorf("parse player: %w", err)
	}
	return p, nil
}

// itemInfo decodes just the generic item.Item subset (overlay.Object plus
// Category/Name/Usage) out of a raw item state — used wherever hack needs
// an item's position, category, or display name without understanding any
// category-specific fields (heal amount, ...).
func itemInfo(raw json.RawMessage) (item.Item, error) {
	var it item.Item
	if err := json.Unmarshal(raw, &it); err != nil {
		return item.Item{}, fmt.Errorf("parse item: %w", err)
	}
	return it, nil
}

// candidateLetters returns exactly n letters, "A".."J" for n in
// [0,item.InventoryCapacity] — so a full inventory's worth of
// same-category candidates never runs out of letters. Shared by
// inventoryLines (the "i" view) and the wield/quaff picker so the two
// never assign different letters to the same item.
func candidateLetters(n int) []string {
	letters := make([]string, n)
	for i := 0; i < n; i++ {
		letters[i] = string(rune('A' + i))
	}
	return letters
}

// pickerLines formats candidates as "A) name" lines using
// candidateLetters — appending " (wielded)"/" (lit)"/" (worn)" for the
// currently-wielded item, currently-lit torch, or a currently-worn item,
// if present — shared by the "i" view (inventoryLines) and every open
// wield/quaff/drop/wear picker, so a player never sees two different
// letter-to-item mappings for what looks like the same kind of list.
func pickerLines(candidates []carriedItem) ([]string, error) {
	letters := candidateLetters(len(candidates))
	lines := make([]string, len(candidates))
	for i, ci := range candidates {
		it, err := itemInfo(ci.Raw)
		if err != nil {
			return nil, err
		}
		name := it.Name
		if ci.IsWielded {
			name += " (wielded)"
		}
		if ci.IsLit {
			name += " (lit)"
		}
		if ci.IsWorn {
			name += " (worn)"
		}
		lines[i] = fmt.Sprintf("%s) %s", letters[i], name)
	}
	return lines, nil
}

// carriedItems returns everything the Player is carrying: the
// currently-wielded item first (if any), then the currently-lit torch
// (if any), then every currently-worn item, then Inventory in order.
// Shared by inventoryLines (the read-only "i" view) and dropItem (the
// drop picker), so the two never disagree about what's carried.
func carriedItems(state *GameState) ([]carriedItem, error) {
	var items []carriedItem
	wielded, hasWielded, err := player.WieldedItem(state.PlayerState)
	if err != nil {
		return nil, err
	}
	if hasWielded {
		items = append(items, carriedItem{Raw: wielded, IsWielded: true})
	}
	litTorch, hasLitTorch, err := player.LitTorchItem(state.PlayerState)
	if err != nil {
		return nil, err
	}
	if hasLitTorch {
		items = append(items, carriedItem{Raw: litTorch, IsLit: true})
	}
	worn, err := player.WornItems(state.PlayerState)
	if err != nil {
		return nil, err
	}
	for _, raw := range worn {
		items = append(items, carriedItem{Raw: raw, IsWorn: true})
	}
	inv, err := player.Inventory(state.PlayerState)
	if err != nil {
		return nil, err
	}
	for _, raw := range inv {
		items = append(items, carriedItem{Raw: raw})
	}
	return items, nil
}

// inventoryLines returns one display line per carried item (wielded item
// first, then Inventory) for the "i" key's read-only view. Unlike
// pickUp/useItem/equipItem/dropItem, this never mutates
// state.PlayerState and is never routed through finishTurn — looking at
// your own inventory doesn't cost a turn.
func inventoryLines(state *GameState) ([]string, error) {
	items, err := carriedItems(state)
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return []string{"You are carrying nothing."}, nil
	}
	return pickerLines(items)
}

// lookHereLines returns one display line per item at the Player's
// current tile — the "L" key's read-only view, following
// inventoryLines' exact shape (reusing pickerLines/candidateLetters so
// the lettering a player sees here matches the picker they'd get from
// "," if they then pick one up). Never mutates state.PlayerState, never
// routed through finishTurn or handleKey — like "i", looking at the
// floor costs no turn.
func lookHereLines(state *GameState) ([]string, error) {
	playerObj, err := playerObject(state.PlayerState)
	if err != nil {
		return nil, err
	}
	floorItems, err := floorItemsAt(state, playerObj.X, playerObj.Y)
	if err != nil {
		return nil, err
	}
	if len(floorItems) == 0 {
		return []string{"There is nothing here."}, nil
	}
	return pickerLines(wrapRaw(floorItems))
}

// statusLines turns a Status call's raw JSON (a []status.Field) into
// display lines — generic across every entity kind, since hack never
// needs to know what any individual field means, only how to show a
// label/value pair. Used by the "S" key's read-only view.
func statusLines(fieldsJSON []byte) ([]string, error) {
	var fields []status.Field
	if err := json.Unmarshal(fieldsJSON, &fields); err != nil {
		return nil, fmt.Errorf("parse status: %w", err)
	}
	lines := make([]string, len(fields))
	for i, f := range fields {
		lines[i] = fmt.Sprintf("%s: %s", f.Label, f.Value)
	}
	return lines, nil
}

// helpLines lists every player-visible command with a short description,
// for the "?" key's read-only view — kept in sync with handleKey's switch
// (below), player.Action's key cases, and the SetOnTypedKey/SetOnTypedRune
// callbacks by hand, since none of those are enumerable at runtime.
func helpLines() []string {
	return []string{
		"Arrow keys — Move (attacks if the destination is occupied)",
		".         — Wait a turn in place",
		",         — Pick up item(s) here",
		"<  /  >   — Go up / down stairs",
		"A         — Use an item (potion, scroll, light a torch, …)",
		"E         — Equip a weapon or piece of armor",
		"D         — Drop an item",
		"T         — Throw an item, then choose a direction",
		"I         — Show inventory",
		"L         — Look at what's on the floor here",
		"S         — Show status (HP, attributes, active effects)",
		"?         — Show this help",
		"Escape    — Cancel a picker, or close the window",
		"Q         — Quit",
	}
}

// wrapRaw wraps plain item JSON as
// never-wielded carriedItems, for building a pendingSelection from
// quaffCandidates/equipCandidates (which only ever return Inventory
// items, never the wielded one).
func wrapRaw(raws []json.RawMessage) []carriedItem {
	items := make([]carriedItem, len(raws))
	for i, raw := range raws {
		items[i] = carriedItem{Raw: raw}
	}
	return items
}

// actionEnvelope mirrors the {"self": ..., "result": ...} shape both
// player.Action and goblin.Action return. Self is kept as raw bytes rather
// than decoded — hack stores it verbatim and only ever decodes the generic
// subset (via playerObject/goblinObject) when it actually needs one, never
// assuming anything about an entity's own private fields.
type actionEnvelope struct {
	Self   json.RawMessage `json:"self"`
	Result action.Result   `json:"result"`
}

// defendEnvelope mirrors the {"self": ..., "outcome": ...} shape both
// player.Defend and goblin.Defend return. Self is kept as raw bytes for
// the same reason actionEnvelope's is.
type defendEnvelope struct {
	Self    json.RawMessage      `json:"self"`
	Outcome action.DefendOutcome `json:"outcome"`
}

// defendMessage returns a short status string for a monster's Defend
// outcome.
func defendMessage(who string, o action.DefendOutcome) string {
	if o.Dead {
		return fmt.Sprintf("%s takes %d damage and dies", who, o.Damage)
	}
	return fmt.Sprintf("%s takes %d damage (%d HP left)", who, o.Damage, o.RemainingHP)
}

// playerDefendMessage returns a short status string for the Player's own
// Defend outcome — a separate function from defendMessage rather than a
// "You" argument to it, since "You takes damage" doesn't read right.
func playerDefendMessage(o action.DefendOutcome) string {
	if o.Dead {
		return fmt.Sprintf("You take %d damage and die", o.Damage)
	}
	return fmt.Sprintf("You take %d damage (%d HP left)", o.Damage, o.RemainingHP)
}

// effectEnvelope mirrors the {"self": ..., "outcome": ...} shape
// player.ApplyEffect returns, the same way defendEnvelope mirrors
// Defend's.
type effectEnvelope struct {
	Self    json.RawMessage    `json:"self"`
	Outcome item.EffectOutcome `json:"outcome"`
}

// dropEnvelope mirrors the {"self": ..., "item": ..., "was_wielded":
// ..., "was_lit_torch": ...} shape player.Drop returns.
type dropEnvelope struct {
	Self        json.RawMessage `json:"self"`
	Item        json.RawMessage `json:"item"`
	WasWielded  bool            `json:"was_wielded"`
	WasWorn     bool            `json:"was_worn"`
	WasLitTorch bool            `json:"was_lit_torch"`
}

// relocateItem returns raw (an item's own JSON) with its generic X/Y
// fields overwritten to x,y — used when an item changes location without
// going through its owning package's Init (e.g. dropping something from
// Inventory back onto the floor at the Player's current tile).
func relocateItem(raw json.RawMessage, x, y int) (json.RawMessage, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, fmt.Errorf("parse item: %w", err)
	}
	xJSON, err := json.Marshal(x)
	if err != nil {
		return nil, fmt.Errorf("marshal x: %w", err)
	}
	yJSON, err := json.Marshal(y)
	if err != nil {
		return nil, fmt.Errorf("marshal y: %w", err)
	}
	fields["X"] = xJSON
	fields["Y"] = yJSON
	out, err := json.Marshal(fields)
	if err != nil {
		return nil, fmt.Errorf("marshal relocated item: %w", err)
	}
	return out, nil
}

// effectMessage returns a short status string for an ApplyEffect outcome,
// following defendMessage's style.
func effectMessage(itemName string, o item.EffectOutcome) string {
	switch o.Kind {
	case item.EffectHeal:
		return fmt.Sprintf("You drink the %s and recover %d HP (%d HP now).", itemName, o.Applied, o.RemainingHP)
	case item.EffectInvisibility:
		return fmt.Sprintf("You drink the %s and turn invisible for %d turns.", itemName, o.Applied)
	case item.EffectTeleport:
		return fmt.Sprintf("You read the %s and are teleported away!", itemName)
	default:
		return fmt.Sprintf("You use the %s.", itemName)
	}
}

// tickEnvelope mirrors the {"self": ..., "outcome": ...} shape
// player.Tick returns, the same way effectEnvelope mirrors ApplyEffect's.
type tickEnvelope struct {
	Self    json.RawMessage  `json:"self"`
	Outcome item.TickOutcome `json:"outcome"`
}

// tickMessage returns a short status string for a Tick outcome's expired
// effects, a burned-out torch, and/or burn damage, following
// defendMessage/effectMessage's style. Returns "" when nothing expired,
// no torch burned out, and no burn damage was dealt — the common case —
// so finishTurn only appends a message when there's actually something
// to report.
func tickMessage(outcome item.TickOutcome) string {
	var msgs []string
	for _, k := range outcome.Expired {
		switch k {
		case item.EffectInvisibility:
			msgs = append(msgs, "Your invisibility wears off.")
		default:
			msgs = append(msgs, fmt.Sprintf("Your %s effect wears off.", k))
		}
	}
	if outcome.TorchBurnedOut {
		msgs = append(msgs, "Your torch burns out.")
	}
	if outcome.BurnDamage > 0 {
		if outcome.BurnedOut {
			msgs = append(msgs, fmt.Sprintf("You take %d fire damage and the flames die out.", outcome.BurnDamage))
		} else {
			msgs = append(msgs, fmt.Sprintf("You take %d fire damage from burning.", outcome.BurnDamage))
		}
	}
	return joinMessages(msgs)
}

// handleKey processes one keypress. If a equip/quaff/drop/pickup picker
// is currently open (state.PendingSelection != nil), the key is
// interpreted as that picker's follow-up letter instead of a normal game
// command — see resolvePendingSelection. Otherwise, if the Player is
// already dead, it reports "Game Over" and does nothing else. Otherwise
// it dispatches on key: "," picks up whatever's on the Player's own tile
// (immediately if there's exactly one item there, via a picker if 2+ —
// see pickUp), "A" uses a one-shot item from the Player's inventory
// (immediately if exactly one is carried, via a picker if 2+ are — see
// useItem), "E" (equipItem) wields a weapon or wears armor — one command
// over both categories together, so a picker with 2+ candidates can mix
// weapons and armor in the same list — "D" (dropItem) always opens a
// picker over everything carried (wielded/worn items included) whenever
// 1+ items are carried — never an immediate auto-drop, even with just one
// item — and every other key goes through the Player service's Action
// exactly as before (a move or an attack, resolved against the Goblin via
// Defend if it was an attack). Every branch that
// actually completes an action (not one that merely opened a picker)
// ends by calling finishTurn, which is always the second half of a turn:
// reveal fog, then give the Goblin its turn. Neither player nor goblin
// nor potion ever calls back into hack or into each other — hack calls
// them, feeds them JSON, and stores whatever JSON they return.
func handleKey(state *GameState, key string) (string, error) {
	if state.PendingSelection != nil {
		return resolvePendingSelection(state, key)
	}

	playerObj, err := playerObject(state.PlayerState)
	if err != nil {
		return "", err
	}
	if playerObj.Dead {
		return "Game Over", nil
	}

	switch key {
	case ",":
		msg, done, err := pickUp(state, playerObj)
		if err != nil {
			return "", err
		}
		if !done {
			return "", nil
		}
		return finishTurn(state, messagesOf(msg))
	case "<":
		msg, err := applyStairs(state, playerObj, stairs.KindUp, "up")
		if err != nil {
			return "", err
		}
		return finishTurn(state, messagesOf(msg))
	case ">":
		msg, err := applyStairs(state, playerObj, stairs.KindDown, "down")
		if err != nil {
			return "", err
		}
		return finishTurn(state, messagesOf(msg))
	case "A":
		msg, done, err := useItem(state)
		if err != nil {
			return "", err
		}
		if !done {
			return "", nil
		}
		return finishTurn(state, messagesOf(msg))
	case "E":
		msg, done, err := equipItem(state)
		if err != nil {
			return "", err
		}
		if !done {
			return "", nil
		}
		return finishTurn(state, messagesOf(msg))
	case "D":
		msg, done, err := dropItem(state)
		if err != nil {
			return "", err
		}
		if !done {
			return "", nil
		}
		return finishTurn(state, messagesOf(msg))
	case "T":
		msg, done, err := throwItem(state)
		if err != nil {
			return "", err
		}
		if !done {
			return "", nil
		}
		return finishTurn(state, messagesOf(msg))
	default:
		// A dead monster is excluded here (mirroring objectsToRender's
		// own filter) so it no longer blocks its own tile: occupantAt
		// would otherwise treat any move onto it as an attack on a
		// corpse forever, and the Player could never step onto/through
		// it.
		liveMonsters, err := liveMonsterObjects(state)
		if err != nil {
			return "", err
		}
		augObjects := append([]overlay.Object{playerObj}, liveMonsters...)
		aug := world.AugmentedMap{Map: *state.Map, Objects: augObjects, OilyTiles: state.oilyTilesSlice()}
		augJSON, err := json.Marshal(aug)
		if err != nil {
			return "", fmt.Errorf("marshal augmented map: %w", err)
		}
		respJSON, err := player.Action(state.PlayerState, augJSON, key)
		if err != nil {
			return "", fmt.Errorf("player action: %w", err)
		}
		var presp actionEnvelope
		if err := json.Unmarshal(respJSON, &presp); err != nil {
			return "", fmt.Errorf("parse player response: %w", err)
		}
		state.PlayerState = presp.Self

		var messages []string
		if presp.Result.Action == "attack" {
			if idx, ok := monsterIndexAt(state, presp.Result.X, presp.Result.Y); ok {
				attackJSON, err := json.Marshal(presp.Result)
				if err != nil {
					return "", fmt.Errorf("marshal player attack: %w", err)
				}
				msgs, err := attackMonster(state, idx, attackJSON)
				if err != nil {
					return "", err
				}
				messages = append(messages, msgs...)
			}
		}

		return finishTurn(state, messages)
	}
}

// attackGoblin sends attackJSON (a JSON-encoded action.Result, as
// messagesOf wraps a single possibly-empty status string as a []string
// suitable for finishTurn — nil (not [""]) when msg is empty, so
// joinMessages doesn't need to special-case a lone empty entry.
func messagesOf(msg string) []string {
	if msg == "" {
		return nil
	}
	return []string{msg}
}

// finishTurn advances the Player's sustained effects by one turn (Tick),
// reveals fog around the Player's current position, then gives the
// Goblin its turn (if still alive) exactly as the second half of every
// turn always has, and returns the combined status message: whatever's
// already in messages (a player-attack outcome, or a pickup/use
// confirmation) followed by anything Tick reports (an effect wearing
// off) and whatever the Goblin's turn adds. Tick runs first so the
// playerObj fed to goblin.Action this same turn always carries this
// turn's up-to-date Invisible state, not last turn's.
func finishTurn(state *GameState, messages []string) (string, error) {
	tickJSON, err := player.Tick(state.PlayerState)
	if err != nil {
		return "", fmt.Errorf("player tick: %w", err)
	}
	var tresp tickEnvelope
	if err := json.Unmarshal(tickJSON, &tresp); err != nil {
		return "", fmt.Errorf("parse player tick response: %w", err)
	}
	state.PlayerState = tresp.Self
	if msg := tickMessage(tresp.Outcome); msg != "" {
		messages = append(messages, msg)
	}

	// Every live monster's own Tick (burn damage etc.) first — it never
	// needs world context (aug/playerObj), so this runs before playerObj
	// is even computed, mirroring the single-goblin ordering this
	// generalizes from exactly.
	for i := range state.Monsters {
		ms := &state.Monsters[i]
		obj, err := monsterObject(ms.State)
		if err != nil {
			return "", err
		}
		if obj.Dead {
			continue
		}
		svc, ok := monsterServices[ms.Kind]
		if !ok {
			return "", fmt.Errorf("hack: no monster service registered for kind %q", ms.Kind)
		}
		tickJSON, err := svc.Tick(ms.State)
		if err != nil {
			return "", fmt.Errorf("%s tick: %w", ms.Kind, err)
		}
		var tresp tickEnvelope
		if err := json.Unmarshal(tickJSON, &tresp); err != nil {
			return "", fmt.Errorf("parse %s tick response: %w", ms.Kind, err)
		}
		ms.State = tresp.Self
		if msg := monsterBurnMessage(svc.DisplayName, tresp.Outcome); msg != "" {
			messages = append(messages, msg)
		}
		if err := finalizeMonsterDeathIfAny(state, i); err != nil {
			return "", err
		}
	}

	playerObj, err := playerObject(state.PlayerState)
	if err != nil {
		return "", err
	}
	state.Visited.reveal(state.Map, playerObj.X, playerObj.Y)
	litTiles, _, _ := vision.CurrentlyLit(*state.Map, playerObj.X, playerObj.Y, playerObj.Lit)
	state.Visited.rememberLitTiles(litTiles)

	// Every still-alive monster (a burn tick above may have just killed
	// one) takes its own turn — movement/chase/attack — in Monsters
	// order. Each only ever sees the Player in its own aug.Objects;
	// monsters don't perceive or interact with each other.
	for i := range state.Monsters {
		ms := &state.Monsters[i]
		obj, err := monsterObject(ms.State)
		if err != nil {
			return "", err
		}
		if obj.Dead {
			continue
		}
		svc := monsterServices[ms.Kind]
		aug := world.AugmentedMap{Map: *state.Map, Objects: []overlay.Object{playerObj}, OilyTiles: state.oilyTilesSlice()}
		augJSON, err := json.Marshal(aug)
		if err != nil {
			return "", fmt.Errorf("marshal augmented map: %w", err)
		}
		aRespJSON, err := svc.Action(ms.State, augJSON)
		if err != nil {
			return "", fmt.Errorf("%s action: %w", ms.Kind, err)
		}
		var aresp actionEnvelope
		if err := json.Unmarshal(aRespJSON, &aresp); err != nil {
			return "", fmt.Errorf("parse %s response: %w", ms.Kind, err)
		}
		ms.State = aresp.Self

		if aresp.Result.Action == "attack" {
			attackJSON, err := json.Marshal(aresp.Result)
			if err != nil {
				return "", fmt.Errorf("marshal %s attack: %w", ms.Kind, err)
			}
			dRespJSON, err := player.Defend(state.PlayerState, attackJSON)
			if err != nil {
				return "", fmt.Errorf("player defend: %w", err)
			}
			var dresp defendEnvelope
			if err := json.Unmarshal(dRespJSON, &dresp); err != nil {
				return "", fmt.Errorf("parse player defend response: %w", err)
			}
			state.PlayerState = dresp.Self
			messages = append(messages, playerDefendMessage(dresp.Outcome))
		}
	}
	return joinMessages(messages), nil
}

// joinMessages combines every non-empty message with " | ".
func joinMessages(messages []string) string {
	combined := ""
	for _, m := range messages {
		if m == "" {
			continue
		}
		if combined != "" {
			combined += " | "
		}
		combined += m
	}
	return combined
}

// floorItemsAt returns every state.FloorItems entry sitting at (x,y), in
// FloorItems order. Shared by pickUp (finding pickup candidates at the
// Player's tile) and lookHereLines (listing everything at the Player's
// tile), so the two never disagree about what's there.
func floorItemsAt(state *GameState, x, y int) ([]json.RawMessage, error) {
	var out []json.RawMessage
	for _, raw := range state.FloorItems {
		it, err := itemInfo(raw)
		if err != nil {
			return nil, err
		}
		if it.X == x && it.Y == y {
			out = append(out, raw)
		}
	}
	return out, nil
}

// pickupCandidatesAt is floorItemsAt filtered to items that can actually
// be picked up — a Fixed item (e.g. stairs) stays on the floor forever
// and is never a pickup candidate, even though it still shows up in
// floorItemsAt's other callers like lookHereLines (looking at what's on
// your own tile shouldn't hide the stairs, just make them unpickupable).
func pickupCandidatesAt(state *GameState, x, y int) ([]json.RawMessage, error) {
	all, err := floorItemsAt(state, x, y)
	if err != nil {
		return nil, err
	}
	var out []json.RawMessage
	for _, raw := range all {
		it, err := itemInfo(raw)
		if err != nil {
			return nil, err
		}
		if !it.Fixed {
			out = append(out, raw)
		}
	}
	return out, nil
}

// applyPickup runs the pickup pipeline (player.PickUp, then remove that
// specific item from FloorItems by ID — not by index, since with 2+
// items at a tile the chosen one may not be FloorItems[0]) against one
// specific item — shared by pickUp's 1-item auto path and
// resolvePendingSelection's picker-completion path. If the Player's
// Inventory is already full, the item is left on the floor and a
// friendly message is reported rather than an error.
func applyPickup(state *GameState, it item.Item, raw json.RawMessage) (string, error) {
	updatedSelf, err := player.PickUp(state.PlayerState, raw)
	if errors.Is(err, player.ErrInventoryFull) {
		return "Your inventory is full.", nil
	}
	if err != nil {
		return "", fmt.Errorf("player pick up: %w", err)
	}
	state.PlayerState = updatedSelf

	idx := -1
	for i, fi := range state.FloorItems {
		fit, err := itemInfo(fi)
		if err != nil {
			return "", err
		}
		if fit.ID == it.ID {
			idx = i
			break
		}
	}
	if idx == -1 {
		return "", fmt.Errorf("hack: picked-up item %q no longer on the floor", it.ID)
	}
	state.FloorItems = append(state.FloorItems[:idx], state.FloorItems[idx+1:]...)
	return fmt.Sprintf("You pick up the %s.", it.Name), nil
}

// pickUp finds every pickable (non-Fixed) state.FloorItems entry at the
// Player's own tile. With exactly one, it's picked up immediately
// (matches the previous single-item behavior). With 2+, a picker opens
// (state.PendingSelection) instead of blindly grabbing the first one.
// Returns (message, done) with the same meaning as
// useItem/equipItem/dropItem's: done is false only when a picker was
// opened.
func pickUp(state *GameState, playerObj overlay.Object) (string, bool, error) {
	cands, err := pickupCandidatesAt(state, playerObj.X, playerObj.Y)
	if err != nil {
		return "", true, err
	}
	switch len(cands) {
	case 0:
		return "There is nothing here to pick up.", true, nil
	case 1:
		it, err := itemInfo(cands[0])
		if err != nil {
			return "", true, err
		}
		msg, err := applyPickup(state, it, cands[0])
		return msg, true, err
	default:
		state.PendingSelection = &pendingSelection{Action: pendingPickup, Candidates: wrapRaw(cands)}
		return "", false, nil
	}
}

// applyStairs is the "<"/">" key's handler: if the Player is standing on
// a stairs item of the matching kind, applyStairsTransition (see
// levels.go) moves them to the level it leads to. Returns
// "There are no stairs %s here." without consuming a special action
// (still finishes a turn, matching pickUp's "There is nothing here"
// precedent) if nothing matches.
func applyStairs(state *GameState, playerObj overlay.Object, kind stairs.Kind, direction string) (string, error) {
	cands, err := floorItemsAt(state, playerObj.X, playerObj.Y)
	if err != nil {
		return "", err
	}
	for _, raw := range cands {
		it, err := itemInfo(raw)
		if err != nil {
			return "", err
		}
		if it.Category != item.CategoryStairs {
			continue
		}
		gotKind, err := stairs.KindOf(raw)
		if err != nil {
			return "", err
		}
		if gotKind != kind {
			continue
		}
		return applyStairsTransition(state, kind)
	}
	return fmt.Sprintf("There are no stairs %s here.", direction), nil
}

// useItemEffect calls the Use function belonging to whichever category
// owns raw, identified generically via category — the seam a future item
// category (food, ...) plugs a new case into.
func useItemEffect(category item.Category, raw json.RawMessage) (json.RawMessage, error) {
	switch category {
	case item.CategoryPotion:
		out, err := potion.Use(raw)
		if err != nil {
			return nil, fmt.Errorf("potion use: %w", err)
		}
		var resp struct {
			Effect item.Effect `json:"effect"`
		}
		if err := json.Unmarshal(out, &resp); err != nil {
			return nil, fmt.Errorf("parse potion use response: %w", err)
		}
		return json.Marshal(resp.Effect)
	case item.CategoryScroll:
		out, err := scroll.Use(raw)
		if err != nil {
			return nil, fmt.Errorf("scroll use: %w", err)
		}
		var resp struct {
			Effect item.Effect `json:"effect"`
		}
		if err := json.Unmarshal(out, &resp); err != nil {
			return nil, fmt.Errorf("parse scroll use response: %w", err)
		}
		return json.Marshal(resp.Effect)
	default:
		return nil, fmt.Errorf("hack: no Use handler for item category %q", category)
	}
}

// resolveEffectTarget fills in game-state-dependent fields an item
// category's own Use couldn't have computed on its own — today, just a
// random unoccupied destination for item.EffectTeleport (scroll.Use only
// knows it's producing a teleport-kind effect, not where the map's open
// tiles currently are; only hack has that). A no-op for every other
// kind, so this is safe to call unconditionally right after
// useItemEffect.
func resolveEffectTarget(state *GameState, effectJSON json.RawMessage) (json.RawMessage, error) {
	var effect item.Effect
	if err := json.Unmarshal(effectJSON, &effect); err != nil {
		return nil, fmt.Errorf("parse effect: %w", err)
	}
	if effect.Kind != item.EffectTeleport {
		return effectJSON, nil
	}
	x, y, ok := randomUnoccupiedTile(state)
	if !ok {
		return nil, fmt.Errorf("hack: no unoccupied tile available to teleport to")
	}
	effect.X, effect.Y = x, y
	return json.Marshal(effect)
}

// randomUnoccupiedTile picks a uniformly random walkable tile on
// state.Map that isn't the Player's own current position or any live
// (non-Dead) monster's position — a dead monster doesn't block,
// matching the same exclusion objectsToRender/handleKey already apply
// elsewhere (see the movement fix in mapToRender); floor items never
// block anything in this game either, so they're not excluded here.
// Uses the global math/rand, matching action.RollDamage's convention
// for turn-time gameplay randomness — spawn-time positions are the only
// thing seeded from m.Seed. ok is false only in the degenerate case of
// no unoccupied tile existing at all.
func randomUnoccupiedTile(state *GameState) (x, y int, ok bool) {
	playerObj, err := playerObject(state.PlayerState)
	if err != nil {
		return 0, 0, false
	}
	liveMonsters, err := liveMonsterObjects(state)
	if err != nil {
		return 0, 0, false
	}

	occupied := map[mapdata.Point]bool{{X: playerObj.X, Y: playerObj.Y}: true}
	for _, m := range liveMonsters {
		occupied[mapdata.Point{X: m.X, Y: m.Y}] = true
	}

	var candidates []mapdata.Point
	for _, p := range state.Map.WalkableTiles() {
		if !occupied[p] {
			candidates = append(candidates, p)
		}
	}
	if len(candidates) == 0 {
		return 0, 0, false
	}
	chosen := candidates[rand.Intn(len(candidates))]
	return chosen.X, chosen.Y, true
}

// tileNearPlayer returns a random walkable tile in the same room as
// (playerX, playerY), excluding the Player's own tile — used to place
// the starting flask of oil somewhere immediately reachable rather than
// in a random room elsewhere on the map, the way its own package-level
// Init would otherwise place it. ok is false if the Player isn't in a
// room (shouldn't happen) or the room is 1x1.
func tileNearPlayer(m *mapdata.Map, playerX, playerY int) (x, y int, ok bool) {
	room, ok := m.RoomAt(playerX, playerY)
	if !ok {
		return 0, 0, false
	}
	var candidates []mapdata.Point
	for dx := 0; dx < room.W; dx++ {
		for dy := 0; dy < room.H; dy++ {
			cx, cy := room.X+dx, room.Y+dy
			if cx == playerX && cy == playerY {
				continue
			}
			candidates = append(candidates, mapdata.Point{X: cx, Y: cy})
		}
	}
	if len(candidates) == 0 {
		return 0, 0, false
	}
	chosen := candidates[rand.Intn(len(candidates))]
	return chosen.X, chosen.Y, true
}

// quaffCandidates returns every Inventory entry with item.UsageOneShot,
// in Inventory order — the set useItem chooses among, whether that's
// auto-picking a sole match or prompting a picker over 2+.
func quaffCandidates(state *GameState) ([]json.RawMessage, error) {
	inv, err := player.Inventory(state.PlayerState)
	if err != nil {
		return nil, err
	}
	var out []json.RawMessage
	for _, raw := range inv {
		it, err := itemInfo(raw)
		if err != nil {
			return nil, err
		}
		if it.Usage == item.UsageOneShot {
			out = append(out, raw)
		}
	}
	return out, nil
}

// equippableCategories are the item.Category values the "E" command
// offers — extend this map (and applyEquip's dispatch below) when a new
// equip-slot category (ring, shield, helm, ...) gets its own package;
// that's the only place a future category needs to be added.
var equippableCategories = map[item.Category]bool{
	item.CategoryWeapon: true,
	item.CategoryArmor:  true,
}

// equipCandidates is quaffCandidates' counterpart for equipping: every
// Inventory entry whose Category is in equippableCategories, in Inventory
// order.
func equipCandidates(state *GameState) ([]json.RawMessage, error) {
	inv, err := player.Inventory(state.PlayerState)
	if err != nil {
		return nil, err
	}
	var out []json.RawMessage
	for _, raw := range inv {
		it, err := itemInfo(raw)
		if err != nil {
			return nil, err
		}
		if equippableCategories[it.Category] {
			out = append(out, raw)
		}
	}
	return out, nil
}

// applyQuaff runs the use-and-consume pipeline (useItemEffect +
// resolveEffectTarget + player.ApplyEffect + player.TakeItem) against one
// specific item — shared by useItem's 0/1-candidate auto path and
// resolvePendingSelection's picker-completion path.
func applyQuaff(state *GameState, it item.Item, raw json.RawMessage) (string, error) {
	effectJSON, err := useItemEffect(it.Category, raw)
	if err != nil {
		return "", err
	}
	effectJSON, err = resolveEffectTarget(state, effectJSON)
	if err != nil {
		return "", err
	}

	applyJSON, err := player.ApplyEffect(state.PlayerState, effectJSON)
	if err != nil {
		return "", fmt.Errorf("player apply effect: %w", err)
	}
	var aresp effectEnvelope
	if err := json.Unmarshal(applyJSON, &aresp); err != nil {
		return "", fmt.Errorf("parse apply-effect response: %w", err)
	}
	state.PlayerState = aresp.Self

	updatedSelf, err := player.TakeItem(state.PlayerState, it.ID)
	if err != nil {
		return "", fmt.Errorf("player take item: %w", err)
	}
	state.PlayerState = updatedSelf

	return effectMessage(it.Name, aresp.Outcome), nil
}

// applyWield equips one specific item as the Player's wielded weapon —
// called by applyEquip's weapon branch, shared by equipItem's
// 0/1-candidate auto path and resolvePendingSelection's picker-completion
// path.
func applyWield(state *GameState, it item.Item, raw json.RawMessage) (string, error) {
	stats, err := weapon.Stats(raw)
	if err != nil {
		return "", fmt.Errorf("weapon stats: %w", err)
	}
	statsJSON, err := json.Marshal(stats)
	if err != nil {
		return "", fmt.Errorf("marshal weapon stats: %w", err)
	}
	updatedSelf, err := player.Wield(state.PlayerState, it.ID, statsJSON)
	if err != nil {
		return "", fmt.Errorf("player wield: %w", err)
	}
	state.PlayerState = updatedSelf
	return fmt.Sprintf("You wield the %s.", it.Name), nil
}

// applyWear equips one specific item into its own item.ArmorStats.Slot —
// called by applyEquip's armor branch, shared by equipItem's
// 0/1-candidate auto path and resolvePendingSelection's picker-completion
// path.
func applyWear(state *GameState, it item.Item, raw json.RawMessage) (string, error) {
	stats, err := armor.Stats(raw)
	if err != nil {
		return "", fmt.Errorf("armor stats: %w", err)
	}
	statsJSON, err := json.Marshal(stats)
	if err != nil {
		return "", fmt.Errorf("marshal armor stats: %w", err)
	}
	updatedSelf, err := player.Wear(state.PlayerState, it.ID, statsJSON)
	if err != nil {
		return "", fmt.Errorf("player wear: %w", err)
	}
	state.PlayerState = updatedSelf
	return fmt.Sprintf("You put on the %s.", it.Name), nil
}

// applyEquip equips one specific item, dispatching to the equip logic for
// its own Category — shared by equipItem's 0/1-candidate auto path and
// resolvePendingSelection's picker-completion path. Mirrors
// useItemEffect's category dispatch.
func applyEquip(state *GameState, it item.Item, raw json.RawMessage) (string, error) {
	switch it.Category {
	case item.CategoryWeapon:
		return applyWield(state, it, raw)
	case item.CategoryArmor:
		return applyWear(state, it, raw)
	default:
		return "", fmt.Errorf("hack: %s is not equippable", it.Name)
	}
}

// applyLight lights one specific torch, moving it into the Player's
// LitTorch slot — shared by useItem's 0/1-candidate auto path (via
// applyUse) and resolvePendingSelection's picker-completion path.
// Refusing a second torch while one is already lit (ErrTorchAlreadyLit)
// is reported as a friendly message rather than an error, the same way
// applyPickup handles player.ErrInventoryFull.
func applyLight(state *GameState, it item.Item, raw json.RawMessage) (string, error) {
	lifespan, err := torch.Lifespan(raw)
	if err != nil {
		return "", fmt.Errorf("torch lifespan: %w", err)
	}
	updatedSelf, err := player.LightTorch(state.PlayerState, it.ID, lifespan)
	if errors.Is(err, player.ErrTorchAlreadyLit) {
		return "Your torch is already lit.", nil
	}
	if err != nil {
		return "", fmt.Errorf("player light torch: %w", err)
	}
	state.PlayerState = updatedSelf
	return fmt.Sprintf("You light the %s. It should burn for %d turns.", it.Name, lifespan), nil
}

// applyUse routes a chosen use-picker candidate to the pipeline that
// actually knows how to activate its category — the seam a future
// one-shot-picker category plugs into, the same idea as useItemEffect's
// own category switch one level down. useItem and
// resolvePendingSelection's pendingQuaff case both call this rather than
// applyQuaff directly, since the 'A' picker now mixes potions and
// torches together.
func applyUse(state *GameState, it item.Item, raw json.RawMessage) (string, error) {
	switch it.Category {
	case item.CategoryTorch:
		return applyLight(state, it, raw)
	default:
		return applyQuaff(state, it, raw)
	}
}

// useItem uses the sole one-shot item in the Player's Inventory if
// exactly one is carried, opens a picker (setting
// state.PendingSelection) if 2+ are carried, or reports "You have
// nothing to use." if none are. Returns (message, done): done is false
// only when a picker was opened — no turn consumed yet;
// resolvePendingSelection finishes the job on the follow-up keypress.
func useItem(state *GameState) (string, bool, error) {
	cands, err := quaffCandidates(state)
	if err != nil {
		return "", true, err
	}
	switch len(cands) {
	case 0:
		return "You have nothing to use.", true, nil
	case 1:
		it, err := itemInfo(cands[0])
		if err != nil {
			return "", true, err
		}
		msg, err := applyUse(state, it, cands[0])
		return msg, true, err
	default:
		state.PendingSelection = &pendingSelection{Action: pendingQuaff, Candidates: wrapRaw(cands)}
		return "", false, nil
	}
}

// equipItem is useItem's counterpart for equipping: equips the sole
// equippable item (weapon or armor — see equippableCategories) if exactly
// one is carried, opens a picker over both categories together if 2+ are,
// or reports "You have nothing to equip." if none are. Returns (message,
// done) with the same meaning as useItem's.
func equipItem(state *GameState) (string, bool, error) {
	cands, err := equipCandidates(state)
	if err != nil {
		return "", true, err
	}
	switch len(cands) {
	case 0:
		return "You have nothing to equip.", true, nil
	case 1:
		it, err := itemInfo(cands[0])
		if err != nil {
			return "", true, err
		}
		msg, err := applyEquip(state, it, cands[0])
		return msg, true, err
	default:
		state.PendingSelection = &pendingSelection{Action: pendingEquip, Candidates: wrapRaw(cands)}
		return "", false, nil
	}
}

// throwCandidates returns every plain Inventory entry, plus the
// currently-lit torch if any — the set throwItem chooses among. Unlike
// equipCandidates/quaffCandidates, Inventory entries are included
// regardless of Category ("anything in the inventory" can be thrown).
// Wielded/Worn items still aren't throwable (they'd need to be
// un-equipped first), but the lit torch is a deliberate exception —
// throwing your only light source at something oily is how you set it
// ablaze (see resolveThrow's ci.IsLit case).
func throwCandidates(state *GameState) ([]carriedItem, error) {
	var items []carriedItem
	litTorch, hasLitTorch, err := player.LitTorchItem(state.PlayerState)
	if err != nil {
		return nil, err
	}
	if hasLitTorch {
		items = append(items, carriedItem{Raw: litTorch, IsLit: true})
	}
	inv, err := player.Inventory(state.PlayerState)
	if err != nil {
		return nil, err
	}
	for _, raw := range inv {
		items = append(items, carriedItem{Raw: raw})
	}
	return items, nil
}

// throwItem starts a throw: with exactly one candidate, it's chosen
// automatically and hack moves straight to waiting on a direction
// keypress; with 2+, a letter picker opens first (mirroring equipItem's
// shape), and picking a letter then ALSO moves to waiting on a
// direction — see resolvePendingSelection. Unlike equipItem/useItem,
// there's never an "auto-act, turn spent immediately" case: a throw
// always needs a direction, so done is false whenever there's at least
// one candidate. Returns "You have nothing to throw." (done=true, no
// turn) if nothing is carried that can be thrown.
func throwItem(state *GameState) (string, bool, error) {
	items, err := throwCandidates(state)
	if err != nil {
		return "", true, err
	}
	if len(items) == 0 {
		return "You have nothing to throw.", true, nil
	}
	if len(items) == 1 {
		state.PendingSelection = &pendingSelection{Action: pendingThrow, Item: &items[0]}
		return "", false, nil
	}
	state.PendingSelection = &pendingSelection{Action: pendingThrow, Candidates: items}
	return "", false, nil
}

// resolveThrow completes a throw once both its item (ci) and a direction
// (key) are known: builds the same world.AugmentedMap the default melee
// attack path builds, calls player.Throw, places the thrown item on the
// floor at its resting position (Result.X,Y — whether that's a wall it
// bounced off, an agent it hit, or the far end of its own throw.Distance)
// via the same relocateItem helper applyDrop already uses, and — if it
// hit a monster — routes through the shared attackMonster helper exactly
// like a melee attack would. key is checked against the four direction
// names itself, rather than letting player.Throw's own error surface,
// so an unrelated keypress cancels the throw silently — the same "any
// other key cancels" convention resolvePendingSelection's letter-picker
// path already follows.
func resolveThrow(state *GameState, ci carriedItem, key string) (string, error) {
	switch key {
	case "Up", "Down", "Left", "Right":
	default:
		return "", nil // cancelled: not a direction, matches the picker-cancel convention
	}

	it, err := itemInfo(ci.Raw)
	if err != nil {
		return "", err
	}

	playerObj, err := playerObject(state.PlayerState)
	if err != nil {
		return "", err
	}
	liveMonsters, err := liveMonsterObjects(state)
	if err != nil {
		return "", err
	}
	augObjects := append([]overlay.Object{playerObj}, liveMonsters...)
	aug := world.AugmentedMap{Map: *state.Map, Objects: augObjects}
	augJSON, err := json.Marshal(aug)
	if err != nil {
		return "", fmt.Errorf("marshal augmented map: %w", err)
	}

	respJSON, err := player.Throw(state.PlayerState, augJSON, it.ID, key)
	if err != nil {
		return "", fmt.Errorf("player throw: %w", err)
	}
	var tresp actionEnvelope
	if err := json.Unmarshal(respJSON, &tresp); err != nil {
		return "", fmt.Errorf("parse player throw response: %w", err)
	}
	state.PlayerState = tresp.Self

	messages := []string{fmt.Sprintf("You throw the %s.", it.Name)}
	switch {
	case ci.IsLit:
		// Mirrors player.Drop's own "goes out and falls apart" handling
		// for a lit torch — thrown or dropped, it never survives as a
		// floor item.
		messages = append(messages, fmt.Sprintf("The %s goes out and falls apart.", it.Name))
	case it.Category == item.CategoryFlask:
		messages = append(messages, resolveFlaskThrow(state, tresp.Result))
	case it.Fragile:
		messages = append(messages, "It shatters!")
	default:
		relocated, err := relocateItem(ci.Raw, tresp.Result.X, tresp.Result.Y)
		if err != nil {
			return "", err
		}
		state.FloorItems = append(state.FloorItems, relocated)
	}
	if tresp.Result.Action == "attack" {
		if idx, ok := monsterIndexAt(state, tresp.Result.X, tresp.Result.Y); ok {
			attackJSON, err := json.Marshal(tresp.Result)
			if err != nil {
				return "", fmt.Errorf("marshal thrown attack: %w", err)
			}
			msgs, err := attackMonster(state, idx, attackJSON)
			if err != nil {
				return "", err
			}
			messages = append(messages, msgs...)
		}
	}

	return finishTurn(state, messages)
}

// resolveFlaskThrow returns the status message for a thrown flask of
// oil's non-item-surviving impact, and — only for the "landed on open
// ground" case — marks that tile oily in state.OilyTiles. The
// agent-hit case's actual Oiliness increment already happens via Defend
// (action.Result.AddsOiliness, applied by goblin.Defend from the
// attackGoblin call right after this returns); this only supplies the
// message.
func resolveFlaskThrow(state *GameState, result action.Result) string {
	switch {
	case result.Action == "attack":
		return "It shatters, coating them in oil!"
	case result.BlockedByWall:
		return "It shatters against the wall!"
	default:
		if state.OilyTiles == nil {
			state.OilyTiles = map[mapdata.Point]bool{}
		}
		state.OilyTiles[mapdata.Point{X: result.X, Y: result.Y}] = true
		return "It shatters, spreading oil across the floor!"
	}
}

// applyDrop drops one specific carried item: player.Drop removes it from
// Wielded (auto-unwielding, reverting combat to bare hands), LitTorch, or
// Inventory. A dropped lit torch is never placed on the floor — it
// simply disappears — so that case returns early before the
// relocate+FloorItems step everything else goes through. Message varies
// depending on which of the three it was.
func applyDrop(state *GameState, it item.Item, raw json.RawMessage) (string, error) {
	dropJSON, err := player.Drop(state.PlayerState, it.ID)
	if err != nil {
		return "", fmt.Errorf("player drop: %w", err)
	}
	var dresp dropEnvelope
	if err := json.Unmarshal(dropJSON, &dresp); err != nil {
		return "", fmt.Errorf("parse drop response: %w", err)
	}
	state.PlayerState = dresp.Self

	if dresp.WasLitTorch {
		return fmt.Sprintf("You drop the %s. It goes out and falls apart.", it.Name), nil
	}

	playerObj, err := playerObject(state.PlayerState)
	if err != nil {
		return "", err
	}
	relocated, err := relocateItem(dresp.Item, playerObj.X, playerObj.Y)
	if err != nil {
		return "", err
	}
	state.FloorItems = append(state.FloorItems, relocated)

	if dresp.WasWielded {
		return fmt.Sprintf("You drop the %s. You are now fighting bare-handed.", it.Name), nil
	}
	if dresp.WasWorn {
		return fmt.Sprintf("You take off the %s and drop it.", it.Name), nil
	}
	return fmt.Sprintf("You drop the %s.", it.Name), nil
}

// dropItem always opens the picker when 1+ items are carried — unlike
// useItem/equipItem, there's no "auto-act on the sole candidate"
// shortcut, since drop is meant to show everything carried on every
// press, not just when there's an ambiguous choice. Returns "You have
// nothing to drop." (done=true, no turn) if nothing is carried at all.
func dropItem(state *GameState) (string, bool, error) {
	items, err := carriedItems(state)
	if err != nil {
		return "", true, err
	}
	if len(items) == 0 {
		return "You have nothing to drop.", true, nil
	}
	state.PendingSelection = &pendingSelection{Action: pendingDrop, Candidates: items}
	return "", false, nil
}

// resolvePendingSelection interprets key as the follow-up letter for an
// open wield/quaff/drop/pickup picker (state.PendingSelection). A key
// matching one of candidateLetters(len(Candidates)) completes that action
// against the chosen candidate via applyWield/applyQuaff/applyDrop/
// applyPickup and proceeds through finishTurn exactly once — the turn is
// spent on completing the action, not on having opened the picker. Any
// other key — out of range, or unrelated — cancels the pending selection
// silently (no message, no turn spent), matching this codebase's
// existing convention that bad input is absorbed harmlessly (an
// unrecognized move key already falls through to a no-op) rather than
// surfaced as an error.
func resolvePendingSelection(state *GameState, key string) (string, error) {
	sel := state.PendingSelection
	state.PendingSelection = nil // cleared unconditionally: cancel or complete, either way it's resolved

	if sel.Action == pendingThrow && sel.Item != nil {
		return resolveThrow(state, *sel.Item, key)
	}

	letters := candidateLetters(len(sel.Candidates))
	idx := -1
	for i, l := range letters {
		if key == l {
			idx = i
			break
		}
	}
	if idx == -1 {
		return "", nil
	}

	chosen := sel.Candidates[idx].Raw
	it, err := itemInfo(chosen)
	if err != nil {
		return "", err
	}

	if sel.Action == pendingThrow {
		// The item is chosen, but a throw still needs a direction —
		// open a new pendingSelection waiting for that, rather than
		// completing the turn now.
		state.PendingSelection = &pendingSelection{Action: pendingThrow, Item: &sel.Candidates[idx]}
		return "", nil
	}

	var msg string
	switch sel.Action {
	case pendingEquip:
		msg, err = applyEquip(state, it, chosen)
	case pendingQuaff:
		msg, err = applyUse(state, it, chosen)
	case pendingDrop:
		msg, err = applyDrop(state, it, chosen)
	case pendingPickup:
		msg, err = applyPickup(state, it, chosen)
	}
	if err != nil {
		return "", err
	}
	return finishTurn(state, messagesOf(msg))
}

func main() {
	cfg := mapgen.DefaultConfig()

	flag.IntVar(&cfg.Width, "width", cfg.Width, "map width in tiles")
	flag.IntVar(&cfg.Height, "height", cfg.Height, "map height in tiles")
	flag.IntVar(&cfg.NumRooms, "rooms", cfg.NumRooms, "number of rooms to place")
	flag.IntVar(&cfg.MinRoomSize, "min-room", cfg.MinRoomSize, "minimum room interior size")
	flag.IntVar(&cfg.MaxRoomSize, "max-room", cfg.MaxRoomSize, "maximum room interior size")
	flag.Float64Var(&cfg.DarkRoomPercent, "dark-percent", cfg.DarkRoomPercent, "percent of rooms that are dark")
	flag.Float64Var(&cfg.HiddenDoorPercent, "hidden-door-percent", cfg.HiddenDoorPercent, "percent of doors that are hidden")
	flag.Float64Var(&cfg.ExtraCorridorPercent, "extra-corridor-percent", cfg.ExtraCorridorPercent, "extra loop corridors, as a percent of room count")
	flag.Int64Var(&cfg.Seed, "seed", cfg.Seed, "RNG seed (0 = random)")
	cellSize := flag.Int("cell-size", 16, "pixel size of each tile")
	revealHidden := flag.Bool("reveal-hidden", false, "show hidden doors distinctly instead of disguising them as walls")
	showFullMap := flag.Bool("show-full-map", false, "debug: ignore fog of war and always render the entire generated map")
	flag.Parse()

	spawnedOnce := map[string]bool{}
	ls, err := populateLevel(cfg, 1, spawnedOnce)
	if err != nil {
		fmt.Fprintln(os.Stderr, "hack:", err)
		os.Exit(1)
	}

	playerState, err := initPlayer(ls.Map)
	if err != nil {
		fmt.Fprintln(os.Stderr, "hack:", err)
		os.Exit(1)
	}
	p, err := playerObject(playerState)
	if err != nil {
		fmt.Fprintln(os.Stderr, "hack:", err)
		os.Exit(1)
	}

	// Relocate the flask near the Player's spawn point — a level-1-only
	// startup nicety (so it's immediately findable for testing) that
	// isn't generalized to every level populateLevel builds afterward.
	for i, raw := range ls.FloorItems {
		it, err := itemInfo(raw)
		if err != nil {
			fmt.Fprintln(os.Stderr, "hack:", err)
			os.Exit(1)
		}
		if it.Category != item.CategoryFlask {
			continue
		}
		if x, y, ok := tileNearPlayer(ls.Map, p.X, p.Y); ok {
			relocated, err := relocateItem(raw, x, y)
			if err != nil {
				fmt.Fprintln(os.Stderr, "hack:", err)
				os.Exit(1)
			}
			ls.FloorItems[i] = relocated
		}
		break
	}

	state := &GameState{
		PlayerState:  playerState,
		CurrentLevel: 1,
		Levels:       make([]*levelState, maxLevels),
		LevelConfig:  cfg,
		SpawnedOnce:  spawnedOnce,
		levelState:   ls,
	}
	state.Levels[0] = ls

	state.Visited.reveal(state.Map, p.X, p.Y)
	initialLitTiles, _, _ := vision.CurrentlyLit(*state.Map, p.X, p.Y, p.Lit)
	state.Visited.rememberLitTiles(initialLitTiles)

	// currentlyVisibleTiles computes vision.CurrentlyVisible fresh from the
	// Player's current position — the CURRENT-frame visible set, used only
	// to decide which movable objects (monsters/items) are visible right
	// now. Distinct from state.Visited.rememberedTiles(), the PERMANENT
	// floor/wall-shape memory used for LitTiles below — a movable object
	// on a tile you remember but aren't currently in view of must stay
	// hidden even though the floor there keeps rendering.
	currentlyVisibleTiles := func() []mapdata.Point {
		p, err := playerObject(state.PlayerState)
		if err != nil {
			fmt.Fprintln(os.Stderr, "hack:", err)
			os.Exit(1)
		}
		return vision.CurrentlyVisible(*state.Map, p.X, p.Y, p.Lit)
	}

	mapToRender := func() *mapdata.Map {
		if *showFullMap {
			return state.Map
		}
		return state.Visited.visibleMap(state.Map)
	}
	objectsToRender := func() []overlay.Object {
		p, err := playerObject(state.PlayerState)
		if err != nil {
			fmt.Fprintln(os.Stderr, "hack:", err)
			os.Exit(1)
		}
		liveMonsters, err := liveMonsterObjects(state)
		if err != nil {
			fmt.Fprintln(os.Stderr, "hack:", err)
			os.Exit(1)
		}
		all := append([]overlay.Object{p}, liveMonsters...)
		for _, raw := range state.FloorItems {
			it, err := itemInfo(raw)
			if err != nil {
				fmt.Fprintln(os.Stderr, "hack:", err)
				os.Exit(1)
			}
			all = append(all, it.Object)
		}
		if *showFullMap {
			return all
		}
		return state.Visited.filterObjects(state.Map, all, currentlyVisibleTiles())
	}
	renderOptions := func() render.Options {
		return render.Options{RevealHidden: *revealHidden, Objects: objectsToRender(), LitTiles: state.Visited.rememberedTiles()}
	}

	img := render.Image(mapToRender(), renderOptions(), *cellSize)

	a := app.NewWithID("com.map_api.hack")
	w := a.NewWindow(fmt.Sprintf("Hack — seed %d, %d rooms", state.Map.Seed, len(state.Map.Rooms)))

	canvasImg := canvas.NewImageFromImage(img)
	canvasImg.ScaleMode = canvas.ImageScalePixels
	canvasImg.FillMode = canvas.ImageFillOriginal

	status := widget.NewLabel("Last key: (none)")
	w.SetContent(container.NewVBox(canvasImg, status))
	w.Resize(fyne.NewSize(float32(img.Bounds().Dx()), float32(img.Bounds().Dy())+40))

	redraw := func() {
		canvasImg.Image = render.Image(mapToRender(), renderOptions(), *cellSize)
		canvasImg.Refresh()
	}

	// activeDialog is whichever popup (the "i" inventory view or an open
	// wield/quaff picker) is currently showing, if any — nil otherwise.
	// activeDialogIsPicker distinguishes the two: the "i" view dismisses
	// on any key (it's purely informational), but the picker must NOT —
	// it needs the follow-up letter key to reach dispatch/handleKey/
	// resolvePendingSelection, per the plan's investigation into Fyne's
	// dialog focus behavior (dialogs built from plain widget.Label
	// content never call Canvas.Focus, so canvas-level key callbacks
	// keep firing normally while one is shown).
	var activeDialog dialog.Dialog
	var activeDialogIsPicker bool

	// showInventoryDialog is invoked directly from the "I" key, not
	// through dispatch/handleKey, since viewing inventory is a read-only
	// query, not a turn-consuming action — it never touches
	// state.PlayerState and never calls finishTurn.
	showInventoryDialog := func() {
		lines, err := inventoryLines(state)
		if err != nil {
			fmt.Fprintln(os.Stderr, "hack:", err)
			return
		}
		content := widget.NewLabel(strings.Join(lines, "\n"))
		activeDialogIsPicker = false
		activeDialog = dialog.NewCustomWithoutButtons("Inventory", content, w)
		activeDialog.Show()
	}

	// showLookDialog is lookHereLines' presentation, invoked directly
	// from the "L" key exactly like showInventoryDialog is from "I" —
	// looking at the floor is a read-only query, not a turn-consuming
	// action.
	showLookDialog := func() {
		lines, err := lookHereLines(state)
		if err != nil {
			fmt.Fprintln(os.Stderr, "hack:", err)
			return
		}
		content := widget.NewLabel(strings.Join(lines, "\n"))
		activeDialogIsPicker = false
		activeDialog = dialog.NewCustomWithoutButtons("Here", content, w)
		activeDialog.Show()
	}

	// showStatusDialog shows the Player's own Status self-description
	// (HP, Attributes, active modifiers), invoked directly from the "S"
	// key exactly like showInventoryDialog/showLookDialog are from
	// "I"/"L" — a read-only query, not a turn-consuming action. hack
	// never interprets player.Status' fields itself; statusLines just
	// renders whatever label/value pairs come back.
	showStatusDialog := func() {
		fieldsJSON, err := player.Status(state.PlayerState)
		if err != nil {
			fmt.Fprintln(os.Stderr, "hack:", err)
			return
		}
		lines, err := statusLines(fieldsJSON)
		if err != nil {
			fmt.Fprintln(os.Stderr, "hack:", err)
			return
		}
		content := widget.NewLabel(strings.Join(lines, "\n"))
		activeDialogIsPicker = false
		activeDialog = dialog.NewCustomWithoutButtons("Status", content, w)
		activeDialog.Show()
	}

	// showHelpDialog shows the full command list (helpLines), invoked
	// directly from the "?" key exactly like showInventoryDialog/
	// showLookDialog/showStatusDialog are from "I"/"L"/"S" — a read-only
	// query, not a turn-consuming action. Unlike its three siblings, "?"
	// has no fyne.KeyName distinct from plain "/" (it's Shift+/), so it's
	// wired up from SetOnTypedRune below instead of SetOnTypedKey.
	showHelpDialog := func() {
		content := widget.NewLabel(strings.Join(helpLines(), "\n"))
		activeDialogIsPicker = false
		activeDialog = dialog.NewCustomWithoutButtons("Help", content, w)
		activeDialog.Show()
	}

	dispatch := func(key, label string) {
		text := fmt.Sprintf("Last key: %s", label)
		message, err := handleKey(state, key)
		if err != nil {
			fmt.Fprintln(os.Stderr, "hack:", err)
			status.SetText(text)
			return
		}
		if state.PendingSelection != nil {
			// A picker resolving into another picker (a throw's item
			// letter resolving into its direction prompt) must hide
			// whatever dialog is already showing before opening the
			// next one — otherwise the old dialog is simply orphaned:
			// reassigning activeDialog below stops this function from
			// ever being able to reach it again, but never actually
			// hides the widget still on screen.
			if activeDialog != nil {
				activeDialog.Hide()
				activeDialog = nil
				activeDialogIsPicker = false
			}
			if state.PendingSelection.Item != nil {
				it, ierr := itemInfo(state.PendingSelection.Item.Raw)
				if ierr != nil {
					fmt.Fprintln(os.Stderr, "hack:", ierr)
					return
				}
				content := widget.NewLabel(fmt.Sprintf("Throw the %s — which direction?", it.Name))
				activeDialogIsPicker = true
				activeDialog = dialog.NewCustomWithoutButtons("Throw", content, w)
				activeDialog.Show()
				status.SetText(text)
				return
			}
			lines, lerr := pickerLines(state.PendingSelection.Candidates)
			if lerr != nil {
				fmt.Fprintln(os.Stderr, "hack:", lerr)
				return
			}
			content := widget.NewLabel(strings.Join(lines, "\n"))
			activeDialogIsPicker = true
			activeDialog = dialog.NewCustomWithoutButtons("Choose one", content, w)
			activeDialog.Show()
			status.SetText(text)
			return
		}
		if activeDialog != nil {
			activeDialog.Hide()
			activeDialog = nil
			activeDialogIsPicker = false
		}
		if message != "" {
			text += " — " + message
		}
		status.SetText(text)
		redraw()
	}

	// gameKeysHandledByTypedKey is every printable-character key that's
	// also a game command. Each one has a fyne.KeyName (KeyPeriod=".",
	// KeyComma=",", KeyA="A"), so it reaches SetOnTypedKey below — and,
	// being printable, ALSO fires SetOnTypedRune for the character it
	// types. Letting both callbacks react (even just the rune callback
	// overwriting the status text after SetOnTypedKey already dispatched
	// the real turn) either double-runs the turn or stomps its message —
	// this is the exact bug "." had before this guard existed. So every
	// key in this set must be ignored by SetOnTypedRune and handled by
	// SetOnTypedKey only. This does NOT cover the picker's dynamic
	// candidate letters (B-J) — those are handled by unconditionally
	// skipping SetOnTypedRune entirely while a picker is open, below.
	gameKeysHandledByTypedKey := map[rune]bool{'.': true, ',': true, 'a': true, 'A': true, 'e': true, 'E': true, 'i': true, 'I': true, 'd': true, 'D': true, 'l': true, 'L': true, 't': true, 'T': true, 's': true, 'S': true}

	// modifierKeys never mean anything on their own — Shift, Control,
	// Alt/Option, and Command/Super only matter held in combination with
	// another key, which arrives as that other key's own event (with the
	// modifier reflected in ev.Physical/ev.Name's own delivery, not as a
	// separate turn). Left unfiltered, tapping one alone would reach
	// SetOnTypedKey as its own named key (e.g. "LeftShift") and dispatch
	// as an unrecognized key, silently consuming a turn.
	modifierKeys := map[fyne.KeyName]bool{
		desktop.KeyShiftLeft:    true,
		desktop.KeyShiftRight:   true,
		desktop.KeyControlLeft:  true,
		desktop.KeyControlRight: true,
		desktop.KeyAltLeft:      true,
		desktop.KeyAltRight:     true,
		desktop.KeySuperLeft:    true,
		desktop.KeySuperRight:   true,
	}

	w.Canvas().SetOnTypedRune(func(r rune) {
		// While a picker is open, every key (including its dynamic
		// candidate letters B-J, which aren't in gameKeysHandledByTypedKey
		// since they're not fixed game commands) must reach SetOnTypedKey
		// only — otherwise this callback's fallthrough status-text branch
		// below would stomp the message dispatch just set, reproducing
		// the exact double-dispatch bug gameKeysHandledByTypedKey exists
		// to prevent, just for whichever letter resolves the picker.
		if activeDialogIsPicker {
			return
		}
		// Recognized game keys (including "I", which OPENS the inventory
		// dialog via SetOnTypedKey) must be checked before the
		// "activeDialog != nil dismisses" branch below, and not just
		// ignored alongside it — GLFW fires SetOnTypedKey before
		// SetOnTypedRune for the same keystroke, so by the time this rune
		// callback runs for "I", SetOnTypedKey has *already* opened the
		// dialog this same press; checking activeDialog first would treat
		// that as "a key arrived while a dialog is showing" and instantly
		// dismiss the dialog it just opened, in the same keystroke.
		if gameKeysHandledByTypedKey[r] {
			return
		}
		if activeDialog != nil {
			activeDialog.Hide()
			activeDialog = nil
			activeDialogIsPicker = false
			return
		}
		if r == 'q' || r == 'Q' {
			w.Close()
			return
		}
		// "<"/">" (Shift+,/Shift+.) have no fyne.KeyName of their own —
		// Fyne only reports the physical key (","/".") to SetOnTypedKey
		// regardless of Shift, so this rune callback is the only place
		// they can be told apart from plain ,/. at all. See
		// SetOnTypedKey's own CurrentKeyModifiers check below, which
		// suppresses the physical ,/. dispatch this same keystroke would
		// otherwise also fire.
		if r == '<' || r == '>' {
			dispatch(string(r), string(r))
			return
		}
		// "?" (Shift+/) has the same no-fyne.KeyName-of-its-own problem as
		// "<"/">" above, and the same fix: told apart from plain "/" only
		// here, and called directly (not via dispatch) since viewing help
		// is read-only, like showInventoryDialog/showLookDialog/
		// showStatusDialog. See SetOnTypedKey's own CurrentKeyModifiers
		// check below, which suppresses the physical "/" dispatch this
		// same keystroke would otherwise also fire.
		if r == '?' {
			showHelpDialog()
			return
		}
		status.SetText(fmt.Sprintf("Last key: %q", r))
	})
	w.Canvas().SetOnTypedKey(func(ev *fyne.KeyEvent) {
		if activeDialog != nil && !activeDialogIsPicker {
			activeDialog.Hide()
			activeDialog = nil
			activeDialogIsPicker = false
			return
		}
		if ev.Name == fyne.KeyEscape {
			if activeDialogIsPicker {
				// Route through dispatch like any other key while a
				// picker is open, so resolvePendingSelection's catch-all
				// cancels the picker instead of Escape closing the whole
				// window mid-selection.
				dispatch(string(ev.Name), string(ev.Name))
				return
			}
			w.Close()
			return
		}
		if modifierKeys[ev.Name] {
			return
		}
		// Shift+,/Shift+./Shift+/ produce '<'/'>'/'?', which SetOnTypedRune
		// handles (see above) — without this check, the physical ,/./ key
		// would still reach the dispatch below and fire its own
		// pickup/wait/wasted-turn as an unwanted side effect of the same
		// keystroke (plain "/" is unbound, so it'd otherwise silently cost
		// a turn on every "?" press). fyne.KeyEvent itself carries no
		// modifier state, so this asks the driver directly for what's
		// currently held.
		if driver, ok := fyne.CurrentApp().Driver().(desktop.Driver); ok &&
			driver.CurrentKeyModifiers()&fyne.KeyModifierShift != 0 &&
			(ev.Name == fyne.KeyComma || ev.Name == fyne.KeyPeriod || ev.Name == fyne.KeySlash) {
			return
		}
		if ev.Name == fyne.KeyI && !activeDialogIsPicker {
			showInventoryDialog()
			return
		}
		if ev.Name == fyne.KeyL && !activeDialogIsPicker {
			showLookDialog()
			return
		}
		if ev.Name == fyne.KeyS && !activeDialogIsPicker {
			showStatusDialog()
			return
		}
		dispatch(string(ev.Name), string(ev.Name))
	})

	// ShowAndRun blocks until the window is closed, then returns, so main
	// exits right after.
	w.ShowAndRun()
}
