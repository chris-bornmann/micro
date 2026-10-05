package game

import (
	"slices"
	"strconv"
	"strings"
	"testing"

	"micro/server/internal/rules"
)

// healAmount is what a healing potion was rolled to heal when it was made.
func healAmount(potion *rules.Entity) int { return potion.Uses[0].Params.Int("amount") }

// grantTurns is how long a granting potion lasts.
func grantTurns(potion *rules.Entity) int { return potion.Uses[0].Params.Int("turns") }

func TestPickUpMovesItemFromFloorToPlayerInventory(t *testing.T) {
	potion := testItem(t, "healing_potion", 7, 7)
	state := newTestGameState(t, itemsTestMap(), testPlayer(t, 7, 7, 15), nil, []*rules.Entity{potion})

	msgs, spent := pickUp(state)
	if !spent || joinMessages(msgs) != "You pick up the healing potion." {
		t.Errorf("got %q, spent %v", msgs, spent)
	}
	if len(state.FloorItems) != 0 || len(state.Player.Inventory) != 1 || state.Player.Inventory[0] != potion {
		t.Error("expected the potion to move from the floor into the inventory")
	}
}

func TestPickUpReportsNothingHereWhenTileIsEmpty(t *testing.T) {
	state := newTestGameState(t, itemsTestMap(), testPlayer(t, 7, 7, 15), nil, []*rules.Entity{testItem(t, "healing_potion", 9, 9)})
	msgs, spent := pickUp(state)
	if !spent || joinMessages(msgs) != "There is nothing here to pick up." || len(state.FloorItems) != 1 {
		t.Errorf("got %q, spent %v, floor %d", msgs, spent, len(state.FloorItems))
	}
}

func TestPickUpReportsInventoryFullAndLeavesItemOnFloor(t *testing.T) {
	p := testPlayer(t, 7, 7, 15)
	for i := 0; i < rules.InventoryCapacity; i++ {
		carrying(p, testItem(t, "healing_potion", 0, 0))
	}
	state := newTestGameState(t, itemsTestMap(), p, nil, []*rules.Entity{testItem(t, "healing_potion", 7, 7)})

	if msg := mustKey(t, state, ","); msg != "Your inventory is full." {
		t.Errorf("got %q", msg)
	}
	if len(state.FloorItems) != 1 || len(p.Inventory) != rules.InventoryCapacity {
		t.Error("expected the item to stay on the floor and the inventory to stay full")
	}
	if state.Turn != 1 {
		t.Error("bumping into a full pack still spends the turn")
	}
}

func TestUseItemAppliesEffectRemovesItFromInventoryAndRaisesHP(t *testing.T) {
	potion := testItem(t, "healing_potion", 0, 0)
	p := carrying(testPlayer(t, 7, 7, 100), potion)
	p.HP = 10
	state := newTestGameState(t, itemsTestMap(), p, nil, nil)
	heal := healAmount(potion)

	msgs, spent := useItem(state)
	want := "You drink the healing potion and recover " + strconv.Itoa(heal) + " HP (" + strconv.Itoa(10+heal) + " HP now)."
	if !spent || joinMessages(msgs) != want {
		t.Errorf("got %q, want %q", msgs, want)
	}
	if p.HP != 10+heal || len(p.Inventory) != 0 {
		t.Errorf("HP %d, inventory %d", p.HP, len(p.Inventory))
	}
}

func TestUseItemReportsNothingToUseWhenInventoryIsEmpty(t *testing.T) {
	state := newTestGameState(t, itemsTestMap(), testPlayer(t, 7, 7, 10), nil, nil)
	if msgs, spent := useItem(state); !spent || joinMessages(msgs) != "You have nothing to use." {
		t.Errorf("got %q", msgs)
	}
}

