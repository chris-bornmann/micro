// Package overlay defines the generic contract game-object services (a
// player, a monster, a treasure item, ...) use to report what they draw on
// top of a generated map. It has no dependency on mapgen or render: any
// object-owning service only needs this package to produce valid output,
// and never needs to teach the map or render services what it is.
package overlay

// Color is plain RGB — the canonical color an object-owning service picks
// once. Renderers derive whatever presentation they need (terminal
// 256-color, true-color image, ...) from it.
type Color struct{ R, G, B uint8 }

// Object is one drawable game object, reported by whichever service owns
// it. ID should be namespaced by the owning service by convention (e.g.
// "goblin:17", "player:1") so IDs from different services never collide;
// this package does not enforce or parse that format.
type Object struct {
	ID    string
	X, Y  int // tile coordinates, in the same space as the accompanying Map
	Glyph rune
	Color Color
	Layer int // higher draws on top when objects share a tile
	// Weight is how much this object weighs, relative to the Player's
	// baseline of 100 (e.g. a goblin is 50, a potion is 1). Every object
	// has a meaningful nonzero weight, so — unlike the generic bits below
	// — this isn't omitempty. Nothing consumes it yet; it exists so a
	// future carrying-capacity/throw-distance system can read it without
	// needing to understand which entity or item package produced it.
	Weight int
	// Dead reports whether the owning entity has been reduced to 0 or
	// fewer hit points. It's the one bit of an entity's private combat
	// state that's generically useful to the coordinator (deciding
	// whether to keep giving it turns or rendering it) without the
	// coordinator needing to understand hit points at all.
	Dead bool `json:"dead,omitempty"`
	// Invisible reports whether the owning entity is currently
	// imperceptible to other entities' generic awareness/targeting logic
	// (see goblin.Action). It's the one bit of an entity's private
	// effect state that's generically useful to the coordinator and to
	// other entities (deciding whether to notice or attack it) without
	// them needing to understand item.EffectInvisibility, ActiveEffects,
	// or durations at all.
	Invisible bool `json:"invisible,omitempty"`
	// Kind is a generic tag for what sort of entity this is (e.g.
	// "goblin", "player") — the one bit of an entity's private nature
	// that's generically useful to other code (specifically, a wielded
	// weapon's item.WeaponStats.VsKind lookup) without that code needing
	// to import goblin, player, or any other entity-specific package to
	// reference it. Empty for anything that doesn't participate in
	// Kind-based matchups.
	Kind string `json:"kind,omitempty"`
	// Lit reports whether the owning entity currently has an active
	// light source (e.g. a burning torch) — the one bit of private
	// light-source state that's generically useful to other code
	// (specifically, vision.CurrentlyLit) without it needing to
	// understand torches, ActiveEffects, or any other mechanism. Follows
	// Dead/Invisible/Kind's same "one generic bit" precedent.
	Lit bool `json:"lit,omitempty"`
	// Sneaking reports whether the owning entity currently benefits from
	// stealth equipment — the one bit of private equipment state that's
	// generically useful to other code (specifically, goblin.Action's
	// awareness check) without it needing to understand armor, Worn, or
	// any other mechanism. Follows Dead/Invisible/Lit/Kind's same "one
	// generic bit" precedent.
	Sneaking bool `json:"sneaking,omitempty"`
	// Fixed reports whether this object is anchored in place and can
	// never be picked up or otherwise relocated (e.g. a future
	// staircase) — the bit that lets the coordinator's room-exit
	// visibility filter treat it like part of the map's own geometry
	// (remembered forever once seen) rather than like a movable item or
	// intelligent entity (only visible while its room is currently in
	// view; see hack.Visibility.filterObjects). False for every object
	// today, matching the common case — nothing sets it yet since no
	// Fixed item category exists.
	Fixed bool `json:"fixed,omitempty"`
	// Fragile reports whether this object shatters and disappears
	// entirely when thrown, rather than landing on the floor as a
	// pickupable item afterward — the bit that lets the coordinator's
	// throw-resolution logic (hack.resolveThrow) skip placing it in
	// FloorItems, regardless of whether the throw hit an agent, a wall,
	// or simply ran out of range. Only affects being thrown — a Fragile
	// item dropped normally (see player.Drop) still lands on the floor
	// untouched. False for every object today except potions; nothing
	// else sets it yet.
	Fragile bool `json:"fragile,omitempty"`
	// Oiliness counts how many times this entity has been doused in oil
	// (e.g. by a thrown flask of oil) — "one oily" after the first hit,
	// "two oily" after a second, and so on. Incremented by whichever
	// Defend implementation applies an incoming action.Result.AddsOiliness
	// (see player.Defend, goblin.Defend). Zero for every object today,
	// matching the common case; nothing consumes the count yet.
	Oiliness int `json:"oiliness,omitempty"`
	// Burning reports whether this entity is currently on fire (see
	// action.Result.Ignites, player.Defend/goblin.Defend, and
	// player.Tick/goblin.Tick, which apply the per-turn damage and clear
	// this once BurningTurnsRemaining reaches 0). False for every object
	// today; nothing sets it yet outside the burn pipeline.
	Burning bool `json:"burning,omitempty"`
	// Level is a rough measure of how powerful an intelligent agent
	// (player, goblin, mummy, ...) is — set once at spawn for a monster
	// and never changed, but free to rise or fall over time for the
	// Player. Only intelligent agents set this; a plain item leaves it
	// at the zero value and omits it from its own JSON, the same way
	// Dead/Invisible/Sneaking do for anything that isn't alive. Nothing
	// reads Level yet — it exists so a future map-level spawn table can
	// pick which monsters are eligible to appear without needing to
	// understand any entity-specific leveling mechanism.
	Level int `json:"level,omitempty"`
}
