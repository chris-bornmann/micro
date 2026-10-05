package mapdata

import "testing"

// walledRoom is one 9x5 room (interior x 1-9, y 1-5) with a wall down
// x=5 from y=1 to y=4: a gap at the bottom, (5,5).
func walledRoom() Map {
	walls := map[Point]bool{}
	for y := 1; y <= 4; y++ {
		walls[Point{X: 5, Y: y}] = true
	}
	return Map{Width: 12, Height: 8, Rooms: []Room{{ID: 0, X: 1, Y: 1, W: 9, H: 5, Walled: true}}, Walls: walls}
}

func TestWallsCannotBeWalkedOnOrAround(t *testing.T) {
	m := walledRoom()
	if m.CanMove(4, 2, 5, 2) {
		t.Error("walked into a wall")
	}
	// From (4,2) to (6,2): the only way is down round the gap at (5,5).
	_, n, ok := m.Route(4, 2, 6, 2, nil)
	if !ok || n != 8 {
		t.Errorf("route round the wall: %d steps, ok %v; want 8", n, ok)
	}
	for _, p := range m.WalkableTiles() {
		if m.IsWall(p.X, p.Y) {
			t.Fatalf("%v is a wall, not walkable", p)
		}
	}
}

func TestClearIsBlockedByAWall(t *testing.T) {
	m := walledRoom()
	if !m.Clear(1, 1, 4, 4) || !m.Clear(2, 3, 2, 3) {
		t.Error("nothing between should be clear")
	}
	if m.Clear(3, 2, 7, 2) {
		t.Error("a wall stands between")
	}
	if !m.Clear(3, 5, 7, 5) {
		t.Error("the gap below the wall is clear")
	}
}
