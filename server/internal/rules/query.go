package rules

import (
	"math"

	"micro/server/internal/overlay"
)

// Role is the part a trait's carrier plays in the decision being made. A
// behavior uses it to decide whether it applies: a weapon's enchantment
// counts only when it is the Weapon, a mummy's vulnerability only when it
// is the Defender.
type Role int

const (
	// RoleSelf: the entity a stat query or tick is about.
	RoleSelf Role = iota
	// RoleGear: something RoleSelf has equipped.
	RoleGear
	// RoleAttacker and RoleAttackerGear: whoever is attacking, and what
	// they have equipped other than the weapon in use.
	RoleAttacker
	RoleAttackerGear
	// RoleWeapon: the wielded item an attack is made with.
	RoleWeapon
	// RoleProjectile: a thrown item, in flight or at rest.
	RoleProjectile
	// RoleDefender and RoleDefenderGear: whoever is being hurt, and what
	// they have equipped.
	RoleDefender
	RoleDefenderGear
	// RoleSubject: the item an Attempt is about.
	RoleSubject
)

// Defending reports whether r is the defender or the defender's gear.
func (r Role) Defending() bool { return r == RoleDefender || r == RoleDefenderGear }

// Own reports whether r is an entity's own trait or its gear's, in a stat
// query or tick about that entity.
func (r Role) Own() bool { return r == RoleSelf || r == RoleGear }

// Holder is what a hook is told about the instance it is being run for.
type Holder struct {
	Inst *TraitInstance
	// Carrier is the entity whose Traits hold Inst: a creature, or an item.
	Carrier *Entity
	// Owner is the creature Carrier belongs to: Carrier itself for a
	// creature's own traits, the wearer for an equipped item's, and nil
	// for an item nobody holds.
	Owner *Entity
	Role  Role
}

// Params returns the instance's parameters.
func (h Holder) Params() Params { return h.Inst.Params }

type participant struct {
	e     *Entity
	owner *Entity
	role  Role
}

// holders expands participants into one Holder per trait instance, in
// participant order and then instance order. That order is stable, but
// nothing depends on it for correctness: see Modifier.
func (reg *Registry) holders(ps []participant) []holderWithTrait {
	var out []holderWithTrait
	for _, p := range ps {
		if p.e == nil {
			continue
		}
		for _, inst := range p.e.Traits {
			t, ok := reg.traits[inst.Trait]
			if !ok {
				continue
			}
			out = append(out, holderWithTrait{Holder{Inst: inst, Carrier: p.e, Owner: p.owner, Role: p.role}, t})
		}
	}
	return out
}

type holderWithTrait struct {
	h Holder
	t Trait
}

// ownParticipants is e itself and everything it has equipped.
func ownParticipants(e *Entity) []participant {
	ps := []participant{{e, e, RoleSelf}}
	for _, it := range e.EquippedItems() {
		ps = append(ps, participant{it, e, RoleGear})
	}
	return ps
}

// QueryKind is what a Query computes.
type QueryKind int

const (
	// QueryStat: a named number about Subject, such as "invisible" or
	// "sneaking" (anything above 0 counts as having it).
	QueryStat QueryKind = iota
	// QueryDamageDealt: how far to shift one part of an attack's damage
	// range before it is rolled.
	QueryDamageDealt
	// QueryDamageTaken: how much of one part of rolled damage Subject
	// actually takes.
	QueryDamageTaken
	// QueryHitChance: the percent chance a melee blow lands on Subject.
	QueryHitChance
)

// Query is a number being computed. Base is the starting value; modifiers
// contribute through Add and Mul only, and the result is (Base+Add)*Mul.
type Query struct {
	Kind QueryKind
	Stat string
	Type DamageType
	// Primary is set on a damage query for an attack's first damage part,
	// which is the one a weapon's own bonuses apply to.
	Primary bool
	Attack  *Attack
	Subject *Entity

	Base int
	Add  int
	Mul  float64
}

func (q *Query) result() int {
	return int(math.Round(float64(q.Base+q.Add) * q.Mul))
}

// fold runs every Modifier among hs against q.
func fold(q *Query, hs []holderWithTrait) {
	for _, ht := range hs {
		if m, ok := ht.t.(Modifier); ok {
			m.Modify(q, ht.h)
		}
	}
}

// Stat computes the named stat for e from its own traits and its gear's.
func (reg *Registry) Stat(e *Entity, stat string) int {
	q := &Query{Kind: QueryStat, Stat: stat, Subject: e, Mul: 1}
	fold(q, reg.holders(ownParticipants(e)))
	return q.result()
}

// Has reports whether e's named stat is above zero.
func (reg *Registry) Has(e *Entity, stat string) bool { return reg.Stat(e, stat) > 0 }

// Look is how an entity appears to others.
type Look struct {
	Glyph rune
	Color overlay.Color
	Name  string
}

// Appearance returns how e looks to a player who recognizes what id says:
// its template's glyph, and its color and name — its look's, if it is an
// item of a type not yet identified — as changed by any Appearer among
// its own and its gear's traits.
func (reg *Registry) Appearance(e *Entity, id *Identity) Look {
	l := Look{Glyph: e.T.Glyph, Color: id.Color(e), Name: id.Name(e)}
	for _, ht := range reg.holders(ownParticipants(e)) {
		if a, ok := ht.t.(Appearer); ok {
			a.Appear(&l, ht.h)
		}
	}
	return l
}

// Object is e as something to draw: its position and how it looks right
// now, to a player who recognizes what id says.
func (reg *Registry) Object(e *Entity, id *Identity) overlay.Object {
	l := reg.Appearance(e, id)
	return overlay.Object{ID: e.ID, X: e.X, Y: e.Y, Glyph: l.Glyph, Color: l.Color, Layer: e.T.Layer}
}