func TestHandleKeyPickupThenUseEndToEnd(t *testing.T) {
	potion := testItem(t, "healing_potion", 7, 7)
	p := testPlayer(t, 7, 7, 100)
	p.HP = 10
	state := newTestGameState(t, itemsTestMap(), p, nil, []*rules.Entity{potion})

	mustKey(t, state, ",")
	if len(state.FloorItems) != 0 || len(p.Inventory) != 1 {
		t.Fatal("expected the potion picked up")
	}
	mustKey(t, state, "A")
	if len(p.Inventory) != 0 || p.HP != 10+healAmount(potion) {
		t.Errorf("HP %d, inventory %d", p.HP, len(p.Inventory))
	}
}

func TestHandleKeyUseInvisibilityPotionSetsInvisibleImmediately(t *testing.T) {
	potion := testItem(t, "invisibility_potion", 7, 7)
	state := newTestGameState(t, itemsTestMap(), testPlayer(t, 7, 7, 10), nil, []*rules.Entity{potion})
	turns := grantTurns(potion)

	mustKey(t, state, ",")
	msg := mustKey(t, state, "A")
	if want := "You drink the invisibility potion and turn invisible."; msg != want {
		t.Errorf("got %q, want %q", msg, want)
	}
	if !state.eng.Reg.Has(state.Player, "invisible") {
		t.Error("expected the Player to be invisible")
	}
	// One tick has already elapsed: the turn the potion was drunk.
	if inst := state.Player.FindTrait("invisible"); inst == nil || inst.Turns != turns-1 {
		t.Errorf("expected %d turns left, got %+v", turns-1, inst)
	}
}

func TestHandleKeyPickupUseInvisibilityThenTickExpiresEndToEnd(t *testing.T) {
	potion := testItem(t, "invisibility_potion", 7, 7)
	state := newTestGameState(t, itemsTestMap(), testPlayer(t, 7, 7, 10), nil, []*rules.Entity{potion})
	turns := grantTurns(potion)

	mustKey(t, state, ",")
	mustKey(t, state, "A")
	// One tick already elapsed on the turn the potion was drunk, so
	// turns-2 further waits keep it active; the next one ends it.
	for i := 0; i < turns-2; i++ {
		mustKey(t, state, ".")
		if !state.eng.Reg.Has(state.Player, "invisible") {
			t.Fatalf("wait %d: expected to still be invisible", i)
		}
	}
	if msg := mustKey(t, state, "."); msg != "Your invisibility wears off." {
		t.Errorf("got %q", msg)
	}
	if state.eng.Reg.Has(state.Player, "invisible") || state.Player.FindTrait("invisible") != nil {
		t.Error("expected invisibility gone")
	}
}

func TestEquipItemEquipsWeaponFromInventoryAndRemovesItFromInventory(t *testing.T) {
	dagger := testItem(t, "fine_dagger", 0, 0)
	state := newTestGameState(t, itemsTestMap(), carrying(testPlayer(t, 7, 7, 15), dagger), nil, nil)

	msgs, spent := equipItem(state)
	if !spent || joinMessages(msgs) != "You wield the fine dagger." {
		t.Errorf("got %q", msgs)
	}
	if len(state.Player.Inventory) != 0 || state.Player.Weapon() != dagger {
		t.Error("expected the dagger wielded, out of the inventory")
	}
}

func TestEquipItemReportsNothingToEquipWhenInventoryHasNothingEquippable(t *testing.T) {
	state := newTestGameState(t, itemsTestMap(), carrying(testPlayer(t, 7, 7, 15), testItem(t, "healing_potion", 0, 0)), nil, nil)
	if msgs, spent := equipItem(state); !spent || joinMessages(msgs) != "You have nothing to equip." {
		t.Errorf("got %q", msgs)
	}
}

