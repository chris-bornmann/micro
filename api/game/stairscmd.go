// Taking a staircase. The level transition it triggers lives in
// levels.go; this is only the command half.
package game

import (
	"fmt"

	"map_api/item"
	"map_api/overlay"
	"map_api/stairs"
)

// applyStairs is the "<"/">" key's handler: if the Player is standing on
// a stairs item of the matching kind, applyStairsTransition (see
// levels.go) moves them to the level it leads to. Returns
// "There are no stairs %s here." without consuming a special action
// (still finishes a turn, matching pickUp's "There is nothing here"
// precedent) if nothing matches.
func applyStairs(state *GameState, playerObj overlay.Object, kind stairs.Kind, direction string) (string, error) {
	cands, err := floorItemsAt(state, playerObj.X, playerObj.Y)
	if err != nil {
		return "", err
	}
	for _, raw := range cands {
		it, err := itemInfo(raw)
		if err != nil {
			return "", err
		}
		if it.Category != item.CategoryStairs {
			continue
		}
		gotKind, err := stairs.KindOf(raw)
		if err != nil {
			return "", err
		}
		if gotKind != kind {
			continue
		}
		return applyStairsTransition(state, kind)
	}
	return fmt.Sprintf("There are no stairs %s here.", direction), nil
}
