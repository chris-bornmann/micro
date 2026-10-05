package rules

import (
	"sort"

	"micro/server/internal/attributes"
	"micro/server/internal/overlay"
)

// Category is what sort of thing a template describes. Only Player and
// Monster change what the engine does (they are the intelligent entities
// that take turns); the item categories are descriptive and are checked at
// load time so a typo is an error rather than a silently new category.
type Category string

const (
	CategoryPlayer  Category = "player"
	CategoryMonster Category = "monster"
	CategoryWeapon  Category = "weapon"
	CategoryArmor   Category = "armor"
	CategoryPotion  Category = "potion"
	CategoryScroll  Category = "scroll"
	CategoryRing    Category = "ring"
	CategoryAmulet  Category = "amulet"
	CategoryTorch   Category = "torch"
	CategoryFlask   Category = "flask"
	CategoryStairs  Category = "stairs"
	CategoryCorpse  Category = "corpse"
	// CategoryTrap is a feature fixed to a floor cell (see the trap traits).
	CategoryTrap Category = "trap"
)

// Categories lists every valid Category.
var Categories = []Category{
	CategoryPlayer, CategoryMonster, CategoryWeapon, CategoryArmor, CategoryPotion,
	CategoryScroll, CategoryRing, CategoryAmulet, CategoryTorch, CategoryFlask, CategoryStairs, CategoryCorpse, CategoryTrap,
}

// IsCreature reports whether c is an intelligent entity: something with HP
// that takes turns, as opposed to an item.
func (c Category) IsCreature() bool { return c == CategoryPlayer || c == CategoryMonster }

// Slot is where an equipped item sits on its owner.
type Slot string

const (
	// SlotWeapon is the main hand: whatever is wielded, a lit torch
	// included.
	SlotWeapon Slot = "weapon"
	// SlotOffHand is the other hand: a shield, or a lit torch carried
	// alongside a weapon.
	SlotOffHand Slot = "off_hand"
	SlotHead    Slot = "head"
	SlotBody    Slot = "body"
	SlotHands   Slot = "hands"
	SlotFeet    Slot = "feet"
	SlotNeck    Slot = "neck"
	SlotCloak   Slot = "cloak"
	// SlotRing is what a ring template names: either hand will do, so a
	// ring goes on SlotRightHand or SlotLeftHand (see EquipSlotsFor). No
	// more than two rings, ever.
	SlotRing      Slot = "ring"
	SlotRightHand Slot = "right_hand"
	SlotLeftHand  Slot = "left_hand"
)

// EquipSlots are the slots a template may declare.
var EquipSlots = []Slot{SlotWeapon, SlotOffHand, SlotBody, SlotFeet, SlotHands, SlotHead, SlotNeck, SlotCloak, SlotRing}

// EquipSlotsFor is where an item whose template declares s can go, in
// order of preference.
func EquipSlotsFor(s Slot) []Slot {
	if s == SlotRing {
		return []Slot{SlotRightHand, SlotLeftHand}
	}
	return []Slot{s}
}

// slotOrder is the order equipped items are listed and visited in: the
// main hand, then the off hand, then armor alphabetically by slot.
func slotOrder(s Slot) int {
	switch s {
	case SlotWeapon:
		return 0
	case SlotOffHand:
		return 1
	default:
		return 2
	}
}

// DamageType names a kind of damage. An attack deals one of DealtTypes;
// traits such as vulnerable and resist act on one type — or on physical,
// which covers slashing, piercing and bludgeoning together.
type DamageType string

const (
	DamageSlashing    DamageType = "slashing"
	DamagePiercing    DamageType = "piercing"
	DamageBludgeoning DamageType = "bludgeoning"
	DamageFire        DamageType = "fire"
	DamagePoison      DamageType = "poison"
	// DamagePhysical is the umbrella over the three physical types. A
	// template's attack may not deal it — every blow is one of the three
	// — but a thrown item does: it hits with whatever surface it lands
	// on, so only resistance to physical damage in general applies.
	DamagePhysical DamageType = "physical"
)

// DealtTypes are the types an attack or weapon in a template may deal.
var DealtTypes = []DamageType{DamageSlashing, DamagePiercing, DamageBludgeoning, DamageFire, DamagePoison}

