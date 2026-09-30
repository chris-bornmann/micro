package mapdata

// isWalkable reports whether (x,y) is an open room, door, or corridor cell:
// inside any room's rectangle, or a point on any corridor's path (which
// includes that corridor's two door endpoints). Coordinates outside every
// room and corridor — including out-of-map coordinates — are unwalkable.
func (m Map) isWalkable(x, y int) bool {
	if _, ok := m.RoomAt(x, y); ok {
		return true
	}
	_, ok := m.CorridorAt(x, y)
	return ok
}

// CanMove reports whether an object can move from (curX,curY) to
// (newX,newY). The destination must be walkable, and — a room and a
// corridor are only connected at their actual doors, so a corridor running
// alongside a room's wall must not let anything step sideways through it —
// the *first* step off a room onto a corridor (or off a corridor into a
// room) is only allowed exactly at the door connecting them.
//
// Generated corridors are routed to stay off every room's wall ring except
// at their own two doors (see mapgen), so a corridor tile and a room tile
// shouldn't coincide in practice. The "once already on a corridor, any
// further step on a corridor is allowed" rule below is kept anyway as a
// defensive fallback — it's a harmless general safety net regardless of
// generator behavior, and door-crossing legality still isn't compromised
// by it, since it only ever loosens movement between two tiles that are
// already both walkable.
func (m Map) CanMove(curX, curY, newX, newY int) bool {
	if !m.isWalkable(newX, newY) {
		return false
	}

	destRoom, destInRoom := m.RoomAt(newX, newY)
	curRoom, curInRoom := m.RoomAt(curX, curY)
	_, curInCorr := m.CorridorAt(curX, curY)
	_, destInCorr := m.CorridorAt(newX, newY)

	switch {
	case destInRoom && curInRoom && destRoom.ID == curRoom.ID:
		// Moving within the same room.
		return true
	case curInCorr && destInCorr:
		// Already on a corridor and staying on (a) corridor.
		return true
	case destInRoom && curInCorr:
		// First step off a corridor into a room: only through that room's door.
		return m.isDoorway(curX, curY, destRoom.ID)
	case !destInRoom && curInRoom:
		// First step off a room onto a corridor: only through that room's door.
		return m.isDoorway(newX, newY, curRoom.ID)
	default:
		return true
	}
}

// isDoorway reports whether (x,y) is a door connecting some corridor to the
// room roomID — i.e. that corridor's DoorA sits at (x,y) and its RoomA is
// roomID, or symmetrically for DoorB/RoomB.
func (m Map) isDoorway(x, y int, roomID int) bool {
	for _, c := range m.Corridors {
		if c.RoomA == roomID && c.DoorA.X == x && c.DoorA.Y == y {
			return true
		}
		if c.RoomB == roomID && c.DoorB.X == x && c.DoorB.Y == y {
			return true
		}
	}
	return false
}

// point is an internal grid coordinate used only for pathfinding bookkeeping
// (distinct from Point, which is part of the public Corridor.Path shape).
type point struct{ X, Y int }

var stepDirs = []point{{X: 0, Y: -1}, {X: 0, Y: 1}, {X: -1, Y: 0}, {X: 1, Y: 0}}

// NextStep returns the next tile to move to when heading from (fromX,fromY)
// toward (toX,toY) via the shortest legal route (using CanMove for edge
// legality, so the route always respects room/corridor door-crossing
// rules). Ties between equally-short routes are broken by preferring the
// axis with the larger remaining distance — a "most direct" chase — then
// horizontal over vertical. ok is false if there's no legal route, or the
// two points already coincide.
func (m Map) NextStep(fromX, fromY, toX, toY int) (nx, ny int, ok bool) {
	if fromX == toX && fromY == toY {
		return 0, 0, false
	}

	// BFS outward from the target along reverse edges, so every reachable
	// point ends up knowing its true shortest number of *forward* steps to
	// the target. cur is already known to be n steps from the target; next
	// belongs at n+1 only if next can actually step into cur directly —
	// CanMove(next, cur), not CanMove(cur, next) — since a legal move in
	// one direction between two points is not guaranteed legal in reverse
	// (e.g. two different corridors can coincidentally share the exact
	// same door tile for the same room, without being interchangeable).
	target := point{X: toX, Y: toY}
	dist := map[point]int{target: 0}
	queue := []point{target}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, d := range stepDirs {
			next := point{X: cur.X + d.X, Y: cur.Y + d.Y}
			if _, seen := dist[next]; seen {
				continue
			}
			if !m.CanMove(next.X, next.Y, cur.X, cur.Y) {
				continue
			}
			dist[next] = dist[cur] + 1
			queue = append(queue, next)
		}
	}

	from := point{X: fromX, Y: fromY}
	fromDist, reachable := dist[from]
	if !reachable {
		return 0, 0, false
	}

	// Even with the corrected BFS direction, only trust a neighbor whose
	// distance value fits *and* that's independently confirmed legal to
	// step into right now — two points can share a distance value without
	// there being a direct legal edge between them.
	var candidates []point
	for _, d := range stepDirs {
		next := point{X: fromX + d.X, Y: fromY + d.Y}
		if nd, ok := dist[next]; ok && nd == fromDist-1 && m.CanMove(fromX, fromY, next.X, next.Y) {
			candidates = append(candidates, next)
		}
	}
	if len(candidates) == 0 {
		return 0, 0, false
	}

	preferX := abs(toX-fromX) >= abs(toY-fromY)
	best := candidates[0]
	for _, c := range candidates {
		if preferX && c.Y == fromY {
			best = c
			break
		}
		if !preferX && c.X == fromX {
			best = c
			break
		}
	}
	return best.X, best.Y, true
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
