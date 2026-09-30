package scroll

import (
	"encoding/json"
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

func initResult(t *testing.T, out []byte) scrollItem {
	t.Helper()
	var got scrollItem
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("unmarshal Init output: %v", err)
	}
	return got
}

func TestInitTeleportPlacesScrollOnTheFloorInARoom(t *testing.T) {
	m := testMap()
	m.Seed = 7
	mapJSON, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal map: %v", err)
	}

	out, err := InitTeleport(mapJSON)
	if err != nil {
		t.Fatalf("InitTeleport: %v", err)
	}
	got := initResult(t, out)

	if got.Glyph != '?' {
		t.Errorf("expected glyph '?', got %q", got.Glyph)
	}
	if got.Category != item.CategoryScroll {
		t.Errorf("expected category %q, got %q", item.CategoryScroll, got.Category)
	}
	if got.Usage != item.UsageOneShot {
		t.Errorf("expected usage %q, got %q", item.UsageOneShot, got.Usage)
	}
	if got.Name != "scroll of teleportation" {
		t.Errorf("expected name %q, got %q", "scroll of teleportation", got.Name)
	}
	if got.Kind != KindTeleport {
		t.Errorf("expected kind %q, got %q", KindTeleport, got.Kind)
	}
	if got.Weight != 1 {
		t.Errorf("expected Weight 1, got %d", got.Weight)
	}

	r := m.Rooms[0]
	if got.X < r.X || got.X >= r.X+r.W || got.Y < r.Y || got.Y >= r.Y+r.H {
		t.Errorf("expected scroll at (%d,%d) to be inside the room", got.X, got.Y)
	}
}

func TestInitTeleportRejectsMapWithNoRooms(t *testing.T) {
	empty, err := json.Marshal(mapdata.Map{Width: 10, Height: 10})
	if err != nil {
		t.Fatalf("marshal empty map: %v", err)
	}
	if _, err := InitTeleport(empty); err == nil {
		t.Error("expected an error for a map with no rooms, got nil")
	}
}

func TestInitTeleportIsDeterministicForTheSameMapSeed(t *testing.T) {
	m := testMap()
	m.Seed = 123
	mapJSON, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal map: %v", err)
	}

	out1, err := InitTeleport(mapJSON)
	if err != nil {
		t.Fatalf("InitTeleport (1): %v", err)
	}
	out2, err := InitTeleport(mapJSON)
	if err != nil {
		t.Fatalf("InitTeleport (2): %v", err)
	}

	s1, s2 := initResult(t, out1), initResult(t, out2)
	if s1.X != s2.X || s1.Y != s2.Y {
		t.Errorf("expected the same map seed to always spawn the scroll at the same position, got (%d,%d) and (%d,%d)",
			s1.X, s1.Y, s2.X, s2.Y)
	}
}

func TestUseTeleportReturnsEffectWithNoDestinationFilledIn(t *testing.T) {
	m := testMap()
	m.Seed = 42
	mapJSON, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal map: %v", err)
	}
	out, err := InitTeleport(mapJSON)
	if err != nil {
		t.Fatalf("InitTeleport: %v", err)
	}
	spawned := initResult(t, out)

	useOut, err := Use(out)
	if err != nil {
		t.Fatalf("Use: %v", err)
	}
	var resp useResponse
	if err := json.Unmarshal(useOut, &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Effect.Kind != item.EffectTeleport {
		t.Errorf("expected effect kind %q, got %q", item.EffectTeleport, resp.Effect.Kind)
	}
	if resp.Effect.Source != spawned.ID {
		t.Errorf("expected effect source %q, got %q", spawned.ID, resp.Effect.Source)
	}
	if resp.Effect.X != 0 || resp.Effect.Y != 0 {
		t.Errorf("expected no destination filled in by this package, got (%d,%d)", resp.Effect.X, resp.Effect.Y)
	}
}

func TestUseErrorsOnUnsupportedKind(t *testing.T) {
	self, err := json.Marshal(scrollItem{
		Item: item.Item{Category: item.CategoryScroll, Usage: item.UsageOneShot},
		Kind: Kind("unknown"),
	})
	if err != nil {
		t.Fatalf("marshal self: %v", err)
	}
	if _, err := Use(self); err == nil {
		t.Error("expected an error for an unsupported kind, got nil")
	}
}

func TestUseErrorsWithInvalidSelfJSON(t *testing.T) {
	if _, err := Use([]byte("not json")); err == nil {
		t.Error("expected an error for invalid self JSON, got nil")
	}
}