func TestHandleKeyPickupThenWieldEndToEnd(t *testing.T) {
	state := newTestGameState(t, itemsTestMap(), testPlayer(t, 7, 7, 15), nil, []*rules.Entity{testItem(t, "fine_dagger", 7, 7)})
	mustKey(t, state, ",")
	if msg := mustKey(t, state, "E"); msg != "You wield the fine dagger." || state.Player.Weapon() == nil {
		t.Errorf("got %q", msg)
	}
}

func TestUseItemSkipsWeaponAndUsesTheOnlyUsableItem(t *testing.T) {
	dagger, potion := testItem(t, "fine_dagger", 0, 0), testItem(t, "healing_potion", 0, 0)
	p := carrying(testPlayer(t, 7, 7, 100), dagger, potion)
	p.HP = 10
	state := newTestGameState(t, itemsTestMap(), p, nil, nil)

	if _, spent := useItem(state); !spent || state.PendingSelection != nil {
		t.Fatal("with one usable item, use should act at once")
	}
	if len(p.Inventory) != 1 || p.Inventory[0] != dagger || p.HP != 10+healAmount(potion) {
		t.Errorf("expected only the potion used; inventory %v, HP %d", names(p.Inventory), p.HP)
	}
}

func TestHandleKeyAttackDamageChangesAfterWieldingWeapon(t *testing.T) {
	g := testMonster(t, "goblin", 8, 7, 1000)
	state := newTestGameState(t, itemsTestMap(), testPlayer(t, 7, 7, 1000), []*rules.Entity{g}, []*rules.Entity{testItem(t, "fine_dagger", 7, 7)})
	mustKey(t, state, ",")
	mustKey(t, state, "E")

	// Moving Right into the adjacent goblin attacks it. Every hit must land
	// in the dagger's range against a goblin — base 1-4, +1 enchantment, +1
	// against goblins — never bare hands' 1-2.
	for i := 0; i < 20; i++ {
		before := g.HP
		mustKey(t, state, "Right")
		if d := before - g.HP; d < 3 || d > 6 {
			t.Fatalf("attack %d: damage %d outside [3,6]", i, d)
		}
	}
}

func TestUseAndEquipCandidates(t *testing.T) {
	cases := []struct {
		name       string
		carried    []string
		use, equip int
	}{
		{"none", nil, 0, 0},
		{"potion and weapon", []string{"healing_potion", "fine_dagger"}, 1, 1},
		{"two potions", []string{"healing_potion", "invisibility_potion"}, 2, 0},
		{"two weapons", []string{"fine_dagger", "fine_dagger"}, 0, 2},
		{"weapon, armor and potion", []string{"healing_potion", "fine_dagger", "shoes_of_sneaking"}, 1, 2},
		{"torch and flask", []string{"torch", "flask_of_oil"}, 1, 1}, // a torch can be wielded
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := testPlayer(t, 7, 7, 15)
			for _, id := range c.carried {
				carrying(p, testItem(t, id, 0, 0))
			}
			state := newTestGameState(t, itemsTestMap(), p, nil, nil)
			if got := len(useCandidates(state)); got != c.use {
				t.Errorf("%d use candidates, want %d", got, c.use)
			}
			if got := len(equipCandidates(state)); got != c.equip {
				t.Errorf("%d equip candidates, want %d", got, c.equip)
			}
		})
	}
}

func TestUseItemOpensPickerWithTwoUsableCandidates(t *testing.T) {
	state := newTestGameState(t, itemsTestMap(), carrying(testPlayer(t, 7, 7, 15),
		testItem(t, "healing_potion", 0, 0), testItem(t, "invisibility_potion", 0, 0)), nil, nil)

	msgs, spent := useItem(state)
	if spent || msgs != nil {
		t.Fatalf("opening a picker is silent and spends nothing; got %q, spent %v", msgs, spent)
	}
	if sel := state.PendingSelection; sel == nil || sel.Action != pendingUse || len(sel.Candidates) != 2 {
		t.Fatalf("expected a use picker over 2 candidates, got %+v", sel)
	}
	if len(state.Player.Inventory) != 2 {
		t.Error("opening a picker must not change the inventory")
	}
}

