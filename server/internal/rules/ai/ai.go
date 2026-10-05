// Package ai holds the built-in monster AI behaviors a template's ai:
// block names. An AI only decides where a monster wants to step each turn;
// moving, sliding on oil and attacking whoever is in the way are the
// engine's (see rules.Ctx.MonsterTurn), so every monster fights the same
// way the player does.
package ai

import (
	"math/rand"

	"micro/server/internal/mapdata"
	"micro/server/internal/rules"
)

// Register adds every built-in AI to reg.
func Register(reg *rules.Registry) {
	reg.AddAI("chase", chase{})
	reg.AddAI("chase_and_flee", chaseAndFlee{})
	reg.AddAI("utility", utility{})
	registerOptions(reg)
	registerThiefOptions(reg)
}

// chase waits until it sees the player, then closes in by the shortest
// legal route every turn.
//
// Seeing the player means sharing a room with them, where standing in a
// room's doorway counts as being in that room: a doorway is an open
// threshold, not a barrier. Two things get in the way:
//   - An invisible player cannot be perceived at all — unless the monster
//     can see invisible. A monster never notices one, and one already
//     chasing loses track and stops.
//   - A sneaking player is not noticed from a doorway, only from fully
//     inside the room. That only prevents being spotted; a monster that
//     is already chasing keeps chasing.
type chase struct{}

func (chase) Params() []rules.ParamDef { return nil }

func (chase) Act(c *rules.Ctx, self *rules.Entity, p rules.Params) (int, int, bool) {
	return pursue(c, self)
}

// pursue is chase's whole behavior, shared with chaseAndFlee for when it
// is not fleeing.
func pursue(c *rules.Ctx, self *rules.Entity) (int, int, bool) {
	player := c.World.Player
	if player == nil {
		return 0, 0, false
	}
	if c.Reg.Has(player, "invisible") && !c.Reg.Has(self, "see_invisible") {
		self.Mind.Aware = false
		return 0, 0, false
	}
	if !self.Mind.Aware {
		if !c.Sees(self, player) {
			return 0, 0, false
		}
		self.Mind.Aware = true
	}
	return c.StepToward(self, player.X, player.Y)
}

// chaseAndFlee chases like chase until a hit leaves it at or below
// flee_at_percent of its maximum HP (at least 1 HP). Then it runs for a
// random cell in some room other than the player's, and keeps running
// whether or not it can see the player. It stops fleeing on arrival —
// having to notice the player all over again — or if it recovers above the
// threshold. Cornered on the way, it attacks whoever blocks its path.
type chaseAndFlee struct{}

func (chaseAndFlee) Params() []rules.ParamDef {
	return []rules.ParamDef{{Name: "flee_at_percent", Kind: rules.ParamInt, Default: 10}}
}

// FleeThreshold is the HP at or below which a monster with maxHP and
// percent starts fleeing: percent of maxHP rounded up, and never below 1,
// so a weak monster still flees rather than rounding its threshold to 0.
func FleeThreshold(maxHP, percent int) int {
	t := (maxHP*percent + 99) / 100
	if t < 1 {
		t = 1
	}
	return t
}

func (chaseAndFlee) Hurt(self *rules.Entity, p rules.Params) {
	if self.Dead || self.Mind.Fleeing || self.HP > FleeThreshold(self.MaxHP, p.Int("flee_at_percent")) {
		return
	}
	self.Mind.Fleeing = true
	self.Mind.FleeRoom = -1
}

func (chaseAndFlee) Act(c *rules.Ctx, self *rules.Entity, p rules.Params) (int, int, bool) {
	mind := &self.Mind
	if mind.Fleeing && mind.FleeRoom == -1 {
		player := c.World.Player
		if room, x, y, ok := pickFleeCell(c.World.Map, player.X, player.Y); ok {
			mind.FleeRoom, mind.FleeX, mind.FleeY = room.ID, x, y
		} else {
			mind.Fleeing = false // nowhere to run: a single-room level
		}
	}
	if mind.Fleeing && self.HP > FleeThreshold(self.MaxHP, p.Int("flee_at_percent")) {
		mind.Fleeing, mind.FleeRoom, mind.Aware = false, -1, false
	}
	if !mind.Fleeing {
		return pursue(c, self)
	}
	x, y, ok := c.StepToward(self, mind.FleeX, mind.FleeY)
	if !ok {
		// Arrived (NextStep from a point to itself reports no step), or
		// no route exists. Either way it is as far as it will get.
		mind.Fleeing, mind.FleeRoom, mind.Aware = false, -1, false
		return 0, 0, false
	}
	return x, y, true
}

// pickFleeCell picks a random room other than the player's, and a random
// cell in it. It uses the global source: fleeing is a turn-time decision.
func pickFleeCell(m *mapdata.Map, playerX, playerY int) (room mapdata.Room, x, y int, ok bool) {
	playerRoom, _ := m.RoomAt(playerX, playerY)
	var candidates []mapdata.Room
	for _, r := range m.Rooms {
		if r.ID != playerRoom.ID {
			candidates = append(candidates, r)
		}
	}
	if len(candidates) == 0 {
		return mapdata.Room{}, 0, 0, false
	}
	room = candidates[rand.Intn(len(candidates))]
	return room, room.X + rand.Intn(room.W), room.Y + rand.Intn(room.H), true
}
