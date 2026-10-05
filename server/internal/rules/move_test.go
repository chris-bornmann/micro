package rules_test

import (
	"testing"

	"micro/server/internal/mapdata"
	"micro/server/internal/mapgen"
	"micro/server/internal/rules"
	"micro/server/internal/rules/rulestest"
)

// TestStepCanWalkEveryGeneratedCorridor walks the full path of every
// corridor across many generated maps, from the middle of its first room,
// through its door and every path point, into the middle of its second
// room, one step at a time. It was written for a real bug — a corridor
// doubling back through its own room's rectangle just past the door, which
// made a mid-corridor step look like an illegal room exit and left the
// player stuck — and is kept as a general guard on corridor walkability.
func TestStepCanWalkEveryGeneratedCorridor(t *testing.T) {
	if testing.Short() {
		t.Skip("seed sweep")
	}
	eng := rulestest.Engine(t)

	for seed := int64(1); seed <= 60; seed++ {
		cfg := mapgen.DefaultConfig()
		cfg.Seed = seed
		cfg.NumRooms = 15
		cfg.ExtraCorridorPercent = 40
		m, err := mapgen.Generate(cfg)
		if err != nil {
			t.Fatalf("seed %d: Generate: %v", seed, err)
		}
		roomByID := map[int]mapdata.Room{}
		for _, r := range m.Rooms {
			roomByID[r.ID] = r
		}

		for _, c := range m.Corridors {
			a, b := roomByID[c.RoomA], roomByID[c.RoomB]
			p := rulestest.New(t, eng, "player", a.CenterX(), a.CenterY(), 15)
			ctx := eng.NewCtx(&rules.World{Map: m, Player: p})

			steps := append([]mapdata.Point{{X: c.DoorA.X, Y: c.DoorA.Y}}, c.Path...)
			steps = append(steps, mapdata.Point{X: b.CenterX(), Y: b.CenterY()})
			for _, target := range steps {
				for p.X != target.X || p.Y != target.Y {
					dx, dy := 0, 0
					switch {
					case target.X > p.X:
						dx = 1
					case target.X < p.X:
						dx = -1
					case target.Y > p.Y:
						dy = 1
					default:
						dy = -1
					}
					x, y := p.X, p.Y
					if !ctx.Step(p, x+dx, y+dy).Moved {
						t.Fatalf("seed %d, corridor %d (room %d -> room %d): stuck at (%d,%d) heading for (%d,%d)",
							seed, c.ID, c.RoomA, c.RoomB, x, y, target.X, target.Y)
					}
				}
			}
		}
	}
}
