package rules_test

import (
	"testing"

	"micro/server/internal/rules"
	"micro/server/internal/rules/rulestest"
)

// corridor puts the player far down the corridor of TwoRooms and two
// aware goblins between: a at (16,9) behind b at (17,9).
func corridor(t *testing.T) (*rules.Ctx, *rules.Entity, *rules.Entity) {
	t.Helper()
	eng := rulestest.Engine(t)
	p := rulestest.New(t, eng, "player", 22, 9, 100)
	a := rulestest.New(t, eng, "goblin", 16, 9, 10)
	b := rulestest.New(t, eng, "goblin", 17, 9, 10)
	for _, g := range []*rules.Entity{a, b} {
		g.Equipped = nil
		g.Mind.Aware = true
	}
	return eng.NewCtx(&rules.World{Map: rulestest.TwoRooms(), Player: p, Monsters: []*rules.Entity{a, b}}), a, b
}

func at(t *testing.T, e *rules.Entity, x, y int) {
	t.Helper()
	if e.X != x || e.Y != y {
		t.Errorf("%s at (%d,%d), want (%d,%d)", e.ID, e.X, e.Y, x, y)
	}
}

func TestAMonsterIsBlockedByAnotherAndDoesNotAttackIt(t *testing.T) {
	c, a, b := corridor(t)
	if _, attacked := c.MonsterTurn(a); attacked {
		t.Error("monsters never attack each other")
	}
	at(t, a, 16, 9)
	if b.HP != 10 {
		t.Errorf("the goblin in the way took damage: HP %d", b.HP)
	}
}

// TestAMonsterMaySwapWithOneThatIsFleeing: a monster on the run gets out
// of the way of one that isn't.
func TestAMonsterMaySwapWithOneThatIsFleeing(t *testing.T) {
	c, a, b := corridor(t)
	b.Mind.Fleeing = true
	if res := c.Step(a, 17, 9); !res.Moved || res.Swapped != b {
		t.Fatalf("expected a swap, got %+v", res)
	}
	at(t, a, 17, 9)
	at(t, b, 16, 9)
}

func TestAFleeingMonsterCannotPushPastAnother(t *testing.T) {
	c, a, b := corridor(t)
	a.Mind.Fleeing = true
	if res := c.Step(a, 17, 9); res.Moved {
		t.Error("a fleeing monster may not swap with one that is not fleeing")
	}
	b.Mind.Fleeing = true
	if res := c.Step(a, 17, 9); res.Moved {
		t.Error("nor with one that is also fleeing")
	}
	at(t, a, 16, 9)
	at(t, b, 17, 9)
}

func TestNobodySwapsWithThePlayer(t *testing.T) {
	c, a, _ := corridor(t)
	p := c.World.Player
	p.X, p.Y = 15, 9
	a.Mind.Fleeing = true
	if res := c.Step(a, 15, 9); res.Moved || res.Target != p {
		t.Errorf("stepping into the player is an attack, never a swap: %+v", res)
	}
	c.World.Monsters[1].X, c.World.Monsters[1].Y = 14, 9
	c.World.Monsters[1].Mind.Fleeing = true
	if res := c.Step(p, 14, 9); res.Moved || res.Target == nil {
		t.Errorf("the player attacks a fleeing monster, never swaps: %+v", res)
	}
}

func TestSlidingOnOilNeverEndsOnAnotherMonster(t *testing.T) {
	for i := 0; i < 200; i++ {
		eng := rulestest.Engine(t)
		a := rulestest.New(t, eng, "goblin", 8, 10, 10)
		b := rulestest.New(t, eng, "goblin", 10, 10, 10)
		w := &rules.World{Map: rulestest.Room(), Player: rulestest.New(t, eng, "player", 14, 14, 10), Monsters: []*rules.Entity{a, b}}
		w.SpillOil(9, 10)
		eng.NewCtx(w).Step(a, 9, 10)
		if a.X == b.X && a.Y == b.Y {
			t.Fatal("slid onto another monster")
		}
	}
}

func TestNearestFreeTile(t *testing.T) {
	eng := rulestest.Engine(t)
	g := rulestest.New(t, eng, "goblin", 8, 8, 10)
	w := &rules.World{Map: rulestest.Room(), Player: rulestest.New(t, eng, "player", 9, 8, 10), Monsters: []*rules.Entity{g}}
	x, y, ok := w.NearestFreeTile(8, 8, nil)
	if !ok || (x == 8 && y == 8) || (x == 9 && y == 8) {
		t.Fatalf("got (%d,%d)", x, y)
	}
	if d := abs(x-8) + abs(y-8); d != 1 {
		t.Errorf("(%d,%d) is %d away; a free neighbor exists", x, y, d)
	}
	if x, y, _ := w.NearestFreeTile(8, 8, g); x != 8 || y != 8 {
		t.Errorf("ignoring the goblin itself, its own tile is free: got (%d,%d)", x, y)
	}
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// TestAMonsterWalksAroundAnotherToReachThePlayer: in an open room, a
// goblin lined up behind another steps around it and takes a free side of
// the player, rather than waiting behind it.
func TestAMonsterWalksAroundAnotherToReachThePlayer(t *testing.T) {
	eng := rulestest.Engine(t)
	p := rulestest.New(t, eng, "player", 10, 10, 1000)
	a := rulestest.New(t, eng, "goblin", 9, 10, 10)
	b := rulestest.New(t, eng, "goblin", 8, 10, 10)
	for _, g := range []*rules.Entity{a, b} {
		g.Equipped = nil
		g.Mind.Aware = true
	}
	c := eng.NewCtx(&rules.World{Map: rulestest.Room(), Player: p, Monsters: []*rules.Entity{a, b}})
	for turn := 0; turn < 3; turn++ {
		c.MonsterTurn(b)
	}
	if d := abs(b.X-p.X) + abs(b.Y-p.Y); d != 1 || (b.X == a.X && b.Y == a.Y) {
		t.Errorf("after three turns the goblin behind is at (%d,%d); want beside the player at (10,10)", b.X, b.Y)
	}
}

// TestAMonsterQueuesWhenThereIsNoWayAround: in a corridor there is no way
// past the goblin in front, so the one behind holds its place.
func TestAMonsterQueuesWhenThereIsNoWayAround(t *testing.T) {
	c, a, _ := corridor(t)
	c.MonsterTurn(a)
	at(t, a, 16, 9)
}
