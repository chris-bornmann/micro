package rules

import (
	"fmt"

	"micro/server/internal/status"
)

// Status describes e for the status view and HUD: level, HP and
// attributes, then one line per distinct thing its traits and gear report
// about it (Oily, Burning, Invisible, ...), each label once however many
// instances contribute to it.
func (reg *Registry) Status(e *Entity) []status.Field {
	fields := []status.Field{
		{Label: "Level", Value: fmt.Sprintf("%d", e.Level)},
		{Label: "HP", Value: fmt.Sprintf("%d/%d", e.HP, e.MaxHP)},
		{Label: "Strength", Value: fmt.Sprintf("%d", e.Attrs.Strength)},
		{Label: "Intelligence", Value: fmt.Sprintf("%d", e.Attrs.Intelligence)},
		{Label: "Dexterity", Value: fmt.Sprintf("%d", e.Attrs.Dexterity)},
		{Label: "Stamina", Value: fmt.Sprintf("%d", e.Attrs.Stamina)},
		{Label: "Healing", Value: reg.healingRate(e)},
	}
	seen := map[string]bool{}
	for _, ht := range reg.holders(ownParticipants(e)) {
		d, ok := ht.t.(Describer)
		if !ok {
			continue
		}
		label, value, ok := d.Describe(ht.h)
		if !ok || seen[label] {
			continue
		}
		seen[label] = true
		fields = append(fields, status.Field{Label: label, Value: value})
	}
	return fields
}
