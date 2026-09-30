package player

import (
	"encoding/json"
	"errors"
	"testing"

	"micromap/server/internal/item"
	"micromap/server/internal/overlay"
)

// invSelfJSON marshals a person with the given position/HP/Inventory, for
// use as PickUp/TakeItem/Inventory/ApplyEffect's selfJSON argument.
// MaxHP is set well above hp so tests unconcerned with healing/clamping
// never trip over it; TestApplyEffectHealRestoresHP sets MaxHP explicitly
// via its own person literals where the cap actually matters.
func invSelfJSON(t *testing.T, obj overlay.Object, hp int, inventory ...json.RawMessage) []byte {
	t.Helper()
	data, err := json.Marshal(person{Object: obj, HP: hp, MaxHP: hp + 1000, Inventory: inventory})
	if err != nil {
		t.Fatalf("marshal self: %v", err)
	}
	return data
}

// testItemJSON marshals a minimal item.Item with the given ID, standing
// in for a floor item's own raw last-returned state.
func testItemJSON(t *testing.T, id, name string) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(item.Item{
		Object:   overlay.Object{ID: id, Glyph: '!'},
		Category: item.CategoryPotion,
		Name:     name,
		Usage:    item.UsageOneShot,
	})
	if err != nil {
		t.Fatalf("marshal item: %v", err)
	}
	return data
}

func TestPickUpAppendsItemToInventory(t *testing.T) {
	self := invSelfJSON(t, overlay.Object{ID: "player:1", X: 5, Y: 5}, 15)
	itemJSON := testItemJSON(t, "potion:1", "healing potion")

	out, err := PickUp(self, itemJSON)
	if err != nil {
		t.Fatalf("PickUp: %v", err)
	}
	inv, err := Inventory(out)
	if err != nil {
		t.Fatalf("Inventory: %v", err)
	}
	if len(inv) != 1 {
		t.Fatalf("expected 1 inventory item, got %d", len(inv))
	}
	var got item.Item
	if err := json.Unmarshal(inv[0], &got); err != nil {
		t.Fatalf("unmarshal inventory item: %v", err)
	}
	if got.ID != "potion:1" {
		t.Errorf("expected inventory item ID %q, got %q", "potion:1", got.ID)
	}
}

func TestPickUpPreservesHPAndPosition(t *testing.T) {
	self := invSelfJSON(t, overlay.Object{ID: "player:1", X: 5, Y: 5}, 15)
	itemJSON := testItemJSON(t, "potion:1", "healing potion")

	out, err := PickUp(self, itemJSON)
	if err != nil {
		t.Fatalf("PickUp: %v", err)
	}
	var got person
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.HP != 15 || got.X != 5 || got.Y != 5 {
		t.Errorf("expected HP/position untouched, got HP=%d (%d,%d)", got.HP, got.X, got.Y)
	}
}

