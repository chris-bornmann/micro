package player

import (
	"encoding/json"
	"testing"

	"map_api/item"
	"map_api/overlay"
)

// testArmorItemJSON marshals a minimal item.Item with CategoryArmor and
// the given ID, standing in for a worn item's own last-returned raw
// state.
func testArmorItemJSON(t *testing.T, id, name string) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(item.Item{
		Object:   overlay.Object{ID: id, Glyph: '['},
		Category: item.CategoryArmor,
		Name:     name,
		Usage:    item.UsagePersistent,
	})
	if err != nil {
		t.Fatalf("marshal item: %v", err)
	}
	return data
}

func TestWearEquipsInventoryItemIntoItsSlot(t *testing.T) {
	self := invSelfJSON(t, overlay.Object{ID: "player:1"}, 15, testArmorItemJSON(t, "armor:1", "shoes of sneaking"))
	stats := item.ArmorStats{Slot: item.SlotFeet, Sneaking: true, Source: "armor:1"}
	statsJSON, err := json.Marshal(stats)
	if err != nil {
		t.Fatalf("marshal stats: %v", err)
	}

	out, err := Wear(self, "armor:1", statsJSON)
	if err != nil {
		t.Fatalf("Wear: %v", err)
	}
	var got person
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(got.Inventory) != 0 {
		t.Errorf("expected armor removed from Inventory, got %d entries", len(got.Inventory))
	}
	worn, ok := got.Worn[item.SlotFeet]
	if !ok {
		t.Fatal("expected Worn[feet] to be set")
	}
	if worn.Stats != stats {
		t.Errorf("got Worn stats %+v, want %+v", worn.Stats, stats)
	}
}

func TestWearSwapsPreviouslyWornItemInSameSlotBackIntoInventory(t *testing.T) {
	self, err := json.Marshal(person{
		Object:    overlay.Object{ID: "player:1"},
		HP:        15,
		Inventory: []json.RawMessage{testArmorItemJSON(t, "armor:2", "boots of speed")},
		Worn: map[item.Slot]*wornItem{
			item.SlotFeet: {
				Item:  testArmorItemJSON(t, "armor:1", "shoes of sneaking"),
				Stats: item.ArmorStats{Slot: item.SlotFeet, Sneaking: true, Source: "armor:1"},
			},
		},
	})
	if err != nil {
		t.Fatalf("marshal self: %v", err)
	}

	secondStats := item.ArmorStats{Slot: item.SlotFeet, Sneaking: false, Source: "armor:2"}
	statsJSON, err := json.Marshal(secondStats)
	if err != nil {
		t.Fatalf("marshal stats: %v", err)
	}

	out, err := Wear(self, "armor:2", statsJSON)
	if err != nil {
		t.Fatalf("Wear: %v", err)
	}
	var got person
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	worn, ok := got.Worn[item.SlotFeet]
	if !ok || worn.Stats.Source != "armor:2" {
		t.Fatalf("expected armor:2 worn in the feet slot, got %+v", got.Worn[item.SlotFeet])
	}
	if len(got.Inventory) != 1 {
		t.Fatalf("expected 1 inventory item (the swapped-out shoes), got %d", len(got.Inventory))
	}
	var swappedOut item.Item
	if err := json.Unmarshal(got.Inventory[0], &swappedOut); err != nil {
		t.Fatalf("unmarshal swapped-out item: %v", err)
	}
	if swappedOut.ID != "armor:1" {
		t.Errorf("expected swapped-out item ID %q, got %q", "armor:1", swappedOut.ID)
	}
}

func TestWearErrorsWhenIDNotFound(t *testing.T) {
	self := invSelfJSON(t, overlay.Object{ID: "player:1"}, 15)
	stats := item.ArmorStats{Slot: item.SlotFeet}
	statsJSON, err := json.Marshal(stats)
	if err != nil {
		t.Fatalf("marshal stats: %v", err)
	}
	if _, err := Wear(self, "armor:999", statsJSON); err == nil {
		t.Error("expected an error for a missing item ID, got nil")
	}
}

func TestWearSetsSneakingWhenStatsGrantIt(t *testing.T) {
	self := invSelfJSON(t, overlay.Object{ID: "player:1"}, 15, testArmorItemJSON(t, "armor:1", "shoes of sneaking"))
	stats := item.ArmorStats{Slot: item.SlotFeet, Sneaking: true, Source: "armor:1"}
	statsJSON, err := json.Marshal(stats)
	if err != nil {
		t.Fatalf("marshal stats: %v", err)
	}

	out, err := Wear(self, "armor:1", statsJSON)
	if err != nil {
		t.Fatalf("Wear: %v", err)
	}
	var got person
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !got.Sneaking {
		t.Error("expected Sneaking true")
	}
}

func TestWearLeavesSneakingFalseWhenStatsDontGrantIt(t *testing.T) {
	self := invSelfJSON(t, overlay.Object{ID: "player:1"}, 15, testArmorItemJSON(t, "armor:1", "plain boots"))
	stats := item.ArmorStats{Slot: item.SlotFeet, Sneaking: false, Source: "armor:1"}
	statsJSON, err := json.Marshal(stats)
	if err != nil {
		t.Fatalf("marshal stats: %v", err)
	}

	out, err := Wear(self, "armor:1", statsJSON)
	if err != nil {
		t.Fatalf("Wear: %v", err)
	}
	var got person
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Sneaking {
		t.Error("expected Sneaking to remain false for non-sneaking armor")
	}
	if got.Worn[item.SlotFeet] == nil {
		t.Error("expected the item to still be worn, just without granting sneaking")
	}
}

