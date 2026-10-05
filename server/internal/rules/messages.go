package rules

import (
	"fmt"
	"math/rand"
	"regexp"
	"strings"
)

// AttackMessages are the ways a melee attack can be described, from the
// player's side of it. Hit and Miss are for attacks on the player; YouHit
// and YouMiss for the player's own. One line is chosen at random each
// time. Any list may be empty.
type AttackMessages struct {
	Hit, Miss, YouHit, YouMiss []string
}

// list returns the lines for one case.
func (m AttackMessages) list(youAttack, hit bool) []string {
	switch {
	case youAttack && hit:
		return m.YouHit
	case youAttack:
		return m.YouMiss
	case hit:
		return m.Hit
	default:
		return m.Miss
	}
}

// Placeholders are everything an attack message may contain, besides
// plain text. A capitalized placeholder gives a capitalized result, for
// the start of a sentence.
var Placeholders = []string{
	"{the attacker}", "{The attacker}", "{the attacker's}", "{The attacker's}",
	"{the defender}", "{The defender}", "{the defender's}", "{The defender's}",
	"{his}", "{His}", "{he}", "{He}",
	"{damage}", "{hp_left}", "{weapon}",
}

var placeholderPattern = regexp.MustCompile(`\{[^{}]*\}`)

// CheckMessage reports the first thing in line that is not a placeholder
// an attack message may use.
func CheckMessage(line string) error {
	if strings.TrimSpace(line) == "" {
		return fmt.Errorf("empty message")
	}
	for _, p := range placeholderPattern.FindAllString(line, -1) {
		known := false
		for _, k := range Placeholders {
			known = known || p == k
		}
		if !known {
			return fmt.Errorf("unknown placeholder %s in %q (known: %s)", p, line, strings.Join(Placeholders, " "))
		}
	}
	return nil
}

// describe says what a melee attack did, as the first event of the
// attack. Its words come from the first of these with a line for the
// case: the weapon's messages, the attacker's own, a built-in default. A
// custom line that kills also gets "The goblin dies!" or "You die!".
func (c *Ctx) describe(a *Attack, out Outcome) {
	youAttack, youDefend := a.Attacker.IsPlayer(), a.Defender.IsPlayer()
	if !youAttack && !youDefend {
		// Neither side is the player; nothing to say in their words.
		c.Say(a.Attacker, "", defaultLine(a, out))
		return
	}
	if !out.Missed && out.Damage == 0 {
		// It landed, but armor (or a curse on the blade) took it all.
		text := capitalize(the(a.Attacker)) + " hits " + the(a.Defender) + ", but the blow glances off."
		if youAttack {
			text = "You hit " + the(a.Defender) + ", but the blow glances off."
		}
		c.Say(a.Defender, text, text)
		return
	}
	var lines []string
	if a.Weapon != nil {
		lines = a.Weapon.T.Messages.list(youAttack, !out.Missed)
	}
	if len(lines) == 0 {
		lines = a.Attacker.T.Messages.list(youAttack, !out.Missed)
	}
	text := defaultLine(a, out)
	if len(lines) > 0 {
		text = fill(lines[rand.Intn(len(lines))], a, out)
		if out.Dead && youDefend {
			text += " You die!"
		} else if out.Dead {
			text += " " + capitalize(the(a.Defender)) + " dies!"
		}
	}
	c.Say(a.Defender, text, text)
}

// defaultLine is what an attack says when nothing more colorful is given
// — the game's lines from before attacks could be described.
func defaultLine(a *Attack, out Outcome) string {
	switch {
	case out.Missed && a.Attacker.IsPlayer():
		return "You miss " + the(a.Defender) + "."
	case out.Missed:
		return capitalize(the(a.Attacker)) + " misses " + the(a.Defender) + "."
	case a.Defender.IsPlayer() && out.Dead:
		return fmt.Sprintf("You take %d damage and die", out.Damage)
	case a.Defender.IsPlayer():
		return fmt.Sprintf("You take %d damage (%d HP left)", out.Damage, out.RemainingHP)
	case out.Dead:
		return fmt.Sprintf("%s takes %d damage and dies", a.Defender.Name, out.Damage)
	default:
		return fmt.Sprintf("%s takes %d damage (%d HP left)", a.Defender.Name, out.Damage, out.RemainingHP)
	}
}

// fill replaces every placeholder in line.
func fill(line string, a *Attack, out Outcome) string {
	he, his := pronouns(a.Attacker)
	values := map[string]string{
		"{the attacker}":   the(a.Attacker),
		"{the attacker's}": possessive(a.Attacker),
		"{the defender}":   the(a.Defender),
		"{the defender's}": possessive(a.Defender),
		"{he}":             he,
		"{his}":            his,
		"{damage}":         fmt.Sprint(out.Damage),
		"{hp_left}":        fmt.Sprint(out.RemainingHP),
		"{weapon}":         a.Name,
	}
	return placeholderPattern.ReplaceAllStringFunc(line, func(p string) string {
		if v, ok := values[p]; ok {
			return v
		}
		if v, ok := values[strings.ToLower(p[:2])+p[2:]]; ok {
			return capitalize(v)
		}
		return p
	})
}

// the is how a sentence names e: "you", or "the goblin".
func the(e *Entity) string {
	if e.IsPlayer() {
		return "you"
	}
	return "the " + strings.ToLower(e.Name)
}

// possessive is "your", or "the goblin's".
func possessive(e *Entity) string {
	if e.IsPlayer() {
		return "your"
	}
	return the(e) + "'s"
}

// pronouns are e's subject and possessive pronouns.
func pronouns(e *Entity) (he, his string) {
	if e.IsPlayer() {
		return "you", "your"
	}
	switch e.T.Pronoun {
	case "he":
		return "he", "his"
	case "she":
		return "she", "her"
	case "they":
		return "they", "their"
	}
	return "it", "its"
}

// Pronouns lists the values a template's pronoun may take.
var Pronouns = []string{"he", "she", "it", "they"}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
