// Package player will eventually be its own standalone Player microservice.
// For now it's an in-process Go package with exactly one exported symbol,
// Init, whose signature is already JSON-in/JSON-out — the same shape an
// HTTP handler's request/response body would have, so swapping this for a
// real network call later doesn't change how callers use it.
package player

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"sort"
	"sync/atomic"

	"micromap/server/internal/action"
	"micromap/server/internal/attributes"
	"micromap/server/internal/fire"
	"micromap/server/internal/item"
	"micromap/server/internal/mapdata"
	"micromap/server/internal/overlay"
	"micromap/server/internal/slide"
	"micromap/server/internal/status"
	"micromap/server/internal/throw"
	"micromap/server/internal/world"
)

var nextID int64

// person is this package's private representation of a player character.
// It embeds overlay.Object so the JSON Init returns is field-for-field the
// same shape as the generic overlay contract render already consumes —
// callers never need a player-specific type to use the result.
type person struct {
	overlay.Object
	HP int
	// MaxHP is the Player's hit-point ceiling, set once at Init and never
	// changed afterward. Healing (ApplyEffect's item.EffectHeal case)
	// clamps HP to this value; nothing else currently reads it, but it's
	// tracked from the start so a future max-HP-raising item wouldn't
	// need a wire-shape change.
	MaxHP int
	// Inventory is every item this Player is carrying, each entry exactly
	// what that item's own Init/Use last returned — carried opaquely, the
	// same way Goblin's private Aware field is opaque to hack. This
	// package never inspects an entry's category-specific contents,
	// beyond finding one by ID (TakeItem) or handing the whole list back
	// out (Inventory) for the coordinator to inspect generically via
	// item.Item.
	Inventory []json.RawMessage `json:"inventory,omitempty"`
	// ActiveEffects is every sustained item.Effect (Duration > 0)
	// currently in progress on the Player — e.g. a drunk invisibility
	// potion's remaining turns. Counted down once per turn by Tick.
	ActiveEffects []item.ActiveEffect `json:"active_effects,omitempty"`
	// Wielded is the Player's single currently-wielded weapon, if any:
	// both its own raw item state (so it can be moved back into
	// Inventory if a different weapon is wielded later) and the neutral
	// item.WeaponStats snapshot computed once at Wield time (via
	// weapon.Stats). Action reads only Stats to compute an attack's
	// damage range — this package never imports weapon or parses a
	// weapon's private JSON shape itself.
	Wielded *wieldedWeapon `json:"wielded,omitempty"`
	// LitTorch is the Player's single currently-burning torch, if any —
	// the torch analog of Wielded, but for a resource that expires on
	// its own (see Tick) rather than persisting until dropped/swapped.
	// Its own raw item state is kept so Drop can recognize it by ID.
	LitTorch *litTorch `json:"lit_torch,omitempty"`
	// Worn is every currently-worn item, keyed by which body slot it
	// occupies — a map rather than a dedicated field per slot (unlike
	// Wielded/LitTorch) so a future second slot (a helm's "head", ...)
	// is just a new item.Slot constant and a new armor Kind, not another
	// near-identical block of Player/hack code.
	Worn map[item.Slot]*wornItem `json:"worn,omitempty"`
	// Attributes are the Player's core stats, rolled once at Init from
	// this package's own range (6-20) and never changed afterward.
	// Nothing consumes them yet — see the attributes package doc.
	Attributes attributes.Attributes `json:"attributes"`
	// BurningTurnsRemaining counts down once per Tick while the Player
	// is on fire (see overlay.Object.Burning, Defend's Ignites
	// handling) — the burning analog of LitTorch.TurnsRemaining, but for
	// a status effect rather than an equipped resource.
	BurningTurnsRemaining int `json:"burning_turns_remaining,omitempty"`
	// BurningStartsNextTurn is true for exactly one Tick after a fresh
	// ignition (see Defend's Ignites handling) — the turn someone
	// catches fire, they don't yet take damage from it; Tick consumes
	// this flag (clearing it, applying no damage, not decrementing
	// BurningTurnsRemaining) the first time it sees it set, so the fire
	// only starts actually burning on the following turn. Not set again
	// by a re-ignition while already burning (self.BurningTurnsRemaining
	// > 0 at the time of the hit) — only a fresh ignition delays.
	BurningStartsNextTurn bool `json:"burning_starts_next_turn,omitempty"`
}

// wieldedWeapon pairs a wielded weapon's own raw item state with the
// combat-stats snapshot taken from it at wield time.
type wieldedWeapon struct {
	Item  json.RawMessage  `json:"item"`
	Stats item.WeaponStats `json:"stats"`
}

// litTorch pairs a lit torch's own raw item state with how many turns
// remain before it burns out.
type litTorch struct {
	Item           json.RawMessage `json:"item"`
	TurnsRemaining int             `json:"turns_remaining"`
}

// wornItem pairs a worn item's own raw item state with the passive
// stats snapshot taken from it at wear time — the armor analog of
// wieldedWeapon.
type wornItem struct {
	Item  json.RawMessage `json:"item"`
	Stats item.ArmorStats `json:"stats"`
}

