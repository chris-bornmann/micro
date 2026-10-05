// Read-only views: what the player is carrying, what is on the floor
// here, their own condition, the command list. None of these mutate
// state or cost a turn.
package game

import (
	"fmt"

	"micro/server/internal/rules"
	"micro/server/internal/status"
)

// candidateLetters returns exactly n letters, "A".."J" for n up to the
// inventory capacity. Shared by the inventory view and every picker so
// the two never assign different letters to the same item.
func candidateLetters(n int) []string {
	letters := make([]string, n)
	for i := 0; i < n; i++ {
		letters[i] = string(rune('A' + i))
	}
	return letters
}

// pickerLines formats items as "A) name" lines, marking whatever the
// Player has equipped: " (wielded)" in the main hand, " (held)" in the
// other — either with ", lit" for a burning torch — " (on right hand)" for
// a ring, or " (worn)". Shared by the inventory view, the look view and every picker,
// so a player never sees two different letter-to-item mappings for what
// looks like the same list.
func pickerLines(state *GameState, items []*rules.Entity) []string {
	player := state.Player
	letters := candidateLetters(len(items))
	lines := make([]string, len(items))
	for i, it := range items {
		name := state.Identity.Name(it) // its look, if not yet identified
		if slot, ok := player.SlotOf(it); ok {
			lit := ""
			if player.Lit() == it {
				lit = ", lit"
			}
			switch slot {
			case rules.SlotWeapon:
				name += " (wielded" + lit + ")"
			case rules.SlotOffHand:
				name += " (held" + lit + ")"
			case rules.SlotRightHand, rules.SlotLeftHand:
				name += " (on " + rules.HandName(slot) + ")"
			default:
				name += " (worn)"
			}
		}
		lines[i] = fmt.Sprintf("%s) %s", letters[i], name)
	}
	return lines
}

// inventoryLines lists everything the Player carries, equipped items
// first — the "i" view.
func inventoryLines(state *GameState) []string {
	items := state.Player.Carried()
	if len(items) == 0 {
		return []string{"You are carrying nothing."}
	}
	return pickerLines(state, items)
}

// lookHereLines lists every item on the Player's tile, stairs included —
// the "L" view, lettered the same way the pickup picker would be.
func lookHereLines(state *GameState) []string {
	var items []*rules.Entity
	for _, it := range state.floorItemsAt(state.Player.X, state.Player.Y) {
		if state.noticed(it) {
			items = append(items, it)
		}
	}
	if len(items) == 0 {
		return []string{"There is nothing here."}
	}
	return pickerLines(state, items)
}

// statusLines formats label/value pairs as "Label: value" lines.
func statusLines(fields []status.Field) []string {
	lines := make([]string, len(fields))
	for i, f := range fields {
		lines[i] = fmt.Sprintf("%s: %s", f.Label, f.Value)
	}
	return lines
}

// hudLines is the Player's own status as the always-visible strip on the
// game screen, as opposed to the modal "S" view of the same data. Sending
// it pre-formatted keeps the client from having to know what any of these
// fields mean.
func hudLines(state *GameState) []string {
	return statusLines(state.eng.Reg.Status(state.Player))
}

// helpLines lists every player-visible command with a short description,
// for the "?" view — kept in sync with handleKeyMessages by hand, since
// neither is enumerable at runtime.
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
