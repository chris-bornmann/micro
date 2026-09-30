package game

import (
	"encoding/json"
	"strings"
	"testing"

	"micro/server/internal/mapdata"
	"micro/server/internal/overlay"
	"micro/server/internal/torch"
	"micro/server/internal/vision"
)

// torchAt is potionAt/weaponAt's counterpart for a torch, built via
// torch.Init and relocated to (x,y) by editing its generic position
// fields directly.
func torchAt(t *testing.T, m *mapdata.Map, x, y int) json.RawMessage {
	t.Helper()
	mapJSON, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal map: %v", err)
	}
	raw, err := torch.Init(mapJSON)
	if err != nil {
		t.Fatalf("torch.Init: %v", err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatalf("unmarshal torch: %v", err)
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
		t.Fatalf("marshal relocated torch: %v", err)
	}
	return out
}

func TestHandleKeyUseOffersTorchAndPotionTogetherAndRoutesCorrectly(t *testing.T) {
	m := itemsTestMap()
	torchJSON := torchAt(t, m, 7, 7)
	potionJSON := potionAt(t, m, 7, 7)
	playerJSON := marshalPerson(t, testPerson{ID: "player:1", X: 7, Y: 7, HP: 15, MaxHP: 15, Inventory: []json.RawMessage{torchJSON, potionJSON}})
	state := newTestGameState(m, playerJSON, []MonsterState{deadMonster(t, "goblin")}, nil)

	if _, err := handleKey(state, "A"); err != nil {
		t.Fatalf("handleKey(\"A\") open picker: %v", err)
	}
	if state.PendingSelection == nil || len(state.PendingSelection.Candidates) != 2 {
		t.Fatalf("expected a picker with 2 candidates, got %+v", state.PendingSelection)
	}

	// The torch is Inventory[0], so letter "A" resolves to it.
	if _, err := handleKey(state, "A"); err != nil {
		t.Fatalf("handleKey(\"A\") resolve torch: %v", err)
	}
	got := unmarshalPerson(t, state.PlayerState)
	if got.LitTorch == nil {
		t.Fatal("expected LitTorch set after choosing the torch candidate")
	}
	if len(got.Inventory) != 1 {
		t.Fatalf("expected 1 remaining inventory item (the potion), got %d", len(got.Inventory))
	}
}

func TestApplyLightStartsCountdownAndSetsLitOnPlayerObject(t *testing.T) {
	m := itemsTestMap()
	torchJSON := torchAt(t, m, 7, 7)
	playerJSON := marshalPerson(t, testPerson{ID: "player:1", X: 7, Y: 7, HP: 15, MaxHP: 15})
	state := newTestGameState(m, playerJSON, []MonsterState{deadMonster(t, "goblin")}, []json.RawMessage{torchJSON})

	if _, err := handleKey(state, ","); err != nil {
		t.Fatalf("handleKey(\",\"): %v", err)
	}
	if _, err := handleKey(state, "A"); err != nil {
		t.Fatalf("handleKey(\"A\"): %v", err)
	}

	playerObj, err := playerObject(state.PlayerState)
	if err != nil {
		t.Fatalf("playerObject: %v", err)
	}
	if !playerObj.Lit {
		t.Error("expected Lit=true after lighting the torch")
	}
	got := unmarshalPerson(t, state.PlayerState)
	if got.LitTorch == nil {
		t.Fatal("expected LitTorch set")
	}
	if got.LitTorch.TurnsRemaining <= 0 {
		t.Errorf("expected a positive TurnsRemaining, got %d", got.LitTorch.TurnsRemaining)
	}
}