// response is what Action returns: the Player's own resulting state
// (exactly what Init/Action have always returned, unchanged shape) plus a
// report of what actually happened this turn.
type response struct {
	Self   person        `json:"self"`
	Result action.Result `json:"result"`
}

// defendResponse is what Defend returns: the Player's own resulting state
// plus a report of the incoming attack's outcome.
type defendResponse struct {
	Self    person               `json:"self"`
	Outcome action.DefendOutcome `json:"outcome"`
}

// Init creates a new player character on the given generated level (a
// JSON-encoded mapdata.Map) and returns it as JSON.
func Init(mapJSON []byte) ([]byte, error) {
	var m mapdata.Map
	if err := json.Unmarshal(mapJSON, &m); err != nil {
		return nil, fmt.Errorf("player: parse map: %w", err)
	}
	if len(m.Rooms) == 0 {
		return nil, fmt.Errorf("player: map has no rooms to spawn in")
	}

	// Seed from the map's own seed rather than using math/rand's global
	// source, which Go auto-seeds randomly at process startup — that would
	// make the player's spawn point (and therefore the initial fog-of-war
	// view) different on every run even when the map itself is reproduced
	// exactly via the same --seed.
	rng := rand.New(rand.NewSource(m.Seed))
	r := m.Rooms[rng.Intn(len(m.Rooms))]
	x := r.X + rng.Intn(r.W)
	y := r.Y + rng.Intn(r.H)
	// Drawn immediately after spawn position, from the same seeded rng, so
	// starting HP is reproducible alongside spawn point for a given map
	// seed. Only this package knows the Player's HP range (10-20). The
	// Player starts at full health, so MaxHP is set to the same rolled
	// value.
	hp := 10 + rng.Intn(11)
	// Drawn immediately after HP, from the same seeded rng, so attributes
	// are reproducible alongside spawn point and HP for a given map
	// seed. Only this package knows the Player's attribute range (6-20).
	attrs := attributes.Range{Min: 6, Max: 20}.Roll(rng)

	p := person{
		Object: overlay.Object{
			ID:    fmt.Sprintf("player:%d", atomic.AddInt64(&nextID, 1)),
			X:     x,
			Y:     y,
			Glyph: '@',
			Color: overlay.Color{R: 255, G: 255, B: 255},
			Layer: 2,
			Kind:  "player",
			// Player is the weight baseline every other object's weight
			// is scaled against; see overlay.Object.Weight.
			Weight: 100,
			// The Player always starts at level 1; see
			// overlay.Object.Level. Unlike a monster's, this is free to
			// change later — nothing does yet.
			Level: 1,
		},
		HP:         hp,
		MaxHP:      hp,
		Attributes: attrs,
	}

	return json.Marshal(p)
}

// Action applies a keystroke to the player. selfJSON is exactly what this
// Player last returned from Init or Action (or Defend) — its own private
// state, including HP — and augmentedMapJSON is the current map plus every
// object's generic position/appearance, fed by the coordinator. The
// Player's own position is taken from selfJSON, not looked up in
// augmentedMapJSON's object list (that list may include a stale copy of
// the Player's own generic fields, but never its private ones). Returns
// {"self": ..., "result": ...} as JSON: self is the Player's own resulting
// state, result reports what happened — a move (recognized keys "Up",
// "Down", "Left", "Right"; an unrecognized key or a move blocked by
// terrain reports a no-op move at the Player's unchanged position), a
// deliberate no-op ("." — wait a turn in place, same reported result shape
// as a blocked move, but intentional rather than incidental), or, if the
// destination tile is legal terrain but occupied by any other object, an
// attack against whatever's there instead of moving onto it.
func Action(selfJSON, augmentedMapJSON []byte, key string) ([]byte, error) {
	var self person
	if err := json.Unmarshal(selfJSON, &self); err != nil {
		return nil, fmt.Errorf("player: parse self: %w", err)
	}

	var aug world.AugmentedMap
	if err := json.Unmarshal(augmentedMapJSON, &aug); err != nil {
		return nil, fmt.Errorf("player: parse augmented map: %w", err)
	}

	dx, dy := 0, 0
	switch key {
	case ".":
		// Deliberate no-op: sit still for a turn. dx,dy stay 0,0.
	case "Up":
		dy = -1
	case "Down":
		dy = 1
	case "Left":
		dx = -1
	case "Right":
		dx = 1
	}

	result := action.Result{Action: "move", X: self.X, Y: self.Y}

	if dx != 0 || dy != 0 {
		nx, ny := self.X+dx, self.Y+dy
		if aug.Map.CanMove(self.X, self.Y, nx, ny) {
			if target, occupied := occupantAt(aug.Objects, self.ID, nx, ny); occupied {
				result = attackResult(self.Wielded, target, self.Attributes)
			} else {
				fx, fy := nx, ny
				if isOily(aug.OilyTiles, nx, ny) {
					fx, fy = slide.Resolve(aug.Map, aug.Objects, nx, ny, dx, dy)
				}
				self.X, self.Y = fx, fy
				result = action.Result{Action: "move", X: self.X, Y: self.Y}
			}
		}
	}

	return json.Marshal(response{Self: self, Result: result})
}

