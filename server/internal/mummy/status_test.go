package mummy

import (
	"encoding/json"
	"testing"

	"micro/server/internal/attributes"
	"micro/server/internal/overlay"
	"micro/server/internal/status"
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
	self, err := json.Marshal(creature{
		Object:     overlay.Object{ID: "mummy:1", Level: 5},
		HP:         22,
		MaxHP:      25,
		Attributes: attributes.Attributes{Strength: 6, Intelligence: 5, Dexterity: 7, Stamina: 8},
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
		"Level": "5", "HP": "22/25", "Strength": "6", "Intelligence": "5", "Dexterity": "7", "Stamina": "8",
	} {
		got, ok := fieldValue(fields, label)
		if !ok || got != want {
			t.Errorf("expected %q field %q, got %q (present=%v)", label, want, got, ok)
		}
	}
}

func TestStatusHasNoWieldedFieldAndOmitsModifiersWhenInactive(t *testing.T) {
	self, err := json.Marshal(creature{Object: overlay.Object{ID: "mummy:1"}, HP: 25, MaxHP: 25})
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
			t.Errorf("expected no %q field (a mummy never wields anything, and this one has no active modifiers), found one", label)
		}
	}
}

func TestStatusIncludesActiveModifiers(t *testing.T) {
	self, err := json.Marshal(creature{
		Object: overlay.Object{ID: "mummy:1", Oiliness: 1, Burning: true},
		HP:     25, MaxHP: 25,
	})
	if err != nil {
		t.Fatalf("marshal self: %v", err)
	}

	raw, err := Status(self)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	fields := decodeStatus(t, raw)

	if got, ok := fieldValue(fields, "Oily"); !ok || got != "x1" {
		t.Errorf("expected Oily field \"x1\", got %q (present=%v)", got, ok)
	}
	if got, ok := fieldValue(fields, "Burning"); !ok || got != "yes" {
		t.Errorf("expected Burning field \"yes\", got %q (present=%v)", got, ok)
	}
}
