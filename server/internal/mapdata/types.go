// Package mapdata defines the map's data types and pure query/movement
// functions — a neutral library with no generation logic in it. It exists
// so that the rules engine and the coordinator never need to depend on
// mapgen's generator; they depend on this instead, exactly as mapgen
// itself does.
package mapdata

// Point is a single tile coordinate.
type Point struct {
	X, Y int
}

// Room is a rectangular room and a node in the level graph. X,Y is the
// top-left corner of its interior floor area, W,H are its interior
// width/height (in tiles).
type Room struct {
	ID   int
	X, Y int
	W, H int
	Dark bool
	// Walled means walls stand inside the room (Map.Walls): a maze, or a
	// hand-drawn hall split up. Sight and light inside it then follow
	// lines of sight (see Map.Clear) rather than filling the room.
	Walled bool
	// CorridorIDs lists every Corridor touching this room, for O(1) graph
	// traversal (index into Map.Corridors).
	CorridorIDs []int
}

func (r Room) CenterX() int { return r.X + r.W/2 }
func (r Room) CenterY() int { return r.Y + r.H/2 }

// Tiles returns every interior tile of r, in row-major order — the
// "reveal it all at once" counterpart to Map.TilesWithinRadius's bounded
// patch.
func (r Room) Tiles() []Point {
	tiles := make([]Point, 0, r.W*r.H)
	for y := r.Y; y < r.Y+r.H; y++ {
		for x := r.X; x < r.X+r.W; x++ {
			tiles = append(tiles, Point{X: x, Y: y})
		}
	}
	return tiles
}

// Door is where a corridor meets a room.
type Door struct {
	X, Y   int
	Hidden bool
}

// Corridor is an edge in the level graph, connecting RoomA to RoomB (by
// Room.ID) via a tile-by-tile Path. Path[0] is always DoorA's position and
// Path[len(Path)-1] is always DoorB's position.
type Corridor struct {
	ID           int
	RoomA, RoomB int
	DoorA, DoorB Door
	Path         []Point
}

// Map is a generated level: a graph of rooms connected by corridors.
type Map struct {
	Width, Height int
	Rooms         []Room
	Corridors     []Corridor
	// Doors is a flattened convenience copy of every corridor's DoorA/DoorB.
	Doors []Door
	Seed  int64
	// Walls are wall tiles inside rooms — what makes a maze of one big
	// room. Nothing walks onto them; nil on an ordinary level.
	Walls map[Point]bool
}
