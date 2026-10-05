// Package levels builds special levels — hand-drawn ones, and mazes —
// into the same map every other level is: rooms, corridors and doors,
// plus walls inside a room where a maze needs them. Nothing downstream
// knows a level was special.
package levels

import (
	"fmt"

	"micro/server/internal/mapdata"
	"micro/server/internal/mapgen"
	"micro/server/internal/rules"
)

// Layout is a special level, built: its map, what is placed where, and
// whether the level is also populated at random like any other.
type Layout struct {
	Map    *mapdata.Map
	Placed []Placement
	// Populate is what is also spawned at random, by depth, as on an
	// ordinary level: everything in a maze; in a drawn level, whatever
	// its spec asks for — nothing, unless it says.
	Populate rules.Populate
}

// Placement is one thing a layout puts on its map: the template with ID,
// at (X, Y).
type Placement struct {
	ID   string
	X, Y int
}

// Build makes spec's level. cfg gives a generated level its size and
// seed; a hand-drawn level is its own size.
func Build(spec *rules.LevelSpec, cfg mapgen.Config) (*Layout, error) {
	switch {
	case spec.Map != "":
		l, err := Parse(spec.Map, spec.Legend)
		if err != nil {
			return nil, fmt.Errorf("level %s: %w", spec.Name, err)
		}
		l.Map.Seed = cfg.Seed
		l.Populate = spec.Populate
		return l, nil
	case spec.Generator == "maze":
		mz, err := mapgen.GenerateMaze(cfg, spec.Braid)
		if err != nil {
			return nil, fmt.Errorf("level %s: %w", spec.Name, err)
		}
		return &Layout{
			Map:      mz.Map,
			Placed:   []Placement{{"stairs_up", mz.Up.X, mz.Up.Y}, {"stairs_down", mz.Down.X, mz.Down.Y}},
			Populate: spec.Populate,
		}, nil
	}
	return nil, fmt.Errorf("level %s: neither a map nor a known generator", spec.Name)
}