func TestApplyLightRefusesSecondTorchWhileOneIsLit(t *testing.T) {
	m := itemsTestMap()
	floorTorch := torchAt(t, m, 7, 7)
	secondTorch := torchAt(t, m, 7, 7)
	playerJSON := marshalPerson(t, testPerson{ID: "player:1", X: 7, Y: 7, HP: 15, MaxHP: 15, Inventory: []json.RawMessage{secondTorch}})
	state := newTestGameState(m, playerJSON, []MonsterState{deadMonster(t, "goblin")}, []json.RawMessage{floorTorch})

	// Pick up the floor torch (Inventory now holds 2 torches), then open
	// the use picker and light the first one.
	if _, err := handleKey(state, ","); err != nil {
		t.Fatalf("handleKey(\",\"): %v", err)
	}
	if _, err := handleKey(state, "A"); err != nil {
		t.Fatalf("handleKey(\"A\") open picker: %v", err)
	}
	if state.PendingSelection == nil {
		t.Fatal("expected a picker with 2 torch candidates")
	}
	if _, err := handleKey(state, "A"); err != nil {
		t.Fatalf("handleKey(\"A\") light first torch: %v", err)
	}
	got := unmarshalPerson(t, state.PlayerState)
	if got.LitTorch == nil {
		t.Fatal("expected a torch lit")
	}
	if len(got.Inventory) != 1 {
		t.Fatalf("expected 1 remaining torch in Inventory, got %d", len(got.Inventory))
	}

	// Only one one-shot candidate remains, so "A" auto-resolves to it —
	// lighting it should be refused since one is already lit.
	msg, err := handleKey(state, "A")
	if err != nil {
		t.Fatalf("handleKey(\"A\") second torch: %v", err)
	}
	if !strings.Contains(msg, "already lit") {
		t.Errorf("expected an 'already lit' message, got %q", msg)
	}
	got = unmarshalPerson(t, state.PlayerState)
	if len(got.Inventory) != 1 {
		t.Errorf("expected the second torch to remain in Inventory, untouched, got %d items", len(got.Inventory))
	}
}

func TestApplyDropLitTorchCreatesNoFloorItem(t *testing.T) {
	m := itemsTestMap()
	torchJSON := torchAt(t, m, 7, 7)
	playerJSON := marshalPerson(t, testPerson{ID: "player:1", X: 7, Y: 7, HP: 15, MaxHP: 15})
	state := newTestGameState(m, playerJSON, []MonsterState{deadMonster(t, "goblin")}, []json.RawMessage{torchJSON})

	if _, err := handleKey(state, ","); err != nil {
		t.Fatalf("handleKey(\",\"): %v", err)
	}
	if _, err := handleKey(state, "A"); err != nil {
		t.Fatalf("handleKey(\"A\"): %v", err)
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
		t.Fatalf("handleKey(\"A\") drop the lit torch: %v", err)
	}

	if len(state.FloorItems) != 0 {
		t.Errorf("expected no floor item created from dropping a lit torch, got %d", len(state.FloorItems))
	}
	got := unmarshalPerson(t, state.PlayerState)
	if got.LitTorch != nil {
		t.Error("expected LitTorch cleared after dropping it")
	}
}

func TestDarkRoomFullyVisibleWhileTorchLit(t *testing.T) {
	m := darkTestMap()
	playerJSON := marshalPerson(t, testPerson{
		ID: "player:1", X: 6, Y: 6, HP: 15, MaxHP: 15, Lit: true,
		LitTorch: &testLitTorch{Item: torchAt(t, &m, 6, 6), TurnsRemaining: 5},
	})
	state := newTestGameState(&m, playerJSON, []MonsterState{deadMonster(t, "goblin")}, nil)

	playerObj, err := playerObject(state.PlayerState)
	if err != nil {
		t.Fatalf("playerObject: %v", err)
	}
	litTiles, _, inDark := vision.CurrentlyLit(*state.Map, playerObj.X, playerObj.Y, playerObj.Lit)
	if !inDark {
		t.Fatal("expected the player's position to be in a dark room")
	}
	if len(litTiles) != 25 {
		t.Fatalf("expected all 25 tiles of the dark room lit while the torch is lit, got %d", len(litTiles))
	}

	farObject := overlay.Object{ID: "goblin:99", X: 8, Y: 8}
	visible := state.Visited.filterObjects(state.Map, []overlay.Object{playerObj, farObject}, litTiles)
	if len(visible) != 2 {
		t.Errorf("expected both the player and the far object visible while the torch is lit, got %+v", visible)
	}
}