func TestWearPreservesHPAndPosition(t *testing.T) {
	self := invSelfJSON(t, overlay.Object{ID: "player:1", X: 3, Y: 4}, 15, testArmorItemJSON(t, "armor:1", "shoes of sneaking"))
	stats := item.ArmorStats{Slot: item.SlotFeet, Sneaking: true}
	statsJSON, err := json.Marshal(stats)
	if err != nil {
		t.Fatalf("marshal stats: %v", err)
	}

	out, err := Wear(self, "armor:1", statsJSON)
	if err != nil {
		t.Fatalf("Wear: %v", err)
	}
	var got person
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.HP != 15 || got.X != 3 || got.Y != 4 {
		t.Errorf("expected HP/position untouched, got HP=%d (%d,%d)", got.HP, got.X, got.Y)
	}
}

func TestWornItemsReturnsAllWornItemsSortedBySlot(t *testing.T) {
	self, err := json.Marshal(person{
		Object: overlay.Object{ID: "player:1"},
		HP:     15,
		Worn: map[item.Slot]*wornItem{
			"head": {Item: testArmorItemJSON(t, "armor:2", "a helm"), Stats: item.ArmorStats{Slot: "head"}},
			"feet": {Item: testArmorItemJSON(t, "armor:1", "shoes of sneaking"), Stats: item.ArmorStats{Slot: item.SlotFeet}},
		},
	})
	if err != nil {
		t.Fatalf("marshal self: %v", err)
	}

	got, err := WornItems(self)
	if err != nil {
		t.Fatalf("WornItems: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 worn items, got %d", len(got))
	}
	var first, second item.Item
	if err := json.Unmarshal(got[0], &first); err != nil {
		t.Fatalf("unmarshal first: %v", err)
	}
	if err := json.Unmarshal(got[1], &second); err != nil {
		t.Fatalf("unmarshal second: %v", err)
	}
	// "feet" sorts before "head" alphabetically.
	if first.ID != "armor:1" || second.ID != "armor:2" {
		t.Errorf("expected sorted order [armor:1, armor:2], got [%s, %s]", first.ID, second.ID)
	}
}

func TestWornItemsReturnsEmptyWhenNothingWorn(t *testing.T) {
	self := invSelfJSON(t, overlay.Object{ID: "player:1"}, 15)
	got, err := WornItems(self)
	if err != nil {
		t.Fatalf("WornItems: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected no worn items, got %d", len(got))
	}
}

func TestDropWornItemClearsSlotRecomputesSneakingAndReturnsFloorItem(t *testing.T) {
	self, err := json.Marshal(person{
		Object: overlay.Object{ID: "player:1", Sneaking: true},
		HP:     15,
		Worn: map[item.Slot]*wornItem{
			item.SlotFeet: {
				Item:  testArmorItemJSON(t, "armor:1", "shoes of sneaking"),
				Stats: item.ArmorStats{Slot: item.SlotFeet, Sneaking: true, Source: "armor:1"},
			},
		},
	})
	if err != nil {
		t.Fatalf("marshal self: %v", err)
	}

	out, err := Drop(self, "armor:1")
	if err != nil {
		t.Fatalf("Drop: %v", err)
	}
	var resp dropResponse
	if err := json.Unmarshal(out, &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !resp.WasWorn {
		t.Error("expected WasWorn=true")
	}
	if resp.WasWielded || resp.WasLitTorch {
		t.Error("expected WasWielded/WasLitTorch false")
	}
	var dropped item.Item
	if err := json.Unmarshal(resp.Item, &dropped); err != nil {
		t.Fatalf("unmarshal dropped item: %v", err)
	}
	if dropped.ID != "armor:1" {
		t.Errorf("expected dropped item ID %q, got %q", "armor:1", dropped.ID)
	}
	if resp.Self.Worn[item.SlotFeet] != nil {
		t.Error("expected the feet slot cleared after dropping it")
	}
	if resp.Self.Sneaking {
		t.Error("expected Sneaking cleared after dropping the only sneaking-granting item")
	}
}

func TestDropWornItemKeepsSneakingWhenAnotherSneakingItemRemains(t *testing.T) {
	self, err := json.Marshal(person{
		Object: overlay.Object{ID: "player:1", Sneaking: true},
		HP:     15,
		Worn: map[item.Slot]*wornItem{
			item.SlotFeet: {
				Item:  testArmorItemJSON(t, "armor:1", "shoes of sneaking"),
				Stats: item.ArmorStats{Slot: item.SlotFeet, Sneaking: true, Source: "armor:1"},
			},
			"head": {
				Item:  testArmorItemJSON(t, "armor:2", "hood of sneaking"),
				Stats: item.ArmorStats{Slot: "head", Sneaking: true, Source: "armor:2"},
			},
		},
	})
	if err != nil {
		t.Fatalf("marshal self: %v", err)
	}

	out, err := Drop(self, "armor:1")
	if err != nil {
		t.Fatalf("Drop: %v", err)
	}
	var resp dropResponse
	if err := json.Unmarshal(out, &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !resp.Self.Sneaking {
		t.Error("expected Sneaking to remain true while another sneaking-granting item is still worn")
	}
}