// attackResult builds the attack action.Result against target: the
// wielded weapon's stats (adjusted for target.Kind) if one is wielded,
// or bare hands (1-2 damage) otherwise. attackerAttributes is carried
// along on the Result unchanged (see action.Result.Attributes) so Defend
// can apply a Strength-based bonus to either branch alike.
func attackResult(wielded *wieldedWeapon, target overlay.Object, attackerAttributes attributes.Attributes) action.Result {
	if wielded != nil {
		min, max := wielded.Stats.Range(target.Kind)
		return action.Result{Action: "attack", X: target.X, Y: target.Y, Weapon: wielded.Stats.Name, DamageMin: min, DamageMax: max, Attributes: attackerAttributes}
	}
	return action.Result{Action: "attack", X: target.X, Y: target.Y, Weapon: "hands", DamageMin: 1, DamageMax: 2, Attributes: attackerAttributes}
}

// Throw removes the Inventory item identified by itemID from selfJSON
// and simulates it flying in direction key (Up/Down/Left/Right) away
// from the Player's own position, up to throw.Distance(item's own
// Weight, self.Attributes.Strength) cells — stopping at whichever comes
// first: a wall (aug.Map.CanMove returns false) or another intelligent
// agent occupying the next cell (occupantAt), exactly the same
// single-step primitives Action already uses for movement and melee.
//
// Hitting an agent is reported as action.Result{Action:"attack", ...},
// the same shape a melee attack produces (see attackResult), so the
// coordinator feeds it to the target's own Defend completely unchanged.
// DamageMin/DamageMax come from throw.Damage(item's Weight, the
// thrower's own Strength), not a wielded weapon's stats, since any
// inventory item can be thrown — and, unlike a melee attack, Attributes
// is deliberately left unset on the Result: throw.Damage already folds
// the thrower's Strength into DamageMax before the roll, so Defend must
// not also apply its usual post-roll Strength/10 bonus on top, or the
// Strength contribution would be counted twice. If it hits nobody
// (blocked by a wall, or it travels the full distance), Result.Action is
// "throw" and Result.X,Y is the item's final resting cell, for the
// coordinator to place it on the floor there — which may be the
// Player's own tile, if thrown straight into an adjacent wall.
func Throw(selfJSON, augmentedMapJSON []byte, itemID, key string) ([]byte, error) {
	var self person
	if err := json.Unmarshal(selfJSON, &self); err != nil {
		return nil, fmt.Errorf("player: parse self: %w", err)
	}
	var aug world.AugmentedMap
	if err := json.Unmarshal(augmentedMapJSON, &aug); err != nil {
		return nil, fmt.Errorf("player: parse augmented map: %w", err)
	}

	var thrown item.Item
	wasLitTorch := false
	if self.LitTorch != nil {
		if err := json.Unmarshal(self.LitTorch.Item, &thrown); err != nil {
			return nil, fmt.Errorf("player: parse lit torch: %w", err)
		}
		if thrown.ID == itemID {
			wasLitTorch = true
			self.LitTorch = nil
			self.Lit = false
		}
	}
	if !wasLitTorch {
		idx, err := indexInInventory(self.Inventory, itemID)
		if err != nil {
			return nil, err
		}
		if idx == -1 {
			return nil, fmt.Errorf("player: no inventory item with ID %q", itemID)
		}
		if err := json.Unmarshal(self.Inventory[idx], &thrown); err != nil {
			return nil, fmt.Errorf("player: parse thrown item: %w", err)
		}
		self.Inventory = append(self.Inventory[:idx], self.Inventory[idx+1:]...)
	}

	dx, dy := 0, 0
	switch key {
	case "Up":
		dy = -1
	case "Down":
		dy = 1
	case "Left":
		dx = -1
	case "Right":
		dx = 1
	default:
		return nil, fmt.Errorf("player: invalid throw direction %q", key)
	}

	distance := throw.Distance(thrown.Weight, self.Attributes.Strength)
	result := action.Result{Action: "throw", X: self.X, Y: self.Y}
	x, y := self.X, self.Y
	for i := 0; i < distance; i++ {
		nx, ny := x+dx, y+dy
		if !aug.Map.CanMove(x, y, nx, ny) {
			result.BlockedByWall = true
			break
		}
		if target, occupied := occupantAt(aug.Objects, self.ID, nx, ny); occupied {
			min, max := throw.Damage(thrown.Weight, self.Attributes.Strength)
			result = action.Result{
				Action: "attack", X: target.X, Y: target.Y, Weapon: thrown.Name,
				DamageMin: min, DamageMax: max, AddsOiliness: thrown.Category == item.CategoryFlask,
				Ignites: wasLitTorch,
			}
			x, y = nx, ny
			break
		}
		x, y = nx, ny
		result = action.Result{Action: "throw", X: x, Y: y}
	}

	return json.Marshal(response{Self: self, Result: result})
}

