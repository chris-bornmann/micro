// Package builtin assembles the registry of every built-in behavior: the
// traits, AIs and actions that ship with the game.
package builtin

import (
	"micro/server/internal/rules"
	"micro/server/internal/rules/actions"
	"micro/server/internal/rules/ai"
	"micro/server/internal/rules/traits"
)

// Registry returns a new registry holding every built-in behavior.
func Registry() *rules.Registry {
	reg := rules.NewRegistry()
	traits.Register(reg)
	ai.Register(reg)
	actions.Register(reg)
	return reg
}
