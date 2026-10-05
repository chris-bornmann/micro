// Package traits holds the built-in trait behaviors: the vocabulary a
// template's traits: list is written in.
//
// Each behavior is small and does one thing, and most are parameterized so
// that one Go type serves many items and monsters. A new behavior is a type
// implementing rules.Trait plus whichever rules hook interfaces it needs,
// registered in Register below.
package traits

import (
	"fmt"
	"math/rand"
	"strings"

	"micro/server/internal/rules"
)

// Register adds every built-in trait to reg.
func Register(reg *rules.Registry) {
	reg.AddTrait("enchant", enchant{})
	reg.AddTrait("bonus_vs", bonusVs{})
	reg.AddTrait("cursed", cursed{})
	reg.AddTrait("vulnerable", damageTakenMultiplier{defaultMultiplier: 2})
	reg.AddTrait("resist", damageTakenMultiplier{defaultMultiplier: 0.5})
	reg.AddTrait("vampiric_attack", vampiricAttack{})
	reg.AddTrait("accuracy", hitChance{attacking: true})
	reg.AddTrait("evasion", hitChance{attacking: false})
	reg.AddTrait("stat", stat{})
	reg.AddTrait("invisible", invisible{reg: reg})
	reg.AddTrait("see_invisible", flag{stat: "see_invisible", label: "See invisible", expired: "Your sight returns to normal."})
	reg.AddTrait("sneaking", flag{stat: "sneaking", label: "Sneaking"})
	reg.AddTrait("monster_vision", flag{stat: "monster_vision", label: "Monster vision", expired: "Your monster vision fades."})
	reg.AddTrait("fixed", fixed{})
	reg.AddTrait("fragile", fragile{})
	reg.AddTrait("spills_oil", spillsOil{})
	reg.AddTrait("adds_oil", addsOil{})
	reg.AddTrait("oily", oily{})
	reg.AddTrait("ignites", ignites{})
	reg.AddTrait("on_fire", onFire{})
	reg.AddTrait("hidden", flag{stat: "hidden", label: "Hidden"})
	reg.AddTrait("pit", pit{})
	reg.AddTrait("in_pit", inPit{})
	reg.AddTrait("dart", dart{})
	reg.AddTrait("trap_door", trapDoor{})
	reg.AddTrait("teleport_trap", teleportTrap{})
	reg.AddTrait("poisons", poisons{})
	reg.AddTrait("poisoned", poisoned{})
	reg.AddTrait("light_source", lightSource{})
	reg.AddTrait("regeneration", regeneration{})
	reg.AddTrait("armor", armor{})
	reg.AddTrait("flammable", flammable{})
	reg.AddTrait("wasting", wasting{})
}

// wielded reports whether h is the weapon an attack is being made with,
// and the query is about its main damage — the part a weapon's own
// bonuses apply to.
func wielded(q *rules.Query, h rules.Holder) bool {
	return q.Kind == rules.QueryDamageDealt && h.Role == rules.RoleWeapon && q.Primary
}

// enchant shifts a weapon's damage range by amount: +1 on a fine dagger.
type enchant struct{}

func (enchant) Params() []rules.ParamDef {
	return []rules.ParamDef{{Name: "amount", Kind: rules.ParamInt, Required: true}}
}

func (enchant) Modify(q *rules.Query, h rules.Holder) {
	if wielded(q, h) {
		q.Add += h.Params().Int("amount")
	}
}

// bonusVs shifts a weapon's damage range by amount against one kind of
// defender.
type bonusVs struct{}

func (bonusVs) Params() []rules.ParamDef {
	return []rules.ParamDef{
		{Name: "kind", Kind: rules.ParamString, Required: true},
		{Name: "amount", Kind: rules.ParamInt, Required: true},
	}
}

func (bonusVs) Modify(q *rules.Query, h rules.Holder) {
	if wielded(q, h) && q.Attack.Defender.Kind() == h.Params().String("kind") {
		q.Add += h.Params().Int("amount")
	}
}

