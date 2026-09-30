// Read-only views: what the player is carrying, what is on the floor
// here, their own condition, the command list. None of these mutate
// state or cost a turn.
package game

import (
	"encoding/json"
	"fmt"

	"micromap/server/internal/player"
	"micromap/server/internal/status"
)

// pickerLines formats candidates as "A) name" lines using
// candidateLetters — appending " (wielded)"/" (lit)"/" (worn)" for the
// currently-wielded item, currently-lit torch, or a currently-worn item,
// if present — shared by the "i" view (inventoryLines) and every open
// wield/quaff/drop/wear picker, so a player never sees two different
// letter-to-item mappings for what looks like the same kind of list.
func pickerLines(candidates []carriedItem) ([]string, error) {
	letters := candidateLetters(len(candidates))
	lines := make([]string, len(candidates))
	for i, ci := range candidates {
		it, err := itemInfo(ci.Raw)
		if err != nil {
			return nil, err
		}
		name := it.Name
		if ci.IsWielded {
			name += " (wielded)"
		}
		if ci.IsLit {
			name += " (lit)"
		}
		if ci.IsWorn {
			name += " (worn)"
		}
		lines[i] = fmt.Sprintf("%s) %s", letters[i], name)
	}
	return lines, nil
}

// carriedItems returns everything the Player is carrying: the
// currently-wielded item first (if any), then the currently-lit torch
// (if any), then every currently-worn item, then Inventory in order.
// Shared by inventoryLines (the read-only "i" view) and dropItem (the
// drop picker), so the two never disagree about what's carried.
func carriedItems(state *GameState) ([]carriedItem, error) {
	var items []carriedItem
	wielded, hasWielded, err := player.WieldedItem(state.PlayerState)
	if err != nil {
		return nil, err
	}
	if hasWielded {
		items = append(items, carriedItem{Raw: wielded, IsWielded: true})
	}
	litTorch, hasLitTorch, err := player.LitTorchItem(state.PlayerState)
	if err != nil {
		return nil, err
	}
	if hasLitTorch {
		items = append(items, carriedItem{Raw: litTorch, IsLit: true})
	}
	worn, err := player.WornItems(state.PlayerState)
	if err != nil {
		return nil, err
	}
	for _, raw := range worn {
		items = append(items, carriedItem{Raw: raw, IsWorn: true})
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

// inventoryLines returns one display line per carried item (wielded item
// first, then Inventory) for the "i" key's read-only view. Unlike
// pickUp/useItem/equipItem/dropItem, this never mutates
// state.PlayerState and is never routed through finishTurn — looking at
// your own inventory doesn't cost a turn.
func inventoryLines(state *GameState) ([]string, error) {
	items, err := carriedItems(state)
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return []string{"You are carrying nothing."}, nil
	}
	return pickerLines(items)
}

// lookHereLines returns one display line per item at the Player's
// current tile — the "L" key's read-only view, following
// inventoryLines' exact shape (reusing pickerLines/candidateLetters so
// the lettering a player sees here matches the picker they'd get from
// "," if they then pick one up). Never mutates state.PlayerState, never
// routed through finishTurn or handleKey — like "i", looking at the
// floor costs no turn.
func lookHereLines(state *GameState) ([]string, error) {
	playerObj, err := playerObject(state.PlayerState)
	if err != nil {
		return nil, err
	}
	floorItems, err := floorItemsAt(state, playerObj.X, playerObj.Y)
	if err != nil {
		return nil, err
	}
	if len(floorItems) == 0 {
		return []string{"There is nothing here."}, nil
	}
	return pickerLines(wrapRaw(floorItems))
}

// statusLines turns a Status call's raw JSON (a []status.Field) into
// display lines — generic across every entity kind, since hack never
// needs to know what any individual field means, only how to show a
// label/value pair. Used by the "S" key's read-only view.
func statusLines(fieldsJSON []byte) ([]string, error) {
	var fields []status.Field
	if err := json.Unmarshal(fieldsJSON, &fields); err != nil {
		return nil, fmt.Errorf("parse status: %w", err)
	}
	lines := make([]string, len(fields))
	for i, f := range fields {
		lines[i] = fmt.Sprintf("%s: %s", f.Label, f.Value)
	}
	return lines, nil
}

// hudLines is the Player's own status as the always-visible strip on the
// game screen, as opposed to statusLines' modal "S" view of the same data.
// Same source, same formatting, no turn cost — the difference is only where
// a client chooses to put it.
//
// It exists because a roguelike player needs HP on screen permanently, and
// because sending it pre-formatted keeps the client from having to know what
// any of these fields mean.
func hudLines(state *GameState) ([]string, error) {
	fieldsJSON, err := player.Status(state.PlayerState)
	if err != nil {
		return nil, fmt.Errorf("player status: %w", err)
	}
	return statusLines(fieldsJSON)
}

// helpLines lists every player-visible command with a short description,
// for the "?" key's read-only view — kept in sync with handleKey's switch
// (below), player.Action's key cases, and the SetOnTypedKey/SetOnTypedRune
// callbacks by hand, since none of those are enumerable at runtime.
func helpLines() []string {
	return []string{
		"Arrow keys — Move (attacks if the destination is occupied)",
		".         — Wait a turn in place",
		",         — Pick up item(s) here",
		"<  /  >   — Go up / down stairs",
		"A         — Use an item (potion, scroll, light a torch, …)",
		"E         — Equip a weapon or piece of armor",
		"D         — Drop an item",
		"T         — Throw an item, then choose a direction",
		"I         — Show inventory",
		"L         — Look at what's on the floor here",
		"S         — Show status (HP, attributes, active effects)",
		"?         — Show this help",
		"Escape    — Cancel a picker, or close the window",
		"Q         — Quit",
	}
}
