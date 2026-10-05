package rules

import (
	"math"
	"math/rand"

	"micro/server/internal/throw"
)

// Attack is one attack being resolved: who is hitting whom, and with what.
type Attack struct {
	Attacker, Defender *Entity
	// Weapon is the wielded item the attack is made with; nil for a
	// natural attack or a throw.
	Weapon *Entity
	// Projectile is the thrown item; nil for a melee attack.
	Projectile *Entity
	// Name is what the attack is made with, for messages.
	Name string
}

// Outcome is what an attack did to its defender. A miss did nothing.
type Outcome struct {
	Missed      bool
	Damage      int
	RemainingHP int
	Dead        bool
}

// The to-hit rule for melee. The chance starts at hitBase percent, gains
// a point for every point of the attacker's Dexterity and loses one for
// every point of the defender's, is adjusted by traits (accuracy,
// evasion, an invisible defender), and then kept within
// [hitFloor, hitCeiling]: however lopsided a fight, a blow can always
// land and can always miss. These are the numbers to turn for balance.
const (
	hitBase    = 50
	hitFloor   = 20
	hitCeiling = 90
)

// Hit is what a HitHandler is told about a hit that has landed.
type Hit struct {
	Attack  *Attack
	Outcome Outcome
}

// participants is everyone with a say in a: the attacker, the attacker's
// gear other than the weapon, the weapon or projectile, the defender and
// the defender's gear.
func (a *Attack) participants() []participant {
	var ps []participant
	if a.Attacker != nil {
		ps = append(ps, participant{a.Attacker, a.Attacker, RoleAttacker})
		for _, it := range a.Attacker.EquippedItems() {
			if it != a.Weapon {
				ps = append(ps, participant{it, a.Attacker, RoleAttackerGear})
			}
		}
	}
	if a.Weapon != nil {
		ps = append(ps, participant{a.Weapon, a.Attacker, RoleWeapon})
	}
	if a.Projectile != nil {
		ps = append(ps, participant{a.Projectile, a.Attacker, RoleProjectile})
	}
	ps = append(ps, participant{a.Defender, a.Defender, RoleDefender})
	for _, it := range a.Defender.EquippedItems() {
		ps = append(ps, participant{it, a.Defender, RoleDefenderGear})
	}
	return ps
}

// Melee resolves attacker swinging at defender with whatever attacker
// wields, or its natural attack if nothing. It rolls to hit first (see
// HitChance); a miss does nothing at all. Either way it says what
// happened, in the words of the weapon's or attacker's messages (see
// describe).
func (c *Ctx) Melee(attacker, defender *Entity) Outcome {
	a := &Attack{Attacker: attacker, Defender: defender}
	parts := attacker.T.Attack.Damage
	a.Name = attacker.T.Attack.Name
	if w := attacker.Weapon(); w != nil {
		a.Weapon, a.Name, parts = w, c.Name(w), w.T.Damage
	}
	c.provoke(attacker, defender)
	hs := c.Reg.holders(a.participants())
	if c.roll() >= c.hitChance(a, hs) {
		out := Outcome{Missed: true, RemainingHP: defender.HP}
		c.describe(a, out)
		return out
	}
	return c.resolve(a, hs, parts, true)
}

// HitChance is the percent chance attacker's melee blow lands on
// defender, all things considered.
func (c *Ctx) HitChance(attacker, defender *Entity) int {
	a := &Attack{Attacker: attacker, Defender: defender, Weapon: attacker.Weapon()}
	return c.hitChance(a, c.Reg.holders(a.participants()))
}

func (c *Ctx) hitChance(a *Attack, hs []holderWithTrait) int {
	base := hitBase + a.Attacker.Attrs.Dexterity - a.Defender.Attrs.Dexterity
	q := &Query{Kind: QueryHitChance, Attack: a, Subject: a.Defender, Base: base, Mul: 1}
	fold(q, hs)
	return min(max(q.result(), hitFloor), hitCeiling)
}

// roll is a percentile roll, 0-99.
func (c *Ctx) roll() int {
	if c.HitRoll != nil {
		return c.HitRoll()
	}
	return rand.Intn(100)
}

// ThrownHit resolves item, thrown by thrower, hitting target. Its damage
// comes from its weight and the thrower's Strength (throw.Damage), and the
// usual Strength bonus is not added on top: throw.Damage already counts
// Strength, and counting it twice would double it.
func (c *Ctx) ThrownHit(thrower, item, target *Entity) Outcome {
	c.provoke(thrower, target)
	lo, hi := throw.Damage(item.Weight, thrower.Attrs.Strength)
	a := &Attack{Attacker: thrower, Defender: target, Projectile: item, Name: item.Name}
	return c.resolve(a, c.Reg.holders(a.participants()), []DamagePart{{Type: DamagePhysical, Range: Range{lo, hi}}}, false)
}

