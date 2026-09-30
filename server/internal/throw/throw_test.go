package throw

import "testing"

func TestDistanceClampsToOneForAHeavyWeakThrow(t *testing.T) {
	if got := Distance(50, 6); got != 1 {
		t.Errorf("expected distance 1 for a heavy object and a weak thrower, got %d", got)
	}
}

func TestDistanceClampsToEightForALightThrow(t *testing.T) {
	if got := Distance(1, 6); got != 8 {
		t.Errorf("expected distance 8 for a very light object, got %d", got)
	}
	if got := Distance(1, 20); got != 8 {
		t.Errorf("expected distance 8 for a very light object thrown by a strong thrower, got %d", got)
	}
}

func TestDistanceInteriorValuesMatchTheFormula(t *testing.T) {
	cases := []struct {
		weight, strength, want int
	}{
		{weight: 6, strength: 6, want: 6},
		{weight: 3, strength: 10, want: 7},
		{weight: 50, strength: 20, want: 3},
		{weight: 100, strength: 20, want: 2},
	}
	for _, c := range cases {
		if got := Distance(c.weight, c.strength); got != c.want {
			t.Errorf("Distance(%d, %d) = %d, want %d", c.weight, c.strength, got, c.want)
		}
	}
}

func TestDistanceStaysWithinOneAndEight(t *testing.T) {
	for weight := 1; weight <= 200; weight += 7 {
		for strength := 1; strength <= 30; strength += 3 {
			got := Distance(weight, strength)
			if got < 1 || got > 8 {
				t.Fatalf("Distance(%d, %d) = %d, expected within [1,8]", weight, strength, got)
			}
		}
	}
}

func TestDamageMinIsAlwaysOne(t *testing.T) {
	cases := []struct{ weight, strength int }{
		{0, 0}, {1, 0}, {6, 15}, {50, 20}, {100, 100},
	}
	for _, c := range cases {
		min, _ := Damage(c.weight, c.strength)
		if min != 1 {
			t.Errorf("Damage(%d, %d) min = %d, want 1", c.weight, c.strength, min)
		}
	}
}

func TestDamageMaxCombinesWeightAndStrengthClampedToEight(t *testing.T) {
	cases := []struct {
		weight, strength, want int
	}{
		{weight: 0, strength: 0, want: 1},     // ceil(0/10)=0, floor(0/10)=0, clamped up to 1
		{weight: 1, strength: 0, want: 1},     // ceil(1/10)=1
		{weight: 10, strength: 0, want: 1},    // ceil(10/10)=1
		{weight: 11, strength: 0, want: 2},    // ceil(11/10)=2
		{weight: 20, strength: 0, want: 2},    // ceil(20/10)=2
		{weight: 21, strength: 0, want: 3},    // ceil(21/10)=3
		{weight: 1, strength: 9, want: 1},     // floor(9/10)=0, no bonus yet
		{weight: 1, strength: 10, want: 2},    // floor(10/10)=1, base 1 + bonus 1
		{weight: 50, strength: 20, want: 7},   // base 5 + bonus 2
		{weight: 100, strength: 100, want: 8}, // base 10 + bonus 10, clamped down to 8
	}
	for _, c := range cases {
		_, max := Damage(c.weight, c.strength)
		if max != c.want {
			t.Errorf("Damage(%d, %d) max = %d, want %d", c.weight, c.strength, max, c.want)
		}
	}
}
