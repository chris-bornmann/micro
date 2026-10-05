package rules

import (
	"micro/server/internal/mapdata"
	"micro/server/internal/overlay"
	"micro/server/internal/slide"
)

// StepResult is what an attempted step did.
type StepResult struct {
	// Moved is whether actor's position changed.
	Moved bool
	// Target is set when the destination was occupied by an enemy: actor
	// attacks it instead of moving.
	Target *Entity
	// Swapped is set when actor traded places with a fleeing monster.
	Swapped *Entity
}

// Step moves actor onto (nx, ny) if the map allows the move. No two live
// creatures ever share a tile, so what happens when one stands there
// depends on who they are:
//   - The player and a monster, either way round: actor does not move,
//     and Target reports the other, for the caller to attack.
//   - Two monsters: actor is blocked and waits — monsters never fight
//     each other — with one exception. A monster that is not fleeing may
//     swap places with one that is, so a monster on the run does not
//     jam a corridor for everyone behind it.
//
// Stepping onto an oily tile may slide actor on (see slide.Resolve), never
// onto another creature. A swap does not slide: both just trade tiles.
//
// Before actor moves, its own traits may hold it where it is (see Leaver);
// once it has moved, whatever lies on its new cell may react (see Enterer).
func (c *Ctx) Step(actor *Entity, nx, ny int) StepResult {
	if !c.World.Map.CanMove(actor.X, actor.Y, nx, ny) {
		return StepResult{}
	}
	if o := c.World.CreatureAt(nx, ny); o != nil && o != actor {
		switch {
		case actor.IsPlayer() || o.IsPlayer():
			return StepResult{Target: o}
		case !actor.Mind.Fleeing && o.Mind.Fleeing:
			if c.held(actor) {
				return StepResult{}
			}
			o.X, o.Y, actor.X, actor.Y = actor.X, actor.Y, nx, ny
			c.entered(actor)
			return StepResult{Moved: true, Swapped: o}
		default:
			return StepResult{}
		}
	}
	if c.held(actor) {
		return StepResult{}
	}
	fx, fy := nx, ny
	if c.World.IsOily(nx, ny) {
		var objs []overlay.Object
		for _, o := range c.World.creatures() {
			if o != actor {
				objs = append(objs, overlay.Object{ID: o.ID, X: o.X, Y: o.Y})
			}
		}
		fx, fy = slide.Resolve(*c.World.Map, objs, nx, ny, nx-actor.X, ny-actor.Y)
	}
	actor.X, actor.Y = fx, fy
	c.entered(actor)
	return StepResult{Moved: true}
}

// maxDetour is how many steps longer than the direct route StepToward
// will walk to get around whoever stands in the way: enough to circle a
// crowd to the far side of its target, never a trek round by another
// corridor.
const maxDetour = 8

// StepToward is the first step self should take toward (x, y): along the
// shortest route that goes around every creature Step would not let it
// through, if one exists within maxDetour extra steps. Otherwise it is the
// direct route's step, as if nobody were there — self queues behind
// whoever blocks it rather than wandering off. ok is false when no route
// exists at all, or self is already there.
func (c *Ctx) StepToward(self *Entity, x, y int) (int, int, bool) {
	m := c.World.Map
	direct, n, ok := m.Route(self.X, self.Y, x, y, nil)
	if !ok {
		return 0, 0, false
	}
	blocking := map[mapdata.Point]bool{}
	for _, o := range c.World.creatures() {
		if o != self && !(!self.Mind.Fleeing && o.Mind.Fleeing && !o.IsPlayer()) {
			blocking[mapdata.Point{X: o.X, Y: o.Y}] = true
		}
	}
	blocked := func(x, y int) bool { return blocking[mapdata.Point{X: x, Y: y}] }
	if around, k, ok := m.Route(self.X, self.Y, x, y, blocked); ok && k <= n+maxDetour {
		return around.X, around.Y, true
	}
	return direct.X, direct.Y, true
}

// Leaving is what a Leaver is told: who is trying to move off its cell.
// Setting Stay keeps it there.
type Leaving struct {
	Who  *Entity
	Stay bool
}

// held asks actor's own traits whether it may move off its cell.
func (c *Ctx) held(actor *Entity) bool {
	l := &Leaving{Who: actor}
	for _, ht := range c.Reg.holders(ownParticipants(actor)) {
		if lv, ok := ht.t.(Leaver); ok {
			lv.OnLeave(c, l, ht.h)
			if l.Stay {
				return true
			}
		}
	}
	return false
}

// Entering is what an Enterer is told: who has just stepped onto the cell.
type Entering struct {
	Who  *Entity
	X, Y int
}

// entered lets everything on the floor where actor now stands react.
func (c *Ctx) entered(actor *Entity) {
	e := &Entering{Who: actor, X: actor.X, Y: actor.Y}
	var ps []participant
	for _, f := range c.World.Floor {
		if f.X == e.X && f.Y == e.Y {
			ps = append(ps, participant{f, nil, RoleSelf})
		}
	}
	for _, ht := range c.Reg.holders(ps) {
		if en, ok := ht.t.(Enterer); ok {
			en.OnEnter(c, e, ht.h)
		}
	}
}

// MonsterTurn gives m its turn: its AI picks a tile, and m steps toward it
// (see Step), attacking the player instead if the player is in the way.
// attacked reports whether an attack happened, and out its outcome.
func (c *Ctx) MonsterTurn(m *Entity) (out Outcome, attacked bool) {
	ai, ok := c.Reg.AI(m.T.AI.Behavior)
	if !ok {
		return Outcome{}, false
	}
	x, y, ok := ai.Act(c, m, m.T.AI.Params)
	if !ok {
		return Outcome{}, false
	}
	res := c.Step(m, x, y)
	if res.Target == nil {
		return Outcome{}, false
	}
	return c.Melee(m, res.Target), true
}
