// Package spawn defines the generic contract intelligent entities use to
// decide their own per-level spawn count — how strongly a monster favors
// a given dungeon level relative to its own native level, and how that
// weight turns into either a randomized count (common monsters) or a
// single-shot yes/no (rare ones). It has no dependency on any specific
// monster package: goblin/mummy each call these functions with their own
// tuning, exactly the way action.RollDamage is shared combat math no
// entity package owns.
package spawn

import (
	"math"
	"math/rand"
)

// MonsterWeight returns how strongly a monster favors dungeonLevel, given its
// own native level. 1.0 at dungeonLevel == monsterLevel; decays gently when
// the monster is easy for the floor (dungeonLevel > monsterLevel), decays
// sharply and then hard-cuts to 0 when the monster is too strong for the floor.
func MonsterWeight(monsterLevel, dungeonLevel int, sigmaUp, sigmaDown, maxAbove float64) float64 {
	diff := float64(dungeonLevel - monsterLevel)

	if diff >= 0 {
		return math.Exp(-diff / sigmaUp)
	}

	aboveBy := -diff
	if aboveBy > maxAbove {
		return 0
	}
	return math.Exp(-aboveBy / sigmaDown)
}

// GenericSpawnCount decides how many of this monster spawn on dungeonLevel.
// Called independently by each monster type — no knowledge of other monsters
// needed. baseDensity is the expected count on the monster's own native level
// (e.g. 3.0 -> average 3 goblins on a goblin-level floor). The Poisson draw
// keeps individual levels varied while the average tracks the weight curve.
func GenericSpawnCount(monsterLevel, dungeonLevel int, baseDensity, sigmaUp, sigmaDown, maxAbove float64, rng *rand.Rand) int {
	weight := MonsterWeight(monsterLevel, dungeonLevel, sigmaUp, sigmaDown, maxAbove)
	if weight <= 0 {
		return 0
	}
	return poisson(weight*baseDensity, rng)
}

// poisson draws from a Poisson distribution (Knuth's algorithm) — fine for
// the small lambda values (typically < 20) involved in spawn counts.
func poisson(lambda float64, rng *rand.Rand) int {
	if lambda <= 0 {
		return 0
	}
	l := math.Exp(-lambda)
	k, p := 0, 1.0
	for {
		k++
		p *= rng.Float64()
		if p <= l {
			return k - 1
		}
	}
}

// SingleShotShouldSpawn decides whether a single-shot monster spawns on
// dungeonLevel. alreadySpawned is state the caller owns and tracks across
// levels — this function is stateless otherwise.
//
//   - Never spawns before earliestLevel.
//   - Guaranteed at forceByLevel if it hasn't spawned yet (so bad RNG can
//     never skip it entirely).
//   - Otherwise a weighted coin flip, scaled by appearChanceScale.
func SingleShotShouldSpawn(
	monsterLevel, dungeonLevel, earliestLevel, forceByLevel int,
	appearChanceScale, sigmaUp, sigmaDown, maxAbove float64,
	alreadySpawned bool,
	rng *rand.Rand,
) bool {
	if alreadySpawned || dungeonLevel < earliestLevel {
		return false
	}
	if dungeonLevel >= forceByLevel {
		return true
	}

	weight := MonsterWeight(monsterLevel, dungeonLevel, sigmaUp, sigmaDown, maxAbove)
	if weight <= 0 {
		return false
	}

	chance := math.Min(1, weight*appearChanceScale)
	return rng.Float64() < chance
}
