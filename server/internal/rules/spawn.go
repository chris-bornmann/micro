package rules

import (
	"hash/fnv"
	"math/rand"
	"slices"

	"micro/server/internal/mapdata"
	"micro/server/internal/spawn"
)

// SpawnSeed is the seed for the index'th entity of template id on a level
// whose map was generated from levelSeed. Hashing the template ID gives
// every template its own stream without anyone assigning offsets by hand,
// and the index keeps several of one template on a level from all landing
// on the same tile with the same rolls.
func SpawnSeed(levelSeed int64, id string, index int) int64 {
	h := fnv.New64a()
	h.Write([]byte(id))
	return levelSeed + int64(h.Sum64()>>1) + int64(index)*1_000_000
}

// RandomRoomCell picks a random room, then a random cell inside it.
func RandomRoomCell(m *mapdata.Map, rng *rand.Rand) (x, y int) {
	for tries := 0; ; tries++ {
		r := m.Rooms[rng.Intn(len(m.Rooms))]
		x, y = r.X+rng.Intn(r.W), r.Y+rng.Intn(r.H)
		// A wall inside a room is no place to put anything. Redrawn only
		// on a level that has them, so ordinary levels draw as they always
		// have from a seed.
		if !m.IsWall(x, y) || tries == maxPlacementTries {
			return x, y
		}
	}
}

// SpawnCount is how many of t appear on dungeonLevel, by t's spawn model.
// alreadySpawned is whether a single-shot template has appeared anywhere
// this run yet.
func SpawnCount(t *Template, dungeonLevel int, alreadySpawned bool, rng *rand.Rand) int {
	s := t.Spawn
	switch s.Model {
	case SpawnCommon:
		return spawn.GenericSpawnCount(t.Level, dungeonLevel, s.Density, s.SigmaUp, s.SigmaDown, s.MaxAbove, rng)
	case SpawnSingleShot:
		if spawn.SingleShotShouldSpawn(t.Level, dungeonLevel, s.Earliest, s.ForceBy, s.ChanceScale, s.SigmaUp, s.SigmaDown, s.MaxAbove, alreadySpawned, rng) {
			return 1
		}
		return 0
	case SpawnFixed:
		return s.Count
	case SpawnPerLevel:
		if dungeonLevel < s.Levels.Min || dungeonLevel > s.Levels.Max {
			return 0
		}
		n := 0
		if rng.Intn(100) < s.Chance {
			n = rollRange(s.Number, rng)
		}
		if n == 0 && slices.Contains(s.AlwaysOn, dungeonLevel) {
			n = 1
		}
		return n
	}
	return 0
}

// Spawn creates the index'th entity of t on m, placed in a random room
// and rolled from its own seeded stream (see SpawnSeed), so a given seed
// always produces the same level. withEquipment says which of t's
// starting equipment to give it: the caller decides, since per-level
// limits (EquipSpec.LimitPerLevel) depend on how many came before.
func (eng *Engine) Spawn(t *Template, m *mapdata.Map, index int, withEquipment func(EquipSpec) bool) *Entity {
	return eng.SpawnWhere(t, m, index, withEquipment, nil)
}

// maxPlacementTries bounds how many cells SpawnWhere draws looking for a
// free one before settling for the last, on a level too crowded to have
// one.
const maxPlacementTries = 1000

// SpawnWhere is Spawn, drawing cells until free reports one available (nil
// accepts any). The draws come from the same seeded stream, so the result
// is still reproducible.
func (eng *Engine) SpawnWhere(t *Template, m *mapdata.Map, index int, withEquipment func(EquipSpec) bool, free func(x, y int) bool) *Entity {
	rng := rand.New(rand.NewSource(SpawnSeed(m.Seed, t.ID, index)))
	x, y := RandomRoomCell(m, rng)
	for try := 1; free != nil && !free(x, y) && try < maxPlacementTries; try++ {
		x, y = RandomRoomCell(m, rng)
	}
	return eng.Create(t, x, y, rng, withEquipment)
}

// Create makes a creature of template t at (x, y), rolled from rng, with
// its starting equipment (those withEquipment accepts; nil means all) and
// its starting inventory.
func (eng *Engine) Create(t *Template, x, y int, rng *rand.Rand, withEquipment func(EquipSpec) bool) *Entity {
	e := New(t, x, y, rng)
	// Equipment fills slots in the order the template lists it, and an
	// item with no free slot left is skipped: a goblin listing a
	// sword (first goblin only) then a dagger (first two) gives the first
	// goblin the sword and the second the dagger.
	for _, es := range t.Equipment {
		if withEquipment != nil && !withEquipment(es) {
			continue
		}
		// Rolled only for what is not certain, so content without chances
		// draws exactly what it always has from a seed.
		if es.Chance < 100 && rng.Intn(100) >= es.Chance {
			continue
		}
		slot := Slot("")
		for _, s := range EquipSlotsFor(es.Item.Slot) {
			if e.Equipped[s] == nil && e.blockerOf(s) == nil {
				slot = s
				break
			}
		}
		for _, b := range es.Item.Blocks {
			if e.Equipped[b] != nil {
				slot = "" // it would cover something already worn
			}
		}
		if slot == "" {
			continue
		}
		e.equipInto(slot, New(es.Item, x, y, rng))
	}
	for _, cs := range t.Inventory {
		if rng.Intn(100) < cs.Chance {
			e.Inventory = append(e.Inventory, New(cs.Item, x, y, rng))
		}
	}
	return e
}

// Kill leaves behind what a creature that has just died drops: its corpse,
// if its template leaves one, then everything it carried, all on the tile
// where it fell. The creature itself is left as it was apart from its
// belongings, so a later resurrection could restore it.
func (c *Ctx) Kill(e *Entity) []*Entity {
	var dropped []*Entity
	if e.T.LeavesCorpse {
		if ct := c.Cat.Get("corpse"); ct != nil {
			corpse := New(ct, e.X, e.Y, nil)
			corpse.Name = e.Kind() + " corpse"
			corpse.Weight = e.Weight
			corpse.Source = e.ID
			// A corpse stays as oily as the creature was, and still shows
			// if it died burning.
			for _, name := range []string{"oily", "on_fire"} {
				if inst := e.FindTrait(name); inst != nil {
					cp := *inst
					corpse.AddTrait(&cp)
				}
			}
			dropped = append(dropped, corpse)
		}
	}
	for _, it := range e.Carried() {
		it.X, it.Y = e.X, e.Y
		dropped = append(dropped, it)
	}
	e.Equipped = nil
	e.Inventory = nil
	return dropped
}
