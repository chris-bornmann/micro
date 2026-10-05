// Package throw computes how far a thrown object travels and how much
// damage it deals if it hits someone — a pure function of the object's
// weight and the thrower's Strength. The rules engine uses it for every
// throw (see rules.Ctx.Throw).
package throw

import "math"

// ScalingFactor tunes how easy it is to throw things: a larger
// ScalingFactor makes it easier to throw things, while a smaller
// ScalingFactor makes it harder to throw things.
const ScalingFactor = 2

// Distance returns how many cells an object of the given weight travels
// when thrown by someone with the given strength.
func Distance(weight, strength int) int {
	// This formula finds a value between 1 and 8 based on the weight of
	// an object and the thrower's strength. A larger SCALING_FACTOR
	// makes it easier to throw things, while a smaller SCALING_FACTOR
	// makes it harder to throw things.
	distance := 1 + 7*math.Exp(-(float64(weight-1))/(float64(strength)*ScalingFactor))
	return clamp(int(math.Round(distance)), 1, 8)
}

// Damage returns the [min,max] damage range a thrown object deals if it
// hits an intelligent agent, combining the object's own weight with the
// thrower's strength BEFORE rolling — unlike a melee attack, where the
// same kind of strength bonus is instead added AFTER the roll (see
// rules.Ctx.Melee). Applied
// uniformly regardless of item category (weapon, potion, ...), since not
// every thrown item has its own combat stats.
//
// The object's own weight contributes ceil(weight/10): 1-10 is worth 1,
// 11-20 is worth 2, and so on. The thrower's strength contributes
// floor(strength/10) on top of that: 1-9 adds 0, 10-19 adds 1, and so
// on. The sum is clamped to 8, and min is always 1 — so the actual
// damage dealt is a random value in [1, that clamped sum].
func Damage(weight, strength int) (min, max int) {
	base := (weight + 9) / 10 // ceil(weight/10) for non-negative weight
	bonus := strength / 10    // floor(strength/10) for non-negative strength
	return 1, clamp(base+bonus, 1, 8)
}

func clamp(value, lo, hi int) int {
	return min(max(value, lo), hi)
}
