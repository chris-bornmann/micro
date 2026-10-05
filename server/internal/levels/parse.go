package levels

// The hand-drawn level format. Every character of the drawing is one tile:
//
//	.   floor, lit           :   floor, dark (any : darkens its room)
//	#   wall                 +   door        S   hidden door
//	,   corridor             (space)  nothing: solid rock
//
// and any other character is whatever the legend says stands there, on
// floor: "<" and ">" are the stairs unless the legend says otherwise.
//
// A room is a rectangle of floor ringed by walls and doors. Walls inside
// the rectangle split it up — a hall of pillars, a maze — and make it a
// walled room (mapdata.Room.Walled). A corridor is a run of "," from one
// door to another, never branching; a door in a wall two rooms share
// joins them directly.

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"micro/server/internal/mapdata"
)

// defaultLegend is what a drawing's characters mean unless its legend
// says otherwise.
var defaultLegend = map[rune]string{'<': "stairs_up", '>': "stairs_down"}

// drawing is the map as written, a rune per tile, every row padded to the
// widest with rock.
type drawing struct {
	rows [][]rune
	w, h int
}

func (d drawing) at(x, y int) rune {
	if x < 0 || y < 0 || y >= d.h || x >= d.w {
		return ' '
	}
	return d.rows[y][x]
}

func isFloor(r rune) bool { return r == '.' || r == ':' }
func isDoor(r rune) bool  { return r == '+' || r == 'S' }

var steps = []mapdata.Point{{X: 1, Y: 0}, {X: -1, Y: 0}, {X: 0, Y: 1}, {X: 0, Y: -1}}

