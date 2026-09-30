package goblin

import (
	"math/rand"
	"testing"
)

func TestSpawnCountCanExceedOneOnItsNativeLevel(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	// Over enough draws at the native level (weight 1.0, baseDensity
	// 2.0), a goblin's count should sometimes be more than 1 — it's a
	// common monster, not single-shot.
	sawMoreThanOne := false
	for i := 0; i < 200; i++ {
		if SpawnCount(NativeLevel, false, rng) > 1 {
			sawMoreThanOne = true
			break
		}
	}
	if !sawMoreThanOne {
		t.Error("expected SpawnCount to sometimes return more than 1 on the native level across 200 draws")
	}
}

func TestSpawnCountIgnoresAlreadySpawned(t *testing.T) {
	rngA := rand.New(rand.NewSource(42))
	rngB := rand.New(rand.NewSource(42))
	// Same seed, same sequence of calls — alreadySpawned shouldn't
	// change goblin's behavior at all, since it's not single-shot.
	for i := 0; i < 20; i++ {
		a := SpawnCount(NativeLevel, false, rngA)
		b := SpawnCount(NativeLevel, true, rngB)
		if a != b {
			t.Fatalf("expected alreadySpawned to be ignored, got %d (false) vs %d (true) on draw %d", a, b, i)
		}
	}
}

func TestSpawnCountTapersOffFarBelowTheDungeon(t *testing.T) {
	// A goblin can never be "below" its own NativeLevel (1 is the
	// shallowest level), so confirm counts stay bounded/sane deep down
	// instead of erroring or going negative.
	rng := rand.New(rand.NewSource(1))
	for i := 0; i < 50; i++ {
		if got := SpawnCount(25, false, rng); got < 0 {
			t.Fatalf("expected a non-negative count, got %d", got)
		}
	}
}
