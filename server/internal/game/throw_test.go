package game

import (
	"strings"
	"testing"

	"micro/server/internal/mapdata"
	"micro/server/internal/rules"
)

// thrower is a Player at (x, y) with the given Strength, carrying items.
func thrower(t *testing.T, x, y, strength int, items ...*rules.Entity) *rules.Entity {
	p := carrying(testPlayer(t, x, y, 15), items...)
	p.Attrs.Strength = strength
	return p
}

func TestThrowItemReportsNothingToThrowWhenInventoryIsEmpty(t *testing.T) {
	state := newTestGameState(t, itemsTestMap(), testPlayer(t, 7, 7, 15), nil, nil)
	if msgs, spent := throwItem(state); !spent || joinMessages(msgs) != "You have nothing to throw." {
		t.Errorf("got %q", msgs)
	}
}

func TestThrowItemMovesStraightToDirectionPromptWithOneCandidate(t *testing.T) {
	potion := testItem(t, "healing_potion", 0, 0)
	state := newTestGameState(t, itemsTestMap(), carrying(testPlayer(t, 7, 7, 15), potion), nil, nil)
	if msgs, spent := throwItem(state); spent || msgs != nil {
		t.Fatalf("got %q, spent %v", msgs, spent)
	}
	if sel := state.PendingSelection; sel == nil || sel.Action != pendingThrow || sel.Item != potion {
		t.Fatalf("expected the sole item chosen, waiting on a direction; got %+v", sel)
	}
}

func TestThrowItemOpensLetterPickerWithTwoCandidates(t *testing.T) {
	state := newTestGameState(t, itemsTestMap(), carrying(testPlayer(t, 7, 7, 15),
		testItem(t, "healing_potion", 0, 0), testItem(t, "invisibility_potion", 0, 0)), nil, nil)
	throwItem(state)
	if sel := state.PendingSelection; sel == nil || sel.Item != nil || len(sel.Candidates) != 2 {
		t.Fatalf("expected a letter picker over 2 items, got %+v", sel)
	}
}

func TestHandleKeyThrowLetterThenDirectionTwoStageFlow(t *testing.T) {
	dagger, torch := testItem(t, "fine_dagger", 0, 0), testItem(t, "torch", 0, 0)
	state := newTestGameState(t, itemsTestMap(), thrower(t, 7, 7, 10, dagger, torch), nil, nil)

	mustKey(t, state, "T")
	mustKey(t, state, "A") // choosing an item spends nothing: it asks for a direction
	if sel := state.PendingSelection; sel == nil || sel.Item != dagger || len(state.Player.Inventory) != 2 || state.Turn != 0 {
		t.Fatalf("expected to be waiting on a direction with nothing thrown, got %+v", sel)
	}
	mustKey(t, state, "Right")
	if state.PendingSelection != nil || len(state.Player.Inventory) != 1 || len(state.FloorItems) != 1 || state.Turn != 1 {
		t.Error("expected the dagger thrown onto the floor and the turn spent")
	}
}

func TestResolveThrowCancelsOnNonDirectionKey(t *testing.T) {
	state := newTestGameState(t, itemsTestMap(), carrying(testPlayer(t, 7, 7, 15), testItem(t, "healing_potion", 0, 0)), nil, nil)
	mustKey(t, state, "T")
	mustKey(t, state, "X")
	if state.PendingSelection != nil || len(state.Player.Inventory) != 1 || len(state.FloorItems) != 0 || state.Turn != 0 {
		t.Error("expected the throw cancelled with nothing changed")
	}
}

// A dagger (weight 3) thrown with Strength 5 does throw.Damage [1, 1]: one
// certain point of damage.
func TestHandleKeyThrowHitsGoblinAndLandsAtItsFeet(t *testing.T) {
	g := testMonster(t, "goblin", 9, 7, 5)
	state := newTestGameState(t, itemsTestMap(), thrower(t, 7, 7, 5, testItem(t, "fine_dagger", 0, 0)), []*rules.Entity{g}, nil)
	mustKey(t, state, "T")
	mustKey(t, state, "Right")
	if g.HP != 4 {
		t.Errorf("expected 1 damage, goblin HP %d", g.HP)
	}
	if len(state.FloorItems) != 1 || state.FloorItems[0].X != 9 || state.FloorItems[0].Y != 7 {
		t.Error("expected the dagger on the floor where the goblin stands")
	}
}

func TestHandleKeyThrowPotionShattersOnHitAndOnMiss(t *testing.T) {
	g := testMonster(t, "goblin", 9, 7, 5)
	state := newTestGameState(t, itemsTestMap(), thrower(t, 7, 7, 5, testItem(t, "healing_potion", 0, 0)), []*rules.Entity{g}, nil)
	mustKey(t, state, "T")
	msg := mustKey(t, state, "Right")
	if g.HP != 4 || len(state.FloorItems) != 0 || !strings.HasPrefix(msg, "You throw the healing potion. | It shatters! | Goblin takes 1 damage") {
		t.Errorf("hit: HP %d, floor %v, %q", g.HP, names(state.FloorItems), msg)
	}

	state = newTestGameState(t, itemsTestMap(), thrower(t, 7, 7, 10, testItem(t, "healing_potion", 0, 0)), nil, nil)
	mustKey(t, state, "T")
	if msg := mustKey(t, state, "Right"); !strings.Contains(msg, "shatters") || len(state.FloorItems) != 0 || len(state.Player.Inventory) != 0 {
		t.Errorf("miss: %q", msg)
	}
}

