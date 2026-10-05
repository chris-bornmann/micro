// Package mapgen generates levels: it holds Config and the procedural
// Generate algorithm. The map's data types and pure query/movement
// functions live in mapdata, a neutral shared library — mapgen depends on
// mapdata like everyone else does.
package mapgen

import (
	"container/heap"
	"math"
	"math/rand"
	"time"

	"micro/server/internal/mapdata"
)

// roomPadding is the minimum gap (in tiles) enforced between room bounding
// boxes so rooms don't crowd each other and corridors have room to run.
// Must be at least 3: each room draws its own 1-tile wall ring just
// outside its bounding box, so a gap of 2 lets two rooms' rings touch
// directly with no tile left between them for a corridor to pass through
// without hugging one of the walls. 3 leaves exactly one clear tile.
const roomPadding = 3

// maxPlacementAttemptsPerRoom bounds how hard we try to fit each room before
// giving up on it (dense configs may not fit every requested room).
const maxPlacementAttemptsPerRoom = 200

// Generate builds a new level map from the given configuration.
func Generate(cfg Config) (*mapdata.Map, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}

	seed := cfg.Seed
	if seed == 0 {
		seed = time.Now().UnixNano()
	}
	rng := rand.New(rand.NewSource(seed))

	m := &mapdata.Map{Width: cfg.Width, Height: cfg.Height, Seed: seed}

	placeRooms(m, cfg, rng)
	connectRooms(m, rng, cfg.ExtraCorridorPercent)
	assignDarkRooms(m, cfg.DarkRoomPercent, rng)
	assignHiddenDoors(m, cfg.HiddenDoorPercent, rng)
	buildDoorsList(m)

	return m, nil
}

func placeRooms(m *mapdata.Map, cfg Config, rng *rand.Rand) {
	nextID := 0
	for i := 0; i < cfg.NumRooms; i++ {
		for attempt := 0; attempt < maxPlacementAttemptsPerRoom; attempt++ {
			w := cfg.MinRoomSize + rng.Intn(cfg.MaxRoomSize-cfg.MinRoomSize+1)
			h := cfg.MinRoomSize + rng.Intn(cfg.MaxRoomSize-cfg.MinRoomSize+1)
			// Keep a 1-tile wall border around the whole map.
			maxX := m.Width - w - 1
			maxY := m.Height - h - 1
			if maxX < 1 || maxY < 1 {
				break // room too big to ever fit; stop trying this size
			}
			x := 1 + rng.Intn(maxX)
			y := 1 + rng.Intn(maxY)

			candidate := mapdata.Room{ID: nextID, X: x, Y: y, W: w, H: h}
			if overlapsAny(m, candidate) {
				continue
			}

			m.Rooms = append(m.Rooms, candidate)
			nextID++
			break
		}
	}
}

func overlapsAny(m *mapdata.Map, candidate mapdata.Room) bool {
	for _, r := range m.Rooms {
		if candidate.X-roomPadding < r.X+r.W &&
			candidate.X+candidate.W+roomPadding > r.X &&
			candidate.Y-roomPadding < r.Y+r.H &&
			candidate.Y+candidate.H+roomPadding > r.Y {
			return true
		}
	}
	return false
}