// cursed does two things at two different times. In combat it shifts the
// weapon's damage by damage (usually negative). And once equipped, the
// item refuses to come off: it cannot be unwielded, swapped, dropped or
// thrown until the curse is gone.
type cursed struct{}

func (cursed) Params() []rules.ParamDef {
	return []rules.ParamDef{{Name: "damage", Kind: rules.ParamInt, Default: 0}}
}

func (cursed) Modify(q *rules.Query, h rules.Holder) {
	if wielded(q, h) {
		q.Add += h.Params().Int("damage")
	}
}

func (cursed) Veto(a *rules.Attempt, h rules.Holder) string {
	if h.Role != rules.RoleSubject || h.Carrier != a.Item {
		return ""
	}
	switch a.Kind {
	case rules.AttemptUnequip, rules.AttemptDrop, rules.AttemptThrow:
	default:
		return ""
	}
	slot, equipped := a.Actor.SlotOf(a.Item)
	if !equipped {
		return ""
	}
	switch slot {
	case rules.SlotWeapon:
		return fmt.Sprintf("The %s is welded to your hand!", a.Name)
	case rules.SlotRightHand, rules.SlotLeftHand:
		return fmt.Sprintf("The %s won't come off your finger!", a.Name)
	}
	return fmt.Sprintf("The %s won't come off!", a.Name)
}

// hitChance adjusts the chance of a melee blow landing by amount
// percentage points. As accuracy it counts for the attacker — on
// themselves, their weapon or their gear — and adds; as evasion it counts
// for the defender or what they wear, and subtracts.
type hitChance struct{ attacking bool }

func (hitChance) Params() []rules.ParamDef {
	return []rules.ParamDef{{Name: "amount", Kind: rules.ParamInt, Required: true}}
}

func (t hitChance) Modify(q *rules.Query, h rules.Holder) {
	if q.Kind != rules.QueryHitChance {
		return
	}
	switch {
	case t.attacking && (h.Role == rules.RoleAttacker || h.Role == rules.RoleAttackerGear || h.Role == rules.RoleWeapon):
		q.Add += h.Params().Int("amount")
	case !t.attacking && h.Role.Defending():
		q.Add -= h.Params().Int("amount")
	}
}

// vampiricAttack heals the attacker by drain_percent of the damage each
// hit does, rounded to the nearest point and never above their maximum HP.
// It counts wherever the attacker carries it into the fight: on
// themselves (a vampire's bite), on the weapon they strike with, or on
// anything else they wear. A thrown item is out of the thrower's hands,
// so it drains nothing.
type vampiricAttack struct{}

// message, if given, replaces the "Vampire drains 2 HP." line: {amount}
// becomes the HP drained, and "" says nothing, for an attack whose own
// description already tells of it.
func (vampiricAttack) Params() []rules.ParamDef {
	return []rules.ParamDef{
		{Name: "drain_percent", Kind: rules.ParamInt, Required: true, Min: rules.Bound(0), Max: rules.Bound(100)},
		{Name: "message", Kind: rules.ParamString},
	}
}

func (vampiricAttack) OnHit(c *rules.Ctx, hit *rules.Hit, h rules.Holder) {
	switch h.Role {
	case rules.RoleAttacker, rules.RoleAttackerGear, rules.RoleWeapon:
	default:
		return
	}
	a := hit.Attack.Attacker
	drain := (hit.Outcome.Damage*h.Params().Int("drain_percent") + 50) / 100
	if drain > a.MaxHP-a.HP {
		drain = a.MaxHP - a.HP
	}
	if drain <= 0 {
		return
	}
	a.HP += drain
	if !h.Params().Has("message") {
		c.Sayf(a, "drain", "drains", "%s %s %d HP.", drain)
	} else if msg := h.Params().String("message"); msg != "" {
		msg = strings.ReplaceAll(msg, "{amount}", fmt.Sprint(drain))
		c.Say(a, msg, msg)
	}
}

// damageTakenMultiplier scales damage of one type taken by whoever carries
// it (or wears it): vulnerable doubles it by default, resist halves it.
// A multiplier of 0 is immunity. type: physical covers slashing, piercing
// and bludgeoning (and a thrown item's untyped blow).
type damageTakenMultiplier struct{ defaultMultiplier float64 }

