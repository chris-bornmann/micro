package mapdata

import "testing"

func TestCanMoveThroughDoor(t *testing.T) {
	m := geometryTestMap()

	if !m.CanMove(9, 7, 10, 7) {
		t.Error("expected moving from the room into its door to be legal")
	}
	if m.CanMove(9, 8, 10, 8) {
		t.Error("expected moving sideways into a non-door corridor tile to be illegal")
	}
}

func TestNextStepFindsRouteThroughDoor(t *testing.T) {
	m := geometryTestMap()

	nx, ny, ok := m.NextStep(7, 7, 12, 7)
	if !ok {
		t.Fatal("expected a route from inside the room to the far end of the corridor")
	}
	if nx != 8 || ny != 7 {
		t.Errorf("NextStep(7,7 -> 12,7) = (%d,%d), want (8,7)", nx, ny)
	}
}

func TestNextStepNoRouteOnDisconnectedMap(t *testing.T) {
	roomA := Room{ID: 0, X: 5, Y: 5, W: 5, H: 5}
	roomB := Room{ID: 1, X: 20, Y: 5, W: 5, H: 5}
	m := Map{Width: 30, Height: 20, Rooms: []Room{roomA, roomB}} // no corridor at all

	if _, _, ok := m.NextStep(6, 6, 21, 6); ok {
		t.Error("expected no route between two rooms with no connecting corridor")
	}
}

func TestNextStepSamePointIsNotOK(t *testing.T) {
	m := geometryTestMap()
	if _, _, ok := m.NextStep(7, 7, 7, 7); ok {
		t.Error("expected NextStep to report no move needed when start equals target")
	}
}
