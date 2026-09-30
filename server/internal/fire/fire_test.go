package fire

import "testing"

func TestTickDamageStaysWithinRange(t *testing.T) {
	for i := 0; i < 200; i++ {
		got := TickDamage()
		if got < 2 || got > 4 {
			t.Fatalf("TickDamage() = %d, want within [2,4]", got)
		}
	}
}

func TestDurationOfZeroOilinessIsAlwaysZero(t *testing.T) {
	for i := 0; i < 50; i++ {
		if got := Duration(0); got != 0 {
			t.Fatalf("Duration(0) = %d, want 0", got)
		}
	}
}

func TestDurationOfOneOilinessStaysWithinOneToThree(t *testing.T) {
	for i := 0; i < 200; i++ {
		got := Duration(1)
		if got < 1 || got > 3 {
			t.Fatalf("Duration(1) = %d, want within [1,3]", got)
		}
	}
}

func TestDurationOfTwoOilinessStaysWithinTwoToSixAndVaries(t *testing.T) {
	seen := map[int]bool{}
	for i := 0; i < 400; i++ {
		got := Duration(2)
		if got < 2 || got > 6 {
			t.Fatalf("Duration(2) = %d, want within [2,6]", got)
		}
		seen[got] = true
	}
	if len(seen) < 2 {
		t.Errorf("expected Duration(2) to vary across 400 calls, only ever saw %v", seen)
	}
}
