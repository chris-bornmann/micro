// Package vision will eventually be its own standalone Vision/Lighting
// microservice: a pure function of map data and a viewer's position, no
// entity-private state, called one-way from hack exactly like
// action.RollDamage or item's helpers.
package vision

import "micromap/server/internal/mapdata"

// DarkRadius is how far a viewer can see with no light source. A future
// torch changes only this number (or takes it as a parameter) — nothing
// about CurrentlyLit's contract or its callers.
const DarkRadius = 1

// CurrentlyLit reports the tiles actually visible to a viewer standing
// at (x,y) THIS instant, accounting for darkness — distinct from
// permanent fog-of-war "ever explored" memory, which stays the
// coordinator's own concern (hack's Visibility, untouched by this
// package). ok is false whenever nothing restricts vision at (x,y) — a
// lit room, a corridor, or nowhere recognized — meaning the caller's
// normal fog-of-war rules apply unchanged. When ok is true, tiles is
// exactly the visible set: with torchLit false, (x,y) itself plus every
// tile within DarkRadius that also belongs to the same room (filtered
// via m.RoomAt so a neighbor tile that's actually a wall, a door, or a
// corridor never gets reported as a lit floor tile — darkness is a
// room-interior-only concept; corridors/doors are unaffected by it and
// keep working through the existing, separate fog-of-war path). With
// torchLit true, tiles is every tile of the room instead (see
// mapdata.Room.Tiles) — a lit torch lifts the radius restriction
// entirely for as long as it burns.
func CurrentlyLit(m mapdata.Map, x, y int, torchLit bool) (tiles []mapdata.Point, room mapdata.Room, ok bool) {
	r, found := m.RoomAt(x, y)
	if !found || !r.Dark {
		return nil, mapdata.Room{}, false
	}
	if torchLit {
		return r.Tiles(), r, true
	}
	for _, p := range m.TilesWithinRadius(x, y, DarkRadius) {
		if pr, ok := m.RoomAt(p.X, p.Y); ok && pr.ID == r.ID {
			tiles = append(tiles, p)
		}
	}
	return tiles, r, true
}

// CurrentlyVisible returns every tile currently visible to a viewer
// standing at (x,y) this instant — the set a movable object must be
// standing on to be shown at all (see hack.Visibility.filterObjects), as
// opposed to a Fixed object's permanent ever-visited memory. For a Dark
// room this defers entirely to CurrentlyLit (radius-limited, or the
// whole room if torchLit). Otherwise it's every room (x,y) counts as
// being "in" via mapdata.Map.RoomsAt — its own room if inside one, plus
// whichever room(s) a door at (x,y) opens into, so a room is visible
// from its threshold before fully stepping inside it, exactly like
// Visibility.reveal and goblin's own detection logic already treat a
// threshold — each room's whole interior (Room.Tiles), skipping any Dark
// room (never revealed wholesale, even from its own threshold) — plus
// the corridor's whole Path if (x,y) is on one. Falls back to just
// (x,y) itself if none of that recognizes the position at all.
func CurrentlyVisible(m mapdata.Map, x, y int, torchLit bool) []mapdata.Point {
	if tiles, _, ok := CurrentlyLit(m, x, y, torchLit); ok {
		return tiles
	}
	var tiles []mapdata.Point
	for roomID := range m.RoomsAt(x, y) {
		r, ok := m.RoomByID(roomID)
		if !ok || r.Dark {
			continue
		}
		tiles = append(tiles, r.Tiles()...)
	}
	if c, ok := m.CorridorAt(x, y); ok {
		tiles = append(tiles, c.Path...)
	}
	if len(tiles) == 0 {
		tiles = []mapdata.Point{{X: x, Y: y}}
	}
	return tiles
}
