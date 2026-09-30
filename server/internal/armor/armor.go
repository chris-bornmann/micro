// Package armor will eventually be its own standalone Armor
// microservice. Like weapon, an armor piece is not an intelligent agent
// — no Action, no Defend — and is persistent: worn, not used up, its
// passive properties read back via item.ArmorStats rather than a
// one-time item.Effect. All specific armor kinds (shoes of sneaking,
// and later a helm, ...) are Kind variants of this package's one
// private type, per the same "new category = new package, new flavor =
// just data" rule potion/weapon already follow.
package armor

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"sync/atomic"

	"micromap/server/internal/item"
	"micromap/server/internal/mapdata"
	"micromap/server/internal/overlay"
)

var nextID int64

// Kind names a specific armor piece. Only KindShoesOfSneaking is
// implemented; future kinds (a helm, ...) are added here as data
// variants of armorItem, never as new packages.
type Kind string

const (
	KindShoesOfSneaking Kind = "shoes_of_sneaking"
)

// armorItem is this package's private representation of a worn item. It
// embeds item.Item exactly as weaponItem/potionItem do. Slot and
// Sneaking are exactly what Stats hands back as item.ArmorStats — kept
// here rather than computed on the fly since, like a weapon's stats,
// they're fixed at spawn and never change afterward.
type armorItem struct {
	item.Item
	Kind     Kind
	Slot     item.Slot
	Sneaking bool `json:",omitempty"`
}

// InitShoesOfSneaking creates a new pair of shoes of sneaking on the
// given generated level (a JSON-encoded mapdata.Map) and returns it as
// JSON. Seeded at m.Seed+7 — the next free offset after
// scroll.InitTeleport (m.Seed+6).
func InitShoesOfSneaking(mapJSON []byte) ([]byte, error) {
	var m mapdata.Map
	if err := json.Unmarshal(mapJSON, &m); err != nil {
		return nil, fmt.Errorf("armor: parse map: %w", err)
	}
	if len(m.Rooms) == 0 {
		return nil, fmt.Errorf("armor: map has no rooms to spawn in")
	}
	rng := rand.New(rand.NewSource(m.Seed + 7))
	r := m.Rooms[rng.Intn(len(m.Rooms))]
	x := r.X + rng.Intn(r.W)
	y := r.Y + rng.Intn(r.H)

	a := armorItem{
		Item: item.Item{
			Object: overlay.Object{
				ID:     fmt.Sprintf("armor:%d", atomic.AddInt64(&nextID, 1)),
				X:      x,
				Y:      y,
				Glyph:  '[',
				Color:  overlay.Color{R: 120, G: 90, B: 60},
				Layer:  0, // floor items draw below goblin (1) and player (2)
				Weight: 3,
			},
			Category: item.CategoryArmor,
			Name:     "shoes of sneaking",
			Usage:    item.UsagePersistent,
		},
		Kind:     KindShoesOfSneaking,
		Slot:     item.SlotFeet,
		Sneaking: true,
	}
	return json.Marshal(a)
}

// Stats parses selfJSON — exactly what this armor's Init last returned,
// or what's sitting in a Player's Inventory unmodified since (armor
// never changes after spawning) — and extracts the neutral
// item.ArmorStats snapshot the coordinator hands to player.Wear. This is
// the seam that keeps player from ever needing to import armor or parse
// armorItem's private shape itself.
func Stats(selfJSON []byte) (item.ArmorStats, error) {
	var a armorItem
	if err := json.Unmarshal(selfJSON, &a); err != nil {
		return item.ArmorStats{}, fmt.Errorf("armor: parse self: %w", err)
	}
	return item.ArmorStats{Slot: a.Slot, Sneaking: a.Sneaking, Source: a.ID}, nil
}
