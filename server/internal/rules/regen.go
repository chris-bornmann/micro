package rules

import "fmt"

// Natural healing. Every creature, the player and monsters alike, slowly
// heals while it is alive and hurt. Healing builds up in points, hpPoints
// to the hit point, so HP come back one at a time:
//
//	natural = min(Strength + Stamina + regenBase, naturalCap)
//	rate    = (natural + Add) × Mul       — from traits; never below 0
//
// With Strength + Stamina at 25 — a typical player — that is 30 points a
// turn: 1 HP every 20 turns. Attributes alone never heal faster than
// naturalCap allows, 1 HP every 10 turns; magic (a ring of regeneration)
// may. Nothing is random, so the same creature always heals on the same
// turns.
//
// Traits take part through the "regeneration" stat query, folded over the
// creature's own traits and everything it wears or wields — so a trait
// counts the same in a monster's template, from a potion, or on a ring:
// the regeneration trait multiplies or adds to it, and poison stops it.
const (
	hpPoints   = 600
	regenBase  = 5
	naturalCap = 60
)

// NaturalRegeneration is how many healing points a turn e's attributes
// alone give it.
func NaturalRegeneration(e *Entity) int {
	return min(e.Attrs.Strength+e.Attrs.Stamina+regenBase, naturalCap)
}

// Regeneration is how many healing points a turn e gains: its natural
// rate, as its own and its gear's traits change it. Never below 0.
func (reg *Registry) Regeneration(e *Entity) int {
	q := &Query{Kind: QueryStat, Stat: "regeneration", Subject: e, Base: NaturalRegeneration(e), Mul: 1}
	fold(q, reg.holders(ownParticipants(e)))
	return max(q.result(), 0)
}

// regenerate is one turn's healing for e: its rate added to the points
// it has built up, and a hit point back for every hpPoints. A creature at
// full health builds nothing up — there is no banking healing for later.
func (c *Ctx) regenerate(e *Entity) {
	if e.Dead || !e.IsCreature() {
		return
	}
	if e.HP >= e.MaxHP {
		e.Healing = 0
		return
	}
	e.Healing += c.Reg.Regeneration(e)
	for e.Healing >= hpPoints && e.HP < e.MaxHP {
		e.HP++
		e.Healing -= hpPoints
	}
}

// healingRate describes e's healing for its status: "1 HP / 20 turns",
// or "none".
func (reg *Registry) healingRate(e *Entity) string {
	rate := reg.Regeneration(e)
	if rate == 0 {
		return "none"
	}
	return fmt.Sprintf("1 HP / %d turns", (hpPoints+rate-1)/rate)
}
