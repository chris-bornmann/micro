// Turn text. The one sentence the coordinator still composes itself — a
// thrown item's hit — is built here; everything else, melee included,
// arrives already phrased from the rules engine.
package game

import (
	"fmt"

	"micro/server/internal/rules"
)

// defendMessage reports what a thrown item did to the monster it hit.
func defendMessage(who string, o rules.Outcome) string {
	if o.Dead {
		return fmt.Sprintf("%s takes %d damage and dies", who, o.Damage)
	}
	return fmt.Sprintf("%s takes %d damage (%d HP left)", who, o.Damage, o.RemainingHP)
}

// messagesOf wraps a single possibly-empty status string as a []string —
// nil (not [""]) when msg is empty, so joinMessages doesn't need to
// special-case a lone empty entry.
func messagesOf(msg string) []string {
	if msg == "" {
		return nil
	}
	return []string{msg}
}

// joinMessages combines every non-empty message with " | ".
func joinMessages(messages []string) string {
	combined := ""
	for _, m := range messages {
		if m == "" {
			continue
		}
		if combined != "" {
			combined += " | "
		}
		combined += m
	}
	return combined
}
