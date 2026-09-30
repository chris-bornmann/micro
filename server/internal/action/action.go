// Package action defines the wire shape any intelligent entity's Action
// method reports back to the coordinator: what actually happened this
// turn — a move, or an attack with weapon/damage details. It's a neutral
// shared library (no gameplay constants, no entity-specific knowledge),
// exactly like overlay, world, and mapdata — every entity package
// constructs its own Result independently.
package action

import (
	"math/rand"

	"micro/server/internal/attributes"
)

// Result describes what an Action call actually did this turn.
//
// For a move, X,Y is the actor's own new position (or its unchanged
// position, if the move was blocked or no move was attempted) and
// Weapon/DamageMin/DamageMax/Attributes are omitted.
//
// For an attack, the actor's own position is unchanged (moving "into" an
// occupied cell doesn't relocate you) and X,Y instead identify the target
// being attacked — whatever's occupying that cell — alongside the weapon
// and damage range used.
type Result struct {
	Action    string `json:"action"` // "move" or "attack"
	X         int    `json:"x"`
	Y         int    `json:"y"`
	Weapon    string `json:"weapon,omitempty"`
	DamageMin int    `json:"damage_min,omitempty"`
	DamageMax int    `json:"damage_max,omitempty"`
	// Attributes are the attacker's own core stats, carried along so the
	// defender's Defend call can react to them (e.g. a Strength-based
	// damage bonus) without the coordinator needing to separately plumb
	// attacker state through. Only meaningful for an attack; future
	// combat mechanics that depend on the attacker's stats (a Dexterity-
	// based hit chance, say) can read this same field rather than
	// widening Defend's signature again.
	Attributes attributes.Attributes `json:"attributes,omitempty"`
	// AddsOiliness reports whether this attack adds one to the target's
	// own Oiliness count on impact (see overlay.Object.Oiliness) — set
	// by player.Throw when the thrown item is a flask of oil. Applied by
	// Defend alongside the normal damage roll; the two are independent
	// consequences of the same hit.
	AddsOiliness bool `json:"adds_oiliness,omitempty"`
	// BlockedByWall reports whether a throw's flight ended because it
	// was physically blocked (aug.Map.CanMove returned false) rather
	// than because it hit an agent or simply ran out of range over open
	// ground. Only meaningful when Action is "throw" — a generic physics
	// fact any thrown item's Result carries, not flask-specific; hack
	// decides what (if anything) to do with it per item.
	BlockedByWall bool `json:"blocked_by_wall,omitempty"`
	// Ignites reports whether this attack sets the target on fire if
	// they're already oily (Oiliness > 0) — set by player.Throw when the
	// thrown item is the Player's currently-lit torch. Applied by Defend
	// alongside damage/AddsOiliness; independent of both.
	Ignites bool `json:"ignites,omitempty"`
}

// RollDamage returns a random value in [min,max] using the global RNG —
// combat damage is genuinely random each time, unlike map generation and
// spawn position, which are seeded for reproducibility. If max is less
// than min, it's clamped up to min (a zero-width range).
func RollDamage(min, max int) int {
	if max < min {
		max = min
	}
	return min + rand.Intn(max-min+1)
}

// DefendOutcome reports what happened when an entity defended against an
// incoming attack.
type DefendOutcome struct {
	Damage      int  `json:"damage"`
	RemainingHP int  `json:"remaining_hp"`
	Dead        bool `json:"dead"`
}
