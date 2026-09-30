// Package torch will eventually be its own standalone Torch
// microservice. Like potion and weapon, a torch is not an intelligent
// agent — no Action, no Defend. It's picked up unlit; the coordinator
// lights it by calling Lifespan (a read-only extraction, the same seam
// weapon.Stats already is for Wield) and handing that value to
// player.LightTorch, which owns the actual countdown and the generic
// overlay.Object.Lit bit. This package never sees a torch again once
// it's been lit.
package torch

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

// MinLifespan and MaxLifespan bound how many turns a lit torch burns —
// the actual value is rolled once per torch, at spawn (Init), not at
// ignition, so two torches on the floor can already differ.
const (
	MinLifespan = 6
	MaxLifespan = 20
)

// torchItem is this package's private representation of a torch. It
// embeds item.Item exactly as weaponItem/potionItem do. Lifespan is
// rolled once at Init and never changes afterward — it's simply read
// back by Lifespan at ignition time.
type torchItem struct {
	item.Item
	Lifespan int
}

// Init creates a new unlit torch on the given generated level (a
// JSON-encoded mapdata.Map) and returns it as JSON. Seeded at m.Seed+5 —
// the next free offset after player.Init (m.Seed), goblin.Init
// (m.Seed+1), the healing potion (m.Seed+2), the invisibility potion
// (m.Seed+3), and weapon.Init (m.Seed+4).
func Init(mapJSON []byte) ([]byte, error) {
	var m mapdata.Map
	if err := json.Unmarshal(mapJSON, &m); err != nil {
		return nil, fmt.Errorf("torch: parse map: %w", err)
	}
	if len(m.Rooms) == 0 {
		return nil, fmt.Errorf("torch: map has no rooms to spawn in")
	}
	rng := rand.New(rand.NewSource(m.Seed + 5))
	r := m.Rooms[rng.Intn(len(m.Rooms))]
	x := r.X + rng.Intn(r.W)
	y := r.Y + rng.Intn(r.H)
	lifespan := MinLifespan + rng.Intn(MaxLifespan-MinLifespan+1)

	t := torchItem{
		Item: item.Item{
			Object: overlay.Object{
				ID:     fmt.Sprintf("torch:%d", atomic.AddInt64(&nextID, 1)),
				X:      x,
				Y:      y,
				Glyph:  '/',
				Color:  overlay.Color{R: 210, G: 130, B: 40},
				Layer:  0, // floor items draw below goblin (1) and player (2)
				Weight: 2,
			},
			Category: item.CategoryTorch,
			Name:     "torch",
			Usage:    item.UsageOneShot,
		},
		Lifespan: lifespan,
	}
	return json.Marshal(t)
}

// Lifespan parses selfJSON — exactly what this torch's Init last
// returned, or what's sitting in a Player's Inventory unmodified since
// (torches never change after spawning) — and extracts the rolled
// lifespan the coordinator hands to player.LightTorch. This is the seam
// that keeps player from ever needing to import torch or understand
// torchItem's private shape.
func Lifespan(selfJSON []byte) (int, error) {
	var t torchItem
	if err := json.Unmarshal(selfJSON, &t); err != nil {
		return 0, fmt.Errorf("torch: parse self: %w", err)
	}
	return t.Lifespan, nil
}
