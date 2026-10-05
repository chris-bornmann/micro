package traits

import (
	"math/rand"

	"micro/server/internal/rules"
)

// Burning, in numbers. A fire lasts 1-3 turns per stack of oil on whoever
// caught it, and each turn it burns for 2-4 fire damage. Both are rolled
// from the global source: they happen at turn time, not spawn time.
const (
	burnTurnsPerOilMin = 1
	burnTurnsPerOilMax = 3
	burnDamageMin      = 2
	burnDamageMax      = 4
)

// BurnDuration is how many turns a fire lasts on something with oiliness
// stacks of oil: one 1-3 roll per stack.
func BurnDuration(oiliness int) int {
	total := 0
	for i := 0; i < oiliness; i++ {
		total += burnTurnsPerOilMin + rand.Intn(burnTurnsPerOilMax-burnTurnsPerOilMin+1)
	}
	return total
}

func burnDamage() int {
	return burnDamageMin + rand.Intn(burnDamageMax-burnDamageMin+1)
}

// ignite sets target on fire, if it will burn: soaked in oil, for 1-3
// turns a stack (BurnDuration), or flammable by nature, for its
// flammable trait's turns. Anything else does not catch. A fresh fire
// starts to burn next turn; a second ignition while already burning adds
// to the fire's length without changing when it started.
func ignite(c *rules.Ctx, target *rules.Entity) {
	turns := 0
	if oil := target.Stacks("oily"); oil > 0 {
		turns = BurnDuration(oil)
	} else if f := target.FindTrait("flammable"); f != nil {
		turns = f.Params.Roll("turns")
	}
	if turns <= 0 {
		return
	}
	if inst := target.FindTrait("on_fire"); inst != nil {
		inst.Turns += turns
		return
	}
	target.AddTrait(&rules.TraitInstance{Trait: "on_fire", Params: rules.Params{}, Turns: turns, From: c.NextTurn()})
}

// flammable is something that burns without oil — dry gauze, old bones —
// for turns turns whenever fire touches it (see ignite).
type flammable struct{}

func (flammable) Params() []rules.ParamDef {
	return []rules.ParamDef{{Name: "turns", Kind: rules.ParamDice, Required: true, Min: rules.Bound(1)}}
}

// ignites sets whoever the item hits on fire, if they are oily.
type ignites struct{}

func (ignites) Params() []rules.ParamDef { return nil }

func (ignites) OnHit(c *rules.Ctx, hit *rules.Hit, h rules.Holder) {
	if h.Role == rules.RoleProjectile || h.Role == rules.RoleWeapon {
		ignite(c, hit.Attack.Defender)
	}
}

// onFire burns whoever carries it each turn until it runs out. The damage
// is fire damage, so a mummy's vulnerability and anyone's resistance apply
// to it like any other fire.
type onFire struct{}

func (onFire) Params() []rules.ParamDef { return nil }

func (onFire) Describe(h rules.Holder) (string, string, bool) { return "Burning", "yes", true }

func (onFire) Tick(c *rules.Ctx, h rules.Holder) {
	inst, e := h.Inst, h.Carrier
	out := c.Hurt(e, rules.DamageFire, burnDamage())
	inst.Turns--
	if inst.Turns <= 0 {
		e.RemoveTrait(inst)
		c.Sayf(e, "take", "takes", "%s %s %d fire damage and the flames die out.", out.Damage)
		return
	}
	c.Sayf(e, "take", "takes", "%s %s %d fire damage from burning.", out.Damage)
}

// lightSource is a lit torch. Held, it lights its owner's surroundings;
// it burns down one turn at a time and is gone when it runs out; struck
// with, it adds 1-2 to the blow's fire; struck with or thrown, it sets a
// target that will burn alight; and it does not survive leaving its
// owner's hands, however that happens.
type lightSource struct{}

func (lightSource) Params() []rules.ParamDef { return nil }

func (lightSource) Modify(q *rules.Query, h rules.Holder) {
	switch {
	case q.Kind == rules.QueryStat && h.Role.Own() && q.Stat == "light":
		q.Add++
	case q.Kind == rules.QueryDamageDealt && h.Role == rules.RoleWeapon && q.Type == rules.DamageFire:
		// Rolled here, once per blow: the torch's own fire part is 0.
		q.Add += 1 + rand.Intn(2)
	}
}

func (lightSource) Tick(c *rules.Ctx, h rules.Holder) {
	inst := h.Inst
	if inst.Turns <= 0 {
		return
	}
	inst.Turns--
	if inst.Turns > 0 {
		return
	}
	c.Destroy(h.Owner, h.Carrier)
	if h.Owner != nil {
		name := c.Name(h.Carrier)
		c.Say(h.Owner, "Your "+name+" burns out.", h.Owner.Name+"'s "+name+" burns out.")
	}
}

func (lightSource) OnHit(c *rules.Ctx, hit *rules.Hit, h rules.Holder) {
	if h.Role == rules.RoleProjectile || h.Role == rules.RoleWeapon {
		ignite(c, hit.Attack.Defender)
	}
}

func (lightSource) OnRelease(c *rules.Ctx, r *rules.Release, h rules.Holder) {
	r.Destroyed = true
	if r.How == rules.ReleaseDropped {
		r.Notes = append(r.Notes, "It goes out and falls apart.")
		return
	}
	r.Notes = append(r.Notes, "The "+c.Name(r.Item)+" goes out and falls apart.")
}
