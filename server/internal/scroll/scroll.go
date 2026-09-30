// Package scroll will eventually be its own standalone Scroll
// microservice. Like potion, a scroll is not an intelligent agent — no
// Action, no Defend — and Use converts a scroll's own private data into
// an item.Effect the coordinator hands to whichever entity read it. All
// specific scroll flavors are Kind variants of this package's one
// private type, per the same convention potion already follows: a new
// item *category* gets its own package, a new *flavor* within an
// existing category doesn't.
//
// Unlike potion, a scroll of teleportation can't fully determine its own
// effect: "a random unoccupied tile" needs the current map and where
// every entity currently is, neither of which this package has (or
// should have — it's meant to stay a neutral service with no map or
// entity-position state of its own). So Use reports only which kind of
// effect this scroll produces, with item.Effect.X/Y left unset; the
// coordinator fills those in itself right after Use returns (see hack's
// resolveEffectTarget) — the same "coordinator fills in what only it can
// know" seam weapon.Stats/player.Wield already established.
package scroll

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

// Kind names a specific scroll flavor. Only KindTeleport is implemented;
// future flavors are added here as data variants of scrollItem, never as
// new packages.
type Kind string

const (
	KindTeleport Kind = "teleport"
)

// scrollItem is this package's private representation of a scroll. It
// embeds item.Item exactly as potionItem/weaponItem do.
type scrollItem struct {
	item.Item
	Kind Kind
}

// useResponse is what Use returns — mirrors potion's useResponse
// exactly: no "self", since a one-shot scroll's lifecycle ends the
// moment it's read.
type useResponse struct {
	Effect item.Effect `json:"effect"`
}

// InitTeleport creates a new scroll of teleportation on the given
// generated level (a JSON-encoded mapdata.Map) and returns it as JSON.
// Seeded at m.Seed+6 — the next free offset after player.Init (m.Seed),
// goblin.Init (m.Seed+1), the healing potion (m.Seed+2), the
// invisibility potion (m.Seed+3), weapon.Init (m.Seed+4), and torch.Init
// (m.Seed+5).
func InitTeleport(mapJSON []byte) ([]byte, error) {
	var m mapdata.Map
	if err := json.Unmarshal(mapJSON, &m); err != nil {
		return nil, fmt.Errorf("scroll: parse map: %w", err)
	}
	if len(m.Rooms) == 0 {
		return nil, fmt.Errorf("scroll: map has no rooms to spawn in")
	}
	rng := rand.New(rand.NewSource(m.Seed + 6))
	r := m.Rooms[rng.Intn(len(m.Rooms))]
	x := r.X + rng.Intn(r.W)
	y := r.Y + rng.Intn(r.H)

	s := scrollItem{
		Item: item.Item{
			Object: overlay.Object{
				ID:     fmt.Sprintf("scroll:%d", atomic.AddInt64(&nextID, 1)),
				X:      x,
				Y:      y,
				Glyph:  '?',
				Color:  overlay.Color{R: 230, G: 220, B: 180},
				Layer:  0, // floor items draw below goblin (1) and player (2)
				Weight: 1,
			},
			Category: item.CategoryScroll,
			Name:     "scroll of teleportation",
			Usage:    item.UsageOneShot,
		},
		Kind: KindTeleport,
	}
	return json.Marshal(s)
}

// Use converts the scroll found in selfJSON into the item.Effect it
// produces when read. selfJSON is exactly what this scroll last returned
// from InitTeleport (scrolls never change after spawning). For
// KindTeleport this reports item.EffectTeleport with X/Y left at their
// zero value — see the package doc for why. Returns {"effect": ...} as
// JSON.
func Use(selfJSON []byte) ([]byte, error) {
	var self scrollItem
	if err := json.Unmarshal(selfJSON, &self); err != nil {
		return nil, fmt.Errorf("scroll: parse self: %w", err)
	}

	var effect item.Effect
	switch self.Kind {
	case KindTeleport:
		effect = item.Effect{Kind: item.EffectTeleport, Source: self.ID}
	default:
		return nil, fmt.Errorf("scroll: unsupported kind %q", self.Kind)
	}

	return json.Marshal(useResponse{Effect: effect})
}
