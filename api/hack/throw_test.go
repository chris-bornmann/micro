package main

import (
	"encoding/json"
	"strings"
	"testing"

	"map_api/attributes"
	"map_api/item"
	"map_api/mapdata"
)

func TestThrowItemReportsNothingToThrowWhenInventoryIsEmpty(t *testing.T) {
	m := itemsTestMap()
	playerJSON := marshalPerson(t, testPerson{ID: "player:1", X: 7, Y: 7, HP: 15})
	state := newTestGameState(m, playerJSON, []MonsterState{deadMonster(t, "goblin")}, nil)

	msg, done, err := throwItem(state)
	if err != nil {
		t.Fatalf("throwItem: %v", err)
	}
	if !done {
		t.Fatal("expected done=true with an empty inventory")
	}
	if msg != "You have nothing to throw." {
		t.Errorf("got message %q, want %q", msg, "You have nothing to throw.")
	}
}

func TestThrowItemMovesStraightToDirectionPromptWithOneCandidate(t *testing.T) {
	m := itemsTestMap()
	inv := potionAt(t, m, 0, 0)
	playerJSON := marshalPerson(t, testPerson{ID: "player:1", X: 7, Y: 7, HP: 15, Inventory: []json.RawMessage{inv}})
	state := newTestGameState(m, playerJSON, []MonsterState{deadMonster(t, "goblin")}, nil)

	msg, done, err := throwItem(state)
	if err != nil {
		t.Fatalf("throwItem: %v", err)
	}
	if done || msg != "" {
		t.Fatalf("expected done=false, msg=\"\" (waiting on a direction), got done=%v msg=%q", done, msg)
	}
	if state.PendingSelection == nil || state.PendingSelection.Action != pendingThrow {
		t.Fatalf("expected a pendingThrow selection, got %+v", state.PendingSelection)
	}
	if state.PendingSelection.Item == nil {
		t.Fatal("expected the sole candidate auto-selected as Item, waiting on a direction")
	}
}

func TestThrowItemOpensLetterPickerWithTwoCandidates(t *testing.T) {
	m := itemsTestMap()
	inv1 := potionAt(t, m, 0, 0)
	inv2 := invisibilityPotionAt(t, m, 0, 0)
	playerJSON := marshalPerson(t, testPerson{ID: "player:1", X: 7, Y: 7, HP: 15, Inventory: []json.RawMessage{inv1, inv2}})
	state := newTestGameState(m, playerJSON, []MonsterState{deadMonster(t, "goblin")}, nil)

	msg, done, err := throwItem(state)
	if err != nil {
		t.Fatalf("throwItem: %v", err)
	}
	if done || msg != "" {
		t.Fatalf("expected done=false, msg=\"\" (opening a picker), got done=%v msg=%q", done, msg)
	}
	if state.PendingSelection == nil || state.PendingSelection.Item != nil {
		t.Fatalf("expected a pendingThrow letter picker (Item nil), got %+v", state.PendingSelection)
	}
	if len(state.PendingSelection.Candidates) != 2 {
		t.Fatalf("expected 2 candidates, got %d", len(state.PendingSelection.Candidates))
	}
}

