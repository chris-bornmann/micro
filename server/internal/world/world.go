// Package world defines the combined "map plus everything on it" wire
// shape passed between the coordinator and object-owning services.
package world

import (
	"micro/server/internal/mapdata"
	"micro/server/internal/overlay"
)

// AugmentedMap is a generated level plus everything currently on it —
// players, monsters, treasure, whatever else comes to occupy the map.
type AugmentedMap struct {
	Map     mapdata.Map      `json:"map"`
	Objects []overlay.Object `json:"objects"`
	// OilyTiles is every floor tile currently coated in spilled oil (see
	// hack.GameState.OilyTiles) — fed in so an entity's own movement
	// logic (player.Action, goblin.Action) can decide to slide (see the
	// slide package) instead of stepping cleanly onto one. A slice
	// rather than a map keyed by mapdata.Point, since encoding/json
	// can't marshal a struct-keyed map — the same reason
	// Visibility.rememberedTiles() converts its own internal map to a
	// slice for render.Options.LitTiles. Nil/empty is safe (no oily
	// tiles), so callers that don't care about oiliness (e.g.
	// player.Throw) can simply omit it.
	OilyTiles []mapdata.Point `json:"oily_tiles,omitempty"`
}
