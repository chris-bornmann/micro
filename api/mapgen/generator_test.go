package mapgen

import (
	"testing"

	"map_api/mapdata"
)

// TestGenerateRoomsAllConnected verifies the room graph produced by Generate
// is fully connected: every room must be reachable from room 0 by walking
// Room.CorridorIDs / Map.Corridors, since gameplay (and pathfinding) depends
// on there being no isolated rooms.
func TestGenerateRoomsAllConnected(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Seed = 1234
	cfg.NumRooms = 20

	m, err := Generate(cfg)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(m.Rooms) < 2 {
		t.Fatalf("expected at least 2 rooms to be placed, got %d", len(m.Rooms))
	}

	corridorByID := make(map[int]mapdata.Corridor, len(m.Corridors))
	for _, c := range m.Corridors {
		corridorByID[c.ID] = c
	}
	roomByID := make(map[int]mapdata.Room, len(m.Rooms))
	for _, r := range m.Rooms {
		roomByID[r.ID] = r
	}

	visited := map[int]bool{m.Rooms[0].ID: true}
	queue := []int{m.Rooms[0].ID}
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		for _, corrID := range roomByID[id].CorridorIDs {
			c := corridorByID[corrID]
			next := c.RoomA
			if next == id {
				next = c.RoomB
			}
			if !visited[next] {
				visited[next] = true
				queue = append(queue, next)
			}
		}
	}

	for _, r := range m.Rooms {
		if !visited[r.ID] {
			t.Errorf("room %d is not reachable from room %d", r.ID, m.Rooms[0].ID)
		}
	}
}

// TestGenerateCorridorsLeaveWallBuffer verifies that a generated corridor
// never touches a room's interior or its 1-tile wall ring, except exactly
// at that corridor's own two doors — i.e. there's always at least one wall
// tile between a corridor and a room it isn't entering through. Checked
// across a range of room counts/densities, since tightly-packed rooms and
// third-party rooms sitting between a corridor's two endpoints are exactly
// the cases this guarantee has to hold up under.
func TestGenerateCorridorsLeaveWallBuffer(t *testing.T) {
	if testing.Short() {
		t.Skip("seed sweep")
	}

	configs := []struct {
		numRooms             int
		extraCorridorPercent float64
	}{
		{numRooms: 6, extraCorridorPercent: 0},
		{numRooms: 10, extraCorridorPercent: 20},
		{numRooms: 15, extraCorridorPercent: 40},
		{numRooms: 25, extraCorridorPercent: 60},
	}

	for _, tc := range configs {
		for seed := int64(1); seed <= 50; seed++ {
			cfg := DefaultConfig()
			cfg.Seed = seed
			cfg.NumRooms = tc.numRooms
			cfg.ExtraCorridorPercent = tc.extraCorridorPercent

			m, err := Generate(cfg)
			if err != nil {
				t.Fatalf("seed %d, rooms %d: Generate: %v", seed, tc.numRooms, err)
			}

			for _, c := range m.Corridors {
				doorA := mapdata.Point{X: c.DoorA.X, Y: c.DoorA.Y}
				doorB := mapdata.Point{X: c.DoorB.X, Y: c.DoorB.Y}
				for _, p := range c.Path {
					for _, r := range m.Rooms {
						allowed := (r.ID == c.RoomA && p == doorA) || (r.ID == c.RoomB && p == doorB)
						if allowed {
							continue
						}
						if p.X >= r.X-1 && p.X <= r.X+r.W && p.Y >= r.Y-1 && p.Y <= r.Y+r.H {
							t.Fatalf("seed %d, rooms %d: corridor %d (room %d<->%d) touches room %d's wall/interior at (%d,%d) without a door there",
								seed, tc.numRooms, c.ID, c.RoomA, c.RoomB, r.ID, p.X, p.Y)
						}
					}
				}
			}
		}
	}
}
