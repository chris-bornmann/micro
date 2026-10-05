package rules

// What creatures perceive. Monsters notice the player the way the player
// sees the dungeon: by room, not by geometric line of sight. Sharing a room
// is seeing; a room's doorway counts as being in it, because a doorway is
// an open threshold — except to notice a sneaking player, who is seen only
// once fully inside. A walled room (a maze) is the exception: there, sight
// reaches MazeSightRadius, and stops at walls.

// Sees reports whether observer can see target right now.
//
// An invisible target is never seen, unless observer can see invisible.
func (c *Ctx) Sees(observer, target *Entity) bool {
	if target == nil || target.Dead {
		return false
	}
	if c.Reg.Has(target, "invisible") && !c.Reg.Has(observer, "see_invisible") {
		return false
	}
	m := c.World.Map
	theirs := m.RoomsAt(target.X, target.Y)
	if c.Reg.Has(target, "sneaking") {
		theirs = map[int]bool{}
		if r, ok := m.RoomAt(target.X, target.Y); ok {
			theirs[r.ID] = true
		}
	}
	for id := range m.RoomsAt(observer.X, observer.Y) {
		if !theirs[id] {
			continue
		}
		if r, ok := m.RoomByID(id); ok && r.Walled {
			// In a maze, sharing the room is not enough: only so far, and
			// not through its walls.
			near := max(observer.X-target.X, target.X-observer.X, observer.Y-target.Y, target.Y-observer.Y) <= MazeSightRadius
			return near && m.Clear(observer.X, observer.Y, target.X, target.Y)
		}
		return true
	}
	return false
}

// MazeSightRadius is how far anything sees in a walled room (a maze).
const MazeSightRadius = 4

// Notice updates observer's memory of the player: whether it can see them
// now, and if so where they are. It reports whether it can.
func (c *Ctx) Notice(observer *Entity) bool {
	p := c.World.Player
	if !c.Sees(observer, p) {
		return false
	}
	observer.Mind.Seen = true
	observer.Mind.LastSeen.X, observer.Mind.LastSeen.Y = p.X, p.Y
	return true
}
