package game

import (
	"strings"
	"testing"

	"micro/server/internal/rules"
)

func TestPickUpSkipsFixedItems(t *testing.T) {
	down := testItem(t, "stairs_down", 7, 7)
	state := newTestGameState(t, itemsTestMap(), testPlayer(t, 7, 7, 15), nil, []*rules.Entity{down})

	if msg := mustKey(t, state, ","); !strings.Contains(msg, "nothing here") {
		t.Errorf("expected a 'nothing here to pick up' message (stairs are fixed), got %q", msg)
	}
	if len(state.FloorItems) != 1 || len(state.Player.Inventory) != 0 {
		t.Error("expected the stairs to stay on the floor")
	}
}

func TestApplyStairsClimbsWhenStandingOnMatchingStairs(t *testing.T) {
	down := testItem(t, "stairs_down", 7, 7)
	state := newTestGameState(t, itemsTestMap(), testPlayer(t, 7, 7, 15), nil, []*rules.Entity{down})
	state.LevelConfig = testLevelConfig()
	state.Levels = make([]*levelState, maxLevels)
	state.Levels[0] = state.levelState

	if msg := mustKey(t, state, ">"); !strings.Contains(msg, "descend to level 2") {
		t.Errorf("expected a 'descend to level 2' message, got %q", msg)
	}
	if state.CurrentLevel != 2 || state.Levels[0] == nil {
		t.Errorf("expected to be on level 2 with level 1 still stored, CurrentLevel %d", state.CurrentLevel)
	}
}

func TestApplyStairsReportsNoStairsWhenNotStandingOnAny(t *testing.T) {
	state := newTestGameState(t, itemsTestMap(), testPlayer(t, 7, 7, 15), nil, nil)
	if msg := mustKey(t, state, ">"); !strings.Contains(msg, "no stairs down here") {
		t.Errorf("expected a 'no stairs down here' message, got %q", msg)
	}
}

func TestApplyStairsIgnoresTheWrongKindUnderfoot(t *testing.T) {
	up := testItem(t, "stairs_up", 7, 7)
	state := newTestGameState(t, itemsTestMap(), testPlayer(t, 7, 7, 15), nil, []*rules.Entity{up})
	if msg := mustKey(t, state, ">"); !strings.Contains(msg, "no stairs down here") {
		t.Errorf("expected standing on stairs up to not satisfy the down key, got %q", msg)
	}
}
