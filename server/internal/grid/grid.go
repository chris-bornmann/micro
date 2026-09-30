// Package grid rasterizes a level into a character grid: the exact form the
// server sends a client, and the only form it sends.
//
// This is the client/server boundary for everything visual. The server
// decides what each cell looks like; the client only paints cells. That
// split is what makes the boundary safe: a client cannot reveal what it was
// never sent, and it is never sent the level. No seed (with it, a client
// could regenerate the whole dungeon, unexplored rooms and hidden doors
// included), no room identities, no corridor graph, no unexplored geometry,
// no entity state beyond the character standing on a tile.
//
// A hidden door is the clearest case. Whether it looks like a door or like
// wall is decided here, in Build, before anything is serialized — so an
// undiscovered one leaves the server as an ordinary wall glyph in an
// ordinary wall color, indistinguishable from the wall beside it. Before
// the split this was the client's decision (render honoured a RevealHidden
// flag), which meant the secret and the instruction not to look at it were
// both shipped to whoever asked.
package grid

import (
	"fmt"
	"sort"

	"micro/server/internal/mapdata"
	"micro/server/internal/overlay"
)

// terrainDimFactor scales down every fixed/terrain glyph's color (wall,
// floor, corridor, door) relative to the "full brightness" values below,
// so static map geometry reads as visually quieter than movable objects
// (Player, monsters, items), which are drawn at whatever intensity their
// own package picked, undimmed. 1.0 would restore full brightness; 0.0
// would make terrain invisible. Tune this one constant to adjust.
const terrainDimFactor = 0.75

// dim scales c's channels by terrainDimFactor.
func dim(c overlay.Color) overlay.Color {
	scale := func(v uint8) uint8 { return uint8(float64(v) * terrainDimFactor) }
	return overlay.Color{R: scale(c.R), G: scale(c.G), B: scale(c.B)}
}

// Canonical RGB palette for each tile kind. Terrain colors are dimmed (see
// terrainDimFactor); colorHiddenDebug is a debug-only reveal aid,
// deliberately left at full intensity so it stays easy to spot.
//
// These live server-side because Build does, and Build is what turns a
// level into cells. A client is still free to substitute its own colors:
// every frame carries its palette with it (see Grid.Encode), so remapping
// is a client-side lookup, not a protocol change.
var (
	colorWall        = dim(overlay.Color{R: 0x82, G: 0x82, B: 0x82}) // legible glyph on a black background, not tuned as a solid block
	colorFloorLit    = dim(overlay.Color{R: 0xe8, G: 0xdf, B: 0xa4}) // warm pale yellow
	colorFloorDark   = dim(overlay.Color{R: 0x50, G: 0x50, B: 0x66}) // dim but not near-invisible on black
	colorCorridor    = dim(overlay.Color{R: 0x5a, G: 0x7f, B: 0xa6}) // light blue
	colorDoor        = dim(overlay.Color{R: 0xb0, G: 0x6a, B: 0x1f}) // orange/brown
	colorHiddenDebug = overlay.Color{R: 0xc9, G: 0x4f, B: 0xd6}      // magenta, debug-only reveal — full intensity
)

// Options controls how a level is rasterized.
type Options struct {
	// RevealHidden draws hidden doors distinctly (magenta 'h') instead of
	// disguising them as plain wall. A cheat/inspection switch, set from
	// the server's own flags and never from anything a client sends.
	RevealHidden bool
	// Objects are drawn on top of the map, sorted by ascending Layer.
	// Whoever builds these has already decided what the player can
	// perceive; Build draws all of them.
	Objects []overlay.Object
	// LitTiles are individual floor tiles to paint with the dark-room
	// floor color even though they don't belong to any Room in m.Rooms —
	// the remembered tiles inside a dark room, where only the tiles the
	// player has actually been next to are known rather than the room's
	// whole rectangle.
	LitTiles []mapdata.Point
}

// Cell is one character and the color to draw it in. A zero Cell (ch 0) and
// a space are both "nothing here"; a renderer draws neither.
type Cell struct {
	Ch    rune
	Color overlay.Color
}

// Grid is a fully rasterized frame: Height rows of Width cells.
type Grid struct {
	Width, Height int
	Cells         [][]Cell
}

