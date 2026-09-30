package game

import (
	"encoding/json"
	"strings"
	"testing"

	"micro/server/internal/player"
)

// The tests here are the ones that matter most for the client/server split:
// they assert what a frame CANNOT carry. Everything a frame omits is
// something a client cannot leak, so these are the guarantees that let the
// client be untrusted.

// frameFor builds a frame from a real level-1 state at the default cheat
// settings (both off, i.e. what a player actually gets).
func frameFor(t *testing.T) Frame {
	t.Helper()
	state := newLevel1TestState(t)
	f, err := buildFrame(state, 0, false, false)
	if err != nil {
		t.Fatalf("buildFrame: %v", err)
	}
	return f
}

// TestFrameCarriesNoSeed is the single most important assertion in this
// file. mapgen.Generate is deterministic in its seed, so a client holding
// one can reconstruct the whole level — every unexplored room, every hidden
// door — no matter how carefully the map it was sent was filtered. Before
// the split, Visibility.visibleMap copied the seed into the filtered map it
// returned.
//
// The seed does appear in Frame.Title for bug reports. That is fine and is
// the point of checking the marshalled JSON keys rather than grepping the
// whole payload for the number: inert text in a caption is not a dungeon.
func TestFrameCarriesNoSeed(t *testing.T) {
	f := frameFor(t)

	data, err := json.Marshal(f)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var generic map[string]any
	if err := json.Unmarshal(data, &generic); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for key := range generic {
		if strings.Contains(strings.ToLower(key), "seed") {
			t.Errorf("frame has a %q field; a client holding the seed can regenerate the level", key)
		}
	}
}

// TestFrameCarriesNoLevelStructure guards the other half of the same idea.
// A rasterized grid has no rooms, corridors, doors or objects as such —
// only characters. Room identities alone would tell a player how many rooms
// they have not found yet.
func TestFrameCarriesNoLevelStructure(t *testing.T) {
	f := frameFor(t)

	data, err := json.Marshal(f)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var generic map[string]any
	if err := json.Unmarshal(data, &generic); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	want := map[string]bool{
		"turn": true, "title": true, "level": true,
		"width": true, "height": true,
		"rows": true, "colors": true, "palette": true,
		"hud": true, "game_over": true,
	}
	for key := range generic {
		if !want[key] {
			t.Errorf("unexpected field %q on the wire; a frame should be characters and text only", key)
		}
	}
	for _, forbidden := range []string{"rooms", "corridors", "doors", "objects", "map", "lit_tiles"} {
		if _, ok := generic[forbidden]; ok {
			t.Errorf("frame exposes %q", forbidden)
		}
	}
}

// TestFrameHidesUnexploredGeometry checks the fog of war actually reaches
// the wire: at spawn the player has seen one room, so the vast majority of
// a 80x40 level must still be blank.
func TestFrameHidesUnexploredGeometry(t *testing.T) {
	f := frameFor(t)

	blank, total := 0, 0
	for _, row := range f.Rows {
		for _, ch := range row {
			total++
			if ch == ' ' {
				blank++
			}
		}
	}
	if total == 0 {
		t.Fatal("frame has no cells")
	}
	if ratio := float64(blank) / float64(total); ratio < 0.5 {
		t.Errorf("only %.0f%% of the frame is blank at spawn; unexplored geometry is being sent", ratio*100)
	}
}

// TestFrameShowsEverythingWithShowFullMap is the control for the test
// above: the same state, cheating, must reveal substantially more. Without
// this, a frame that was blank for the wrong reason would pass.
func TestFrameShowsEverythingWithShowFullMap(t *testing.T) {
	state := newLevel1TestState(t)

	fogged, err := buildFrame(state, 0, false, false)
	if err != nil {
		t.Fatalf("buildFrame: %v", err)
	}
	full, err := buildFrame(state, 0, false, true)
	if err != nil {
		t.Fatalf("buildFrame: %v", err)
	}

	drawn := func(f Frame) int {
		n := 0
		for _, row := range f.Rows {
			for _, ch := range row {
				if ch != ' ' {
					n++
				}
			}
		}
		return n
	}

	if drawn(full) <= drawn(fogged) {
		t.Errorf("-show-full-map drew %d cells, fog of war drew %d; expected strictly more",
			drawn(full), drawn(fogged))
	}
}

// TestFrameConformsToTheProtocol closes the loop on the wire format: the
// server's encoder and every client's decoder share no code, so something has
// to check the output against the documented shape. This runs on a real
// generated level rather than a hand-built grid, and through JSON, exactly as
// a client receives it.
func TestFrameConformsToTheProtocol(t *testing.T) {
	f := frameFor(t)

	data, err := json.Marshal(f)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got Frame
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	assertFrameConformsToProtocol(t, got)
}

