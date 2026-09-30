// Turn text. Every player-visible sentence the coordinator itself
// composes is built here, out of the outcome values services report.
package main

import (
	"fmt"

	"map_api/action"
	"map_api/item"
)

// defendMessage returns a short status string for a monster's Defend
// outcome.
func defendMessage(who string, o action.DefendOutcome) string {
	if o.Dead {
		return fmt.Sprintf("%s takes %d damage and dies", who, o.Damage)
	}
	return fmt.Sprintf("%s takes %d damage (%d HP left)", who, o.Damage, o.RemainingHP)
}

// playerDefendMessage returns a short status string for the Player's own
// Defend outcome — a separate function from defendMessage rather than a
// "You" argument to it, since "You takes damage" doesn't read right.
func playerDefendMessage(o action.DefendOutcome) string {
	if o.Dead {
		return fmt.Sprintf("You take %d damage and die", o.Damage)
	}
	return fmt.Sprintf("You take %d damage (%d HP left)", o.Damage, o.RemainingHP)
}

// effectMessage returns a short status string for an ApplyEffect outcome,
// following defendMessage's style.
func effectMessage(itemName string, o item.EffectOutcome) string {
	switch o.Kind {
	case item.EffectHeal:
		return fmt.Sprintf("You drink the %s and recover %d HP (%d HP now).", itemName, o.Applied, o.RemainingHP)
	case item.EffectInvisibility:
		return fmt.Sprintf("You drink the %s and turn invisible for %d turns.", itemName, o.Applied)
	case item.EffectTeleport:
		return fmt.Sprintf("You read the %s and are teleported away!", itemName)
	default:
		return fmt.Sprintf("You use the %s.", itemName)
	}
}

// tickMessage returns a short status string for a Tick outcome's expired
// effects, a burned-out torch, and/or burn damage, following
// defendMessage/effectMessage's style. Returns "" when nothing expired,
// no torch burned out, and no burn damage was dealt — the common case —
// so finishTurn only appends a message when there's actually something
// to report.
func tickMessage(outcome item.TickOutcome) string {
	var msgs []string
	for _, k := range outcome.Expired {
		switch k {
		case item.EffectInvisibility:
			msgs = append(msgs, "Your invisibility wears off.")
		default:
			msgs = append(msgs, fmt.Sprintf("Your %s effect wears off.", k))
		}
	}
	if outcome.TorchBurnedOut {
		msgs = append(msgs, "Your torch burns out.")
	}
	if outcome.BurnDamage > 0 {
		if outcome.BurnedOut {
			msgs = append(msgs, fmt.Sprintf("You take %d fire damage and the flames die out.", outcome.BurnDamage))
		} else {
			msgs = append(msgs, fmt.Sprintf("You take %d fire damage from burning.", outcome.BurnDamage))
		}
	}
	return joinMessages(msgs)
}

// attackGoblin sends attackJSON (a JSON-encoded action.Result, as
// messagesOf wraps a single possibly-empty status string as a []string
// suitable for finishTurn — nil (not [""]) when msg is empty, so
// joinMessages doesn't need to special-case a lone empty entry.
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