func TestHandleKeyThrowLetterThenDirectionTwoStageFlow(t *testing.T) {
	m := itemsTestMap()
	// Non-Fragile items, so the final "lands on the floor" assertion
	// below reflects the generic case, not potion-shattering behavior.
	inv1 := weaponAt(t, m, 0, 0)
	inv2 := torchAt(t, m, 0, 0)
	playerJSON := marshalPerson(t, testPerson{
		ID: "player:1", X: 7, Y: 7, HP: 15,
		Inventory:  []json.RawMessage{inv1, inv2},
		Attributes: attributes.Attributes{Strength: 10},
	})
	state := newTestGameState(m, playerJSON, []MonsterState{deadMonster(t, "goblin")}, nil)

	if _, err := handleKey(state, "T"); err != nil {
		t.Fatalf("handleKey(\"T\"): %v", err)
	}
	if state.PendingSelection == nil || len(state.PendingSelection.Candidates) != 2 {
		t.Fatalf("expected the letter picker open with 2 candidates, got %+v", state.PendingSelection)
	}

	// Choosing the first candidate's letter must NOT spend a turn — it
	// only advances to the direction-waiting stage.
	if _, err := handleKey(state, "A"); err != nil {
		t.Fatalf("handleKey(\"A\"): %v", err)
	}
	if state.PendingSelection == nil || state.PendingSelection.Item == nil {
		t.Fatalf("expected the direction-waiting stage after choosing a letter, got %+v", state.PendingSelection)
	}
	stillTwo := len(unmarshalPerson(t, state.PlayerState).Inventory)
	if stillTwo != 2 {
		t.Fatalf("expected Inventory untouched until a direction is chosen, got %d items", stillTwo)
	}

	// Now the direction completes the throw and spends the turn.
	if _, err := handleKey(state, "Right"); err != nil {
		t.Fatalf("handleKey(\"Right\"): %v", err)
	}
	if state.PendingSelection != nil {
		t.Error("expected the pending selection cleared after the throw completes")
	}
	got := unmarshalPerson(t, state.PlayerState)
	if len(got.Inventory) != 1 {
		t.Fatalf("expected 1 inventory item left after throwing the other, got %d", len(got.Inventory))
	}
	if len(state.FloorItems) != 1 {
		t.Fatalf("expected the thrown item to land on the floor, got %d floor items", len(state.FloorItems))
	}
}

func TestResolveThrowCancelsOnNonDirectionKey(t *testing.T) {
	m := itemsTestMap()
	inv := potionAt(t, m, 0, 0)
	playerJSON := marshalPerson(t, testPerson{ID: "player:1", X: 7, Y: 7, HP: 15, Inventory: []json.RawMessage{inv}})
	state := newTestGameState(m, playerJSON, []MonsterState{deadMonster(t, "goblin")}, nil)

	if _, err := handleKey(state, "T"); err != nil {
		t.Fatalf("handleKey(\"T\"): %v", err)
	}
	if state.PendingSelection == nil || state.PendingSelection.Item == nil {
		t.Fatalf("expected the direction-waiting stage, got %+v", state.PendingSelection)
	}

	if _, err := handleKey(state, "X"); err != nil {
		t.Fatalf("handleKey(\"X\"): %v", err)
	}
	if state.PendingSelection != nil {
		t.Error("expected the pending throw cancelled by a non-direction key")
	}
	got := unmarshalPerson(t, state.PlayerState)
	if len(got.Inventory) != 1 {
		t.Errorf("expected the item to remain in Inventory after a cancelled throw, got %d items", len(got.Inventory))
	}
	if len(state.FloorItems) != 0 {
		t.Errorf("expected no floor item from a cancelled throw, got %d", len(state.FloorItems))
	}
}

func TestHandleKeyThrowHitsGoblinAndDealsDamage(t *testing.T) {
	m := itemsTestMap()
	// weapon.Init's dagger has weight 3, non-Fragile — this test covers
	// the generic "hits and lands on the floor" case; see
	// TestHandleKeyThrowPotionShattersOnHit for the Fragile counterpart.
	// strength 5 => throw.Damage(3, 5) gives base ceil(3/10)=1 + bonus
	// floor(5/10)=0 = [1,1], a deterministic single point of damage.
	inv := weaponAt(t, m, 0, 0)
	playerJSON := marshalPerson(t, testPerson{
		ID: "player:1", X: 7, Y: 7, HP: 15,
		Inventory:  []json.RawMessage{inv},
		Attributes: attributes.Attributes{Strength: 5},
	})
	goblinJSON := marshalMonster(t, testMonster{ID: "goblin:1", X: 9, Y: 7, Kind: "goblin", HP: 5, MaxHP: 5, Aware: true})
	state := newTestGameState(m, playerJSON, []MonsterState{monster("goblin", goblinJSON)}, nil)

	if _, err := handleKey(state, "T"); err != nil {
		t.Fatalf("handleKey(\"T\"): %v", err)
	}
	if _, err := handleKey(state, "Right"); err != nil {
		t.Fatalf("handleKey(\"Right\"): %v", err)
	}

	got := unmarshalMonster(t, monsterStateOf(t, state, "goblin"))
	if got.HP != 4 {
		t.Errorf("expected the goblin to take 1 damage (HP 5 -> 4), got HP %d", got.HP)
	}
	if len(state.FloorItems) != 1 {
		t.Fatalf("expected the thrown item to land on the floor at the impact tile, got %d", len(state.FloorItems))
	}
	it, err := itemInfo(state.FloorItems[0])
	if err != nil {
		t.Fatalf("itemInfo: %v", err)
	}
	if it.X != 9 || it.Y != 7 {
		t.Errorf("expected the thrown item to land at the goblin's tile (9,7), got (%d,%d)", it.X, it.Y)
	}
}

