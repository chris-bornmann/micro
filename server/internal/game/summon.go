// Summoning: World.Summon, which a scroll of summon monster asks for. The
// game does it rather than the rules because a unique character may be
// anywhere in the run — elsewhere on this level, on another level, or in
// the roster of those who left — and only the game knows all of that.
package game

import (
	"math/rand"

	"micro/server/internal/mapdata"
	"micro/server/internal/rules"
)

// summon brings a creature of template t into the Player's room. An
// ordinary monster is made new, as it would spawn. A unique character is
// the only one there is: taken from wherever it is, alive, with everything
// it carries and remembers — or, if it has never appeared, made now, and
// then it never appears by itself. A dead one cannot come.
func (state *GameState) summon(t *rules.Template) (*rules.Entity, bool) {
	var e *rules.Entity
	if t.Unique {
		var dead bool
		if e, dead = state.takeUnique(t); dead {
			return nil, false
		}
	}
	if e == nil {
		e = state.eng.Create(t, 0, 0, rand.New(rand.NewSource(rand.Int63())), nil)
		if state.SpawnedOnce == nil {
			state.SpawnedOnce = map[string]bool{}
		}
		state.SpawnedOnce[t.ID] = true
	}
	e.X, e.Y = state.summoningCell()
	// Wherever it was going, it is somewhere else now.
	e.Mind.Fleeing, e.Mind.FleeRoom, e.Mind.Aware = false, -1, false
	state.Monsters = append(state.Monsters, e)
	return e, true
}

// takeUnique finds the one creature of unique template t, on any level
// made so far or in the roster, and takes it from there. dead is true if
// it has died; e is nil if it has never appeared.
func (state *GameState) takeUnique(t *rules.Template) (e *rules.Entity, dead bool) {
	for _, ls := range state.Levels {
		if ls == nil {
			continue
		}
		for i, m := range ls.Monsters {
			if m.T.ID != t.ID {
				continue
			}
			if m.Dead {
				return nil, true
			}
			ls.Monsters = append(ls.Monsters[:i], ls.Monsters[i+1:]...)
			return m, false
		}
	}
	for i, r := range state.Roster {
		if r.Who.T.ID == t.ID {
			state.Roster = append(state.Roster[:i], state.Roster[i+1:]...)
			return r.Who, false
		}
	}
	return nil, false
}

// summoningCell is a random free cell in the Player's room — or, with the
// Player out of any room or the room full, the free tile nearest them.
func (state *GameState) summoningCell() (int, int) {
	w, p := state.world(), state.Player
	if room, ok := state.Map.RoomAt(p.X, p.Y); ok {
		var free []mapdata.Point
		for y := room.Y; y < room.Y+room.H; y++ {
			for x := room.X; x < room.X+room.W; x++ {
				if w.CreatureAt(x, y) == nil {
					free = append(free, mapdata.Point{X: x, Y: y})
				}
			}
		}
		if len(free) > 0 {
			c := free[rand.Intn(len(free))]
			return c.X, c.Y
		}
	}
	x, y, _ := w.NearestFreeTile(p.X, p.Y, nil)
	return x, y
}