// TestFrameShowsThePlayer is the sanity check that the object pass survives
// fog filtering — the player is always visible to themselves, including
// inside a dark room, whose rectangle is never sent as such.
//
// It reads the expected glyph out of the state rather than hardcoding '@'
// so it tests buildFrame rather than the fixture: newLevel1TestState builds
// a synthetic player, not one from player.Init, and leaves Glyph at zero.
// Checking the player's own coordinates is the stronger assertion anyway —
// it catches the object landing on the frame at the wrong place, which
// searching the whole grid for a character would not.
func TestFrameShowsThePlayer(t *testing.T) {
	state := newLevel1TestState(t)
	p, err := playerObject(state.PlayerState)
	if err != nil {
		t.Fatalf("playerObject: %v", err)
	}
	f, err := buildFrame(state, 0, false, false)
	if err != nil {
		t.Fatalf("buildFrame: %v", err)
	}

	want := p.Glyph
	if want == 0 {
		want = ' ' // Build normalises a zero glyph to a blank cell
	}
	got, err := glyphAt(f, p.X, p.Y)
	if err != nil {
		t.Fatalf("glyphAt(%d,%d): %v", p.X, p.Y, err)
	}
	if got != want {
		t.Errorf("cell at the player's position (%d,%d): got %q, want the player's own glyph %q",
			p.X, p.Y, got, want)
	}
}

// TestFrameDrawsTheRealPlayerGlyph complements the test above using a player
// built the way the game builds one, so the '@' actually reaching the wire
// is covered too.
func TestFrameDrawsTheRealPlayerGlyph(t *testing.T) {
	cfg := testLevelConfig()
	ls, err := populateLevel(cfg, 1, map[string]bool{})
	if err != nil {
		t.Fatalf("populateLevel: %v", err)
	}
	playerState, err := initPlayer(ls.Map)
	if err != nil {
		t.Fatalf("initPlayer: %v", err)
	}
	state := &GameState{
		PlayerState:  playerState,
		CurrentLevel: 1,
		Levels:       make([]*levelState, maxLevels),
		LevelConfig:  cfg,
		SpawnedOnce:  map[string]bool{},
		levelState:   ls,
	}
	state.Levels[0] = ls
	p, err := playerObject(state.PlayerState)
	if err != nil {
		t.Fatalf("playerObject: %v", err)
	}
	state.Visited.reveal(state.Map, p.X, p.Y)

	f, err := buildFrame(state, 0, false, false)
	if err != nil {
		t.Fatalf("buildFrame: %v", err)
	}

	found := false
	for _, row := range f.Rows {
		if strings.ContainsRune(row, '@') {
			found = true
			break
		}
	}
	if !found {
		t.Error("a real player's '@' never reached the frame")
	}
}

func TestFrameCarriesAHUDAndTitle(t *testing.T) {
	f := frameFor(t)

	if len(f.HUD) == 0 {
		t.Error("expected a HUD; a client cannot compose one itself")
	}
	for _, line := range f.HUD {
		if !strings.Contains(line, ":") {
			t.Errorf("HUD line %q is not preformatted as \"Label: Value\"", line)
		}
	}
	if f.Title == "" {
		t.Error("expected a title; the client has no seed or room count to build one from")
	}
	if f.Level != 1 {
		t.Errorf("Level = %d, want 1", f.Level)
	}
}

// TestTurnCountsTurnsNotKeypresses pins the distinction a client relies on to
// tell "a turn passed" from "nothing happened" without reading messages.
//
// It was originally wrong in the other direction: the counter lived on Game
// and incremented once per command that did not leave a prompt open, so
// cancelling a picker with Escape — which spends no turn — still advanced it.
// Counting inside finishTurn instead makes the number mean what it says,
// because finishTurn is reached by exactly the branches that spend a turn.
func TestTurnCountsTurnsNotKeypresses(t *testing.T) {
	state := newLevel1TestState(t)

	if state.Turn != 0 {
		t.Fatalf("Turn = %d on a fresh game, want 0", state.Turn)
	}

	// Waiting spends a turn.
	if _, err := handleKey(state, "."); err != nil {
		t.Fatalf("handleKey(\".\"): %v", err)
	}
	if state.Turn != 1 {
		t.Errorf("Turn = %d after waiting, want 1", state.Turn)
	}

	// Opening a picker does not. "D" always opens one when anything is
	// carried, so give the Player something first.
	give := func() {
		t.Helper()
		raw := potionAt(t, state.Map, 0, 0)
		items, err := player.PickUp(state.PlayerState, raw)
		if err != nil {
			t.Fatalf("PickUp: %v", err)
		}
		state.PlayerState = items
	}
	give()
	before := state.Turn
	if _, err := handleKey(state, "D"); err != nil {
		t.Fatalf("handleKey(\"D\"): %v", err)
	}
	if state.PendingSelection == nil {
		t.Fatal("expected \"D\" to open a picker")
	}
	if state.Turn != before {
		t.Errorf("Turn = %d after opening a picker, want %d unchanged", state.Turn, before)
	}

	// Cancelling it does not either.
	if _, err := handleKey(state, "Escape"); err != nil {
		t.Fatalf("handleKey(\"Escape\"): %v", err)
	}
	if state.PendingSelection != nil {
		t.Error("expected Escape to cancel the picker")
	}
	if state.Turn != before {
		t.Errorf("Turn = %d after cancelling a picker, want %d unchanged", state.Turn, before)
	}
}
