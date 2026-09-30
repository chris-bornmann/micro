package mapdata

// RoomAt returns the room containing (x,y), if any.
func (m Map) RoomAt(x, y int) (Room, bool) {
	for _, r := range m.Rooms {
		if x >= r.X && x < r.X+r.W && y >= r.Y && y < r.Y+r.H {
			return r, true
		}
	}
	return Room{}, false
}

// RoomByID returns the room with the given ID, if any.
func (m Map) RoomByID(id int) (Room, bool) {
	for _, r := range m.Rooms {
		if r.ID == id {
			return r, true
		}
	}
	return Room{}, false
}

// CorridorAt returns the corridor whose Path contains (x,y), if any.
func (m Map) CorridorAt(x, y int) (Corridor, bool) {
	for _, c := range m.Corridors {
		for _, p := range c.Path {
			if p.X == x && p.Y == y {
				return c, true
			}
		}
	}
	return Corridor{}, false
}

// TilesWithinRadius returns every tile whose Chebyshev distance from
// (x,y) is at most radius — a (2*radius+1)x(2*radius+1) square including
// (x,y) itself. radius=1 gives the 8 Moore-neighbor tiles plus (x,y). No
// bounds/walkability/room filtering is applied — callers filter further
// as needed, the same minimal-primitive style as the rest of this file.
func (m Map) TilesWithinRadius(x, y, radius int) []Point {
	var tiles []Point
	for dy := -radius; dy <= radius; dy++ {
		for dx := -radius; dx <= radius; dx++ {
			tiles = append(tiles, Point{X: x + dx, Y: y + dy})
		}
	}
	return tiles
}

// WalkableTiles returns every walkable tile on the map: every room's
// interior (via Room.Tiles) plus every corridor's full path (which
// already includes its own door endpoints). Unfiltered by occupancy —
// callers exclude currently-occupied tiles themselves, the same "pure
// geometry, caller filters further" style as TilesWithinRadius.
func (m Map) WalkableTiles() []Point {
	var tiles []Point
	for _, r := range m.Rooms {
		tiles = append(tiles, r.Tiles()...)
	}
	for _, c := range m.Corridors {
		tiles = append(tiles, c.Path...)
	}
	return tiles
}

// RoomsAt returns every room that (x,y) counts as being "in" for
// visibility purposes: the room whose rectangle contains it (if any), plus
// any room a door at (x,y) opens into. An unlocked door is just an open
// threshold — standing on it counts as being in the room on either side of
// it, not blocked by it — so two positions "see" each other whenever their
// RoomsAt sets intersect. (Once locked doors exist, a locked door should
// stop counting here until unlocked.)
func (m Map) RoomsAt(x, y int) map[int]bool {
	rooms := map[int]bool{}
	if r, ok := m.RoomAt(x, y); ok {
		rooms[r.ID] = true
	}
	for _, c := range m.Corridors {
		if c.DoorA.X == x && c.DoorA.Y == y {
			rooms[c.RoomA] = true
		}
		if c.DoorB.X == x && c.DoorB.Y == y {
			rooms[c.RoomB] = true
		}
	}
	return rooms
}
