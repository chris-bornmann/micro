package player

import (
	"encoding/json"
	"testing"

	"micro/server/internal/mapdata"
	"micro/server/internal/mapgen"
	"micro/server/internal/overlay"
	"micro/server/internal/world"
)

// TestActionCanWalkEveryGeneratedCorridor programmatically walks the full
// path of every corridor across many generated maps, from inside its
// RoomA, through every Path point, into RoomB, pressing the exact
// direction key needed at each step. Originally written to guard against a
// real bug where a corridor's path could double back through its own
// origin room's rectangle right after the door, causing canMove to
// misidentify a mid-corridor step as an illegal room exit and get the
// player stuck; corridor routing (mapgen) now keeps every corridor off its
// rooms' wall rings except at their own doors, so that specific overlap
// can no longer happen — this test is kept as a general regression guard
// for corridor walkability.
func TestActionCanWalkEveryGeneratedCorridor(t *testing.T) {
	if testing.Short() {
		t.Skip("seed sweep")
	}

	keyFor := func(dx, dy int) string {
		switch {
		case dx == 1:
			return "Right"
		case dx == -1:
			return "Left"
		case dy == 1:
			return "Down"
		case dy == -1:
			return "Up"
		}
		return ""
	}

	move := func(m *mapdata.Map, p overlay.Object, key string) overlay.Object {
		self, err := json.Marshal(person{Object: p, HP: 15})
		if err != nil {
			t.Fatalf("marshal self: %v", err)
		}
		data, err := json.Marshal(world.AugmentedMap{Map: *m})
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		out, err := Action(self, data, key)
		if err != nil {
			t.Fatalf("Action: %v", err)
		}
		var resp actionResponse
		if err := json.Unmarshal(out, &resp); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		return resp.Self.Object
	}

	for seed := int64(1); seed <= 60; seed++ {
		cfg := mapgen.DefaultConfig()
		cfg.Seed = seed
		cfg.NumRooms = 15
		cfg.ExtraCorridorPercent = 40
		m, err := mapgen.Generate(cfg)
		if err != nil {
			t.Fatalf("seed %d: Generate: %v", seed, err)
		}

		roomByID := make(map[int]mapdata.Room, len(m.Rooms))
		for _, r := range m.Rooms {
			roomByID[r.ID] = r
		}

		for _, c := range m.Corridors {
			roomA, roomB := roomByID[c.RoomA], roomByID[c.RoomB]
			p := overlay.Object{ID: "player:1", X: roomA.CenterX(), Y: roomA.CenterY()}

			steps := append([]mapdata.Point{{X: c.DoorA.X, Y: c.DoorA.Y}}, c.Path...)
			steps = append(steps, mapdata.Point{X: roomB.CenterX(), Y: roomB.CenterY()})

			for _, target := range steps {
				for p.X != target.X || p.Y != target.Y {
					dx, dy := 0, 0
					if p.X != target.X {
						if target.X > p.X {
							dx = 1
						} else {
							dx = -1
						}
					} else if target.Y > p.Y {
						dy = 1
					} else {
						dy = -1
					}

					before := p
					p = move(m, p, keyFor(dx, dy))
					if p == before {
						t.Fatalf("seed %d, corridor %d (room %d -> room %d): stuck at (%d,%d) moving toward (%d,%d)",
							seed, c.ID, c.RoomA, c.RoomB, before.X, before.Y, target.X, target.Y)
					}
				}
			}
		}
	}
}