func TestEquipItemOpensPickerWithTwoWeaponCandidates(t *testing.T) {
	state := newTestGameState(t, itemsTestMap(), carrying(testPlayer(t, 7, 7, 15),
		testItem(t, "fine_dagger", 0, 0), testItem(t, "fine_dagger", 0, 0)), nil, nil)
	if _, spent := equipItem(state); spent {
		t.Fatal("opening a picker spends no turn")
	}
	if sel := state.PendingSelection; sel == nil || sel.Action != pendingEquip || len(sel.Candidates) != 2 {
		t.Fatalf("expected an equip picker over 2 candidates, got %+v", sel)
	}
}

func TestHandleKeyPendingSelectionValidLetterCompletesChosenItem(t *testing.T) {
	first, second := testItem(t, "fine_dagger", 0, 0), testItem(t, "fine_dagger", 0, 0)
	state := newTestGameState(t, itemsTestMap(), carrying(testPlayer(t, 7, 7, 15), first, second), nil, nil)

	mustKey(t, state, "E")
	if state.PendingSelection == nil {
		t.Fatal("expected a picker with 2 candidates")
	}
	if msg := mustKey(t, state, "B"); msg != "You wield the fine dagger." {
		t.Errorf("got %q", msg)
	}
	if state.PendingSelection != nil || state.Player.Weapon() != second || !slices.Equal(state.Player.Inventory, []*rules.Entity{first}) {
		t.Error("expected the SECOND dagger wielded and the first still carried")
	}
}

func TestHandleKeyPendingSelectionInvalidKeyCancelsWithoutConsumingTurn(t *testing.T) {
	state := newTestGameState(t, itemsTestMap(), carrying(testPlayer(t, 7, 7, 15),
		testItem(t, "fine_dagger", 0, 0), testItem(t, "fine_dagger", 0, 0)), nil, nil)

	mustKey(t, state, "E")
	if msg := mustKey(t, state, "Z"); msg != "" {
		t.Errorf("cancelling should be silent, got %q", msg)
	}
	if state.PendingSelection != nil || state.Player.Weapon() != nil || len(state.Player.Inventory) != 2 || state.Turn != 0 {
		t.Error("expected the picker cancelled with nothing changed and no turn spent")
	}
}

func TestHandleKeyPickerQuaffsTheChosenPotion(t *testing.T) {
	heal, invis := testItem(t, "healing_potion", 0, 0), testItem(t, "invisibility_potion", 0, 0)
	p := carrying(testPlayer(t, 7, 7, 100), heal, invis)
	p.HP = 10
	state := newTestGameState(t, itemsTestMap(), p, nil, nil)

	mustKey(t, state, "A")
	msg := mustKey(t, state, "B")
	if want := "You drink the invisibility potion and turn invisible."; msg != want {
		t.Errorf("got %q, want %q", msg, want)
	}
	if !state.eng.Reg.Has(p, "invisible") || p.HP != 10 || !slices.Equal(p.Inventory, []*rules.Entity{heal}) {
		t.Error("expected only the invisibility potion drunk")
	}
}

func TestInventoryLinesFormatsLetteredNamesAndMarksEquipment(t *testing.T) {
	state := newTestGameState(t, itemsTestMap(), testPlayer(t, 7, 7, 15), nil, nil)
	if got := inventoryLines(state); !slices.Equal(got, []string{"You are carrying nothing."}) {
		t.Errorf("got %v", got)
	}

	p := state.Player
	carrying(p, testItem(t, "fine_dagger", 0, 0), testItem(t, "healing_potion", 0, 0), testItem(t, "shoes_of_sneaking", 0, 0))
	if got, want := inventoryLines(state), []string{"A) fine dagger", "B) healing potion", "C) shoes of sneaking"}; !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}

	c := state.ctx()
	c.Equip(p, p.Inventory[2])
	c.Equip(p, p.Inventory[0])
	torch := carrying(p, testItem(t, "torch", 0, 0)).Inventory[1]
	rules.Light(p, torch, 10)
	// Equipped first — weapon, light, armor — then the pack.
	want := []string{"A) fine dagger (wielded)", "B) torch (held, lit)", "C) shoes of sneaking (worn)", "D) healing potion"}
	if got := inventoryLines(state); !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// TestHelpLinesMentionsEveryCommand is a lightweight guard against
