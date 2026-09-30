// Package mummy will eventually be its own standalone Mummy
// microservice, following the exact same shape as goblin: a private
// type embedding overlay.Object, plus Init/Action/Defend/Tick/Corpse
// functions that are all JSON-in/JSON-out. It never imports (or knows
// about) the goblin package — hack is the only thing that knows both
// exist, exactly the boundary a real separate process would need.
//
// A mummy is simpler than a goblin in two ways: it never wields a
// weapon (it always attacks by touch, causing flesh to rot) and it
// never flees (an undead pursuer has no self-preservation instinct) —
// both are deliberate omissions, not gaps to fill in later.
//
// A mummy's one distinguishing trait: wrapped in gauze, it's especially
// susceptible to fire.

package mummy

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"strings"
	"sync/atomic"

	"micro/server/internal/action"
	"micro/server/internal/attributes"
	"micro/server/internal/fire"
	"micro/server/internal/item"
	"micro/server/internal/mapdata"
	"micro/server/internal/overlay"
	"micro/server/internal/slide"
	"micro/server/internal/spawn"
	"micro/server/internal/status"
	"micro/server/internal/world"
)

var nextID int64

// NativeLevel is the dungeon level a mummy is tuned for — both the
// Level a spawned mummy reports (see Init below) and the center of
// SpawnCount's own weight curve.
const NativeLevel = 5

// Spawn-count tuning (see SpawnCount) — a mummy is rare and single-shot
// (never more than one alive at a time; see SpawnCount's own doc
// comment): earliestLevel/forceByLevel bound when it can first appear
// and guarantee it eventually does, appearChanceScale controls the odds
// in between, and sigmaUp/sigmaDown/maxAbove shape how those odds ramp
// up toward NativeLevel and taper off past it.
const (
	spawnEarliestLevel     = 3
	spawnForceByLevel      = 8
	spawnAppearChanceScale = 0.6
	spawnSigmaUp           = 8.0
	spawnSigmaDown         = 2.0
	spawnMaxAbove          = 4.0
)

// SpawnCount decides whether a mummy spawns on dungeonLevel — see
// spawn.SingleShotShouldSpawn. Always 0 or 1: a mummy is single-shot,
// not common like a goblin. alreadySpawned is the coordinator's own
// record of whether a mummy has ever appeared on any level reached so
// far this run (see hack.GameState.SpawnedOnce) — this package has no
// memory of its own between Init calls, the same statelessness every
// other exported function here already has.
func SpawnCount(dungeonLevel int, alreadySpawned bool, rng *rand.Rand) int {
	if spawn.SingleShotShouldSpawn(NativeLevel, dungeonLevel, spawnEarliestLevel, spawnForceByLevel, spawnAppearChanceScale, spawnSigmaUp, spawnSigmaDown, spawnMaxAbove, alreadySpawned, rng) {
		return 1
	}
	return 0
}

var nextCorpseID int64

// creature is this package's private representation of a mummy. It
// embeds overlay.Object so the generic fields stay field-for-field
// identical to the overlay contract render already consumes. Aware is
// this package's own private memory: whether it has ever detected the
// Player.
type creature struct {
	overlay.Object
	Aware bool
	HP    int
	// MaxHP is the mummy's hit-point ceiling, set once at Init and never
	// changed afterward — same role as goblin.monster.MaxHP.
	MaxHP int
	// Attributes are the mummy's core stats, rolled once at spawn from
	// this package's own range (4-10) and never changed afterward.
	// Nothing consumes them yet beyond Defend's Strength bonus — see the
	// attributes package doc.
	Attributes attributes.Attributes `json:"attributes"`
	// BurningTurnsRemaining counts down once per Tick while the mummy is
	// on fire (see overlay.Object.Burning, Defend's Ignites handling) —
	// mirrors goblin.monster's own field of the same name.
	BurningTurnsRemaining int `json:"burning_turns_remaining,omitempty"`
	// BurningStartsNextTurn mirrors goblin.monster's own field of the
	// same name: true for exactly one Tick after a fresh ignition, so
	// the mummy doesn't take damage the same turn it catches fire.
	BurningStartsNextTurn bool `json:"burning_starts_next_turn,omitempty"`
}

// response is what Action returns: the mummy's own resulting state plus
// a report of what actually happened this turn.
type response struct {
	Self   creature      `json:"self"`
	Result action.Result `json:"result"`
}

