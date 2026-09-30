// The coordinator's exported surface: one game, and the three things that
// can be asked of it.
//
// Everything else in this package is unexported, and stays that way. A
// client gets frames, turn messages, prompts and preformatted text — never a
// GameState, never a level, never an item. That is the whole point of the
// boundary, and keeping the surface this small is what enforces it.

package game

import (
	"fmt"
	"sync"

	"map_api/item"
	"map_api/mapgen"
	"map_api/player"
	"map_api/vision"
)

// Game is one playthrough: the coordinator's state plus the server-side
// switches it was started with.
//
// Every method takes mu for its whole body. That is not defensive
// programming — handleKeyMessages mutates GameState transitively
// (PlayerState, each monster's State, Visited, OilyTiles, Levels) and
// descending a staircase can call mapgen.Generate mid-request, while
// net/http serves every request on its own goroutine. Without the lock two
// overlapping keypresses corrupt the level.
type Game struct {
	mu    sync.Mutex
	state *GameState

	// reveal and full are cheat switches, set once from the server's own
	// flags. They are fields here rather than parameters on a request
	// because a client must not be able to ask for either: both exist to
	// show what the game is otherwise hiding.
	reveal bool
	full   bool

	// turn counts consumed turns. It does not advance when a command opens
	// a picker or is refused, so a client can distinguish "nothing
	// happened" from "a turn passed" without parsing messages.
	turn int
}

// CommandResponse is the result of one keypress.
//
// Exactly one of Frame and Prompt is set. A Prompt means the keypress opened
// an interaction rather than spending a turn — nothing moved, so there is no
// new frame to draw.
type CommandResponse struct {
	Messages []string `json:"messages,omitempty"`
	Frame    *Frame   `json:"frame,omitempty"`
	Prompt   *Prompt  `json:"prompt,omitempty"`
}

// PromptKind tells a client how to present a Prompt. A client may ignore it
// and just show Lines; it exists so a richer one can draw arrow-key
// affordances for a direction prompt instead of a list.
type PromptKind string

const (
	// PromptChoose expects one of the letters Lines is keyed by.
	PromptChoose PromptKind = "choose"
	// PromptDirection expects "Up", "Down", "Left" or "Right".
	PromptDirection PromptKind = "direction"
)

// Prompt is a pending interaction, fully rendered.
//
// Lines arrive already formatted ("A) short sword (wielded)"), so a client
// never learns what an item, a category or a pending action is. It shows the
// lines and sends back whatever key the player pressed; any key is
// acceptable, and one that matches neither a letter nor a direction cancels,
// exactly as it did before the split.
type Prompt struct {
	Kind  PromptKind `json:"kind"`
	Title string     `json:"title"`
	Lines []string   `json:"lines"`
}

// View is a read-only query that costs no turn. Title is included so dialog
// captions stay with the lines they caption, rather than being hardcoded
// once per client.
type View struct {
	Name  string   `json:"name"`
	Title string   `json:"title"`
	Lines []string `json:"lines"`
}

