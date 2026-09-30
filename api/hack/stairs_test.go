package main

import (
	"encoding/json"
	"strings"
	"testing"

	"map_api/mapdata"
	"map_api/mapgen"
	"map_api/stairs"
)

// stairsAt builds a real stairs item (via stairs.InitUp/InitDown, so its
// JSON shape is exactly what the stairs package actually produces) and
// relocates it to (x,y), mirroring flaskAt/potionAt's exact pattern.
func stairsAt(t *testing.T, m *mapdata.Map, kind stairs.Kind, x, y int) json.RawMessage {
	t.Helper()
	mapJSON, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal map: %v", err)
	}
	var raw []byte
	if kind == stairs.KindUp {
		raw, err = stairs.InitUp(mapJSON)
	} else {
		raw, err = stairs.InitDown(mapJSON)
	}
	if err != nil {
		t.Fatalf("stairs.Init: %v", err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatalf("unmarshal stairs: %v", err)
	}
	xJSON, err := json.Marshal(x)
	if err != nil {
		t.Fatalf("marshal x: %v", err)
	}
	yJSON, err := json.Marshal(y)
	if err != nil {
		t.Fatalf("marshal y: %v", err)
	}
	fields["X"] = xJSON
	fields["Y"] = yJSON
	out, err := json.Marshal(fields)
	if err != nil {
		t.Fatalf("marshal relocated stairs: %v", err)
	}
	return out
}

func TestPickUpSkipsFixedItems(t *testing.T) {
	m := itemsTestMap()
	down := stairsAt(t, m, stairs.KindDown, 7, 7)
	playerJSON := marshalPerson(t, testPerson{ID: "player:1", X: 7, Y: 7, HP: 15})
	state := newTestGameState(m, playerJSON, []MonsterState{deadMonster(t, "goblin")}, []json.RawMessage{down})

	msg, err := handleKey(state, ",")
	if err != nil {
		t.Fatalf("handleKey(\",\"): %v", err)
	}
	if !strings.Contains(msg, "nothing here") {
		t.Errorf("expected a 'nothing here to pick up' message (stairs are Fixed), got %q", msg)
	}
	if len(state.FloorItems) != 1 {
		t.Fatalf("expected the stairs to remain on the floor, got %d floor items", len(state.FloorItems))
	}
	got := unmarshalPerson(t, state.PlayerState)
	if len(got.Inventory) != 0 {
		t.Errorf("expected Inventory to stay empty, got %d items", len(got.Inventory))
	}
}

func testLevelConfig() mapgen.Config {
	cfg := mapgen.DefaultConfig()
	cfg.Width, cfg.Height, cfg.NumRooms = 30, 30, 3
	cfg.Seed = 99
	return cfg
}

func TestApplyStairsClimbsWhenStandingOnMatchingStairs(t *testing.T) {
	m := itemsTestMap()
	down := stairsAt(t, m, stairs.KindDown, 7, 7)
	playerJSON := marshalPerson(t, testPerson{ID: "player:1", X: 7, Y: 7, HP: 15})
	state := newTestGameState(m, playerJSON, []MonsterState{deadMonster(t, "goblin")}, []json.RawMessage{down})
	state.LevelConfig = testLevelConfig()
	state.Levels = make([]*levelState, maxLevels)
	state.Levels[0] = state.levelState

	msg, err := handleKey(state, ">")
	if err != nil {
		t.Fatalf("handleKey(\">\"): %v", err)
	}
	if !strings.Contains(msg, "descend to level 2") {
		t.Errorf("expected a 'descend to level 2' message, got %q", msg)
	}
	if state.CurrentLevel != 2 {
		t.Errorf("expected CurrentLevel 2, got %d", state.CurrentLevel)
	}
	if state.Levels[0] == nil {
		t.Error("expected level 1's state to remain stored in Levels[0]")
	}
}

func TestApplyStairsReportsNoStairsWhenNotStandingOnAny(t *testing.T) {
	m := itemsTestMap()
	playerJSON := marshalPerson(t, testPerson{ID: "player:1", X: 7, Y: 7, HP: 15})
	state := newTestGameState(m, playerJSON, []MonsterState{deadMonster(t, "goblin")}, nil)

	msg, err := handleKey(state, ">")
	if err != nil {
		t.Fatalf("handleKey(\">\"): %v", err)
	}
	if !strings.Contains(msg, "no stairs down here") {
		t.Errorf("expected a 'no stairs down here' message, got %q", msg)
	}
}

func TestApplyStairsIgnoresTheWrongKindUnderfoot(t *testing.T) {
	m := itemsTestMap()
	up := stairsAt(t, m, stairs.KindUp, 7, 7)
	playerJSON := marshalPerson(t, testPerson{ID: "player:1", X: 7, Y: 7, HP: 15})
	state := newTestGameState(m, playerJSON, []MonsterState{deadMonster(t, "goblin")}, []json.RawMessage{up})

	msg, err := handleKey(state, ">")
	if err != nil {
		t.Fatalf("handleKey(\">\"): %v", err)
	}
	if !strings.Contains(msg, "no stairs down here") {
		t.Errorf("expected standing on stairs up to not satisfy the down key, got %q", msg)
	}
}
