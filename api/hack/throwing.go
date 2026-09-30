// Throwing: choosing an item, choosing a direction, and resolving what
// the thrown thing hits.
package main

import (
	"encoding/json"
	"fmt"

	"map_api/action"
	"map_api/item"
	"map_api/mapdata"
	"map_api/overlay"
	"map_api/player"
	"map_api/world"
)

// throwCandidates returns every plain Inventory entry, plus the
// currently-lit torch if any — the set throwItem chooses among. Unlike
// equipCandidates/quaffCandidates, Inventory entries are included
// regardless of Category ("anything in the inventory" can be thrown).
// Wielded/Worn items still aren't throwable (they'd need to be
// un-equipped first), but the lit torch is a deliberate exception —
// throwing your only light source at something oily is how you set it
// ablaze (see resolveThrow's ci.IsLit case).
func throwCandidates(state *GameState) ([]carriedItem, error) {
	var items []carriedItem
	litTorch, hasLitTorch, err := player.LitTorchItem(state.PlayerState)
	if err != nil {
		return nil, err
	}
	if hasLitTorch {
		items = append(items, carriedItem{Raw: litTorch, IsLit: true})
	}
	inv, err := player.Inventory(state.PlayerState)
	if err != nil {
		return nil, err
	}
	for _, raw := range inv {
		items = append(items, carriedItem{Raw: raw})
	}
	return items, nil
}

// throwItem starts a throw: with exactly one candidate, it's chosen
// automatically and hack moves straight to waiting on a direction
// keypress; with 2+, a letter picker opens first (mirroring equipItem's
// shape), and picking a letter then ALSO moves to waiting on a
// direction — see resolvePendingSelection. Unlike equipItem/useItem,
// there's never an "auto-act, turn spent immediately" case: a throw
// always needs a direction, so done is false whenever there's at least
// one candidate. Returns "You have nothing to throw." (done=true, no
// turn) if nothing is carried that can be thrown.
func throwItem(state *GameState) (string, bool, error) {
	items, err := throwCandidates(state)
	if err != nil {
		return "", true, err
	}
	if len(items) == 0 {
		return "You have nothing to throw.", true, nil
	}
	if len(items) == 1 {
		state.PendingSelection = &pendingSelection{Action: pendingThrow, Item: &items[0]}
		return "", false, nil
	}
	state.PendingSelection = &pendingSelection{Action: pendingThrow, Candidates: items}
	return "", false, nil
}

// resolveThrow completes a throw once both its item (ci) and a direction
// (key) are known: builds the same world.AugmentedMap the default melee
// attack path builds, calls player.Throw, places the thrown item on the
// floor at its resting position (Result.X,Y — whether that's a wall it
// bounced off, an agent it hit, or the far end of its own throw.Distance)
// via the same relocateItem helper applyDrop already uses, and — if it
// hit a monster — routes through the shared attackMonster helper exactly
// like a melee attack would. key is checked against the four direction
// names itself, rather than letting player.Throw's own error surface,
// so an unrelated keypress cancels the throw silently — the same "any
// other key cancels" convention resolvePendingSelection's letter-picker
// path already follows.
func resolveThrow(state *GameState, ci carriedItem, key string) ([]string, error) {
	switch key {
	case "Up", "Down", "Left", "Right":
	default:
		return nil, nil // cancelled: not a direction, matches the picker-cancel convention
	}

	it, err := itemInfo(ci.Raw)
	if err != nil {
		return nil, err
	}

	playerObj, err := playerObject(state.PlayerState)
	if err != nil {
		return nil, err
	}
	liveMonsters, err := liveMonsterObjects(state)
	if err != nil {
		return nil, err
	}
	augObjects := append([]overlay.Object{playerObj}, liveMonsters...)
	aug := world.AugmentedMap{Map: *state.Map, Objects: augObjects}
	augJSON, err := json.Marshal(aug)
	if err != nil {
		return nil, fmt.Errorf("marshal augmented map: %w", err)
	}

	respJSON, err := player.Throw(state.PlayerState, augJSON, it.ID, key)
	if err != nil {
		return nil, fmt.Errorf("player throw: %w", err)
	}
	var tresp actionEnvelope
	if err := json.Unmarshal(respJSON, &tresp); err != nil {
		return nil, fmt.Errorf("parse player throw response: %w", err)
	}
	state.PlayerState = tresp.Self

	messages := []string{fmt.Sprintf("You throw the %s.", it.Name)}
	switch {
	case ci.IsLit:
		// Mirrors player.Drop's own "goes out and falls apart" handling
		// for a lit torch — thrown or dropped, it never survives as a
		// floor item.
		messages = append(messages, fmt.Sprintf("The %s goes out and falls apart.", it.Name))
	case it.Category == item.CategoryFlask:
		messages = append(messages, resolveFlaskThrow(state, tresp.Result))
	case it.Fragile:
		messages = append(messages, "It shatters!")
	default:
		relocated, err := relocateItem(ci.Raw, tresp.Result.X, tresp.Result.Y)
		if err != nil {
			return nil, err
		}
		state.FloorItems = append(state.FloorItems, relocated)
	}
	if tresp.Result.Action == "attack" {
		if idx, ok := monsterIndexAt(state, tresp.Result.X, tresp.Result.Y); ok {
			attackJSON, err := json.Marshal(tresp.Result)
			if err != nil {
				return nil, fmt.Errorf("marshal thrown attack: %w", err)
			}
			msgs, err := attackMonster(state, idx, attackJSON)
			if err != nil {
				return nil, err
			}
			messages = append(messages, msgs...)
		}
	}

	return finishTurn(state, messages)
}

// resolveFlaskThrow returns the status message for a thrown flask of
// oil's non-item-surviving impact, and — only for the "landed on open
// ground" case — marks that tile oily in state.OilyTiles. The
// agent-hit case's actual Oiliness increment already happens via Defend
// (action.Result.AddsOiliness, applied by goblin.Defend from the
// attackGoblin call right after this returns); this only supplies the
// message.
func resolveFlaskThrow(state *GameState, result action.Result) string {
	switch {
	case result.Action == "attack":
		return "It shatters, coating them in oil!"
	case result.BlockedByWall:
		return "It shatters against the wall!"
	default:
		if state.OilyTiles == nil {
			state.OilyTiles = map[mapdata.Point]bool{}
		}
		state.OilyTiles[mapdata.Point{X: result.X, Y: result.Y}] = true
		return "It shatters, spreading oil across the floor!"
	}
}
