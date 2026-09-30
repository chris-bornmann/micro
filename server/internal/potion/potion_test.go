package potion

import (
	"encoding/json"
	"testing"

	"micromap/server/internal/item"
	"micromap/server/internal/mapdata"
	"micromap/server/internal/overlay"
)

func testMap() mapdata.Map {
	room := mapdata.Room{ID: 0, X: 5, Y: 5, W: 10, H: 10}
	return mapdata.Map{
		Width: 20, Height: 20,
		Rooms: []mapdata.Room{room},
	}
}

func initResult(t *testing.T, out []byte) potionItem {
	t.Helper()
	var got potionItem
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("unmarshal Init output: %v", err)
	}
	return got
}

func TestInitPlacesPotionOnTheFloorInARoom(t *testing.T) {
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

	if got.Glyph != '!' {
		t.Errorf("expected glyph '!', got %q", got.Glyph)
	}
	if got.Category != item.CategoryPotion {
		t.Errorf("expected category %q, got %q", item.CategoryPotion, got.Category)
	}
	if got.Usage != item.UsageOneShot {
		t.Errorf("expected usage %q, got %q", item.UsageOneShot, got.Usage)
	}
	if got.Name != "healing potion" {
		t.Errorf("expected name %q, got %q", "healing potion", got.Name)
	}
	if got.Kind != KindHealing {
		t.Errorf("expected kind %q, got %q", KindHealing, got.Kind)
	}
	if !got.Fragile {
		t.Error("expected a healing potion to be Fragile (shatters when thrown)")
	}
	if got.HealAmount < 8 || got.HealAmount > 15 {
		t.Errorf("expected HealAmount in [8,15], got %d", got.HealAmount)
	}
	if got.Weight != 1 {
		t.Errorf("expected Weight 1, got %d", got.Weight)
	}

	r := m.Rooms[0]
	if got.X < r.X || got.X >= r.X+r.W || got.Y < r.Y || got.Y >= r.Y+r.H {
		t.Errorf("expected potion at (%d,%d) to be inside the room", got.X, got.Y)
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

	p1, p2 := initResult(t, out1), initResult(t, out2)
	if p1.X != p2.X || p1.Y != p2.Y {
		t.Errorf("expected the same map seed to always spawn the potion at the same position, got (%d,%d) and (%d,%d)",
			p1.X, p1.Y, p2.X, p2.Y)
	}
	if p1.HealAmount != p2.HealAmount {
		t.Errorf("expected the same map seed to always roll the same heal amount, got %d and %d", p1.HealAmount, p2.HealAmount)
	}
}

func TestUseProducesHealEffectFromSelfsHealAmount(t *testing.T) {
	self, err := json.Marshal(potionItem{
		Item: item.Item{
			Object:   overlay.Object{ID: "potion:42"},
			Category: item.CategoryPotion,
			Name:     "healing potion",
			Usage:    item.UsageOneShot,
		},
		Kind:       KindHealing,
		HealAmount: 11,
	})
	if err != nil {
		t.Fatalf("marshal self: %v", err)
	}

	out, err := Use(self)
	if err != nil {
		t.Fatalf("Use: %v", err)
	}
	var resp struct {
		Effect item.Effect `json:"effect"`
	}
	if err := json.Unmarshal(out, &resp); err != nil {
		t.Fatalf("unmarshal Use output: %v", err)
	}

	want := item.Effect{Kind: item.EffectHeal, Amount: 11, Source: "potion:42"}
	if resp.Effect != want {
		t.Errorf("got effect %+v, want %+v", resp.Effect, want)
	}
}

func TestInitInvisibilityPlacesPotionOnTheFloorInARoom(t *testing.T) {
	m := testMap()
	m.Seed = 7
	mapJSON, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal map: %v", err)
	}

	out, err := InitInvisibility(mapJSON)
	if err != nil {
		t.Fatalf("InitInvisibility: %v", err)
	}
	got := initResult(t, out)

	if got.Glyph != '!' {
		t.Errorf("expected glyph '!', got %q", got.Glyph)
	}
	if got.Category != item.CategoryPotion {
		t.Errorf("expected category %q, got %q", item.CategoryPotion, got.Category)
	}
	if got.Usage != item.UsageOneShot {
		t.Errorf("expected usage %q, got %q", item.UsageOneShot, got.Usage)
	}
	if got.Name != "invisibility potion" {
		t.Errorf("expected name %q, got %q", "invisibility potion", got.Name)
	}
	if got.Kind != KindInvisibility {
		t.Errorf("expected kind %q, got %q", KindInvisibility, got.Kind)
	}
	if !got.Fragile {
		t.Error("expected an invisibility potion to be Fragile (shatters when thrown)")
	}
	if got.Duration < 10 || got.Duration > 30 {
		t.Errorf("expected Duration in [10,30], got %d", got.Duration)
	}
	if got.Weight != 1 {
		t.Errorf("expected Weight 1, got %d", got.Weight)
	}

	r := m.Rooms[0]
	if got.X < r.X || got.X >= r.X+r.W || got.Y < r.Y || got.Y >= r.Y+r.H {
		t.Errorf("expected potion at (%d,%d) to be inside the room", got.X, got.Y)
	}
}

