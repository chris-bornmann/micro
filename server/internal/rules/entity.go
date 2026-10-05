package rules

import (
	"fmt"
	"math/rand"
	"slices"
	"sort"
	"sync/atomic"

	"micro/server/internal/attributes"
	"micro/server/internal/mapdata"
)

// TraitInstance is one trait on one entity: a behavior name, its rolled
// parameters, and the bits of state the engine and behaviors keep on it.
type TraitInstance struct {
	Trait  string
	Params Params
	// Turns is how many turns remain before the instance expires; 0 means
	// permanent. The engine counts it down unless the behavior is a
	// Ticker, which does its own.
	Turns int
	// Stacks counts repeated applications of a stacking trait (how oily
	// something is).
	Stacks int
	// From is the turn (World.Turn) the instance starts ticking; until
	// then the engine passes it over each turn, neither ticking it nor
	// counting it down. 0, the default, means at once. An attack's
	// lingering effect starts the turn after the blow (Ctx.NextTurn), so
	// the blow itself is all the damage that turn.
	From int
	// Suspended means the instance is not in effect this turn. Only a
	// Ticker sets or clears it, once per turn, so it holds steady however
	// many times the trait is consulted in between.
	Suspended bool
	// Source is the ID of whatever granted the instance, if anything.
	Source string
}

// Mind is a monster AI's own memory between turns. The engine never reads
// it; it belongs to whichever AI behavior the monster's template names.
type Mind struct {
	// Aware is whether the monster has noticed the player.
	Aware bool
	// Fleeing is whether it is running away, and FleeRoom/FleeX/FleeY
	// where to. FleeRoom -1 means fleeing with no destination chosen yet.
	Fleeing      bool
	FleeRoom     int
	FleeX, FleeY int

	// Attitude is how it feels about the player: hostile, neutral or
	// afraid. It starts as its template says.
	Attitude Attitude
	// Attacked counts the times the player has attacked it, hit or miss,
	// and AttackedTurn is the turn (World.Turn) of the latest: it
	// remembers being attacked, and can tell a fresh attack from an old
	// one.
	Attacked     int
	AttackedTurn int
	// LastSeen is where it last saw the player, if Seen.
	Seen     bool
	LastSeen mapdata.Point
	// Counters are named numbers an AI keeps for itself — items stolen,
	// turns spent fighting — so a new behavior needs no new field.
	Counters map[string]int
}

// Provoked reports whether the player has ever attacked it.
func (m *Mind) Provoked() bool { return m.Attacked > 0 }

// Count returns the named counter.
func (m *Mind) Count(name string) int { return m.Counters[name] }

// Add adds n to the named counter.
func (m *Mind) Add(name string, n int) {
	if m.Counters == nil {
		m.Counters = map[string]int{}
	}
	m.Counters[name] += n
}

// Attitude is how a creature feels about the player.
type Attitude string

const (
	Hostile Attitude = "hostile"
	Neutral Attitude = "neutral"
	Afraid  Attitude = "afraid"
)

// Attitudes lists every Attitude.
var Attitudes = []Attitude{Hostile, Neutral, Afraid}

// Entity is a player, a monster or an item. Which fields matter depends on
// T.Category: an item has no HP, and a creature is never in anyone's
// inventory.
type Entity struct {
	ID string
	T  *Template
	// Name is the display name. It starts as T.Name; a corpse gets its own.
	Name   string
	X, Y   int
	Weight int
	Level  int

	HP, MaxHP int
	// Healing is the healing points built up toward the next HP (see
	// regenerate).
	Healing int
	Attrs   attributes.Attributes
	Dead    bool

	Traits    []*TraitInstance
	Uses      []*ActionInstance
	Equipped  map[Slot]*Entity
	Inventory []*Entity
	Mind      Mind
	// Source is, for a corpse, the ID of the creature it was.
	Source string
}

// ActionInstance is an on-use action with its parameters rolled.
type ActionInstance struct {
	Action string
	Params Params
}

var nextID atomic.Int64

// newID returns a fresh ID namespaced by prefix, e.g. "goblin:7".
func newID(prefix string) string {
	return fmt.Sprintf("%s:%d", prefix, nextID.Add(1))
}

// New creates an entity from t at (x, y). rng rolls its HP, attributes and
// every range parameter on its traits and actions, in that order; nil uses
// the global source. Starting equipment is not created here — see
// Equip and the spawner.
func New(t *Template, x, y int, rng *rand.Rand) *Entity {
	e := &Entity{
		ID:     newID(t.ID),
		T:      t,
		Name:   t.Name,
		X:      x,
		Y:      y,
		Weight: t.Weight,
		Level:  t.Level,
	}
	if t.Category.IsCreature() {
		e.HP = rollRange(t.HP, rng)
		e.MaxHP = e.HP
		if rng == nil {
			rng = rand.New(rand.NewSource(rand.Int63()))
		}
		e.Attrs = t.Attributes.Roll(rng)
		e.Mind.FleeRoom = -1
		e.Mind.Attitude = t.Attitude
		if e.Mind.Attitude == "" {
			e.Mind.Attitude = Hostile
		}
	}
	for _, s := range t.Traits {
		e.Traits = append(e.Traits, &TraitInstance{Trait: s.Trait, Params: s.Params.roll(rng), Turns: s.Duration})
	}
	for _, s := range t.OnUse {
		e.Uses = append(e.Uses, &ActionInstance{Action: s.Action, Params: s.Params.roll(rng)})
	}
	return e
}

