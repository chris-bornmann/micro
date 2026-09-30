package main

import (
	"encoding/json"
	"testing"

	"map_api/armor"
	"map_api/item"
	"map_api/mapdata"
)

// armorAt is torchAt/scrollAt's counterpart for armor, built via
// armor.InitShoesOfSneaking and relocated to (x,y) by editing its
// generic position fields directly.
func armorAt(t *testing.T, m *mapdata.Map, x, y int) json.RawMessage {
	t.Helper()
	mapJSON, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal map: %v", err)
	}
	raw, err := armor.InitShoesOfSneaking(mapJSON)
	if err != nil {
		t.Fatalf("armor.InitShoesOfSneaking: %v", err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatalf("unmarshal armor: %v", err)
	}
	xJSON, err := json.Marshal(x)
	if err != nil {
		t.Fatalf("marshal x: %v", err)
	}
	yJSON, err := json.Marshal(y)
	if err != nil {
		t.Fatalf("marshal y: %v", err)
	}
	fields["X"] = xJSON
	fields["Y"] = yJSON
	out, err := json.Marshal(fields)
	if err != nil {
		t.Fatalf("marshal relocated armor: %v", err)
	}
	return out
}

func TestHandleKeyWearEquipsArmorAndSetsSneakingOnPlayerObject(t *testing.T) {
	m := itemsTestMap()
	armorJSON := armorAt(t, m, 7, 7)
	playerJSON := marshalPerson(t, testPerson{ID: "player:1", X: 7, Y: 7, HP: 15, MaxHP: 15})
	state := newTestGameState(m, playerJSON, []MonsterState{deadMonster(t, "goblin")}, []json.RawMessage{armorJSON})

	if _, err := handleKey(state, ","); err != nil {
		t.Fatalf("handleKey(\",\"): %v", err)
	}
	if _, err := handleKey(state, "E"); err != nil {
		t.Fatalf("handleKey(\"E\"): %v", err)
	}

	playerObj, err := playerObject(state.PlayerState)
	if err != nil {
		t.Fatalf("playerObject: %v", err)
	}
	if !playerObj.Sneaking {
		t.Error("expected Sneaking=true after wearing the shoes")
	}
	got := unmarshalPerson(t, state.PlayerState)
	if got.Worn[item.SlotFeet] == nil {
		t.Fatal("expected Worn[feet] set")
	}
	if len(got.Inventory) != 0 {
		t.Errorf("expected the shoes removed from Inventory, got %d entries", len(got.Inventory))
	}
}

func TestApplyDropWornItemCreatesFloorItemAndClearsSneaking(t *testing.T) {
	m := itemsTestMap()
	armorJSON := armorAt(t, m, 7, 7)
	playerJSON := marshalPerson(t, testPerson{ID: "player:1", X: 7, Y: 7, HP: 15, MaxHP: 15})
	state := newTestGameState(m, playerJSON, []MonsterState{deadMonster(t, "goblin")}, []json.RawMessage{armorJSON})

	if _, err := handleKey(state, ","); err != nil {
		t.Fatalf("handleKey(\",\"): %v", err)
	}
	if _, err := handleKey(state, "E"); err != nil {
		t.Fatalf("handleKey(\"E\"): %v", err)
	}
	if len(state.FloorItems) != 0 {
		t.Fatalf("expected floor empty after pickup, got %d", len(state.FloorItems))
	}

	if _, err := handleKey(state, "D"); err != nil {
		t.Fatalf("handleKey(\"D\") open drop picker: %v", err)
	}
	if state.PendingSelection == nil {
		t.Fatal("expected the drop picker open")
	}
	if _, err := handleKey(state, "A"); err != nil {
		t.Fatalf("handleKey(\"A\") drop the worn shoes: %v", err)
	}

	if len(state.FloorItems) != 1 {
		t.Errorf("expected a floor item created from dropping the worn shoes, got %d", len(state.FloorItems))
	}
	playerObj, err := playerObject(state.PlayerState)
	if err != nil {
		t.Fatalf("playerObject: %v", err)
	}
	if playerObj.Sneaking {
		t.Error("expected Sneaking cleared after dropping the shoes")
	}
	got := unmarshalPerson(t, state.PlayerState)
	if got.Worn[item.SlotFeet] != nil {
		t.Error("expected the feet slot cleared")
	}
}

