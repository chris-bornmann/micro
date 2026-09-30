package player

import (
	"encoding/json"
	"testing"

	"micromap/server/internal/action"
	"micromap/server/internal/item"
	"micromap/server/internal/overlay"
)

// testWeaponItemJSON marshals a minimal item.Item with CategoryWeapon and
// the given ID, standing in for a weapon's own last-returned raw state.
func testWeaponItemJSON(t *testing.T, id, name string) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(item.Item{
		Object:   overlay.Object{ID: id, Glyph: ')'},
		Category: item.CategoryWeapon,
		Name:     name,
		Usage:    item.UsagePersistent,
	})
	if err != nil {
		t.Fatalf("marshal item: %v", err)
	}
	return data
}

func testWeaponStatsJSON(t *testing.T, stats item.WeaponStats) []byte {
	t.Helper()
	data, err := json.Marshal(stats)
	if err != nil {
		t.Fatalf("marshal stats: %v", err)
	}
	return data
}

func TestWieldEquipsInventoryItemAndRemovesItFromInventory(t *testing.T) {
	self := invSelfJSON(t, overlay.Object{ID: "player:1"}, 15, testWeaponItemJSON(t, "weapon:1", "fine dagger"))
	stats := item.WeaponStats{Name: "fine dagger", Source: "weapon:1", BaseDamageMax: 4, Enchantment: 1}

	out, err := Wield(self, "weapon:1", testWeaponStatsJSON(t, stats))
	if err != nil {
		t.Fatalf("Wield: %v", err)
	}
	var got person
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(got.Inventory) != 0 {
		t.Errorf("expected weapon removed from Inventory, got %d entries", len(got.Inventory))
	}
	if got.Wielded == nil {
		t.Fatal("expected Wielded to be set")
	}
	if got.Wielded.Stats.Name != "fine dagger" || got.Wielded.Stats.BaseDamageMax != 4 || got.Wielded.Stats.Enchantment != 1 {
		t.Errorf("got Wielded.Stats %+v, want matching fine dagger stats", got.Wielded.Stats)
	}
}

func TestWieldSwapsPreviouslyWieldedItemBackIntoInventory(t *testing.T) {
	self, err := json.Marshal(person{
		Object:    overlay.Object{ID: "player:1"},
		HP:        15,
		Inventory: []json.RawMessage{testWeaponItemJSON(t, "weapon:2", "fine sword")},
		Wielded: &wieldedWeapon{
			Item:  testWeaponItemJSON(t, "weapon:1", "rusty dagger"),
			Stats: item.WeaponStats{Name: "rusty dagger", Source: "weapon:1", BaseDamageMax: 4},
		},
	})
	if err != nil {
		t.Fatalf("marshal self: %v", err)
	}

	secondStats := item.WeaponStats{Name: "fine sword", Source: "weapon:2", BaseDamageMax: 6}
	out, err := Wield(self, "weapon:2", testWeaponStatsJSON(t, secondStats))
	if err != nil {
		t.Fatalf("Wield: %v", err)
	}
	var got person
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Wielded == nil || got.Wielded.Stats.Name != "fine sword" {
		t.Fatalf("expected fine sword wielded, got %+v", got.Wielded)
	}
	if len(got.Inventory) != 1 {
		t.Fatalf("expected 1 inventory item (the swapped-out rusty dagger), got %d", len(got.Inventory))
	}
	var swappedOut item.Item
	if err := json.Unmarshal(got.Inventory[0], &swappedOut); err != nil {
		t.Fatalf("unmarshal swapped-out item: %v", err)
	}
	if swappedOut.ID != "weapon:1" {
		t.Errorf("expected swapped-out item ID %q, got %q", "weapon:1", swappedOut.ID)
	}
}

func TestWieldErrorsWhenIDNotFound(t *testing.T) {
	self := invSelfJSON(t, overlay.Object{ID: "player:1"}, 15)
	stats := item.WeaponStats{Name: "fine dagger", BaseDamageMax: 4}
	if _, err := Wield(self, "weapon:999", testWeaponStatsJSON(t, stats)); err == nil {
		t.Error("expected an error for a missing item ID, got nil")
	}
}

func TestWieldPreservesHPAndPosition(t *testing.T) {
	self := invSelfJSON(t, overlay.Object{ID: "player:1", X: 3, Y: 4}, 15, testWeaponItemJSON(t, "weapon:1", "fine dagger"))
	stats := item.WeaponStats{Name: "fine dagger", BaseDamageMax: 4}

	out, err := Wield(self, "weapon:1", testWeaponStatsJSON(t, stats))
	if err != nil {
		t.Fatalf("Wield: %v", err)
	}
	var got person
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.HP != 15 || got.X != 3 || got.Y != 4 {
		t.Errorf("expected HP/position untouched, got HP=%d (%d,%d)", got.HP, got.X, got.Y)
	}
}

func TestActionAttacksWithWieldedWeaponUsesItsStatsRange(t *testing.T) {
	m := testMap()
	start := overlay.Object{ID: "player:1", X: 7, Y: 7, Glyph: '@'}
	occupant := overlay.Object{ID: "goblin:1", X: 8, Y: 7, Glyph: 'g', Kind: "goblin"}

	stats := item.WeaponStats{Name: "fine dagger", BaseDamageMax: 4, Enchantment: 1, VsKind: map[string]int{"goblin": 1}}
	self, err := json.Marshal(person{
		Object: start,
		HP:     15,
		Wielded: &wieldedWeapon{
			Item:  testWeaponItemJSON(t, "weapon:1", "fine dagger"),
			Stats: stats,
		},
	})
	if err != nil {
		t.Fatalf("marshal self: %v", err)
	}

	out, err := Action(self, augmentedJSON(t, m, occupant), "Right")
	if err != nil {
		t.Fatalf("Action: %v", err)
	}
	got := actionResult(t, out)

	wantMin, wantMax := stats.Range("goblin")
	want := action.Result{Action: "attack", X: occupant.X, Y: occupant.Y, Weapon: "fine dagger", DamageMin: wantMin, DamageMax: wantMax}
	if got.Result != want {
		t.Errorf("got result %+v, want %+v", got.Result, want)
	}
}

