// The turn loop: dispatch a key, then run everything that happens
// after the player has acted.
package game

import "strings"

// handleKey is handleKeyMessages with this turn's messages joined into one
// string — what reads well at a call site, a test above all, that just
// wants the line of text a player would see.
func handleKey(state *GameState, key string) (string, error) {
	messages, err := handleKeyMessages(state, key)
	return joinMessages(messages), err
}

// handleKeyMessages processes one keypress.
//
// If a picker is open (state.PendingSelection != nil), the key answers it
// instead of being a command — see resolvePendingSelection. Otherwise, if
// the game is over (see GameState.over), it reports "Game Over" and does
// nothing else.
// Otherwise it dispatches:
//
//	,      pick up what's here (a picker if there are several things)
//	< >    take the stairs underfoot
//	A      use an item (a picker if there are several usable ones)
//	E      equip a weapon or armor (likewise)
//	D      drop something (always a picker)
//	T      throw something (a picker, then a direction)
//	X      abandon the quest (asks to confirm first)
//	arrows move, or attack whatever is in the way
//	.      wait a turn
//
// Any other key is a wasted turn, the same as bumping into a wall.
//
// A command that completes — even by being told there is nothing to pick
// up — ends in finishTurn, which gives every monster its turn. One that
// opens a picker, or that the rules refuse, does not.
//
// The only error is a level that could not be generated (see
// applyStairs).
func handleKeyMessages(state *GameState, key string) ([]string, error) {
	if state.PendingSelection != nil {
		return resolvePendingSelection(state, key), nil
	}
	if state.over() {
		return []string{"Game Over"}, nil
	}

	var messages []string
	var spent bool
	switch key {
	case ",":
		messages, spent = pickUp(state)
	case "<", ">":
		dir := "up"
		if key == ">" {
			dir = "down"
		}
		var err error
		if messages, err = applyStairs(state, dir); err != nil {
			return nil, err
		}
		spent = true
	case "A":
		messages, spent = useItem(state)
	case "E":
		messages, spent = equipItem(state)
	case "D":
		messages, spent = dropItem(state)
	case "T":
		messages, spent = throwItem(state)
	case "X":
		messages, spent = abandonQuest(state)
	default:
		var err error
		if messages, err = move(state, key); err != nil {
			return nil, err
		}
		spent = true
	}
	if !spent {
		return messages, nil
	}
	return finishTurn(state, messages), nil
}

// move steps the Player one tile in the direction key names, attacking
// the monster there instead if there is one. "." and any key that is not
// a direction leave the Player where they are. Its one error is a level
// that cannot be generated, when a trap door drops the Player into it.
func move(state *GameState, key string) ([]string, error) {
	d, ok := directions[key]
	if !ok {
		return nil, nil
	}
	p := state.Player
	c := state.ctx()
	fromX, fromY := p.X, p.Y
	res := c.Step(p, p.X+d[0], p.Y+d[1])
	if res.Target == nil {
		messages := append(c.Messages(), findHiddenDoor(state, fromX, fromY)...)
		if c.Descend && !p.Dead {
			msg, err := fallToNextLevel(state)
			if err != nil {
				return nil, err
			}
			messages = append(messages, msg)
		}
		return messages, nil
	}
	c.Melee(p, res.Target)
	fallen(state, c, res.Target)
	return c.Messages(), nil
}

// findHiddenDoor reveals a hidden door the Player has just stepped onto,
// for good: from then on it is drawn as the door it is. Stepping through
// what looked like a room's wall is a discovery, and is announced. Coming
// the other way, down the corridor, there is no surprise — a corridor can
// only end in a door — so the door is revealed without a word.
func findHiddenDoor(state *GameState, fromX, fromY int) []string {
	p := state.Player
	if !state.Map.RevealDoorAt(p.X, p.Y) {
		return nil
	}
	if _, inRoom := state.Map.RoomAt(fromX, fromY); inRoom {
		return []string{"You found a hidden door!"}
	}
	return nil
}

