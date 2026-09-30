package main

import (
	"encoding/json"
	"testing"

	"map_api/item"
)

// testMonster mirrors goblin.monster's JSON shape closely enough for
// these tests to build and inspect GoblinState directly — unlike
// testPerson's Wielded (which mirrors player.wieldedWeapon's {item,stats}
// envelope), a goblin's Wielded is a bare item.WeaponStats with no
// envelope, so it needs its own fixture type.
type testMonster struct {
	ID                    string
	X, Y                  int
	Kind                  string `json:"kind,omitempty"`
	Dead                  bool   `json:"dead,omitempty"`
	Aware                 bool
	HP                    int
	MaxHP                 int
	Oiliness              int               `json:"oiliness,omitempty"`
	Burning               bool              `json:"burning,omitempty"`
	BurningTurnsRemaining int               `json:"burning_turns_remaining,omitempty"`
	Wielded               *item.WeaponStats `json:"wielded,omitempty"`
}

func marshalMonster(t *testing.T, m testMonster) []byte {
	t.Helper()
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal monster: %v", err)
	}
	return data
}

func unmarshalMonster(t *testing.T, data []byte) testMonster {
	t.Helper()
	var m testMonster
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal monster: %v", err)
	}
	return m
}

func TestHandleKeyAttackKillingBareHandedGoblinAddsCorpseAtDeathTile(t *testing.T) {
	m := itemsTestMap()
	playerJSON := marshalPerson(t, testPerson{ID: "player:1", X: 7, Y: 7, HP: 100, MaxHP: 100})
	goblinJSON := marshalMonster(t, testMonster{ID: "goblin:1", X: 8, Y: 7, Kind: "goblin", HP: 1, MaxHP: 5, Aware: true})
	state := newTestGameState(m, playerJSON, []MonsterState{monster("goblin", goblinJSON)}, nil)

	if _, err := handleKey(state, "Right"); err != nil {
		t.Fatalf("handleKey(\"Right\"): %v", err)
	}

	goblinObj, err := monsterObject(monsterStateOf(t, state, "goblin"))
	if err != nil {
		t.Fatalf("goblinObject: %v", err)
	}
	if !goblinObj.Dead {
		t.Fatal("expected the goblin to be dead after a guaranteed-lethal attack")
	}

	if len(state.FloorItems) != 1 {
		t.Fatalf("expected exactly 1 floor item (the corpse), got %d", len(state.FloorItems))
	}
	it, err := itemInfo(state.FloorItems[0])
	if err != nil {
		t.Fatalf("itemInfo: %v", err)
	}
	if it.Category != item.CategoryCorpse {
		t.Errorf("expected the floor item to be a corpse, got category %q", it.Category)
	}
	if it.X != 8 || it.Y != 7 {
		t.Errorf("expected the corpse at the death tile (8,7), got (%d,%d)", it.X, it.Y)
	}
}

func TestHandleKeyAttackKillingArmedGoblinDropsCorpseAndWeaponAndClearsWielded(t *testing.T) {
	m := itemsTestMap()
	stats := item.WeaponStats{Name: "rusty sword", BaseDamageMax: 6, Enchantment: -1}
	playerJSON := marshalPerson(t, testPerson{ID: "player:1", X: 7, Y: 7, HP: 100, MaxHP: 100})
	goblinJSON := marshalMonster(t, testMonster{ID: "goblin:1", X: 8, Y: 7, Kind: "goblin", HP: 1, MaxHP: 5, Wielded: &stats})
	state := newTestGameState(m, playerJSON, []MonsterState{monster("goblin", goblinJSON)}, nil)

	if _, err := handleKey(state, "Right"); err != nil {
		t.Fatalf("handleKey(\"Right\"): %v", err)
	}

	if len(state.FloorItems) != 2 {
		t.Fatalf("expected 2 floor items (corpse + dropped weapon), got %d", len(state.FloorItems))
	}
	var sawCorpse, sawWeapon bool
	for _, raw := range state.FloorItems {
		it, err := itemInfo(raw)
		if err != nil {
			t.Fatalf("itemInfo: %v", err)
		}
		if it.X != 8 || it.Y != 7 {
			t.Errorf("expected floor item %q at the death tile (8,7), got (%d,%d)", it.Category, it.X, it.Y)
		}
		switch it.Category {
		case item.CategoryCorpse:
			sawCorpse = true
		case item.CategoryWeapon:
			sawWeapon = true
			if it.Name != "rusty sword" {
				t.Errorf("expected dropped weapon named %q, got %q", "rusty sword", it.Name)
			}
		}
	}
	if !sawCorpse {
		t.Error("expected a corpse item among FloorItems")
	}
	if !sawWeapon {
		t.Error("expected a dropped weapon item among FloorItems")
	}

	got := unmarshalMonster(t, monsterStateOf(t, state, "goblin"))
	if got.Wielded != nil {
		t.Errorf("expected Wielded cleared in GoblinState after dropping, got %+v", got.Wielded)
	}
}

