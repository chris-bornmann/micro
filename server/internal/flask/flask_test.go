package flask

import (
	"encoding/json"
	"regexp"
	"testing"

	"micro/server/internal/item"
	"micro/server/internal/mapdata"
)

func testMap() mapdata.Map {
	room := mapdata.Room{ID: 0, X: 5, Y: 5, W: 10, H: 10}
	return mapdata.Map{
		Width: 20, Height: 20,
		Rooms: []mapdata.Room{room},
	}
}

func initResult(t *testing.T, out []byte) flaskItem {
	t.Helper()
	var got flaskItem
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("unmarshal Init output: %v", err)
	}
	return got
}

func TestInitPlacesFlaskOnTheFloorInARoom(t *testing.T) {
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

	if ok, _ := regexp.MatchString(`^flask:\d+$`, got.ID); !ok {
		t.Errorf("expected ID matching flask:<N>, got %q", got.ID)
	}
	if got.Glyph != '!' {
		t.Errorf("expected glyph '!' (a flask is a liquid container, same as a potion), got %q", got.Glyph)
	}
	if got.Category != item.CategoryFlask {
		t.Errorf("expected category %q, got %q", item.CategoryFlask, got.Category)
	}
	if got.Usage != item.UsagePersistent {
		t.Errorf("expected usage %q, got %q", item.UsagePersistent, got.Usage)
	}
	if got.Name != "flask of oil" {
		t.Errorf("expected name %q, got %q", "flask of oil", got.Name)
	}
	if got.Kind != KindOil {
		t.Errorf("expected kind %q, got %q", KindOil, got.Kind)
	}
	if got.Weight != 2 {
		t.Errorf("expected Weight 2, got %d", got.Weight)
	}
	if !got.Fragile {
		t.Error("expected a flask of oil to be Fragile (never lands intact after being thrown)")
	}

	r := m.Rooms[0]
	if got.X < r.X || got.X >= r.X+r.W || got.Y < r.Y || got.Y >= r.Y+r.H {
		t.Errorf("expected flask at (%d,%d) to be inside the room", got.X, got.Y)
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

	f1, f2 := initResult(t, out1), initResult(t, out2)
	if f1.X != f2.X || f1.Y != f2.Y {
		t.Errorf("expected the same map seed to always spawn the flask at the same position, got (%d,%d) and (%d,%d)",
			f1.X, f1.Y, f2.X, f2.Y)
	}
}

func TestInitMintsUniqueIDsAcrossCalls(t *testing.T) {
	m := testMap()
	m.Seed = 42
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

	f1, f2 := initResult(t, out1), initResult(t, out2)
	if f1.ID == f2.ID {
		t.Errorf("expected distinct IDs across calls, got %q twice", f1.ID)
	}
}