// connectRooms links all rooms with a minimum-spanning-tree of corridors
// (guaranteeing every room is reachable, and that the level is not densely
// interconnected), then adds a configurable number of extra corridors
// between nearby rooms to create loops and alternate routes.
func connectRooms(m *mapdata.Map, rng *rand.Rand, extraCorridorPercent float64) {
	n := len(m.Rooms)
	if n < 2 {
		return
	}

	nextCorridorID := 0
	addCorridor := func(i, j int) {
		c := buildCorridor(nextCorridorID, m, m.Rooms[i], m.Rooms[j])
		nextCorridorID++
		m.Corridors = append(m.Corridors, c)
		m.Rooms[i].CorridorIDs = append(m.Rooms[i].CorridorIDs, c.ID)
		m.Rooms[j].CorridorIDs = append(m.Rooms[j].CorridorIDs, c.ID)
	}

	inTree := make([]bool, n)
	inTree[0] = true
	edgesUsed := map[[2]int]bool{}

	for connected := 1; connected < n; connected++ {
		bestFrom, bestTo := -1, -1
		bestDist := math.MaxFloat64
		for i := 0; i < n; i++ {
			if !inTree[i] {
				continue
			}
			for j := 0; j < n; j++ {
				if inTree[j] {
					continue
				}
				d := roomDistance(m.Rooms[i], m.Rooms[j])
				if d < bestDist {
					bestDist, bestFrom, bestTo = d, i, j
				}
			}
		}
		addCorridor(bestFrom, bestTo)
		inTree[bestTo] = true
		edgesUsed[edgeKey(bestFrom, bestTo)] = true
	}

	// Extra edges for alternate paths: rank all remaining pairs by distance
	// and add the closest ones, up to a budget derived from room count.
	type pair struct {
		i, j int
		dist float64
	}
	var candidates []pair
	for i := 0; i < n; i++ {
		for j := i + 1; j < n; j++ {
			if edgesUsed[edgeKey(i, j)] {
				continue
			}
			candidates = append(candidates, pair{i, j, roomDistance(m.Rooms[i], m.Rooms[j])})
		}
	}
	rng.Shuffle(len(candidates), func(a, b int) { candidates[a], candidates[b] = candidates[b], candidates[a] })
	// A light distance-biased sort keeps extra corridors mostly local
	// (shorter, more plausible tunnels) while the shuffle above still
	// gives variety among similarly-close pairs.
	for a := 1; a < len(candidates); a++ {
		key := candidates[a]
		b := a - 1
		for b >= 0 && candidates[b].dist > key.dist {
			candidates[b+1] = candidates[b]
			b--
		}
		candidates[b+1] = key
	}

	extra := int(math.Round(float64(n) * extraCorridorPercent / 100))
	for k := 0; k < extra && k < len(candidates); k++ {
		p := candidates[k]
		addCorridor(p.i, p.j)
		edgesUsed[edgeKey(p.i, p.j)] = true
	}
}

func edgeKey(i, j int) [2]int {
	if i > j {
		i, j = j, i
	}
	return [2]int{i, j}
}

func roomDistance(a, b mapdata.Room) float64 {
	dx := float64(a.CenterX() - b.CenterX())
	dy := float64(a.CenterY() - b.CenterY())
	return math.Hypot(dx, dy)
}

// buildCorridor connects room a to room b with a tunnel from a's exit point
// to b's exit point (exitPoint; these become DoorA and DoorB), routed by
// findCorridorPath so the path stays off every room's interior and 1-tile
// wall ring except at those two door tiles — simple geometric L/Z paths
// can't guarantee that in general (rooms can be placed with almost no
// clearance along the axis a corridor needs to travel, or a third room can
// sit in the way), so this searches the grid instead.
//
// If no such route exists at all (very rare — would need a room fully
// boxed in by others with no gap anywhere), the search is retried allowing
// the path to touch a's and b's own walls (but still avoiding every other
// room), and failing even that, falls back to a direct unblocked path, so
// a corridor is always produced: dropping one would disconnect the MST.
func buildCorridor(id int, m *mapdata.Map, a, b mapdata.Room) mapdata.Corridor {
	ax, ay := exitPoint(a, b)
	bx, by := exitPoint(b, a)
	doorA := mapdata.Point{X: ax, Y: ay}
	doorB := mapdata.Point{X: bx, Y: by}

	path, ok := findCorridorPath(m.Width, m.Height, doorA, doorB, blockedFn(m.Rooms, a.ID, b.ID, doorA, doorB, false))
	if !ok {
		path, ok = findCorridorPath(m.Width, m.Height, doorA, doorB, blockedFn(m.Rooms, a.ID, b.ID, doorA, doorB, true))
	}
	if !ok {
		var direct []mapdata.Point
		x, y := appendRun(&direct, ax, ay, bx, ay)
		appendRun(&direct, x, y, bx, by)
		path = dedupe(direct)
	}

	return mapdata.Corridor{
		ID:    id,
		RoomA: a.ID,
		RoomB: b.ID,
		DoorA: mapdata.Door{X: ax, Y: ay},
		DoorB: mapdata.Door{X: bx, Y: by},
		Path:  path,
	}
}

