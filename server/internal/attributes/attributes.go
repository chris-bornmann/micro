// Package attributes defines the core stats every creature has. Strength
// feeds melee damage and throwing; the others are rolled but not yet
// read by anything.
package attributes

import "math/rand"

// Attributes are the four core stats every intelligent entity has.
type Attributes struct {
	Strength     int `json:"strength"`
	Intelligence int `json:"intelligence"`
	Dexterity    int `json:"dexterity"`
	Stamina      int `json:"stamina"`
}

// Range is an entity kind's [Min,Max] bounds for every attribute roll —
// a different Range per kind (Player, Goblin, ...); each entity package
// owns its own, the same way each already owns its own private HP
// range.
type Range struct {
	Min, Max int
}

// Roll returns a fresh Attributes with each of the four rolled
// independently and uniformly within [r.Min, r.Max] using rng — the
// caller's own already-seeded RNG (see rules.New), so
// attribute rolls stay reproducible from the map seed exactly like
// spawn position and HP already are.
func (r Range) Roll(rng *rand.Rand) Attributes { return Same(r).Roll(rng) }

// Ranges are an entity kind's bounds for each attribute separately — a
// hand-crafted player with Strength 18 exactly and Dexterity anywhere in
// 6-20, say.
type Ranges struct {
	Strength, Intelligence, Dexterity, Stamina Range
}

// Same is r for all four attributes.
func Same(r Range) Ranges { return Ranges{r, r, r, r} }

// Roll rolls each attribute within its own range, in a fixed order —
// Strength, Intelligence, Dexterity, Stamina — so Same(r).Roll draws
// exactly what r.Roll always has from the same rng.
func (r Ranges) Roll(rng *rand.Rand) Attributes {
	roll := func(r Range) int { return r.Min + rng.Intn(r.Max-r.Min+1) }
	return Attributes{
		Strength:     roll(r.Strength),
		Intelligence: roll(r.Intelligence),
		Dexterity:    roll(r.Dexterity),
		Stamina:      roll(r.Stamina),
	}
}
