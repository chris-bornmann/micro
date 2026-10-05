package game

import (
	"fmt"
	"testing"

	"micro/server/internal/grid"
	"micro/server/internal/overlay"
	"micro/server/internal/rules"
)

// phantomScene is a Player at (7,7) with a phantom (full HP, so hidden)
// right beside them, and the room already revealed.
func phantomScene(t *testing.T) (*GameState, *rules.Entity) {
	t.Helper()
	ph := testMonster(t, "phantom", 8, 7, 10)
	state := newTestGameState(t, itemsTestMap(), testPlayer(t, 7, 7, 100), []*rules.Entity{ph}, nil)
	revealAroundPlayer(state)
	return state, ph
}

// cellAt is the glyph and "#rrggbb" color drawn at (x, y).
func cellAt(t *testing.T, f Frame, x, y int) (rune, string) {
	t.Helper()
	ch, err := glyphAt(f, x, y)
	if err != nil {
		t.Fatal(err)
	}
	return ch, f.Palette[f.Colors[y][x]]
}

func hex(c overlay.Color) string { return fmt.Sprintf("#%02x%02x%02x", c.R, c.G, c.B) }

func TestAnInvisibleMonsterIsDrawnOnlyWithRevealHidden(t *testing.T) {
	state, _ := phantomScene(t)

	if ch, _ := cellAt(t, buildFrame(state, 0, false, false), 8, 7); ch == 'p' {
		t.Error("an invisible phantom was drawn")
	}
	if ch, _ := cellAt(t, buildFrame(state, 0, false, true), 8, 7); ch == 'p' {
		t.Error("-show-full-map lifts fog of war, not invisibility")
	}
	ch, color := cellAt(t, buildFrame(state, 0, true, false), 8, 7)
	if want := hex(grid.RevealColor); ch != 'p' || color != want {
		t.Errorf("-reveal-hidden should draw it as p in %s, got %q in %s", want, ch, color)
	}
}

func TestAWoundedPhantomShowsInItsOwnColor(t *testing.T) {
	state, ph := phantomScene(t)
	ph.HP = 3 // below 4
	ch, color := cellAt(t, buildFrame(state, 0, false, false), 8, 7)
	if want := hex(ph.T.Color); ch != 'p' || color != want {
		t.Errorf("got %q in %s, want p in %s", ch, color, want)
	}
}

func TestADeadPhantomsCorpseIsDrawn(t *testing.T) {
	state, ph := phantomScene(t)
	state.Player.Attrs.Strength = 100 // one blow kills
	mustKey(t, state, "Right")        // you can still bump into what you cannot see
	if !ph.Dead {
		t.Fatal("expected the phantom dead")
	}
	if ch, _ := cellAt(t, buildFrame(state, 0, false, false), 8, 7); ch != '%' {
		t.Errorf("expected its corpse drawn, got %q", ch)
	}
}
