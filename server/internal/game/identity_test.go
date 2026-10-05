package game

import (
	"math/rand"
	"strings"
	"testing"

	"micro/server/internal/rules"
	"micro/server/internal/rules/rulestest"
)

// unknownEverything deals state an Identity in which every identifiable
// type starts unknown. It deals from a freshly loaded catalog, made
// unknowing, so the engine every test shares is left alone; looks are
// keyed by template ID, so they apply to state's items all the same.
func unknownEverything(t *testing.T, state *GameState) {
	t.Helper()
	cat := rulestest.Engine(t).Cat
	for _, tmpl := range cat.All() {
		if tmpl.Unidentified != nil {
			tmpl.Unidentified.Chance = 100
		}
	}
	state.Identity = rules.NewIdentity(cat, rand.New(rand.NewSource(1)))
}

func TestTheInventoryShowsLooksAndCopiesAgree(t *testing.T) {
	p := testPlayer(t, 7, 7, 100)
	carrying(p, testItem(t, "healing_potion", 0, 0), testItem(t, "healing_potion", 0, 0), testItem(t, "torch", 0, 0))
	state := newTestGameState(t, itemsTestMap(), p, nil, nil)
	unknownEverything(t, state)

	look := state.Identity.Name(p.Inventory[0])
	lines := inventoryLines(state)
	if lines[0] != "A) "+look || lines[1] != "B) "+look || lines[2] != "C) torch" {
		t.Fatalf("inventory %q: both potions should show %q, the torch its name", lines, look)
	}
	if strings.Contains(strings.Join(lines, " "), "healing") {
		t.Error("an unknown potion gave itself away")
	}

	state.Identity.Identify("healing_potion")
	if lines := inventoryLines(state); lines[0] != "A) healing potion" || lines[1] != "B) healing potion" {
		t.Errorf("identified: %q", lines)
	}
}

func TestTheDropPickerShowsLooks(t *testing.T) {
	p := carrying(testPlayer(t, 7, 7, 100), testItem(t, "ring_of_true_sight", 0, 0), testItem(t, "torch", 0, 0))
	g := &Game{state: newTestGameState(t, itemsTestMap(), p, nil, nil)}
	unknownEverything(t, g.state)
	mustKey(t, g.state, "D")
	pr := g.pendingPrompt()
	if pr == nil || pr.Lines[0] != "A) "+g.state.Identity.Name(p.Inventory[0]) || strings.Contains(pr.Lines[0], "true sight") {
		t.Errorf("drop picker %+v", pr)
	}
}

// TestYouKnowYourOwnKit: a new game's player recognizes everything they
// start with — here a hand-crafted player's dagger, shoes and ring.
func TestYouKnowYourOwnKit(t *testing.T) {
	g := examplePlayer(t)
	for _, it := range g.state.Player.Carried() {
		if !g.state.Identity.Known(it.T.ID) {
			t.Errorf("the starting %s is unidentified", it.Name)
		}
	}
}

func TestEachGameDealsItsOwnLooks(t *testing.T) {
	looks := map[string]bool{}
	for seed := int64(1); seed <= 10; seed++ {
		cfg := testLevelConfig()
		cfg.Seed = seed
		g, err := NewGame(cfg, false, false, false)
		if err != nil {
			t.Fatal(err)
		}
		ring := &rules.Entity{T: g.state.eng.Cat.Get("ring_of_true_sight"), Name: "ring of true sight"}
		looks[g.state.Identity.Name(ring)] = true
	}
	if len(looks) < 3 {
		t.Errorf("ten games dealt the ring of true sight only %d looks: %v", len(looks), looks)
	}
}

// TestAMonstersPotionIsNamedAsThePlayerKnowsIt: a hurt thief drinks his
// healing potion on his turn. While the Player doesn't know healing
// potions, they see only its look; once they do, its true name.
func TestAMonstersPotionIsNamedAsThePlayerKnowsIt(t *testing.T) {
	for _, known := range []bool{false, true} {
		thief := testMonster(t, "thief", 9, 7, 20)
		thief.HP = 4 // below his drink-to-heal threshold
		carrying(thief, testItem(t, "healing_potion", 0, 0))
		state := newTestGameState(t, itemsTestMap(), testPlayer(t, 7, 7, 100), []*rules.Entity{thief}, nil)
		unknownEverything(t, state)
		look := state.Identity.Name(thief.Inventory[0])
		want := "The thief drinks a " + look + " and looks healthier."
		if known {
			state.Identity.Identify("healing_potion")
			want = "The thief drinks a healing potion and looks healthier."
		}

		msg := mustKey(t, state, ".")
		if !strings.Contains(msg, want) {
			t.Errorf("known %v: %q, want it to contain %q", known, msg, want)
		}
		if !known && strings.Contains(msg, "healing") {
			t.Errorf("an unknown potion gave itself away: %q", msg)
		}
	}
}