// defendResponse is what Defend returns: the mummy's own resulting
// state plus a report of the incoming attack's outcome.
type defendResponse struct {
	Self    creature             `json:"self"`
	Outcome action.DefendOutcome `json:"outcome"`
}

// moveResult marshals self alongside a "move" result at self's current
// position — used whenever nothing happened this turn (not yet aware,
// no legal route) as well as after an actual successful move.
func moveResult(self creature) ([]byte, error) {
	return json.Marshal(response{Self: self, Result: action.Result{Action: "move", X: self.X, Y: self.Y}})
}

// Init creates a new mummy on the given generated level (a JSON-encoded
// mapdata.Map) and returns it as JSON. spawnIndex distinguishes this
// mummy from any others spawned alongside it on the same level — see
// goblin.spawn's doc comment for why that matters and how the offset is
// kept clear of other packages' reserved m.Seed+N slots; SpawnCount
// currently never asks for more than one mummy per level, so spawnIndex
// is always 0 in practice, but Init still applies it for the same reason
// goblin's does.
func Init(mapJSON []byte, spawnIndex int) ([]byte, error) {
	var m mapdata.Map
	if err := json.Unmarshal(mapJSON, &m); err != nil {
		return nil, fmt.Errorf("mummy: parse map: %w", err)
	}
	if len(m.Rooms) == 0 {
		return nil, fmt.Errorf("mummy: map has no rooms to spawn in")
	}

	// Seed from m.Seed+9 — the next free offset after armor.
	// InitShoesOfSneaking (m.Seed+7) and flask.Init (m.Seed+8).
	rng := rand.New(rand.NewSource(m.Seed + 9 + int64(spawnIndex)*1_000_000))
	r := m.Rooms[rng.Intn(len(m.Rooms))]
	x := r.X + rng.Intn(r.W)
	y := r.Y + rng.Intn(r.H)
	// Drawn immediately after spawn position, from the same seeded rng,
	// so starting HP is reproducible alongside spawn point for a given
	// map seed. Only this package knows the mummy's HP range (20-30).
	// The mummy starts at full health, so MaxHP is set to the same
	// rolled value.
	hp := 20 + rng.Intn(11)
	// Drawn immediately after HP, from the same seeded rng, so
	// attributes are reproducible alongside spawn point and HP for a
	// given map seed. Only this package knows the mummy's attribute
	// range (4-10) — tougher than a goblin's (2-6).
	attrs := attributes.Range{Min: 4, Max: 10}.Roll(rng)

	c := creature{
		Object: overlay.Object{
			ID:    fmt.Sprintf("mummy:%d", atomic.AddInt64(&nextID, 1)),
			X:     x,
			Y:     y,
			Glyph: 'm',
			Color: overlay.Color{R: 200, G: 190, B: 150}, // dusty bandage tan
			Layer: 1,
			Kind:  "mummy",
			// Heavier than a goblin (50), lighter than the Player (100);
			// see overlay.Object.Weight.
			Weight: 60,
			// A mummy's Level never changes, fixed at NativeLevel for
			// its whole lifetime — noticeably tougher than a level 1
			// goblin; see overlay.Object.Level.
			Level: NativeLevel,
		},
		Aware:      false,
		HP:         hp,
		MaxHP:      hp,
		Attributes: attrs,
	}
	return json.Marshal(c)
}

