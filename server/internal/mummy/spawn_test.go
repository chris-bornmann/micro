package mummy

import (
	"math/rand"
	"testing"
)

func TestSpawnCountNeverExceedsOne(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	for level := 1; level <= 25; level++ {
		if got := SpawnCount(level, false, rng); got > 1 {
			t.Fatalf("expected SpawnCount to never exceed 1 (single-shot), got %d at level %d", got, level)
		}
	}
}

func TestSpawnCountNeverBeforeEarliestLevel(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	for level := 1; level < spawnEarliestLevel; level++ {
		if got := SpawnCount(level, false, rng); got != 0 {
			t.Errorf("expected 0 before earliestLevel %d, got %d at level %d", spawnEarliestLevel, got, level)
		}
	}
}

func TestSpawnCountGuaranteedByForceByLevelIfNotAlreadySpawned(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	if got := SpawnCount(spawnForceByLevel, false, rng); got != 1 {
		t.Errorf("expected a guaranteed spawn (1) at forceByLevel %d, got %d", spawnForceByLevel, got)
	}
}

func TestSpawnCountRespectsAlreadySpawned(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	if got := SpawnCount(spawnForceByLevel, true, rng); got != 0 {
		t.Errorf("expected 0 once alreadySpawned, even at forceByLevel, got %d", got)
	}
}
