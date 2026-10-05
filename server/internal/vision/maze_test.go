package vision

import (
	"testing"

	"micro/server/internal/mapdata"
)

func TestATorchInAMazeReachesThreeTilesAndNotPastWalls(t *testing.T) {
	walls := map[mapdata.Point]bool{}
	for y := 1; y <= 4; y++ {
		walls[mapdata.Point{X: 5, Y: y}] = true
	}
	m := mapdata.Map{Width: 18, Height: 8, Rooms: []mapdata.Room{{ID: 0, X: 1, Y: 1, W: 15, H: 5, Dark: true, Walled: true}}, Walls: walls}
	lit := map[mapdata.Point]bool{}
	tiles, _, _ := CurrentlyLit(m, 3, 2, true)
	for _, p := range tiles {
		lit[p] = true
	}
	switch {
	case !lit[mapdata.Point{X: 1, Y: 5}] || !lit[mapdata.Point{X: 5, Y: 2}]:
		t.Error("within three tiles in plain view, including the wall itself, should be lit")
	case lit[mapdata.Point{X: 6, Y: 2}]:
		t.Error("lit through a wall")
	case len(tiles) >= m.Rooms[0].W*m.Rooms[0].H:
		t.Error("a torch should not light a whole maze")
	}
}
