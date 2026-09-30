// Package item defines the neutral wire contract every item-category
// service (potion, and later food, ring, weapon, armor) shares — the
// item-world analog of overlay.Object for drawable things and
// action.Result/DefendOutcome for turn/combat outcomes. It has no
// dependency on any specific category package; each category package
// embeds Item the same way player/goblin embed overlay.Object.
package item

import "map_api/overlay"

// Category identifies which item-category service owns an item — the
// would-be microservice name. The coordinator and any other neutral code
// use this to route a pickup/use/equip action to the right package
// without needing to understand what's inside a category-specific type.
type Category string

const (
	CategoryPotion Category = "potion"
	CategoryFood   Category = "food"
	CategoryRing   Category = "ring"
	CategoryWeapon Category = "weapon"
	CategoryArmor  Category = "armor"
	CategoryCorpse Category = "corpse" // a dead monster's remains, left behind at its death tile
	CategoryTorch  Category = "torch"
	CategoryScroll Category = "scroll"
	CategoryFlask  Category = "flask"
	CategoryStairs Category = "stairs" // an immovable staircase; see overlay.Object.Fixed
)

// Usage distinguishes a one-shot item — consumed entirely the instant
// it's used (food, potions) — from a persistent one, equipped/worn, whose
// effect keeps applying for as long as it stays equipped (rings, weapons,
// armor). Nothing branches on Usage yet (only one-shot items exist); it's
// here so the coordinator's future equip path has a generic way to tell
// the two apart without special-casing categories by name.
type Usage string

const (
	UsageOneShot    Usage = "one-shot"
	UsagePersistent Usage = "persistent"
)

// InventoryCapacity is the maximum number of items any inventory-holding
// entity (today, only the Player) may carry at once. It lives here rather
// than in player because it's a generic inventory-domain fact, the same
// kind of thing Category and Usage already are — not a player-specific
// detail.
const InventoryCapacity = 10

// Item is the generic, cross-category shape every item-owning service's
// private type embeds — exactly how player.person and goblin.monster
// embed overlay.Object. It follows overlay.Object's bare-field (untagged)
// JSON convention rather than action.Result's snake_case, deliberately:
// Item embeds Object and the two are always marshaled as one flat merged
// object (a potionItem is Object+Item+its own fields all in one JSON
// blob), so consistency within that single flattened shape matters more
// than matching action's convention. Effect and EffectOutcome below, by
// contrast, are always their own separate top-level JSON value inside an
// envelope — so they follow action.Result/DefendOutcome's snake_case
// convention instead, matching that sibling precedent.
type Item struct {
	overlay.Object
	Category Category
	Name     string // display name, e.g. "healing potion"
	Usage    Usage
}

// EffectKind names the kind of change an item's effect makes to whichever
// entity it's applied to. The coordinator and every entity's ApplyEffect
// switch on this to decide what to do, without needing to know which item
// category or specific item produced it. New kinds are added here as new
// item categories are built — only EffectHeal is implemented end to end
// (in player.ApplyEffect) so far; the rest are declared now so Effect's
// shape never needs to change to accommodate them later.
type EffectKind string

const (
	EffectHeal            EffectKind = "heal"             // restores Amount HP
	EffectInvisibility    EffectKind = "invisibility"     // grants invisibility for Duration turns (0 = until removed)
	EffectDamageBonus     EffectKind = "damage_bonus"     // adds Amount to outgoing damage rolls
	EffectDamageReduction EffectKind = "damage_reduction" // subtracts Amount from incoming damage
	EffectSatiate         EffectKind = "satiate"          // resets/advances a starvation countdown by Amount
	EffectTeleport        EffectKind = "teleport"         // moves the entity to X,Y
)

// Effect describes what a used or persistently-applied item does to
// whichever entity it's applied to — the item-world analog of
// action.Result (what an Action did) and action.DefendOutcome (what a
// Defend did). Amount is EffectKind-specific: HP restored for
// EffectHeal, a damage delta for EffectDamageBonus/EffectDamageReduction,
// countdown ticks for EffectSatiate; unused (0) for EffectInvisibility.
// Duration is turns remaining for a persistent effect (0 for a one-shot
// effect that applies immediately and is done, like EffectHeal). Source
// is the producing item's own ID (e.g. "potion:3"), for status messages
// and debugging. X,Y are meaningful only for EffectTeleport — the
// destination tile — and are deliberately left unset by whichever item
// category produces the effect (e.g. scroll.Use): only the coordinator
// knows the current map and where every entity currently is, so it fills
// these in itself before the effect ever reaches player.ApplyEffect (see
// hack's resolveEffectTarget). One item produces exactly one Effect at a
// time in this design — a multi-effect item would need this to become a
// slice, not built ahead of that need.
type Effect struct {
	Kind     EffectKind `json:"kind"`
	Amount   int        `json:"amount,omitempty"`
	Duration int        `json:"duration,omitempty"`
	Source   string     `json:"source,omitempty"`
	X        int        `json:"x,omitempty"`
	Y        int        `json:"y,omitempty"`
}

