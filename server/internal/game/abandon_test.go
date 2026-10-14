package game

import "testing"

// abandonTestGame is a game on one plain room, for the tests below that
// drive it through the same API a client uses.
func abandonTestGame(t *testing.T) *Game {
	t.Helper()
	return &Game{state: newTestGameState(t, itemsTestMap(), testPlayer(t, 2, 2, 10), nil, nil)}
}

func TestAbandoningTheQuestAsksFirstAndCostsNoTurn(t *testing.T) {
	g := abandonTestGame(t)

	resp, err := g.Command("X")
	if err != nil {
		t.Fatal(err)
	}
	if resp.Prompt == nil || resp.Frame != nil {
		t.Fatalf("X should ask before doing anything, got %+v", resp)
	}
	if resp.Prompt.Kind != PromptCharacter || resp.Prompt.Lines[0] != abandonQuestQuestion {
		t.Errorf("prompt = %+v, want a typed answer to %q", resp.Prompt, abandonQuestQuestion)
	}
	if g.state.Turn != 0 || g.Over() {
		t.Errorf("asking must change nothing: turn %d, over %v", g.state.Turn, g.Over())
	}
}

func TestAYesAbandonsTheQuestAndEndsTheGame(t *testing.T) {
	for _, yes := range []string{"y", "Y"} {
		g := abandonTestGame(t)
		if _, err := g.Command("X"); err != nil {
			t.Fatal(err)
		}

		resp, err := g.Command(yes)
		if err != nil {
			t.Fatal(err)
		}
		if resp.Frame == nil || !resp.Frame.GameOver {
			t.Fatalf("%q: the frame should show the game over, got %+v", yes, resp)
		}
		if len(resp.Messages) != 1 || resp.Messages[0] != "You abandon your quest." {
			t.Errorf("%q: messages = %v", yes, resp.Messages)
		}
		// Giving up is not a turn: the monsters get no last move.
		if resp.Frame.Turn != 0 {
			t.Errorf("%q: turn = %d, want 0", yes, resp.Frame.Turn)
		}
		if !g.Over() {
			t.Errorf("%q: Over() = false after abandoning", yes)
		}
		if msg := mustKey(t, g.state, "."); msg != "Game Over" {
			t.Errorf("%q: a key after abandoning = %q, want \"Game Over\"", yes, msg)
		}
	}
}

func TestAnyOtherAnswerKeepsTheQuestGoing(t *testing.T) {
	for _, no := range []string{"n", "N", "Escape", "x"} {
		g := abandonTestGame(t)
		if _, err := g.Command("X"); err != nil {
			t.Fatal(err)
		}

		resp, err := g.Command(no)
		if err != nil {
			t.Fatal(err)
		}
		if g.Over() || g.state.PendingSelection != nil {
			t.Errorf("%q: over %v, still asking %v", no, g.Over(), g.state.PendingSelection != nil)
		}
		if len(resp.Messages) != 0 || resp.Frame == nil || resp.Frame.Turn != 0 {
			t.Errorf("%q: should cancel silently and spend nothing, got %+v", no, resp)
		}
	}
}
