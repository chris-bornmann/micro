package traits

import (
	"fmt"
	"strings"

	"micro/server/internal/rules"
)

// poisons poisons whoever its bearer's blow lands on: damage HP a turn for
// turns turns, less what the victim's constitution fights off (see
// Poison). It counts on the weapon a blow is struck with, or on the
// attacker itself — a venomous bite. A thrown item has left the thrower's
// hand and poisons nothing; a miss never reaches here at all.
type poisons struct{}

func (poisons) Params() []rules.ParamDef {
	return []rules.ParamDef{
		{Name: "damage", Kind: rules.ParamInt, Required: true, Min: rules.Bound(0)},
		{Name: "turns", Kind: rules.ParamInt, Required: true, Min: rules.Bound(1)},
	}
}

func (poisons) OnHit(c *rules.Ctx, hit *rules.Hit, h rules.Holder) {
	if h.Role != rules.RoleWeapon && h.Role != rules.RoleAttacker {
		return
	}
	Poison(c, hit.Attack.Defender, h.Params().Int("damage"), h.Params().Int("turns"))
}

// Poison tries to poison target for damage HP a turn over turns turns.
//
// The poison starts to act next turn: the blow or dart that delivered it
// is all the damage this turn.
//
// Stamina fights it: the per-turn damage drops by Stamina/5, rounded up.
// If that, or target's resistance to poison, leaves nothing, the poison
// doesn't take. Already poisoned, the worse of the two poisonings wins —
// the longer time left and the higher damage — rather than both running.
// The dead are past poisoning: a blow that kills poisons nothing, and says
// nothing about it.
func Poison(c *rules.Ctx, target *rules.Entity, damage, turns int) {
	if target.Dead {
		return
	}
	perTurn := damage - (target.Attrs.Stamina+4)/5
	if perTurn <= 0 || c.Taken(target, rules.DamagePoison, perTurn) <= 0 {
		c.Say(target, "You shrug off the poison.", fmt.Sprintf("The %s shrugs off the poison.", strings.ToLower(target.Name)))
		return
	}
	if inst := target.FindTrait("poisoned"); inst != nil {
		inst.Turns = max(inst.Turns, turns)
		inst.Params = rules.Params{"damage": max(inst.Params.Int("damage"), perTurn)}
		return
	}
	target.AddTrait(&rules.TraitInstance{Trait: "poisoned", Params: rules.Params{"damage": perTurn}, Turns: turns, From: c.NextTurn()})
	c.Say(target, "You feel poison burning in your veins!", fmt.Sprintf("The %s looks sick.", strings.ToLower(target.Name)))
}

// poisoned is being poisoned: damage poison damage each turn — through
// the bearer's resistances, so immunity found later still helps — until
// its turns run out. It can kill. Written into a template with no time
// limit, it never wears off. A cure action removes it.
type poisoned struct{}

func (poisoned) Params() []rules.ParamDef {
	return []rules.ParamDef{{Name: "damage", Kind: rules.ParamInt, Default: 1, Min: rules.Bound(0)}}
}

func (poisoned) Describe(h rules.Holder) (string, string, bool) { return "Poisoned", "yes", true }

// Modify stops natural healing while the poison lasts.
func (poisoned) Modify(q *rules.Query, h rules.Holder) {
	if q.Kind == rules.QueryStat && h.Role.Own() && q.Stat == "regeneration" {
		q.Mul *= 0
	}
}

func (poisoned) Tick(c *rules.Ctx, h rules.Holder) {
	inst, e := h.Inst, h.Carrier
	out := c.Hurt(e, rules.DamagePoison, h.Params().Int("damage"))
	if out.Dead {
		c.Say(e, fmt.Sprintf("You take %d poison damage and die.", out.Damage), fmt.Sprintf("%s takes %d poison damage and dies.", e.Name, out.Damage))
	} else {
		c.Sayf(e, "take", "takes", "%s %s %d poison damage.", out.Damage)
	}
	if inst.Turns <= 0 {
		return // permanent
	}
	inst.Turns--
	if inst.Turns == 0 {
		e.RemoveTrait(inst)
		if !out.Dead {
			c.Say(e, "The poison wears off.", "")
		}
	}
}
