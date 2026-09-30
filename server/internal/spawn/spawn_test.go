package spawn

import (
	"math"
	"math/rand"
	"testing"
)

func TestMonsterWeightIsOneAtNativeLevel(t *testing.T) {
	got := MonsterWeight(5, 5, 6, 2, 4)
	if math.Abs(got-1.0) > 1e-9 {
		t.Errorf("expected weight 1.0 at dungeonLevel==monsterLevel, got %v", got)
	}
}

func TestMonsterWeightDecaysGentlyAboveNativeLevel(t *testing.T) {
	near := MonsterWeight(1, 2, 6, 2, 4)
	far := MonsterWeight(1, 10, 6, 2, 4)
	if !(1.0 > near && near > far && far > 0) {
		t.Errorf("expected weight to decay monotonically as dungeonLevel rises above monsterLevel, got near=%v far=%v", near, far)
	}
}

func TestMonsterWeightHardCutsPastMaxAbove(t *testing.T) {
	got := MonsterWeight(10, 1, 6, 2, 4) // 9 levels above native, maxAbove=4
	if got != 0 {
		t.Errorf("expected 0 weight when aboveBy > maxAbove, got %v", got)
	}
}

func TestMonsterWeightDecaysSharplyBelowNativeLevel(t *testing.T) {
	justBelow := MonsterWeight(5, 4, 6, 2, 4)    // 1 level below native
	fartherBelow := MonsterWeight(5, 2, 6, 2, 4) // 3 levels below native
	if !(1.0 > justBelow && justBelow > fartherBelow && fartherBelow > 0) {
		t.Errorf("expected weight to decay as dungeonLevel drops below monsterLevel, got justBelow=%v fartherBelow=%v", justBelow, fartherBelow)
	}
}

func TestGenericSpawnCountAveragesTowardWeightTimesBaseDensity(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	const trials = 20000
	const baseDensity = 3.0
	total := 0
	for i := 0; i < trials; i++ {
		total += GenericSpawnCount(1, 1, baseDensity, 6, 2, 4, rng)
	}
	mean := float64(total) / float64(trials)
	if math.Abs(mean-baseDensity) > 0.1 {
		t.Errorf("expected sample mean near %v (weight 1.0 at native level), got %v", baseDensity, mean)
	}
}

func TestGenericSpawnCountIsZeroWhenWeightIsZero(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	got := GenericSpawnCount(10, 1, 5.0, 6, 2, 4, rng) // 9 above native, past maxAbove
	if got != 0 {
		t.Errorf("expected 0 when weight is 0, got %d", got)
	}
}

func TestGenericSpawnCountIsZeroWhenBaseDensityIsZero(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	for i := 0; i < 100; i++ {
		if got := GenericSpawnCount(1, 1, 0, 6, 2, 4, rng); got != 0 {
			t.Fatalf("expected 0 with baseDensity 0, got %d", got)
		}
	}
}

func TestSingleShotShouldSpawnNeverBeforeEarliestLevel(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	for level := 1; level < 3; level++ {
		if SingleShotShouldSpawn(5, level, 3, 8, 0.6, 8, 2, 4, false, rng) {
			t.Errorf("expected no spawn before earliestLevel 3, got true at level %d", level)
		}
	}
}

func TestSingleShotShouldSpawnAlwaysTrueAtForceByLevel(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	if !SingleShotShouldSpawn(5, 8, 3, 8, 0.6, 8, 2, 4, false, rng) {
		t.Error("expected guaranteed spawn at forceByLevel when not already spawned")
	}
}

func TestSingleShotShouldSpawnNeverTrueOnceAlreadySpawned(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	if SingleShotShouldSpawn(5, 8, 3, 8, 0.6, 8, 2, 4, true, rng) {
		t.Error("expected no further spawn once alreadySpawned is true, even past forceByLevel")
	}
}
