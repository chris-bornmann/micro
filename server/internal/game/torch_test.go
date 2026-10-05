package game

import (
	"strings"
	"testing"

	"micro/server/internal/mapdata"
	"micro/server/internal/overlay"
	"micro/server/internal/rules"
	"micro/server/internal/vision"
)

func TestHandleKeyUseOffersTorchAndPotionTogetherAndRoutesCorrectly(t *testing.T) {
	torch, potion := testItem(t, "torch", 0, 0), testItem(t, "healing_potion", 0, 0)
	state := newTestGameState(t, itemsTestMap(), carrying(testPlayer(t, 7, 7, 15), torch, potion), nil, nil)

	mustKey(t, state, "A")
	if sel := state.PendingSelection; sel == nil || len(sel.Candidates) != 2 {
		t.Fatalf("expected a picker over the torch and the potion, got %+v", sel)
	}
	mustKey(t, state, "A") // the torch
	if state.Player.Lit() != torch || len(state.Player.Inventory) != 1 {
		t.Error("expected the torch lit and the potion still carried")
	}
}

func TestLightingATorchLightsThePlayer(t *testing.T) {
	torch := testItem(t, "torch", 7, 7)
	state := newTestGameState(t, itemsTestMap(), testPlayer(t, 7, 7, 15), nil, []*rules.Entity{torch})
	mustKey(t, state, ",")
	mustKey(t, state, "A")
	if !state.eng.Reg.Has(state.Player, "light") {
		t.Error("expected the Player lit")
	}
	if inst := torch.FindTrait("light_source"); inst == nil || inst.Turns <= 0 {
		t.Errorf("expected a burning countdown, got %+v", inst)
	}
}

func TestApplyLightRefusesSecondTorchWhileOneIsLit(t *testing.T) {
	state := newTestGameState(t, itemsTestMap(), carrying(testPlayer(t, 7, 7, 15), testItem(t, "torch", 0, 0)), nil,
		[]*rules.Entity{testItem(t, "torch", 7, 7)})

	mustKey(t, state, ",")
	mustKey(t, state, "A")
	mustKey(t, state, "A") // light the first of the two
	if state.Player.Lit() == nil || len(state.Player.Inventory) != 1 {
		t.Fatal("expected one torch lit and one carried")
	}
	// One usable item left, so "A" acts on it at once — and is refused.
	if msg := mustKey(t, state, "A"); !strings.Contains(msg, "already lit") || len(state.Player.Inventory) != 1 {
		t.Errorf("got %q, inventory %d", msg, len(state.Player.Inventory))
	}
}

func TestApplyDropLitTorchCreatesNoFloorItem(t *testing.T) {
	state := newTestGameState(t, itemsTestMap(), testPlayer(t, 7, 7, 15), nil, []*rules.Entity{testItem(t, "torch", 7, 7)})
	mustKey(t, state, ",")
	mustKey(t, state, "A")
	mustKey(t, state, "D")
	if msg := mustKey(t, state, "A"); msg != "You drop the torch. It goes out and falls apart." {
		t.Errorf("got %q", msg)
	}
	if len(state.FloorItems) != 0 || state.Player.Lit() != nil {
		t.Error("expected the lit torch gone, not on the floor")
	}
}

// litPlayer is a Player at (x, y) holding a torch with turns left to burn.
func litPlayer(t *testing.T, x, y, turns int) *rules.Entity {
	p := testPlayer(t, x, y, 15)
	rules.Light(p, carrying(p, testItem(t, "torch", 0, 0)).Inventory[0], turns)
	return p
}

func TestDarkRoomFullyVisibleWhileTorchLit(t *testing.T) {
	m := darkTestMap()
	state := newTestGameState(t, &m, litPlayer(t, 6, 6, 5), nil, nil)

	lit, _, inDark := vision.CurrentlyLit(m, 6, 6, state.eng.Reg.Has(state.Player, "light"))
	if !inDark || len(lit) != 25 {
		t.Fatalf("expected all 25 tiles of the dark room lit, got %d (in dark: %v)", len(lit), inDark)
	}
	far := sighting{Object: overlay.Object{ID: "goblin:99", X: 8, Y: 8}}
	if visible := state.Visited.filterObjects(state.Map, []sighting{far}, lit); len(visible) != 1 {
		t.Error("expected a far object in the dark room visible by torchlight")
	}
}

func TestDarkRoomTilesSeenWhileTorchLitStayRememberedAfterBurnout(t *testing.T) {
	m := darkTestMap()
	state := newTestGameState(t, &m, litPlayer(t, 6, 6, 2), nil, nil)

	mustKey(t, state, ".") // 2 -> 1: still lit for this turn's reveal
	if msg := mustKey(t, state, "."); !strings.Contains(msg, "torch burns out") {
		t.Errorf("expected a burn-out message, got %q", msg)
	}
	if state.eng.Reg.Has(state.Player, "light") {
		t.Fatal("expected the torch out")
	}

	remembered := map[mapdata.Point]bool{}
	for _, p := range state.Visited.rememberedTiles() {
		remembered[p] = true
	}
	if !remembered[mapdata.Point{X: 8, Y: 8}] {
		t.Error("the far corner's floor, seen by torchlight, should stay remembered")
	}
	current, _, _ := vision.CurrentlyLit(m, 6, 6, false)
	far := sighting{Object: overlay.Object{ID: "goblin:99", X: 8, Y: 8}}
	if visible := state.Visited.filterObjects(state.Map, []sighting{far}, current); len(visible) != 0 {
		t.Error("but an object there should be hidden again in the dark")
	}
}
