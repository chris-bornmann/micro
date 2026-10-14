// Command hackgui is the graphical client: it draws frames and sends
// keypresses. It contains no game logic of any kind.
//
// Everything it knows about the game arrives over HTTP, and everything it
// draws arrives as a grid of characters it cannot interpret. It cannot tell a
// wall from a monster, does not know what an item is, and has no way to find
// out — which is the point. Fog of war, hidden doors and the level itself are
// all decided on the far side of the connection.
//
// It links no game code at all; a test in package httpclient asserts that of
// every client package.
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"micro/client/internal/httpclient"
	"micro/client/internal/render"
	"micro/client/internal/screen"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"
)

func main() {
	// Only presentation and connection settings live here. Dungeon geometry is
	// the server's policy and the two cheat switches are its flags, so this
	// client has no way to ask for either — see PROTOCOL.md.
	url := flag.String("url", "http://localhost:8080", "server address")
	seed := flag.Int64("seed", 0, "seed for a reproducible opening state (0 = let the server choose)")
	cellSize := flag.Int("cell-size", 16, "pixel size of each tile")
	brightness := flag.Float64("brightness", 1.6, "lift dark colors so glyphs are easier to see (1 = colors exactly as the server sent them)")
	wait := flag.Duration("wait", 5*time.Second, "how long to wait for the server to come up")
	flag.Parse()

	c := httpclient.New(*url)
	// The editor's compound launch starts client and server together, so the
	// first request routinely loses the race.
	if err := c.WaitForServer(*wait); err != nil {
		fmt.Fprintln(os.Stderr, "hackgui:", err)
		fmt.Fprintln(os.Stderr, "hackgui: is the server running? try: go run ./cmd/mapd")
		os.Exit(1)
	}

	// paint decodes a frame into the paintable grid. Decoding is the client's
	// half of the contract and shares no types with the server's encoder, so
	// a frame that cannot be decoded is a protocol error, not a game error.
	paint := func(f httpclient.Frame) screen.Grid {
		grid, err := f.Decode()
		if err != nil {
			fmt.Fprintln(os.Stderr, "hackgui:", err)
			os.Exit(1)
		}
		return render.Brighten(grid, *brightness)
	}

	initial, err := c.NewGame(seed)
	if err != nil {
		fmt.Fprintln(os.Stderr, "hackgui:", err)
		os.Exit(1)
	}
	img := render.Image(paint(initial), *cellSize)

	a := app.NewWithID("com.map_api.hack")
	w := a.NewWindow(initial.Title)

	canvasImg := canvas.NewImageFromImage(img)
	canvasImg.ScaleMode = canvas.ImageScalePixels
	canvasImg.FillMode = canvas.ImageFillOriginal

	statusBar := widget.NewLabel("Last key: (none)")
	hudBar := widget.NewLabel(strings.Join(initial.HUD, "   "))
	w.SetContent(container.NewVBox(canvasImg, hudBar, statusBar))
	w.Resize(fyne.NewSize(float32(img.Bounds().Dx()), float32(img.Bounds().Dy())+70))

	// draw shows a frame the server has already produced. Unlike the
	// pre-split redraw, it never recomputes anything: what to draw was
	// decided before the frame was sent.
	draw := func(f httpclient.Frame) {
		canvasImg.Image = render.Image(paint(f), *cellSize)
		canvasImg.Refresh()
		hudBar.SetText(strings.Join(f.HUD, "   "))
	}

	// refresh asks for the current frame, for cases where the client has no
	// frame in hand (startup, or after an error).
	refresh := func() {
		f, err := c.Frame()
		if err != nil {
			fmt.Fprintln(os.Stderr, "hackgui:", err)
			return
		}
		draw(f)
	}

	// activeDialog is whichever popup (a read-only view, or an open
	// wield/quaff picker) is currently showing, if any — nil otherwise.
	// activeDialogIsPicker distinguishes the two: a read-only view dismisses
	// on any key (it's purely informational), but the picker must NOT —
	// it needs the follow-up letter key to reach dispatch and the server,
	// per the plan's investigation into Fyne's dialog focus behavior
	// (dialogs built from plain widget.Label content never call
	// Canvas.Focus, so canvas-level key callbacks keep firing normally
	// while one is shown).
	var activeDialog dialog.Dialog
	var activeDialogIsPicker bool
	// answering tracks an open "character" prompt: its answer is the rune
	// typed, case and all, so it comes from SetOnTypedRune — a key name is
	// "T" for both t and T — but never the rune of the keystroke that
	// opened it (see answerGate).
	var answering answerGate

	hideDialog := func() {
		if activeDialog != nil {
			activeDialog.Hide()
			activeDialog = nil
			activeDialogIsPicker = false
		}
	}

	showModal := func(title, body string, isPicker bool) {
		hideDialog()
		activeDialogIsPicker = isPicker
		activeDialog = dialog.NewCustomWithoutButtons(title, widget.NewLabel(body), w)
		activeDialog.Show()
	}

	// showView opens one of the server's read-only views. These cost no turn
	// — looking at your own inventory is not an action — so they never go
	// through dispatch. The client does not know which views exist beyond
	// the name it asks for, and does not compose their titles.
	showView := func(name string) {
		v, err := c.View(name)
		if err != nil {
			fmt.Fprintln(os.Stderr, "hackgui:", err)
			return
		}
		showModal(v.Title, strings.Join(v.Lines, "\n"), false)
	}

	dispatch := func(key, label string) {
		text := fmt.Sprintf("Last key: %s", label)
		resp, err := c.Command(key)
		if err != nil {
			fmt.Fprintln(os.Stderr, "hackgui:", err)
			statusBar.SetText(text)
			return
		}

		// A prompt means no turn was consumed, so the frame already on
		// screen is still correct and there is nothing to redraw. Note that
		// showModal hides any existing dialog first: a picker resolving into
		// another picker (a throw's item letter resolving into its direction
		// prompt) would otherwise orphan the first one on screen.
		answering.update(resp.Prompt != nil && resp.Prompt.Kind == httpclient.PromptCharacter, key)
		if resp.Prompt != nil {
			showModal(resp.Prompt.Title, strings.Join(resp.Prompt.Lines, "\n"), true)
			statusBar.SetText(text)
			return
		}

		hideDialog()
		if msg := strings.Join(resp.Messages, " | "); msg != "" {
			text += " — " + msg
		}
		statusBar.SetText(text)
		if resp.Frame != nil {
			draw(*resp.Frame)
		} else {
			refresh()
		}
	}

	// gameKeysHandledByTypedKey is every printable-character key that's
	// also a game command. Each one has a fyne.KeyName (KeyPeriod=".",
	// KeyComma=",", KeyA="A"), so it reaches SetOnTypedKey below — and,
	// being printable, ALSO fires SetOnTypedRune for the character it
	// types. Letting both callbacks react (even just the rune callback
	// overwriting the status text after SetOnTypedKey already dispatched
	// the real turn) either double-runs the turn or stomps its message —
	// this is the exact bug "." had before this guard existed. So every
	// key in this set must be ignored by SetOnTypedRune and handled by
	// SetOnTypedKey only. This does NOT cover the picker's dynamic
	// candidate letters (A-Z) — those are handled by unconditionally
	// skipping SetOnTypedRune entirely while a picker is open, below.
	gameKeysHandledByTypedKey := map[rune]bool{'.': true, ',': true, 'a': true, 'A': true, 'e': true, 'E': true, 'i': true, 'I': true, 'd': true, 'D': true, 'l': true, 'L': true, 't': true, 'T': true, 's': true, 'S': true, 'x': true, 'X': true}

	// modifierKeys never mean anything on their own — Shift, Control,
	// Alt/Option, and Command/Super only matter held in combination with
	// another key, which arrives as that other key's own event (with the
	// modifier reflected in ev.Physical/ev.Name's own delivery, not as a
	// separate turn). Left unfiltered, tapping one alone would reach
	// SetOnTypedKey as its own named key (e.g. "LeftShift") and dispatch
	// as an unrecognized key, silently consuming a turn.
	modifierKeys := map[fyne.KeyName]bool{
		desktop.KeyShiftLeft:    true,
		desktop.KeyShiftRight:   true,
		desktop.KeyControlLeft:  true,
		desktop.KeyControlRight: true,
		desktop.KeyAltLeft:      true,
		desktop.KeyAltRight:     true,
		desktop.KeySuperLeft:    true,
		desktop.KeySuperRight:   true,
	}

	w.Canvas().SetOnTypedRune(func(r rune) {
		if answering.open {
			if answering.answer(r) {
				dispatch(string(r), string(r))
			}
			return
		}
		// While a picker is open, every key (including its dynamic
		// candidate letters A-Z, which aren't in gameKeysHandledByTypedKey
		// since they're not fixed game commands) must reach SetOnTypedKey
		// only — otherwise this callback's fallthrough status-text branch
		// below would stomp the message dispatch just set, reproducing
		// the exact double-dispatch bug gameKeysHandledByTypedKey exists
		// to prevent, just for whichever letter resolves the picker.
		if activeDialogIsPicker {
			return
		}
		// Recognized game keys (including "I", which OPENS the inventory
		// dialog via SetOnTypedKey) must be checked before the
		// "activeDialog != nil dismisses" branch below, and not just
		// ignored alongside it — GLFW fires SetOnTypedKey before
		// SetOnTypedRune for the same keystroke, so by the time this rune
		// callback runs for "I", SetOnTypedKey has *already* opened the
		// dialog this same press; checking activeDialog first would treat
		// that as "a key arrived while a dialog is showing" and instantly
		// dismiss the dialog it just opened, in the same keystroke.
		if gameKeysHandledByTypedKey[r] {
			return
		}
		if activeDialog != nil {
			hideDialog()
			return
		}
		if r == 'q' || r == 'Q' {
			w.Close()
			return
		}
		// "<"/">" (Shift+,/Shift+.) have no fyne.KeyName of their own —
		// Fyne only reports the physical key (","/".") to SetOnTypedKey
		// regardless of Shift, so this rune callback is the only place
		// they can be told apart from plain ,/. at all. See
		// SetOnTypedKey's own CurrentKeyModifiers check below, which
		// suppresses the physical ,/. dispatch this same keystroke would
		// otherwise also fire.
		if r == '<' || r == '>' {
			dispatch(string(r), string(r))
			return
		}
		// "?" (Shift+/) has the same no-fyne.KeyName-of-its-own problem as
		// "<"/">" above, and the same fix: told apart from plain "/" only
		// here, and opened directly (not via dispatch) since viewing help
		// is read-only. See SetOnTypedKey's own CurrentKeyModifiers check
		// below, which suppresses the physical "/" dispatch this same
		// keystroke would otherwise also fire.
		if r == '?' {
			showView("help")
			return
		}
		statusBar.SetText(fmt.Sprintf("Last key: %q", r))
	})
	w.Canvas().SetOnTypedKey(func(ev *fyne.KeyEvent) {
		if activeDialog != nil && !activeDialogIsPicker {
			hideDialog()
			return
		}
		if ev.Name == fyne.KeyEscape {
			if activeDialogIsPicker {
				// Route through dispatch like any other key while a
				// picker is open, so the server's own catch-all cancels
				// the picker instead of Escape closing the whole window
				// mid-selection.
				dispatch(string(ev.Name), string(ev.Name))
				return
			}
			w.Close()
			return
		}
		if modifierKeys[ev.Name] {
			return
		}
		// A character prompt is answered by the rune (SetOnTypedRune);
		// Escape, handled above, is the only key that cancels it here.
		if answering.open {
			return
		}
		// Shift+,/Shift+./Shift+/ produce '<'/'>'/'?', which SetOnTypedRune
		// handles (see above) — without this check, the physical ,/./ key
		// would still reach the dispatch below and fire its own
		// pickup/wait/wasted-turn as an unwanted side effect of the same
		// keystroke (plain "/" is unbound, so it'd otherwise silently cost
		// a turn on every "?" press). fyne.KeyEvent itself carries no
		// modifier state, so this asks the driver directly for what's
		// currently held.
		if driver, ok := fyne.CurrentApp().Driver().(desktop.Driver); ok &&
			driver.CurrentKeyModifiers()&fyne.KeyModifierShift != 0 &&
			(ev.Name == fyne.KeyComma || ev.Name == fyne.KeyPeriod || ev.Name == fyne.KeySlash) {
			return
		}
		if ev.Name == fyne.KeyI && !activeDialogIsPicker {
			showView("inventory")
			return
		}
		if ev.Name == fyne.KeyL && !activeDialogIsPicker {
			showView("look")
			return
		}
		if ev.Name == fyne.KeyS && !activeDialogIsPicker {
			showView("status")
			return
		}
		dispatch(string(ev.Name), string(ev.Name))
	})

	// ShowAndRun blocks until the window is closed, then returns, so main
	// exits right after.
	w.ShowAndRun()
}