func (d damageTakenMultiplier) Params() []rules.ParamDef {
	return []rules.ParamDef{
		{Name: "type", Kind: rules.ParamDamageType, Required: true},
		{Name: "multiplier", Kind: rules.ParamFloat, Default: d.defaultMultiplier},
	}
}

func (damageTakenMultiplier) Modify(q *rules.Query, h rules.Holder) {
	if q.Kind == rules.QueryDamageTaken && h.Role.Defending() && rules.DamageType(h.Params().String("type")).Covers(q.Type) {
		q.Mul *= h.Params().Float("multiplier")
	}
}

// armor takes amount off every physical part — slashing, piercing,
// bludgeoning — of a hit on its bearer, down to nothing at all; fire,
// poison and the like get through. A negative amount (cursed armor) adds
// to them instead. It counts on the bearer or anything it wears or
// wields, and applies before any resistance multiplies what is left.
type armor struct{}

func (armor) Params() []rules.ParamDef {
	return []rules.ParamDef{{Name: "amount", Kind: rules.ParamInt, Required: true}}
}

func (armor) Modify(q *rules.Query, h rules.Holder) {
	if q.Kind == rules.QueryDamageTaken && h.Role.Defending() && rules.DamagePhysical.Covers(q.Type) {
		q.Add -= h.Params().Int("amount")
	}
}

// stat adds amount to a named stat of whoever carries or wears it.
type stat struct{}

func (stat) Params() []rules.ParamDef {
	return []rules.ParamDef{
		{Name: "stat", Kind: rules.ParamString, Required: true},
		{Name: "amount", Kind: rules.ParamInt, Required: true},
	}
}

func (stat) Modify(q *rules.Query, h rules.Holder) {
	if q.Kind == rules.QueryStat && h.Role.Own() && q.Stat == h.Params().String("stat") {
		q.Add += h.Params().Int("amount")
	}
}

// flag gives whoever carries or wears it a yes/no stat and shows it on
// their status. Granted for a limited time, it says expired when it
// wears off, if that is set.
type flag struct {
	stat, label, expired string
}

func (flag) Params() []rules.ParamDef { return nil }

func (f flag) Modify(q *rules.Query, h rules.Holder) {
	if q.Kind == rules.QueryStat && h.Role.Own() && q.Stat == f.stat {
		q.Add++
	}
}

func (f flag) Describe(h rules.Holder) (string, string, bool) { return f.label, "yes", true }

func (f flag) Expired(c *rules.Ctx, h rules.Holder) {
	if f.expired != "" && h.Owner != nil {
		c.Say(h.Owner, f.expired, "")
	}
}

// invisible hides whoever carries or wears it from others' sight. Two
// things let them be seen anyway:
//   - visible_below: their HP drops below this. 0, the default, means
//     never while alive.
//   - periodic_visible: the percent chance, rolled once each turn, that
//     they show for that turn. 0, the default, means never.
//
// While in effect it also makes them harder to hit: evasion percentage
// points off any melee blow aimed at them — unless the attacker can see
// invisible, in which case the bearer is no harder to hit than anyone.
//
// Granted for a limited time (a potion), it says when it wears off.
//
// The roll happens when the trait ticks and is kept on the instance, not
// made each time visibility is asked: the AI and every frame drawn in a
// turn must agree on what they saw.
type invisible struct{ reg *rules.Registry }

func (invisible) Params() []rules.ParamDef {
	return []rules.ParamDef{
		{Name: "visible_below", Kind: rules.ParamInt, Default: 0, Min: rules.Bound(0)},
		{Name: "periodic_visible", Kind: rules.ParamInt, Default: 0, Min: rules.Bound(0), Max: rules.Bound(100)},
		{Name: "evasion", Kind: rules.ParamInt, Default: 20, Min: rules.Bound(0), Max: rules.Bound(100)},
	}
}