func TestHandleKeyUseAndEquipPickersStayIndependent(t *testing.T) {
	m := itemsTestMap()
	potionJSON := potionAt(t, m, 7, 7)
	armorJSON := armorAt(t, m, 7, 7)
	playerJSON := marshalPerson(t, testPerson{ID: "player:1", X: 7, Y: 7, HP: 15, MaxHP: 15, Inventory: []json.RawMessage{potionJSON, armorJSON}})
	state := newTestGameState(m, playerJSON, []MonsterState{deadMonster(t, "goblin")}, nil)

	// The armor is persistent-use, not one-shot, so it's excluded from
	// the 'A' picker's candidacy entirely — with only the potion
	// qualifying, useItem auto-resolves it immediately (no picker).
	if _, err := handleKey(state, "A"); err != nil {
		t.Fatalf("handleKey(\"A\"): %v", err)
	}
	if state.PendingSelection != nil {
		t.Fatalf("expected no picker (armor excluded, only the potion qualifies), got %+v", state.PendingSelection)
	}
	got := unmarshalPerson(t, state.PlayerState)
	if len(got.Inventory) != 1 {
		t.Fatalf("expected only the potion consumed, 1 item (the armor) remaining, got %d", len(got.Inventory))
	}
	var remaining item.Item
	if err := json.Unmarshal(got.Inventory[0], &remaining); err != nil {
		t.Fatalf("unmarshal remaining item: %v", err)
	}
	if remaining.Category != item.CategoryArmor {
		t.Errorf("expected the remaining item to be the armor, got category %q", remaining.Category)
	}
}

// TestHandleKeyEquipOffersBothWeaponAndArmorCandidates is the direct
// regression test for consolidating "wield" (weapon-only) and "wear"
// (armor-only) into one "equip" command: with a potion (not equippable),
// a weapon, and two armor pieces carried, "E" must open a single picker
// listing all three equippable items — weapon and armor mixed together —
// not just one category. Resolving the weapon's letter must wield it;
// resolving an armor letter must wear it.
func TestHandleKeyEquipOffersBothWeaponAndArmorCandidates(t *testing.T) {
	m := itemsTestMap()
	potionJSON := potionAt(t, m, 7, 7)
	weaponJSON := weaponAt(t, m, 7, 7)
	armorJSON1 := armorAt(t, m, 7, 7)
	armorJSON2 := armorAt(t, m, 7, 7)
	playerJSON := marshalPerson(t, testPerson{
		ID: "player:1", X: 7, Y: 7, HP: 15, MaxHP: 15,
		Inventory: []json.RawMessage{potionJSON, weaponJSON, armorJSON1, armorJSON2},
	})
	state := newTestGameState(m, playerJSON, []MonsterState{deadMonster(t, "goblin")}, nil)

	if _, err := handleKey(state, "E"); err != nil {
		t.Fatalf("handleKey(\"E\"): %v", err)
	}
	if state.PendingSelection == nil {
		t.Fatal("expected the equip picker open with 3 candidates")
	}
	if len(state.PendingSelection.Candidates) != 3 {
		t.Errorf("expected exactly 3 candidates (weapon + 2 armor, potion excluded), got %d", len(state.PendingSelection.Candidates))
	}

	// Resolve the first candidate (the weapon, per Inventory order) and
	// confirm it was wielded, not worn.
	if _, err := handleKey(state, "A"); err != nil {
		t.Fatalf("handleKey(\"A\") resolve weapon candidate: %v", err)
	}
	got := unmarshalPerson(t, state.PlayerState)
	if got.Wielded == nil {
		t.Error("expected the weapon candidate to be wielded")
	}

	// Re-open the picker for the two remaining armor candidates and
	// resolve one, confirming it was worn, not wielded.
	if _, err := handleKey(state, "E"); err != nil {
		t.Fatalf("handleKey(\"E\") re-open picker: %v", err)
	}
	if state.PendingSelection == nil || len(state.PendingSelection.Candidates) != 2 {
		t.Fatalf("expected the equip picker open with 2 remaining armor candidates, got %+v", state.PendingSelection)
	}
	if _, err := handleKey(state, "A"); err != nil {
		t.Fatalf("handleKey(\"A\") resolve armor candidate: %v", err)
	}
	got = unmarshalPerson(t, state.PlayerState)
	if got.Worn[item.SlotFeet] == nil {
		t.Error("expected the armor candidate to be worn")
	}
}
