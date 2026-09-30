package game

import (
	"encoding/json"
	"strings"
	"testing"

	"micro/server/internal/mapdata"
	"micro/server/internal/scroll"
)

// scrollAt is torchAt's counterpart for a scroll, built via
// scroll.InitTeleport and relocated to (x,y) by editing its generic
// position fields directly.
func scrollAt(t *testing.T, m *mapdata.Map, x, y int) json.RawMessage {
	t.Helper()
	mapJSON, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal map: %v", err)
	}
	raw, err := scroll.InitTeleport(mapJSON)
	if err != nil {
		t.Fatalf("scroll.InitTeleport: %v", err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatalf("unmarshal scroll: %v", err)
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
		t.Fatalf("marshal relocated scroll: %v", err)
	}
	return out
}

func TestRandomUnoccupiedTileExcludesPlayerAndLiveGoblin(t *testing.T) {
	m := itemsTestMap()
	playerJSON := marshalPerson(t, testPerson{ID: "player:1", X: 7, Y: 7, HP: 15, MaxHP: 15})
	goblinJSON := marshalMonster(t, testMonster{ID: "goblin:1", X: 8, Y: 7, Kind: "goblin", HP: 5, MaxHP: 5})
	state := newTestGameState(m, playerJSON, []MonsterState{monster("goblin", goblinJSON)}, nil)

	for i := 0; i < 200; i++ {
		x, y, ok := randomUnoccupiedTile(state)
		if !ok {
			t.Fatalf("iteration %d: expected ok=true", i)
		}
		if x == 7 && y == 7 {
			t.Fatalf("iteration %d: destination (%d,%d) equals the Player's own tile", i, x, y)
		}
		if x == 8 && y == 7 {
			t.Fatalf("iteration %d: destination (%d,%d) equals the live Goblin's tile", i, x, y)
		}
		r := m.Rooms[0]
		if x < r.X || x >= r.X+r.W || y < r.Y || y >= r.Y+r.H {
			t.Fatalf("iteration %d: destination (%d,%d) is outside the room's walkable area", i, x, y)
		}
	}
}

func TestRandomUnoccupiedTileIgnoresDeadGoblin(t *testing.T) {
	m := itemsTestMap()
	playerJSON := marshalPerson(t, testPerson{ID: "player:1", X: 7, Y: 7, HP: 15, MaxHP: 15})
	state := newTestGameState(m, playerJSON, []MonsterState{deadMonster(t, "goblin")}, nil)

	// deadGoblinState places the goblin at the zero value (0,0), well
	// outside itemsTestMap's room — if randomUnoccupiedTile incorrectly
	// excluded a dead goblin's tile it wouldn't be observable here, but
	// this at least confirms a dead goblin never causes a spurious
	// failure (e.g. treating (0,0) as blocking somehow).
	for i := 0; i < 50; i++ {
		if _, _, ok := randomUnoccupiedTile(state); !ok {
			t.Fatalf("iteration %d: expected ok=true with only a dead goblin present", i)
		}
	}
}

func TestApplyQuaffTeleportMovesPlayerToAWalkableUnoccupiedTile(t *testing.T) {
	m := itemsTestMap()
	scrollJSON := scrollAt(t, m, 7, 7)
	playerJSON := marshalPerson(t, testPerson{ID: "player:1", X: 7, Y: 7, HP: 15, MaxHP: 15})
	goblinJSON := marshalMonster(t, testMonster{ID: "goblin:1", X: 8, Y: 7, Kind: "goblin", HP: 5, MaxHP: 5})
	state := newTestGameState(m, playerJSON, []MonsterState{monster("goblin", goblinJSON)}, []json.RawMessage{scrollJSON})

	if _, err := handleKey(state, ","); err != nil {
		t.Fatalf("handleKey(\",\"): %v", err)
	}
	if _, err := handleKey(state, "A"); err != nil {
		t.Fatalf("handleKey(\"A\"): %v", err)
	}

	got := unmarshalPerson(t, state.PlayerState)
	if got.X == 7 && got.Y == 7 {
		t.Error("expected the Player to have moved off the original tile")
	}
	if got.X == 8 && got.Y == 7 {
		t.Error("expected the Player not to land on the Goblin's tile")
	}
	r := m.Rooms[0]
	if got.X < r.X || got.X >= r.X+r.W || got.Y < r.Y || got.Y >= r.Y+r.H {
		t.Errorf("expected the Player's new position (%d,%d) inside the room", got.X, got.Y)
	}
}

func TestApplyQuaffTeleportConsumesTheScroll(t *testing.T) {
	m := itemsTestMap()
	scrollJSON := scrollAt(t, m, 7, 7)
	playerJSON := marshalPerson(t, testPerson{ID: "player:1", X: 7, Y: 7, HP: 15, MaxHP: 15})
	state := newTestGameState(m, playerJSON, []MonsterState{deadMonster(t, "goblin")}, []json.RawMessage{scrollJSON})

	if _, err := handleKey(state, ","); err != nil {
		t.Fatalf("handleKey(\",\"): %v", err)
	}
	if _, err := handleKey(state, "A"); err != nil {
		t.Fatalf("handleKey(\"A\"): %v", err)
	}

	got := unmarshalPerson(t, state.PlayerState)
	if len(got.Inventory) != 0 {
		t.Errorf("expected the scroll consumed from Inventory, got %d entries", len(got.Inventory))
	}
}

func TestApplyQuaffTeleportRevealsTheNewPositionThisTurn(t *testing.T) {
	m := itemsTestMap()
	scrollJSON := scrollAt(t, m, 7, 7)
	playerJSON := marshalPerson(t, testPerson{ID: "player:1", X: 7, Y: 7, HP: 15, MaxHP: 15})
	state := newTestGameState(m, playerJSON, []MonsterState{deadMonster(t, "goblin")}, []json.RawMessage{scrollJSON})

	if _, err := handleKey(state, ","); err != nil {
		t.Fatalf("handleKey(\",\"): %v", err)
	}
	if _, err := handleKey(state, "A"); err != nil {
		t.Fatalf("handleKey(\"A\"): %v", err)
	}

	if !state.Visited.Rooms[0] {
		t.Error("expected the room containing the Player's new position to be revealed")
	}
}

func TestEffectMessageReportsTeleport(t *testing.T) {
	m := itemsTestMap()
	scrollJSON := scrollAt(t, m, 7, 7)
	playerJSON := marshalPerson(t, testPerson{ID: "player:1", X: 7, Y: 7, HP: 15, MaxHP: 15})
	state := newTestGameState(m, playerJSON, []MonsterState{deadMonster(t, "goblin")}, []json.RawMessage{scrollJSON})

	if _, err := handleKey(state, ","); err != nil {
		t.Fatalf("handleKey(\",\"): %v", err)
	}
	msg, err := handleKey(state, "A")
	if err != nil {
		t.Fatalf("handleKey(\"A\"): %v", err)
	}
	if !strings.Contains(msg, "teleported") {
		t.Errorf("expected a teleport message, got %q", msg)
	}
}
