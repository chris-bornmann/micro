package mummy

import (
	"encoding/json"
	"testing"

	"micro/server/internal/overlay"
)

func TestTickConsumesGraceTurnWithoutDamageOrDecrementing(t *testing.T) {
	self, err := json.Marshal(creature{
		Object:                overlay.Object{ID: "mummy:1", X: 6, Y: 6, Glyph: 'm', Burning: true},
		HP:                    25,
		MaxHP:                 25,
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
	if resp.Self.HP != 25 {
		t.Errorf("expected HP untouched on the grace turn, got %d", resp.Self.HP)
	}
	if resp.Self.BurningTurnsRemaining != 3 {
		t.Errorf("expected BurningTurnsRemaining left untouched at 3, got %d", resp.Self.BurningTurnsRemaining)
	}
	if resp.Self.BurningStartsNextTurn {
		t.Error("expected BurningStartsNextTurn cleared after being consumed")
	}
}

func TestTickAppliesDoubleBurnDamageAndDecrementsCountdown(t *testing.T) {
	self, err := json.Marshal(creature{
		Object:                overlay.Object{ID: "mummy:1", X: 6, Y: 6, Glyph: 'm', Burning: true},
		HP:                    25,
		MaxHP:                 25,
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
	// fire.TickDamage() is [2,4]; a mummy takes double that: [4,8] —
	// this is the key thing that distinguishes a mummy from a goblin
	// (whose burn damage stays [2,4]).
	if resp.Outcome.BurnDamage < 4 || resp.Outcome.BurnDamage > 8 {
		t.Errorf("expected doubled BurnDamage in [4,8], got %d", resp.Outcome.BurnDamage)
	}
	if resp.Self.HP != 25-resp.Outcome.BurnDamage {
		t.Errorf("expected HP reduced by BurnDamage, got HP=%d BurnDamage=%d", resp.Self.HP, resp.Outcome.BurnDamage)
	}
	if resp.Self.BurningTurnsRemaining != 2 {
		t.Errorf("expected BurningTurnsRemaining decremented to 2, got %d", resp.Self.BurningTurnsRemaining)
	}
}

func TestTickClearsBurningAndReportsBurnedOutOnLastTurn(t *testing.T) {
	self, err := json.Marshal(creature{
		Object:                overlay.Object{ID: "mummy:1", X: 6, Y: 6, Glyph: 'm', Burning: true},
		HP:                    25,
		MaxHP:                 25,
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

func TestTickBurnDamageCanKillTheMummy(t *testing.T) {
	self, err := json.Marshal(creature{
		Object:                overlay.Object{ID: "mummy:1", X: 6, Y: 6, Glyph: 'm', Burning: true},
		HP:                    1,
		MaxHP:                 25,
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

func TestTickWithNoBurningReportsNoBurnDamage(t *testing.T) {
	self := selfJSONWithHP(t, "mummy:1", 6, 6, 25)
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
