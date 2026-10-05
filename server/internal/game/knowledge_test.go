package game

import (
	"strings"
	"testing"

	"micro/server/internal/rules"
)

// reading a scroll of knowledge, with two unknown rings and a torch also
// in the pack, every identifiable type unknown. The use picker is
// answered (the scroll is A), so the game is asking which item.
func readingKnowledge(t *testing.T) (*Game, *rules.Entity) {
	t.Helper()
	scroll := testItem(t, "knowledge_scroll", 0, 0)
	p := carrying(testPlayer(t, 7, 7, 100), scroll,
		testItem(t, "ring_of_true_sight", 0, 0), testItem(t, "ring_of_true_sight", 0, 0), testItem(t, "torch", 0, 0))
	g := &Game{state: newTestGameState(t, itemsTestMap(), p, nil, nil)}
	unknownEverything(t, g.state)
	mustKey(t, g.state, "A") // use: the scroll or the torch?
	mustKey(t, g.state, "A") // the scroll
	return g, scroll
}

func TestTheScrollOfKnowledgeAsksWhichItem(t *testing.T) {
	g, _ := readingKnowledge(t)
	look := g.state.Identity.Name(g.state.Player.Inventory[1])
	pr := g.pendingPrompt()
	if pr == nil || pr.Kind != PromptChoose || pr.Title != "Learn the true name of which item?" {
		t.Fatalf("prompt %+v", pr)
	}
	want := []string{"A) " + look, "B) " + look, "C) torch"}
	if strings.Join(pr.Lines, "|") != strings.Join(want, "|") {
		t.Errorf("lines %q, want %q: every other item, by its look", pr.Lines, want)
	}
	if g.state.Turn != 0 {
		t.Error("asking costs no turn")
	}
}

// TestKnowledgeRevealsATypeForGood: the chosen ring's true name, for both
// rings; the scroll is used up, and its own kind now known too.
func TestKnowledgeRevealsATypeForGood(t *testing.T) {
	g, scroll := readingKnowledge(t)
	id := g.state.Identity
	ringLook, scrollLook := id.Name(g.state.Player.Inventory[1]), id.Name(scroll)
	msg := mustKey(t, g.state, "B")
	want := "You read the " + scrollLook + " and learn that the " + ringLook + " is a ring of true sight."
	if !strings.HasPrefix(msg, want) {
		t.Fatalf("message %q, want %q", msg, want)
	}
	lines := inventoryLines(g.state)
	if strings.Join(lines, "|") != "A) ring of true sight|B) ring of true sight|C) torch" {
		t.Errorf("inventory %q: the scroll is used up, both rings known", lines)
	}
	if !id.Known("knowledge_scroll") || g.state.Turn != 1 {
		t.Errorf("reading it should show what it is (%v), and spend a turn (%d)", id.Known("knowledge_scroll"), g.state.Turn)
	}
}

func TestEscapeKeepsTheScrollOfKnowledge(t *testing.T) {
	g, scroll := readingKnowledge(t)
	mustKey(t, g.state, "Escape")
	if g.state.Player.Inventory[0] != scroll || g.state.Identity.Known("ring_of_true_sight") || g.state.Turn != 0 {
		t.Error("cancelling should keep the scroll, reveal nothing and spend nothing")
	}
}

// TestAKnownItemIsNotWorthAScroll: choosing something already known is
// refused, and the scroll kept.
func TestAKnownItemIsNotWorthAScroll(t *testing.T) {
	g, scroll := readingKnowledge(t)
	msg := mustKey(t, g.state, "C") // the torch
	if msg != "You already know the torch." || g.state.Player.Inventory[0] != scroll || g.state.Turn != 0 {
		t.Errorf("%q, turn %d: the scroll should be kept", msg, g.state.Turn)
	}
}

func TestKnowledgeWithNothingElseCarried(t *testing.T) {
	scroll := testItem(t, "knowledge_scroll", 0, 0)
	state := newTestGameState(t, itemsTestMap(), carrying(testPlayer(t, 7, 7, 100), scroll), nil, nil)
	msg := mustKey(t, state, "A")
	if msg != "You have nothing else to use it on." || state.PendingSelection != nil || len(state.Player.Inventory) != 1 {
		t.Errorf("%q: the scroll should be kept", msg)
	}
}
