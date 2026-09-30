package slide

import (
	"testing"

	"micro/server/internal/mapdata"
	"micro/server/internal/overlay"
)

// openRoomMap is a single large room with no obstructions — every one of
// an oily cell's 4 candidate destinations (stop, forward, and both
// perpendiculars) is walkable.
func openRoomMap() mapdata.Map {
	room := mapdata.Room{ID: 0, X: 0, Y: 0, W: 20, H: 20}
	return mapdata.Map{Width: 20, Height: 20, Rooms: []mapdata.Room{room}}
}

// corridorMap is a single 1-wide east-west corridor with no rooms at
// all — a cell's only walkable neighbors are the ones directly ahead of
// and behind it on the corridor's own path, so a perpendicular slide is
// always blocked, exactly like the corridor example in the request.
func corridorMap() mapdata.Map {
	var path []mapdata.Point
	for x := 5; x <= 15; x++ {
		path = append(path, mapdata.Point{X: x, Y: 7})
	}
	corridor := mapdata.Corridor{ID: 0, RoomA: -1, RoomB: -1, Path: path}
	return mapdata.Map{Width: 20, Height: 20, Corridors: []mapdata.Corridor{corridor}}
}

// deadEndCorridorMap is corridorMap but the oily cell sits at the very
// last cell of the corridor, so continuing straight ahead also runs off
// the end — all 3 non-stop destinations are blocked.
func deadEndCorridorMap() mapdata.Map {
	var path []mapdata.Point
	for x := 5; x <= 10; x++ {
		path = append(path, mapdata.Point{X: x, Y: 7})
	}
	corridor := mapdata.Corridor{ID: 0, RoomA: -1, RoomB: -1, Path: path}
	return mapdata.Map{Width: 20, Height: 20, Corridors: []mapdata.Corridor{corridor}}
}

func TestResolveInOpenGroundVisitsAllFourCandidates(t *testing.T) {
	m := openRoomMap()
	seen := map[[2]int]bool{}
	for i := 0; i < 400; i++ {
		x, y := Resolve(m, nil, 10, 10, 1, 0)
		seen[[2]int{x, y}] = true
		switch {
		case x == 10 && y == 10: // stop
		case x == 11 && y == 10: // forward
		case x == 10 && y == 9: // perpendicular
		case x == 10 && y == 11: // perpendicular
		default:
			t.Fatalf("unexpected destination (%d,%d)", x, y)
		}
	}
	if len(seen) != 4 {
		t.Errorf("expected all 4 candidate destinations observed across 400 iterations, got %d: %v", len(seen), seen)
	}
}

func TestResolveInCorridorOnlyStopsOrContinuesStraight(t *testing.T) {
	m := corridorMap()
	sawStop, sawForward := false, false
	for i := 0; i < 400; i++ {
		x, y := Resolve(m, nil, 10, 7, 1, 0)
		switch {
		case x == 10 && y == 7:
			sawStop = true
		case x == 11 && y == 7:
			sawForward = true
		default:
			t.Fatalf("expected only the oily cell (10,7) or the forward cell (11,7), got (%d,%d)", x, y)
		}
	}
	if !sawStop {
		t.Error("expected the oily cell to be reached at least once across 400 iterations")
	}
	if !sawForward {
		t.Error("expected the forward cell to be reached at least once across 400 iterations")
	}
}

func TestResolveInDeadEndAlwaysStops(t *testing.T) {
	m := deadEndCorridorMap()
	for i := 0; i < 200; i++ {
		x, y := Resolve(m, nil, 10, 7, 1, 0)
		if x != 10 || y != 7 {
			t.Fatalf("expected every result to bounce back to the oily cell (10,7) at a dead end, got (%d,%d)", x, y)
		}
	}
}

func TestResolveBouncesOffAnOccupiedForwardCell(t *testing.T) {
	m := openRoomMap()
	objects := []overlay.Object{{ID: "goblin:1", X: 11, Y: 10}} // sits exactly on the forward cell
	for i := 0; i < 400; i++ {
		x, y := Resolve(m, objects, 10, 10, 1, 0)
		if x == 11 && y == 10 {
			t.Fatalf("expected the mover to never land on the occupied forward cell, got (%d,%d)", x, y)
		}
	}
}
