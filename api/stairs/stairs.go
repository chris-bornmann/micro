// Package stairs will eventually be its own standalone Stairs
// microservice, following the same "new category = new package, new
// flavor = just data" rule potion/weapon/torch/scroll/armor/flask
// already do. A stairs item is immovable (see overlay.Object.Fixed) —
// the coordinator must never let one be picked up, dropped, or thrown.
// For now a stairs-up and stairs-down item are only spawned; activating
// one (the coordinator's "<"/">" keys) is a placeholder that does
// nothing yet — real level generation/storage is a future pass.
package stairs

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

// Kind names which direction a staircase leads. Both are implemented —
// unlike most other item categories, a level (once multi-level play
// exists) needs both, except the very last level, which has no down
// stairs.
type Kind string

const (
	KindUp   Kind = "up"
	KindDown Kind = "down"
)

// stairsItem is this package's private representation of a staircase. It
// embeds item.Item exactly as flaskItem/armorItem do.
type stairsItem struct {
	item.Item
	Kind Kind
}

// InitUp creates a new stairs-up item on the given generated level (a
// JSON-encoded mapdata.Map) and returns it as JSON. Seeded at
// m.Seed+10 — the next free offset after mummy.Init (m.Seed+9).
func InitUp(mapJSON []byte) ([]byte, error) {
	return spawn(mapJSON, 10, KindUp, '<')
}

// InitDown creates a new stairs-down item on the given generated level,
// the same way InitUp does. Seeded at m.Seed+11 — the next free offset
// after InitUp (m.Seed+10).
func InitDown(mapJSON []byte) ([]byte, error) {
	return spawn(mapJSON, 11, KindDown, '>')
}

// spawn is InitUp/InitDown's shared body: parses the map and rolls a
// spawn position from a seeded rng, offset from m.Seed by seedOffset so
// the two kinds (and every other item type) never draw the same
// sequence.
func spawn(mapJSON []byte, seedOffset int64, kind Kind, glyph rune) ([]byte, error) {
	var m mapdata.Map
	if err := json.Unmarshal(mapJSON, &m); err != nil {
		return nil, fmt.Errorf("stairs: parse map: %w", err)
	}
	if len(m.Rooms) == 0 {
		return nil, fmt.Errorf("stairs: map has no rooms to spawn in")
	}

	rng := rand.New(rand.NewSource(m.Seed + seedOffset))
	r := m.Rooms[rng.Intn(len(m.Rooms))]
	x := r.X + rng.Intn(r.W)
	y := r.Y + rng.Intn(r.H)

	name := "stairs up"
	if kind == KindDown {
		name = "stairs down"
	}

	s := stairsItem{
		Item: item.Item{
			Object: overlay.Object{
				ID:    fmt.Sprintf("stairs:%d", atomic.AddInt64(&nextID, 1)),
				X:     x,
				Y:     y,
				Glyph: glyph,
				Color: overlay.Color{R: 200, G: 200, B: 200}, // pale stone
				Layer: 0,                                     // floor items draw below goblin (1) and player (2)
				// Weight is meaningless for something that can never be
				// carried, but every object sets it; see
				// overlay.Object.Weight.
				Weight: 1,
				// Fixed marks this immovable: never a pickup candidate,
				// never thrown, never relocated. See overlay.Object.Fixed
				// and hack.pickupCandidatesAt.
				Fixed: true,
			},
			Category: item.CategoryStairs,
			Name:     name,
			Usage:    item.UsagePersistent,
		},
		Kind: kind,
	}
	return json.Marshal(s)
}

// KindOf decodes just the Kind of the stairs item in raw — up or down —
// so the coordinator can tell which one it's looking at without needing
// this package's own private stairsItem type, mirroring
// player.WieldedItem's "small accessor, not the whole private struct"
// pattern.
func KindOf(raw []byte) (Kind, error) {
	var s struct {
		Kind Kind
	}
	if err := json.Unmarshal(raw, &s); err != nil {
		return "", fmt.Errorf("stairs: parse self: %w", err)
	}
	return s.Kind, nil
}
