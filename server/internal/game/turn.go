// The turn loop: dispatch a key, then run everything that happens
// after the player has acted.
package game

import (
	"encoding/json"
	"fmt"

	"micromap/server/internal/overlay"
	"micromap/server/internal/player"
	"micromap/server/internal/stairs"
	"micromap/server/internal/vision"
	"micromap/server/internal/world"
)

// handleKey is handleKeyMessages with this turn's messages joined into one
// string, the shape the game used before a turn's text had to travel over a
// network.
//
// It stays because it is what reads well at a call site that just wants to
// show the player a line of text, and because every one of hack's tests
// drives the game through it — keeping it meant pluralizing the turn
// pipeline without editing a single test, which is the only real evidence
// that the pluralization changed nothing.
func handleKey(state *GameState, key string) (string, error) {
	messages, err := handleKeyMessages(state, key)
	if err != nil {
		return "", err
	}
	return joinMessages(messages), nil
}

// handleKeyMessages processes one keypress. If a equip/quaff/drop/pickup picker
// is currently open (state.PendingSelection != nil), the key is
// interpreted as that picker's follow-up letter instead of a normal game
// command — see resolvePendingSelection. Otherwise, if the Player is
// already dead, it reports "Game Over" and does nothing else. Otherwise
// it dispatches on key: "," picks up whatever's on the Player's own tile
// (immediately if there's exactly one item there, via a picker if 2+ —
// see pickUp), "A" uses a one-shot item from the Player's inventory
// (immediately if exactly one is carried, via a picker if 2+ are — see
// useItem), "E" (equipItem) wields a weapon or wears armor — one command
// over both categories together, so a picker with 2+ candidates can mix
// weapons and armor in the same list — "D" (dropItem) always opens a
// picker over everything carried (wielded/worn items included) whenever
// 1+ items are carried — never an immediate auto-drop, even with just one
// item — and every other key goes through the Player service's Action
// exactly as before (a move or an attack, resolved against the Goblin via
// Defend if it was an attack). Every branch that
// actually completes an action (not one that merely opened a picker)
// ends by calling finishTurn, which is always the second half of a turn:
// reveal fog, then give the Goblin its turn. Neither player nor goblin
// nor potion ever calls back into hack or into each other — hack calls
// them, feeds them JSON, and stores whatever JSON they return.
func handleKeyMessages(state *GameState, key string) ([]string, error) {
	if state.PendingSelection != nil {
		return resolvePendingSelection(state, key)
	}

	playerObj, err := playerObject(state.PlayerState)
	if err != nil {
		return nil, err
	}
	if playerObj.Dead {
		return []string{"Game Over"}, nil
	}

	switch key {
	case ",":
		msg, done, err := pickUp(state, playerObj)
		if err != nil {
			return nil, err
		}
		if !done {
			return nil, nil
		}
		return finishTurn(state, messagesOf(msg))
	case "<":
		msg, err := applyStairs(state, playerObj, stairs.KindUp, "up")
		if err != nil {
			return nil, err
		}
		return finishTurn(state, messagesOf(msg))
	case ">":
		msg, err := applyStairs(state, playerObj, stairs.KindDown, "down")
		if err != nil {
			return nil, err
		}
		return finishTurn(state, messagesOf(msg))
	case "A":
		msg, done, err := useItem(state)
		if err != nil {
			return nil, err
		}
		if !done {
			return nil, nil
		}
		return finishTurn(state, messagesOf(msg))
	case "E":
		msg, done, err := equipItem(state)
		if err != nil {
			return nil, err
		}
		if !done {
			return nil, nil
		}
		return finishTurn(state, messagesOf(msg))
	case "D":
		msg, done, err := dropItem(state)
		if err != nil {
			return nil, err
		}
		if !done {
			return nil, nil
		}
		return finishTurn(state, messagesOf(msg))
	case "T":
		msg, done, err := throwItem(state)
		if err != nil {
			return nil, err
		}
		if !done {
			return nil, nil
		}
		return finishTurn(state, messagesOf(msg))
	default:
		// A dead monster is excluded here (mirroring objectsToRender's
		// own filter) so it no longer blocks its own tile: occupantAt
		// would otherwise treat any move onto it as an attack on a
		// corpse forever, and the Player could never step onto/through
		// it.
		liveMonsters, err := liveMonsterObjects(state)
		if err != nil {
			return nil, err
		}
		augObjects := append([]overlay.Object{playerObj}, liveMonsters...)
		aug := world.AugmentedMap{Map: *state.Map, Objects: augObjects, OilyTiles: state.oilyTilesSlice()}
		augJSON, err := json.Marshal(aug)
		if err != nil {
			return nil, fmt.Errorf("marshal augmented map: %w", err)
		}
		respJSON, err := player.Action(state.PlayerState, augJSON, key)
		if err != nil {
			return nil, fmt.Errorf("player action: %w", err)
		}
		var presp actionEnvelope
		if err := json.Unmarshal(respJSON, &presp); err != nil {
			return nil, fmt.Errorf("parse player response: %w", err)
		}
		state.PlayerState = presp.Self

		var messages []string
		if presp.Result.Action == "attack" {
			if idx, ok := monsterIndexAt(state, presp.Result.X, presp.Result.Y); ok {
				attackJSON, err := json.Marshal(presp.Result)
				if err != nil {
					return nil, fmt.Errorf("marshal player attack: %w", err)
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
}

// finishTurn advances the Player's sustained effects by one turn (Tick),
// reveals fog around the Player's current position, then gives the
// Goblin its turn (if still alive) exactly as the second half of every
// turn always has, and returns the combined status message: whatever's
// already in messages (a player-attack outcome, or a pickup/use
// confirmation) followed by anything Tick reports (an effect wearing
// off) and whatever the Goblin's turn adds. Tick runs first so the
// playerObj fed to goblin.Action this same turn always carries this
// turn's up-to-date Invisible state, not last turn's.
func finishTurn(state *GameState, messages []string) ([]string, error) {
	// A turn is now definitively being spent; see GameState.Turn.
	state.Turn++

	tickJSON, err := player.Tick(state.PlayerState)
	if err != nil {
		return nil, fmt.Errorf("player tick: %w", err)
	}
	var tresp tickEnvelope
	if err := json.Unmarshal(tickJSON, &tresp); err != nil {
		return nil, fmt.Errorf("parse player tick response: %w", err)
	}
	state.PlayerState = tresp.Self
	if msg := tickMessage(tresp.Outcome); msg != "" {
		messages = append(messages, msg)
	}

	// Every live monster's own Tick (burn damage etc.) first — it never
	// needs world context (aug/playerObj), so this runs before playerObj
	// is even computed, mirroring the single-goblin ordering this
	// generalizes from exactly.
	for i := range state.Monsters {
		ms := &state.Monsters[i]
		obj, err := monsterObject(ms.State)
		if err != nil {
			return nil, err
		}
		if obj.Dead {
			continue
		}
		svc, ok := monsterServices[ms.Kind]
		if !ok {
			return nil, fmt.Errorf("hack: no monster service registered for kind %q", ms.Kind)
		}
		tickJSON, err := svc.Tick(ms.State)
		if err != nil {
			return nil, fmt.Errorf("%s tick: %w", ms.Kind, err)
		}
		var tresp tickEnvelope
		if err := json.Unmarshal(tickJSON, &tresp); err != nil {
			return nil, fmt.Errorf("parse %s tick response: %w", ms.Kind, err)
		}
		ms.State = tresp.Self
		if msg := monsterBurnMessage(svc.DisplayName, tresp.Outcome); msg != "" {
			messages = append(messages, msg)
		}
		if err := finalizeMonsterDeathIfAny(state, i); err != nil {
			return nil, err
		}
	}

	playerObj, err := playerObject(state.PlayerState)
	if err != nil {
		return nil, err
	}
	state.Visited.reveal(state.Map, playerObj.X, playerObj.Y)
	litTiles, _, _ := vision.CurrentlyLit(*state.Map, playerObj.X, playerObj.Y, playerObj.Lit)
	state.Visited.rememberLitTiles(litTiles)

	// Every still-alive monster (a burn tick above may have just killed
	// one) takes its own turn — movement/chase/attack — in Monsters
	// order. Each only ever sees the Player in its own aug.Objects;
	// monsters don't perceive or interact with each other.
	for i := range state.Monsters {
		ms := &state.Monsters[i]
		obj, err := monsterObject(ms.State)
		if err != nil {
			return nil, err
		}
		if obj.Dead {
			continue
		}
		svc := monsterServices[ms.Kind]
		aug := world.AugmentedMap{Map: *state.Map, Objects: []overlay.Object{playerObj}, OilyTiles: state.oilyTilesSlice()}
		augJSON, err := json.Marshal(aug)
		if err != nil {
			return nil, fmt.Errorf("marshal augmented map: %w", err)
		}
		aRespJSON, err := svc.Action(ms.State, augJSON)
		if err != nil {
			return nil, fmt.Errorf("%s action: %w", ms.Kind, err)
		}
		var aresp actionEnvelope
		if err := json.Unmarshal(aRespJSON, &aresp); err != nil {
			return nil, fmt.Errorf("parse %s response: %w", ms.Kind, err)
		}
		ms.State = aresp.Self

		if aresp.Result.Action == "attack" {
			attackJSON, err := json.Marshal(aresp.Result)
			if err != nil {
				return nil, fmt.Errorf("marshal %s attack: %w", ms.Kind, err)
			}
			dRespJSON, err := player.Defend(state.PlayerState, attackJSON)
			if err != nil {
				return nil, fmt.Errorf("player defend: %w", err)
			}
			var dresp defendEnvelope
			if err := json.Unmarshal(dRespJSON, &dresp); err != nil {
				return nil, fmt.Errorf("parse player defend response: %w", err)
			}
			state.PlayerState = dresp.Self
			messages = append(messages, playerDefendMessage(dresp.Outcome))
		}
	}
	return messages, nil
}

// floorItemsAt returns every state.FloorItems entry sitting at (x,y), in
// FloorItems order. Shared by pickUp (finding pickup candidates at the
// Player's tile) and lookHereLines (listing everything at the Player's
// tile), so the two never disagree about what's there.
func floorItemsAt(state *GameState, x, y int) ([]json.RawMessage, error) {
	var out []json.RawMessage
	for _, raw := range state.FloorItems {
		it, err := itemInfo(raw)
		if err != nil {
			return nil, err
		}
		if it.X == x && it.Y == y {
			out = append(out, raw)
		}
	}
	return out, nil
}

// pickupCandidatesAt is floorItemsAt filtered to items that can actually
// be picked up — a Fixed item (e.g. stairs) stays on the floor forever
// and is never a pickup candidate, even though it still shows up in
// floorItemsAt's other callers like lookHereLines (looking at what's on
// your own tile shouldn't hide the stairs, just make them unpickupable).
func pickupCandidatesAt(state *GameState, x, y int) ([]json.RawMessage, error) {
	all, err := floorItemsAt(state, x, y)
	if err != nil {
		return nil, err
	}
	var out []json.RawMessage
	for _, raw := range all {
		it, err := itemInfo(raw)
		if err != nil {
			return nil, err
		}
		if !it.Fixed {
			out = append(out, raw)
		}
	}
	return out, nil
}

// resolvePendingSelection interprets key as the follow-up letter for an
// open wield/quaff/drop/pickup picker (state.PendingSelection). A key
// matching one of candidateLetters(len(Candidates)) completes that action
// against the chosen candidate via applyWield/applyQuaff/applyDrop/
// applyPickup and proceeds through finishTurn exactly once — the turn is
// spent on completing the action, not on having opened the picker. Any
// other key — out of range, or unrelated — cancels the pending selection
// silently (no message, no turn spent), matching this codebase's
// existing convention that bad input is absorbed harmlessly (an
// unrecognized move key already falls through to a no-op) rather than
// surfaced as an error.
func resolvePendingSelection(state *GameState, key string) ([]string, error) {
	sel := state.PendingSelection
	state.PendingSelection = nil // cleared unconditionally: cancel or complete, either way it's resolved

	if sel.Action == pendingThrow && sel.Item != nil {
		return resolveThrow(state, *sel.Item, key)
	}

	letters := candidateLetters(len(sel.Candidates))
	idx := -1
	for i, l := range letters {
		if key == l {
			idx = i
			break
		}
	}
	if idx == -1 {
		return nil, nil
	}

	chosen := sel.Candidates[idx].Raw
	it, err := itemInfo(chosen)
	if err != nil {
		return nil, err
	}

	if sel.Action == pendingThrow {
		// The item is chosen, but a throw still needs a direction —
		// open a new pendingSelection waiting for that, rather than
		// completing the turn now.
		state.PendingSelection = &pendingSelection{Action: pendingThrow, Item: &sel.Candidates[idx]}
		return nil, nil
	}

	var msg string
	switch sel.Action {
	case pendingEquip:
		msg, err = applyEquip(state, it, chosen)
	case pendingQuaff:
		msg, err = applyUse(state, it, chosen)
	case pendingDrop:
		msg, err = applyDrop(state, it, chosen)
	case pendingPickup:
		msg, err = applyPickup(state, it, chosen)
	}
	if err != nil {
		return nil, err
	}
	return finishTurn(state, messagesOf(msg))
}
