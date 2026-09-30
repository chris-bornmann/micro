package player

import (
	"encoding/json"
	"testing"

	"micromap/server/internal/attributes"
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
	self, err := json.Marshal(person{
		Object:     overlay.Object{ID: "player:1", Level: 3},
		HP:         12,
		MaxHP:      20,
		Attributes: attributes.Attributes{Strength: 10, Intelligence: 8, Dexterity: 12, Stamina: 9},
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
		"Level": "3", "HP": "12/20", "Strength": "10", "Intelligence": "8", "Dexterity": "12", "Stamina": "9",
	} {
		got, ok := fieldValue(fields, label)
		if !ok {
			t.Errorf("expected a %q field, found none", label)
			continue
		}
		if got != want {
			t.Errorf("expected %q field %q, got %q", label, want, got)
		}
	}
}

func TestStatusOmitsModifiersWhenInactive(t *testing.T) {
	self, err := json.Marshal(person{Object: overlay.Object{ID: "player:1"}, HP: 10, MaxHP: 10})
	if err != nil {
		t.Fatalf("marshal self: %v", err)
	}

	raw, err := Status(self)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	fields := decodeStatus(t, raw)

	for _, label := range []string{"Oily", "Burning", "Invisible", "Sneaking"} {
		if _, ok := fieldValue(fields, label); ok {
			t.Errorf("expected no %q field when inactive, found one", label)
		}
	}
}

func TestStatusIncludesActiveModifiers(t *testing.T) {
	self, err := json.Marshal(person{
		Object: overlay.Object{ID: "player:1", Oiliness: 2, Burning: true, Invisible: true, Sneaking: true},
		HP:     10, MaxHP: 10,
	})
	if err != nil {
		t.Fatalf("marshal self: %v", err)
	}

	raw, err := Status(self)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	fields := decodeStatus(t, raw)

	if got, ok := fieldValue(fields, "Oily"); !ok || got != "x2" {
		t.Errorf("expected Oily field \"x2\", got %q (present=%v)", got, ok)
	}
	for _, label := range []string{"Burning", "Invisible", "Sneaking"} {
		if got, ok := fieldValue(fields, label); !ok || got != "yes" {
			t.Errorf("expected %q field \"yes\", got %q (present=%v)", label, got, ok)
		}
	}
}
