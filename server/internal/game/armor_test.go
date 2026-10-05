package game

import (
	"testing"

	"micro/server/internal/rules"
)

func TestHandleKeyWearThenDropShoesTogglesSneaking(t *testing.T) {
	shoes := testItem(t, "shoes_of_sneaking", 7, 7)
	state := newTestGameState(t, itemsTestMap(), testPlayer(t, 7, 7, 15), nil, []*rules.Entity{shoes})
	p := state.Player

	mustKey(t, state, ",")
	if msg := mustKey(t, state, "E"); msg != "You put on the shoes of sneaking." {
		t.Errorf("got %q", msg)
	}
	if !state.eng.Reg.Has(p, "sneaking") || p.Equipped[rules.SlotFeet] != shoes || len(p.Inventory) != 0 {
		t.Fatal("expected the shoes worn and the Player sneaking")
	}

	mustKey(t, state, "D")
	if msg := mustKey(t, state, "A"); msg != "You take off the shoes of sneaking and drop it." {
		t.Errorf("got %q", msg)
	}
	if state.eng.Reg.Has(p, "sneaking") || p.Equipped[rules.SlotFeet] != nil || len(state.FloorItems) != 1 {
		t.Error("expected the shoes on the floor and the Player no longer sneaking")
	}
}

func TestHandleKeyUseAndEquipPickersStayIndependent(t *testing.T) {
	potion, shoes := testItem(t, "healing_potion", 0, 0), testItem(t, "shoes_of_sneaking", 0, 0)
	state := newTestGameState(t, itemsTestMap(), carrying(testPlayer(t, 7, 7, 15), potion, shoes), nil, nil)

	// Armor does nothing when used, so only the potion is a use candidate
	// and "A" acts on it at once.
	mustKey(t, state, "A")
	if state.PendingSelection != nil || len(state.Player.Inventory) != 1 || state.Player.Inventory[0] != shoes {
		t.Errorf("expected only the potion used, inventory %v", names(state.Player.Inventory))
	}
}

// TestHandleKeyEquipOffersBothWeaponAndArmorCandidates: "E" is one command
// over weapons and armor together — one picker listing both kinds, where
// choosing a weapon wields it and choosing armor wears it.
func TestHandleKeyEquipOffersBothWeaponAndArmorCandidates(t *testing.T) {
	dagger := testItem(t, "fine_dagger", 0, 0)
	state := newTestGameState(t, itemsTestMap(), carrying(testPlayer(t, 7, 7, 15),
		testItem(t, "healing_potion", 0, 0), dagger, testItem(t, "shoes_of_sneaking", 0, 0), testItem(t, "shoes_of_sneaking", 0, 0)), nil, nil)

	mustKey(t, state, "E")
	if sel := state.PendingSelection; sel == nil || len(sel.Candidates) != 3 {
		t.Fatalf("expected a picker over the weapon and both shoes, got %+v", sel)
	}
	mustKey(t, state, "A")
	if state.Player.Weapon() != dagger {
		t.Fatal("expected the weapon candidate wielded")
	}
	mustKey(t, state, "E")
	if sel := state.PendingSelection; sel == nil || len(sel.Candidates) != 2 {
		t.Fatalf("expected a picker over the two shoes, got %+v", sel)
	}
	mustKey(t, state, "A")
	if state.Player.Equipped[rules.SlotFeet] == nil {
		t.Error("expected the armor candidate worn")
	}
}