// Build rasterizes m onto a blank Width x Height grid: every cell starts
// blank, then each room's perimeter is painted as wall, then room floors,
// then corridor paths, then doors (which must win over the corridor color
// painted beneath them), then LitTiles, then objects by ascending Layer.
//
// Unlike a real dungeon, only the ring of tiles immediately around a room's
// rectangle gets a wall glyph — the rest of the canvas (corridors included)
// stays blank, since a corridor already reads as a passage from its own
// distinct glyph/color and doesn't need a wall border to be legible.
func Build(m *mapdata.Map, opts Options) Grid {
	g := Grid{Width: m.Width, Height: m.Height}
	g.Cells = make([][]Cell, m.Height)
	for y := range g.Cells {
		g.Cells[y] = make([]Cell, m.Width)
		for x := range g.Cells[y] {
			g.Cells[y][x] = Cell{' ', overlay.Color{}} // blank: nothing here
		}
	}

	set := func(x, y int, c Cell) {
		if x < 0 || x >= m.Width || y < 0 || y >= m.Height {
			return
		}
		g.Cells[y][x] = c
	}

	for _, r := range m.Rooms {
		for y := r.Y - 1; y <= r.Y+r.H; y++ {
			for x := r.X - 1; x <= r.X+r.W; x++ {
				if x >= r.X && x < r.X+r.W && y >= r.Y && y < r.Y+r.H {
					continue // interior — the floor pass below paints this
				}
				set(x, y, Cell{'#', colorWall})
			}
		}
	}

	for _, r := range m.Rooms {
		c := Cell{'.', colorFloorLit}
		if r.Dark {
			c = Cell{'.', colorFloorDark}
		}
		for y := r.Y; y < r.Y+r.H; y++ {
			for x := r.X; x < r.X+r.W; x++ {
				set(x, y, c)
			}
		}
	}

	for _, corr := range m.Corridors {
		for _, p := range corr.Path {
			set(p.X, p.Y, Cell{'.', colorCorridor})
		}
		set(corr.DoorA.X, corr.DoorA.Y, doorCell(corr.DoorA, opts))
		set(corr.DoorB.X, corr.DoorB.Y, doorCell(corr.DoorB, opts))
	}

	for _, p := range opts.LitTiles {
		set(p.X, p.Y, Cell{'.', colorFloorDark})
	}

	objects := append([]overlay.Object(nil), opts.Objects...)
	sort.SliceStable(objects, func(i, j int) bool { return objects[i].Layer < objects[j].Layer })
	for _, o := range objects {
		set(o.X, o.Y, Cell{o.Glyph, o.Color})
	}

	return g
}

// doorCell decides what a door looks like — including whether it looks like
// a door at all. An undiscovered hidden door returns exactly the wall cell
// the perimeter pass would have drawn, so nothing downstream, on either
// side of the network, can tell the difference.
func doorCell(d mapdata.Door, opts Options) Cell {
	if d.Hidden {
		if opts.RevealHidden {
			return Cell{'h', colorHiddenDebug}
		}
		return Cell{'#', colorWall} // indistinguishable from wall to a player
	}
	return Cell{'+', colorDoor}
}

// Encode flattens g into the three parallel pieces that go over the wire.
//
// rows is the glyph layer, one string per row, so a frame is legible
// straight out of curl — you can read the dungeon in the response. colors
// holds a palette index per cell, and palette the distinct "#rrggbb"
// values those index into, deduplicated in first-appearance order. Index 0
// is always the blank cell's color, since Build fills the grid with blanks
// before painting anything.
//
// Colors are indices rather than inline hex because a 40x40 frame repeats
// two dozen distinct colors across 1600 cells, and because it gives a
// client one obvious place to restyle: substitute the palette and every
// cell follows.
func (g Grid) Encode() (rows []string, colors [][]int, palette []string) {
	index := map[overlay.Color]int{}
	rows = make([]string, g.Height)
	colors = make([][]int, g.Height)

	for y, row := range g.Cells {
		runes := make([]rune, len(row))
		idx := make([]int, len(row))
		for x, c := range row {
			ch := c.Ch
			if ch == 0 {
				ch = ' '
			}
			runes[x] = ch
			i, ok := index[c.Color]
			if !ok {
				i = len(palette)
				index[c.Color] = i
				palette = append(palette, hex(c.Color))
			}
			idx[x] = i
		}
		rows[y] = string(runes)
		colors[y] = idx
	}

	return rows, colors, palette
}

// hex formats c as "#rrggbb".
func hex(c overlay.Color) string {
	return fmt.Sprintf("#%02x%02x%02x", c.R, c.G, c.B)
}
