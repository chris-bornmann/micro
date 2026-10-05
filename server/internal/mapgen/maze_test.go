package mapgen

import (
	"testing"

	"micro/server/internal/mapdata"
)

func maze(t *testing.T, seed int64, braid int) *Maze {
	t.Helper()
	cfg := DefaultConfig()
	cfg.Width, cfg.Height, cfg.Seed = 41, 21, seed
	mz, err := GenerateMaze(cfg, braid)
	if err != nil {
		t.Fatal(err)
	}
	return mz
}

// reach is every tile reachable from start, by walking.
func reach(m *mapdata.Map, start mapdata.Point) map[mapdata.Point]bool {
	seen := map[mapdata.Point]bool{start: true}
	queue := []mapdata.Point{start}
	for len(queue) > 0 {
		p := queue[0]
		queue = queue[1:]
		for _, d := range [][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
			n := mapdata.Point{X: p.X + d[0], Y: p.Y + d[1]}
			if !seen[n] && m.CanMove(p.X, p.Y, n.X, n.Y) {
				seen[n] = true
				queue = append(queue, n)
			}
		}
	}
	return seen
}

// deadEnds counts open tiles with exactly one open neighbor.
func deadEnds(m *mapdata.Map) int {
	n := 0
	for _, p := range m.WalkableTiles() {
		exits := 0
		for _, d := range [][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
			if m.CanMove(p.X, p.Y, p.X+d[0], p.Y+d[1]) {
				exits++
			}
		}
		if exits == 1 {
			n++
		}
	}
	return n
}

func TestEveryOpenTileOfAMazeIsReachable(t *testing.T) {
	for seed := int64(1); seed <= 5; seed++ {
		mz := maze(t, seed, 10)
		r := reach(mz.Map, mz.Up)
		for _, p := range mz.Map.WalkableTiles() {
			if !r[p] {
				t.Fatalf("seed %d: %v cannot be reached from the way in", seed, p)
			}
		}
		if !r[mz.Down] || mz.Down == mz.Up {
			t.Errorf("seed %d: the way down at %v", seed, mz.Down)
		}
		if len(mz.Map.Walls) == 0 || !mz.Map.Rooms[0].Walled || !mz.Map.Rooms[0].Dark {
			t.Errorf("seed %d: a maze is a dark, walled room", seed)
		}
	}
}

// TestTheWayDownIsAsFarAsItGets: no open tile is farther from the way in.
func TestTheWayDownIsAsFarAsItGets(t *testing.T) {
	mz := maze(t, 3, 10)
	_, n, _ := mz.Map.Route(mz.Up.X, mz.Up.Y, mz.Down.X, mz.Down.Y, nil)
	for _, p := range mz.Map.WalkableTiles() {
		if p == mz.Up {
			continue
		}
		if _, k, _ := mz.Map.Route(mz.Up.X, mz.Up.Y, p.X, p.Y, nil); k > n {
			t.Fatalf("%v is %d steps away, farther than the way down's %d", p, k, n)
		}
	}
}

func TestBraidingOpensDeadEnds(t *testing.T) {
	perfect, braided := deadEnds(maze(t, 7, 0).Map), deadEnds(maze(t, 7, 100).Map)
	if perfect < 20 {
		t.Errorf("a perfect maze this size should be full of dead ends, has %d", perfect)
	}
	if braided >= perfect/2 {
		t.Errorf("braiding every dead end: %d left of %d", braided, perfect)
	}
}

func TestTheSameSeedMakesTheSameMaze(t *testing.T) {
	a, b, c := maze(t, 11, 10), maze(t, 11, 10), maze(t, 12, 10)
	if len(a.Map.Walls) != len(b.Map.Walls) || a.Down != b.Down {
		t.Error("same seed, different maze")
	}
	same := true
	for p := range a.Map.Walls {
		same = same && c.Map.Walls[p]
	}
	if same && len(a.Map.Walls) == len(c.Map.Walls) {
		t.Error("another seed made the same maze")
	}
}
