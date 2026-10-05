package main

import "testing"

// TestTheOpeningKeystrokeDoesNotAnswer: "C" chose the scroll and opened
// the prompt; its own rune "c" follows and must be ignored. The next
// keystroke, "T", answers.
func TestTheOpeningKeystrokeDoesNotAnswer(t *testing.T) {
	var g answerGate
	g.update(true, "C")
	if g.answer('c') {
		t.Fatal("the rune of the keystroke that opened the prompt answered it")
	}
	if !g.answer('T') {
		t.Error("the next keystroke should answer")
	}
}

// TestTheSameLetterCanStillBeTheAnswer: only the opening keystroke's own
// rune is swallowed — typing that letter again answers.
func TestTheSameLetterCanStillBeTheAnswer(t *testing.T) {
	var g answerGate
	g.update(true, "C")
	g.answer('c')
	if !g.answer('c') {
		t.Error("a second c is a real answer")
	}
}

func TestNoPromptNoAnswer(t *testing.T) {
	var g answerGate
	if g.answer('t') {
		t.Error("no prompt is open")
	}
	g.update(true, "A")
	g.update(false, "Escape")
	if g.answer('t') {
		t.Error("the prompt was closed")
	}
}