// finishTurn is the second half of every turn that was actually spent,
// after the messages the Player's own action produced:
//
//  1. The Player's traits tick: effects wear off, a torch burns down,
//     burning hurts.
//  2. Every live monster's traits tick the same way. Burning can kill one
//     here.
//  3. Fog of war is revealed around wherever the Player now stands.
//  4. Every monster still alive takes its turn, which may be an attack
//     on the Player.
//
// The Player ticks first so that a monster deciding whether it can see
// them this turn sees this turn's invisibility, not last turn's.
//
// The turn is counted complete only at the very end, so the action, the
// ticks and the monsters' moves all share one turn number (World.Turn).
func finishTurn(state *GameState, messages []string) []string {
	defer func() { state.Turn++ }()

	c := state.ctx()
	c.Tick(state.Player)
	messages = append(messages, c.Messages()...)

	for _, m := range state.liveMonsters() {
		c := state.ctx()
		c.Tick(m)
		messages = append(messages, c.Messages()...)
		fallen(state, c, m)
	}

	revealAroundPlayer(state)

	for _, m := range state.liveMonsters() {
		c := state.ctx()
		// The engine describes the blow, hit or miss, then whatever it
		// set off (a vampire's drain).
		c.MonsterTurn(m)
		messages = append(messages, c.Messages()...)
		for _, e := range c.Departed {
			depart(state, e)
		}
	}
	return messages
}

// resolvePendingSelection interprets key as the answer to the open picker.
// A letter matching a candidate completes that action and, if the rules
// allow it, spends the turn; any other key cancels silently, spending
// nothing — bad input is absorbed harmlessly rather than surfaced as an
// error. A throw's item choice opens a second prompt, for the direction.
func resolvePendingSelection(state *GameState, key string) []string {
	sel := state.PendingSelection
	state.PendingSelection = nil // cleared unconditionally: cancel or complete, either way it's resolved

	if sel.Action == pendingAbandon {
		return confirmAbandon(state, key)
	}
	if sel.Action != pendingUseOn && sel.Item != nil {
		resolve := resolveThrow
		if sel.Action == pendingAnswer {
			resolve = applyAnswer
		}
		messages, spent := resolve(state, sel.Item, key)
		if !spent {
			return messages
		}
		return finishTurn(state, messages)
	}

	idx := -1
	for i, l := range candidateLetters(len(sel.Candidates)) {
		if strings.EqualFold(key, l) {
			idx = i
			break
		}
	}
	if idx == -1 {
		return nil
	}
	chosen := sel.Candidates[idx]

	var messages []string
	var spent bool
	switch sel.Action {
	case pendingThrow:
		// The item is chosen, but a throw still needs a direction.
		state.PendingSelection = &pendingSelection{Action: pendingThrow, Item: chosen}
		return nil
	case pendingEquip:
		messages, spent = applyEquip(state, chosen)
	case pendingUse:
		messages, spent = applyUse(state, chosen)
	case pendingDrop:
		messages, spent = applyDrop(state, chosen)
	case pendingUseOn:
		messages, spent = applyUseOn(state, sel.Item, chosen)
	case pendingPickup:
		messages, spent = applyPickup(state, chosen)
	}
	if !spent {
		return messages
	}
	return finishTurn(state, messages)
}

// abandonQuestQuestion is what X asks before it ends the game.
const abandonQuestQuestion = "Really abandon your quest? (y/n)"

// abandonQuest asks the Player to confirm that they want to give up. It
// spends no turn: nothing has happened yet, and the answer comes next (see
// confirmAbandon).
func abandonQuest(state *GameState) ([]string, bool) {
	state.PendingSelection = &pendingSelection{Action: pendingAbandon, Question: abandonQuestQuestion}
	return nil, false
}

// confirmAbandon ends the game if key is a yes, and otherwise cancels
// silently, as any other prompt does. Either way no turn passes: giving
// up is not an action the monsters get to answer.
func confirmAbandon(state *GameState, key string) []string {
	if key != "y" && key != "Y" {
		return nil
	}
	state.Abandoned = true
	return []string{"You abandon your quest."}
}
