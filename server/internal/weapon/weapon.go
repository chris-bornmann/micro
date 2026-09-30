// Package weapon will eventually be its own standalone Weapon
// microservice. Like potion, a weapon is not an intelligent agent — no
// Action, no Defend — but unlike potion (one-shot, consumed on Use), a
// weapon is persistent: it's wielded, not used up, and its effect on
// combat comes from being read back on every attack via item.WeaponStats
// rather than a one-time item.Effect. All specific weapon kinds (dagger,
// sword, ...) are Kind variants of this package's one private type, per
// the same "new category = new package, new flavor = just data" rule
// potion already follows.
package weapon

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

// Kind names a specific weapon type. Only KindDagger and KindSword are
// implemented; future kinds (KindMace, ...) are added here as data
// variants of weaponItem, never as new packages.
type Kind string

const (
	KindDagger Kind = "dagger"
	KindSword  Kind = "sword"
)

// Base damage maxima by Kind — every weapon's range starts at 1
// (weaponItem carries no BaseDamageMin field for the same reason
// item.WeaponStats doesn't); only the top of the range is Kind-specific.
const (
	daggerBaseDamageMax = 4
	swordBaseDamageMax  = 6
)

// Weight by Kind, relative to the Player's baseline of 100; see
// overlay.Object.Weight. weaponSword also stands in for FromStats'
// reconstructed weapon below, since a rusty sword dropped by a dead
// goblin is the only real call site today.
const (
	daggerWeight = 3
	swordWeight  = 6
)

// weaponItem is this package's private representation of a weapon. It
// embeds item.Item exactly as potionItem embeds it. Enchantment is a flat
// modifier applied to both ends of the base [1,BaseDamageMax] range
// before it's ever sent in an action.Result — positive for a blessed
// weapon, negative for a cursed one, 0 for a plain weapon. There's no
// separate "cursed" bool: the sign of Enchantment alone is sufficient,
// following Dead/Invisible's "one bit is enough" minimalism. VsKind holds
// this weapon's bonus/penalty against specific overlay.Object.Kind
// values (e.g. {"goblin": 1} for a dagger especially deadly to goblins),
// keyed generically by Kind string so this package never needs to import
// goblin or any other monster-specific package to reference it.
type weaponItem struct {
	item.Item
	Kind          Kind
	BaseDamageMax int
	Enchantment   int            `json:",omitempty"`
	VsKind        map[string]int `json:",omitempty"`
}

// Init creates a new fine dagger on the given generated level (a
// JSON-encoded mapdata.Map) and returns it as JSON. Seeded at m.Seed+4 —
// the next free offset after player.Init (m.Seed), goblin.Init
// (m.Seed+1), the healing potion (m.Seed+2), and the invisibility potion
// (m.Seed+3). This dagger carries a +1 enchantment and a VsKind bonus
// against goblins, demonstrating both the enchantment and monster-matchup
// mechanisms with real values.
func Init(mapJSON []byte) ([]byte, error) {
	var m mapdata.Map
	if err := json.Unmarshal(mapJSON, &m); err != nil {
		return nil, fmt.Errorf("weapon: parse map: %w", err)
	}
	if len(m.Rooms) == 0 {
		return nil, fmt.Errorf("weapon: map has no rooms to spawn in")
	}
	rng := rand.New(rand.NewSource(m.Seed + 4))
	r := m.Rooms[rng.Intn(len(m.Rooms))]
	x := r.X + rng.Intn(r.W)
	y := r.Y + rng.Intn(r.H)

	w := weaponItem{
		Item: item.Item{
			Object: overlay.Object{
				ID:     fmt.Sprintf("weapon:%d", atomic.AddInt64(&nextID, 1)),
				X:      x,
				Y:      y,
				Glyph:  ')',
				Color:  overlay.Color{R: 180, G: 180, B: 210},
				Layer:  0, // floor items draw below goblin (1) and player (2)
				Weight: daggerWeight,
			},
			Category: item.CategoryWeapon,
			Name:     "fine dagger",
			Usage:    item.UsagePersistent,
		},
		Kind:          KindDagger,
		BaseDamageMax: daggerBaseDamageMax,
		Enchantment:   1,
		VsKind:        map[string]int{"goblin": 1},
	}
	return json.Marshal(w)
}

// Stats parses selfJSON — exactly what this weapon's Init last returned,
// or what's sitting in a Player's Inventory unmodified since (weapons
// never change after spawning) — and extracts the neutral
// item.WeaponStats snapshot the coordinator hands to player.Wield/
// goblin.InitArmed. This is the seam that keeps player/goblin from ever
// needing to import weapon or understand weaponItem's private shape.
func Stats(selfJSON []byte) (item.WeaponStats, error) {
	var w weaponItem
	if err := json.Unmarshal(selfJSON, &w); err != nil {
		return item.WeaponStats{}, fmt.Errorf("weapon: parse self: %w", err)
	}
	return statsOf(w), nil
}

// GoblinStarter returns the item.WeaponStats snapshot for a goblin's
// pre-equipped starting weapon: a cursed (-1 enchantment) rusty sword.
// Unlike Init's dagger, this weapon never sits on the floor as a
// pickupable item — hack passes its stats straight to goblin.InitArmed
// as spawn configuration. Deterministic and map-independent by design: a
// goblin's starting weapon isn't something a player can ever find or
// lose.
func GoblinStarter() item.WeaponStats {
	w := weaponItem{
		Item: item.Item{
			Object:   overlay.Object{ID: "weapon:goblin-starter", Glyph: ')', Weight: swordWeight},
			Category: item.CategoryWeapon,
			Name:     "rusty sword",
			Usage:    item.UsagePersistent,
		},
		Kind:          KindSword,
		BaseDamageMax: swordBaseDamageMax,
		Enchantment:   -1,
	}
	return statsOf(w)
}

// FromStats builds a standalone, freshly-IDed weaponItem floor item from
// an item.WeaponStats snapshot — used when a wielded weapon needs to
// become a real pickupable item independent of its original holder (a
// goblin's death, for instance: see goblin.Corpse). weaponItem's Kind
// data variant (dagger vs sword) isn't part of item.WeaponStats and can't
// be recovered here — left as the zero value, which is safe since
// nothing outside this package ever branches on weaponItem.Kind. Weight
// is likewise not part of item.WeaponStats; since the only real call
// site today reconstructs a dead goblin's dropped rusty sword, Weight is
// set to swordWeight rather than left at zero.
// Position is left at the zero value; callers reposition via the same
// relocateItem mechanism hack already uses for dropped Inventory items.
func FromStats(stats item.WeaponStats) ([]byte, error) {
	w := weaponItem{
		Item: item.Item{
			Object: overlay.Object{
				ID:     fmt.Sprintf("weapon:%d", atomic.AddInt64(&nextID, 1)),
				Glyph:  ')',
				Color:  overlay.Color{R: 180, G: 180, B: 210},
				Layer:  0,
				Weight: swordWeight,
			},
			Category: item.CategoryWeapon,
			Name:     stats.Name,
			Usage:    item.UsagePersistent,
		},
		BaseDamageMax: stats.BaseDamageMax,
		Enchantment:   stats.Enchantment,
		VsKind:        stats.VsKind,
	}
	return json.Marshal(w)
}

func statsOf(w weaponItem) item.WeaponStats {
	return item.WeaponStats{
		Name:          w.Name,
		Source:        w.ID,
		BaseDamageMax: w.BaseDamageMax,
		Enchantment:   w.Enchantment,
		VsKind:        w.VsKind,
	}
}