func TestDarkRoomTilesSeenWhileTorchLitStayRememberedAfterBurnout(t *testing.T) {
	m := darkTestMap()
	playerJSON := marshalPerson(t, testPerson{
		ID: "player:1", X: 6, Y: 6, HP: 15, MaxHP: 15, Lit: true,
		LitTorch: &testLitTorch{Item: torchAt(t, &m, 6, 6), TurnsRemaining: 2},
	})
	state := newTestGameState(&m, playerJSON, []MonsterState{deadMonster(t, "goblin")}, nil)

	// Turn 1: the torch is still lit going into this turn's Tick (2->1),
	// so this turn's reveal/remember still captures the whole room.
	if _, err := handleKey(state, "."); err != nil {
		t.Fatalf("handleKey(\".\") turn 1: %v", err)
	}
	got := unmarshalPerson(t, state.PlayerState)
	if got.LitTorch == nil || got.LitTorch.TurnsRemaining != 1 {
		t.Fatalf("expected LitTorch with 1 turn remaining, got %+v", got.LitTorch)
	}

	// Turn 2: the torch burns out this turn (1->0).
	msg, err := handleKey(state, ".")
	if err != nil {
		t.Fatalf("handleKey(\".\") turn 2: %v", err)
	}
	if !strings.Contains(msg, "torch burns out") {
		t.Errorf("expected a burn-out message, got %q", msg)
	}
	got = unmarshalPerson(t, state.PlayerState)
	if got.LitTorch != nil {
		t.Error("expected LitTorch cleared after burning out")
	}
	if got.Lit {
		t.Error("expected Lit cleared after burning out")
	}

	// The far corner's floor should still be remembered (it was fully
	// lit during turn 1)...
	remembered := map[mapdata.Point]bool{}
	for _, p := range state.Visited.rememberedTiles() {
		remembered[p] = true
	}
	if !remembered[(mapdata.Point{X: 8, Y: 8})] {
		t.Error("expected the far corner's floor to remain remembered after the torch burned out")
	}

	// ...but an object sitting there must be hidden again, back to
	// radius-1 current-frame visibility.
	playerObj, err := playerObject(state.PlayerState)
	if err != nil {
		t.Fatalf("playerObject: %v", err)
	}
	currentTiles, _, inDark := vision.CurrentlyLit(*state.Map, playerObj.X, playerObj.Y, playerObj.Lit)
	if !inDark {
		t.Fatal("expected radius-1 darkness restriction to apply again with no torch lit")
	}
	farObject := overlay.Object{ID: "goblin:99", X: 8, Y: 8}
	visible := state.Visited.filterObjects(state.Map, []overlay.Object{farObject}, currentTiles)
	if len(visible) != 0 {
		t.Errorf("expected the far object hidden again after burnout, got %+v", visible)
	}
}

func TestTorchBurnsOutMessageReported(t *testing.T) {
	m := itemsTestMap()
	playerJSON := marshalPerson(t, testPerson{
		ID: "player:1", X: 7, Y: 7, HP: 15, MaxHP: 15, Lit: true,
		LitTorch: &testLitTorch{Item: torchAt(t, m, 7, 7), TurnsRemaining: 1},
	})
	state := newTestGameState(m, playerJSON, []MonsterState{deadMonster(t, "goblin")}, nil)

	msg, err := handleKey(state, ".")
	if err != nil {
		t.Fatalf("handleKey(\".\"): %v", err)
	}
	if !strings.Contains(msg, "torch burns out") {
		t.Errorf("expected a burn-out message, got %q", msg)
	}
}