// Action computes the mummy's move for this turn — mirrors
// goblin.Action's shape (see its own doc comment for the full
// awareness/Invisible/Sneaking rules, which apply identically here),
// minus fleeing: a mummy never flees, so low HP never changes its
// behavior. selfJSON is exactly what this mummy last returned from Init
// or Action; augmentedMapJSON is the current map plus the Player's
// position, fed by the coordinator. Returns {"self": ..., "result":
// ...} as JSON.
func Action(selfJSON, augmentedMapJSON []byte) ([]byte, error) {
	var self creature
	if err := json.Unmarshal(selfJSON, &self); err != nil {
		return nil, fmt.Errorf("mummy: parse self: %w", err)
	}

	var aug world.AugmentedMap
	if err := json.Unmarshal(augmentedMapJSON, &aug); err != nil {
		return nil, fmt.Errorf("mummy: parse augmented map: %w", err)
	}

	playerIdx := indexByPrefix(aug.Objects, "player:")
	if playerIdx == -1 {
		return nil, fmt.Errorf("mummy: no player object found in augmented map")
	}
	p := aug.Objects[playerIdx]

	if p.Invisible {
		self.Aware = false
		return moveResult(self)
	}

	if !self.Aware {
		playerRooms := aug.Map.RoomsAt(p.X, p.Y)
		if p.Sneaking {
			// While sneaking, merely standing on a doorway threshold
			// doesn't count as being "in" the room for detection
			// purposes — only genuinely standing inside one does.
			// Mirrors goblin.Action's identical rule.
			playerRooms = map[int]bool{}
			if r, ok := aug.Map.RoomAt(p.X, p.Y); ok {
				playerRooms[r.ID] = true
			}
		}
		if !sharesARoom(aug.Map.RoomsAt(self.X, self.Y), playerRooms) {
			return moveResult(self)
		}
		self.Aware = true
	}

	nx, ny, ok := aug.Map.NextStep(self.X, self.Y, p.X, p.Y)
	if !ok {
		return moveResult(self)
	}

	if target, occupied := occupantAt(aug.Objects, nx, ny); occupied {
		return json.Marshal(response{Self: self, Result: attackResult(target, self.Attributes)})
	}

	fx, fy := nx, ny
	if isOily(aug.OilyTiles, nx, ny) {
		fx, fy = slide.Resolve(aug.Map, aug.Objects, nx, ny, nx-self.X, ny-self.Y)
	}
	self.X, self.Y = fx, fy
	return moveResult(self)
}

// attackResult builds the attack action.Result against target: a mummy
// always attacks the same way — a touch that rots flesh, DamageMin 4 to
// DamageMax 10, never a wielded weapon. attackerAttributes is carried
// along unchanged (see action.Result.Attributes) so Defend can apply a
// Strength-based bonus.
func attackResult(target overlay.Object, attackerAttributes attributes.Attributes) action.Result {
	return action.Result{Action: "attack", X: target.X, Y: target.Y, Weapon: "rotting touch", DamageMin: 4, DamageMax: 10, Attributes: attackerAttributes}
}