// helpLines drifting out of sync with handleKeyMessages and the clients'
// key bindings — those are the actual source of truth, so this only checks
// each documented command's key still appears somewhere.
func TestHelpLinesMentionsEveryCommand(t *testing.T) {
	joined := strings.Join(helpLines(), "\n")
	for _, key := range []string{"Arrow keys", ".", ",", "<", ">", "A", "E", "D", "T", "I", "L", "S", "?", "Escape", "Q"} {
		if !strings.Contains(joined, key) {
			t.Errorf("expected helpLines to mention %q", key)
		}
	}
}

func TestCandidateLettersProducesUppercaseSequenceUpToCapacity(t *testing.T) {
	if got := candidateLetters(0); len(got) != 0 {
		t.Errorf("candidateLetters(0) = %v", got)
	}
	if got := candidateLetters(3); !slices.Equal(got, []string{"A", "B", "C"}) {
		t.Errorf("candidateLetters(3) = %v", got)
	}
	if got := candidateLetters(rules.InventoryCapacity); len(got) != rules.InventoryCapacity || got[len(got)-1] != "J" {
		t.Errorf("candidateLetters(%d) = %v", rules.InventoryCapacity, got)
	}
}

func TestDropItemAlwaysOpensPickerEvenWithOneItem(t *testing.T) {
	state := newTestGameState(t, itemsTestMap(), carrying(testPlayer(t, 7, 7, 15), testItem(t, "healing_potion", 0, 0)), nil, nil)
	if msgs, spent := dropItem(state); spent || msgs != nil {
		t.Fatalf("got %q, spent %v", msgs, spent)
	}
	if sel := state.PendingSelection; sel == nil || sel.Action != pendingDrop || len(sel.Candidates) != 1 {
		t.Fatalf("expected a drop picker over 1 candidate, got %+v", sel)
	}
}

func TestDropItemReportsNothingToDropWhenEmpty(t *testing.T) {
	state := newTestGameState(t, itemsTestMap(), testPlayer(t, 7, 7, 15), nil, nil)
	if msgs, spent := dropItem(state); !spent || joinMessages(msgs) != "You have nothing to drop." {
		t.Errorf("got %q", msgs)
	}
}

func TestApplyDropPlacesItemAtPlayersCurrentTile(t *testing.T) {
	potion := testItem(t, "healing_potion", 0, 0)
	existing := testItem(t, "healing_potion", 9, 11)
	state := newTestGameState(t, itemsTestMap(), carrying(testPlayer(t, 9, 11, 15), potion), nil, []*rules.Entity{existing})

	if msgs, _ := applyDrop(state, potion); joinMessages(msgs) != "You drop the healing potion." {
		t.Errorf("got %q", msgs)
	}
	if potion.X != 9 || potion.Y != 11 || len(state.Player.Inventory) != 0 {
		t.Error("expected the potion at the Player's feet")
	}
	if len(state.floorItemsAt(9, 11)) != 2 {
		t.Error("a dropped item lies alongside whatever is already there")
	}
}