// EffectOutcome reports what applying an Effect actually did to the
// entity it was applied to — the item-effect analog of
// action.DefendOutcome. Applied is the actual amount applied, which may
// be less than the Effect's own Amount if a max-HP cap clamped it (see
// player.ApplyEffect's EffectHeal case); for EffectInvisibility, Applied
// is the duration (in turns) granted, since there's no HP amount to
// report. RemainingHP is meaningful for EffectHeal only.
type EffectOutcome struct {
	Kind        EffectKind `json:"kind"`
	Applied     int        `json:"applied"`
	RemainingHP int        `json:"remaining_hp,omitempty"`
}

// ActiveEffect is an Effect still in progress — how an entity tracks a
// persistent effect (Duration > 0) between the turn it was applied and
// the turn it expires. TurnsRemaining starts at Effect.Duration and
// counts down once per Tick call; Effect.Duration itself is left
// unchanged, so it still records how long the effect originally granted.
//
// Like Effect and EffectOutcome, and unlike Item, an ActiveEffect is
// never flattened into another type's JSON — it always appears as its
// own nested value (an element of an entity's own ActiveEffects slice) —
// so it follows their snake_case convention rather than Item's
// bare-field one.
type ActiveEffect struct {
	Effect         Effect `json:"effect"`
	TurnsRemaining int    `json:"turns_remaining"`
}

// TickOutcome reports what a per-turn countdown call (player.Tick,
// goblin.Tick) actually did — the effect-countdown analog of
// EffectOutcome. Expired lists every ActiveEffect's Effect.Kind whose
// TurnsRemaining reached 0 this call, in list order; nil (omitted from
// JSON) when nothing expired, the common case most turns.
// TorchBurnedOut is the same idea for a lit torch's dedicated slot (see
// player.litTorch) — a torch isn't an ActiveEffect, so it needs its own
// "did something expire this tick" bit rather than a place in Expired.
// BurnDamage/BurnedOut are the same shape for burning (see
// overlay.Object.Burning): BurnDamage is how much damage burning dealt
// this tick (0 if not currently burning), and BurnedOut reports whether
// the fire went out this tick.
type TickOutcome struct {
	Expired        []EffectKind `json:"expired,omitempty"`
	TorchBurnedOut bool         `json:"torch_burned_out,omitempty"`
	BurnDamage     int          `json:"burn_damage,omitempty"`
	BurnedOut      bool         `json:"burned_out,omitempty"`
}

// WeaponStats is a wielded weapon's combat-relevant data, captured once
// at wield time from the weapon's own private state (the weapon package)
// into the wielder's own state — the item-world analog of ActiveEffect,
// but for a persistent capability rather than a countdown. Player and
// goblin never import the weapon package at all: wielding is a one-time
// translation hack performs (via weapon.Stats/weapon.GoblinStarter), the
// same way it already calls potion.Use directly to dispatch a use
// action, and every subsequent attack reads this stored snapshot from
// the wielder's own selfJSON — no further calls into weapon are needed.
//
// BaseDamageMin is deliberately absent: every weapon's base range starts
// at 1 regardless of Kind (only the top varies), so combat code hardcodes
// that 1 rather than carrying a field that could never legitimately hold
// anything else.
type WeaponStats struct {
	Name          string         `json:"name"`
	Source        string         `json:"source"` // the wielded weapon's own item ID, e.g. "weapon:3"
	BaseDamageMax int            `json:"base_damage_max"`
	Enchantment   int            `json:"enchantment,omitempty"`
	VsKind        map[string]int `json:"vs_kind,omitempty"`
}

// Range computes this weapon's actual usable damage range against a
// target whose overlay.Object.Kind is targetKind, folding in Enchantment
// and any VsKind bonus/penalty for that specific kind (0 if targetKind
// isn't a key). Per the "a weapon's enchantment shifts the entire base
// range" rule, the same delta is added to both ends: base [1,
// BaseDamageMax] becomes [1+delta, BaseDamageMax+delta]. The minimum is
// clamped to 0 (a sufficiently cursed weapon may deal no damage, never
// negative); if that leaves max < min, action.RollDamage's own clamp
// handles it the same as any other zero-width range.
func (s WeaponStats) Range(targetKind string) (min, max int) {
	delta := s.Enchantment + s.VsKind[targetKind]
	min, max = 1+delta, s.BaseDamageMax+delta
	if min < 0 {
		min = 0
	}
	return min, max
}

// Slot identifies which body slot a worn item occupies — the neutral key
// both an armor-category package and the Player agree on, the same role
// Category/Usage already play. New slots (a helm's "head", ...) are
// added here as new constants, never a redesign.
type Slot string

const (
	SlotFeet Slot = "feet"
)

// ArmorStats is a worn item's passive-property snapshot, captured once
// at wear time from the item's own private state (an armor-category
// package) into the wearer's own state — the armor-world analog of
// WeaponStats, but for passive properties rather than combat stats.
// Sneaking is per-item data, not a blanket rule of being worn at all
// (WeaponStats.Enchantment is the equivalent precedent: per-weapon, not
// a fact about weapons in general) — a future non-sneaking boot or a
// helm would simply leave it false.
type ArmorStats struct {
	Slot     Slot   `json:"slot"`
	Sneaking bool   `json:"sneaking,omitempty"`
	Source   string `json:"source,omitempty"` // the worn item's own ID, e.g. "armor:3"
}
