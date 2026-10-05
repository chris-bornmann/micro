package rules

import (
	"fmt"
	"math/rand"

	"micro/server/internal/mapdata"
)

// Engine is the rules engine: the behaviors it can run and the templates
// it can create entities from.
type Engine struct {
	Reg *Registry
	Cat *Catalog
	// HitRoll, if set, replaces the percentile to-hit roll (0-99; a blow
	// lands when the roll is below the hit chance). Tests use it to make
	// every blow land, or none.
	HitRoll func() int
}

// World is the part of the game state the rules act on for one level: the
// map, who is on it, and the oil spilled across it. The engine mutates the
// entities it is handed in place.
type World struct {
	Map    *mapdata.Map
	Player *Entity
	// Monsters includes dead ones, which the engine skips.
	Monsters []*Entity
	// Oily is every floor tile coated in spilled oil. Nil until the first
	// spill.
	Oily map[mapdata.Point]bool
	// Floor is everything lying on the level's floor: items, and fixed
	// features such as stairs and traps, whose traits may react to a
	// creature stepping onto their cell (see Enterer).
	Floor []*Entity
	// Depth is the level's depth: 1 at the top, DeepestLevel at the bottom.
	Depth int
	// Visible reports whether the player can see (x, y) right now — the
	// same tiles the frame draws things on. The game fills it in; nil
	// means everything is in sight.
	Visible func(x, y int) bool
	// Summon brings a creature of template t to the player's side, for a
	// summoning — the game's job, since a unique character may be anywhere:
	// elsewhere on this level, on another, or gone from the dungeon for
	// now. It reports false if t cannot come: a unique character who is
	// dead. Nil means nothing can be summoned.
	Summon func(t *Template) (*Entity, bool)
	// Identity is which item types the player does not recognize yet, and
	// how they look. Nil recognizes everything.
	Identity *Identity
	// Turn numbers the turn being played: everything that happens in one
	// turn — the player's action, every trait's tick, every monster's
	// move — sees the same number.
	Turn int
}

// NextTurn is the number of the turn after this one: when an attack's
// lingering effect — poison, fire — starts to act (see
// TraitInstance.From).
func (c *Ctx) NextTurn() int { return c.World.Turn + 1 }

// PlayerSees reports whether the player can see (x, y) right now.
func (w *World) PlayerSees(x, y int) bool { return w.Visible == nil || w.Visible(x, y) }

// DeepestLevel is how deep the dungeon goes.
const DeepestLevel = 25

// LiveMonsters returns every monster in w that is not dead.
func (w *World) LiveMonsters() []*Entity {
	var out []*Entity
	for _, m := range w.Monsters {
		if !m.Dead {
			out = append(out, m)
		}
	}
	return out
}

// obstacles is what a thrown item can hit: every live monster, for the
// player; the player, for a monster.
func (w *World) obstacles(actor *Entity) []*Entity {
	if actor.IsPlayer() {
		return w.LiveMonsters()
	}
	if w.Player == nil {
		return nil
	}
	return []*Entity{w.Player}
}

// creatures is every live creature on the level: the player and every
// monster that is not dead. No two of them ever share a tile.
func (w *World) creatures() []*Entity {
	out := w.LiveMonsters()
	if w.Player != nil && !w.Player.Dead {
		out = append(out, w.Player)
	}
	return out
}

// CreatureAt returns the live creature on (x, y), if any.
func (w *World) CreatureAt(x, y int) *Entity {
	for _, e := range w.creatures() {
		if e.X == x && e.Y == y {
			return e
		}
	}
	return nil
}