func TestHandleKeyPickupWieldThenDropUnwieldsAndRevertsToBareHandedDamage(t *testing.T) {
	g := testMonster(t, "goblin", 8, 7, 1000)
	state := newTestGameState(t, itemsTestMap(), testPlayer(t, 7, 7, 1000), []*rules.Entity{g}, []*rules.Entity{testItem(t, "fine_dagger", 7, 7)})
	mustKey(t, state, ",")
	mustKey(t, state, "E")

	before := g.HP
	mustKey(t, state, "Right")
	if d := before - g.HP; d < 3 || d > 6 {
		t.Fatalf("expected dagger damage in [3,6], got %d", d)
	}

	mustKey(t, state, "D")
	// The goblin is alive and adjacent, so it may add its own message in
	// the same turn: only check the drop's message leads.
	if msg := mustKey(t, state, "A"); !strings.HasPrefix(msg, "You drop the fine dagger. You are now fighting bare-handed.") {
		t.Errorf("got %q", msg)
	}
	if state.Player.Weapon() != nil || len(state.floorItemsAt(7, 7)) != 1 {
		t.Fatal("expected the dagger on the floor at the Player's feet")
	}

	before = g.HP
	mustKey(t, state, "Right")
	if d := before - g.HP; d < 1 || d > 2 {
		t.Errorf("expected bare-handed damage in [1,2], got %d", d)
	}
}

func TestHandleKeyDropInventoryItemLeavesWieldedIntact(t *testing.T) {
	dagger, potion := testItem(t, "fine_dagger", 0, 0), testItem(t, "healing_potion", 0, 0)
	p := carrying(testPlayer(t, 7, 7, 15), dagger, potion)
	state := newTestGameState(t, itemsTestMap(), p, nil, nil)
	state.ctx().Equip(p, dagger)

	mustKey(t, state, "D") // A) wielded dagger, B) potion
	mustKey(t, state, "B")
	if p.Weapon() != dagger || len(p.Inventory) != 0 || len(state.FloorItems) != 1 || state.FloorItems[0] != potion {
		t.Error("expected the potion dropped and the dagger still wielded")
	}
}

func TestPickUpOpensPickerWithTwoItemsAtSameTile(t *testing.T) {
	state := newTestGameState(t, itemsTestMap(), testPlayer(t, 7, 7, 15), nil,
		[]*rules.Entity{testItem(t, "healing_potion", 7, 7), testItem(t, "invisibility_potion", 7, 7)})
	if msgs, spent := pickUp(state); spent || msgs != nil {
		t.Fatalf("got %q, spent %v", msgs, spent)
	}
	if sel := state.PendingSelection; sel == nil || sel.Action != pendingPickup || len(sel.Candidates) != 2 || len(state.FloorItems) != 2 {
		t.Fatalf("expected a pickup picker over 2 candidates, got %+v", sel)
	}
}

func TestHandleKeyPendingPickupValidLetterPicksUpChosenItem(t *testing.T) {
	first, second := testItem(t, "healing_potion", 7, 7), testItem(t, "invisibility_potion", 7, 7)
	state := newTestGameState(t, itemsTestMap(), testPlayer(t, 7, 7, 15), nil, []*rules.Entity{first, second})

	mustKey(t, state, ",")
	if msg := mustKey(t, state, "B"); msg != "You pick up the invisibility potion." {
		t.Errorf("got %q", msg)
	}
	if !slices.Equal(state.Player.Inventory, []*rules.Entity{second}) || !slices.Equal(state.FloorItems, []*rules.Entity{first}) {
		t.Error("expected the SECOND item picked up and the first left")
	}
}

func TestApplyPickupReportsInventoryFullAndLeavesChosenItemOnFloor(t *testing.T) {
	p := testPlayer(t, 7, 7, 15)
	for i := 0; i < rules.InventoryCapacity; i++ {
		carrying(p, testItem(t, "healing_potion", 0, 0))
	}
	state := newTestGameState(t, itemsTestMap(), p, nil,
		[]*rules.Entity{testItem(t, "healing_potion", 7, 7), testItem(t, "invisibility_potion", 7, 7)})

	mustKey(t, state, ",")
	if msg := mustKey(t, state, "B"); msg != "Your inventory is full." {
		t.Errorf("got %q", msg)
	}
	if len(p.Inventory) != rules.InventoryCapacity || len(state.FloorItems) != 2 {
		t.Error("expected nothing to move")
	}
}