// Defend applies an incoming attack (a JSON-encoded action.Result, as
// produced by whichever entity attacked) to the Player found in selfJSON:
// rolls damage in the attack's [DamageMin,DamageMax] range, subtracts it
// from HP, and marks the Player Dead if HP drops to 0 or below. Returns
// {"self": ..., "outcome": ...} as JSON.
func Defend(selfJSON, attackJSON []byte) ([]byte, error) {
	var self person
	if err := json.Unmarshal(selfJSON, &self); err != nil {
		return nil, fmt.Errorf("player: parse self: %w", err)
	}

	var attack action.Result
	if err := json.Unmarshal(attackJSON, &attack); err != nil {
		return nil, fmt.Errorf("player: parse attack: %w", err)
	}

	damage := action.RollDamage(attack.DamageMin, attack.DamageMax)
	// A flat bonus from the attacker's own Strength — floor(Strength/10),
	// so 0-9 contributes nothing, 10-19 contributes +1, 20-29 +2, and so
	// on. Go's integer division already truncates toward zero, which for
	// a non-negative Strength is exactly floor. Applies to weapon and
	// bare-handed attacks alike, since attackResult sets Attributes on
	// both branches.
	damage += attack.Attributes.Strength / 10
	self.HP -= damage
	if self.HP <= 0 {
		self.Dead = true
	}
	// Independent of damage — a hit can both hurt and add oiliness (e.g.
	// a thrown flask of oil), or neither, or either alone.
	if attack.AddsOiliness {
		self.Oiliness++
	}
	// Igniting requires already being oily — a lit torch thrown at a dry
	// target does nothing extra. A second ignition while already
	// burning extends the fire rather than resetting it, and doesn't
	// re-delay damage (only a fresh ignition starts next turn).
	if attack.Ignites && self.Oiliness > 0 {
		if self.BurningTurnsRemaining == 0 {
			self.BurningStartsNextTurn = true
		}
		self.BurningTurnsRemaining += fire.Duration(self.Oiliness)
		self.Burning = true
	}

	outcome := action.DefendOutcome{Damage: damage, RemainingHP: self.HP, Dead: self.Dead}
	return json.Marshal(defendResponse{Self: self, Outcome: outcome})
}

// effectResponse is what ApplyEffect returns: the Player's own resulting
// state plus a report of what the effect actually did.
type effectResponse struct {
	Self    person             `json:"self"`
	Outcome item.EffectOutcome `json:"outcome"`
}

// ApplyEffect applies a used item's effect (a JSON-encoded item.Effect,
// as returned by whichever item category's Use the coordinator called,
// e.g. potion.Use) to the Player found in selfJSON. Mirrors Defend's
// shape exactly, and for the same reason: only this package may read or
// write the Player's own private fields (HP), so hack passes the effect
// through rather than touching HP itself. Returns {"self": ...,
// "outcome": ...} as JSON.
//
// item.EffectHeal increases HP by Amount, clamped so it never exceeds
// MaxHP — Outcome.Applied reports the actual amount restored, which may
// be less than Amount if the clamp reduced it (e.g. healing while already
// near full health). Defend, by contrast, still allows HP to go negative
// (see goblin's TestDefendKillsAtOrBelowZeroHP) — MaxHP is a ceiling on
// healing, not a general HP bound. item.EffectInvisibility sets Invisible
// and records an ActiveEffect whose countdown Tick advances each turn.
// item.EffectTeleport overwrites X,Y with the effect's own (already
// resolved by the coordinator — see hack's resolveEffectTarget; this
// package has no map of its own to pick a destination from). Any other
// EffectKind returns an error — this package only claims to handle
// effects it actually implements, rather than silently no-op'ing kinds a
// future item might introduce.
func ApplyEffect(selfJSON, effectJSON []byte) ([]byte, error) {
	var self person
	if err := json.Unmarshal(selfJSON, &self); err != nil {
		return nil, fmt.Errorf("player: parse self: %w", err)
	}
	var effect item.Effect
	if err := json.Unmarshal(effectJSON, &effect); err != nil {
		return nil, fmt.Errorf("player: parse effect: %w", err)
	}

	var outcome item.EffectOutcome
	switch effect.Kind {
	case item.EffectHeal:
		before := self.HP
		self.HP += effect.Amount
		if self.HP > self.MaxHP {
			self.HP = self.MaxHP
		}
		outcome = item.EffectOutcome{Kind: effect.Kind, Applied: self.HP - before, RemainingHP: self.HP}
	case item.EffectInvisibility:
		self.Invisible = true
		self.ActiveEffects = append(self.ActiveEffects, item.ActiveEffect{
			Effect:         effect,
			TurnsRemaining: effect.Duration,
		})
		outcome = item.EffectOutcome{Kind: effect.Kind, Applied: effect.Duration}
	case item.EffectTeleport:
		self.X, self.Y = effect.X, effect.Y
		outcome = item.EffectOutcome{Kind: effect.Kind}
	default:
		return nil, fmt.Errorf("player: unsupported effect kind %q", effect.Kind)
	}

	return json.Marshal(effectResponse{Self: self, Outcome: outcome})
}

// tickResponse is what Tick returns: the Player's own resulting state
// plus a report of which effects expired this call.
type tickResponse struct {
	Self    person           `json:"self"`
	Outcome item.TickOutcome `json:"outcome"`
}

