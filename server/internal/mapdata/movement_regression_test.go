package mapdata

import "testing"

// sharedDoorTestMap reproduces the exact geometry shape that caused a real
// bug: two different corridors coincidentally ending at the identical door
// tile for the same room (corridor A: room1 -> room2 via door (10,10);
// corridor B: room1 -> room3, also via door (10,10)). A point one step
// away from that shared door — but not itself a door — can end up with the
// same BFS distance-to-target as a legitimate neighbor, without there
// being any real edge to it. NextStep must never be fooled by that into
// stepping through a wall.
func sharedDoorTestMap() Map {
	room1 := Room{ID: 1, X: 5, Y: 5, W: 5, H: 5} // interior 5-9
	room2 := Room{ID: 2, X: 15, Y: 8, W: 5, H: 5}
	room3 := Room{ID: 3, X: 15, Y: 12, W: 5, H: 5}

	corridorA := Corridor{
		ID: 0, RoomA: 1, RoomB: 2,
		DoorA: Door{X: 10, Y: 10}, DoorB: Door{X: 14, Y: 10},
		Path: []Point{{X: 10, Y: 10}, {X: 11, Y: 10}, {X: 12, Y: 10}, {X: 13, Y: 10}, {X: 14, Y: 10}},
	}
	// corridorB shares corridorA's exact door tile (10,10) for room1, then
	// immediately runs parallel one row below before diverging — this is
	// the shape that put a non-door corridor tile at the same
	// distance-to-target as the legitimate door.
	corridorB := Corridor{
		ID: 1, RoomA: 1, RoomB: 3,
		DoorA: Door{X: 10, Y: 10}, DoorB: Door{X: 14, Y: 13},
		Path: []Point{
			{X: 10, Y: 10}, {X: 10, Y: 11}, {X: 10, Y: 12}, {X: 10, Y: 13},
			{X: 11, Y: 13}, {X: 12, Y: 13}, {X: 13, Y: 13}, {X: 14, Y: 13},
		},
	}

	return Map{
		Width: 25, Height: 25,
		Rooms:     []Room{room1, room2, room3},
		Corridors: []Corridor{corridorA, corridorB},
	}
}

func TestNextStepNeverStepsThroughAWallWithSharedDoorCoordinate(t *testing.T) {
	m := sharedDoorTestMap()

	var pts []Point
	for _, r := range m.Rooms {
		for y := r.Y; y < r.Y+r.H; y++ {
			for x := r.X; x < r.X+r.W; x++ {
				pts = append(pts, Point{X: x, Y: y})
			}
		}
	}
	for _, c := range m.Corridors {
		pts = append(pts, c.Path...)
	}

	for _, s := range pts {
		for _, tg := range pts {
			if s == tg {
				continue
			}
			nx, ny, ok := m.NextStep(s.X, s.Y, tg.X, tg.Y)
			if !ok {
				continue
			}
			if !m.CanMove(s.X, s.Y, nx, ny) {
				t.Fatalf("NextStep(%v -> %v) returned illegal step (%d,%d): CanMove(%v -> %d,%d) is false",
					s, tg, nx, ny, s, nx, ny)
			}
		}
	}
}
