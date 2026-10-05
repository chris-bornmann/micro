package traits

// Traits about natural healing (see rules.Regeneration). Each counts for
// whoever has it, or wears or wields the thing that has it.

import (
	"fmt"

	"micro/server/internal/rules"
)

// regeneration changes how fast its bearer heals: its healing rate times
// multiplier, plus amount points a turn. multiplier 0 means it never heals
// (the undead); 2, twice as fast (a ring of regeneration).
type regeneration struct{}

func (regeneration) Params() []rules.ParamDef {
	return []rules.ParamDef{
		{Name: "multiplier", Kind: rules.ParamFloat, Default: 1.0, Min: rules.Bound(0)},
		{Name: "amount", Kind: rules.ParamInt, Default: 0},
	}
}

func (regeneration) Modify(q *rules.Query, h rules.Holder) {
	if q.Kind == rules.QueryStat && h.Role.Own() && q.Stat == "regeneration" {
		q.Mul *= h.Params().Float("multiplier")
		q.Add += h.Params().Int("amount")
	}
}

// wasting wears its bearer away: amount damage of type every every turns,
// said with message — an amulet that saps the life of whoever wears it.
// It is damage like any other: resistances count, and it can kill.
type wasting struct{}

func (wasting) Params() []rules.ParamDef {
	return []rules.ParamDef{
		{Name: "every", Kind: rules.ParamInt, Required: true, Min: rules.Bound(1)},
		{Name: "amount", Kind: rules.ParamInt, Default: 1, Min: rules.Bound(1)},
		{Name: "type", Kind: rules.ParamDamageType, Required: true},
		{Name: "message", Kind: rules.ParamString, Default: "Something saps your life."},
	}
}

func (wasting) Describe(h rules.Holder) (string, string, bool) { return "Wasting", "yes", true }

func (wasting) Tick(c *rules.Ctx, h rules.Holder) {
	p := h.Params()
	if c.World.Turn%p.Int("every") != 0 {
		return
	}
	bearer := h.Carrier
	if h.Owner != nil {
		bearer = h.Owner // worn: it is the wearer who wastes
	}
	out := c.Hurt(bearer, rules.DamageType(p.String("type")), p.Int("amount"))
	if out.Damage == 0 {
		return
	}
	msg := fmt.Sprintf("%s You lose %d HP.", p.String("message"), out.Damage)
	if out.Dead {
		msg = fmt.Sprintf("%s You lose %d HP and die.", p.String("message"), out.Damage)
	}
	c.Say(bearer, msg, "")
}