func TestTakeItemRemovesMatchingItemByID(t *testing.T) {
	self := invSelfJSON(t, overlay.Object{ID: "player:1"}, 15,
		testItemJSON(t, "potion:1", "healing potion"),
		testItemJSON(t, "potion:2", "another potion"),
	)

	out, err := TakeItem(self, "potion:1")
	if err != nil {
		t.Fatalf("TakeItem: %v", err)
	}
	inv, err := Inventory(out)
	if err != nil {
		t.Fatalf("Inventory: %v", err)
	}
	if len(inv) != 1 {
		t.Fatalf("expected 1 remaining inventory item, got %d", len(inv))
	}
	var got item.Item
	if err := json.Unmarshal(inv[0], &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.ID != "potion:2" {
		t.Errorf("expected remaining item ID %q, got %q", "potion:2", got.ID)
	}
}

func TestTakeItemErrorsWhenIDNotFound(t *testing.T) {
	self := invSelfJSON(t, overlay.Object{ID: "player:1"}, 15, testItemJSON(t, "potion:1", "healing potion"))
	if _, err := TakeItem(self, "potion:999"); err == nil {
		t.Error("expected an error for a missing item ID, got nil")
	}
}

func TestInventoryReturnsCarriedItemsRaw(t *testing.T) {
	self := invSelfJSON(t, overlay.Object{ID: "player:1"}, 15)
	inv, err := Inventory(self)
	if err != nil {
		t.Fatalf("Inventory: %v", err)
	}
	if len(inv) != 0 {
		t.Fatalf("expected no inventory items, got %d", len(inv))
	}
}

func TestApplyEffectHealRestoresHP(t *testing.T) {
	cases := []struct {
		startHP     int
		maxHP       int
		amount      int
		wantHP      int
		wantApplied int
	}{
		{startHP: 10, maxHP: 100, amount: 5, wantHP: 15, wantApplied: 5},
		{startHP: 1, maxHP: 100, amount: 20, wantHP: 21, wantApplied: 20},
		{startHP: 10, maxHP: 100, amount: 0, wantHP: 10, wantApplied: 0},
	}
	for _, c := range cases {
		self, err := json.Marshal(person{Object: overlay.Object{ID: "player:1"}, HP: c.startHP, MaxHP: c.maxHP})
		if err != nil {
			t.Fatalf("marshal self: %v", err)
		}
		effect, err := json.Marshal(item.Effect{Kind: item.EffectHeal, Amount: c.amount, Source: "potion:1"})
		if err != nil {
			t.Fatalf("marshal effect: %v", err)
		}

		out, err := ApplyEffect(self, effect)
		if err != nil {
			t.Fatalf("ApplyEffect: %v", err)
		}
		var resp effectResponse
		if err := json.Unmarshal(out, &resp); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if resp.Self.HP != c.wantHP {
			t.Errorf("startHP=%d amount=%d: got HP %d, want %d", c.startHP, c.amount, resp.Self.HP, c.wantHP)
		}
		if resp.Outcome.Applied != c.wantApplied {
			t.Errorf("startHP=%d amount=%d: got Applied %d, want %d", c.startHP, c.amount, resp.Outcome.Applied, c.wantApplied)
		}
		if resp.Outcome.RemainingHP != c.wantHP {
			t.Errorf("startHP=%d amount=%d: got RemainingHP %d, want %d", c.startHP, c.amount, resp.Outcome.RemainingHP, c.wantHP)
		}
	}
}

func TestApplyEffectHealClampsToMaxHP(t *testing.T) {
	cases := []struct {
		name        string
		startHP     int
		maxHP       int
		amount      int
		wantHP      int
		wantApplied int
	}{
		{name: "heal would exceed max", startHP: 18, maxHP: 20, amount: 5, wantHP: 20, wantApplied: 2},
		{name: "already at max", startHP: 20, maxHP: 20, amount: 5, wantHP: 20, wantApplied: 0},
		{name: "heal lands exactly on max", startHP: 15, maxHP: 20, amount: 5, wantHP: 20, wantApplied: 5},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			self, err := json.Marshal(person{Object: overlay.Object{ID: "player:1"}, HP: c.startHP, MaxHP: c.maxHP})
			if err != nil {
				t.Fatalf("marshal self: %v", err)
			}
			effect, err := json.Marshal(item.Effect{Kind: item.EffectHeal, Amount: c.amount, Source: "potion:1"})
			if err != nil {
				t.Fatalf("marshal effect: %v", err)
			}

			out, err := ApplyEffect(self, effect)
			if err != nil {
				t.Fatalf("ApplyEffect: %v", err)
			}
			var resp effectResponse
			if err := json.Unmarshal(out, &resp); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if resp.Self.HP != c.wantHP {
				t.Errorf("got HP %d, want %d", resp.Self.HP, c.wantHP)
			}
			if resp.Outcome.Applied != c.wantApplied {
				t.Errorf("got Applied %d, want %d", resp.Outcome.Applied, c.wantApplied)
			}
			if resp.Outcome.RemainingHP != c.wantHP {
				t.Errorf("got RemainingHP %d, want %d", resp.Outcome.RemainingHP, c.wantHP)
			}
		})
	}
}

