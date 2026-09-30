package action

import "testing"

func TestRollDamageStaysInRange(t *testing.T) {
	for i := 0; i < 200; i++ {
		got := RollDamage(3, 7)
		if got < 3 || got > 7 {
			t.Fatalf("RollDamage(3,7) = %d, want in [3,7]", got)
		}
	}
}

func TestRollDamageWithEqualMinMax(t *testing.T) {
	for i := 0; i < 20; i++ {
		if got := RollDamage(4, 4); got != 4 {
			t.Fatalf("RollDamage(4,4) = %d, want 4", got)
		}
	}
}

func TestRollDamageClampsWhenMaxLessThanMin(t *testing.T) {
	for i := 0; i < 20; i++ {
		if got := RollDamage(5, 2); got != 5 {
			t.Fatalf("RollDamage(5,2) = %d, want 5 (clamped)", got)
		}
	}
}
