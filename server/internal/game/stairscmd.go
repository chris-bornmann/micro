// Taking a staircase. The level transition it triggers lives in
// levels.go; this is only the command half.
package game

import "fmt"

// applyStairs is the "<"/">" key's handler: if the Player is standing on
// stairs leading dir ("up" or "down"), applyStairsTransition moves them to
// the level it leads to. Otherwise it says there are no such stairs here;
// the turn still passes, as it does for picking up nothing.
//
// It is the one command that can fail: reaching a level for the first
// time generates it, and a level that cannot be generated is a server
// fault to report, not a game event.
func applyStairs(state *GameState, dir string) ([]string, error) {
	if stairsAmong(state.floorItemsAt(state.Player.X, state.Player.Y), dir) == nil {
		return messagesOf(fmt.Sprintf("There are no stairs %s here.", dir)), nil
	}
	msg, err := applyStairsTransition(state, dir)
	if err != nil {
		return nil, err
	}
	return messagesOf(msg), nil
}
