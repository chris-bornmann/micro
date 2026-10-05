// Items: picking them up, using them, equipping them, dropping them, and
// the candidate lists each of those offers when more than one carried
// item would match.
//
// Every command here returns what to tell the player and whether a turn
// was spent. A turn is not spent when a picker opens (the follow-up key
// finishes the job) or when the rules refused the action outright.
package game

import (
	"math/rand"
	"unicode/utf8"

	"micro/server/internal/mapdata"
	"micro/server/internal/rules"
)

// resultOf turns an engine result into a command's return values.
func resultOf(res rules.Result) ([]string, bool) {
	return messagesOf(res.Msg), !res.Refused
}

// pickupCandidates is every item on the Player's tile that could be picked
// up: a fixed item (stairs) is shown by "look" but never offered here.
func pickupCandidates(state *GameState) []*rules.Entity {
	var out []*rules.Entity
	for _, it := range state.floorItemsAt(state.Player.X, state.Player.Y) {
		if !state.eng.Reg.Has(it, "fixed") {
			out = append(out, it)
		}
	}
	return out
}

// pickUp picks up the one item on the Player's tile, or opens a picker if
// there are several.
func pickUp(state *GameState) ([]string, bool) {
	cands := pickupCandidates(state)
	switch len(cands) {
	case 0:
		return messagesOf("There is nothing here to pick up."), true
	case 1:
		return applyPickup(state, cands[0])
	default:
		state.PendingSelection = &pendingSelection{Action: pendingPickup, Candidates: cands}
		return nil, false
	}
}

// applyPickup moves item from the floor into the Player's inventory. If
// the inventory is full, the item stays where it is.
func applyPickup(state *GameState, item *rules.Entity) ([]string, bool) {
	res, taken := state.ctx().PickUp(state.Player, item)
	if taken {
		state.removeFloorItem(item)
	}
	return resultOf(res)
}

// useCandidates is every carried item that does something when used, in
// inventory order.
func useCandidates(state *GameState) []*rules.Entity {
	var out []*rules.Entity
	for _, it := range state.Player.Inventory {
		if rules.Usable(it) {
			out = append(out, it)
		}
	}
	return out
}

// useItem uses the one usable item the Player carries, or opens a picker
// if there are several.
func useItem(state *GameState) ([]string, bool) {
	cands := useCandidates(state)
	switch len(cands) {
	case 0:
		return messagesOf("You have nothing to use."), true
	case 1:
		return applyUse(state, cands[0])
	default:
		state.PendingSelection = &pendingSelection{Action: pendingUse, Candidates: cands}
		return nil, false
	}
}

// applyUse uses item — unless using it asks something first, in which
// case it waits for the answer: a typed character (a scroll of summon
// monster: which monster?) or one of the Player's other items, picked by
// letter (a scroll of knowledge: which item?).
func applyUse(state *GameState, item *rules.Entity) ([]string, bool) {
	q, ok := state.ctx().Question(item)
	switch {
	case !ok:
		return resultOf(state.ctx().Use(state.Player, item))
	case q.Kind == rules.AskItem:
		var others []*rules.Entity
		for _, it := range state.Player.Carried() {
			if it != item {
				others = append(others, it)
			}
		}
		if len(others) == 0 {
			return messagesOf("You have nothing else to use it on."), false
		}
		state.PendingSelection = &pendingSelection{Action: pendingUseOn, Item: item, Candidates: others, Question: q.Text}
	default:
		state.PendingSelection = &pendingSelection{Action: pendingAnswer, Item: item, Question: q.Text}
	}
	return nil, false
}

// applyAnswer finishes using item with the character typed in answer to
// its question. Any key that is not a single character — Escape above
// all — cancels, and the item is kept.
func applyAnswer(state *GameState, item *rules.Entity, key string) ([]string, bool) {
	if utf8.RuneCountInString(key) != 1 {
		return nil, false
	}
	return resultOf(state.ctx().UseWith(state.Player, item, rules.Answer{Char: key}))
}

// applyUseOn finishes using item on target, the item picked in answer to
// its question.
func applyUseOn(state *GameState, item, target *rules.Entity) ([]string, bool) {
	return resultOf(state.ctx().UseWith(state.Player, item, rules.Answer{Item: target}))
}

// equipCandidates is every carried item that has a slot to go in, in
// inventory order — weapons and armor together, in one list.
func equipCandidates(state *GameState) []*rules.Entity {
	var out []*rules.Entity
	for _, it := range state.Player.Inventory {
		if it.T.Slot != "" {
			out = append(out, it)
		}
	}
	return out
}

// equipItem equips the one equippable item the Player carries, or opens a
// picker if there are several.
func equipItem(state *GameState) ([]string, bool) {
	cands := equipCandidates(state)
	switch len(cands) {
	case 0:
		return messagesOf("You have nothing to equip."), true
	case 1:
		return applyEquip(state, cands[0])
	default:
		state.PendingSelection = &pendingSelection{Action: pendingEquip, Candidates: cands}
		return nil, false
	}
}

func applyEquip(state *GameState, item *rules.Entity) ([]string, bool) {
	return resultOf(state.ctx().Equip(state.Player, item))
}

// dropItem always opens the picker when anything is carried — unlike use
// and equip, there is no acting on a sole candidate, since drop is meant
// to show everything carried every time.
func dropItem(state *GameState) ([]string, bool) {
	items := state.Player.Carried()
	if len(items) == 0 {
		return messagesOf("You have nothing to drop."), true
	}
	state.PendingSelection = &pendingSelection{Action: pendingDrop, Candidates: items}
	return nil, false
}

// applyDrop drops item at the Player's feet, unless it refuses to leave
// them or does not survive the fall.
func applyDrop(state *GameState, item *rules.Entity) ([]string, bool) {
	res, dropped := state.ctx().Drop(state.Player, item)
	if dropped != nil {
		state.FloorItems = append(state.FloorItems, dropped)
	}
	return resultOf(res)
}

// tileNearPlayer returns a random walkable tile in the same room as
// (playerX, playerY), excluding the Player's own tile — used to place the
// starting flask of oil somewhere immediately reachable. ok is false if
// the Player isn't in a room or the room is 1x1.
//
// rng is passed in rather than taken from the global source because this
// is a spawn-time placement, and spawn-time placement is seeded so that a
// given seed reproduces a given opening position.
func tileNearPlayer(m *mapdata.Map, playerX, playerY int, rng *rand.Rand) (x, y int, ok bool) {
	room, ok := m.RoomAt(playerX, playerY)
	if !ok {
		return 0, 0, false
	}
	var candidates []mapdata.Point
	for dx := 0; dx < room.W; dx++ {
		for dy := 0; dy < room.H; dy++ {
			cx, cy := room.X+dx, room.Y+dy
			if cx == playerX && cy == playerY || m.IsWall(cx, cy) {
				continue
			}
			candidates = append(candidates, mapdata.Point{X: cx, Y: cy})
		}
	}
	if len(candidates) == 0 {
		return 0, 0, false
	}
	chosen := candidates[rng.Intn(len(candidates))]
	return chosen.X, chosen.Y, true
}
