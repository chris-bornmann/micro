package stairs

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

func initResult(t *testing.T, out []byte) stairsItem {
	t.Helper()
	var got stairsItem
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("unmarshal Init output: %v", err)
	}
	return got
}

func TestInitUpPlacesStairsInARoom(t *testing.T) {
	m := testMap()
	m.Seed = 7
	mapJSON, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal map: %v", err)
	}

	out, err := InitUp(mapJSON)
	if err != nil {
		t.Fatalf("InitUp: %v", err)
	}
	got := initResult(t, out)

	if ok, _ := regexp.MatchString(`^stairs:\d+$`, got.ID); !ok {
		t.Errorf("expected ID matching stairs:<N>, got %q", got.ID)
	}
	if got.Glyph != '<' {
		t.Errorf("expected glyph '<', got %q", got.Glyph)
	}
	if got.Category != item.CategoryStairs {
		t.Errorf("expected category %q, got %q", item.CategoryStairs, got.Category)
	}
	if got.Usage != item.UsagePersistent {
		t.Errorf("expected usage %q, got %q", item.UsagePersistent, got.Usage)
	}
	if got.Name != "stairs up" {
		t.Errorf("expected name %q, got %q", "stairs up", got.Name)
	}
	if got.Kind != KindUp {
		t.Errorf("expected kind %q, got %q", KindUp, got.Kind)
	}
	if !got.Fixed {
		t.Error("expected stairs to be Fixed (never picked up, dropped, or thrown)")
	}

	r := m.Rooms[0]
	if got.X < r.X || got.X >= r.X+r.W || got.Y < r.Y || got.Y >= r.Y+r.H {
		t.Errorf("expected stairs at (%d,%d) to be inside the room", got.X, got.Y)
	}
}

func TestInitDownPlacesStairsInARoom(t *testing.T) {
	m := testMap()
	m.Seed = 7
	mapJSON, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal map: %v", err)
	}

	out, err := InitDown(mapJSON)
	if err != nil {
		t.Fatalf("InitDown: %v", err)
	}
	got := initResult(t, out)

	if got.Glyph != '>' {
		t.Errorf("expected glyph '>', got %q", got.Glyph)
	}
	if got.Kind != KindDown {
		t.Errorf("expected kind %q, got %q", KindDown, got.Kind)
	}
	if got.Name != "stairs down" {
		t.Errorf("expected name %q, got %q", "stairs down", got.Name)
	}
	if !got.Fixed {
		t.Error("expected stairs to be Fixed (never picked up, dropped, or thrown)")
	}
}

func TestInitUpAndInitDownDoNotCollide(t *testing.T) {
	m := testMap()
	m.Seed = 55
	mapJSON, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal map: %v", err)
	}

	upOut, err := InitUp(mapJSON)
	if err != nil {
		t.Fatalf("InitUp: %v", err)
	}
	downOut, err := InitDown(mapJSON)
	if err != nil {
		t.Fatalf("InitDown: %v", err)
	}

	up, down := initResult(t, upOut), initResult(t, downOut)
	if up.ID == down.ID {
		t.Errorf("expected distinct IDs for stairs up/down, got %q twice", up.ID)
	}
}

func TestInitRejectsMapWithNoRooms(t *testing.T) {
	empty, err := json.Marshal(mapdata.Map{Width: 10, Height: 10})
	if err != nil {
		t.Fatalf("marshal empty map: %v", err)
	}
	if _, err := InitUp(empty); err == nil {
		t.Error("expected InitUp to error for a map with no rooms, got nil")
	}
	if _, err := InitDown(empty); err == nil {
		t.Error("expected InitDown to error for a map with no rooms, got nil")
	}
}

func TestInitIsDeterministicForTheSameMapSeed(t *testing.T) {
	m := testMap()
	m.Seed = 123
	mapJSON, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal map: %v", err)
	}

	out1, err := InitUp(mapJSON)
	if err != nil {
		t.Fatalf("InitUp (1): %v", err)
	}
	out2, err := InitUp(mapJSON)
	if err != nil {
		t.Fatalf("InitUp (2): %v", err)
	}

	s1, s2 := initResult(t, out1), initResult(t, out2)
	if s1.X != s2.X || s1.Y != s2.Y {
		t.Errorf("expected the same map seed to always spawn stairs up at the same position, got (%d,%d) and (%d,%d)",
			s1.X, s1.Y, s2.X, s2.Y)
	}
}

func TestKindOfReportsUpAndDown(t *testing.T) {
	m := testMap()
	m.Seed = 9
	mapJSON, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal map: %v", err)
	}

	upOut, err := InitUp(mapJSON)
	if err != nil {
		t.Fatalf("InitUp: %v", err)
	}
	downOut, err := InitDown(mapJSON)
	if err != nil {
		t.Fatalf("InitDown: %v", err)
	}

	gotUp, err := KindOf(upOut)
	if err != nil {
		t.Fatalf("KindOf(up): %v", err)
	}
	if gotUp != KindUp {
		t.Errorf("expected KindOf to report %q, got %q", KindUp, gotUp)
	}

	gotDown, err := KindOf(downOut)
	if err != nil {
		t.Fatalf("KindOf(down): %v", err)
	}
	if gotDown != KindDown {
		t.Errorf("expected KindOf to report %q, got %q", KindDown, gotDown)
	}
}
