package ai

// The utility AI. A creature with it has a menu of options in its template
// (brain:). Each turn every option says how much it wants to act — its
// score, times its weight — and the most eager one acts, ties going to the
// one listed first. A creature whose options all score 0 waits.
//
// Base scores put the options in a sensible order before any weights:
//
//	drink_to_heal 1.5 · read_to_escape 1.4 · flee 1.3 · attack 1.0
//	approach 0.5 · wander 0.1 · wait 0.01
//
// so, unweighted, a creature first saves itself when it is hurt enough
// for those options to apply, then fights what's in front of it, closes in
// on what it's after, and otherwise ambles about.
// Weights in the template retune a character from there.

import (
	"math/rand"

	"micro/server/internal/mapdata"
	"micro/server/internal/rules"
)

const (
	scoreDrink    = 1.5
	scoreEscape   = 1.4
	scoreFlee     = 1.3
	scoreAttack   = 1.0
	scoreApproach = 0.5
	scoreWander   = 0.1
	scoreWait     = 0.01
)

// utility is the AI behavior that runs a creature's brain.
type utility struct{}

func (utility) Params() []rules.ParamDef { return nil }

func (utility) Act(c *rules.Ctx, self *rules.Entity, _ rules.Params) (int, int, bool) {
	c.Notice(self)
	self.Mind.Fleeing = false // only while flee is the choice (see Step's swap rule)

	var best rules.Option
	var bestSpec rules.OptionSpec
	bestScore := 0.0
	for _, spec := range self.T.Brain {
		o, ok := c.Reg.Option(spec.Option)
		if !ok {
			continue
		}
		if s := o.Score(c, self, spec.Params) * spec.Weight; s > bestScore {
			best, bestSpec, bestScore = o, spec, s
		}
	}
	if best == nil {
		self.Mind.FleeRoom = -1
		return 0, 0, false
	}
	x, y, step := best.Act(c, self, bestSpec.Params)
	if !self.Mind.Fleeing {
		self.Mind.FleeRoom = -1 // stopped running: next time, a fresh destination
	}
	return x, y, step
}

// registerOptions adds every built-in option.
func registerOptions(reg *rules.Registry) {
	reg.AddOption("attack", attack{})
	reg.AddOption("approach", approach{})
	reg.AddOption("flee", flee{})
	reg.AddOption("drink_to_heal", useWhenHurt{action: "heal", score: scoreDrink})
	reg.AddOption("read_to_escape", useWhenHurt{action: "teleport", score: scoreEscape})
	reg.AddOption("wander", wander{})
	reg.AddOption("wait", wait{})
}

// --- helpers ---

// hpPercent is how much of its health e has left.
func hpPercent(e *rules.Entity) int {
	if e.MaxHP <= 0 {
		return 100
	}
	return e.HP * 100 / e.MaxHP
}

// nextTo reports whether a could step straight onto b's tile.
func nextTo(c *rules.Ctx, a, b *rules.Entity) bool {
	dx, dy := a.X-b.X, a.Y-b.Y
	return dx*dx+dy*dy == 1 && c.World.Map.CanMove(a.X, a.Y, b.X, b.Y)
}

// freeSteps are the tiles self could step onto without bumping into
// anyone.
func freeSteps(c *rules.Ctx, self *rules.Entity) [][2]int {
	var out [][2]int
	for _, d := range [][2]int{{0, -1}, {0, 1}, {-1, 0}, {1, 0}} {
		x, y := self.X+d[0], self.Y+d[1]
		if c.World.Map.CanMove(self.X, self.Y, x, y) && c.World.CreatureAt(x, y) == nil {
			out = append(out, [2]int{x, y})
		}
	}
	return out
}

// carrying returns the first item self carries whose use includes the
// named action. Options look items up by what they do, never by name.
func carrying(self *rules.Entity, action string) *rules.Entity {
	for _, it := range self.Inventory {
		for _, u := range it.Uses {
			if u.Action == action {
				return it
			}
		}
	}
	return nil
}

func hostile(self *rules.Entity) bool { return self.Mind.Attitude == rules.Hostile }

// --- options ---

// attack fights the player when hostile and right next to them.
type attack struct{}

func (attack) Params() []rules.ParamDef { return nil }

func (attack) Score(c *rules.Ctx, self *rules.Entity, _ rules.Params) float64 {
	p := c.World.Player
	if hostile(self) && c.Sees(self, p) && nextTo(c, self, p) {
		return scoreAttack
	}
	return 0
}

func (attack) Act(c *rules.Ctx, self *rules.Entity, _ rules.Params) (int, int, bool) {
	return c.World.Player.X, c.World.Player.Y, true
}

// approach moves toward the player: hostile, toward where they were last
// seen; otherwise only with until_adjacent, to sidle up beside them
// without attacking — and never onto them.
type approach struct{}

func (approach) Params() []rules.ParamDef {
	return []rules.ParamDef{{Name: "until_adjacent", Kind: rules.ParamBool, Default: false}}
}

func (approach) Score(c *rules.Ctx, self *rules.Entity, p rules.Params) float64 {
	pl := c.World.Player
	switch {
	case hostile(self) && self.Mind.Seen && !(self.X == self.Mind.LastSeen.X && self.Y == self.Mind.LastSeen.Y):
		return scoreApproach
	case !hostile(self) && self.Mind.Attitude != rules.Afraid && p.Bool("until_adjacent") && c.Sees(self, pl) && !nextTo(c, self, pl):
		return scoreApproach
	}
	return 0
}

