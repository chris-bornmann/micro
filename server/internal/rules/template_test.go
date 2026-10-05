package rules_test

import (
	"math/rand"
	"testing"

	"micro/server/internal/attributes"
	"micro/server/internal/rules"
)

// TestATemplateTraitsDurationIsHowLongItLasts: what a template gives
// with a duration wears off after that many turns; without one, never.
func TestATemplateTraitsDurationIsHowLongItLasts(t *testing.T) {
	tmpl := &rules.Template{ID: "x", Category: rules.CategoryMonster, Traits: []rules.TraitSpec{
		{Trait: "poisoned", Params: rules.Params{"damage": 1}, Duration: 5},
		{Trait: "sneaking", Params: rules.Params{}},
	}}
	e := rules.New(tmpl, 0, 0, rand.New(rand.NewSource(1)))
	if e.Traits[0].Turns != 5 || e.Traits[1].Turns != 0 {
		t.Errorf("turns %d and %d, want 5 and 0 (for good)", e.Traits[0].Turns, e.Traits[1].Turns)
	}
}

// TestPerStatRangesRollEachStatWithinItsOwn: an exact stat is exact, a
// range stays in range.
func TestPerStatRangesRollEachStatWithinItsOwn(t *testing.T) {
	r := attributes.Ranges{
		Strength:     attributes.Range{Min: 18, Max: 18},
		Intelligence: attributes.Range{Min: 6, Max: 20},
		Dexterity:    attributes.Range{Min: 3, Max: 3},
		Stamina:      attributes.Range{Min: 1, Max: 2},
	}
	rng := rand.New(rand.NewSource(7))
	for i := 0; i < 100; i++ {
		a := r.Roll(rng)
		if a.Strength != 18 || a.Dexterity != 3 || a.Intelligence < 6 || a.Intelligence > 20 || a.Stamina < 1 || a.Stamina > 2 {
			t.Fatalf("rolled %+v", a)
		}
	}
}