// Tick advances every one of the Player's ActiveEffects by one turn:
// decrements TurnsRemaining, drops any that reach 0, and recomputes
// Invisible as true iff at least one remaining ActiveEffect is
// EffectInvisibility — never simply cleared on any one expiry — so two
// overlapping invisibility potions leave the Player invisible until the
// longer-lasting one itself expires, rather than the first one to expire
// clobbering the second's still-active state. Separately (LitTorch isn't
// an ActiveEffect), also decrements LitTorch's own countdown, clearing it
// and reporting Outcome.TorchBurnedOut when it reaches 0 — Lit is then
// recomputed from whether LitTorch is still set, the same "recompute from
// current state" pattern Invisible already follows. Returns {"self": ...,
// "outcome": ...} as JSON, mirroring Defend/ApplyEffect's envelope shape;
// Outcome.Expired lists which EffectKinds ran out this call.
//
// An ActiveEffect's (or a lit torch's) countdown begins the very turn
// it's applied, since hack calls Tick as the last step of every turn
// regardless of what happened earlier in it.
func Tick(selfJSON []byte) ([]byte, error) {
	var self person
	if err := json.Unmarshal(selfJSON, &self); err != nil {
		return nil, fmt.Errorf("player: parse self: %w", err)
	}

	var remaining []item.ActiveEffect
	var expired []item.EffectKind
	for _, ae := range self.ActiveEffects {
		ae.TurnsRemaining--
		if ae.TurnsRemaining <= 0 {
			expired = append(expired, ae.Effect.Kind)
			continue
		}
		remaining = append(remaining, ae)
	}
	self.ActiveEffects = remaining

	self.Invisible = false
	for _, ae := range self.ActiveEffects {
		if ae.Effect.Kind == item.EffectInvisibility {
			self.Invisible = true
			break
		}
	}

	var torchBurnedOut bool
	if self.LitTorch != nil {
		self.LitTorch.TurnsRemaining--
		if self.LitTorch.TurnsRemaining <= 0 {
			self.LitTorch = nil
			torchBurnedOut = true
		}
	}
	self.Lit = self.LitTorch != nil

	var burnDamage int
	var burnedOut bool
	if self.BurningStartsNextTurn {
		// The grace turn: caught fire this same turn, so no damage yet
		// — the fire actually starts burning next Tick.
		self.BurningStartsNextTurn = false
	} else if self.BurningTurnsRemaining > 0 {
		burnDamage = fire.TickDamage()
		self.HP -= burnDamage
		self.BurningTurnsRemaining--
		if self.HP <= 0 {
			self.Dead = true
		}
		if self.BurningTurnsRemaining <= 0 {
			self.Burning = false
			burnedOut = true
		}
	}

	outcome := item.TickOutcome{Expired: expired, TorchBurnedOut: torchBurnedOut, BurnDamage: burnDamage, BurnedOut: burnedOut}
	return json.Marshal(tickResponse{Self: self, Outcome: outcome})
}

// ErrInventoryFull is returned by PickUp when the Player's Inventory is
// already at item.InventoryCapacity. Unlike TakeItem/Wield's "no such
// item ID" errors — which signal the coordinator lost sync with the
// Player's own state and are always unexpected — this is a normal,
// common outcome of ordinary play (walking onto an item while already
// full), so hack checks for it specifically (via errors.Is) and reports
// it as a friendly in-game message rather than an internal error.
var ErrInventoryFull = errors.New("player: inventory is full")

// PickUp appends itemJSON — an item's own last-returned raw state, as
// found sitting on the floor — to the Player's Inventory. This package
// never parses itemJSON's contents. Returns the Player's updated own
// state as JSON directly — no envelope, like Init: there's no "what
// happened" report beyond the state change itself. Returns
// ErrInventoryFull, leaving self unchanged, if Inventory already holds
// item.InventoryCapacity items — the caller is expected to leave itemJSON
// wherever it was (e.g. still on the floor) in that case.
func PickUp(selfJSON, itemJSON []byte) ([]byte, error) {
	var self person
	if err := json.Unmarshal(selfJSON, &self); err != nil {
		return nil, fmt.Errorf("player: parse self: %w", err)
	}
	if len(self.Inventory) >= item.InventoryCapacity {
		return nil, ErrInventoryFull
	}
	self.Inventory = append(self.Inventory, json.RawMessage(itemJSON))
	return json.Marshal(self)
}

// TakeItem removes the Inventory entry whose generic ID matches itemID
// from the Player found in selfJSON and returns the Player's updated own
// state as JSON directly (no envelope, for the same reason PickUp has
// none). Used after a used item's effect has already been applied via
// ApplyEffect, to discard the now-consumed item. Errors if no Inventory
// entry has that ID — that would mean the coordinator lost sync with the
// Player's own state.
func TakeItem(selfJSON []byte, itemID string) ([]byte, error) {
	var self person
	if err := json.Unmarshal(selfJSON, &self); err != nil {
		return nil, fmt.Errorf("player: parse self: %w", err)
	}
	idx, err := indexInInventory(self.Inventory, itemID)
	if err != nil {
		return nil, err
	}
	if idx == -1 {
		return nil, fmt.Errorf("player: no inventory item with ID %q", itemID)
	}
	self.Inventory = append(self.Inventory[:idx], self.Inventory[idx+1:]...)
	return json.Marshal(self)
}

