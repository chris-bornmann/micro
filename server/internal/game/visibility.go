package game

import (
	"micro/server/internal/mapdata"
	"micro/server/internal/overlay"
)

// offMap is used as a sentinel door position to suppress rendering a door
// that shouldn't be visible yet — render's paint helpers silently skip any
// coordinate outside the map bounds.
var offMap = mapdata.Door{X: -1, Y: -1}

// Visibility tracks which rooms and corridors the Player has visited. Once
// visited, a room or corridor stays visible even after the Player leaves
// it — nothing is ever un-revealed. A Dark room's own interior is the one
// exception to that room-level granularity: it's tracked per-tile instead
// (LitTiles), since only the specific tiles the Player was actually once
// adjacent to should be remembered — not its whole rectangle, the way a
// lit room's shape is remembered wholesale the instant it's entered.
type Visibility struct {
	Rooms     map[int]bool
	Corridors map[int]bool
	// LitTiles is every tile, inside any Dark room, that has ever been
	// part of vision.CurrentlyLit's result — permanent floor/wall-shape
	// memory (see rememberLitTiles), deliberately separate from "is an
	// object on this tile visible right now," which stays a per-turn
	// question (see filterObjects's own currentlyVisible parameter).
	LitTiles map[mapdata.Point]bool
}

func newVisibility() Visibility {
	return Visibility{Rooms: map[int]bool{}, Corridors: map[int]bool{}, LitTiles: map[mapdata.Point]bool{}}
}

// rememberLitTiles permanently records tiles as having been lit at least
// once — the dark-room analog of reveal's per-room bookkeeping, but at
// individual-tile granularity. Call once per turn (and once at spawn)
// with vision.CurrentlyLit's current tiles; a no-op call with nil/empty
// tiles (the common case, outside a dark room) costs nothing. Idempotent,
// like reveal.
func (v Visibility) rememberLitTiles(tiles []mapdata.Point) {
	for _, p := range tiles {
		v.LitTiles[p] = true
	}
}

// rememberedTiles returns every tile ever passed to rememberLitTiles, as
// a slice suitable for render.Options.LitTiles.
func (v Visibility) rememberedTiles() []mapdata.Point {
	tiles := make([]mapdata.Point, 0, len(v.LitTiles))
	for p := range v.LitTiles {
		tiles = append(tiles, p)
	}
	return tiles
}

// reveal marks whatever room or corridor contains (x,y) as visited. Standing
// exactly on a door also reveals the room that door opens into, even before
// stepping fully inside it — the threshold is enough, matching how a door's
// own room already made that same door visible from the other side (see
// visibleMap). Checks every corridor at (x,y), not just the first match,
// since two corridors can coincidentally share a door position.
func (v Visibility) reveal(m *mapdata.Map, x, y int) {
	if r, ok := m.RoomAt(x, y); ok {
		v.Rooms[r.ID] = true
		return
	}
	for _, c := range m.Corridors {
		onPath := false
		for _, p := range c.Path {
			if p.X == x && p.Y == y {
				onPath = true
				break
			}
		}
		if !onPath {
			continue
		}
		v.Corridors[c.ID] = true
		if c.DoorA.X == x && c.DoorA.Y == y {
			v.Rooms[c.RoomA] = true
		}
		if c.DoorB.X == x && c.DoorB.Y == y {
			v.Rooms[c.RoomB] = true
		}
	}
}

// visibleMap returns a filtered copy of m containing only what's been
// visited: full rooms, full corridors, and — for a corridor not yet fully
// visited — a synthetic version showing only the door(s) whose owning room
// has been visited, so a doorway is visible from its room before the
// corridor beyond it has been explored.
//
// A Dark room is never included here, full stop, regardless of whether
// the Player is currently standing in it or has ever visited it — its
// interior is only ever shown via the individual tiles in v.LitTiles
// (rendered separately, see render.Options.LitTiles), never as a whole
// rectangle. reveal still marks it visited in v.Rooms (useful for any
// future minimap/quest tracking, and consulted directly by the corridor
// synthesis below), only this render-time filter hides its interior.
// Corridor synthesis still consults v.Rooms directly, not this exclusion,
// so a doorway into a dark room still renders normally — darkness only
// ever hides a room's own interior, never the corridors/doors around it.
func (v Visibility) visibleMap(m *mapdata.Map) *mapdata.Map {
	out := &mapdata.Map{Width: m.Width, Height: m.Height, Seed: m.Seed}

	for _, r := range m.Rooms {
		if r.Dark {
			continue
		}
		if v.Rooms[r.ID] {
			out.Rooms = append(out.Rooms, r)
		}
	}

	for _, c := range m.Corridors {
		if v.Corridors[c.ID] {
			out.Corridors = append(out.Corridors, c)
			continue
		}

		aVisible, bVisible := v.Rooms[c.RoomA], v.Rooms[c.RoomB]
		if !aVisible && !bVisible {
			continue
		}

		synth := mapdata.Corridor{ID: c.ID, RoomA: c.RoomA, RoomB: c.RoomB, DoorA: offMap, DoorB: offMap}
		if aVisible {
			synth.DoorA = c.DoorA
			synth.Path = append(synth.Path, mapdata.Point{X: c.DoorA.X, Y: c.DoorA.Y})
		}
		if bVisible {
			synth.DoorB = c.DoorB
			synth.Path = append(synth.Path, mapdata.Point{X: c.DoorB.X, Y: c.DoorB.Y})
		}
		out.Corridors = append(out.Corridors, synth)
	}

	return out
}

// filterObjects keeps only the objects that should actually be drawn this
// turn, and treats a movable object very differently from a Fixed one
// (overlay.Object.Fixed).
//
// A movable object — every item and intelligent entity today — is shown
// only while it's somewhere currentlyVisible: if you can't see it right
// now, you have no idea whether it's still there, so there's no memory
// fallback at all. currentlyVisible is exactly vision.CurrentlyVisible's
// current-frame result (not v.LitTiles' permanent memory), which is what
// lets the Player's own object (and anything else standing right next to
// them in a dark room) keep rendering even though a Dark room is always
// excluded from the visited map below.
//
// A Fixed object (e.g. a future staircase) behaves like part of the
// map's own geometry instead: once its tile has ever been seen, it's
// remembered forever, exactly like a room or corridor's shape — via
// v.LitTiles for a Dark room's interior (excluded wholesale from the
// visited map) or vis.RoomAt/CorridorAt for everything else.
func (v Visibility) filterObjects(m *mapdata.Map, objects []overlay.Object, currentlyVisible []mapdata.Point) []overlay.Object {
	vis := v.visibleMap(m)
	var out []overlay.Object
	for _, o := range objects {
		if !o.Fixed {
			if containsPoint(currentlyVisible, o.X, o.Y) {
				out = append(out, o)
			}
			continue
		}
		if v.LitTiles[mapdata.Point{X: o.X, Y: o.Y}] {
			out = append(out, o)
			continue
		}
		if _, ok := vis.RoomAt(o.X, o.Y); ok {
			out = append(out, o)
			continue
		}
		if _, ok := vis.CorridorAt(o.X, o.Y); ok {
			out = append(out, o)
		}
	}
	return out
}

// containsPoint reports whether (x,y) is one of tiles.
func containsPoint(tiles []mapdata.Point, x, y int) bool {
	for _, p := range tiles {
		if p.X == x && p.Y == y {
			return true
		}
	}
	return false
}
