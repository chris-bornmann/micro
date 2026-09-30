// Package potion will eventually be its own standalone Potion
// microservice. It follows player/goblin's shape — Init spawns on the
// floor of a generated level via a seeded rng — but a potion is not an
// intelligent agent: no Action, no Defend. Instead it has Use, which
// converts a potion's own private data into an item.Effect the
// coordinator hands to whichever entity drank it. All specific potion
// flavors ("healing", "invisibility", ...) are Kind variants of this
// package's one private type, per the item-subsystem convention that a
// new item *category* gets its own package but a new *flavor* within an
// existing category doesn't.
package potion

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"sync/atomic"

	"map_api/item"
	"map_api/mapdata"
	"map_api/overlay"
)

var nextID int64

// Kind names a specific potion flavor. Only KindHealing is implemented;
// future flavors (KindInvisibility, ...) are added here as data variants
// of potionItem, never as new packages.
type Kind string

const (
	KindHealing      Kind = "healing"
	KindInvisibility Kind = "invisibility"
)

// potionItem is this package's private representation of a potion. It
// embeds item.Item so the JSON it marshals to is field-for-field the
// generic item contract (Category, Name, Usage, plus the overlay.Object
// subset) with HealAmount/Duration layered on top — exactly how
// player.person embeds overlay.Object with HP layered on top.
type potionItem struct {
	item.Item
	Kind       Kind
	HealAmount int // HP restored on Use; only meaningful for KindHealing
	Duration   int // turns of invisibility granted on Use; only meaningful for KindInvisibility
}

// useResponse is what Use returns. Unlike player/goblin's Action/Defend,
// there is no "self" here — a one-shot potion's lifecycle ends the moment
// it's used, so there's no continuing state for the coordinator to store
// back. (A future persistent item's Equip response, by contrast, likely
// would need to return an updated self — see the ring/weapon/armor design
// notes.)
type useResponse struct {
	Effect item.Effect `json:"effect"`
}

// newSpawnRNG parses mapJSON, validates it has rooms, and rolls a spawn
// position from a rng seeded at m.Seed+seedOffset. It returns the rng
// itself (positioned right after the position roll) so the caller can
// immediately draw its own kind-specific stat from the same deterministic
// stream — exactly how Init's HealAmount and InitInvisibility's Duration
// are each drawn. Distinct seedOffsets keep every spawn roll independent
// of (and non-colliding with) player.Init (m.Seed), goblin.Init
// (m.Seed+1), and each other.
func newSpawnRNG(mapJSON []byte, seedOffset int64) (rng *rand.Rand, x, y int, err error) {
	var m mapdata.Map
	if err := json.Unmarshal(mapJSON, &m); err != nil {
		return nil, 0, 0, fmt.Errorf("potion: parse map: %w", err)
	}
	if len(m.Rooms) == 0 {
		return nil, 0, 0, fmt.Errorf("potion: map has no rooms to spawn in")
	}
	rng = rand.New(rand.NewSource(m.Seed + seedOffset))
	r := m.Rooms[rng.Intn(len(m.Rooms))]
	x = r.X + rng.Intn(r.W)
	y = r.Y + rng.Intn(r.H)
	return rng, x, y, nil
}

// Init creates a new healing potion on the given generated level (a
// JSON-encoded mapdata.Map) and returns it as JSON. Seeded at m.Seed+2.
func Init(mapJSON []byte) ([]byte, error) {
	rng, x, y, err := newSpawnRNG(mapJSON, 2)
	if err != nil {
		return nil, err
	}
	// Drawn immediately after spawn position, from the same seeded rng,
	// following player/goblin's pattern for full determinism from one
	// --seed flag. Only this package knows a healing potion's heal range
	// (8-15).
	heal := 8 + rng.Intn(8)

	p := potionItem{
		Item: item.Item{
			Object: overlay.Object{
				ID:      fmt.Sprintf("potion:%d", atomic.AddInt64(&nextID, 1)),
				X:       x,
				Y:       y,
				Glyph:   '!',
				Color:   overlay.Color{R: 220, G: 20, B: 60},
				Layer:   0, // floor items draw below goblin (1) and player (2)
				Weight:  1,
				Fragile: true,
			},
			Category: item.CategoryPotion,
			Name:     "healing potion",
			Usage:    item.UsageOneShot,
		},
		Kind:       KindHealing,
		HealAmount: heal,
	}

	return json.Marshal(p)
}

// InitInvisibility creates a new invisibility potion on the given
// generated level, parallel to Init. Seeded at m.Seed+3 — the next free
// offset after player.Init (m.Seed), goblin.Init (m.Seed+1), and Init's
// healing potion (m.Seed+2).
func InitInvisibility(mapJSON []byte) ([]byte, error) {
	rng, x, y, err := newSpawnRNG(mapJSON, 3)
	if err != nil {
		return nil, err
	}
	// Drawn immediately after spawn position, from the same seeded rng,
	// following Init's pattern. Only this package knows an invisibility
	// potion's duration range (10-30 turns).
	duration := 10 + rng.Intn(21)

	p := potionItem{
		Item: item.Item{
			Object: overlay.Object{
				ID:      fmt.Sprintf("potion:%d", atomic.AddInt64(&nextID, 1)),
				X:       x,
				Y:       y,
				Glyph:   '!',
				Color:   overlay.Color{R: 100, G: 20, B: 220}, // distinct from healing's red
				Layer:   0,
				Weight:  1,
				Fragile: true,
			},
			Category: item.CategoryPotion,
			Name:     "invisibility potion",
			Usage:    item.UsageOneShot,
		},
		Kind:     KindInvisibility,
		Duration: duration,
	}

	return json.Marshal(p)
}

// Use converts the potion found in selfJSON into the item.Effect it
// produces when drunk. selfJSON is exactly what this potion last
// returned from Init (potions never change after spawning, so there is
// no intermediate Action-equivalent call). Use needs nothing about the
// drinker: a healing potion restores a fixed amount regardless of who
// drinks it — any drinker-specific reaction (clamping to a max HP,
// resistances, ...) is the drinking entity's own business, not the
// potion's; see player.ApplyEffect. Returns {"effect": ...} as JSON.
func Use(selfJSON []byte) ([]byte, error) {
	var self potionItem
	if err := json.Unmarshal(selfJSON, &self); err != nil {
		return nil, fmt.Errorf("potion: parse self: %w", err)
	}

	var effect item.Effect
	switch self.Kind {
	case KindHealing:
		effect = item.Effect{Kind: item.EffectHeal, Amount: self.HealAmount, Source: self.ID}
	case KindInvisibility:
		effect = item.Effect{Kind: item.EffectInvisibility, Duration: self.Duration, Source: self.ID}
	default:
		return nil, fmt.Errorf("potion: unsupported kind %q", self.Kind)
	}

	return json.Marshal(useResponse{Effect: effect})
}