// indexInInventory returns the index of the Inventory entry whose
// generic ID matches itemID, or -1 if none does.
func indexInInventory(inventory []json.RawMessage, itemID string) (int, error) {
	for i, raw := range inventory {
		var obj overlay.Object
		if err := json.Unmarshal(raw, &obj); err != nil {
			return -1, fmt.Errorf("player: parse inventory item: %w", err)
		}
		if obj.ID == itemID {
			return i, nil
		}
	}
	return -1, nil
}

// Wield equips the Inventory entry whose generic ID matches itemID as the
// Player's single wielded weapon. statsJSON is the JSON-encoded
// item.WeaponStats already computed from that same item by weapon.Stats —
// hack computes it and passes it through, the same coordinator-mediates
// pattern ApplyEffect already follows for item.Effect. Only one weapon
// can be wielded at a time: if the Player already has one wielded, its
// raw item state is appended back into Inventory before the new one is
// wielded, so wielding a second weapon automatically swaps rather than
// requiring an explicit unwield first. Errors if no Inventory entry has
// itemID. Returns the Player's updated own state as JSON directly (no
// envelope — same as PickUp/TakeItem).
func Wield(selfJSON []byte, itemID string, statsJSON []byte) ([]byte, error) {
	var self person
	if err := json.Unmarshal(selfJSON, &self); err != nil {
		return nil, fmt.Errorf("player: parse self: %w", err)
	}
	var stats item.WeaponStats
	if err := json.Unmarshal(statsJSON, &stats); err != nil {
		return nil, fmt.Errorf("player: parse weapon stats: %w", err)
	}
	idx, err := indexInInventory(self.Inventory, itemID)
	if err != nil {
		return nil, err
	}
	if idx == -1 {
		return nil, fmt.Errorf("player: no inventory item with ID %q", itemID)
	}
	newItem := self.Inventory[idx]
	self.Inventory = append(self.Inventory[:idx], self.Inventory[idx+1:]...)
	if self.Wielded != nil {
		self.Inventory = append(self.Inventory, self.Wielded.Item)
	}
	self.Wielded = &wieldedWeapon{Item: newItem, Stats: stats}
	return json.Marshal(self)
}

// recomputeSneaking sets self.Sneaking true iff at least one currently
// Worn item's Stats.Sneaking is true — mirrors how Invisible is
// recomputed from ActiveEffects ("true if anything currently grants it,"
// not baked in at the moment one specific item was equipped), so wearing
// or dropping any sneaking-granting item anywhere in Worn keeps the bit
// correct without every call site needing to know the rule itself.
func recomputeSneaking(worn map[item.Slot]*wornItem) bool {
	for _, w := range worn {
		if w.Stats.Sneaking {
			return true
		}
	}
	return false
}

// Wear equips the Inventory entry whose generic ID matches itemID into
// the body slot given by its own item.ArmorStats.Slot (statsJSON is the
// JSON-encoded item.ArmorStats already computed from that same item by
// armor.Stats — hack computes it and passes it through, the same
// coordinator-mediates pattern Wield already follows for
// item.WeaponStats). If that slot is already occupied, the previous
// item is swapped back into Inventory first, exactly like wielding a
// second weapon. Errors if no Inventory entry has itemID. Returns the
// Player's updated own state as JSON directly (no envelope — same as
// PickUp/TakeItem/Wield).
func Wear(selfJSON []byte, itemID string, statsJSON []byte) ([]byte, error) {
	var self person
	if err := json.Unmarshal(selfJSON, &self); err != nil {
		return nil, fmt.Errorf("player: parse self: %w", err)
	}
	var stats item.ArmorStats
	if err := json.Unmarshal(statsJSON, &stats); err != nil {
		return nil, fmt.Errorf("player: parse armor stats: %w", err)
	}
	idx, err := indexInInventory(self.Inventory, itemID)
	if err != nil {
		return nil, err
	}
	if idx == -1 {
		return nil, fmt.Errorf("player: no inventory item with ID %q", itemID)
	}
	newItem := self.Inventory[idx]
	self.Inventory = append(self.Inventory[:idx], self.Inventory[idx+1:]...)
	if self.Worn == nil {
		self.Worn = map[item.Slot]*wornItem{}
	}
	if previous, ok := self.Worn[stats.Slot]; ok {
		self.Inventory = append(self.Inventory, previous.Item)
	}
	self.Worn[stats.Slot] = &wornItem{Item: newItem, Stats: stats}
	self.Sneaking = recomputeSneaking(self.Worn)
	return json.Marshal(self)
}

