package goblin

import (
	"encoding/json"
	"testing"

	"map_api/overlay"
)

func TestTickConsumesGraceTurnWithoutDamageOrDecrementing(t *testing.T) {
	self, err := json.Marshal(monster{
		Object: overlay.Object{ID: "goblin:1", X: 6, Y: 6, Glyph: 'g', Burning: true},
		HP:     10, MaxHP: 10,
		BurningTurnsRemaining: 3,
		BurningStartsNextTurn: true,
	})
	if err != nil {
		t.Fatalf("marshal self: %v", err)
	}

	out, err := Tick(self)
	if err != nil {
		t.Fatalf("Tick: %v", err)
	}
	var resp tickResponse
	if err := json.Unmarshal(out, &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Outcome.BurnDamage != 0 {
		t.Errorf("expected no burn damage on the grace turn, got %d", resp.Outcome.BurnDamage)
	}
	if resp.Self.HP != 10 {
		t.Errorf("expected HP untouched on the grace turn, got %d", resp.Self.HP)
	}
	if resp.Self.BurningTurnsRemaining != 3 {
		t.Errorf("expected BurningTurnsRemaining left untouched at 3 on the grace turn, got %d", resp.Self.BurningTurnsRemaining)
	}
	if resp.Self.BurningStartsNextTurn {
		t.Error("expected BurningStartsNextTurn cleared after being consumed")
	}

	// The following Tick then applies real burn damage.
	selfAfterGraceTurn, err := json.Marshal(resp.Self)
	if err != nil {
		t.Fatalf("marshal self after grace turn: %v", err)
	}
	out2, err := Tick(selfAfterGraceTurn)
	if err != nil {
		t.Fatalf("Tick (2nd, real damage): %v", err)
	}
	var resp2 tickResponse
	if err := json.Unmarshal(out2, &resp2); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp2.Outcome.BurnDamage < 2 || resp2.Outcome.BurnDamage > 4 {
		t.Errorf("expected real burn damage in [2,4] on the turn after the grace turn, got %d", resp2.Outcome.BurnDamage)
	}
}

func TestTickAppliesBurnDamageAndDecrementsCountdown(t *testing.T) {
	self, err := json.Marshal(monster{
		Object: overlay.Object{ID: "goblin:1", X: 6, Y: 6, Glyph: 'g', Burning: true},
		HP:     10, MaxHP: 10,
		BurningTurnsRemaining: 3,
	})
	if err != nil {
		t.Fatalf("marshal self: %v", err)
	}

	out, err := Tick(self)
	if err != nil {
		t.Fatalf("Tick: %v", err)
	}
	var resp tickResponse
	if err := json.Unmarshal(out, &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Outcome.BurnDamage < 2 || resp.Outcome.BurnDamage > 4 {
		t.Errorf("expected BurnDamage in [2,4], got %d", resp.Outcome.BurnDamage)
	}
	if resp.Self.HP != 10-resp.Outcome.BurnDamage {
		t.Errorf("expected HP reduced by BurnDamage, got HP=%d BurnDamage=%d", resp.Self.HP, resp.Outcome.BurnDamage)
	}
	if resp.Self.BurningTurnsRemaining != 2 {
		t.Errorf("expected BurningTurnsRemaining decremented to 2, got %d", resp.Self.BurningTurnsRemaining)
	}
	if !resp.Self.Burning {
		t.Error("expected Burning to stay true with turns still remaining")
	}
	if resp.Outcome.BurnedOut {
		t.Error("expected BurnedOut false with turns still remaining")
	}
}

func TestTickClearsBurningAndReportsBurnedOutOnLastTurn(t *testing.T) {
	self, err := json.Marshal(monster{
		Object: overlay.Object{ID: "goblin:1", X: 6, Y: 6, Glyph: 'g', Burning: true},
		HP:     10, MaxHP: 10,
		BurningTurnsRemaining: 1,
	})
	if err != nil {
		t.Fatalf("marshal self: %v", err)
	}

	out, err := Tick(self)
	if err != nil {
		t.Fatalf("Tick: %v", err)
	}
	var resp tickResponse
	if err := json.Unmarshal(out, &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Self.Burning {
		t.Error("expected Burning cleared once BurningTurnsRemaining reaches 0")
	}
	if !resp.Outcome.BurnedOut {
		t.Error("expected BurnedOut true on the tick the fire goes out")
	}
}

func TestTickBurnDamageCanKillTheGoblin(t *testing.T) {
	self, err := json.Marshal(monster{
		Object: overlay.Object{ID: "goblin:1", X: 6, Y: 6, Glyph: 'g', Burning: true},
		HP:     1, MaxHP: 10,
		BurningTurnsRemaining: 3,
	})
	if err != nil {
		t.Fatalf("marshal self: %v", err)
	}

	out, err := Tick(self)
	if err != nil {
		t.Fatalf("Tick: %v", err)
	}
	var resp tickResponse
	if err := json.Unmarshal(out, &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !resp.Self.Dead {
		t.Errorf("expected Dead true after burn damage drops HP to %d", resp.Self.HP)
	}
}

func TestTickBurnDamageTriggersFleeingAtThreshold(t *testing.T) {
	self, err := json.Marshal(monster{
		Object: overlay.Object{ID: "goblin:1", X: 6, Y: 6, Glyph: 'g', Burning: true},
		HP:     5, MaxHP: 10, // fleeThreshold(10) == 1
		BurningTurnsRemaining: 3,
	})
	if err != nil {
		t.Fatalf("marshal self: %v", err)
	}

	out, err := Tick(self)
	if err != nil {
		t.Fatalf("Tick: %v", err)
	}
	var resp tickResponse
	if err := json.Unmarshal(out, &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	// Burn damage is [2,4], so HP could land anywhere from 1 to 3 —
	// fleeThreshold(10) is 1, so Fleeing should trigger only if HP drops
	// to exactly 1. Instead of depending on the roll, just confirm the
	// invariant: Fleeing is true whenever (and only whenever) HP is at
	// or below the threshold, mirroring Defend's own rule.
	wantFleeing := !resp.Self.Dead && resp.Self.HP <= fleeThreshold(10)
	if resp.Self.Fleeing != wantFleeing {
		t.Errorf("expected Fleeing=%v at HP=%d (threshold %d), got %v", wantFleeing, resp.Self.HP, fleeThreshold(10), resp.Self.Fleeing)
	}
}

func TestTickWithNoBurningReportsNoBurnDamage(t *testing.T) {
	self := selfJSONWithHP(t, "goblin:1", 6, 6, 10)
	out, err := Tick(self)
	if err != nil {
		t.Fatalf("Tick: %v", err)
	}
	var resp tickResponse
	if err := json.Unmarshal(out, &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Outcome.BurnDamage != 0 || resp.Outcome.BurnedOut {
		t.Errorf("expected no burn outcome without burning, got %+v", resp.Outcome)
	}
}
