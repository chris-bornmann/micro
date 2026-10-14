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
	"math/rand"
	"strings"
	"sync"

	"micro/server/internal/mapgen"
	"micro/server/internal/rules"
)

// Game is one playthrough: the coordinator's state plus the server-side
// switches it was started with.
//
// Every method takes mu for its whole body. That is not defensive
// programming — handleKeyMessages mutates GameState transitively
// (the Player, every monster, Visited, Oily, Levels) and
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
	// PromptCharacter expects any single character, sent exactly as typed
	// — case matters: "t" and "T" are different monsters.
	PromptCharacter PromptKind = "character"
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

// cheatModeHP is the Player's starting (and maximum) HP in cheat mode.
const cheatModeHP = 1000

// NewGame starts a playthrough: generate level 1, create the Player, and
// reveal what they can see from where they are standing. revealHidden,
// showFullMap and cheatMode are the server's cheat switches; cheatMode
// starts the Player on cheatModeHP.
func NewGame(cfg mapgen.Config, revealHidden, showFullMap, cheatMode bool) (*Game, error) {
	eng, err := engine()
	if err != nil {
		return nil, err
	}
	spawnedOnce := map[string]bool{}
	special := assignSpecial(eng.Cat.Levels, cfg.Seed)
	ls, err := makeLevel(eng, cfg, 1, spawnedOnce, special[1])
	if err != nil {
		return nil, err
	}

	// The Player materializes somewhere in the room with the stairs up —
	// the way into the dungeon — just as on every later level they arrive
	// on them. The tile and their stats come from the level's own seed, so
	// a given seed reproduces the same start.
	up := stairsAmong(ls.FloorItems, "up")
	if up == nil {
		return nil, fmt.Errorf("level 1 has no stairs up to start the Player by")
	}
	room, ok := ls.Map.RoomAt(up.X, up.Y)
	if !ok {
		return nil, fmt.Errorf("level 1's stairs up at (%d,%d) are not in a room", up.X, up.Y)
	}
	// Never on a monster or a trap: redraw from the same stream until the
	// tile is free (a room with no free tile would be a broken level).
	playerTemplates := eng.Cat.ByCategory(rules.CategoryPlayer)
	rng := rand.New(rand.NewSource(ls.Map.Seed))
	w := &rules.World{Map: ls.Map, Monsters: ls.Monsters}
	x, y := room.X+rng.Intn(room.W), room.Y+rng.Intn(room.H)
	for tries := 0; w.CreatureAt(x, y) != nil || trapAt(ls.FloorItems, x, y) || ls.Map.IsWall(x, y); tries++ {
		if tries == room.W*room.H*10 {
			return nil, fmt.Errorf("level 1's starting room has no free tile")
		}
		x, y = room.X+rng.Intn(room.W), room.Y+rng.Intn(room.H)
	}
	if room.Walled {
		// A maze is one room: anywhere in it would skip the maze. Start on
		// the way in.
		x, y = up.X, up.Y
	}
	// Create, not New: a hand-crafted player starts with the equipment and
	// pack its template lists. It rolls HP and attributes first, from the
	// same stream, so a given seed's player is what it always was.
	p := eng.Create(playerTemplates[0], x, y, rng, nil)
	if cheatMode {
		// MaxHP too, so healing tops back up to it rather than capping
		// at the rolled value.
		p.HP, p.MaxHP = cheatModeHP, cheatModeHP
	}

	// Move the flask of oil next to the Player — a level-1-only startup
	// nicety, so it is immediately findable. Seeded from the map's seed
	// like every other spawn-time placement; the offset keeps this stream
	// from correlating with the others derived the same way.
	relocateRNG := rand.New(rand.NewSource(ls.Map.Seed + 37))
	for _, it := range ls.FloorItems {
		if it.T.Category != rules.CategoryFlask {
			continue
		}
		if x, y, ok := tileNearPlayer(ls.Map, p.X, p.Y, relocateRNG); ok {
			it.X, it.Y = x, y
		}
		break
	}

	// Which look goes with which item type, dealt afresh for every game —
	// seeded from the map like everything else, so a -seed reproduces it.
	// The Player knows their own starting kit.
	identity := rules.NewIdentity(eng.Cat, rand.New(rand.NewSource(ls.Map.Seed+41)))
	for _, it := range p.Carried() {
		identity.Identify(it.T.ID)
	}

	state := &GameState{
		Special:      special,
		Identity:     identity,
		eng:          eng,
		Player:       p,
		CurrentLevel: 1,
		Levels:       make([]*levelState, maxLevels),
		LevelConfig:  cfg,
		SpawnedOnce:  spawnedOnce,
		levelState:   ls,
	}
	state.Levels[0] = ls
	revealAroundPlayer(state)

	return &Game{state: state, reveal: revealHidden, full: showFullMap}, nil
}

// Frame returns the current screen without advancing anything — for a client
// that has just connected, or has lost track of what it was drawing.
func (g *Game) Frame() (Frame, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return buildFrame(g.state, g.state.Turn, g.reveal, g.full), nil
}

// Over is whether the game has ended, by death or by abandoning the quest.
// The server asks so it can let a player start their next game.
func (g *Game) Over() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.state.over()
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

	if prompt := g.pendingPrompt(); prompt != nil {
		return CommandResponse{Prompt: prompt}, nil
	}

	f := buildFrame(g.state, g.state.Turn, g.reveal, g.full)
	return CommandResponse{Messages: messages, Frame: &f}, nil
}

// pendingPrompt renders state.PendingSelection for a client, or returns nil
// if no picker is open. Caller holds mu.
//
// This is the whole of what used to be inline in the Fyne dispatch closure:
// deciding between a letter picker and a throw's direction prompt, and
// formatting either. It belongs here because both decisions need item names
// and candidate lettering, which is to say they need the game.
func (g *Game) pendingPrompt() *Prompt {
	sel := g.state.PendingSelection
	if sel == nil {
		return nil
	}

	// An item already chosen is waiting on the answer to its question, or —
	// for a throw — a direction, not a letter.
	if sel.Action == pendingUseOn {
		return &Prompt{Kind: PromptChoose, Title: sel.Question, Lines: pickerLines(g.state, sel.Candidates)}
	}
	if sel.Action == pendingAbandon {
		return &Prompt{Kind: PromptCharacter, Title: "Abandon the Quest", Lines: []string{sel.Question}}
	}
	if sel.Action == pendingAnswer {
		return &Prompt{Kind: PromptCharacter, Title: capitalized(g.state.Identity.Name(sel.Item)), Lines: []string{sel.Question}}
	}
	if sel.Item != nil {
		return &Prompt{
			Kind:  PromptDirection,
			Title: "Throw",
			Lines: []string{fmt.Sprintf("Throw the %s — which direction?", g.state.Identity.Name(sel.Item))},
		}
	}
	return &Prompt{Kind: PromptChoose, Title: "Choose one", Lines: pickerLines(g.state, sel.Candidates)}
}

// capitalized is s with its first letter upper-cased.
func capitalized(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
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
	switch name {
	case "inventory":
		lines = inventoryLines(g.state)
	case "look":
		lines = lookHereLines(g.state)
	case "status":
		lines = hudLines(g.state)
	case "help":
		lines = helpLines()
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
