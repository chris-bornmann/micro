// Package overlay defines what gets drawn on top of a generated map: an
// object's position and appearance, and nothing about what it is. The
// rules engine produces these (rules.Registry.Object) and package grid
// rasterizes them; neither needs the other's types.
package overlay

// Color is plain RGB — the canonical color picked once. Renderers derive
// whatever presentation they need (terminal 256-color, true-color image,
// ...) from it.
type Color struct{ R, G, B uint8 }

// Object is one drawable object. ID is namespaced by what the object is
// (e.g. "goblin:17", "player:1"); this package does not parse it.
type Object struct {
	ID    string
	X, Y  int // tile coordinates, in the same space as the accompanying Map
	Glyph rune
	Color Color
	Layer int // higher draws on top when objects share a tile
}