// resolve rolls the damage of an attack that has landed, applies it to the
// defender, and runs every HitHandler. melee adds the Strength bonus and
// says what happened (a throw's caller says it itself).
//
// For each damage part, in order:
//  1. A QueryDamageDealt shifts both ends of the part's range by the total
//     of Add (a weapon's enchantment, a curse, a bonus against this kind of
//     defender) and scales them by Mul. The minimum is then clamped to 0 —
//     a badly cursed weapon can do nothing, never heal — and a maximum
//     below the minimum collapses to it.
//  2. The range is rolled from the global source: combat is random each
//     time, unlike spawning, which is seeded.
//  3. On the first part of a melee attack, the attacker's Strength/10 is
//     added — a core rule, not a trait, because every creature has it.
//  4. A QueryDamageTaken scales what the defender actually takes of this
//     type (vulnerable, resist), never below 0.
//
// The parts' total comes off the defender's HP at once.
func (c *Ctx) resolve(a *Attack, hs []holderWithTrait, parts []DamagePart, melee bool) Outcome {
	total := 0
	for i, part := range parts {
		dealt := &Query{Kind: QueryDamageDealt, Type: part.Type, Primary: i == 0, Attack: a, Subject: a.Defender, Mul: 1}
		fold(dealt, hs)
		lo := int(math.Round(float64(part.Range.Min+dealt.Add) * dealt.Mul))
		hi := int(math.Round(float64(part.Range.Max+dealt.Add) * dealt.Mul))
		if lo < 0 {
			lo = 0
		}
		if hi < lo {
			hi = lo
		}
		v := lo + rand.Intn(hi-lo+1)
		if i == 0 && melee && a.Attacker != nil {
			v += a.Attacker.Attrs.Strength / 10
		}
		total += c.taken(a.Defender, part.Type, v, a, hs)
	}
	out := c.applyDamage(a.Defender, total)
	if melee {
		c.describe(a, out)
	}

	hit := &Hit{Attack: a, Outcome: out}
	for _, ht := range hs {
		if h, ok := ht.t.(HitHandler); ok {
			h.OnHit(c, hit, ht.h)
		}
	}
	return out
}

// taken folds a QueryDamageTaken for amount of typ against target. hs is
// the attack's holders, or nil for damage with no attacker (burning), in
// which case only target's own traits and gear are consulted.
func (c *Ctx) taken(target *Entity, typ DamageType, amount int, a *Attack, hs []holderWithTrait) int {
	if hs == nil {
		var ps []participant
		ps = append(ps, participant{target, target, RoleDefender})
		for _, it := range target.EquippedItems() {
			ps = append(ps, participant{it, target, RoleDefenderGear})
		}
		hs = c.Reg.holders(ps)
	}
	q := &Query{Kind: QueryDamageTaken, Type: typ, Attack: a, Subject: target, Base: amount, Mul: 1}
	fold(q, hs)
	if v := q.result(); v > 0 {
		return v
	}
	return 0
}

// applyDamage takes amount off target's HP, marks it dead at 0 or below,
// and tells a living monster's AI it was hurt. HP may go negative; MaxHP
// caps healing, not damage.
func (c *Ctx) applyDamage(target *Entity, amount int) Outcome {
	target.HP -= amount
	if target.HP <= 0 {
		target.Dead = true
	}
	if !target.Dead && target.T.AI.Behavior != "" {
		if ai, ok := c.Reg.AI(target.T.AI.Behavior); ok {
			if h, ok := ai.(Hurter); ok {
				h.Hurt(target, target.T.AI.Params)
			}
		}
	}
	return Outcome{Damage: amount, RemainingHP: target.HP, Dead: target.Dead}
}

// Taken is how much of amount of typ damage target would actually take,
// after its own resistances and vulnerabilities — without taking it.
func (c *Ctx) Taken(target *Entity, typ DamageType, amount int) int {
	return c.taken(target, typ, amount, nil, nil)
}

// Hurt deals amount of typ damage to target with no attacker — burning,
// say — after target's own resistances and vulnerabilities.
func (c *Ctx) Hurt(target *Entity, typ DamageType, amount int) Outcome {
	return c.applyDamage(target, c.taken(target, typ, amount, nil, nil))
}

// provoke records that the player has attacked defender, hit or miss: a
// creature remembers being attacked, and when.
func (c *Ctx) provoke(attacker, defender *Entity) {
	if attacker != nil && attacker.IsPlayer() && !defender.IsPlayer() {
		defender.Mind.Attacked++
		defender.Mind.AttackedTurn = c.World.Turn
	}
}