// NewGame starts a playthrough: generate level 1, create the Player, and
// reveal what they can see from where they are standing.
//
// This is main()'s former startup block, with each os.Exit(1) turned into a
// returned error — a server must answer a bad request, not die of it.
func NewGame(cfg mapgen.Config, revealHidden, showFullMap bool) (*Game, error) {
	spawnedOnce := map[string]bool{}
	ls, err := populateLevel(cfg, 1, spawnedOnce)
	if err != nil {
		return nil, err
	}

	playerState, err := initPlayer(ls.Map)
	if err != nil {
		return nil, err
	}
	p, err := playerObject(playerState)
	if err != nil {
		return nil, err
	}

	// Relocate the flask near the Player's spawn point — a level-1-only
	// startup nicety (so it's immediately findable for testing) that
	// isn't generalized to every level populateLevel builds afterward.
	for i, raw := range ls.FloorItems {
		it, err := itemInfo(raw)
		if err != nil {
			return nil, err
		}
		if it.Category != item.CategoryFlask {
			continue
		}
		if x, y, ok := tileNearPlayer(ls.Map, p.X, p.Y); ok {
			relocated, err := relocateItem(raw, x, y)
			if err != nil {
				return nil, err
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

	return &Game{state: state, reveal: revealHidden, full: showFullMap}, nil
}

// Frame returns the current screen without advancing anything — for a client
// that has just connected, or has lost track of what it was drawing.
func (g *Game) Frame() (Frame, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return buildFrame(g.state, g.turn, g.reveal, g.full)
}

// Command applies one keypress.
//
// If the key left a picker open, the response is a Prompt and nothing else:
// no turn was consumed, so the frame a client is already showing is still
// correct. Otherwise the turn resolved and the response carries its messages
// and a fresh frame.
func (g *Game) Command(key string) (CommandResponse, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	messages, err := handleKeyMessages(g.state, key)
	if err != nil {
		return CommandResponse{}, err
	}

	if prompt, err := g.pendingPrompt(); err != nil {
		return CommandResponse{}, err
	} else if prompt != nil {
		return CommandResponse{Prompt: prompt}, nil
	}

	g.turn++
	f, err := buildFrame(g.state, g.turn, g.reveal, g.full)
	if err != nil {
		return CommandResponse{}, err
	}
	return CommandResponse{Messages: messages, Frame: &f}, nil
}

// pendingPrompt renders state.PendingSelection for a client, or returns nil
// if no picker is open. Caller holds mu.
//
// This is the whole of what used to be inline in the Fyne dispatch closure:
// deciding between a letter picker and a throw's direction prompt, and
// formatting either. It belongs here because both decisions need item names
// and candidate lettering, which is to say they need the game.
func (g *Game) pendingPrompt() (*Prompt, error) {
	sel := g.state.PendingSelection
	if sel == nil {
		return nil, nil
	}

	// A throw whose item is already chosen is waiting on a direction, not a
	// letter.
	if sel.Item != nil {
		it, err := itemInfo(sel.Item.Raw)
		if err != nil {
			return nil, err
		}
		return &Prompt{
			Kind:  PromptDirection,
			Title: "Throw",
			Lines: []string{fmt.Sprintf("Throw the %s — which direction?", it.Name)},
		}, nil
	}

	lines, err := pickerLines(sel.Candidates)
	if err != nil {
		return nil, err
	}
	return &Prompt{Kind: PromptChoose, Title: "Choose one", Lines: lines}, nil
}

// viewTitles is every read-only view a client can ask for, and the caption
// to show it under. Keeping the set here rather than in each client means a
// second client cannot invent a view the server does not implement, or
// caption an existing one differently.
var viewTitles = map[string]string{
	"inventory": "Inventory",
	"look":      "Here",
	"status":    "Status",
	"help":      "Help",
}

// View answers a read-only query. It costs no turn and mutates nothing —
// looking at your own inventory is not an action.
func (g *Game) View(name string) (View, error) {
	title, ok := viewTitles[name]
	if !ok {
		return View{}, fmt.Errorf("unknown view %q", name)
	}

	g.mu.Lock()
	defer g.mu.Unlock()

	var lines []string
	var err error
	switch name {
	case "inventory":
		lines, err = inventoryLines(g.state)
	case "look":
		lines, err = lookHereLines(g.state)
	case "status":
		var fieldsJSON []byte
		if fieldsJSON, err = player.Status(g.state.PlayerState); err == nil {
			lines, err = statusLines(fieldsJSON)
		}
	case "help":
		lines = helpLines()
	}
	if err != nil {
		return View{}, err
	}

	return View{Name: name, Title: title, Lines: lines}, nil
}

// ViewNames lists every view View accepts, for a server's own error
// messages.
func ViewNames() []string {
	names := make([]string, 0, len(viewTitles))
	for n := range viewTitles {
		names = append(names, n)
	}
	return names
}