// NearestFreeTile is the walkable tile closest to (x, y) that no live
// creature other than except stands on — (x, y) itself if it qualifies.
// Ties go to the first in map order, so the answer is always the same.
func (w *World) NearestFreeTile(x, y int, except *Entity) (int, int, bool) {
	best, bestDist := mapdata.Point{}, -1
	for _, p := range w.Map.WalkableTiles() {
		if c := w.CreatureAt(p.X, p.Y); c != nil && c != except {
			continue
		}
		d := abs(p.X-x) + abs(p.Y-y)
		if bestDist == -1 || d < bestDist {
			best, bestDist = p, d
		}
	}
	return best.X, best.Y, bestDist != -1
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// SpillOil marks (x, y) as oily.
func (w *World) SpillOil(x, y int) {
	if w.Oily == nil {
		w.Oily = map[mapdata.Point]bool{}
	}
	w.Oily[mapdata.Point{X: x, Y: y}] = true
}

// IsOily reports whether (x, y) is coated in oil.
func (w *World) IsOily(x, y int) bool { return w.Oily[mapdata.Point{X: x, Y: y}] }

// RandomUnoccupiedTile picks a uniformly random walkable tile that neither
// the player nor a live monster stands on. Floor items never block. It
// uses the global source: this is a turn-time decision, and only spawn-time
// placement is seeded. ok is false only if no such tile exists.
func (w *World) RandomUnoccupiedTile() (x, y int, ok bool) {
	occupied := map[mapdata.Point]bool{}
	if w.Player != nil {
		occupied[mapdata.Point{X: w.Player.X, Y: w.Player.Y}] = true
	}
	for _, m := range w.LiveMonsters() {
		occupied[mapdata.Point{X: m.X, Y: m.Y}] = true
	}
	var candidates []mapdata.Point
	for _, p := range w.Map.WalkableTiles() {
		if !occupied[p] {
			candidates = append(candidates, p)
		}
	}
	if len(candidates) == 0 {
		return 0, 0, false
	}
	p := candidates[rand.Intn(len(candidates))]
	return p.X, p.Y, true
}

// Event is something that happened that the player may be told about,
// already phrased both ways: You for when Subject is the player, They for
// anyone else.
type Event struct {
	Subject *Entity
	You     string
	They    string
	// Damage and Type are set on an event that reports damage.
	Damage int
	Type   DamageType
}

// Ctx is one resolution in progress: the engine, the world it acts on, and
// the events produced so far. Hooks receive it to act on the world.
type Ctx struct {
	*Engine
	World  *World
	Events []Event
	// Descend is a request to the game, which alone can change level: the
	// player is to fall to the level below (a trap door).
	Descend bool
	// Departed asks the game to take these creatures off the level, alive
	// and carrying everything they had: a unique character escaping or
	// teleporting away. See Depart.
	Departed []*Entity
}

// Depart asks for e to leave the level alive (see Ctx.Departed).
func (c *Ctx) Depart(e *Entity) {
	c.Departed = append(c.Departed, e)
}

// NewCtx starts a resolution against w.
func (eng *Engine) NewCtx(w *World) *Ctx { return &Ctx{Engine: eng, World: w} }

// Say records an event about subject, phrased for the player (you) and for
// anyone else (they).
func (c *Ctx) Say(subject *Entity, you, they string) {
	c.Events = append(c.Events, Event{Subject: subject, You: you, They: they})
}

// Sayf is Say for the common shape where the only difference is the
// subject: format has one leading %s that becomes "You" or the subject's
// name, and verbs are given both ways, e.g.
// Sayf(e, "%s %s fire damage", "take", "takes").
func (c *Ctx) Sayf(subject *Entity, youVerb, theyVerb, format string, args ...any) {
	you := fmt.Sprintf(format, append([]any{"You", youVerb}, args...)...)
	they := fmt.Sprintf(format, append([]any{subject.Name, theyVerb}, args...)...)
	c.Say(subject, you, they)
}

// Messages returns the text of every event so far, from the player's
// point of view.
func (c *Ctx) Messages() []string {
	var out []string
	for _, ev := range c.Events {
		if ev.Subject != nil && ev.Subject.IsPlayer() {
			out = append(out, ev.You)
		} else {
			out = append(out, ev.They)
		}
	}
	return out
}