func TestInitInvisibilityRejectsMapWithNoRooms(t *testing.T) {
	empty, err := json.Marshal(mapdata.Map{Width: 10, Height: 10})
	if err != nil {
		t.Fatalf("marshal empty map: %v", err)
	}
	if _, err := InitInvisibility(empty); err == nil {
		t.Error("expected an error for a map with no rooms, got nil")
	}
}

func TestInitInvisibilityIsDeterministicForTheSameMapSeed(t *testing.T) {
	m := testMap()
	m.Seed = 123
	mapJSON, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal map: %v", err)
	}

	out1, err := InitInvisibility(mapJSON)
	if err != nil {
		t.Fatalf("InitInvisibility (1): %v", err)
	}
	out2, err := InitInvisibility(mapJSON)
	if err != nil {
		t.Fatalf("InitInvisibility (2): %v", err)
	}

	p1, p2 := initResult(t, out1), initResult(t, out2)
	if p1.X != p2.X || p1.Y != p2.Y {
		t.Errorf("expected the same map seed to always spawn the potion at the same position, got (%d,%d) and (%d,%d)",
			p1.X, p1.Y, p2.X, p2.Y)
	}
	if p1.Duration != p2.Duration {
		t.Errorf("expected the same map seed to always roll the same duration, got %d and %d", p1.Duration, p2.Duration)
	}
}

func TestUseProducesInvisibilityEffectFromSelfsDuration(t *testing.T) {
	self, err := json.Marshal(potionItem{
		Item: item.Item{
			Object:   overlay.Object{ID: "potion:42"},
			Category: item.CategoryPotion,
			Name:     "invisibility potion",
			Usage:    item.UsageOneShot,
		},
		Kind:     KindInvisibility,
		Duration: 17,
	})
	if err != nil {
		t.Fatalf("marshal self: %v", err)
	}

	out, err := Use(self)
	if err != nil {
		t.Fatalf("Use: %v", err)
	}
	var resp struct {
		Effect item.Effect `json:"effect"`
	}
	if err := json.Unmarshal(out, &resp); err != nil {
		t.Fatalf("unmarshal Use output: %v", err)
	}

	want := item.Effect{Kind: item.EffectInvisibility, Duration: 17, Source: "potion:42"}
	if resp.Effect != want {
		t.Errorf("got effect %+v, want %+v", resp.Effect, want)
	}
}

func TestUseErrorsOnUnsupportedKind(t *testing.T) {
	self, err := json.Marshal(potionItem{
		Item: item.Item{Object: overlay.Object{ID: "potion:1"}, Category: item.CategoryPotion},
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
