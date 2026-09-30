// Package slide computes where an intelligent agent ends up after
// stepping onto an oily floor cell — a neutral, pure-of-entity-knowledge
// function of the map, current objects, and direction of travel, the
// same one-way shape as action.RollDamage or throw.Distance.
package slide

import (
	"math/rand"

	"micromap/server/internal/mapdata"
	"micromap/server/internal/overlay"
)

// Resolve computes where an agent ends up after choosing to step onto
// an oily cell at (oilyX,oilyY), having approached it while moving in
// cardinal direction (dx,dy) — exactly one of dx,dy nonzero, matching
// how movement already works everywhere in this game. Rolls one of 4
// equally-likely (25% each) outcomes: stop in the oily cell; continue
// one more cell in the same direction; or slide to either of the two
// cells perpendicular to the direction of travel (a 90-degree rotation
// of dx,dy each way). If the rolled destination is blocked — a wall
// (m.CanMove) or another agent (occupies a cell in objects) — the agent
// bounces off it and lands in the oily cell instead, exactly as if
// "stop" had been rolled.
func Resolve(m mapdata.Map, objects []overlay.Object, oilyX, oilyY, dx, dy int) (x, y int) {
	offsets := [4][2]int{
		{0, 0},    // stop
		{dx, dy},  // continue straight
		{-dy, dx}, // rotate left
		{dy, -dx}, // rotate right
	}
	off := offsets[rand.Intn(4)]
	if off[0] == 0 && off[1] == 0 {
		return oilyX, oilyY
	}
	tx, ty := oilyX+off[0], oilyY+off[1]
	if !m.CanMove(oilyX, oilyY, tx, ty) {
		return oilyX, oilyY
	}
	for _, o := range objects {
		if o.X == tx && o.Y == ty {
			return oilyX, oilyY
		}
	}
	return tx, ty
}