// Covers reports whether a trait about damage type d applies to damage
// of type t: the same type, or physical over any physical type.
func (d DamageType) Covers(t DamageType) bool {
	if d == t {
		return true
	}
	return d == DamagePhysical && (t == DamageSlashing || t == DamagePiercing || t == DamageBludgeoning)
}

// Range is an inclusive [Min, Max] pair, rolled uniformly.
type Range struct{ Min, Max int }

// DamagePart is one typed component of an attack's damage.
type DamagePart struct {
	Type  DamageType
	Range Range
}

// NaturalAttack is a creature's own attack, used when it has no weapon.
type NaturalAttack struct {
	Name   string
	Damage []DamagePart
}

// AISpec names a creature's AI behavior and its parameters.
type AISpec struct {
	Behavior string
	Params   Params
}

// SpawnModel is how many of a template appear on each level.
type SpawnModel string

const (
	// SpawnCommon draws a Poisson count weighted by how far the level is
	// from the monster's own level (spawn.GenericSpawnCount).
	SpawnCommon SpawnModel = "common"
	// SpawnSingleShot is a rare monster that appears at most once per run
	// (spawn.SingleShotShouldSpawn).
	SpawnSingleShot SpawnModel = "single_shot"
	// SpawnFixed places exactly Count on every level.
	SpawnFixed SpawnModel = "fixed"
	// SpawnPerLevel is for traps: on each level within Levels, with Chance
	// percent, place a number rolled from Number — and at least one on every
	// level in AlwaysOn.
	SpawnPerLevel SpawnModel = "per_level"
	// SpawnPlaced never appears at random: only where a hand-drawn level
	// puts it (see LevelSpec).
	SpawnPlaced SpawnModel = "placed"
)

// SpawnSpec is a template's spawn tuning. Which fields matter depends on
// Model; see the spawn package for what each one does.
type SpawnSpec struct {
	Model       SpawnModel
	Count       int
	Density     float64
	SigmaUp     float64
	SigmaDown   float64
	MaxAbove    float64
	Earliest    int
	ForceBy     int
	ChanceScale float64
	// SpawnPerLevel's tuning.
	Levels   Range
	Chance   int
	Number   Range
	AlwaysOn []int
}

// EquipSpec is a starting item a creature spawns wearing or wielding.
// LimitPerLevel caps how many creatures of this template on one level get
// it (0 means every one), which is how only the first goblin on a level
// carries the rusty sword.
type EquipSpec struct {
	Item          *Template
	LimitPerLevel int
	// Chance is the percent chance the creature spawns with it.
	Chance int
}

// CarrySpec is an item a creature spawns carrying, unequipped, with Chance
// percent.
type CarrySpec struct {
	Item   *Template
	Chance int
}

// TraitSpec is a trait as written in a template: a behavior name and its
// parameters, some of which may still be ranges to roll when an entity is
// created (see NewTraitInstance).
type TraitSpec struct {
	Trait  string
	Params Params
	// Duration is how many turns the trait lasts on whatever is made from
	// the template — a hand-crafted player poisoned from the start, say.
	// 0 means for good.
	Duration int
}

// OptionSpec is one option on a utility AI creature's menu: which option,
// how much it counts (Weight multiplies its score), and its parameters.
type OptionSpec struct {
	Option string
	Weight float64
	Params Params
}

// ActionSpec is an on-use action as written in a template, with the same
// roll-at-creation rule for range parameters as TraitSpec.
type ActionSpec struct {
	Action string
	Params Params
}

