package weapon

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

func initResult(t *testing.T, out []byte) weaponItem {
	t.Helper()
	var got weaponItem
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("unmarshal Init output: %v", err)
	}
	return got
}

func TestInitPlacesWeaponOnTheFloorInARoom(t *testing.T) {
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

	if got.Glyph != ')' {
		t.Errorf("expected glyph ')', got %q", got.Glyph)
	}
	if got.Category != item.CategoryWeapon {
		t.Errorf("expected category %q, got %q", item.CategoryWeapon, got.Category)
	}
	if got.Usage != item.UsagePersistent {
		t.Errorf("expected usage %q, got %q", item.UsagePersistent, got.Usage)
	}
	if got.Name != "fine dagger" {
		t.Errorf("expected name %q, got %q", "fine dagger", got.Name)
	}
	if got.Kind != KindDagger {
		t.Errorf("expected kind %q, got %q", KindDagger, got.Kind)
	}
	if got.BaseDamageMax != 4 {
		t.Errorf("expected BaseDamageMax 4, got %d", got.BaseDamageMax)
	}
	if got.Enchantment != 1 {
		t.Errorf("expected Enchantment 1, got %d", got.Enchantment)
	}
	if got.VsKind["goblin"] != 1 {
		t.Errorf("expected VsKind[goblin] 1, got %d", got.VsKind["goblin"])
	}
	if got.Weight != daggerWeight {
		t.Errorf("expected Weight %d, got %d", daggerWeight, got.Weight)
	}

	r := m.Rooms[0]
	if got.X < r.X || got.X >= r.X+r.W || got.Y < r.Y || got.Y >= r.Y+r.H {
		t.Errorf("expected weapon at (%d,%d) to be inside the room", got.X, got.Y)
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

	w1, w2 := initResult(t, out1), initResult(t, out2)
	if w1.X != w2.X || w1.Y != w2.Y {
		t.Errorf("expected the same map seed to always spawn the weapon at the same position, got (%d,%d) and (%d,%d)",
			w1.X, w1.Y, w2.X, w2.Y)
	}
}

func TestStatsExtractsWeaponStatsFromSelf(t *testing.T) {
	self, err := json.Marshal(weaponItem{
		Item: item.Item{
			Object:   overlay.Object{ID: "weapon:42"},
			Category: item.CategoryWeapon,
			Name:     "fine dagger",
			Usage:    item.UsagePersistent,
		},
		Kind:          KindDagger,
		BaseDamageMax: 4,
		Enchantment:   1,
		VsKind:        map[string]int{"goblin": 1},
	})
	if err != nil {
		t.Fatalf("marshal self: %v", err)
	}

	got, err := Stats(self)
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	want := item.WeaponStats{
		Name:          "fine dagger",
		Source:        "weapon:42",
		BaseDamageMax: 4,
		Enchantment:   1,
		VsKind:        map[string]int{"goblin": 1},
	}
	if got.Name != want.Name || got.Source != want.Source || got.BaseDamageMax != want.BaseDamageMax || got.Enchantment != want.Enchantment {
		t.Errorf("got %+v, want %+v", got, want)
	}
	if got.VsKind["goblin"] != 1 {
		t.Errorf("expected VsKind[goblin] 1, got %+v", got.VsKind)
	}
}

func TestStatsErrorsWithInvalidSelfJSON(t *testing.T) {
	if _, err := Stats([]byte("not json")); err == nil {
		t.Error("expected an error for invalid self JSON, got nil")
	}
}

func TestGoblinStarterReturnsCursedSword(t *testing.T) {
	got := GoblinStarter()
	if got.Name != "rusty sword" {
		t.Errorf("expected name %q, got %q", "rusty sword", got.Name)
	}
	if got.BaseDamageMax != 6 {
		t.Errorf("expected BaseDamageMax 6, got %d", got.BaseDamageMax)
	}
	if got.Enchantment != -1 {
		t.Errorf("expected Enchantment -1 (cursed), got %d", got.Enchantment)
	}
}

func TestFromStatsBuildsPickupableWeaponItem(t *testing.T) {
	stats := item.WeaponStats{
		Name:          "rusty sword",
		Source:        "weapon:goblin-starter",
		BaseDamageMax: 6,
		Enchantment:   -1,
		VsKind:        map[string]int{"player": 2},
	}

	out, err := FromStats(stats)
	if err != nil {
		t.Fatalf("FromStats: %v", err)
	}
	got := initResult(t, out)

	if got.Glyph != ')' {
		t.Errorf("expected glyph ')', got %q", got.Glyph)
	}
	if got.Category != item.CategoryWeapon {
		t.Errorf("expected category %q, got %q", item.CategoryWeapon, got.Category)
	}
	if got.Usage != item.UsagePersistent {
		t.Errorf("expected usage %q, got %q", item.UsagePersistent, got.Usage)
	}
	if got.Name != "rusty sword" {
		t.Errorf("expected name %q, got %q", "rusty sword", got.Name)
	}
	if got.BaseDamageMax != 6 {
		t.Errorf("expected BaseDamageMax 6, got %d", got.BaseDamageMax)
	}
	if got.Enchantment != -1 {
		t.Errorf("expected Enchantment -1, got %d", got.Enchantment)
	}
	if got.VsKind["player"] != 2 {
		t.Errorf("expected VsKind[player] 2, got %+v", got.VsKind)
	}
	if got.ID == "" {
		t.Error("expected a freshly minted, non-empty ID")
	}
	if got.Weight != swordWeight {
		t.Errorf("expected Weight %d (matching the sword constant), got %d", swordWeight, got.Weight)
	}
}

func TestFromStatsMintsUniqueIDsAcrossCalls(t *testing.T) {
	stats := item.WeaponStats{Name: "rusty sword", BaseDamageMax: 6}

	out1, err := FromStats(stats)
	if err != nil {
		t.Fatalf("FromStats (1): %v", err)
	}
	out2, err := FromStats(stats)
	if err != nil {
		t.Fatalf("FromStats (2): %v", err)
	}
	w1, w2 := initResult(t, out1), initResult(t, out2)
	if w1.ID == w2.ID {
		t.Errorf("expected distinct IDs across calls, got %q both times", w1.ID)
	}
}
