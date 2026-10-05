// Package rulestest builds rules-engine fixtures for tests: the engine
// with the real content loaded, maps, and entities placed where a test
// wants them.
package rulestest

import (
	"testing"

	"micro/server/internal/attributes"
	"micro/server/internal/content"
	"micro/server/internal/mapdata"
	"micro/server/internal/rules"
	"micro/server/internal/rules/builtin"
)

// Engine returns an engine with every built-in behavior and the game's
// real content, on which every melee blow lands (see AlwaysHit): tests
// about misses set their own HitRoll.
func Engine(t testing.TB) *rules.Engine {
	t.Helper()
	reg := builtin.Registry()
	cat, err := content.Load(reg)
	if err != nil {
		t.Fatalf("load content: %v", err)
	}
	return &rules.Engine{Reg: reg, Cat: cat, HitRoll: AlwaysHit}
}

// AlwaysHit and AlwaysMiss are HitRolls that decide every blow: the
// lowest and highest percentile rolls.
func AlwaysHit() int  { return 0 }
func AlwaysMiss() int { return 99 }

// Room is a single 10x10 room (interior x and y 5-14) on a 20x20 map: room
// to place things without any corridor or door concerns.
func Room() *mapdata.Map {
	return &mapdata.Map{Width: 20, Height: 20, Rooms: []mapdata.Room{{ID: 0, X: 5, Y: 5, W: 10, H: 10}}}
}

// TwoRooms is a 10x10 room A (interior x and y 5-14) joined by a corridor
// to a 5x5 room B (interior x 30-34, y 5-9). The corridor leaves A by the
// door at (15, 9), runs east along y=9 to x=29, then north to B's door at
// (29, 7).
func TwoRooms() *mapdata.Map {
	var path []mapdata.Point
	for x := 15; x <= 29; x++ {
		path = append(path, mapdata.Point{X: x, Y: 9})
	}
	for y := 8; y >= 7; y-- {
		path = append(path, mapdata.Point{X: 29, Y: y})
	}
	return &mapdata.Map{
		Width: 40, Height: 20,
		Rooms: []mapdata.Room{{ID: 0, X: 5, Y: 5, W: 10, H: 10}, {ID: 1, X: 30, Y: 5, W: 5, H: 5}},
		Corridors: []mapdata.Corridor{{
			ID: 0, RoomA: 0, RoomB: 1,
			DoorA: mapdata.Door{X: 15, Y: 9}, DoorB: mapdata.Door{X: 29, Y: 7},
			Path: path,
		}},
	}
}

// New creates an entity from the template with the given ID at (x, y).
// A creature gets exactly hp HP (and MaxHP) and all-zero attributes, so a
// test controls every number that matters; hp is ignored for items.
func New(t testing.TB, eng *rules.Engine, id string, x, y, hp int) *rules.Entity {
	t.Helper()
	tmpl := eng.Cat.Get(id)
	if tmpl == nil {
		t.Fatalf("no template %q", id)
	}
	e := rules.New(tmpl, x, y, nil)
	if e.IsCreature() {
		e.HP, e.MaxHP = hp, hp
		e.Attrs = attributes.Attributes{}
	}
	return e
}

// Give puts item in e's inventory.
func Give(e, item *rules.Entity) *rules.Entity {
	e.Inventory = append(e.Inventory, item)
	return item
}
