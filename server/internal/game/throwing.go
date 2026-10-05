// Throwing: choosing an item, choosing a direction, and resolving what
// the thrown thing hits.
package game

import (
	"micro/server/internal/rules"
)

// throwItem starts a throw. With one throwable item it is chosen at once
// and the game waits for a direction; with several, a letter picker opens
// first. Either way no turn passes until a direction is given.
func throwItem(state *GameState) ([]string, bool) {
	items := rules.Throwable(state.Player)
	switch len(items) {
	case 0:
		return messagesOf("You have nothing to throw."), true
	case 1:
		state.PendingSelection = &pendingSelection{Action: pendingThrow, Item: items[0]}
	default:
		state.PendingSelection = &pendingSelection{Action: pendingThrow, Candidates: items}
	}
	return nil, false
}

// directions maps each arrow key to the step it means.
var directions = map[string][2]int{
	"Up":    {0, -1},
	"Down":  {0, 1},
	"Left":  {-1, 0},
	"Right": {1, 0},
}

// resolveThrow throws item once a direction is known. Any key that is not
// a direction cancels the throw silently, the same way any other key
// cancels a letter picker.
func resolveThrow(state *GameState, item *rules.Entity, key string) ([]string, bool) {
	d, ok := directions[key]
	if !ok {
		return nil, false
	}
	c := state.ctx()
	res := c.Throw(state.Player, item, d[0], d[1])
	if res.Refused {
		return messagesOf(res.Msg), false
	}
	messages := append(messagesOf(res.Msg), res.Notes...)
	if res.Landed != nil {
		state.FloorItems = append(state.FloorItems, res.Landed)
	}
	if res.Target != nil {
		messages = append(messages, defendMessage(res.Target.Name, res.Outcome))
		fallen(state, c, res.Target)
	}
	return append(messages, c.Messages()...), true
}

// fallen leaves a monster that has just died where it fell, with its
// corpse and everything it carried on the floor. It must be called once,
// right after whatever killed it; a no-op if m is alive.
func fallen(state *GameState, c *rules.Ctx, m *rules.Entity) {
	if m.Dead {
		state.FloorItems = append(state.FloorItems, c.Kill(m)...)
	}
}
