package item

import (
	"encoding/json"
	"testing"

	"micromap/server/internal/overlay"
)

// TestItemMarshalsEmbeddedObjectFieldsFlat pins the deliberate choice to
// follow overlay.Object's bare-field convention: Item's embedded Object
// fields must appear as top-level keys, not nested under "Object", since
// every category package merges Item's fields with its own into one flat
// JSON blob (exactly how player.person merges overlay.Object with HP).
func TestItemMarshalsEmbeddedObjectFieldsFlat(t *testing.T) {
	it := Item{
		Object:   overlay.Object{ID: "potion:1", X: 3, Y: 4, Glyph: '!'},
		Category: CategoryPotion,
		Name:     "healing potion",
		Usage:    UsageOneShot,
	}
	data, err := json.Marshal(it)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal into map: %v", err)
	}
	for _, key := range []string{"ID", "X", "Y", "Glyph", "Category", "Name", "Usage"} {
		if _, ok := raw[key]; !ok {
			t.Errorf("expected top-level key %q in %s, got %v", key, data, raw)
		}
	}
	if _, ok := raw["Object"]; ok {
		t.Errorf("expected Object to be flattened, not nested, got %s", data)
	}
}

// TestEffectUsesSnakeCaseTags pins Effect's deliberate choice to follow
// action.Result/DefendOutcome's snake_case convention rather than Item's
// bare-field one, since an Effect is always its own standalone JSON value
// inside an envelope, never merged flat into something else.
func TestEffectUsesSnakeCaseTags(t *testing.T) {
	e := Effect{Kind: EffectHeal, Amount: 10, Duration: 3, Source: "potion:1"}
	data, err := json.Marshal(e)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal into map: %v", err)
	}
	for _, key := range []string{"kind", "amount", "duration", "source"} {
		if _, ok := raw[key]; !ok {
			t.Errorf("expected snake_case key %q in %s", key, data)
		}
	}
}

// TestEffectOutcomeUsesSnakeCaseTags is EffectUsesSnakeCaseTags's
// counterpart for EffectOutcome.
func TestEffectOutcomeUsesSnakeCaseTags(t *testing.T) {
	o := EffectOutcome{Kind: EffectHeal, Applied: 10, RemainingHP: 18}
	data, err := json.Marshal(o)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal into map: %v", err)
	}
	for _, key := range []string{"kind", "applied", "remaining_hp"} {
		if _, ok := raw[key]; !ok {
			t.Errorf("expected snake_case key %q in %s", key, data)
		}
	}
}

// TestWeaponStatsUsesSnakeCaseTags pins WeaponStats's choice to follow
// Effect/EffectOutcome's snake_case convention, for the same reason: it's
// always its own nested value (an entry on an entity's Wielded field),
// never flattened into something else.
func TestWeaponStatsUsesSnakeCaseTags(t *testing.T) {
	s := WeaponStats{Name: "fine dagger", Source: "weapon:1", BaseDamageMax: 4, Enchantment: 1, VsKind: map[string]int{"goblin": 1}}
	data, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal into map: %v", err)
	}
	for _, key := range []string{"name", "source", "base_damage_max", "enchantment", "vs_kind"} {
		if _, ok := raw[key]; !ok {
			t.Errorf("expected snake_case key %q in %s", key, data)
		}
	}
}

func TestWeaponStatsRangeAppliesEnchantmentAndVsKind(t *testing.T) {
	cases := []struct {
		name             string
		stats            WeaponStats
		targetKind       string
		wantMin, wantMax int
	}{
		{"plain weapon, no matchup", WeaponStats{BaseDamageMax: 4}, "goblin", 1, 4},
		{"enchanted +1", WeaponStats{BaseDamageMax: 4, Enchantment: 1}, "goblin", 2, 5},
		{"cursed -1", WeaponStats{BaseDamageMax: 6, Enchantment: -1}, "goblin", 0, 5},
		{"vs-kind bonus stacks with enchantment", WeaponStats{BaseDamageMax: 4, Enchantment: 1, VsKind: map[string]int{"goblin": 1}}, "goblin", 3, 6},
		{"vs-kind bonus doesn't apply to non-matching kind", WeaponStats{BaseDamageMax: 4, Enchantment: 1, VsKind: map[string]int{"goblin": 1}}, "player", 2, 5},
		{"deeply cursed clamps min to 0 not negative", WeaponStats{BaseDamageMax: 2, Enchantment: -5}, "", 0, -3},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			gotMin, gotMax := c.stats.Range(c.targetKind)
			if gotMin != c.wantMin || gotMax != c.wantMax {
				t.Errorf("Range(%q) = (%d,%d), want (%d,%d)", c.targetKind, gotMin, gotMax, c.wantMin, c.wantMax)
			}
		})
	}
}

func TestInventoryCapacityIsTen(t *testing.T) {
	if InventoryCapacity != 10 {
		t.Errorf("expected InventoryCapacity 10, got %d", InventoryCapacity)
	}
}