func TestHandleKeyThrowPotionShattersOnHit(t *testing.T) {
	m := itemsTestMap()
	inv := potionAt(t, m, 0, 0)
	playerJSON := marshalPerson(t, testPerson{
		ID: "player:1", X: 7, Y: 7, HP: 15,
		Inventory:  []json.RawMessage{inv},
		Attributes: attributes.Attributes{Strength: 5},
	})
	goblinJSON := marshalMonster(t, testMonster{ID: "goblin:1", X: 9, Y: 7, Kind: "goblin", HP: 5, MaxHP: 5, Aware: true})
	state := newTestGameState(m, playerJSON, []MonsterState{monster("goblin", goblinJSON)}, nil)

	if _, err := handleKey(state, "T"); err != nil {
		t.Fatalf("handleKey(\"T\"): %v", err)
	}
	msg, err := handleKey(state, "Right")
	if err != nil {
		t.Fatalf("handleKey(\"Right\"): %v", err)
	}

	// Damage still applies exactly like the non-Fragile case (see
	// TestHandleKeyThrowHitsGoblinAndDealsDamage) — only whether the
	// item survives the impact changes.
	got := unmarshalMonster(t, monsterStateOf(t, state, "goblin"))
	if got.HP != 4 {
		t.Errorf("expected the goblin to take 1 damage (HP 5 -> 4), got HP %d", got.HP)
	}
	if len(state.FloorItems) != 0 {
		t.Errorf("expected the shattered potion to NOT land on the floor, got %d floor items", len(state.FloorItems))
	}
	if !strings.Contains(msg, "shatters") {
		t.Errorf("expected a shatter message, got %q", msg)
	}
}

func TestHandleKeyThrowKillingGoblinDropsCorpseButNotTheShatteredPotion(t *testing.T) {
	m := itemsTestMap()
	inv := potionAt(t, m, 0, 0)
	playerJSON := marshalPerson(t, testPerson{
		ID: "player:1", X: 7, Y: 7, HP: 15,
		Inventory:  []json.RawMessage{inv},
		Attributes: attributes.Attributes{Strength: 10},
	})
	goblinJSON := marshalMonster(t, testMonster{ID: "goblin:1", X: 9, Y: 7, Kind: "goblin", HP: 1, MaxHP: 5, Aware: true})
	state := newTestGameState(m, playerJSON, []MonsterState{monster("goblin", goblinJSON)}, nil)

	if _, err := handleKey(state, "T"); err != nil {
		t.Fatalf("handleKey(\"T\"): %v", err)
	}
	if _, err := handleKey(state, "Right"); err != nil {
		t.Fatalf("handleKey(\"Right\"): %v", err)
	}

	goblinObj, err := monsterObject(monsterStateOf(t, state, "goblin"))
	if err != nil {
		t.Fatalf("goblinObject: %v", err)
	}
	if !goblinObj.Dead {
		t.Fatal("expected the goblin dead after a guaranteed-lethal throw")
	}

	if len(state.FloorItems) != 1 {
		t.Fatalf("expected exactly 1 floor item (the corpse; the shattered potion should NOT land), got %d", len(state.FloorItems))
	}
	it, err := itemInfo(state.FloorItems[0])
	if err != nil {
		t.Fatalf("itemInfo: %v", err)
	}
	if it.Category != item.CategoryCorpse {
		t.Errorf("expected the sole floor item to be the corpse, got category %q", it.Category)
	}
}

