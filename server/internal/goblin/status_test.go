package goblin

import (
	"encoding/json"
	"testing"

	"micromap/server/internal/attributes"
	"micromap/server/internal/item"
	"micromap/server/internal/overlay"
	"micromap/server/internal/status"
)

func decodeStatus(t *testing.T, raw []byte) []status.Field {
	t.Helper()
	var fields []status.Field
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatalf("unmarshal status: %v", err)
	}
	return fields
}

func fieldValue(fields []status.Field, label string) (string, bool) {
	for _, f := range fields {
		if f.Label == label {
			return f.Value, true
		}
	}
	return "", false
}

func TestStatusReportsHPAndAttributes(t *testing.T) {
	self, err := json.Marshal(monster{
		Object:     overlay.Object{ID: "goblin:1", Level: 1},
		HP:         3,
		MaxHP:      5,
		Attributes: attributes.Attributes{Strength: 4, Intelligence: 2, Dexterity: 5, Stamina: 3},
	})
	if err != nil {
		t.Fatalf("marshal self: %v", err)
	}

	raw, err := Status(self)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	fields := decodeStatus(t, raw)

	for label, want := range map[string]string{
		"Level": "1", "HP": "3/5", "Strength": "4", "Intelligence": "2", "Dexterity": "5", "Stamina": "3",
	} {
		got, ok := fieldValue(fields, label)
		if !ok || got != want {
			t.Errorf("expected %q field %q, got %q (present=%v)", label, want, got, ok)
		}
	}
}

func TestStatusOmitsWieldedAndModifiersWhenInactive(t *testing.T) {
	self, err := json.Marshal(monster{Object: overlay.Object{ID: "goblin:1"}, HP: 5, MaxHP: 5})
	if err != nil {
		t.Fatalf("marshal self: %v", err)
	}

	raw, err := Status(self)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	fields := decodeStatus(t, raw)

	for _, label := range []string{"Wielded", "Oily", "Burning"} {
		if _, ok := fieldValue(fields, label); ok {
			t.Errorf("expected no %q field when inactive, found one", label)
		}
	}
}

func TestStatusIncludesWieldedAndActiveModifiers(t *testing.T) {
	self, err := json.Marshal(monster{
		Object: overlay.Object{ID: "goblin:1", Oiliness: 3, Burning: true},
		HP:     5, MaxHP: 5,
		Wielded: &item.WeaponStats{Name: "rusty sword"},
	})
	if err != nil {
		t.Fatalf("marshal self: %v", err)
	}

	raw, err := Status(self)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	fields := decodeStatus(t, raw)

	if got, ok := fieldValue(fields, "Wielded"); !ok || got != "rusty sword" {
		t.Errorf("expected Wielded field \"rusty sword\", got %q (present=%v)", got, ok)
	}
	if got, ok := fieldValue(fields, "Oily"); !ok || got != "x3" {
		t.Errorf("expected Oily field \"x3\", got %q (present=%v)", got, ok)
	}
	if got, ok := fieldValue(fields, "Burning"); !ok || got != "yes" {
		t.Errorf("expected Burning field \"yes\", got %q (present=%v)", got, ok)
	}
}
