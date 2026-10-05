package game

import (
	"testing"

	"micro/server/internal/attributes"
	"micro/server/internal/rules"
)

// TestTheDefaultPlayerIsUnchanged pins the built-in player for one seed:
// the per-stat attributes, and starting the player through Create, must
// roll exactly what a seed always has — the same HP, stats and position.
func TestTheDefaultPlayerIsUnchanged(t *testing.T) {
	g, err := NewGame(testLevelConfig(), false, false, false)
	if err != nil {
		t.Fatal(err)
	}
	p := g.state.Player
	want := attributes.Attributes{Strength: 18, Intelligence: 8, Dexterity: 19, Stamina: 12}
	if p.HP != 12 || p.MaxHP != 12 || p.Attrs != want || p.X != 17 || p.Y != 18 {
		t.Errorf("seed 99's player: HP %d/%d, %+v at (%d,%d)", p.HP, p.MaxHP, p.Attrs, p.X, p.Y)
	}
	if len(p.Inventory) != 0 || len(p.Equipped) != 0 || len(p.Traits) != 0 {
		t.Error("the built-in player starts with nothing")
	}
}

// examplePlayer starts a game with testdata/player.yaml as the player,
// as mapd -player does, putting the built-in player back afterward.
func examplePlayer(t *testing.T) *Game {
	t.Helper()
	if err := LoadContent("testdata/player.yaml"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { playerEngine = nil })
	g, err := NewGame(testLevelConfig(), false, false, false)
	if err != nil {
		t.Fatal(err)
	}
	return g
}

func TestAHandCraftedPlayerStartsAsWritten(t *testing.T) {
	p := examplePlayer(t).state.Player
	if p.HP != 40 || p.MaxHP != 40 || p.Attrs.Strength != 18 || p.Attrs.Dexterity != 15 || p.Attrs.Stamina != 8 {
		t.Errorf("HP %d/%d, attributes %+v", p.HP, p.MaxHP, p.Attrs)
	}
	for slot, id := range map[rules.Slot]string{
		rules.SlotWeapon: "fine_dagger", rules.SlotFeet: "shoes_of_sneaking", rules.SlotRightHand: "ring_of_true_sight",
	} {
		if it := p.Equipped[slot]; it == nil || it.T.ID != id {
			t.Errorf("%s: %v, want %s", slot, it, id)
		}
	}
	// Two potions and the summon scroll for sure; the teleport scroll
	// half the time.
	if n := len(p.Inventory); n < 3 || n > 4 || p.Inventory[0].T.ID != "healing_potion" || p.Inventory[2].T.ID != "summon_scroll" {
		t.Errorf("inventory %v", names(p.Inventory))
	}
	if inst := p.FindTrait("poisoned"); inst == nil || inst.Turns != 5 || inst.Params.Int("damage") != 2 {
		t.Errorf("poisoned %+v", inst)
	}
}

// TestAStartingPoisonWearsOff: duration 5 means five turns, then gone.
func TestAStartingPoisonWearsOff(t *testing.T) {
	g := examplePlayer(t)
	p := g.state.Player
	for turn := 1; turn <= 5; turn++ {
		if p.FindTrait("poisoned") == nil {
			t.Fatalf("worn off before turn %d", turn)
		}
		mustKey(t, g.state, ".")
	}
	if p.FindTrait("poisoned") != nil {
		t.Error("still poisoned after 5 turns")
	}
	if p.HP > 40-10 {
		t.Errorf("five turns at 2 a turn should cost at least 10 HP: %d", p.HP)
	}
}

// TestNoPlayerFileIsTheBuiltInPlayer: LoadContent("") is the default.
func TestNoPlayerFileIsTheBuiltInPlayer(t *testing.T) {
	if err := LoadContent(""); err != nil || playerEngine != nil {
		t.Errorf("err %v, custom engine %v", err, playerEngine)
	}
}

// TestAHurtPlayerHealsWhileWaiting: Strength + Stamina 25 heals 1 HP every
// 20 turns spent, here by waiting.
func TestAHurtPlayerHealsWhileWaiting(t *testing.T) {
	p := testPlayer(t, 7, 7, 100)
	p.HP, p.Attrs.Strength, p.Attrs.Stamina = 50, 12, 13
	state := newTestGameState(t, itemsTestMap(), p, nil, nil)
	for i := 0; i < 40; i++ {
		mustKey(t, state, ".")
	}
	if p.HP != 52 {
		t.Errorf("40 turns should heal 2 HP: %d", p.HP)
	}
}
