// Package attributes defines the neutral shape every intelligent
// entity's core stats share. Nothing consumes these yet — Strength,
// Intelligence, Dexterity, and Stamina are only initialized for now;
// future systems (attack damage, scroll comprehension, ...) will read
// them without needing to understand which entity package produced
// them, the same way action.Result is a neutral contract every attacker
// already produces.
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
// caller's own already-seeded RNG (see player.Init/goblin.spawn), so
// attribute rolls stay reproducible from the map seed exactly like
// spawn position and HP already are.
func (r Range) Roll(rng *rand.Rand) Attributes {
	roll := func() int { return r.Min + rng.Intn(r.Max-r.Min+1) }
	return Attributes{Strength: roll(), Intelligence: roll(), Dexterity: roll(), Stamina: roll()}
}
