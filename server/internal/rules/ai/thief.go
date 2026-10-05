package ai

// Options for a thief, or anyone who wants to be one: sidle up, steal, run,
// and slip away once out of sight. Attacked, it decides each time whether
// to fight back or flee.

import (
	"math/rand"

	"micro/server/internal/rules"
)

const (
	scoreDecide = 3.0 // retaliate: settle fight-or-flight before anything else
	scoreVanish = 2.0 // escape: out of sight is the moment to go
	scoreSteal  = 1.0
)

func registerThiefOptions(reg *rules.Registry) {
	reg.AddOption("steal", steal{})
	reg.AddOption("escape", escape{})
	reg.AddOption("retaliate", retaliate{})
}

// steal takes something from the player when next to them and not hostile:
// chance percent each turn, one random item from their pack. Equipped
// things are safe. Nothing is said — the item is simply gone. Afterward the
// thief is afraid, and runs.
type steal struct{}

func (steal) Params() []rules.ParamDef {
	return []rules.ParamDef{{Name: "chance", Kind: rules.ParamInt, Default: 50, Min: rules.Bound(0), Max: rules.Bound(100)}}
}

func (steal) Score(c *rules.Ctx, self *rules.Entity, _ rules.Params) float64 {
	p := c.World.Player
	if self.Mind.Attitude == rules.Neutral && len(p.Inventory) > 0 && nextTo(c, self, p) && c.Sees(self, p) {
		return scoreSteal
	}
	return 0
}

func (steal) Act(c *rules.Ctx, self *rules.Entity, p rules.Params) (int, int, bool) {
	if rand.Intn(100) >= p.Int("chance") {
		return 0, 0, false // waits for a better moment
	}
	pl := c.World.Player
	i := rand.Intn(len(pl.Inventory))
	item := pl.Inventory[i]
	pl.Inventory = append(pl.Inventory[:i], pl.Inventory[i+1:]...)
	self.Inventory = append(self.Inventory, item)
	self.Mind.Add("stolen", 1)
	self.Mind.Attitude = rules.Afraid
	return 0, 0, false
}

// escape leaves the level for good — this one, anyway — when afraid and
// out of the player's sight, taking everything along. A unique character
// may turn up again deeper (see Template.ReturnChance).
type escape struct{}

func (escape) Params() []rules.ParamDef { return nil }

func (escape) Score(c *rules.Ctx, self *rules.Entity, _ rules.Params) float64 {
	if self.Mind.Attitude == rules.Afraid && !c.World.PlayerSees(self.X, self.Y) {
		return scoreVanish
	}
	return 0
}

func (escape) Act(c *rules.Ctx, self *rules.Entity, _ rules.Params) (int, int, bool) {
	c.Depart(self)
	return 0, 0, false
}

// retaliate decides, each time the player attacks, whether to fight back:
// a neutral creature with fight_chance percent, an afraid one — a thief
// running off with your things — with afraid_fight_chance. Fighting makes
// it hostile, and it strikes at once if it can; otherwise it is afraid,
// and runs. One hurt enough that its own flee option would run never
// stands and fights. Once hostile there is nothing left to decide.
type retaliate struct{}

func (retaliate) Params() []rules.ParamDef {
	return []rules.ParamDef{
		{Name: "fight_chance", Kind: rules.ParamInt, Default: 50, Min: rules.Bound(0), Max: rules.Bound(100)},
		{Name: "afraid_fight_chance", Kind: rules.ParamInt, Default: 0, Min: rules.Bound(0), Max: rules.Bound(100)},
	}
}

func (retaliate) Score(c *rules.Ctx, self *rules.Entity, _ rules.Params) float64 {
	m := self.Mind
	if m.Attitude != rules.Hostile && m.Provoked() && m.AttackedTurn == c.World.Turn {
		return scoreDecide
	}
	return 0
}

func (retaliate) Act(c *rules.Ctx, self *rules.Entity, p rules.Params) (int, int, bool) {
	chance := p.Int("fight_chance")
	if self.Mind.Attitude == rules.Afraid {
		chance = p.Int("afraid_fight_chance")
	}
	fleeing := fleeParams(self)
	if hpPercent(self) >= fleeing.Int("below_hp_percent") && rand.Intn(100) < chance {
		self.Mind.Attitude = rules.Hostile
		if pl := c.World.Player; nextTo(c, self, pl) {
			return pl.X, pl.Y, true // and swings at once
		}
		return 0, 0, false
	}
	self.Mind.Attitude = rules.Afraid
	return flee{}.Act(c, self, fleeing)
}

// fleeParams is how self's own flee option is set up, if it has one.
func fleeParams(self *rules.Entity) rules.Params {
	for _, spec := range self.T.Brain {
		if spec.Option == "flee" {
			return spec.Params
		}
	}
	return rules.Params{}
}
