package main

import "strings"

// answerGate decides which typed runes answer a "character" prompt.
//
// GLFW reports one keystroke twice: SetOnTypedKey first, with the key's
// name, then SetOnTypedRune, with the character. The keystroke that opens
// a character prompt — the letter choosing the scroll — arrives by name
// and opens it, and then its own rune follows. That rune is not an answer:
// the player has not seen the question yet. So the gate swallows it, and
// only the next keystroke's rune answers.
type answerGate struct {
	open bool
	// openedBy is the key name whose rune is still to come, if the
	// keystroke that opened the prompt was a key with one.
	openedBy string
}

// update records what a response left open: a character prompt or not.
// key is the name of the key that was sent.
func (g *answerGate) update(open bool, key string) {
	if open && !g.open {
		g.openedBy = key
	}
	if !open {
		g.openedBy = ""
	}
	g.open = open
}

// answer reports whether rune r, just typed, answers the open prompt.
func (g *answerGate) answer(r rune) bool {
	if !g.open {
		return false
	}
	if g.openedBy != "" {
		openedBy := g.openedBy
		g.openedBy = ""
		if strings.EqualFold(string(r), openedBy) {
			return false // the opening keystroke's own rune
		}
	}
	return true
}