func (approach) Act(c *rules.Ctx, self *rules.Entity, _ rules.Params) (int, int, bool) {
	to := self.Mind.LastSeen
	x, y, ok := c.StepToward(self, to.X, to.Y)
	if !ok || (!hostile(self) && c.World.CreatureAt(x, y) != nil) {
		return 0, 0, false
	}
	return x, y, true
}

// flee runs from the player when hurt below below_hp_percent, or — with
// when_afraid — whenever afraid. Running because it is hurt makes it
// afraid: it is running for its life, and once out of sight, a creature
// that can escape will.
//
// It runs for the exit: to a spot in some room other than the one the
// player is in, by the shortest way around anyone in the way, choosing
// afresh on arrival or when the player follows it into that room. With
// nowhere to go, or only past the player, it backs off to whichever free
// neighboring tile is farthest from them.
type flee struct{}

func (flee) Params() []rules.ParamDef {
	return []rules.ParamDef{
		{Name: "below_hp_percent", Kind: rules.ParamInt, Default: 0, Min: rules.Bound(0), Max: rules.Bound(100)},
		{Name: "when_afraid", Kind: rules.ParamBool, Default: false},
	}
}

func (flee) Score(c *rules.Ctx, self *rules.Entity, p rules.Params) float64 {
	hurt := hpPercent(self) < p.Int("below_hp_percent")
	afraid := p.Bool("when_afraid") && self.Mind.Attitude == rules.Afraid
	if (hurt || afraid) && self.Mind.Seen {
		return scoreFlee
	}
	return 0
}

func (flee) Act(c *rules.Ctx, self *rules.Entity, p rules.Params) (int, int, bool) {
	m := &self.Mind
	m.Fleeing = true
	if hpPercent(self) < p.Int("below_hp_percent") {
		m.Attitude = rules.Afraid
	}
	from := m.LastSeen
	if x, y, ok := runForTheExit(c, self, from); ok {
		return x, y, true
	}
	dist := func(x, y int) int { return abs(x-from.X) + abs(y-from.Y) }
	best, bestDist := [2]int{}, dist(self.X, self.Y)
	found := false
	for _, s := range freeSteps(c, self) {
		if d := dist(s[0], s[1]); d > bestDist {
			best, bestDist, found = s, d, true
		}
	}
	return best[0], best[1], found
}

// runForTheExit is flee's first choice: the next step toward a spot in a
// room the player (last seen at from) is not in. ok is false with no such
// room, no way there, or only a way straight through the player.
func runForTheExit(c *rules.Ctx, self *rules.Entity, from mapdata.Point) (int, int, bool) {
	m, level := &self.Mind, c.World.Map
	playerRoom, inRoom := level.RoomAt(from.X, from.Y)
	arrived := self.X == m.FleeX && self.Y == m.FleeY
	if m.FleeRoom == -1 || arrived || (inRoom && playerRoom.ID == m.FleeRoom) {
		room, x, y, ok := pickFleeCell(level, from.X, from.Y)
		if !ok {
			m.FleeRoom = -1
			return 0, 0, false
		}
		m.FleeRoom, m.FleeX, m.FleeY = room.ID, x, y
	}
	x, y, ok := c.StepToward(self, m.FleeX, m.FleeY)
	if !ok || (x == from.X && y == from.Y) || c.World.CreatureAt(x, y) == c.World.Player {
		return 0, 0, false
	}
	return x, y, true
}

// useWhenHurt uses a carried item that does action — a healing potion, a
// teleportation scroll — when HP is below below_hp_percent.
type useWhenHurt struct {
	action string
	score  float64
}

func (useWhenHurt) Params() []rules.ParamDef {
	return []rules.ParamDef{{Name: "below_hp_percent", Kind: rules.ParamInt, Default: 40, Min: rules.Bound(0), Max: rules.Bound(100)}}
}

func (u useWhenHurt) Score(c *rules.Ctx, self *rules.Entity, p rules.Params) float64 {
	if hpPercent(self) < p.Int("below_hp_percent") && carrying(self, u.action) != nil {
		return u.score
	}
	return 0
}

func (u useWhenHurt) Act(c *rules.Ctx, self *rules.Entity, _ rules.Params) (int, int, bool) {
	if it := carrying(self, u.action); it != nil {
		c.Use(self, it)
	}
	return 0, 0, false
}

// wander ambles to a random free neighboring tile.
type wander struct{}

func (wander) Params() []rules.ParamDef { return nil }

func (wander) Score(*rules.Ctx, *rules.Entity, rules.Params) float64 { return scoreWander }

func (wander) Act(c *rules.Ctx, self *rules.Entity, _ rules.Params) (int, int, bool) {
	steps := freeSteps(c, self)
	if len(steps) == 0 {
		return 0, 0, false
	}
	s := steps[rand.Intn(len(steps))]
	return s[0], s[1], true
}

// wait does nothing, which is sometimes best.
type wait struct{}

func (wait) Params() []rules.ParamDef { return nil }

func (wait) Score(*rules.Ctx, *rules.Entity, rules.Params) float64 { return scoreWait }

func (wait) Act(*rules.Ctx, *rules.Entity, rules.Params) (int, int, bool) { return 0, 0, false }

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
