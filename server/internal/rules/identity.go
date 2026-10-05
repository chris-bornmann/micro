package rules

// Unidentified items. Most items show only how they look — "an agate
// ring", "a bubbly potion", "a notched dagger" — until their type is
// identified. Every copy of a type looks the same for the whole game, and
// identifying one identifies them all. Which look goes with which type is
// shuffled for each game, so a jade ring is not always the same ring.
//
// The looks come in pools (appearances.yaml): a pool has a format ("{}
// ring") and a list of looks, each with a color, so an unknown item is
// drawn in its look's color rather than its type's own. An item template
// names its pool, and the percent chance its type starts a game unknown
// (Template.Unidentified).

import (
	"math/rand"
	"strings"

	"micro/server/internal/overlay"
)

// Unidentified says how an item type can go unrecognized: which pool its
// look comes from, and the percent chance a game starts with it unknown.
type Unidentified struct {
	Pool   string
	Chance int
}

// Pool is a set of looks for one kind of item.
type Pool struct {
	Name string
	// Format is how a look is named: "{}" is replaced by the look's name
	// ("{} ring" → "agate ring").
	Format string
	Looks  []PoolLook
}

// PoolLook is one look: its name, and the color it is drawn in.
type PoolLook struct {
	Name  string
	Color overlay.Color
}

// look is a look as a game assigns it: fully named ("agate ring").
type look struct {
	name  string
	color overlay.Color
}

// Identity is, for one game, every item type the player does not yet
// recognize, and how each looks. A nil Identity recognizes everything.
type Identity struct {
	unknown map[string]look
}

// NewIdentity deals out looks for a new game: each pool's looks shuffled
// with rng and handed, in turn, to the item types that use it (in ID
// order, so a seed always deals the same). Each type then rolls its
// chance of starting unknown; one that starts known keeps nothing.
func NewIdentity(cat *Catalog, rng *rand.Rand) *Identity {
	id := &Identity{unknown: map[string]look{}}
	for _, name := range cat.PoolNames() {
		p := cat.Pools[name]
		looks := append([]PoolLook(nil), p.Looks...)
		rng.Shuffle(len(looks), func(i, j int) { looks[i], looks[j] = looks[j], looks[i] })
		next := 0
		for _, t := range cat.All() {
			if t.Unidentified == nil || t.Unidentified.Pool != name {
				continue
			}
			l := looks[next]
			next++
			if rng.Intn(100) < t.Unidentified.Chance {
				id.unknown[t.ID] = look{name: strings.ReplaceAll(p.Format, "{}", l.Name), color: l.Color}
			}
		}
	}
	return id
}

// Identify makes the type with template ID tmplID known: every copy shows
// its true name from now on.
func (id *Identity) Identify(tmplID string) {
	if id != nil {
		delete(id.unknown, tmplID)
	}
}

// Known reports whether the type with template ID tmplID is recognized.
func (id *Identity) Known(tmplID string) bool {
	if id == nil {
		return true
	}
	_, unknown := id.unknown[tmplID]
	return !unknown
}

// Name is what e is called: its look while its type is unknown, its own
// name otherwise.
func (id *Identity) Name(e *Entity) string {
	if id != nil {
		if l, ok := id.unknown[e.T.ID]; ok {
			return l.name
		}
	}
	return e.Name
}

// Color is what color e is drawn in: its look's while its type is
// unknown, its template's otherwise.
func (id *Identity) Color(e *Entity) overlay.Color {
	if id != nil {
		if l, ok := id.unknown[e.T.ID]; ok {
			return l.color
		}
	}
	return e.T.Color
}

// Name is what the player calls e (see Identity.Name).
func (c *Ctx) Name(e *Entity) string { return c.World.Identity.Name(e) }

// A is e's name with "a" or "an" in front: "an agate ring".
func (c *Ctx) A(e *Entity) string { return A(c.Name(e)) }

// A puts "a" or "an" in front of name, by its first letter.
func A(name string) string {
	if name != "" && strings.ContainsRune("aeiouAEIOU", rune(name[0])) {
		return "an " + name
	}
	return "a " + name
}
