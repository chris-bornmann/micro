package armor

import (
	"encoding/json"
	"testing"

	"micro/server/internal/item"
	"micro/server/internal/mapdata"
	"micro/server/internal/overlay"
)

func testMap() mapdata.Map {
	room := mapdata.Room{ID: 0, X: 5, Y: 5, W: 10, H: 10}
	return mapdata.Map{
		Width: 20, Height: 20,
		Rooms: []mapdata.Room{room},
	}
}

func initResult(t *testing.T, out []byte) armorItem {
	t.Helper()
	var got armorItem
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("unmarshal Init output: %v", err)
	}
	return got
}

func TestInitShoesOfSneakingPlacesItemOnTheFloorInARoom(t *testing.T) {
	m := testMap()
	m.Seed = 7
	mapJSON, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal map: %v", err)
	}

	out, err := InitShoesOfSneaking(mapJSON)
	if err != nil {
		t.Fatalf("InitShoesOfSneaking: %v", err)
	}
	got := initResult(t, out)

	if got.Glyph != '[' {
		t.Errorf("expected glyph '[', got %q", got.Glyph)
	}
	if got.Category != item.CategoryArmor {
		t.Errorf("expected category %q, got %q", item.CategoryArmor, got.Category)
	}
	if got.Usage != item.UsagePersistent {
		t.Errorf("expected usage %q, got %q", item.UsagePersistent, got.Usage)
	}
	if got.Name != "shoes of sneaking" {
		t.Errorf("expected name %q, got %q", "shoes of sneaking", got.Name)
	}
	if got.Kind != KindShoesOfSneaking {
		t.Errorf("expected kind %q, got %q", KindShoesOfSneaking, got.Kind)
	}
	if got.Slot != item.SlotFeet {
		t.Errorf("expected slot %q, got %q", item.SlotFeet, got.Slot)
	}
	if !got.Sneaking {
		t.Error("expected Sneaking true")
	}
	if got.Weight != 3 {
		t.Errorf("expected Weight 3, got %d", got.Weight)
	}

	r := m.Rooms[0]
	if got.X < r.X || got.X >= r.X+r.W || got.Y < r.Y || got.Y >= r.Y+r.H {
		t.Errorf("expected shoes at (%d,%d) to be inside the room", got.X, got.Y)
	}
}

func TestInitShoesOfSneakingRejectsMapWithNoRooms(t *testing.T) {
	empty, err := json.Marshal(mapdata.Map{Width: 10, Height: 10})
	if err != nil {
		t.Fatalf("marshal empty map: %v", err)
	}
	if _, err := InitShoesOfSneaking(empty); err == nil {
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

	out1, err := InitShoesOfSneaking(mapJSON)
	if err != nil {
		t.Fatalf("InitShoesOfSneaking (1): %v", err)
	}
	out2, err := InitShoesOfSneaking(mapJSON)
	if err != nil {
		t.Fatalf("InitShoesOfSneaking (2): %v", err)
	}

	a1, a2 := initResult(t, out1), initResult(t, out2)
	if a1.X != a2.X || a1.Y != a2.Y {
		t.Errorf("expected the same map seed to always spawn the shoes at the same position, got (%d,%d) and (%d,%d)",
			a1.X, a1.Y, a2.X, a2.Y)
	}
}

func TestStatsExtractsSlotAndSneakingFromSelf(t *testing.T) {
	self, err := json.Marshal(armorItem{
		Item: item.Item{
			Object:   overlay.Object{ID: "armor:42"},
			Category: item.CategoryArmor,
			Name:     "shoes of sneaking",
			Usage:    item.UsagePersistent,
		},
		Kind:     KindShoesOfSneaking,
		Slot:     item.SlotFeet,
		Sneaking: true,
	})
	if err != nil {
		t.Fatalf("marshal self: %v", err)
	}

	got, err := Stats(self)
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	want := item.ArmorStats{Slot: item.SlotFeet, Sneaking: true, Source: "armor:42"}
	if got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestStatsErrorsWithInvalidSelfJSON(t *testing.T) {
	if _, err := Stats([]byte("not json")); err == nil {
		t.Error("expected an error for invalid self JSON, got nil")
	}
}