// Parse reads a hand-drawn level. legend maps the drawing's other
// characters to template IDs. Errors name the line and column (1-based)
// of the drawing.
func Parse(src string, legend map[rune]string) (*Layout, error) {
	lines := strings.Split(strings.TrimRight(src, "\n"), "\n")
	d := drawing{h: len(lines)}
	for _, l := range lines {
		d.w = max(d.w, len([]rune(l)))
	}
	for _, l := range lines {
		row := []rune(l)
		for len(row) < d.w {
			row = append(row, ' ')
		}
		d.rows = append(d.rows, row)
	}
	what := func(r rune) (string, bool) {
		if id, ok := legend[r]; ok {
			return id, true
		}
		id, ok := defaultLegend[r]
		return id, ok
	}

	var errs []error
	bad := func(x, y int, format string, args ...any) {
		errs = append(errs, fmt.Errorf("line %d, column %d: %s", y+1, x+1, fmt.Sprintf(format, args...)))
	}

	// Every tile is known: floor, wall, door, corridor, rock, or a legend
	// character, which stands on floor.
	walkable := func(x, y int) bool {
		r := d.at(x, y)
		_, placed := what(r)
		return isFloor(r) || placed
	}
	for y, row := range d.rows {
		for x, r := range row {
			if _, ok := what(r); !ok && !isFloor(r) && !isDoor(r) && !strings.ContainsRune("#, ", r) {
				bad(x, y, "%q is not in the legend", r)
			}
		}
	}
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}

	m := &mapdata.Map{Width: d.w, Height: d.h, Walls: map[mapdata.Point]bool{}}
	var placed []Placement
	roomOf := map[mapdata.Point]int{} // floor tile -> room ID

	// Rooms: areas of floor, merged while one's ring reaches into another
	// (a wall inside a room splits its floor; its ring is still one).
	regions := floodAll(d, walkable)
	for merged := true; merged; {
		merged = false
		for i := 0; i < len(regions) && !merged; i++ {
			for j := i + 1; j < len(regions) && !merged; j++ {
				if regions[i].reaches(regions[j]) {
					regions[i] = regions[i].join(regions[j])
					regions = append(regions[:j], regions[j+1:]...)
					merged = true
				}
			}
		}
	}
	sort.Slice(regions, func(i, j int) bool {
		a, b := regions[i].box, regions[j].box
		return a.Y < b.Y || a.Y == b.Y && a.X < b.X
	})
	for id, rg := range regions {
		b := rg.box
		room := mapdata.Room{ID: id, X: b.X, Y: b.Y, W: b.W, H: b.H}
		for y := b.Y - 1; y <= b.Y+b.H; y++ {
			for x := b.X - 1; x <= b.X+b.W; x++ {
				r := d.at(x, y)
				inside := x >= b.X && x < b.X+b.W && y >= b.Y && y < b.Y+b.H
				switch {
				case !inside && r != '#' && !isDoor(r):
					bad(x, y, "a room's edge must be wall or door, not %q (rooms are rectangles)", r)
				case inside && r == '#':
					m.Walls[mapdata.Point{X: x, Y: y}] = true
					room.Walled = true
				case inside && !walkable(x, y):
					bad(x, y, "%q inside a room", r)
				case inside:
					room.Dark = room.Dark || r == ':'
					roomOf[mapdata.Point{X: x, Y: y}] = id
					if pid, ok := what(r); ok {
						placed = append(placed, Placement{ID: pid, X: x, Y: y})
					}
				}
			}
		}
		m.Rooms = append(m.Rooms, room)
	}

	// Doors: each on the edge of one room, or two.
	roomsBy := func(x, y int) []int {
		var ids []int
		for _, s := range steps {
			if id, ok := roomOf[mapdata.Point{X: x + s.X, Y: y + s.Y}]; ok && !containsInt(ids, id) {
				ids = append(ids, id)
			}
		}
		return ids
	}
	doorAt := map[mapdata.Point][]int{}
	for y, row := range d.rows {
		for x, r := range row {
			if !isDoor(r) {
				continue
			}
			ids := roomsBy(x, y)
			if len(ids) == 0 {
				bad(x, y, "a door opens onto no room")
			}
			doorAt[mapdata.Point{X: x, Y: y}] = ids
		}
	}

	// Corridors: runs of "," from one door to another; or a door two
	// rooms share, which joins them with no corridor at all.
	door := func(p mapdata.Point) mapdata.Door {
		return mapdata.Door{X: p.X, Y: p.Y, Hidden: d.at(p.X, p.Y) == 'S'}
	}
	addCorridor := func(a mapdata.Point, ra int, b mapdata.Point, rb int, path []mapdata.Point) {
		id := len(m.Corridors)
		m.Corridors = append(m.Corridors, mapdata.Corridor{ID: id, RoomA: ra, RoomB: rb, DoorA: door(a), DoorB: door(b), Path: path})
		m.Rooms[ra].CorridorIDs = append(m.Rooms[ra].CorridorIDs, id)
		if rb != ra {
			m.Rooms[rb].CorridorIDs = append(m.Rooms[rb].CorridorIDs, id)
		}
	}
	for _, p := range sortedPoints(doorAt) {
		if ids := doorAt[p]; len(ids) == 2 {
			addCorridor(p, ids[0], p, ids[1], []mapdata.Point{p})
		}
	}
	isCorr := func(x, y int) bool { return d.at(x, y) == ',' }
	seen := map[mapdata.Point]bool{}
	for y, row := range d.rows {
		for x := range row {
			start := mapdata.Point{X: x, Y: y}
			if !isCorr(x, y) || seen[start] {
				continue
			}
			tiles := flood(d, start, isCorr, seen)
			var ends []mapdata.Point
			for _, t := range tiles {
				n := 0
				for _, s := range steps {
					q := mapdata.Point{X: t.X + s.X, Y: t.Y + s.Y}
					if isCorr(q.X, q.Y) {
						n++
					} else if _, ok := doorAt[q]; ok {
						n++
						ends = append(ends, q)
					}
				}
				if n > 2 {
					bad(t.X, t.Y, "a corridor may not branch")
				}
			}
			if len(ends) != 2 {
				bad(start.X, start.Y, "a corridor must run from one door to another; this one reaches %d", len(ends))
				continue
			}
			a, b := ends[0], ends[1]
			ra, rb := doorAt[a], doorAt[b]
			if len(ra) != 1 || len(rb) != 1 {
				bad(start.X, start.Y, "a corridor's doors must each open onto one room")
				continue
			}
			addCorridor(a, ra[0], b, rb[0], walk(a, b, isCorr))
		}
	}
	for _, p := range sortedPoints(doorAt) {
		if ids := doorAt[p]; len(ids) == 1 && !usedDoor(m, p) {
			bad(p.X, p.Y, "a door leads nowhere: no corridor, and no room beyond")
		}
	}
	for _, c := range m.Corridors {
		m.Doors = append(m.Doors, c.DoorA)
		if c.DoorB != c.DoorA {
			m.Doors = append(m.Doors, c.DoorB)
		}
	}
	if !hasPlacement(placed, "stairs_up") {
		errs = append(errs, errors.New("no way in: place stairs up (<)"))
	}
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return &Layout{Map: m, Placed: placed}, nil
}