// Template is everything data-driven about one kind of entity. Templates
// are built by package content and are never modified afterward; several
// entities share one.
type Template struct {
	ID       string
	Name     string
	Category Category
	// Kind is the matchup tag other code compares against (a weapon's
	// bonus_vs). It defaults to ID.
	Kind   string
	Glyph  rune
	Color  overlay.Color
	Layer  int
	Weight int
	// Level is a creature's power level, and the center of its spawn
	// curve.
	Level int
	HP    Range
	// Attributes bounds each of a creature's four attribute rolls.
	Attributes attributes.Ranges

	// Attack is a creature's natural attack.
	Attack NaturalAttack
	// Damage is what an item deals when wielded as a weapon.
	Damage []DamagePart
	// Slot is where an item goes when equipped; empty if it cannot be.
	Slot Slot
	// Blocks are other slots the item takes up while equipped: iron
	// gloves wielded cover the hands; a two-handed sword would block the
	// off hand.
	Blocks []Slot
	// UseVerb is how using the item reads ("drink", "read").
	UseVerb string
	// Stairs is "up" or "down" for a staircase, empty otherwise.
	Stairs string
	// Unidentified, if set, is how the item can go unrecognized: shown
	// only by its look until its type is identified (see Identity).
	Unidentified *Unidentified

	Traits []TraitSpec
	OnUse  []ActionSpec
	AI     AISpec
	// Brain is a utility AI creature's menu of options (see Option).
	Brain     []OptionSpec
	Spawn     SpawnSpec
	Equipment []EquipSpec
	// Inventory is what a creature spawns carrying, unequipped.
	Inventory []CarrySpec
	// LeavesCorpse is whether a creature leaves a corpse when it dies.
	LeavesCorpse bool
	// Attitude is how a creature starts out feeling about the player.
	// Empty means hostile.
	Attitude Attitude
	// Unique marks a character — at most one in a whole run, with an
	// upper-case glyph — that may leave a level alive and come back.
	Unique bool
	// ReturnChance is the percent chance a unique character that left
	// turns up again on each new, deeper level.
	ReturnChance int

	// Pronoun is he, she, it or they, for messages. Defaults to it.
	Pronoun string
	// Messages describe melee attacks: a creature's own natural attack,
	// or attacks made with an item. See describe.
	Messages AttackMessages
}

// Catalog is every template, by ID; the pools of looks unidentified
// items are dealt from (see Identity); and the special levels.
type Catalog struct {
	byID   map[string]*Template
	ids    []string
	Pools  map[string]*Pool
	Levels []*LevelSpec
}

// LevelSpec is a special level: one drawn by hand, or made by a generator
// other than the usual rooms and corridors (a maze). Which depth it takes
// is settled once per game (see Depths and Chance).
type LevelSpec struct {
	Name string
	// Depths is where it may go: a single depth, or a range to pick from.
	Depths Range
	// Chance is the percent chance a game has it at all.
	Chance int
	// Map is a hand-drawn level's drawing, and Legend what each of its
	// other characters places there (a template ID).
	Map    string
	Legend map[rune]string
	// Generator names another generator ("maze"), for a level not drawn;
	// Braid tunes a maze (see mapgen.GenerateMaze).
	Generator string
	Braid     int
	// Populate is what the level also gets at random, by its depth, the
	// way an ordinary level does — on top of whatever it places.
	Populate Populate
}

// Populate says which of a level's random spawns happen: monsters, items
// and traps, each chosen by the level's depth as on any level.
type Populate struct {
	Monsters, Items, Traps bool
}

// PopulateAll is every random spawn: an ordinary level's.
var PopulateAll = Populate{Monsters: true, Items: true, Traps: true}

// PoolNames returns every pool's name, sorted.
func (c *Catalog) PoolNames() []string {
	names := make([]string, 0, len(c.Pools))
	for n := range c.Pools {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// NewCatalog builds a Catalog from templates, which must have unique IDs.
func NewCatalog(templates []*Template) *Catalog {
	c := &Catalog{byID: map[string]*Template{}, Pools: map[string]*Pool{}}
	for _, t := range templates {
		c.byID[t.ID] = t
		c.ids = append(c.ids, t.ID)
	}
	sort.Strings(c.ids)
	return c
}

// Get returns the template with the given ID, or nil.
func (c *Catalog) Get(id string) *Template { return c.byID[id] }

// ByCategory returns every template of category cat, sorted by ID so that
// anything iterating them (spawning, above all) is reproducible.
func (c *Catalog) ByCategory(cat Category) []*Template {
	var out []*Template
	for _, id := range c.ids {
		if t := c.byID[id]; t.Category == cat {
			out = append(out, t)
		}
	}
	return out
}

// All returns every template, sorted by ID.
func (c *Catalog) All() []*Template {
	out := make([]*Template, len(c.ids))
	for i, id := range c.ids {
		out[i] = c.byID[id]
	}
	return out
}
