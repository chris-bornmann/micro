package mapgen

import (
	"fmt"
	"math/rand"
	"time"

	"micro/server/internal/mapdata"
)

// Maze is a generated maze level: the map, and where its stairs belong.
type Maze struct {
	Map *mapdata.Map
	// Up is a corner of the maze, where the way in goes; Down is the open
	// cell farthest from it, so the whole maze lies between them.
	Up, Down mapdata.Point
}

// GenerateMaze builds a maze level the size of cfg's map: one big dark
// room, walled (mapdata.Room.Walled) into winding passages one tile wide.
// The passages are carved by a randomized depth-first search — a "perfect"
// maze, one way between any two cells, full of dead ends — and then braid
// percent of the dead ends are opened onto a neighboring passage, making
// loops, so it confuses rather than merely leads. cfg's room settings are
// ignored; its Seed works as for Generate.
func GenerateMaze(cfg Config, braid int) (*Maze, error) {
	if cfg.Width < 7 || cfg.Height < 7 {
		return nil, fmt.Errorf("a maze needs at least 7x7 tiles (got %dx%d)", cfg.Width, cfg.Height)
	}
	seed := cfg.Seed
	if seed == 0 {
		seed = time.Now().UnixNano()
	}
	rng := rand.New(rand.NewSource(seed))

	// The room's interior is 1..w by 1..h, inside its wall ring. Cells sit
	// at odd coordinates, with a wall-or-passage tile between neighbors,
	// so both sides must be odd.
	w, h := cfg.Width-2, cfg.Height-2
	if w%2 == 0 {
		w--
	}
	if h%2 == 0 {
		h--
	}
	room := mapdata.Room{ID: 0, X: 1, Y: 1, W: w, H: h, Dark: true, Walled: true}
	open := map[mapdata.Point]bool{}
	cell := func(cx, cy int) mapdata.Point { return mapdata.Point{X: 1 + 2*cx, Y: 1 + 2*cy} }
	cw, ch := (w+1)/2, (h+1)/2
	dirs := [][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}}

	// Carve: a randomized depth-first walk over the cells.
	seen := map[[2]int]bool{{0, 0}: true}
	stack := [][2]int{{0, 0}}
	open[cell(0, 0)] = true
	for len(stack) > 0 {
		cur := stack[len(stack)-1]
		var next [][2]int
		for _, d := range dirs {
			n := [2]int{cur[0] + d[0], cur[1] + d[1]}
			if n[0] >= 0 && n[0] < cw && n[1] >= 0 && n[1] < ch && !seen[n] {
				next = append(next, n)
			}
		}
		if len(next) == 0 {
			stack = stack[:len(stack)-1]
			continue
		}
		n := next[rng.Intn(len(next))]
		a, b := cell(cur[0], cur[1]), cell(n[0], n[1])
		open[mapdata.Point{X: (a.X + b.X) / 2, Y: (a.Y + b.Y) / 2}] = true
		open[b] = true
		seen[n] = true
		stack = append(stack, n)
	}

	// Braid: open some dead ends onto a neighbor they don't already reach.
	for cy := 0; cy < ch; cy++ {
		for cx := 0; cx < cw; cx++ {
			c := cell(cx, cy)
			var closed []mapdata.Point
			exits := 0
			for _, d := range dirs {
				between := mapdata.Point{X: c.X + d[0], Y: c.Y + d[1]}
				beyond := mapdata.Point{X: c.X + 2*d[0], Y: c.Y + 2*d[1]}
				if beyond.X < 1 || beyond.X > w || beyond.Y < 1 || beyond.Y > h {
					continue
				}
				if open[between] {
					exits++
				} else {
					closed = append(closed, between)
				}
			}
			if exits == 1 && len(closed) > 0 && rng.Intn(100) < braid {
				open[closed[rng.Intn(len(closed))]] = true
			}
		}
	}

	walls := map[mapdata.Point]bool{}
	for _, p := range room.Tiles() {
		if !open[p] {
			walls[p] = true
		}
	}
	m := &mapdata.Map{Width: cfg.Width, Height: cfg.Height, Seed: seed, Rooms: []mapdata.Room{room}, Walls: walls}
	up := cell(0, 0)
	return &Maze{Map: m, Up: up, Down: farthest(m, up)}, nil
}

// farthest is the open tile of m farthest from start by walking.
func farthest(m *mapdata.Map, start mapdata.Point) mapdata.Point {
	dist := map[mapdata.Point]int{start: 0}
	queue := []mapdata.Point{start}
	best := start
	for len(queue) > 0 {
		p := queue[0]
		queue = queue[1:]
		if dist[p] > dist[best] {
			best = p
		}
		for _, d := range [][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
			n := mapdata.Point{X: p.X + d[0], Y: p.Y + d[1]}
			if _, done := dist[n]; done || !m.CanMove(p.X, p.Y, n.X, n.Y) {
				continue
			}
			dist[n] = dist[p] + 1
			queue = append(queue, n)
		}
	}
	return best
}