func TestLookHereListsAllItemsAtPlayersTile(t *testing.T) {
	state := newTestGameState(t, itemsTestMap(), testPlayer(t, 7, 7, 15), nil, nil)
	if got := lookHereLines(state); !slices.Equal(got, []string{"There is nothing here."}) {
		t.Errorf("got %v", got)
	}
	// Lettered exactly as the pickup picker would be, stairs included.
	state.FloorItems = []*rules.Entity{testItem(t, "healing_potion", 7, 7), testItem(t, "fine_dagger", 7, 7), testItem(t, "stairs_down", 7, 7), testItem(t, "torch", 8, 7)}
	if got, want := lookHereLines(state), []string{"A) healing potion", "B) fine dagger", "C) stairs down"}; !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// TestACursedSwordRefusesToBeDroppedAndCostsNoTurn is the curse from the
// game's side: the refusal is shown, the sword stays in hand, and the
// monsters get no free turn out of it.
func TestACursedSwordRefusesToBeDroppedAndCostsNoTurn(t *testing.T) {
	sword := testItem(t, "rusty_sword", 0, 0)
	g := testMonster(t, "goblin", 8, 7, 100)
	p := carrying(testPlayer(t, 7, 7, 100), sword, testItem(t, "fine_dagger", 0, 0))
	state := newTestGameState(t, itemsTestMap(), p, []*rules.Entity{g}, nil)

	mustKey(t, state, "E")
	if msg := mustKey(t, state, "A"); !strings.HasPrefix(msg, "You wield the rusty sword.") {
		t.Fatalf("got %q", msg)
	}
	turn, hp := state.Turn, p.HP

	mustKey(t, state, "D")
	if msg := mustKey(t, state, "A"); msg != "The rusty sword is welded to your hand!" {
		t.Errorf("drop: got %q", msg)
	}
	// Only the dagger is left to equip, so E acts at once — and wielding
	// it would mean letting go of the sword.
	if msg := mustKey(t, state, "E"); msg != "The rusty sword is welded to your hand!" {
		t.Errorf("swap: got %q", msg)
	}
	if p.Weapon() != sword || len(state.FloorItems) != 0 || state.Turn != turn || p.HP != hp {
		t.Errorf("the sword should stay put and no turn pass: weapon %v, turn %d->%d", p.Weapon().Name, turn, state.Turn)
	}
}

// TestThePickerTakesAnyLetterInEitherCase: with a dozen items, the 11th
// (K) and 12th (L) drop like the first, and a lower-case letter chooses
// just as its capital does. A letter no item has closes the picker.
func TestThePickerTakesAnyLetterInEitherCase(t *testing.T) {
	p := testPlayer(t, 7, 7, 100)
	for i := 0; i < 12; i++ {
		carrying(p, testItem(t, "fine_dagger", 0, 0))
	}
	state := newTestGameState(t, itemsTestMap(), p, nil, nil)
	eleventh, twelfth := p.Inventory[10], p.Inventory[11]

	mustKey(t, state, "D")
	mustKey(t, state, "K")
	mustKey(t, state, "D")
	mustKey(t, state, "k") // the old twelfth is now eleventh
	if len(state.FloorItems) != 2 || state.FloorItems[0] != eleventh || state.FloorItems[1] != twelfth {
		t.Fatalf("want the 11th and 12th daggers dropped, floor %v", names(state.FloorItems))
	}

	mustKey(t, state, "D")
	mustKey(t, state, "Z")
	if state.PendingSelection != nil || len(state.FloorItems) != 2 || len(p.Inventory) != 10 {
		t.Errorf("Z names nothing: the picker should just close, dropping nothing")
	}
}
