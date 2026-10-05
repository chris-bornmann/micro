package mapdata

// Walls inside rooms, and lines of sight across them.

// IsWall reports whether (x, y) is a wall standing inside a room.
func (m Map) IsWall(x, y int) bool { return m.Walls[Point{X: x, Y: y}] }

// Clear reports whether nothing blocks a line of sight from (x0, y0) to
// (x1, y1): no wall stands on the straight line between them (Bresenham's
// line), the two ends themselves aside. Only walls inside rooms block; it
// knows nothing of rooms' outer edges, and is meant for looking across a
// walled room (a maze).
func (m Map) Clear(x0, y0, x1, y1 int) bool {
	dx, dy := abs(x1-x0), -abs(y1-y0)
	sx, sy := 1, 1
	if x0 > x1 {
		sx = -1
	}
	if y0 > y1 {
		sy = -1
	}
	err := dx + dy
	x, y := x0, y0
	for x != x1 || y != y1 {
		if (x != x0 || y != y0) && m.IsWall(x, y) {
			return false
		}
		e2 := 2 * err
		if e2 >= dy {
			err += dy
			x += sx
		}
		if e2 <= dx {
			err += dx
			y += sy
		}
	}
	return true
}