func TestHandleKeyThrowKillingGoblinDropsCorpseButNotTheShatteredPotion(t *testing.T) {
	g := testMonster(t, "goblin", 9, 7, 1)
	state := newTestGameState(t, itemsTestMap(), thrower(t, 7, 7, 10, testItem(t, "healing_potion", 0, 0)), []*rules.Entity{g}, nil)
	mustKey(t, state, "T")
	mustKey(t, state, "Right")
	if !g.Dead || len(state.FloorItems) != 1 || state.FloorItems[0].T.Category != rules.CategoryCorpse {
		t.Errorf("dead %v, floor %v", g.Dead, names(state.FloorItems))
	}
}

// A weight-3 dagger thrown with Strength 10 flies 7 cells; from x=7 that
// is exactly x=14, the room's east edge.
func TestHandleKeyThrowMissLandsOnFloorAtRestingCell(t *testing.T) {
	state := newTestGameState(t, itemsTestMap(), thrower(t, 7, 7, 10, testItem(t, "fine_dagger", 0, 0)), nil, nil)
	mustKey(t, state, "T")
	mustKey(t, state, "Right")
	if len(state.FloorItems) != 1 || state.FloorItems[0].X != 14 || state.FloorItems[0].Y != 7 {
		t.Errorf("expected the dagger at (14,7), floor %v", names(state.FloorItems))
	}
}

func TestHandleKeyThrowFlaskOfOilIntoWallDisappearsWithNoSideEffect(t *testing.T) {
	// From the room's top-left corner, Up is a wall on the first step.
	state := newTestGameState(t, itemsTestMap(), thrower(t, 5, 5, 10, testItem(t, "flask_of_oil", 0, 0)), nil, nil)
	mustKey(t, state, "T")
	msg := mustKey(t, state, "Up")
	if len(state.FloorItems) != 0 || len(state.Oily) != 0 || msg != "You throw the flask of oil. | It shatters against the wall!" {
		t.Errorf("floor %v, oily %v, %q", names(state.FloorItems), state.Oily, msg)
	}
}

// Weight 2 and Strength 1 fly 5 cells: from x=7, open floor at x=12.
func TestHandleKeyThrowFlaskOfOilOntoOpenFloorMarksTileOily(t *testing.T) {
	state := newTestGameState(t, itemsTestMap(), thrower(t, 7, 7, 1, testItem(t, "flask_of_oil", 0, 0)), nil, nil)
	mustKey(t, state, "T")
	msg := mustKey(t, state, "Right")
	if len(state.FloorItems) != 0 || !state.Oily[mapdata.Point{X: 12, Y: 7}] || len(state.Oily) != 1 {
		t.Errorf("floor %v, oily %v", names(state.FloorItems), state.Oily)
	}
	if msg != "You throw the flask of oil. | It shatters, spreading oil across the floor!" {
		t.Errorf("got %q", msg)
	}
}

func TestHandleKeyThrowFlaskOfOilHitsGoblinDealsDamageAndAddsOiliness(t *testing.T) {
	g := testMonster(t, "goblin", 9, 7, 5)
	state := newTestGameState(t, itemsTestMap(), thrower(t, 7, 7, 1, testItem(t, "flask_of_oil", 0, 0)), []*rules.Entity{g}, nil)
	mustKey(t, state, "T")
	msg := mustKey(t, state, "Right")
	if g.HP != 4 || g.Stacks("oily") != 1 || len(state.FloorItems) != 0 || len(state.Oily) != 0 {
		t.Errorf("HP %d, oily %d, floor %v, oily tiles %v", g.HP, g.Stacks("oily"), names(state.FloorItems), state.Oily)
	}
	if !strings.HasPrefix(msg, "You throw the flask of oil. | It shatters, coating them in oil! | Goblin takes 1 damage (4 HP left)") {
		t.Errorf("got %q", msg)
	}
}

func TestHandleKeyThrowLitTorchIgnitesOilyGoblin(t *testing.T) {
	g := testMonster(t, "goblin", 9, 7, 20)
	g.AddStacks("oily", 1)
	p := thrower(t, 7, 7, 1)
	rules.Light(p, carrying(p, testItem(t, "torch", 0, 0)).Inventory[0], 5)
	state := newTestGameState(t, itemsTestMap(), p, []*rules.Entity{g}, nil)

	mustKey(t, state, "T") // the lit torch is the only thing to throw
	msg := mustKey(t, state, "Right")
	// It catches this turn but starts burning next turn, so the only
	// damage so far is the torch's own 1.
	if g.HP != 19 {
		t.Errorf("expected HP 19, got %d", g.HP)
	}
	fire := g.FindTrait("on_fire")
	if fire == nil || fire.Turns < 1 || fire.Turns > 3 {
		t.Fatalf("expected one stack of oil to burn 1-3 turns, got %+v", fire)
	}
	if len(state.FloorItems) != 0 || state.eng.Reg.Has(p, "light") || !strings.Contains(msg, "The torch goes out and falls apart.") {
		t.Errorf("the thrown torch should be gone: floor %v, %q", names(state.FloorItems), msg)
	}

	mustKey(t, state, ".")
	if g.HP < 15 || g.HP > 17 {
		t.Errorf("expected 2-4 burn damage the next turn, HP %d", g.HP)
	}
}

func TestHandleKeyThrowLitTorchNeverLandsOnFloorEvenBlockedByWall(t *testing.T) {
	p := thrower(t, 5, 5, 10)
	rules.Light(p, carrying(p, testItem(t, "torch", 0, 0)).Inventory[0], 5)
	state := newTestGameState(t, itemsTestMap(), p, nil, nil)
	mustKey(t, state, "T")
	if msg := mustKey(t, state, "Up"); len(state.FloorItems) != 0 || !strings.Contains(msg, "goes out") || p.Lit() != nil {
		t.Errorf("floor %v, %q", names(state.FloorItems), msg)
	}
}