func TestHandleKeyThrowPotionShattersOnMiss(t *testing.T) {
	m := itemsTestMap()
	inv := potionAt(t, m, 0, 0)
	playerJSON := marshalPerson(t, testPerson{
		ID: "player:1", X: 7, Y: 7, HP: 15,
		Inventory:  []json.RawMessage{inv},
		Attributes: attributes.Attributes{Strength: 10},
	})
	state := newTestGameState(m, playerJSON, []MonsterState{deadMonster(t, "goblin")}, nil)

	if _, err := handleKey(state, "T"); err != nil {
		t.Fatalf("handleKey(\"T\"): %v", err)
	}
	msg, err := handleKey(state, "Right")
	if err != nil {
		t.Fatalf("handleKey(\"Right\"): %v", err)
	}

	got := unmarshalPerson(t, state.PlayerState)
	if len(got.Inventory) != 0 {
		t.Fatalf("expected the item removed from Inventory, got %d left", len(got.Inventory))
	}
	if len(state.FloorItems) != 0 {
		t.Errorf("expected the shattered potion to NOT land on the floor even on a clean miss, got %d floor items", len(state.FloorItems))
	}
	if !strings.Contains(msg, "shatters") {
		t.Errorf("expected a shatter message, got %q", msg)
	}
}

func TestHandleKeyThrowMissLandsOnFloorAtRestingCell(t *testing.T) {
	m := itemsTestMap()
	// weapon.Init's dagger (weight 3, non-Fragile) preserves the original
	// "a missed throw still lands on the floor" coverage now that
	// potions shatter instead — see TestHandleKeyThrowPotionShattersOnMiss.
	inv := weaponAt(t, m, 0, 0)
	playerJSON := marshalPerson(t, testPerson{
		ID: "player:1", X: 7, Y: 7, HP: 15,
		Inventory:  []json.RawMessage{inv},
		Attributes: attributes.Attributes{Strength: 10},
	})
	state := newTestGameState(m, playerJSON, []MonsterState{deadMonster(t, "goblin")}, nil)

	if _, err := handleKey(state, "T"); err != nil {
		t.Fatalf("handleKey(\"T\"): %v", err)
	}
	if _, err := handleKey(state, "Right"); err != nil {
		t.Fatalf("handleKey(\"Right\"): %v", err)
	}

	got := unmarshalPerson(t, state.PlayerState)
	if len(got.Inventory) != 0 {
		t.Fatalf("expected the item removed from Inventory, got %d left", len(got.Inventory))
	}
	if len(state.FloorItems) != 1 {
		t.Fatalf("expected exactly 1 floor item (the missed throw), got %d", len(state.FloorItems))
	}
	it, err := itemInfo(state.FloorItems[0])
	if err != nil {
		t.Fatalf("itemInfo: %v", err)
	}
	// itemsTestMap's room spans x:5-14 — a weight-3, strength-10 throw
	// (distance 7) thrown Right from x=7 travels exactly to x=14, the
	// room's own east wall (7 steps: 8,9,...,14), so it lands there.
	if it.X != 14 || it.Y != 7 {
		t.Errorf("expected the item to land at the room's east wall (14,7), got (%d,%d)", it.X, it.Y)
	}
}

func TestHandleKeyThrowFlaskOfOilIntoWallDisappearsWithNoSideEffect(t *testing.T) {
	m := itemsTestMap()
	inv := flaskAt(t, m, 0, 0)
	playerJSON := marshalPerson(t, testPerson{
		ID: "player:1", X: 5, Y: 5, HP: 15, // room's own top-left interior corner
		Inventory:  []json.RawMessage{inv},
		Attributes: attributes.Attributes{Strength: 10},
	})
	state := newTestGameState(m, playerJSON, []MonsterState{deadMonster(t, "goblin")}, nil)

	if _, err := handleKey(state, "T"); err != nil {
		t.Fatalf("handleKey(\"T\"): %v", err)
	}
	// Up immediately exits the room into unmapped space with no corridor
	// there — blocked on the very first step.
	msg, err := handleKey(state, "Up")
	if err != nil {
		t.Fatalf("handleKey(\"Up\"): %v", err)
	}

	if len(state.FloorItems) != 0 {
		t.Errorf("expected no floor item from a flask blocked by a wall, got %d", len(state.FloorItems))
	}
	if len(state.OilyTiles) != 0 {
		t.Errorf("expected no oily tile from a flask blocked by a wall, got %d", len(state.OilyTiles))
	}
	if !strings.Contains(msg, "shatters") {
		t.Errorf("expected a shatter message, got %q", msg)
	}
}