// Defend applies an incoming attack (a JSON-encoded action.Result) to
// the mummy found in selfJSON — mirrors goblin.Defend exactly, minus
// the flee-threshold check (a mummy never flees). Returns
// {"self": ..., "outcome": ...} as JSON.
func Defend(selfJSON, attackJSON []byte) ([]byte, error) {
	var self creature
	if err := json.Unmarshal(selfJSON, &self); err != nil {
		return nil, fmt.Errorf("mummy: parse self: %w", err)
	}

	var attack action.Result
	if err := json.Unmarshal(attackJSON, &attack); err != nil {
		return nil, fmt.Errorf("mummy: parse attack: %w", err)
	}

	damage := action.RollDamage(attack.DamageMin, attack.DamageMax)
	// A flat bonus from the attacker's own Strength — see
	// goblin.Defend's identical comment for the exact rule.
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

// tickResponse is what Tick returns: the mummy's own resulting state
// plus a report of what the tick actually did — mirrors goblin's own
// tickResponse shape.
type tickResponse struct {
	Self    creature         `json:"self"`
	Outcome item.TickOutcome `json:"outcome"`
}

// Tick applies one turn of any active time-based state to the mummy
// found in selfJSON — today, just burning — mirroring goblin.Tick's
// shape and grace-turn behavior exactly, with one difference: wrapped
// in gauze, a mummy is especially susceptible to fire, so its per-turn
// burn damage is double the generic fire.TickDamage() roll. Returns
// {"self": ..., "outcome": ...} as JSON.
func Tick(selfJSON []byte) ([]byte, error) {
	var self creature
	if err := json.Unmarshal(selfJSON, &self); err != nil {
		return nil, fmt.Errorf("mummy: parse self: %w", err)
	}

	var burnDamage int
	var burnedOut bool
	if self.BurningStartsNextTurn {
		// The grace turn: caught fire this same turn, so no damage yet
		// — the fire actually starts burning next Tick.
		self.BurningStartsNextTurn = false
	} else if self.BurningTurnsRemaining > 0 {
		// Wrapped in gauze — mummies are especially susceptible to
		// fire. Scoped to this one fire mechanic for now; a future fire
		// attack would need its own susceptibility multiplier applied
		// here too, the same way this one is — the neutral fire
		// package itself never needs to know a mummy exists.
		burnDamage = fire.TickDamage() * 2
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

	outcome := item.TickOutcome{BurnDamage: burnDamage, BurnedOut: burnedOut}
	return json.Marshal(tickResponse{Self: self, Outcome: outcome})
}

// corpseItem is this package's private floor-item representation of a
// dead mummy's remains — embeds item.Item exactly as goblin.corpseItem
// does. Source records the originating mummy's ID.
type corpseItem struct {
	item.Item
	Source string
}

// CorpseResult is Corpse's JSON response — the same shape as
// goblin.CorpseResult, so hack can decode either generically. Wielded
// is never populated (a mummy never wields anything); it's kept in the
// shape purely so hack's corpse-handling code doesn't need to branch by
// monster kind.
type CorpseResult struct {
	Self    json.RawMessage   `json:"self"`
	Item    json.RawMessage   `json:"item"`
	Wielded *item.WeaponStats `json:"wielded,omitempty"`
}

// Corpse builds a floor-item representation of self (expected to
// already have Dead=true) for the coordinator to add to its floor
// items — mirrors goblin.Corpse exactly, minus stripping Wielded
// (mummy has none). Every non-inventory attribute is left exactly as
// it was.
func Corpse(selfJSON []byte) ([]byte, error) {
	var self creature
	if err := json.Unmarshal(selfJSON, &self); err != nil {
		return nil, fmt.Errorf("mummy: parse self: %w", err)
	}

	c := corpseItem{
		Item: item.Item{
			Object: overlay.Object{
				ID:    fmt.Sprintf("corpse:%d", atomic.AddInt64(&nextCorpseID, 1)),
				X:     self.X,
				Y:     self.Y,
				Glyph: '%',
				Color: overlay.Color{R: 140, G: 40, B: 40},
				Layer: 0, // floor items draw below monsters (1) and player (2)
				Kind:  self.Kind,
				// A corpse weighs what the mummy weighed, stays as oily
				// as the mummy was, and still shows if it died mid-fire.
				Weight:   self.Weight,
				Oiliness: self.Oiliness,
				Burning:  self.Burning,
			},
			Category: item.CategoryCorpse,
			Name:     self.Kind + " corpse",
			Usage:    item.UsagePersistent,
		},
		Source: self.ID,
	}
	itemRaw, err := json.Marshal(c)
	if err != nil {
		return nil, fmt.Errorf("mummy: marshal corpse item: %w", err)
	}
	selfRaw, err := json.Marshal(self)
	if err != nil {
		return nil, fmt.Errorf("mummy: marshal self: %w", err)
	}

	return json.Marshal(CorpseResult{Self: selfRaw, Item: itemRaw})
}

// Status describes this mummy's own current condition — HP, Attributes,
// and any active modifiers — as an ordered list of label/value pairs,
// following player.Status/goblin.Status' exact shape (minus Wielded — a
// mummy never wields anything). Not called by hack yet (no
// monster-inspection UI exists), but registered in hack's
// monsterServices so a future one can call it generically by Kind.
func Status(selfJSON []byte) ([]byte, error) {
	var self creature
	if err := json.Unmarshal(selfJSON, &self); err != nil {
		return nil, fmt.Errorf("mummy: parse self: %w", err)
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
	return json.Marshal(fields)
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

// occupantAt returns whatever object occupies (x,y), if any. The mummy
// itself is never in objects (it comes from selfJSON, not the
// augmented map), so there's no self-exclusion needed here.
func occupantAt(objects []overlay.Object, x, y int) (overlay.Object, bool) {
	for _, o := range objects {
		if o.X == x && o.Y == y {
			return o, true
		}
	}
	return overlay.Object{}, false
}

func indexByPrefix(objects []overlay.Object, prefix string) int {
	for i, o := range objects {
		if strings.HasPrefix(o.ID, prefix) {
			return i
		}
	}
	return -1
}

// sharesARoom reports whether a and b (each a set of room IDs from
// mapdata.Map.RoomsAt) have any room in common.
func sharesARoom(a, b map[int]bool) bool {
	for id := range a {
		if b[id] {
			return true
		}
	}
	return false
}