// blockedFn returns a predicate reporting whether (x,y) is off-limits for a
// corridor connecting roomAID to roomBID at doorA/doorB: inside some room's
// interior or 1-tile wall ring, other than exactly at that corridor's own
// door into roomAID or roomBID. When allowOwnRooms is true, roomAID and
// roomBID are excluded from blocking entirely (used as a fallback so a
// corridor between two very tightly-spaced rooms can still be routed,
// while other rooms remain off-limits).
func blockedFn(rooms []mapdata.Room, roomAID, roomBID int, doorA, doorB mapdata.Point, allowOwnRooms bool) func(x, y int) bool {
	return func(x, y int) bool {
		for _, r := range rooms {
			if r.ID == roomAID || r.ID == roomBID {
				if allowOwnRooms {
					continue
				}
				if (r.ID == roomAID && x == doorA.X && y == doorA.Y) || (r.ID == roomBID && x == doorB.X && y == doorB.Y) {
					continue
				}
			}
			if x >= r.X-1 && x <= r.X+r.W && y >= r.Y-1 && y <= r.Y+r.H {
				return true
			}
		}
		return false
	}
}

// corridorTurnPenalty is the extra cost findCorridorPath charges for
// changing direction, biasing routes toward long straight runs (reading as
// a corridor) over the shortest-possible staircase a plain BFS would find.
const corridorTurnPenalty = 4

// corridorPathNode is a search state: a grid cell plus the direction of
// travel used to reach it (dirNone only for the start), so turns can be
// costed.
type corridorPathNode struct {
	x, y, dir int
}

var corridorDirs = []mapdata.Point{{X: 0, Y: -1}, {X: 0, Y: 1}, {X: -1, Y: 0}, {X: 1, Y: 0}}

const dirNone = -1

type corridorQueueItem struct {
	node corridorPathNode
	cost int
}

type corridorQueue []corridorQueueItem

func (q corridorQueue) Len() int           { return len(q) }
func (q corridorQueue) Less(i, j int) bool { return q[i].cost < q[j].cost }
func (q corridorQueue) Swap(i, j int)      { q[i], q[j] = q[j], q[i] }
func (q *corridorQueue) Push(x any)        { *q = append(*q, x.(corridorQueueItem)) }
func (q *corridorQueue) Pop() any {
	old := *q
	n := len(old)
	item := old[n-1]
	*q = old[:n-1]
	return item
}

// findCorridorPath searches the width x height grid for the cheapest route
// from start to goal using 4-directional steps where blocked(x,y) is
// false, charging corridorTurnPenalty extra for each direction change so
// the result reads as a corridor rather than a shortest-path staircase. ok
// is false if no route exists at all.
func findCorridorPath(width, height int, start, goal mapdata.Point, blocked func(x, y int) bool) (path []mapdata.Point, ok bool) {
	startNode := corridorPathNode{start.X, start.Y, dirNone}
	dist := map[corridorPathNode]int{startNode: 0}
	prev := map[corridorPathNode]corridorPathNode{}

	pq := &corridorQueue{{node: startNode, cost: 0}}
	heap.Init(pq)

	var goalNode corridorPathNode
	found := false
	for pq.Len() > 0 {
		cur := heap.Pop(pq).(corridorQueueItem)
		if d, seen := dist[cur.node]; seen && cur.cost > d {
			continue // stale entry from before a cheaper route was found
		}
		if cur.node.x == goal.X && cur.node.y == goal.Y {
			goalNode = cur.node
			found = true
			break
		}
		for dIdx, d := range corridorDirs {
			nx, ny := cur.node.x+d.X, cur.node.y+d.Y
			if nx < 0 || nx >= width || ny < 0 || ny >= height {
				continue
			}
			isGoal := nx == goal.X && ny == goal.Y
			if !isGoal && blocked(nx, ny) {
				continue
			}
			step := 1
			if cur.node.dir != dirNone && cur.node.dir != dIdx {
				step += corridorTurnPenalty
			}
			next := corridorPathNode{nx, ny, dIdx}
			newCost := cur.cost + step
			if d, seen := dist[next]; !seen || newCost < d {
				dist[next] = newCost
				prev[next] = cur.node
				heap.Push(pq, corridorQueueItem{node: next, cost: newCost})
			}
		}
	}
	if !found {
		return nil, false
	}

	for n := goalNode; ; {
		path = append(path, mapdata.Point{X: n.x, Y: n.y})
		if n == startNode {
			break
		}
		n = prev[n]
	}
	for i, j := 0, len(path)-1; i < j; i, j = i+1, j-1 {
		path[i], path[j] = path[j], path[i]
	}
	return path, true
}