// inEffect reports whether h's bearer is actually hidden right now.
func (invisible) inEffect(h rules.Holder) bool {
	if h.Inst.Suspended {
		return false
	}
	return h.Owner == nil || h.Owner.HP >= h.Params().Int("visible_below")
}

func (i invisible) Modify(q *rules.Query, h rules.Holder) {
	switch {
	case q.Kind == rules.QueryStat && h.Role.Own() && q.Stat == "invisible" && i.inEffect(h):
		q.Add++
	case q.Kind == rules.QueryHitChance && h.Role.Defending() && i.inEffect(h) && !i.reg.Has(q.Attack.Attacker, "see_invisible"):
		q.Add -= h.Params().Int("evasion")
	}
}

func (i invisible) Describe(h rules.Holder) (string, string, bool) {
	return "Invisible", "yes", i.inEffect(h)
}

func (invisible) Tick(c *rules.Ctx, h rules.Holder) {
	inst := h.Inst
	if inst.Turns > 0 {
		inst.Turns--
		if inst.Turns == 0 {
			h.Carrier.RemoveTrait(inst)
			if h.Owner != nil {
				c.Say(h.Owner, "Your invisibility wears off.", h.Owner.Name+" flickers back into view.")
			}
			return
		}
	}
	inst.Suspended = rand.Intn(100) < h.Params().Int("periodic_visible")
}

// fixed anchors an item in place: it can never be picked up, and once its
// tile has been seen it is remembered like the map itself.
type fixed struct{}

func (fixed) Params() []rules.ParamDef { return nil }

func (fixed) Modify(q *rules.Query, h rules.Holder) {
	if q.Kind == rules.QueryStat && h.Role == rules.RoleSelf && q.Stat == "fixed" {
		q.Add++
	}
}

func (fixed) Veto(a *rules.Attempt, h rules.Holder) string {
	if h.Role == rules.RoleSubject && a.Kind == rules.AttemptPickUp {
		return fmt.Sprintf("The %s can't be moved.", a.Name)
	}
	return ""
}

// fragile items break when thrown, wherever they land. The messages say
// how, for each way a throw can end.
type fragile struct{}

func (fragile) Params() []rules.ParamDef {
	return []rules.ParamDef{
		{Name: "hit", Kind: rules.ParamString, Default: "It shatters!"},
		{Name: "wall", Kind: rules.ParamString, Default: "It shatters!"},
		{Name: "floor", Kind: rules.ParamString, Default: "It shatters!"},
	}
}

func (fragile) OnRelease(c *rules.Ctx, r *rules.Release, h rules.Holder) {
	var key string
	switch r.How {
	case rules.ReleaseThrownHit:
		key = "hit"
	case rules.ReleaseThrownWall:
		key = "wall"
	case rules.ReleaseThrownFloor:
		key = "floor"
	default:
		return
	}
	r.Destroyed = true
	if msg := h.Params().String(key); msg != "" {
		r.Notes = append(r.Notes, msg)
	}
}

// spillsOil coats the floor in oil where a thrown item lands on open
// ground.
type spillsOil struct{}

func (spillsOil) Params() []rules.ParamDef { return nil }

func (spillsOil) OnRelease(c *rules.Ctx, r *rules.Release, h rules.Holder) {
	if r.How == rules.ReleaseThrownFloor {
		c.World.SpillOil(r.X, r.Y)
	}
}

// addsOil douses whoever the item hits: one more stack of oily.
type addsOil struct{}

func (addsOil) Params() []rules.ParamDef { return nil }

func (addsOil) OnHit(c *rules.Ctx, hit *rules.Hit, h rules.Holder) {
	if h.Role == rules.RoleProjectile || h.Role == rules.RoleWeapon {
		hit.Attack.Defender.AddStacks("oily", 1)
	}
}

// oily records how many times something has been doused in oil. It does
// nothing by itself; ignites reads it.
type oily struct{}

func (oily) Params() []rules.ParamDef { return nil }

func (oily) Describe(h rules.Holder) (string, string, bool) {
	return "Oily", fmt.Sprintf("x%d", h.Inst.Stacks), h.Inst.Stacks > 0
}
