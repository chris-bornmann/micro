// Command hack is the game coordinator: it holds central game state and
// drives the other services. Today that's just generating a level and
// showing it in a window with basic keyboard input; future steps add
// monsters/treasure and a turn loop over sentient objects.
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"map_api/item"
	"map_api/mapdata"
	"map_api/mapgen"
	"map_api/overlay"
	"map_api/player"
	"map_api/render"
	"map_api/vision"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"
)

func main() {
	cfg := mapgen.DefaultConfig()

	flag.IntVar(&cfg.Width, "width", cfg.Width, "map width in tiles")
	flag.IntVar(&cfg.Height, "height", cfg.Height, "map height in tiles")
	flag.IntVar(&cfg.NumRooms, "rooms", cfg.NumRooms, "number of rooms to place")
	flag.IntVar(&cfg.MinRoomSize, "min-room", cfg.MinRoomSize, "minimum room interior size")
	flag.IntVar(&cfg.MaxRoomSize, "max-room", cfg.MaxRoomSize, "maximum room interior size")
	flag.Float64Var(&cfg.DarkRoomPercent, "dark-percent", cfg.DarkRoomPercent, "percent of rooms that are dark")
	flag.Float64Var(&cfg.HiddenDoorPercent, "hidden-door-percent", cfg.HiddenDoorPercent, "percent of doors that are hidden")
	flag.Float64Var(&cfg.ExtraCorridorPercent, "extra-corridor-percent", cfg.ExtraCorridorPercent, "extra loop corridors, as a percent of room count")
	flag.Int64Var(&cfg.Seed, "seed", cfg.Seed, "RNG seed (0 = random)")
	cellSize := flag.Int("cell-size", 16, "pixel size of each tile")
	revealHidden := flag.Bool("reveal-hidden", false, "show hidden doors distinctly instead of disguising them as walls")
	showFullMap := flag.Bool("show-full-map", false, "debug: ignore fog of war and always render the entire generated map")
	flag.Parse()

	spawnedOnce := map[string]bool{}
	ls, err := populateLevel(cfg, 1, spawnedOnce)
	if err != nil {
		fmt.Fprintln(os.Stderr, "hack:", err)
		os.Exit(1)
	}

	playerState, err := initPlayer(ls.Map)
	if err != nil {
		fmt.Fprintln(os.Stderr, "hack:", err)
		os.Exit(1)
	}
	p, err := playerObject(playerState)
	if err != nil {
		fmt.Fprintln(os.Stderr, "hack:", err)
		os.Exit(1)
	}

	// Relocate the flask near the Player's spawn point — a level-1-only
	// startup nicety (so it's immediately findable for testing) that
	// isn't generalized to every level populateLevel builds afterward.
	for i, raw := range ls.FloorItems {
		it, err := itemInfo(raw)
		if err != nil {
			fmt.Fprintln(os.Stderr, "hack:", err)
			os.Exit(1)
		}
		if it.Category != item.CategoryFlask {
			continue
		}
		if x, y, ok := tileNearPlayer(ls.Map, p.X, p.Y); ok {
			relocated, err := relocateItem(raw, x, y)
			if err != nil {
				fmt.Fprintln(os.Stderr, "hack:", err)
				os.Exit(1)
			}
			ls.FloorItems[i] = relocated
		}
		break
	}

	state := &GameState{
		PlayerState:  playerState,
		CurrentLevel: 1,
		Levels:       make([]*levelState, maxLevels),
		LevelConfig:  cfg,
		SpawnedOnce:  spawnedOnce,
		levelState:   ls,
	}
	state.Levels[0] = ls

	state.Visited.reveal(state.Map, p.X, p.Y)
	initialLitTiles, _, _ := vision.CurrentlyLit(*state.Map, p.X, p.Y, p.Lit)
	state.Visited.rememberLitTiles(initialLitTiles)

	// currentlyVisibleTiles computes vision.CurrentlyVisible fresh from the
	// Player's current position — the CURRENT-frame visible set, used only
	// to decide which movable objects (monsters/items) are visible right
	// now. Distinct from state.Visited.rememberedTiles(), the PERMANENT
	// floor/wall-shape memory used for LitTiles below — a movable object
	// on a tile you remember but aren't currently in view of must stay
	// hidden even though the floor there keeps rendering.
	currentlyVisibleTiles := func() []mapdata.Point {
		p, err := playerObject(state.PlayerState)
		if err != nil {
			fmt.Fprintln(os.Stderr, "hack:", err)
			os.Exit(1)
		}
		return vision.CurrentlyVisible(*state.Map, p.X, p.Y, p.Lit)
	}

	mapToRender := func() *mapdata.Map {
		if *showFullMap {
			return state.Map
		}
		return state.Visited.visibleMap(state.Map)
	}
	objectsToRender := func() []overlay.Object {
		p, err := playerObject(state.PlayerState)
		if err != nil {
			fmt.Fprintln(os.Stderr, "hack:", err)
			os.Exit(1)
		}
		liveMonsters, err := liveMonsterObjects(state)
		if err != nil {
			fmt.Fprintln(os.Stderr, "hack:", err)
			os.Exit(1)
		}
		all := append([]overlay.Object{p}, liveMonsters...)
		for _, raw := range state.FloorItems {
			it, err := itemInfo(raw)
			if err != nil {
				fmt.Fprintln(os.Stderr, "hack:", err)
				os.Exit(1)
			}
			all = append(all, it.Object)
		}
		if *showFullMap {
			return all
		}
		return state.Visited.filterObjects(state.Map, all, currentlyVisibleTiles())
	}
	renderOptions := func() render.Options {
		return render.Options{RevealHidden: *revealHidden, Objects: objectsToRender(), LitTiles: state.Visited.rememberedTiles()}
	}

	img := render.Image(mapToRender(), renderOptions(), *cellSize)

	a := app.NewWithID("com.map_api.hack")
	w := a.NewWindow(fmt.Sprintf("Hack — seed %d, %d rooms", state.Map.Seed, len(state.Map.Rooms)))

	canvasImg := canvas.NewImageFromImage(img)
	canvasImg.ScaleMode = canvas.ImageScalePixels
	canvasImg.FillMode = canvas.ImageFillOriginal

	status := widget.NewLabel("Last key: (none)")
	w.SetContent(container.NewVBox(canvasImg, status))
	w.Resize(fyne.NewSize(float32(img.Bounds().Dx()), float32(img.Bounds().Dy())+40))

	redraw := func() {
		canvasImg.Image = render.Image(mapToRender(), renderOptions(), *cellSize)
		canvasImg.Refresh()
	}

	// activeDialog is whichever popup (the "i" inventory view or an open
	// wield/quaff picker) is currently showing, if any — nil otherwise.
	// activeDialogIsPicker distinguishes the two: the "i" view dismisses
	// on any key (it's purely informational), but the picker must NOT —
	// it needs the follow-up letter key to reach dispatch/handleKey/
	// resolvePendingSelection, per the plan's investigation into Fyne's
	// dialog focus behavior (dialogs built from plain widget.Label
	// content never call Canvas.Focus, so canvas-level key callbacks
	// keep firing normally while one is shown).
	var activeDialog dialog.Dialog
	var activeDialogIsPicker bool

	// showInventoryDialog is invoked directly from the "I" key, not
	// through dispatch/handleKey, since viewing inventory is a read-only
	// query, not a turn-consuming action — it never touches
	// state.PlayerState and never calls finishTurn.
	showInventoryDialog := func() {
		lines, err := inventoryLines(state)
		if err != nil {
			fmt.Fprintln(os.Stderr, "hack:", err)
			return
		}
		content := widget.NewLabel(strings.Join(lines, "\n"))
		activeDialogIsPicker = false
		activeDialog = dialog.NewCustomWithoutButtons("Inventory", content, w)
		activeDialog.Show()
	}

	// showLookDialog is lookHereLines' presentation, invoked directly
	// from the "L" key exactly like showInventoryDialog is from "I" —
	// looking at the floor is a read-only query, not a turn-consuming
	// action.
	showLookDialog := func() {
		lines, err := lookHereLines(state)
		if err != nil {
			fmt.Fprintln(os.Stderr, "hack:", err)
			return
		}
		content := widget.NewLabel(strings.Join(lines, "\n"))
		activeDialogIsPicker = false
		activeDialog = dialog.NewCustomWithoutButtons("Here", content, w)
		activeDialog.Show()
	}

	// showStatusDialog shows the Player's own Status self-description
	// (HP, Attributes, active modifiers), invoked directly from the "S"
	// key exactly like showInventoryDialog/showLookDialog are from
	// "I"/"L" — a read-only query, not a turn-consuming action. hack
	// never interprets player.Status' fields itself; statusLines just
	// renders whatever label/value pairs come back.
	showStatusDialog := func() {
		fieldsJSON, err := player.Status(state.PlayerState)
		if err != nil {
			fmt.Fprintln(os.Stderr, "hack:", err)
			return
		}
		lines, err := statusLines(fieldsJSON)
		if err != nil {
			fmt.Fprintln(os.Stderr, "hack:", err)
			return
		}
		content := widget.NewLabel(strings.Join(lines, "\n"))
		activeDialogIsPicker = false
		activeDialog = dialog.NewCustomWithoutButtons("Status", content, w)
		activeDialog.Show()
	}

	// showHelpDialog shows the full command list (helpLines), invoked
	// directly from the "?" key exactly like showInventoryDialog/
	// showLookDialog/showStatusDialog are from "I"/"L"/"S" — a read-only
	// query, not a turn-consuming action. Unlike its three siblings, "?"
	// has no fyne.KeyName distinct from plain "/" (it's Shift+/), so it's
	// wired up from SetOnTypedRune below instead of SetOnTypedKey.
	showHelpDialog := func() {
		content := widget.NewLabel(strings.Join(helpLines(), "\n"))
		activeDialogIsPicker = false
		activeDialog = dialog.NewCustomWithoutButtons("Help", content, w)
		activeDialog.Show()
	}

	dispatch := func(key, label string) {
		text := fmt.Sprintf("Last key: %s", label)
		message, err := handleKey(state, key)
		if err != nil {
			fmt.Fprintln(os.Stderr, "hack:", err)
			status.SetText(text)
			return
		}
		if state.PendingSelection != nil {
			// A picker resolving into another picker (a throw's item
			// letter resolving into its direction prompt) must hide
			// whatever dialog is already showing before opening the
			// next one — otherwise the old dialog is simply orphaned:
			// reassigning activeDialog below stops this function from
			// ever being able to reach it again, but never actually
			// hides the widget still on screen.
			if activeDialog != nil {
				activeDialog.Hide()
				activeDialog = nil
				activeDialogIsPicker = false
			}
			if state.PendingSelection.Item != nil {
				it, ierr := itemInfo(state.PendingSelection.Item.Raw)
				if ierr != nil {
					fmt.Fprintln(os.Stderr, "hack:", ierr)
					return
				}
				content := widget.NewLabel(fmt.Sprintf("Throw the %s — which direction?", it.Name))
				activeDialogIsPicker = true
				activeDialog = dialog.NewCustomWithoutButtons("Throw", content, w)
				activeDialog.Show()
				status.SetText(text)
				return
			}
			lines, lerr := pickerLines(state.PendingSelection.Candidates)
			if lerr != nil {
				fmt.Fprintln(os.Stderr, "hack:", lerr)
				return
			}
			content := widget.NewLabel(strings.Join(lines, "\n"))
			activeDialogIsPicker = true
			activeDialog = dialog.NewCustomWithoutButtons("Choose one", content, w)
			activeDialog.Show()
			status.SetText(text)
			return
		}
		if activeDialog != nil {
			activeDialog.Hide()
			activeDialog = nil
			activeDialogIsPicker = false
		}
		if message != "" {
			text += " — " + message
		}
		status.SetText(text)
		redraw()
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
	// candidate letters (B-J) — those are handled by unconditionally
	// skipping SetOnTypedRune entirely while a picker is open, below.
	gameKeysHandledByTypedKey := map[rune]bool{'.': true, ',': true, 'a': true, 'A': true, 'e': true, 'E': true, 'i': true, 'I': true, 'd': true, 'D': true, 'l': true, 'L': true, 't': true, 'T': true, 's': true, 'S': true}

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
		// While a picker is open, every key (including its dynamic
		// candidate letters B-J, which aren't in gameKeysHandledByTypedKey
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
			activeDialog.Hide()
			activeDialog = nil
			activeDialogIsPicker = false
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
		// here, and called directly (not via dispatch) since viewing help
		// is read-only, like showInventoryDialog/showLookDialog/
		// showStatusDialog. See SetOnTypedKey's own CurrentKeyModifiers
		// check below, which suppresses the physical "/" dispatch this
		// same keystroke would otherwise also fire.
		if r == '?' {
			showHelpDialog()
			return
		}
		status.SetText(fmt.Sprintf("Last key: %q", r))
	})
	w.Canvas().SetOnTypedKey(func(ev *fyne.KeyEvent) {
		if activeDialog != nil && !activeDialogIsPicker {
			activeDialog.Hide()
			activeDialog = nil
			activeDialogIsPicker = false
			return
		}
		if ev.Name == fyne.KeyEscape {
			if activeDialogIsPicker {
				// Route through dispatch like any other key while a
				// picker is open, so resolvePendingSelection's catch-all
				// cancels the picker instead of Escape closing the whole
				// window mid-selection.
				dispatch(string(ev.Name), string(ev.Name))
				return
			}
			w.Close()
			return
		}
		if modifierKeys[ev.Name] {
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
			showInventoryDialog()
			return
		}
		if ev.Name == fyne.KeyL && !activeDialogIsPicker {
			showLookDialog()
			return
		}
		if ev.Name == fyne.KeyS && !activeDialogIsPicker {
			showStatusDialog()
			return
		}
		dispatch(string(ev.Name), string(ev.Name))
	})

	// ShowAndRun blocks until the window is closed, then returns, so main
	// exits right after.
	w.ShowAndRun()
}
