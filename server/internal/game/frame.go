// Building a frame: everything a client needs to draw one screen, and
// nothing else.
//
// This is the whole of what leaves the server. It is a rasterized character
// grid (see package grid), not a level — so the decisions about what the
// player may know are all made here, before anything is serialized, and a
// client has nothing to be trusted with. Before the split these decisions
// lived in four closures inside main(), which is to say they lived in the
// UI.

package game

import (
	"fmt"

	"micro/server/internal/grid"
	"micro/server/internal/mapdata"
	"micro/server/internal/overlay"
	"micro/server/internal/vision"
)

// Frame is one screen's worth of state, as sent to a client.
//
// The field tags are this side's declaration of the protocol in PROTOCOL.md.
// The client declares its own, separately — the two sides share no code, so
// keeping these in step is the job of the golden test in httpapi, not of the
// compiler.
//
// Note what a Frame cannot express: there is no seed (with one, a client
// could regenerate the entire dungeon), no room or corridor identity, no
// unexplored geometry, no entity state. Only characters, colors, and text.
type Frame struct {
	// Turn counts turns actually consumed. It does not advance when a
	// command opens a picker or is refused, so a client can tell "nothing
	// happened" from "a turn passed" without reading the messages, and can
	// discard a response that arrives out of order.
	Turn int `json:"turn"`
	// Title is the window caption, composed here. It carries the seed for
	// bug reports, which is exactly why the map itself does not: a caption
	// is inert text, a seed in a structured field is a dungeon.
	Title string `json:"title"`
	Level int    `json:"level"`

	Width  int      `json:"width"`
	Height int      `json:"height"`
	Rows   []string `json:"rows"`
	Colors [][]int  `json:"colors"`
	// Palette holds the "#rrggbb" values Colors indexes into. It travels
	// with every frame so a client can restyle by substituting it, rather
	// than by learning what a wall is.
	Palette []string `json:"palette"`

	// HUD is the always-visible status strip, already formatted. The client
	// prints the strings; it never learns what an attribute is.
	HUD []string `json:"hud,omitempty"`
	// GameOver is sent even when false, unlike HUD. An absent array and an
	// empty one mean the same thing, but a client reading a boolean should
	// not have to treat "absent" and "false" as the same case — and a frame
	// whose key set is stable is one a golden test can actually pin.
	GameOver bool `json:"game_over"`
}

// visibleObjects returns every object the player can perceive right now:
// themselves, every live monster that is not invisible, and every floor
// item, filtered through fog of war.
//
// An invisible monster is drawn only with revealHidden, the cheat for
// seeing what the game hides, and then in grid.RevealColor so it is plain
// which monsters are invisible. fullMap lifts fog of war and nothing else.
//
// A Player who can see invisible sees an invisible monster like any
// other, in its own color, wherever they could see it anyway — and hidden
// things on the floor, such as undiscovered traps, the same way. Without
// it, those are drawn only with revealHidden, in grid.RevealColor.
//
// A Player with monster vision senses every live monster on the level —
// through fog and walls, invisible or not — so monsters skip both checks.
// Only the monsters: the map itself shows no more than it did.
//
// The filter is deliberately two different rules, both enforced here rather
// than anywhere a client could reach. A movable object is shown only while
// it is currently in view — if you cannot see it, you do not know whether
// it is still there. A fixed one (a staircase) is remembered once seen,
// like the shape of a room. See Visibility.filterObjects.
func visibleObjects(state *GameState, fullMap, revealHidden bool) []overlay.Object {
	reg := state.eng.Reg
	sensing := reg.Has(state.Player, "monster_vision")
	seeing := reg.Has(state.Player, "see_invisible")
	all := []sighting{{Object: reg.Object(state.Player, state.Identity)}}
	var sensed []sighting
	for _, m := range state.liveMonsters() {
		obj := reg.Object(m, state.Identity)
		if reg.Has(m, "invisible") {
			switch {
			case revealHidden:
				obj.Color = grid.RevealColor
			case !sensing && !seeing:
				continue
			}
		}
		if sensing {
			sensed = append(sensed, sighting{Object: obj})
		} else {
			all = append(all, sighting{Object: obj})
		}
	}
	for _, it := range state.FloorItems {
		obj := reg.Object(it, state.Identity)
		if !state.noticed(it) {
			if !revealHidden {
				continue // an undiscovered trap: the floor looks like floor
			}
			obj.Color = grid.RevealColor
		}
		all = append(all, sighting{Object: obj, Fixed: reg.Has(it, "fixed")})
	}

	if !fullMap {
		all = state.Visited.filterObjects(state.Map, all, currentlyVisibleTiles(state))
	}
	all = append(all, sensed...)
	out := make([]overlay.Object, len(all))
	for i, s := range all {
		out[i] = s.Object
	}
	return out
}

// currentlyVisibleTiles is the CURRENT-frame visible set, used only to
// decide which movable objects are visible right now. Distinct from
// state.Visited.rememberedTiles(), the PERMANENT floor/wall-shape memory
// used for the lit-tile pass — a movable object on a tile you remember but
// aren't currently in view of must stay hidden even though the floor there
// keeps rendering.
func currentlyVisibleTiles(state *GameState) []mapdata.Point {
	p := state.Player
	return vision.CurrentlyVisible(*state.Map, p.X, p.Y, state.eng.Reg.Has(p, "light"))
}

// buildFrame rasterizes the player's current view.
//
// revealHidden and fullMap are the server's own cheat switches, set from its
// flags. They are parameters rather than fields on a request because a
// client must not be able to ask for either: both exist to reveal what the
// game is otherwise hiding.
func buildFrame(state *GameState, turn int, revealHidden, fullMap bool) Frame {
	// The map a client sees is the filtered one unless we are deliberately
	// cheating. filterObjects consults the same filter internally, so the
	// two can never disagree about what has been explored.
	m := state.Map
	if !fullMap {
		m = state.Visited.visibleMap(state.Map)
	}

	g := grid.Build(m, grid.Options{
		RevealHidden: revealHidden,
		SeeHidden:    state.eng.Reg.Has(state.Player, "see_invisible"),
		Objects:      visibleObjects(state, fullMap, revealHidden),
		LitTiles:     state.Visited.rememberedTiles(),
	})
	rows, colors, palette := g.Encode()

	return Frame{
		Turn:     turn,
		Title:    fmt.Sprintf("Hack — level %d, seed %d", state.CurrentLevel, state.Map.Seed),
		Level:    state.CurrentLevel,
		Width:    g.Width,
		Height:   g.Height,
		Rows:     rows,
		Colors:   colors,
		Palette:  palette,
		HUD:      hudLines(state),
		GameOver: state.Player.Dead,
	}
}