// IsCreature reports whether e is a player or monster.
func (e *Entity) IsCreature() bool { return e.T.Category.IsCreature() }

// IsPlayer reports whether e is the player.
func (e *Entity) IsPlayer() bool { return e.T.Category == CategoryPlayer }

// Kind is the matchup tag a weapon's bonus_vs compares against.
func (e *Entity) Kind() string {
	if e.T.Kind != "" {
		return e.T.Kind
	}
	return e.T.ID
}

// AddTrait gives e a new trait instance and returns it.
func (e *Entity) AddTrait(inst *TraitInstance) *TraitInstance {
	e.Traits = append(e.Traits, inst)
	return inst
}

// FindTrait returns e's first instance of the named trait, if any.
func (e *Entity) FindTrait(name string) *TraitInstance {
	for _, t := range e.Traits {
		if t.Trait == name {
			return t
		}
	}
	return nil
}

// RemoveTrait removes inst from e, if present.
func (e *Entity) RemoveTrait(inst *TraitInstance) {
	for i, t := range e.Traits {
		if t == inst {
			e.Traits = append(e.Traits[:i], e.Traits[i+1:]...)
			return
		}
	}
}

// Stacks returns the stack count of e's instance of the named trait, or 0.
func (e *Entity) Stacks(name string) int {
	if t := e.FindTrait(name); t != nil {
		return t.Stacks
	}
	return 0
}

// AddStacks adds n stacks of the named trait to e, creating a permanent
// instance if it has none.
func (e *Entity) AddStacks(name string, n int) {
	t := e.FindTrait(name)
	if t == nil {
		t = e.AddTrait(&TraitInstance{Trait: name, Params: Params{}})
	}
	t.Stacks += n
}

// EquippedItems returns e's equipped items in listing order: the weapon,
// then a light source, then armor by slot name.
func (e *Entity) EquippedItems() []*Entity {
	slots := make([]Slot, 0, len(e.Equipped))
	for s := range e.Equipped {
		slots = append(slots, s)
	}
	sort.Slice(slots, func(i, j int) bool {
		oi, oj := slotOrder(slots[i]), slotOrder(slots[j])
		if oi != oj {
			return oi < oj
		}
		return slots[i] < slots[j]
	})
	out := make([]*Entity, len(slots))
	for i, s := range slots {
		out[i] = e.Equipped[s]
	}
	return out
}

// Lit returns the lit torch e holds, if any: an equipped item that is
// burning, in either hand.
func (e *Entity) Lit() *Entity {
	for _, it := range e.EquippedItems() {
		if it.FindTrait(lightSourceTrait) != nil {
			return it
		}
	}
	return nil
}

// blockerOf returns what e has equipped elsewhere that takes up slot too
// (see Template.Blocks), if anything.
func (e *Entity) blockerOf(slot Slot) *Entity {
	for s, it := range e.Equipped {
		if s != slot && slices.Contains(it.T.Blocks, slot) {
			return it
		}
	}
	return nil
}

// SlotOf returns the slot item is equipped in on e, if it is.
func (e *Entity) SlotOf(item *Entity) (Slot, bool) {
	for s, it := range e.Equipped {
		if it == item {
			return s, true
		}
	}
	return "", false
}

// Weapon returns e's wielded weapon, or nil.
func (e *Entity) Weapon() *Entity { return e.Equipped[SlotWeapon] }

// Carried returns everything e carries — equipped items in listing order,
// then inventory — which is the order the inventory view and drop picker
// show them in.
func (e *Entity) Carried() []*Entity {
	return append(e.EquippedItems(), e.Inventory...)
}

// removeFromInventory removes item from e's inventory, reporting whether
// it was there.
func (e *Entity) removeFromInventory(item *Entity) bool {
	for i, it := range e.Inventory {
		if it == item {
			e.Inventory = append(e.Inventory[:i], e.Inventory[i+1:]...)
			return true
		}
	}
	return false
}

// release takes item off e wherever it is — an equip slot or the
// inventory — reporting whether it was found.
func (e *Entity) release(item *Entity) bool {
	if s, ok := e.SlotOf(item); ok {
		delete(e.Equipped, s)
		return true
	}
	return e.removeFromInventory(item)
}

// equipInto puts item in slot, creating the map on first use.
func (e *Entity) equipInto(slot Slot, item *Entity) {
	if e.Equipped == nil {
		e.Equipped = map[Slot]*Entity{}
	}
	e.Equipped[slot] = item
}
