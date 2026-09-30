// Package render rasterizes a mapdata.Map (a graph of rooms and corridors)
// onto a blank canvas — as ANSI-colored ASCII text or as an RGBA image — for
// visually inspecting generated levels.
package render

import (
	"fmt"
	"sort"
	"strings"

	"map_api/mapdata"
	"map_api/overlay"
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

// Canonical RGB palette for each tile kind — the single source of truth
// both ANSI() (via rgbToANSI256) and Image() draw from, so the terminal
// and GUI views can never drift out of sync with each other. Terrain
// colors are dimmed (see terrainDimFactor); colorHiddenDebug is a
// debug-only reveal aid, deliberately left at full intensity so it
// stays easy to spot.
var (
	colorWall        = dim(overlay.Color{R: 0x82, G: 0x82, B: 0x82}) // legible glyph on a black background, not tuned as a solid block
	colorFloorLit    = dim(overlay.Color{R: 0xe8, G: 0xdf, B: 0xa4}) // warm pale yellow
	colorFloorDark   = dim(overlay.Color{R: 0x50, G: 0x50, B: 0x66}) // dim but not near-invisible on black
	colorCorridor    = dim(overlay.Color{R: 0x5a, G: 0x7f, B: 0xa6}) // light blue
	colorDoor        = dim(overlay.Color{R: 0xb0, G: 0x6a, B: 0x1f}) // orange/brown
	colorHiddenDebug = overlay.Color{R: 0xc9, G: 0x4f, B: 0xd6}      // magenta, debug-only reveal — full intensity
)

// Options controls how a map is rendered.
type Options struct {
	// RevealHidden shows hidden doors distinctly (magenta 'h') instead of
	// disguising them as plain wall. Useful when inspecting generation
	// output; a real game view should leave this false.
	RevealHidden bool
	// Legend appends a color/character legend below the map.
	Legend bool
	// Objects are drawn on top of the map, sorted by ascending Layer.
	Objects []overlay.Object
	// LitTiles are individual floor tiles to paint with the dark-room
	// floor color even though they don't belong to any Room in m.Rooms —
	// the currently-visible radius inside a dark room, where only a
	// handful of tiles near the viewer are lit rather than the room's
	// whole rectangle.
	LitTiles []mapdata.Point
}

type cell struct {
	ch    rune
	color overlay.Color
}

// grid rasterizes m onto a blank Width x Height canvas: every cell starts
// blank (nothing drawn — see the ' ' comment below), then each room's
// perimeter is painted as wall, then room floors, then corridor paths,
// then doors (which must win over the corridor color painted beneath
// them) are painted on top. Unlike a real dungeon, only the ring of
// tiles immediately around a room's rectangle gets a wall glyph — the
// rest of the canvas (corridors included) stays blank, since a corridor
// already reads as a passage from its own distinct glyph/color and
// doesn't need a wall border to be legible.
func grid(m *mapdata.Map, opts Options) [][]cell {
	g := make([][]cell, m.Height)
	for y := range g {
		g[y] = make([]cell, m.Width)
		for x := range g[y] {
			g[y][x] = cell{' ', overlay.Color{}} // blank: nothing here
		}
	}

	set := func(x, y int, c cell) {
		if x < 0 || x >= m.Width || y < 0 || y >= m.Height {
			return
		}
		g[y][x] = c
	}

	for _, r := range m.Rooms {
		for y := r.Y - 1; y <= r.Y+r.H; y++ {
			for x := r.X - 1; x <= r.X+r.W; x++ {
				if x >= r.X && x < r.X+r.W && y >= r.Y && y < r.Y+r.H {
					continue // interior — the floor pass below paints this
				}
				set(x, y, cell{'#', colorWall})
			}
		}
	}

	for _, r := range m.Rooms {
		c := cell{'.', colorFloorLit}
		if r.Dark {
			c = cell{'.', colorFloorDark}
		}
		for y := r.Y; y < r.Y+r.H; y++ {
			for x := r.X; x < r.X+r.W; x++ {
				set(x, y, c)
			}
		}
	}

	for _, corr := range m.Corridors {
		for _, p := range corr.Path {
			set(p.X, p.Y, cell{'.', colorCorridor})
		}
		set(corr.DoorA.X, corr.DoorA.Y, doorCell(corr.DoorA, opts))
		set(corr.DoorB.X, corr.DoorB.Y, doorCell(corr.DoorB, opts))
	}

	for _, p := range opts.LitTiles {
		set(p.X, p.Y, cell{'.', colorFloorDark})
	}

	objects := append([]overlay.Object(nil), opts.Objects...)
	sort.SliceStable(objects, func(i, j int) bool { return objects[i].Layer < objects[j].Layer })
	for _, o := range objects {
		set(o.X, o.Y, cell{o.Glyph, o.Color})
	}

	return g
}

// rgbToANSI256 maps an RGB color to the nearest xterm 256-color palette
// index, using the standard 6x6x6 color cube (indices 16-231).
func rgbToANSI256(c overlay.Color) int {
	to6 := func(v uint8) int { return int(v) * 5 / 255 }
	r, g, b := to6(c.R), to6(c.G), to6(c.B)
	return 16 + 36*r + 6*g + b
}

func doorCell(d mapdata.Door, opts Options) cell {
	if d.Hidden {
		if opts.RevealHidden {
			return cell{'h', colorHiddenDebug}
		}
		return cell{'#', colorWall} // indistinguishable from wall to a player
	}
	return cell{'+', colorDoor}
}

// ANSI renders m to a string using 8-bit (256-color) ANSI escape codes.
func ANSI(m *mapdata.Map, opts Options) string {
	g := grid(m, opts)

	var b strings.Builder
	for _, row := range g {
		for _, c := range row {
			fmt.Fprintf(&b, "\x1b[38;5;%dm%c\x1b[0m", rgbToANSI256(c.color), c.ch)
		}
		b.WriteByte('\n')
	}

	fmt.Fprintf(&b, "\nSeed: %d | Rooms: %d | Corridors: %d | Doors: %d | Objects: %d\n",
		m.Seed, len(m.Rooms), len(m.Corridors), len(m.Doors), len(opts.Objects))

	if opts.Legend {
		b.WriteString(legend(opts))
	}

	return b.String()
}

func legend(opts Options) string {
	var b strings.Builder
	b.WriteString("\nLegend:\n")
	fmt.Fprintf(&b, "  \x1b[38;5;%dm#\x1b[0m wall\n", rgbToANSI256(colorWall))
	fmt.Fprintf(&b, "  \x1b[38;5;%dm.\x1b[0m lit room floor\n", rgbToANSI256(colorFloorLit))
	fmt.Fprintf(&b, "  \x1b[38;5;%dm.\x1b[0m dark room floor\n", rgbToANSI256(colorFloorDark))
	fmt.Fprintf(&b, "  \x1b[38;5;%dm.\x1b[0m corridor\n", rgbToANSI256(colorCorridor))
	fmt.Fprintf(&b, "  \x1b[38;5;%dm+\x1b[0m door\n", rgbToANSI256(colorDoor))
	if opts.RevealHidden {
		fmt.Fprintf(&b, "  \x1b[38;5;%dmh\x1b[0m hidden door (debug reveal)\n", rgbToANSI256(colorHiddenDebug))
	} else {
		fmt.Fprintf(&b, "  \x1b[38;5;%dm#\x1b[0m hidden door (indistinguishable from wall)\n", rgbToANSI256(colorWall))
	}
	return b.String()
}
