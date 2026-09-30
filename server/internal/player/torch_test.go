package player

import (
	"encoding/json"
	"errors"
	"testing"

	"micromap/server/internal/item"
	"micromap/server/internal/overlay"
)

// testTorchItemJSON marshals a minimal item.Item with CategoryTorch and
// the given ID, standing in for a torch's own last-returned raw state.
func testTorchItemJSON(t *testing.T, id, name string) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(item.Item{
		Object:   overlay.Object{ID: id, Glyph: '/'},
		Category: item.CategoryTorch,
		Name:     name,
		Usage:    item.UsageOneShot,
	})
	if err != nil {
		t.Fatalf("marshal item: %v", err)
	}
	return data
}

func TestLightTorchMovesItemFromInventoryToLitTorchSlot(t *testing.T) {
	self := invSelfJSON(t, overlay.Object{ID: "player:1"}, 15, testTorchItemJSON(t, "torch:1", "torch"))

	out, err := LightTorch(self, "torch:1", 12)
	if err != nil {
		t.Fatalf("LightTorch: %v", err)
	}
	var got person
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(got.Inventory) != 0 {
		t.Errorf("expected torch removed from Inventory, got %d entries", len(got.Inventory))
	}
	if got.LitTorch == nil {
		t.Fatal("expected LitTorch to be set")
	}
	if got.LitTorch.TurnsRemaining != 12 {
		t.Errorf("expected TurnsRemaining 12, got %d", got.LitTorch.TurnsRemaining)
	}
	var lit item.Item
	if err := json.Unmarshal(got.LitTorch.Item, &lit); err != nil {
		t.Fatalf("unmarshal lit torch item: %v", err)
	}
	if lit.ID != "torch:1" {
		t.Errorf("expected lit torch item ID %q, got %q", "torch:1", lit.ID)
	}
}

func TestLightTorchSetsLitBit(t *testing.T) {
	self := invSelfJSON(t, overlay.Object{ID: "player:1"}, 15, testTorchItemJSON(t, "torch:1", "torch"))

	out, err := LightTorch(self, "torch:1", 12)
	if err != nil {
		t.Fatalf("LightTorch: %v", err)
	}
	var got person
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !got.Lit {
		t.Error("expected Lit to be true")
	}
}

func TestLightTorchErrorsWhenAlreadyLit(t *testing.T) {
	self, err := json.Marshal(person{
		Object:   overlay.Object{ID: "player:1", Lit: true},
		HP:       15,
		LitTorch: &litTorch{Item: testTorchItemJSON(t, "torch:1", "torch"), TurnsRemaining: 5},
		Inventory: []json.RawMessage{
			testTorchItemJSON(t, "torch:2", "torch"),
		},
	})
	if err != nil {
		t.Fatalf("marshal self: %v", err)
	}

	out, lightErr := LightTorch(self, "torch:2", 12)
	if !errors.Is(lightErr, ErrTorchAlreadyLit) {
		t.Fatalf("expected ErrTorchAlreadyLit, got %v", lightErr)
	}
	if out != nil {
		t.Errorf("expected nil output on rejection, got %s", out)
	}
}

func TestLightTorchErrorsWhenIDNotFound(t *testing.T) {
	self := invSelfJSON(t, overlay.Object{ID: "player:1"}, 15)
	if _, err := LightTorch(self, "torch:999", 12); err == nil {
		t.Error("expected an error for a missing item ID, got nil")
	}
}

func TestLightTorchPreservesHPAndPosition(t *testing.T) {
	self := invSelfJSON(t, overlay.Object{ID: "player:1", X: 3, Y: 4}, 15, testTorchItemJSON(t, "torch:1", "torch"))

	out, err := LightTorch(self, "torch:1", 12)
	if err != nil {
		t.Fatalf("LightTorch: %v", err)
	}
	var got person
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.HP != 15 || got.X != 3 || got.Y != 4 {
		t.Errorf("expected HP/position untouched, got HP=%d (%d,%d)", got.HP, got.X, got.Y)
	}
}

func TestLitTorchItemReturnsRawItemAndFlag(t *testing.T) {
	torchJSON := testTorchItemJSON(t, "torch:1", "torch")
	self, err := json.Marshal(person{
		Object:   overlay.Object{ID: "player:1", Lit: true},
		HP:       15,
		LitTorch: &litTorch{Item: torchJSON, TurnsRemaining: 5},
	})
	if err != nil {
		t.Fatalf("marshal self: %v", err)
	}

	raw, ok, err := LitTorchItem(self)
	if err != nil {
		t.Fatalf("LitTorchItem: %v", err)
	}
	if !ok {
		t.Fatal("expected ok=true when a torch is lit")
	}
	var got item.Item
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.ID != "torch:1" {
		t.Errorf("expected lit torch item ID %q, got %q", "torch:1", got.ID)
	}
}

func TestLitTorchItemReturnsFalseWhenNothingLit(t *testing.T) {
	self := invSelfJSON(t, overlay.Object{ID: "player:1"}, 15)
	raw, ok, err := LitTorchItem(self)
	if err != nil {
		t.Fatalf("LitTorchItem: %v", err)
	}
	if ok {
		t.Errorf("expected ok=false, got true with raw=%s", raw)
	}
}

