package main

import (
	"strings"
	"testing"

	"map_api/httpclient"
)

// sampleFrame is a small frame in the wire form PROTOCOL.md describes.
func sampleFrame() httpclient.Frame {
	return httpclient.Frame{
		Turn:   4,
		Title:  "Hack — level 1, seed 1234",
		Level:  1,
		Width:  4,
		Height: 2,
		Rows:   []string{"#..#", " @g "},
		Colors: [][]int{{0, 1, 1, 0}, {2, 3, 4, 2}},
		Palette: []string{
			"#828282", "#e8dfa4", "#000000", "#ffffff", "#00c800",
		},
		HUD: []string{"HP: 18/18", "Level: 1"},
	}
}

func TestRenderDrawsTheBoardHUDAndMessages(t *testing.T) {
	var b strings.Builder

	render(&b, sampleFrame(), []string{"Goblin takes 3 damage", "You take 2 damage"}, "Left")

	out := b.String()
	for _, want := range []string{
		"Hack — level 1, seed 1234",
		"(turn 4)",
		"HP: 18/18",
		"Goblin takes 3 damage",
		"You take 2 damage",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output is missing %q:\n%s", want, out)
		}
	}
	// Two board rows plus the surrounding furniture.
	if n := strings.Count(out, "\n"); n < 4 {
		t.Errorf("expected several lines, got %d:\n%s", n, out)
	}
}

// TestRenderReportsAMalformedFrameInsteadOfPanicking matters because a frame
// is a network response: a server that sends a row of the wrong length must
// produce a message, not a crash mid-game.
func TestRenderReportsAMalformedFrame(t *testing.T) {
	f := sampleFrame()
	f.Rows = []string{"###"} // wrong count and wrong width

	var b strings.Builder
	render(&b, f, nil, ".")

	out := b.String()
	if !strings.Contains(out, "malformed frame") {
		t.Errorf("expected a malformed-frame message, got:\n%s", out)
	}
}

func TestRenderOmitsAnEmptyHUD(t *testing.T) {
	f := sampleFrame()
	f.HUD = nil

	var b strings.Builder
	render(&b, f, nil, ".")

	if strings.Contains(b.String(), "HP:") {
		t.Error("expected no HUD line when the frame carries none")
	}
}

func TestKeyForTranslatesTypedWords(t *testing.T) {
	cases := map[string]string{
		"h": "Left", "j": "Down", "k": "Up", "l": "Right",
		"left": "Left", "up": "Up",
		"wait": ".", "get": ",",
		"down-stairs": ">", "up-stairs": "<",
		"esc": "Escape", "escape": "Escape",
	}
	for typed, want := range cases {
		if got := keyFor[typed]; got != want {
			t.Errorf("keyFor[%q] = %q, want %q", typed, got, want)
		}
	}
}

// TestKeyForLeavesRealKeyNamesAlone is why unknown words are passed through: a
// single letter like "A" or "D" is already a protocol key, and an unrecognized
// word should reach the server so the server's own answer is what the player
// sees.
func TestKeyForLeavesRealKeyNamesAlone(t *testing.T) {
	for _, key := range []string{"A", "D", "E", "T", "Up", "Down", "banana"} {
		if mapped, ok := keyFor[strings.ToLower(key)]; ok && mapped != key {
			// Only flag it when the mapping would change a real key name into
			// something else; lowercase aliases are intentional.
			if strings.ToLower(key) == key && len(key) > 1 {
				continue
			}
			t.Errorf("keyFor would rewrite the protocol key %q to %q", key, mapped)
		}
	}
}

func TestViewForCoversEveryServerView(t *testing.T) {
	// The four the server implements; see PROTOCOL.md.
	for _, name := range []string{"inventory", "look", "status", "help"} {
		found := false
		for _, mapped := range viewFor {
			if mapped == name {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("no typed word reaches the %q view", name)
		}
	}
	// And nothing maps to a view the server does not have.
	valid := map[string]bool{"inventory": true, "look": true, "status": true, "help": true}
	for typed, mapped := range viewFor {
		if !valid[mapped] {
			t.Errorf("viewFor[%q] = %q, which is not a server view", typed, mapped)
		}
	}
}
