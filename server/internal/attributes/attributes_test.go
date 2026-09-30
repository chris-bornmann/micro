package attributes

import (
	"math/rand"
	"testing"
)

func TestRollStaysWithinRange(t *testing.T) {
	r := Range{Min: 2, Max: 6}
	rng := rand.New(rand.NewSource(1))
	for i := 0; i < 200; i++ {
		got := r.Roll(rng)
		for name, v := range map[string]int{
			"Strength": got.Strength, "Intelligence": got.Intelligence,
			"Dexterity": got.Dexterity, "Stamina": got.Stamina,
		} {
			if v < r.Min || v > r.Max {
				t.Fatalf("iteration %d: expected %s in [%d,%d], got %d", i, name, r.Min, r.Max, v)
			}
		}
	}
}

func TestRollIsDeterministicForTheSameRNGSeed(t *testing.T) {
	r := Range{Min: 6, Max: 20}
	got1 := r.Roll(rand.New(rand.NewSource(42)))
	got2 := r.Roll(rand.New(rand.NewSource(42)))
	if got1 != got2 {
		t.Errorf("expected the same rng seed to roll the same attributes, got %+v and %+v", got1, got2)
	}
}

func TestRollDrawsAttributesIndependently(t *testing.T) {
	// A single-value range would make every field equal regardless of
	// independence; use a real range and confirm the four fields aren't
	// forced to move in lockstep across many rolls.
	r := Range{Min: 1, Max: 100}
	rng := rand.New(rand.NewSource(7))
	sawDifference := false
	for i := 0; i < 20; i++ {
		got := r.Roll(rng)
		if got.Strength != got.Intelligence || got.Strength != got.Dexterity || got.Strength != got.Stamina {
			sawDifference = true
			break
		}
	}
	if !sawDifference {
		t.Error("expected the four attributes to be rolled independently, but they were identical on every iteration")
	}
}