// WornItems decodes every currently-worn item's own raw JSON out of the
// Player found in selfJSON, sorted by Slot for stable ordering (Go map
// iteration order is randomized, and with more than one slot worn at
// once a caller listing them — the "i" view, the drop picker — needs a
// consistent order across calls). Mirrors Inventory's "hand back raw
// JSON, let the coordinator decode the generic subset itself" pattern.
func WornItems(selfJSON []byte) ([]json.RawMessage, error) {
	var self person
	if err := json.Unmarshal(selfJSON, &self); err != nil {
		return nil, fmt.Errorf("player: parse self: %w", err)
	}
	slots := make([]string, 0, len(self.Worn))
	for slot := range self.Worn {
		slots = append(slots, string(slot))
	}
	sort.Strings(slots)
	out := make([]json.RawMessage, 0, len(slots))
	for _, slot := range slots {
		out = append(out, self.Worn[item.Slot(slot)].Item)
	}
	return out, nil
}

// ErrTorchAlreadyLit is returned by LightTorch when the Player already
// has a torch burning. Only one may be lit at a time; lighting a second
// one is refused rather than silently swapping/extinguishing the first,
// mirroring how ErrInventoryFull is a normal, expected outcome hack
// checks for specifically (via errors.Is) rather than an internal error.
var ErrTorchAlreadyLit = errors.New("player: a torch is already lit")

// LightTorch equips the Inventory entry whose generic ID matches itemID
// as the Player's single lit torch, starting its countdown at turns —
// the neutral lifespan value hack already extracted via torch.Lifespan.
// Mirrors Wield's shape (find in Inventory, splice it out) but, unlike
// Wield, never swaps: LightTorch errors with ErrTorchAlreadyLit if
// LitTorch is already set, leaving self unchanged. Sets Lit, the generic
// overlay bit other code (vision.CurrentlyLit, via hack) reads without
// needing to understand torches. Errors if no Inventory entry has
// itemID. Returns the Player's updated own state as JSON directly (no
// envelope — same as PickUp/TakeItem/Wield).
func LightTorch(selfJSON []byte, itemID string, turns int) ([]byte, error) {
	var self person
	if err := json.Unmarshal(selfJSON, &self); err != nil {
		return nil, fmt.Errorf("player: parse self: %w", err)
	}
	if self.LitTorch != nil {
		return nil, ErrTorchAlreadyLit
	}
	idx, err := indexInInventory(self.Inventory, itemID)
	if err != nil {
		return nil, err
	}
	if idx == -1 {
		return nil, fmt.Errorf("player: no inventory item with ID %q", itemID)
	}
	newItem := self.Inventory[idx]
	self.Inventory = append(self.Inventory[:idx], self.Inventory[idx+1:]...)
	self.LitTorch = &litTorch{Item: newItem, TurnsRemaining: turns}
	self.Lit = true
	return json.Marshal(self)
}

// LitTorchItem decodes just the currently-lit torch's own raw JSON out of
// the Player found in selfJSON, mirroring WieldedItem's "hand back raw
// JSON, let the coordinator decode the generic subset itself" pattern.
// Returns (nil, false, nil) if no torch is lit.
func LitTorchItem(selfJSON []byte) (json.RawMessage, bool, error) {
	var self person
	if err := json.Unmarshal(selfJSON, &self); err != nil {
		return nil, false, fmt.Errorf("player: parse self: %w", err)
	}
	if self.LitTorch == nil {
		return nil, false, nil
	}
	return self.LitTorch.Item, true, nil
}

// Inventory decodes just the carried items out of the Player found in
// selfJSON, returned as their own raw, unparsed JSON — callers (the
// coordinator) inspect each item's generic item.Item subset themselves,
// exactly like hack already decodes the Player's own generic
// overlay.Object subset via playerObject, without this package needing to
// know what's inside them.
func Inventory(selfJSON []byte) ([]json.RawMessage, error) {
	var self person
	if err := json.Unmarshal(selfJSON, &self); err != nil {
		return nil, fmt.Errorf("player: parse self: %w", err)
	}
	return self.Inventory, nil
}

// Status describes the Player's own current condition — HP, Attributes,
// and any active modifiers — as an ordered list of label/value pairs
// for a UI to render generically, without needing to understand what
// HP, Attributes, or Oiliness/Burning/Invisible/Sneaking mean. Never
// mutates selfJSON.
func Status(selfJSON []byte) ([]byte, error) {
	var self person
	if err := json.Unmarshal(selfJSON, &self); err != nil {
		return nil, fmt.Errorf("player: parse self: %w", err)
	}
	fields := []status.Field{
		{Label: "Level", Value: fmt.Sprintf("%d", self.Level)},
		{Label: "HP", Value: fmt.Sprintf("%d/%d", self.HP, self.MaxHP)},
		{Label: "Strength", Value: fmt.Sprintf("%d", self.Attributes.Strength)},
		{Label: "Intelligence", Value: fmt.Sprintf("%d", self.Attributes.Intelligence)},
		{Label: "Dexterity", Value: fmt.Sprintf("%d", self.Attributes.Dexterity)},
		{Label: "Stamina", Value: fmt.Sprintf("%d", self.Attributes.Stamina)},
	}
	if self.Oiliness > 0 {
		fields = append(fields, status.Field{Label: "Oily", Value: fmt.Sprintf("x%d", self.Oiliness)})
	}
	if self.Burning {
		fields = append(fields, status.Field{Label: "Burning", Value: "yes"})
	}
	if self.Invisible {
		fields = append(fields, status.Field{Label: "Invisible", Value: "yes"})
	}
	if self.Sneaking {
		fields = append(fields, status.Field{Label: "Sneaking", Value: "yes"})
	}
	return json.Marshal(fields)
}