// appendRun walks in a straight line from (x,y) to (tx,ty) — first
// horizontally, then vertically, or vice versa depending on which differs —
// appending every visited point (including both ends) to path.
func appendRun(path *[]mapdata.Point, x, y, tx, ty int) (int, int) {
	for x != tx {
		*path = append(*path, mapdata.Point{X: x, Y: y})
		if tx > x {
			x++
		} else {
			x--
		}
	}
	for y != ty {
		*path = append(*path, mapdata.Point{X: x, Y: y})
		if ty > y {
			y++
		} else {
			y--
		}
	}
	*path = append(*path, mapdata.Point{X: x, Y: y})
	return x, y
}

// dedupe removes consecutive duplicate points, which occur where two
// appendRun calls join at a corner.
func dedupe(path []mapdata.Point) []mapdata.Point {
	if len(path) < 2 {
		return path
	}
	out := path[:1]
	for _, p := range path[1:] {
		if p != out[len(out)-1] {
			out = append(out, p)
		}
	}
	return out
}

// exitPoint picks a point just outside room r's wall, on the side facing
// toward, to use as a door/corridor attachment point.
func exitPoint(r mapdata.Room, toward mapdata.Room) (int, int) {
	dx := toward.CenterX() - r.CenterX()
	dy := toward.CenterY() - r.CenterY()

	if abs(dx) >= abs(dy) {
		y := clamp(r.CenterY(), r.Y, r.Y+r.H-1)
		if dx >= 0 {
			return r.X + r.W, y // east wall
		}
		return r.X - 1, y // west wall
	}
	x := clamp(r.CenterX(), r.X, r.X+r.W-1)
	if dy >= 0 {
		return x, r.Y + r.H // south wall
	}
	return x, r.Y - 1 // north wall
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func assignDarkRooms(m *mapdata.Map, darkPercent float64, rng *rand.Rand) {
	n := len(m.Rooms)
	if n == 0 {
		return
	}
	numDark := int(math.Round(float64(n) * darkPercent / 100))
	order := rng.Perm(n)
	for i := 0; i < numDark; i++ {
		m.Rooms[order[i]].Dark = true
	}
}

func assignHiddenDoors(m *mapdata.Map, hiddenPercent float64, rng *rand.Rand) {
	var doors []*mapdata.Door
	for i := range m.Corridors {
		doors = append(doors, &m.Corridors[i].DoorA, &m.Corridors[i].DoorB)
	}
	n := len(doors)
	if n == 0 {
		return
	}
	numHidden := int(math.Round(float64(n) * hiddenPercent / 100))
	order := rng.Perm(n)
	for i := 0; i < numHidden; i++ {
		doors[order[i]].Hidden = true
	}
}

// buildDoorsList flattens every corridor's DoorA/DoorB into Map.Doors, for
// callers that just want "every door on the map." Must run after
// assignHiddenDoors so the Hidden flag is already set.
func buildDoorsList(m *mapdata.Map) {
	m.Doors = nil
	for _, c := range m.Corridors {
		m.Doors = append(m.Doors, c.DoorA, c.DoorB)
	}
}