func TestHandleKeyThrowFlaskOfOilOntoOpenFloorMarksTileOily(t *testing.T) {
	m := itemsTestMap()
	inv := flaskAt(t, m, 0, 0)
	// weight 2, strength 1 => throw.Distance == 5 (well within the
	// room's open interior, x:5-14), so it lands on open ground.
	playerJSON := marshalPerson(t, testPerson{
		ID: "player:1", X: 7, Y: 7, HP: 15,
		Inventory:  []json.RawMessage{inv},
		Attributes: attributes.Attributes{Strength: 1},
	})
	state := newTestGameState(m, playerJSON, []MonsterState{deadMonster(t, "goblin")}, nil)

	if _, err := handleKey(state, "T"); err != nil {
		t.Fatalf("handleKey(\"T\"): %v", err)
	}
	msg, err := handleKey(state, "Right")
	if err != nil {
		t.Fatalf("handleKey(\"Right\"): %v", err)
	}

	if len(state.FloorItems) != 0 {
		t.Errorf("expected no floor item from a shattered flask, got %d", len(state.FloorItems))
	}
	if !state.OilyTiles[mapdata.Point{X: 12, Y: 7}] {
		t.Errorf("expected (12,7) marked oily, got OilyTiles=%+v", state.OilyTiles)
	}
	if !strings.Contains(msg, "shatters") {
		t.Errorf("expected a shatter message, got %q", msg)
	}
}

func TestHandleKeyThrowFlaskOfOilHitsGoblinDealsDamageAndAddsOiliness(t *testing.T) {
	m := itemsTestMap()
	inv := flaskAt(t, m, 0, 0)
	// weight 2, strength 1 => throw.Damage(2, 1) gives base ceil(2/10)=1
	// + bonus floor(1/10)=0 = [1,1], a deterministic single point of
	// damage — and throw.Distance == 5, easily enough to reach a target
	// 2 cells away.
	playerJSON := marshalPerson(t, testPerson{
		ID: "player:1", X: 7, Y: 7, HP: 15,
		Inventory:  []json.RawMessage{inv},
		Attributes: attributes.Attributes{Strength: 1},
	})
	goblinJSON := marshalMonster(t, testMonster{ID: "goblin:1", X: 9, Y: 7, Kind: "goblin", HP: 5, MaxHP: 5, Aware: true})
	state := newTestGameState(m, playerJSON, []MonsterState{monster("goblin", goblinJSON)}, nil)

	if _, err := handleKey(state, "T"); err != nil {
		t.Fatalf("handleKey(\"T\"): %v", err)
	}
	msg, err := handleKey(state, "Right")
	if err != nil {
		t.Fatalf("handleKey(\"Right\"): %v", err)
	}

	got := unmarshalMonster(t, monsterStateOf(t, state, "goblin"))
	if got.HP != 4 {
		t.Errorf("expected the goblin to take 1 damage (HP 5 -> 4), got HP %d", got.HP)
	}
	if got.Oiliness != 1 {
		t.Errorf("expected the goblin's Oiliness to become 1, got %d", got.Oiliness)
	}
	if len(state.FloorItems) != 0 {
		t.Errorf("expected no floor item from a shattered flask, got %d", len(state.FloorItems))
	}
	if len(state.OilyTiles) != 0 {
		t.Errorf("expected no oily floor tile when the flask hit an agent instead of the floor, got %d", len(state.OilyTiles))
	}
	if !strings.Contains(msg, "oil") {
		t.Errorf("expected an oil-related message, got %q", msg)
	}
}