func TestTickDecrementsLitTorchAndReportsNothingBeforeExpiry(t *testing.T) {
	self, err := json.Marshal(person{
		Object:   overlay.Object{ID: "player:1", Lit: true},
		HP:       10,
		LitTorch: &litTorch{Item: testTorchItemJSON(t, "torch:1", "torch"), TurnsRemaining: 5},
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
	if resp.Self.LitTorch == nil || resp.Self.LitTorch.TurnsRemaining != 4 {
		t.Errorf("expected LitTorch with TurnsRemaining 4, got %+v", resp.Self.LitTorch)
	}
	if !resp.Self.Lit {
		t.Error("expected Lit to remain true")
	}
	if resp.Outcome.TorchBurnedOut {
		t.Error("expected TorchBurnedOut false before expiry")
	}
}

func TestTickClearsLitTorchAndClearsLitBitAndReportsTorchBurnedOutAtZero(t *testing.T) {
	self, err := json.Marshal(person{
		Object:   overlay.Object{ID: "player:1", Lit: true},
		HP:       10,
		LitTorch: &litTorch{Item: testTorchItemJSON(t, "torch:1", "torch"), TurnsRemaining: 1},
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
	if resp.Self.LitTorch != nil {
		t.Errorf("expected LitTorch cleared, got %+v", resp.Self.LitTorch)
	}
	if resp.Self.Lit {
		t.Error("expected Lit to be cleared")
	}
	if !resp.Outcome.TorchBurnedOut {
		t.Error("expected TorchBurnedOut true at expiry")
	}
}

func TestTickWithNoLitTorchReportsNoBurnout(t *testing.T) {
	self := invSelfJSON(t, overlay.Object{ID: "player:1"}, 10)
	out, err := Tick(self)
	if err != nil {
		t.Fatalf("Tick: %v", err)
	}
	var resp tickResponse
	if err := json.Unmarshal(out, &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Outcome.TorchBurnedOut {
		t.Error("expected TorchBurnedOut false with no lit torch")
	}
}

func TestTickConsumesGraceTurnWithoutDamageOrDecrementing(t *testing.T) {
	self, err := json.Marshal(person{
		Object:                overlay.Object{ID: "player:1", Burning: true},
		HP:                    20,
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
	if resp.Self.HP != 20 {
		t.Errorf("expected HP untouched on the grace turn, got %d", resp.Self.HP)
	}
	if resp.Self.BurningTurnsRemaining != 3 {
		t.Errorf("expected BurningTurnsRemaining left untouched at 3 on the grace turn, got %d", resp.Self.BurningTurnsRemaining)
	}
	if resp.Self.BurningStartsNextTurn {
		t.Error("expected BurningStartsNextTurn cleared after being consumed")
	}
	if !resp.Self.Burning {
		t.Error("expected Burning to remain true through the grace turn")
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
	self, err := json.Marshal(person{
		Object:                overlay.Object{ID: "player:1", Burning: true},
		HP:                    20,
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
	if resp.Self.HP != 20-resp.Outcome.BurnDamage {
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
	self, err := json.Marshal(person{
		Object:                overlay.Object{ID: "player:1", Burning: true},
		HP:                    20,
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
	if resp.Self.BurningTurnsRemaining != 0 {
		t.Errorf("expected BurningTurnsRemaining 0, got %d", resp.Self.BurningTurnsRemaining)
	}
}

func TestTickBurnDamageCanKillThePlayer(t *testing.T) {
	self, err := json.Marshal(person{
		Object:                overlay.Object{ID: "player:1", Burning: true},
		HP:                    1,
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
	self := invSelfJSON(t, overlay.Object{ID: "player:1"}, 10)
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

func TestDropLitTorchClearsSlotReturnsNoItemAndReportsWasLitTorch(t *testing.T) {
	torchJSON := testTorchItemJSON(t, "torch:1", "torch")
	self, err := json.Marshal(person{
		Object:   overlay.Object{ID: "player:1", Lit: true},
		HP:       15,
		LitTorch: &litTorch{Item: torchJSON, TurnsRemaining: 5},
	})
	if err != nil {
		t.Fatalf("marshal self: %v", err)
	}

	out, err := Drop(self, "torch:1")
	if err != nil {
		t.Fatalf("Drop: %v", err)
	}
	var resp dropResponse
	if err := json.Unmarshal(out, &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !resp.WasLitTorch {
		t.Error("expected WasLitTorch=true")
	}
	if resp.WasWielded {
		t.Error("expected WasWielded=false")
	}
	if len(resp.Item) != 0 && string(resp.Item) != "null" {
		t.Errorf("expected no real Item in the response (it disappears), got %s", resp.Item)
	}
	if resp.Self.LitTorch != nil {
		t.Error("expected LitTorch cleared after dropping it")
	}
	if resp.Self.Lit {
		t.Error("expected Lit cleared after dropping the lit torch")
	}
}

func TestDropUnlitTorchFromInventoryBehavesLikeAnyOtherItem(t *testing.T) {
	self := invSelfJSON(t, overlay.Object{ID: "player:1"}, 15, testTorchItemJSON(t, "torch:1", "torch"))

	out, err := Drop(self, "torch:1")
	if err != nil {
		t.Fatalf("Drop: %v", err)
	}
	var resp dropResponse
	if err := json.Unmarshal(out, &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.WasLitTorch {
		t.Error("expected WasLitTorch=false for an unlit torch dropped from Inventory")
	}
	var dropped item.Item
	if err := json.Unmarshal(resp.Item, &dropped); err != nil {
		t.Fatalf("unmarshal dropped item: %v", err)
	}
	if dropped.ID != "torch:1" {
		t.Errorf("expected dropped item ID %q, got %q", "torch:1", dropped.ID)
	}
	if len(resp.Self.Inventory) != 0 {
		t.Errorf("expected Inventory empty after dropping its only item, got %d", len(resp.Self.Inventory))
	}
}