// region is a connected area of floor and the rectangle it fills.
type region struct {
	tiles []mapdata.Point
	box   mapdata.Room
}

// reaches reports whether r's ring — its box grown by one — touches o's
// floor: they are parts of one room, split by walls inside it.
func (r region) reaches(o region) bool {
	b := r.box
	for _, t := range o.tiles {
		if t.X >= b.X-1 && t.X <= b.X+b.W && t.Y >= b.Y-1 && t.Y <= b.Y+b.H {
			return true
		}
	}
	return false
}

func (r region) join(o region) region { return regionOf(append(r.tiles, o.tiles...)) }

func regionOf(tiles []mapdata.Point) region {
	x0, y0, x1, y1 := tiles[0].X, tiles[0].Y, tiles[0].X, tiles[0].Y
	for _, t := range tiles {
		x0, y0, x1, y1 = min(x0, t.X), min(y0, t.Y), max(x1, t.X), max(y1, t.Y)
	}
	return region{tiles: tiles, box: mapdata.Room{X: x0, Y: y0, W: x1 - x0 + 1, H: y1 - y0 + 1}}
}

// floodAll splits every tile ok accepts into connected regions.
func floodAll(d drawing, ok func(x, y int) bool) []region {
	var out []region
	seen := map[mapdata.Point]bool{}
	for y := 0; y < d.h; y++ {
		for x := 0; x < d.w; x++ {
			p := mapdata.Point{X: x, Y: y}
			if ok(x, y) && !seen[p] {
				out = append(out, regionOf(flood(d, p, ok, seen)))
			}
		}
	}
	return out
}

// flood is every tile ok accepts connected to start, marked in seen.
func flood(d drawing, start mapdata.Point, ok func(x, y int) bool, seen map[mapdata.Point]bool) []mapdata.Point {
	seen[start] = true
	tiles := []mapdata.Point{start}
	for i := 0; i < len(tiles); i++ {
		for _, s := range steps {
			q := mapdata.Point{X: tiles[i].X + s.X, Y: tiles[i].Y + s.Y}
			if !seen[q] && ok(q.X, q.Y) {
				seen[q] = true
				tiles = append(tiles, q)
			}
		}
	}
	return tiles
}

// walk is the corridor path from door a to door b along tiles corr
// accepts, both doors included.
func walk(a, b mapdata.Point, corr func(x, y int) bool) []mapdata.Point {
	path := []mapdata.Point{a}
	prev, cur := a, a
	for cur != b {
		for _, s := range steps {
			q := mapdata.Point{X: cur.X + s.X, Y: cur.Y + s.Y}
			if q != prev && (corr(q.X, q.Y) || q == b) && !(cur == a && q == b) {
				prev, cur = cur, q
				break
			}
		}
		path = append(path, cur)
		if len(path) > 10000 {
			break // a loop: Parse's checks already refused it
		}
	}
	return path
}

func usedDoor(m *mapdata.Map, p mapdata.Point) bool {
	for _, c := range m.Corridors {
		if (c.DoorA.X == p.X && c.DoorA.Y == p.Y) || (c.DoorB.X == p.X && c.DoorB.Y == p.Y) {
			return true
		}
	}
	return false
}

func hasPlacement(ps []Placement, id string) bool {
	for _, p := range ps {
		if p.ID == id {
			return true
		}
	}
	return false
}

func containsInt(xs []int, x int) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

func sortedPoints(m map[mapdata.Point][]int) []mapdata.Point {
	ps := make([]mapdata.Point, 0, len(m))
	for p := range m {
		ps = append(ps, p)
	}
	sort.Slice(ps, func(i, j int) bool { return ps[i].Y < ps[j].Y || ps[i].Y == ps[j].Y && ps[i].X < ps[j].X })
	return ps
}
