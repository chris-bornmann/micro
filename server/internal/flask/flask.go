// Package flask will eventually be its own standalone Flask
// microservice, following the same "new category = new package, new
// flavor = just data" rule potion/weapon/torch/scroll/armor already do.
// For now a flask of oil is only spawned — nothing consumes it yet; a
// future Use (or a throw-specific effect) will read its Kind the same
// way potion.Use already does for its own flavors.
package flask

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

// Kind names a specific flask flavor. Only KindOil is implemented;
// future flavors (acid, ...) are added here as data variants of
// flaskItem, never as new packages.
type Kind string

const (
	KindOil Kind = "oil"
)

// flaskItem is this package's private representation of a flask. It
// embeds item.Item exactly as potionItem/scrollItem do.
type flaskItem struct {
	item.Item
	Kind Kind
}

// Init creates a new flask of oil on the given generated level (a
// JSON-encoded mapdata.Map) and returns it as JSON. Seeded at m.Seed+8 —
// the next free offset after armor.InitShoesOfSneaking (m.Seed+7).
//
// Usage is deliberately UsagePersistent rather than UsageOneShot: the
// latter would make it show up in the coordinator's "use" (quaff)
// picker (see hack.quaffCandidates), which would error out today since
// there's no Use handler for item.CategoryFlask yet. Persistent keeps it
// out of that picker without implying it's equippable — nothing filters
// on Category==CategoryFlask anywhere, so it just sits in Inventory
// (and can already be dropped or thrown generically) until real
// functionality is built.
func Init(mapJSON []byte) ([]byte, error) {
	var m mapdata.Map
	if err := json.Unmarshal(mapJSON, &m); err != nil {
		return nil, fmt.Errorf("flask: parse map: %w", err)
	}
	if len(m.Rooms) == 0 {
		return nil, fmt.Errorf("flask: map has no rooms to spawn in")
	}

	// The RNG seed increment should be passed in from the central
	// coordinator.  That way the coordinator can control the RNG sequence for
	// all spawned items, and the coordinator can also ensure that the RNG
	// sequence is different for each spawned item type.  For now, we just
	// use m.Seed + 8 to avoid collisions with other item types.
	rng := rand.New(rand.NewSource(m.Seed + 8))

	r := m.Rooms[rng.Intn(len(m.Rooms))]
	x := r.X + rng.Intn(r.W)
	y := r.Y + rng.Intn(r.H)

	f := flaskItem{
		Item: item.Item{
			Object: overlay.Object{
				ID:      fmt.Sprintf("flask:%d", atomic.AddInt64(&nextID, 1)),
				X:       x,
				Y:       y,
				Glyph:   '!',
				Color:   overlay.Color{R: 40, G: 25, B: 5},
				Layer:   0, // floor items draw below goblin (1) and player (2)
				Weight:  2,
				Fragile: true,
			},
			Category: item.CategoryFlask,
			Name:     "flask of oil",
			Usage:    item.UsagePersistent,
		},
		Kind: KindOil,
	}
	return json.Marshal(f)
}
