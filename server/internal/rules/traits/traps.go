package traits

// Traps. A trap is a feature entity lying on a floor cell — a template with
// category trap — carrying two traits: hidden, until it is found, and one
// effect trait from this file, which reacts when a creature steps onto its
// cell (rules.Enterer).
//
// Every effect goes through springs, which holds the rules all traps share:
// they never touch monsters, a hidden one always goes off and is found
// doing so, and a known one is usually stepped around.

import (
	"fmt"
	"math/rand"

	"micro/server/internal/rules"
)

// pitEscapePercent is the chance each try of climbing out of a pit.
const pitEscapePercent = 50

// trapParams are the parameters every trap takes, plus extra.
func trapParams(extra ...rules.ParamDef) []rules.ParamDef {
	return append([]rules.ParamDef{
		// The chance a player who knows the trap is there steps around it.
		{Name: "avoid_known", Kind: rules.ParamInt, Default: 75, Min: rules.Bound(0), Max: rules.Bound(100)},
	}, extra...)
}

// springs reports whether the trap carried by h goes off for whoever just
// stepped onto it:
//   - never for a monster: traps are for the player;
//   - always while hidden — and then it is hidden no more;
//   - once known, unless the player steps around it (avoid_known percent).
func springs(c *rules.Ctx, e *rules.Entering, h rules.Holder) bool {
	if !e.Who.IsPlayer() {
		return false
	}
	trap := h.Carrier
	if hidden := trap.FindTrait("hidden"); hidden != nil {
		trap.RemoveTrait(hidden)
		return true
	}
	if rand.Intn(100) < h.Params().Int("avoid_known") {
		c.Say(e.Who, fmt.Sprintf("You carefully avoid the %s.", trap.Name), "")
		return false
	}
	return true
}

// pit drops the player into a pit: bludgeoning damage from min up to
// per_level times the level's depth, so pits hurt more the deeper they
// are — and then they are stuck in it (see inPit).
type pit struct{}

func (pit) Params() []rules.ParamDef {
	return trapParams(
		rules.ParamDef{Name: "min", Kind: rules.ParamInt, Default: 1, Min: rules.Bound(0)},
		rules.ParamDef{Name: "per_level", Kind: rules.ParamInt, Default: 1, Min: rules.Bound(0)},
	)
}

func (pit) OnEnter(c *rules.Ctx, e *rules.Entering, h rules.Holder) {
	if !springs(c, e, h) {
		return
	}
	p := h.Params()
	lo := p.Int("min")
	hi := max(lo, c.World.Depth*p.Int("per_level"))
	out := c.Hurt(e.Who, rules.DamageBludgeoning, lo+rand.Intn(hi-lo+1))
	if out.Dead {
		c.Say(e.Who, fmt.Sprintf("You fall into a pit, take %d damage and die.", out.Damage), "")
		return
	}
	c.Say(e.Who, fmt.Sprintf("You fall into a pit and take %d damage!", out.Damage), "")
	e.Who.AddTrait(&rules.TraitInstance{Trait: "in_pit", Params: rules.Params{"x": e.X, "y": e.Y}})
}

// inPit is being stuck in the pit at (x, y). Each try at moving off the
// cell climbs out with pitEscapePercent chance, and otherwise slips back;
// the turn is spent either way. A neighbor to fight is fought as usual —
// rules.Step only asks a Leaver about an actual move. Leaving the cell by
// any other means (a teleport) ends it.
type inPit struct{}

func (inPit) Params() []rules.ParamDef {
	return []rules.ParamDef{
		{Name: "x", Kind: rules.ParamInt, Required: true},
		{Name: "y", Kind: rules.ParamInt, Required: true},
	}
}

func (inPit) Describe(h rules.Holder) (string, string, bool) { return "In a pit", "yes", true }

func (inPit) OnLeave(c *rules.Ctx, l *rules.Leaving, h rules.Holder) {
	if h.Role != rules.RoleSelf {
		return
	}
	if rand.Intn(100) < pitEscapePercent {
		h.Carrier.RemoveTrait(h.Inst)
		c.Say(l.Who, "You climb out of the pit.", "")
		return
	}
	l.Stay = true
	c.Say(l.Who, "You try to climb out of the pit, but slip back.", "")
}

func (inPit) Tick(c *rules.Ctx, h rules.Holder) {
	e := h.Carrier
	if e.X != h.Params().Int("x") || e.Y != h.Params().Int("y") {
		e.RemoveTrait(h.Inst)
	}
}

// dart shoots a dart at the player. It hits with hit_chance percent less
// their Dexterity (kept within 5-95), for damage piercing damage — rolled
// each time it fires — and poisons them if poison_damage is set (see
// Poison, which Stamina fights).
type dart struct{}

func (dart) Params() []rules.ParamDef {
	return trapParams(
		rules.ParamDef{Name: "hit_chance", Kind: rules.ParamInt, Required: true, Min: rules.Bound(0), Max: rules.Bound(100)},
		rules.ParamDef{Name: "damage", Kind: rules.ParamDice, Required: true, Min: rules.Bound(0)},
		rules.ParamDef{Name: "poison_damage", Kind: rules.ParamInt, Default: 0, Min: rules.Bound(0)},
		rules.ParamDef{Name: "poison_turns", Kind: rules.ParamInt, Default: 1, Min: rules.Bound(1)},
	)
}

// DartHitChance is the percent chance a dart with hitChance hits someone
// with dexterity: nimbler targets dodge more often, but never always.
func DartHitChance(hitChance, dexterity int) int {
	return min(max(hitChance-dexterity, 5), 95)
}

func (dart) OnEnter(c *rules.Ctx, e *rules.Entering, h rules.Holder) {
	if !springs(c, e, h) {
		return
	}
	p := h.Params()
	if rand.Intn(100) >= DartHitChance(p.Int("hit_chance"), e.Who.Attrs.Dexterity) {
		c.Say(e.Who, "A dart shoots out of the wall and misses you.", "")
		return
	}
	out := c.Hurt(e.Who, rules.DamagePiercing, p.Roll("damage"))
	c.Say(e.Who, fmt.Sprintf("A dart shoots out of the wall and hits you for %d damage!", out.Damage), "")
	if p.Int("poison_damage") > 0 {
		Poison(c, e.Who, p.Int("poison_damage"), p.Int("poison_turns"))
	}
}

// trapDoor drops the player to the level below. Changing level is the
// game's business, so it only asks (rules.Ctx.Descend). On the deepest
// level there is nowhere to fall, and it does nothing.
type trapDoor struct{}

func (trapDoor) Params() []rules.ParamDef { return trapParams() }

func (trapDoor) OnEnter(c *rules.Ctx, e *rules.Entering, h rules.Holder) {
	if c.World.Depth >= rules.DeepestLevel || !springs(c, e, h) {
		return
	}
	c.Say(e.Who, "A trap door opens beneath you!", "")
	c.Descend = true
}

// teleportTrap moves the player to a random free tile on the level.
type teleportTrap struct{}

func (teleportTrap) Params() []rules.ParamDef { return trapParams() }

func (teleportTrap) OnEnter(c *rules.Ctx, e *rules.Entering, h rules.Holder) {
	if !springs(c, e, h) {
		return
	}
	if x, y, ok := c.World.RandomUnoccupiedTile(); ok {
		e.Who.X, e.Who.Y = x, y
	}
	c.Say(e.Who, "There is a flash of light, and you are somewhere else!", "")
}