func TestHandleKeyThrowLitTorchIgnitesOilyGoblin(t *testing.T) {
	m := itemsTestMap()
	litTorch := &testLitTorch{Item: torchAt(t, m, 0, 0), TurnsRemaining: 5}
	// weight 2, strength 1 => throw.Damage(2, 1) gives [1,1] (deterministic)
	// and throw.Distance == 5, easily enough to reach a target 2 cells away.
	playerJSON := marshalPerson(t, testPerson{
		ID: "player:1", X: 7, Y: 7, HP: 15, Lit: true,
		LitTorch:   litTorch,
		Attributes: attributes.Attributes{Strength: 1},
	})
	goblinJSON := marshalMonster(t, testMonster{ID: "goblin:1", X: 9, Y: 7, Kind: "goblin", HP: 20, MaxHP: 20, Aware: true, Oiliness: 1})
	state := newTestGameState(m, playerJSON, []MonsterState{monster("goblin", goblinJSON)}, nil)

	if _, err := handleKey(state, "T"); err != nil {
		t.Fatalf("handleKey(\"T\"): %v", err)
	}
	msg, err := handleKey(state, "Right")
	if err != nil {
		t.Fatalf("handleKey(\"Right\"): %v", err)
	}

	// Ignition happens this turn, but burning starts NEXT turn — so the
	// only damage this turn is the deterministic throw hit itself (1).
	got := unmarshalMonster(t, monsterStateOf(t, state, "goblin"))
	if got.HP != 19 {
		t.Errorf("expected HP 19 (20 - 1 deterministic throw damage, no burn damage yet), got %d", got.HP)
	}
	if !got.Burning {
		t.Error("expected the goblin to be Burning immediately after ignition")
	}
	// Oiliness 1 => fire.Duration is a single 1-3 roll, untouched by the
	// grace turn.
	if got.BurningTurnsRemaining < 1 || got.BurningTurnsRemaining > 3 {
		t.Errorf("expected BurningTurnsRemaining in [1,3], got %d", got.BurningTurnsRemaining)
	}
	if len(state.FloorItems) != 0 {
		t.Errorf("expected the lit torch to never land on the floor, got %d floor items", len(state.FloorItems))
	}

	gotPlayer := unmarshalPerson(t, state.PlayerState)
	if gotPlayer.LitTorch != nil {
		t.Errorf("expected LitTorch cleared after throwing it, got %+v", gotPlayer.LitTorch)
	}
	if gotPlayer.Lit {
		t.Error("expected Lit cleared after throwing the lit torch")
	}
	if !strings.Contains(msg, "goes out") {
		t.Errorf("expected the lit-torch-consumed message, got %q", msg)
	}

	// The NEXT turn is when burning actually starts dealing damage.
	if _, err := handleKey(state, "."); err != nil {
		t.Fatalf("handleKey(\".\"): %v", err)
	}
	gotAfterNextTurn := unmarshalMonster(t, monsterStateOf(t, state, "goblin"))
	if gotAfterNextTurn.HP < 15 || gotAfterNextTurn.HP > 17 {
		t.Errorf("expected HP in [15,17] (19 minus [2,4] burn damage) after the following turn, got %d", gotAfterNextTurn.HP)
	}
}

func TestHandleKeyThrowLitTorchNeverLandsOnFloorEvenBlockedByWall(t *testing.T) {
	m := itemsTestMap()
	litTorch := &testLitTorch{Item: torchAt(t, m, 0, 0), TurnsRemaining: 5}
	playerJSON := marshalPerson(t, testPerson{
		ID: "player:1", X: 5, Y: 5, HP: 15, Lit: true, // room's own top-left interior corner
		LitTorch:   litTorch,
		Attributes: attributes.Attributes{Strength: 10},
	})
	state := newTestGameState(m, playerJSON, []MonsterState{deadMonster(t, "goblin")}, nil)

	if _, err := handleKey(state, "T"); err != nil {
		t.Fatalf("handleKey(\"T\"): %v", err)
	}
	// Up immediately exits the room into unmapped space with no corridor
	// there — blocked on the very first step.
	msg, err := handleKey(state, "Up")
	if err != nil {
		t.Fatalf("handleKey(\"Up\"): %v", err)
	}

	if len(state.FloorItems) != 0 {
		t.Errorf("expected the lit torch to never land on the floor even when blocked by a wall, got %d floor items", len(state.FloorItems))
	}
	if !strings.Contains(msg, "goes out") {
		t.Errorf("expected a \"goes out and falls apart\" message, got %q", msg)
	}

	got := unmarshalPerson(t, state.PlayerState)
	if got.LitTorch != nil {
		t.Errorf("expected LitTorch cleared after throwing it, got %+v", got.LitTorch)
	}
}