// WieldedItem decodes just the currently-wielded item's own raw JSON out
// of the Player found in selfJSON, mirroring Inventory's "hand back raw
// JSON, let the coordinator decode the generic subset itself" pattern.
// Returns (nil, false, nil) if nothing is wielded.
func WieldedItem(selfJSON []byte) (json.RawMessage, bool, error) {
	var self person
	if err := json.Unmarshal(selfJSON, &self); err != nil {
		return nil, false, fmt.Errorf("player: parse self: %w", err)
	}
	if self.Wielded == nil {
		return nil, false, nil
	}
	return self.Wielded.Item, true, nil
}

// dropResponse is what Drop returns: the Player's own resulting state,
// the raw item that was dropped (for the coordinator to place on the
// floor), and whether it was the currently-wielded item, a currently-worn
// item, or the currently-lit torch.
type dropResponse struct {
	Self       person          `json:"self"`
	Item       json.RawMessage `json:"item"`
	WasWielded bool            `json:"was_wielded"`
	// WasWorn reports whether the dropped item was removed from Worn.
	// Unlike WasLitTorch, Item is populated as normal — worn equipment
	// drops like any ordinary item, the same way an unwielded weapon
	// does, it just doesn't stay equipped.
	WasWorn bool `json:"was_worn"`
	// WasLitTorch reports whether the dropped item was the Player's
	// LitTorch. Item is deliberately left empty in that case — a burning
	// torch simply disappears when dropped, it never lands on the floor,
	// unlike every other droppable item (including an unlit torch still
	// sitting in Inventory, which drops normally).
	WasLitTorch bool `json:"was_lit_torch"`
}

// Drop removes the item with the given ID from the Player — checking the
// currently-Wielded item first (clearing Wielded, so combat reverts to
// bare hands, if it matches), then every currently-Worn item (clearing
// that slot and recomputing Sneaking, if it matches), then the
// currently-lit torch (clearing LitTorch and Lit, reporting WasLitTorch
// with no Item — it disappears rather than landing on the floor), then
// Inventory — and returns it via {"self": ..., "item": ...,
// "was_wielded": ..., "was_worn": ..., "was_lit_torch": ...} for the
// coordinator to place on the floor (or not, for a lit torch). Errors if
// itemID matches none of these.
func Drop(selfJSON []byte, itemID string) ([]byte, error) {
	var self person
	if err := json.Unmarshal(selfJSON, &self); err != nil {
		return nil, fmt.Errorf("player: parse self: %w", err)
	}

	if self.Wielded != nil {
		var obj overlay.Object
		if err := json.Unmarshal(self.Wielded.Item, &obj); err != nil {
			return nil, fmt.Errorf("player: parse wielded item: %w", err)
		}
		if obj.ID == itemID {
			dropped := self.Wielded.Item
			self.Wielded = nil
			return json.Marshal(dropResponse{Self: self, Item: dropped, WasWielded: true})
		}
	}

	for slot, w := range self.Worn {
		var obj overlay.Object
		if err := json.Unmarshal(w.Item, &obj); err != nil {
			return nil, fmt.Errorf("player: parse worn item: %w", err)
		}
		if obj.ID == itemID {
			dropped := w.Item
			delete(self.Worn, slot)
			self.Sneaking = recomputeSneaking(self.Worn)
			return json.Marshal(dropResponse{Self: self, Item: dropped, WasWorn: true})
		}
	}

	if self.LitTorch != nil {
		var obj overlay.Object
		if err := json.Unmarshal(self.LitTorch.Item, &obj); err != nil {
			return nil, fmt.Errorf("player: parse lit torch item: %w", err)
		}
		if obj.ID == itemID {
			self.LitTorch = nil
			self.Lit = false
			return json.Marshal(dropResponse{Self: self, WasLitTorch: true})
		}
	}

	idx, err := indexInInventory(self.Inventory, itemID)
	if err != nil {
		return nil, err
	}
	if idx == -1 {
		return nil, fmt.Errorf("player: no carried item with ID %q", itemID)
	}
	dropped := self.Inventory[idx]
	self.Inventory = append(self.Inventory[:idx], self.Inventory[idx+1:]...)
	return json.Marshal(dropResponse{Self: self, Item: dropped, WasWielded: false})
}

// isOily reports whether (x,y) is one of tiles.
func isOily(tiles []mapdata.Point, x, y int) bool {
	for _, p := range tiles {
		if p.X == x && p.Y == y {
			return true
		}
	}
	return false
}

// occupantAt returns whatever object other than the one with ID selfID
// occupies (x,y), if any.
func occupantAt(objects []overlay.Object, selfID string, x, y int) (overlay.Object, bool) {
	for _, o := range objects {
		if o.ID == selfID {
			continue
		}
		if o.X == x && o.Y == y {
			return o, true
		}
	}
	return overlay.Object{}, false
}
