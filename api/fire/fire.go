// Package fire computes burn damage and duration — a neutral, pure
// function of an entity's own Oiliness, the same one-way "no
// entity-specific knowledge" shape as action.RollDamage, throw, or
// slide.
package fire

import "map_api/action"

// TickDamage returns how much damage burning deals on a single turn.
// Every intelligent agent takes the same range for now — a future
// per-entity variation would only need to change this function.
func TickDamage() int {
	return action.RollDamage(2, 4)
}

// Duration returns how many turns an ignition burns for, given the
// target's current Oiliness: the sum of `oiliness` independent 1-3
// rolls, so Oiliness 1 gives a range of [1,3], Oiliness 2 gives [2,6],
// and so on.
func Duration(oiliness int) int {
	total := 0
	for i := 0; i < oiliness; i++ {
		total += action.RollDamage(1, 3)
	}
	return total
}
