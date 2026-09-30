// Package status defines the generic contract intelligent entities
// (player, goblin, mummy, ...) use to describe their own current
// condition for display — an ordered list of label/value pairs, so a
// UI can render "what is this thing right now" without understanding
// any entity-specific fields (HP, Attributes, Oiliness, ...) itself.
// Order matters (unlike a map), which is why this isn't just
// map[string]string.
package status

// Field is one label/value line in an entity's self-description, e.g.
// {"HP", "15/20"} or {"Oily", "x2"}.
type Field struct {
	Label string `json:"label"`
	Value string `json:"value"`
}
