package rules

// Tick advances e by one turn: every trait instance on e and on its gear is
// visited once. A Ticker's own Tick runs; any other instance with Turns
// left counts down, and at zero is removed and its Expirer, if any, told.
//
// Countdowns start the turn an instance is applied, because the game ticks
// at the end of every turn, including the one in which a potion was drunk —
// unless the instance waits for a later turn (see TraitInstance.From).
//
// Then e heals naturally, if it is still alive and hurt (see regenerate).
func (c *Ctx) Tick(e *Entity) {
	defer c.regenerate(e)
	for _, ht := range c.Reg.holders(ownParticipants(e)) {
		if ht.h.Inst.From > c.World.Turn {
			continue
		}
		if t, ok := ht.t.(Ticker); ok {
			t.Tick(c, ht.h)
			continue
		}
		inst := ht.h.Inst
		if inst.Turns <= 0 {
			continue
		}
		inst.Turns--
		if inst.Turns > 0 {
			continue
		}
		ht.h.Carrier.RemoveTrait(inst)
		if x, ok := ht.t.(Expirer); ok {
			x.Expired(c, ht.h)
		}
	}
}

// Destroy removes item from its owner entirely, wherever it is carried —
// a torch that burns out.
func (c *Ctx) Destroy(owner, item *Entity) {
	if owner != nil {
		owner.release(item)
	}
}
