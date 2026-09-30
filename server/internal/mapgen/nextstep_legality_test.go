package mapgen_test

import (
	"math/rand"
	"testing"

	"micro/server/internal/mapdata"
	"micro/server/internal/mapgen"
)

// TestNextStepForwardLegalityAcrossGeneratedMaps stress-tests the same
// invariant mapdata's own
// TestNextStepNeverStepsThroughAWallWithSharedDoorCoordinate checks by
// hand, but against real generated maps across many seeds and a high
// extra-corridor rate (more corridors sharing/crossing geometry, so more
// chances to hit the same class of bug): every step NextStep returns must
// be independently confirmed legal via CanMove from the exact starting
// point.
//
// It lives here rather than beside the hand-built version in mapdata
// because it is really a mapgen x mapdata integration test: the bug class
// it guards only arises from geometry a generator produces. Keeping it in
// mapdata would mean mapdata's own tests depend on mapgen, which inverts
// the dependency — mapgen is built on mapdata — and would stop mapdata
// being a leaf package.
//
// This is also the single slowest test in the suite by a wide margin
// (~16s of a ~20s run), hence the -short skip.
func TestNextStepForwardLegalityAcrossGeneratedMaps(t *testing.T) {
	if testing.Short() {
		t.Skip("seed sweep")
	}

	rng := rand.New(rand.NewSource(1))

	for seed := int64(1); seed <= 25; seed++ {
		cfg := mapgen.DefaultConfig()
		cfg.Seed = seed
		cfg.NumRooms = 15
		cfg.ExtraCorridorPercent = 60
		m, err := mapgen.Generate(cfg)
		if err != nil {
			t.Fatalf("seed %d: Generate: %v", seed, err)
		}

		var pts []mapdata.Point
		for _, r := range m.Rooms {
			for y := r.Y; y < r.Y+r.H; y++ {
				for x := r.X; x < r.X+r.W; x++ {
					pts = append(pts, mapdata.Point{X: x, Y: y})
				}
			}
		}
		for _, c := range m.Corridors {
			pts = append(pts, c.Path...)
		}
		if len(pts) < 2 {
			continue
		}

		const samples = 400
		for i := 0; i < samples; i++ {
			s := pts[rng.Intn(len(pts))]
			tg := pts[rng.Intn(len(pts))]
			if s == tg {
				continue
			}
			nx, ny, ok := m.NextStep(s.X, s.Y, tg.X, tg.Y)
			if !ok {
				continue
			}
			if !m.CanMove(s.X, s.Y, nx, ny) {
				t.Fatalf("seed %d: NextStep(%v -> %v) returned illegal step (%d,%d): CanMove(%v -> %d,%d) is false",
					seed, s, tg, nx, ny, s, nx, ny)
			}
		}
	}
}