func TestActionAttacksBareHandedWhenNothingWielded(t *testing.T) {
	m := testMap()
	start := overlay.Object{ID: "player:1", X: 7, Y: 7, Glyph: '@'}
	occupant := overlay.Object{ID: "goblin:1", X: 8, Y: 7, Glyph: 'g', Kind: "goblin"}

	out, err := Action(selfJSON(t, start, 15), augmentedJSON(t, m, occupant), "Right")
	if err != nil {
		t.Fatalf("Action: %v", err)
	}
	got := actionResult(t, out)
	want := action.Result{Action: "attack", X: occupant.X, Y: occupant.Y, Weapon: "hands", DamageMin: 1, DamageMax: 2}
	if got.Result != want {
		t.Errorf("got result %+v, want %+v", got.Result, want)
	}
}

func TestWieldedItemReturnsRawItemAndFlag(t *testing.T) {
	weaponJSON := testWeaponItemJSON(t, "weapon:1", "fine dagger")
	self, err := json.Marshal(person{
		Object:  overlay.Object{ID: "player:1"},
		HP:      15,
		Wielded: &wieldedWeapon{Item: weaponJSON, Stats: item.WeaponStats{Name: "fine dagger", BaseDamageMax: 4}},
	})
	if err != nil {
		t.Fatalf("marshal self: %v", err)
	}

	raw, ok, err := WieldedItem(self)
	if err != nil {
		t.Fatalf("WieldedItem: %v", err)
	}
	if !ok {
		t.Fatal("expected ok=true when something is wielded")
	}
	var got item.Item
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.ID != "weapon:1" {
		t.Errorf("expected wielded item ID %q, got %q", "weapon:1", got.ID)
	}
}

func TestWieldedItemReturnsFalseWhenNothingWielded(t *testing.T) {
	self := invSelfJSON(t, overlay.Object{ID: "player:1"}, 15)
	raw, ok, err := WieldedItem(self)
	if err != nil {
		t.Fatalf("WieldedItem: %v", err)
	}
	if ok {
		t.Errorf("expected ok=false, got true with raw=%s", raw)
	}
}

func TestDropRemovesWieldedItemAndClearsWielded(t *testing.T) {
	weaponJSON := testWeaponItemJSON(t, "weapon:1", "fine dagger")
	self, err := json.Marshal(person{
		Object:  overlay.Object{ID: "player:1"},
		HP:      15,
		Wielded: &wieldedWeapon{Item: weaponJSON, Stats: item.WeaponStats{Name: "fine dagger", BaseDamageMax: 4}},
	})
	if err != nil {
		t.Fatalf("marshal self: %v", err)
	}

	out, err := Drop(self, "weapon:1")
	if err != nil {
		t.Fatalf("Drop: %v", err)
	}
	var resp dropResponse
	if err := json.Unmarshal(out, &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !resp.WasWielded {
		t.Error("expected WasWielded=true")
	}
	if resp.Self.Wielded != nil {
		t.Error("expected Wielded cleared after dropping the wielded item")
	}
	var droppedItem item.Item
	if err := json.Unmarshal(resp.Item, &droppedItem); err != nil {
		t.Fatalf("unmarshal dropped item: %v", err)
	}
	if droppedItem.ID != "weapon:1" {
		t.Errorf("expected dropped item ID %q, got %q", "weapon:1", droppedItem.ID)
	}
}

func TestDropRemovesInventoryItemLeavingWieldedUntouched(t *testing.T) {
	wieldedJSON := testWeaponItemJSON(t, "weapon:1", "fine dagger")
	self, err := json.Marshal(person{
		Object:    overlay.Object{ID: "player:1"},
		HP:        15,
		Inventory: []json.RawMessage{testItemJSON(t, "potion:1", "healing potion")},
		Wielded:   &wieldedWeapon{Item: wieldedJSON, Stats: item.WeaponStats{Name: "fine dagger", BaseDamageMax: 4}},
	})
	if err != nil {
		t.Fatalf("marshal self: %v", err)
	}

	out, err := Drop(self, "potion:1")
	if err != nil {
		t.Fatalf("Drop: %v", err)
	}
	var resp dropResponse
	if err := json.Unmarshal(out, &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.WasWielded {
		t.Error("expected WasWielded=false when dropping an Inventory item")
	}
	if resp.Self.Wielded == nil || resp.Self.Wielded.Stats.Name != "fine dagger" {
		t.Errorf("expected Wielded untouched, got %+v", resp.Self.Wielded)
	}
	if len(resp.Self.Inventory) != 0 {
		t.Errorf("expected Inventory empty after dropping its only item, got %d", len(resp.Self.Inventory))
	}
}

func TestDropErrorsWhenIDMatchesNeither(t *testing.T) {
	self := invSelfJSON(t, overlay.Object{ID: "player:1"}, 15, testItemJSON(t, "potion:1", "healing potion"))
	if _, err := Drop(self, "potion:999"); err == nil {
		t.Error("expected an error for an ID matching neither Wielded nor Inventory, got nil")
	}
}
