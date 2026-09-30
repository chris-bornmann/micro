// Decoding the generic subset out of a service's opaque reply, and the
// envelope types those replies arrive in. The coordinator stores each
// service's own state verbatim and looks only at the fields every
// service agrees on; this is the whole of that narrow view.
package main

import (
	"encoding/json"
	"fmt"

	"map_api/action"
	"map_api/item"
	"map_api/overlay"
)

// playerObject decodes just the generic overlay.Object subset out of the
// Player's raw state — used only where hack needs a plain drawable/
// positioned object (rendering, building the world snapshot fed to other
// services).
func playerObject(state []byte) (overlay.Object, error) {
	var p overlay.Object
	if err := json.Unmarshal(state, &p); err != nil {
		return overlay.Object{}, fmt.Errorf("parse player: %w", err)
	}
	return p, nil
}

// itemInfo decodes just the generic item.Item subset (overlay.Object plus
// Category/Name/Usage) out of a raw item state — used wherever hack needs
// an item's position, category, or display name without understanding any
// category-specific fields (heal amount, ...).
func itemInfo(raw json.RawMessage) (item.Item, error) {
	var it item.Item
	if err := json.Unmarshal(raw, &it); err != nil {
		return item.Item{}, fmt.Errorf("parse item: %w", err)
	}
	return it, nil
}

// candidateLetters returns exactly n letters, "A".."J" for n in
// [0,item.InventoryCapacity] — so a full inventory's worth of
// same-category candidates never runs out of letters. Shared by
// inventoryLines (the "i" view) and the wield/quaff picker so the two
// never assign different letters to the same item.
func candidateLetters(n int) []string {
	letters := make([]string, n)
	for i := 0; i < n; i++ {
		letters[i] = string(rune('A' + i))
	}
	return letters
}

// wrapRaw wraps plain item JSON as
// never-wielded carriedItems, for building a pendingSelection from
// quaffCandidates/equipCandidates (which only ever return Inventory
// items, never the wielded one).
func wrapRaw(raws []json.RawMessage) []carriedItem {
	items := make([]carriedItem, len(raws))
	for i, raw := range raws {
		items[i] = carriedItem{Raw: raw}
	}
	return items
}

// actionEnvelope mirrors the {"self": ..., "result": ...} shape both
// player.Action and goblin.Action return. Self is kept as raw bytes rather
// than decoded — hack stores it verbatim and only ever decodes the generic
// subset (via playerObject/goblinObject) when it actually needs one, never
// assuming anything about an entity's own private fields.
type actionEnvelope struct {
	Self   json.RawMessage `json:"self"`
	Result action.Result   `json:"result"`
}

// defendEnvelope mirrors the {"self": ..., "outcome": ...} shape both
// player.Defend and goblin.Defend return. Self is kept as raw bytes for
// the same reason actionEnvelope's is.
type defendEnvelope struct {
	Self    json.RawMessage      `json:"self"`
	Outcome action.DefendOutcome `json:"outcome"`
}

// effectEnvelope mirrors the {"self": ..., "outcome": ...} shape
// player.ApplyEffect returns, the same way defendEnvelope mirrors
// Defend's.
type effectEnvelope struct {
	Self    json.RawMessage    `json:"self"`
	Outcome item.EffectOutcome `json:"outcome"`
}

// dropEnvelope mirrors the {"self": ..., "item": ..., "was_wielded":
// ..., "was_lit_torch": ...} shape player.Drop returns.
type dropEnvelope struct {
	Self        json.RawMessage `json:"self"`
	Item        json.RawMessage `json:"item"`
	WasWielded  bool            `json:"was_wielded"`
	WasWorn     bool            `json:"was_worn"`
	WasLitTorch bool            `json:"was_lit_torch"`
}

// tickEnvelope mirrors the {"self": ..., "outcome": ...} shape
// player.Tick returns, the same way effectEnvelope mirrors ApplyEffect's.
type tickEnvelope struct {
	Self    json.RawMessage  `json:"self"`
	Outcome item.TickOutcome `json:"outcome"`
}