func TestApplyEffectLeavesPositionAndInventoryUntouched(t *testing.T) {
	self := invSelfJSON(t, overlay.Object{ID: "player:1", X: 3, Y: 4}, 10, testItemJSON(t, "potion:1", "healing potion"))
	effect, err := json.Marshal(item.Effect{Kind: item.EffectHeal, Amount: 5})
	if err != nil {
		t.Fatalf("marshal effect: %v", err)
	}

	out, err := ApplyEffect(self, effect)
	if err != nil {
		t.Fatalf("ApplyEffect: %v", err)
	}
	var resp effectResponse
	if err := json.Unmarshal(out, &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Self.X != 3 || resp.Self.Y != 4 {
		t.Errorf("expected position untouched, got (%d,%d)", resp.Self.X, resp.Self.Y)
	}
	if len(resp.Self.Inventory) != 1 {
		t.Errorf("expected inventory untouched (1 item), got %d", len(resp.Self.Inventory))
	}
}

func TestApplyEffectErrorsOnUnsupportedKind(t *testing.T) {
	self := invSelfJSON(t, overlay.Object{ID: "player:1"}, 10)
	effect, err := json.Marshal(item.Effect{Kind: item.EffectDamageBonus})
	if err != nil {
		t.Fatalf("marshal effect: %v", err)
	}
	if _, err := ApplyEffect(self, effect); err == nil {
		t.Error("expected an error for an unsupported effect kind, got nil")
	}
}

func TestApplyEffectInvisibilitySetsInvisibleAndAppendsActiveEffect(t *testing.T) {
	self := invSelfJSON(t, overlay.Object{ID: "player:1"}, 10)
	effect, err := json.Marshal(item.Effect{Kind: item.EffectInvisibility, Duration: 15, Source: "potion:1"})
	if err != nil {
		t.Fatalf("marshal effect: %v", err)
	}

	out, err := ApplyEffect(self, effect)
	if err != nil {
		t.Fatalf("ApplyEffect: %v", err)
	}
	var resp effectResponse
	if err := json.Unmarshal(out, &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !resp.Self.Invisible {
		t.Error("expected Invisible to be true")
	}
	if len(resp.Self.ActiveEffects) != 1 {
		t.Fatalf("expected 1 active effect, got %d", len(resp.Self.ActiveEffects))
	}
	ae := resp.Self.ActiveEffects[0]
	if ae.TurnsRemaining != 15 {
		t.Errorf("expected TurnsRemaining 15, got %d", ae.TurnsRemaining)
	}
	if ae.Effect.Kind != item.EffectInvisibility {
		t.Errorf("expected active effect kind %q, got %q", item.EffectInvisibility, ae.Effect.Kind)
	}
	if resp.Outcome.Kind != item.EffectInvisibility {
		t.Errorf("expected outcome kind %q, got %q", item.EffectInvisibility, resp.Outcome.Kind)
	}
	if resp.Outcome.Applied != 15 {
		t.Errorf("expected outcome Applied 15, got %d", resp.Outcome.Applied)
	}
	if resp.Outcome.RemainingHP != 0 {
		t.Errorf("expected outcome RemainingHP 0 (no HP involved), got %d", resp.Outcome.RemainingHP)
	}
}

func TestApplyEffectTeleportMovesPlayerToXY(t *testing.T) {
	self := invSelfJSON(t, overlay.Object{ID: "player:1", X: 3, Y: 4}, 10)
	effect, err := json.Marshal(item.Effect{Kind: item.EffectTeleport, X: 17, Y: 22, Source: "scroll:1"})
	if err != nil {
		t.Fatalf("marshal effect: %v", err)
	}

	out, err := ApplyEffect(self, effect)
	if err != nil {
		t.Fatalf("ApplyEffect: %v", err)
	}
	var resp effectResponse
	if err := json.Unmarshal(out, &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Self.X != 17 || resp.Self.Y != 22 {
		t.Errorf("expected position (17,22), got (%d,%d)", resp.Self.X, resp.Self.Y)
	}
	if resp.Outcome.Kind != item.EffectTeleport {
		t.Errorf("expected outcome kind %q, got %q", item.EffectTeleport, resp.Outcome.Kind)
	}
}

func TestApplyEffectTeleportLeavesHPAndInventoryUntouched(t *testing.T) {
	self := invSelfJSON(t, overlay.Object{ID: "player:1", X: 3, Y: 4}, 10, testItemJSON(t, "potion:1", "healing potion"))
	effect, err := json.Marshal(item.Effect{Kind: item.EffectTeleport, X: 8, Y: 8})
	if err != nil {
		t.Fatalf("marshal effect: %v", err)
	}

	out, err := ApplyEffect(self, effect)
	if err != nil {
		t.Fatalf("ApplyEffect: %v", err)
	}
	var resp effectResponse
	if err := json.Unmarshal(out, &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Self.HP != 10 {
		t.Errorf("expected HP untouched at 10, got %d", resp.Self.HP)
	}
	if len(resp.Self.Inventory) != 1 {
		t.Errorf("expected inventory untouched (1 item), got %d", len(resp.Self.Inventory))
	}
}

func TestTickDecrementsActiveEffectsTurnsRemaining(t *testing.T) {
	self, err := json.Marshal(person{
		Object:        overlay.Object{ID: "player:1", Invisible: true},
		HP:            10,
		ActiveEffects: []item.ActiveEffect{{Effect: item.Effect{Kind: item.EffectInvisibility}, TurnsRemaining: 5}},
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
	if len(resp.Self.ActiveEffects) != 1 || resp.Self.ActiveEffects[0].TurnsRemaining != 4 {
		t.Errorf("expected 1 active effect with TurnsRemaining 4, got %+v", resp.Self.ActiveEffects)
	}
	if !resp.Self.Invisible {
		t.Error("expected Invisible to remain true")
	}
	if len(resp.Outcome.Expired) != 0 {
		t.Errorf("expected nothing expired, got %v", resp.Outcome.Expired)
	}
}

func TestTickExpiresActiveEffectAndClearsInvisible(t *testing.T) {
	self, err := json.Marshal(person{
		Object:        overlay.Object{ID: "player:1", Invisible: true},
		HP:            10,
		ActiveEffects: []item.ActiveEffect{{Effect: item.Effect{Kind: item.EffectInvisibility}, TurnsRemaining: 1}},
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
	if len(resp.Self.ActiveEffects) != 0 {
		t.Errorf("expected ActiveEffects empty, got %d", len(resp.Self.ActiveEffects))
	}
	if resp.Self.Invisible {
		t.Error("expected Invisible to be cleared")
	}
	want := []item.EffectKind{item.EffectInvisibility}
	if len(resp.Outcome.Expired) != 1 || resp.Outcome.Expired[0] != want[0] {
		t.Errorf("expected Expired %v, got %v", want, resp.Outcome.Expired)
	}
}

func TestTickKeepsInvisibleWhenOneOfTwoOverlappingEffectsExpires(t *testing.T) {
	self, err := json.Marshal(person{
		Object: overlay.Object{ID: "player:1", Invisible: true},
		HP:     10,
		ActiveEffects: []item.ActiveEffect{
			{Effect: item.Effect{Kind: item.EffectInvisibility}, TurnsRemaining: 1},
			{Effect: item.Effect{Kind: item.EffectInvisibility}, TurnsRemaining: 5},
		},
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
	if len(resp.Self.ActiveEffects) != 1 || resp.Self.ActiveEffects[0].TurnsRemaining != 4 {
		t.Errorf("expected 1 remaining active effect with TurnsRemaining 4, got %+v", resp.Self.ActiveEffects)
	}
	if !resp.Self.Invisible {
		t.Error("expected Invisible to remain true while the second effect is still active")
	}
	if len(resp.Outcome.Expired) != 1 {
		t.Errorf("expected 1 expired entry, got %v", resp.Outcome.Expired)
	}
}

func TestTickLeavesPositionAndInventoryUntouched(t *testing.T) {
	self, err := json.Marshal(person{
		Object:    overlay.Object{ID: "player:1", X: 3, Y: 4},
		HP:        10,
		Inventory: []json.RawMessage{testItemJSON(t, "potion:1", "healing potion")},
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
	if resp.Self.X != 3 || resp.Self.Y != 4 {
		t.Errorf("expected position untouched, got (%d,%d)", resp.Self.X, resp.Self.Y)
	}
	if len(resp.Self.Inventory) != 1 {
		t.Errorf("expected inventory untouched (1 item), got %d", len(resp.Self.Inventory))
	}
}

func TestTickErrorsWithInvalidSelfJSON(t *testing.T) {
	if _, err := Tick([]byte("not json")); err == nil {
		t.Error("expected an error for invalid self JSON, got nil")
	}
}

func TestPickUpRejectsWhenInventoryIsFull(t *testing.T) {
	full := make([]json.RawMessage, item.InventoryCapacity)
	for i := range full {
		full[i] = testItemJSON(t, "potion:filler", "filler potion")
	}
	self := invSelfJSON(t, overlay.Object{ID: "player:1"}, 15, full...)

	out, err := PickUp(self, testItemJSON(t, "potion:new", "new potion"))
	if !errors.Is(err, ErrInventoryFull) {
		t.Fatalf("expected ErrInventoryFull, got %v", err)
	}
	if out != nil {
		t.Errorf("expected nil output on rejection, got %s", out)
	}

	// Original self must be untouched: still exactly InventoryCapacity items.
	inv, err := Inventory(self)
	if err != nil {
		t.Fatalf("Inventory: %v", err)
	}
	if len(inv) != item.InventoryCapacity {
		t.Errorf("expected original Inventory untouched at capacity %d, got %d", item.InventoryCapacity, len(inv))
	}
}

func TestPickUpAcceptsAtOneBelowCapacity(t *testing.T) {
	almostFull := make([]json.RawMessage, item.InventoryCapacity-1)
	for i := range almostFull {
		almostFull[i] = testItemJSON(t, "potion:filler", "filler potion")
	}
	self := invSelfJSON(t, overlay.Object{ID: "player:1"}, 15, almostFull...)

	out, err := PickUp(self, testItemJSON(t, "potion:new", "new potion"))
	if err != nil {
		t.Fatalf("PickUp: %v", err)
	}
	inv, err := Inventory(out)
	if err != nil {
		t.Fatalf("Inventory: %v", err)
	}
	if len(inv) != item.InventoryCapacity {
		t.Errorf("expected Inventory at capacity %d after pickup, got %d", item.InventoryCapacity, len(inv))
	}
}
