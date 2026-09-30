package game

import (
	"encoding/json"
	"testing"
)

// TestBothGoblinAndMummySpawnAtStartup mirrors what main() does when it
// builds state.Monsters — calling both initGoblinArmed and initMummy and
// appending each with its own Kind — and confirms the result is a
// two-entry polymorphic list, not just a goblin.
func TestBothGoblinAndMummySpawnAtStartup(t *testing.T) {
	m := itemsTestMap()

	goblinState, err := initGoblinArmed(m, 0)
	if err != nil {
		t.Fatalf("initGoblinArmed: %v", err)
	}
	mummyState, err := initMummy(m, 0)
	if err != nil {
		t.Fatalf("initMummy: %v", err)
	}
	state := newTestGameState(m, nil, []MonsterState{
		{Kind: "goblin", State: goblinState},
		{Kind: "mummy", State: mummyState},
	}, nil)

	if len(state.Monsters) != 2 {
		t.Fatalf("expected 2 monsters, got %d", len(state.Monsters))
	}
	goblinObj, err := monsterObject(monsterStateOf(t, state, "goblin"))
	if err != nil {
		t.Fatalf("monsterObject(goblin): %v", err)
	}
	if goblinObj.Kind != "goblin" {
		t.Errorf("expected goblin Kind %q, got %q", "goblin", goblinObj.Kind)
	}
	mummyObj, err := monsterObject(monsterStateOf(t, state, "mummy"))
	if err != nil {
		t.Fatalf("monsterObject(mummy): %v", err)
	}
	if mummyObj.Kind != "mummy" {
		t.Errorf("expected mummy Kind %q, got %q", "mummy", mummyObj.Kind)
	}
}

// TestHandleKeyAttackHitsOnlyTheAdjacentMonster confirms the Player can
// attack one monster (the goblin, adjacent) while a second monster (the
// mummy, elsewhere in the same room) is left untouched by that attack —
// each is tracked and routed to independently via monsterIndexAt.
func TestHandleKeyAttackHitsOnlyTheAdjacentMonster(t *testing.T) {
	m := itemsTestMap()
	playerJSON := marshalPerson(t, testPerson{ID: "player:1", X: 7, Y: 7, HP: 100, MaxHP: 100})
	goblinJSON := marshalMonster(t, testMonster{ID: "goblin:1", X: 8, Y: 7, Kind: "goblin", HP: 5, MaxHP: 5, Aware: true})
	mummyJSON := marshalMonster(t, testMonster{ID: "mummy:1", X: 13, Y: 13, Kind: "mummy", HP: 25, MaxHP: 25})
	state := newTestGameState(m, playerJSON, []MonsterState{monster("goblin", goblinJSON), monster("mummy", mummyJSON)}, nil)

	if _, err := handleKey(state, "Right"); err != nil {
		t.Fatalf("handleKey(\"Right\"): %v", err)
	}

	goblin := unmarshalMonster(t, monsterStateOf(t, state, "goblin"))
	if goblin.HP >= 5 {
		t.Errorf("expected the adjacent goblin to take damage, HP still %d", goblin.HP)
	}
	mummy := unmarshalMonster(t, monsterStateOf(t, state, "mummy"))
	if mummy.HP != 25 {
		t.Errorf("expected the distant mummy's HP untouched by the goblin's attack, got %d", mummy.HP)
	}
	if mummy.Dead {
		t.Error("expected the distant mummy to remain alive")
	}
}

// TestKillingOneMonsterDoesNotDisturbTheOther confirms that killing the
// goblin (built with HP low enough to die in one hit) builds its corpse
// and leaves the mummy's own state completely unaffected.
func TestKillingOneMonsterDoesNotDisturbTheOther(t *testing.T) {
	m := itemsTestMap()
	playerJSON := marshalPerson(t, testPerson{ID: "player:1", X: 7, Y: 7, HP: 100, MaxHP: 100})
	goblinJSON := marshalMonster(t, testMonster{ID: "goblin:1", X: 8, Y: 7, Kind: "goblin", HP: 1, MaxHP: 5, Aware: true})
	mummyJSON := marshalMonster(t, testMonster{ID: "mummy:1", X: 13, Y: 13, Kind: "mummy", HP: 25, MaxHP: 25})
	state := newTestGameState(m, playerJSON, []MonsterState{monster("goblin", goblinJSON), monster("mummy", mummyJSON)}, nil)

	if _, err := handleKey(state, "Right"); err != nil {
		t.Fatalf("handleKey(\"Right\"): %v", err)
	}

	if len(state.Monsters) != 2 {
		t.Fatalf("expected the dead goblin to remain in state.Monsters (not removed), got %d entries", len(state.Monsters))
	}
	goblin := unmarshalMonster(t, monsterStateOf(t, state, "goblin"))
	if !goblin.Dead {
		t.Error("expected the goblin to be dead")
	}
	if len(state.FloorItems) != 1 {
		t.Fatalf("expected exactly one corpse item on the floor, got %d", len(state.FloorItems))
	}

	mummy := unmarshalMonster(t, monsterStateOf(t, state, "mummy"))
	if mummy.HP != 25 || mummy.Dead {
		t.Errorf("expected the mummy's state untouched by the goblin's death, got HP=%d Dead=%v", mummy.HP, mummy.Dead)
	}
}

// TestMummyBurnTickDamageIsDoubleGoblins is a side-by-side check, using
// the same monsterServices registry hack itself dispatches through, that
// a mummy's per-turn burn damage lands in double a goblin's range under
// identical burning state — the one behavioral difference between the
// two monster kinds' Tick.
func TestMummyBurnTickDamageIsDoubleGoblins(t *testing.T) {
	for i := 0; i < 30; i++ {
		goblinJSON := marshalMonster(t, testMonster{ID: "goblin:1", X: 6, Y: 6, Kind: "goblin", HP: 100, MaxHP: 100, Burning: true, BurningTurnsRemaining: 3})
		goblinOut, err := monsterServices["goblin"].Tick(goblinJSON)
		if err != nil {
			t.Fatalf("goblin Tick: %v", err)
		}
		var goblinResp tickEnvelope
		if err := json.Unmarshal(goblinOut, &goblinResp); err != nil {
			t.Fatalf("unmarshal goblin tick response: %v", err)
		}
		if goblinResp.Outcome.BurnDamage < 2 || goblinResp.Outcome.BurnDamage > 4 {
			t.Fatalf("expected goblin BurnDamage in [2,4], got %d", goblinResp.Outcome.BurnDamage)
		}

		mummyJSON := marshalMonster(t, testMonster{ID: "mummy:1", X: 6, Y: 6, Kind: "mummy", HP: 100, MaxHP: 100, Burning: true, BurningTurnsRemaining: 3})
		mummyOut, err := monsterServices["mummy"].Tick(mummyJSON)
		if err != nil {
			t.Fatalf("mummy Tick: %v", err)
		}
		var mummyResp tickEnvelope
		if err := json.Unmarshal(mummyOut, &mummyResp); err != nil {
			t.Fatalf("unmarshal mummy tick response: %v", err)
		}
		if mummyResp.Outcome.BurnDamage < 4 || mummyResp.Outcome.BurnDamage > 8 {
			t.Fatalf("expected mummy BurnDamage in [4,8] (double a goblin's), got %d", mummyResp.Outcome.BurnDamage)
		}
	}
}