func TestHandleKeyAttackKillingGoblinPreservesNonInventoryAttributesInGoblinState(t *testing.T) {
	m := itemsTestMap()
	playerJSON := marshalPerson(t, testPerson{ID: "player:1", X: 7, Y: 7, HP: 100, MaxHP: 100})
	goblinJSON := marshalMonster(t, testMonster{ID: "goblin:7", X: 8, Y: 7, Kind: "goblin", HP: 1, MaxHP: 9, Aware: true})
	state := newTestGameState(m, playerJSON, []MonsterState{monster("goblin", goblinJSON)}, nil)

	if _, err := handleKey(state, "Right"); err != nil {
		t.Fatalf("handleKey(\"Right\"): %v", err)
	}

	got := unmarshalMonster(t, monsterStateOf(t, state, "goblin"))
	if got.ID != "goblin:7" {
		t.Errorf("expected ID preserved as %q, got %q", "goblin:7", got.ID)
	}
	if got.MaxHP != 9 {
		t.Errorf("expected MaxHP preserved as 9, got %d", got.MaxHP)
	}
	if !got.Aware {
		t.Error("expected Aware preserved as true")
	}
	if got.Kind != "goblin" {
		t.Errorf("expected Kind preserved as %q, got %q", "goblin", got.Kind)
	}
	if !got.Dead || got.HP > 0 {
		t.Errorf("expected the goblin to be dead with HP<=0, got Dead=%v HP=%d", got.Dead, got.HP)
	}
}

func TestPlayerCanMoveOntoDeadGoblinsTile(t *testing.T) {
	m := itemsTestMap()
	playerJSON := marshalPerson(t, testPerson{ID: "player:1", X: 7, Y: 7, HP: 100, MaxHP: 100})
	goblinJSON := marshalMonster(t, testMonster{ID: "goblin:1", X: 8, Y: 7, Kind: "goblin", HP: 1, MaxHP: 5})
	state := newTestGameState(m, playerJSON, []MonsterState{monster("goblin", goblinJSON)}, nil)

	// First "Right" kills the goblin (it's adjacent and occupies (8,7)).
	if _, err := handleKey(state, "Right"); err != nil {
		t.Fatalf("handleKey(\"Right\") kill: %v", err)
	}
	goblinObj, err := monsterObject(monsterStateOf(t, state, "goblin"))
	if err != nil {
		t.Fatalf("goblinObject: %v", err)
	}
	if !goblinObj.Dead {
		t.Fatal("expected the goblin to be dead before testing movement onto its tile")
	}

	// A second "Right" should now be a real move onto (8,7), not another
	// harmless no-op attack against a corpse that can never be walked
	// onto.
	if _, err := handleKey(state, "Right"); err != nil {
		t.Fatalf("handleKey(\"Right\") move: %v", err)
	}
	playerObj, err := playerObject(state.PlayerState)
	if err != nil {
		t.Fatalf("playerObject: %v", err)
	}
	if playerObj.X != 8 || playerObj.Y != 7 {
		t.Errorf("expected the Player to have moved onto the dead goblin's tile (8,7), got (%d,%d)", playerObj.X, playerObj.Y)
	}
}

func TestLookAndPickupSeeCorpseAtDeathTile(t *testing.T) {
	m := itemsTestMap()
	playerJSON := marshalPerson(t, testPerson{ID: "player:1", X: 7, Y: 7, HP: 100, MaxHP: 100})
	goblinJSON := marshalMonster(t, testMonster{ID: "goblin:1", X: 8, Y: 7, Kind: "goblin", HP: 1, MaxHP: 5})
	state := newTestGameState(m, playerJSON, []MonsterState{monster("goblin", goblinJSON)}, nil)

	if _, err := handleKey(state, "Right"); err != nil {
		t.Fatalf("handleKey(\"Right\") kill: %v", err)
	}
	if _, err := handleKey(state, "Right"); err != nil {
		t.Fatalf("handleKey(\"Right\") move onto corpse: %v", err)
	}

	lines, err := lookHereLines(state)
	if err != nil {
		t.Fatalf("lookHereLines: %v", err)
	}
	if len(lines) != 1 || lines[0] == "There is nothing here." {
		t.Fatalf("expected the corpse listed at the Player's tile, got %v", lines)
	}

	playerObj, err := playerObject(state.PlayerState)
	if err != nil {
		t.Fatalf("playerObject: %v", err)
	}
	msg, done, err := pickUp(state, playerObj)
	if err != nil {
		t.Fatalf("pickUp: %v", err)
	}
	if !done {
		t.Fatal("expected done=true with a single candidate (the corpse)")
	}
	if len(state.FloorItems) != 0 {
		t.Errorf("expected the corpse removed from FloorItems after pickup, got %d remaining", len(state.FloorItems))
	}
	if msg == "" {
		t.Error("expected a non-empty pickup confirmation message")
	}
}
