package rules_test

import (
	"testing"

	"micro/server/internal/mapdata"
	"micro/server/internal/rules"
	"micro/server/internal/rules/rulestest"
)

// mazeRoom is one 15x5 walled room (interior x 1-15, y 1-5), with a wall
// down x=5 from y=1 to y=4.
func mazeRoom() *mapdata.Map {
	walls := map[mapdata.Point]bool{}
	for y := 1; y <= 4; y++ {
		walls[mapdata.Point{X: 5, Y: y}] = true
	}
	return &mapdata.Map{Width: 18, Height: 8, Rooms: []mapdata.Room{{ID: 0, X: 1, Y: 1, W: 15, H: 5, Dark: true, Walled: true}}, Walls: walls}
}

func TestInAMazeSightStopsAtWallsAndDistance(t *testing.T) {
	eng := rulestest.Engine(t)
	p := rulestest.New(t, eng, "player", 3, 2, 10)
	g := rulestest.New(t, eng, "goblin", 7, 2, 10)
	c := eng.NewCtx(&rules.World{Map: mazeRoom(), Player: p, Monsters: []*rules.Entity{g}})
	if c.Sees(g, p) {
		t.Error("seen through a wall")
	}
	p.X, p.Y, g.X, g.Y = 3, 5, 7, 5
	if !c.Sees(g, p) {
		t.Error("four tiles along a clear passage should be seen")
	}
	g.X = 12
	if c.Sees(g, p) {
		t.Error("nine tiles away is out of sight in a maze")
	}
	c.World.Map = rulestest.Room()
	p.X, p.Y, g.X, g.Y = 5, 5, 14, 14
	if !c.Sees(g, p) {
		t.Error("an ordinary room is still seen across")
	}
}
