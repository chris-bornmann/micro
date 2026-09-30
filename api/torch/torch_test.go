package torch

import (
	"encoding/json"
	"testing"

	"map_api/item"
	"map_api/mapdata"
)

func testMap() mapdata.Map {
	room := mapdata.Room{ID: 0, X: 5, Y: 5, W: 10, H: 10}
	return mapdata.Map{
		Width: 20, Height: 20,
		Rooms: []mapdata.Room{room},
	}
}

func initResult(t *testing.T, out []byte) torchItem {
	t.Helper()
	var got torchItem
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("unmarshal Init output: %v", err)
	}
	return got
}

func TestInitPlacesTorchOnTheFloorInARoomUnlit(t *testing.T) {
	m := testMap()
	m.Seed = 7
	mapJSON, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal map: %v", err)
	}

	out, err := Init(mapJSON)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	got := initResult(t, out)

	if got.Glyph != '/' {
		t.Errorf("expected glyph '/', got %q", got.Glyph)
	}
	if got.Category != item.CategoryTorch {
		t.Errorf("expected category %q, got %q", item.CategoryTorch, got.Category)
	}
	if got.Usage != item.UsageOneShot {
		t.Errorf("expected usage %q, got %q", item.UsageOneShot, got.Usage)
	}
	if got.Name != "torch" {
		t.Errorf("expected name %q, got %q", "torch", got.Name)
	}
	if got.Weight != 2 {
		t.Errorf("expected Weight 2, got %d", got.Weight)
	}

	r := m.Rooms[0]
	if got.X < r.X || got.X >= r.X+r.W || got.Y < r.Y || got.Y >= r.Y+r.H {
		t.Errorf("expected torch at (%d,%d) to be inside the room", got.X, got.Y)
	}
}

func TestInitRollsLifespanWithinRange(t *testing.T) {
	m := testMap()
	for seed := int64(0); seed < 50; seed++ {
		m.Seed = seed
		mapJSON, err := json.Marshal(m)
		if err != nil {
			t.Fatalf("marshal map: %v", err)
		}
		out, err := Init(mapJSON)
		if err != nil {
			t.Fatalf("Init: %v", err)
		}
		got := initResult(t, out)
		if got.Lifespan < MinLifespan || got.Lifespan > MaxLifespan {
			t.Fatalf("seed %d: expected lifespan in [%d,%d], got %d", seed, MinLifespan, MaxLifespan, got.Lifespan)
		}
	}
}

func TestInitRejectsMapWithNoRooms(t *testing.T) {
	empty, err := json.Marshal(mapdata.Map{Width: 10, Height: 10})
	if err != nil {
		t.Fatalf("marshal empty map: %v", err)
	}
	if _, err := Init(empty); err == nil {
		t.Error("expected an error for a map with no rooms, got nil")
	}
}

func TestInitIsDeterministicForTheSameMapSeed(t *testing.T) {
	m := testMap()
	m.Seed = 123
	mapJSON, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal map: %v", err)
	}

	out1, err := Init(mapJSON)
	if err != nil {
		t.Fatalf("Init (1): %v", err)
	}
	out2, err := Init(mapJSON)
	if err != nil {
		t.Fatalf("Init (2): %v", err)
	}

	t1, t2 := initResult(t, out1), initResult(t, out2)
	if t1.X != t2.X || t1.Y != t2.Y {
		t.Errorf("expected the same map seed to always spawn the torch at the same position, got (%d,%d) and (%d,%d)",
			t1.X, t1.Y, t2.X, t2.Y)
	}
	if t1.Lifespan != t2.Lifespan {
		t.Errorf("expected the same map seed to always roll the same lifespan, got %d and %d", t1.Lifespan, t2.Lifespan)
	}
}

func TestLifespanExtractsRolledValue(t *testing.T) {
	m := testMap()
	m.Seed = 42
	mapJSON, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal map: %v", err)
	}
	out, err := Init(mapJSON)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	want := initResult(t, out).Lifespan

	got, err := Lifespan(out)
	if err != nil {
		t.Fatalf("Lifespan: %v", err)
	}
	if got != want {
		t.Errorf("expected Lifespan to extract %d, got %d", want, got)
	}
}

func TestLifespanErrorsWithInvalidSelfJSON(t *testing.T) {
	if _, err := Lifespan([]byte("not json")); err == nil {
		t.Error("expected an error for invalid self JSON, got nil")
	}
}
