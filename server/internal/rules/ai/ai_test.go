package ai_test

import (
	"testing"

	"micro/server/internal/mapdata"
	"micro/server/internal/rules"
	"micro/server/internal/rules/ai"
	"micro/server/internal/rules/rulestest"
)

// scene puts a player and one monster of kind on m.
func scene(t *testing.T, m *mapdata.Map, kind string, mx, my, px, py int) (*rules.Engine, *rules.World, *rules.Entity) {
	t.Helper()
	eng := rulestest.Engine(t)
	p := rulestest.New(t, eng, "player", px, py, 1000)
	mon := rulestest.New(t, eng, kind, mx, my, 10)
	mon.Equipped = nil // no starting sword in these tests
	return eng, &rules.World{Map: m, Player: p, Monsters: []*rules.Entity{mon}}, mon
}

func turn(eng *rules.Engine, w *rules.World, m *rules.Entity) (rules.Outcome, bool) {
	return eng.NewCtx(w).MonsterTurn(m)
}

func at(t *testing.T, m *rules.Entity, x, y int) {
	t.Helper()
	if m.X != x || m.Y != y {
		t.Fatalf("at (%d,%d), want (%d,%d)", m.X, m.Y, x, y)
	}
}

// Both AIs perceive the player the same way, so every awareness rule is
// checked against each.
var chasers = []string{"goblin", "mummy"}

func TestStaysPutWhileThePlayerIsInAnotherRoom(t *testing.T) {
	for _, kind := range chasers {
		eng, w, m := scene(t, rulestest.TwoRooms(), kind, 10, 10, 32, 7)
		turn(eng, w, m)
		at(t, m, 10, 10)
		if m.Mind.Aware {
			t.Errorf("%s noticed a player in another room", kind)
		}
	}
}

func TestNoticesAndClosesInWhenThePlayerEntersTheRoom(t *testing.T) {
	for _, kind := range chasers {
		eng, w, m := scene(t, rulestest.TwoRooms(), kind, 10, 10, 13, 10)
		turn(eng, w, m)
		if !m.Mind.Aware {
			t.Fatalf("%s should notice a player in its room", kind)
		}
		at(t, m, 11, 10)
	}
}

func TestNoticesAPlayerOnTheDoorwayUnlessTheyAreSneaking(t *testing.T) {
	for _, kind := range chasers {
		eng, w, m := scene(t, rulestest.TwoRooms(), kind, 10, 9, 15, 9)
		turn(eng, w, m)
		if !m.Mind.Aware {
			t.Errorf("%s should notice a player standing in its doorway", kind)
		}

		eng, w, m = scene(t, rulestest.TwoRooms(), kind, 10, 9, 15, 9)
		w.Player.Equipped = map[rules.Slot]*rules.Entity{rules.SlotFeet: rulestest.New(t, eng, "shoes_of_sneaking", 0, 0, 0)}
		turn(eng, w, m)
		if m.Mind.Aware {
			t.Errorf("%s should not notice a sneaking player in its doorway", kind)
		}

		w.Player.X = 14 // one step fully inside
		turn(eng, w, m)
		if !m.Mind.Aware {
			t.Errorf("%s should notice a sneaking player fully inside its room", kind)
		}

		w.Player.X = 15 // back to the doorway: already aware, so it keeps chasing
		x := m.X
		turn(eng, w, m)
		if !m.Mind.Aware || m.X != x+1 {
			t.Errorf("%s should keep chasing a sneaking player it has already seen", kind)
		}
	}
}

func TestAnInvisiblePlayerIsNeitherNoticedNorAttacked(t *testing.T) {
	for _, kind := range chasers {
		eng, w, m := scene(t, rulestest.TwoRooms(), kind, 10, 10, 11, 10)
		m.Mind.Aware = true
		w.Player.AddTrait(&rules.TraitInstance{Trait: "invisible", Turns: 5})
		if _, attacked := turn(eng, w, m); attacked {
			t.Errorf("%s attacked an invisible player", kind)
		}
		at(t, m, 10, 10)
		if m.Mind.Aware {
			t.Errorf("%s should lose track of a player who turns invisible", kind)
		}
	}
}

func TestAttacksWhenThePlayerIsInTheWay(t *testing.T) {
	for _, kind := range chasers {
		eng, w, m := scene(t, rulestest.TwoRooms(), kind, 10, 10, 11, 10)
		hp := w.Player.HP
		out, attacked := turn(eng, w, m)
		if !attacked || out.Damage <= 0 || w.Player.HP != hp-out.Damage {
			t.Errorf("%s: attacked %v, outcome %+v, HP %d->%d", kind, attacked, out, hp, w.Player.HP)
		}
		at(t, m, 10, 10)
	}
}

func TestChasesThroughTheDoorAndDownTheCorridor(t *testing.T) {
	eng, w, m := scene(t, rulestest.TwoRooms(), "mummy", 13, 9, 14, 9)
	turn(eng, w, m) // notices the player, and attacks: they are adjacent
	w.Player.X, w.Player.Y = 20, 9
	for i := 0; i < 5; i++ {
		turn(eng, w, m)
	}
	at(t, m, 18, 9) // out through the door at x=15 and four tiles on

}

func TestStaysPutWithNoRouteToThePlayer(t *testing.T) {
	m := rulestest.TwoRooms()
	m.Corridors = nil
	eng, w, mon := scene(t, m, "mummy", 10, 10, 32, 7)
	mon.Mind.Aware = true
	turn(eng, w, mon)
	at(t, mon, 10, 10)
}

func TestSlidesOnOil(t *testing.T) {
	seen := map[mapdata.Point]bool{}
	for i := 0; i < 200; i++ {
		eng, w, m := scene(t, rulestest.TwoRooms(), "mummy", 8, 10, 13, 10)
		w.SpillOil(9, 10)
		turn(eng, w, m)
		seen[mapdata.Point{X: m.X, Y: m.Y}] = true
	}
	// Stop on the oil, slide on, or skid left or right.
	for p := range seen {
		switch p {
		case mapdata.Point{X: 9, Y: 10}, mapdata.Point{X: 10, Y: 10}, mapdata.Point{X: 9, Y: 9}, mapdata.Point{X: 9, Y: 11}:
		default:
			t.Errorf("slid to an impossible tile %v", p)
		}
	}
	if len(seen) < 2 {
		t.Error("never slid off the oily tile in 200 tries")
	}
}

func TestFleeThresholdIsATenthOfMaxHPAndAtLeastOne(t *testing.T) {
	for hp := 5; hp <= 10; hp++ {
		if got := ai.FleeThreshold(hp, 10); got != 1 {
			t.Errorf("FleeThreshold(%d, 10) = %d, want 1", hp, got)
		}
	}
	if got := ai.FleeThreshold(25, 10); got != 3 {
		t.Errorf("FleeThreshold(25, 10) = %d, want 3", got)
	}
}

// hurt deals damage to m with no attacker, as burning does.
func hurt(eng *rules.Engine, w *rules.World, m *rules.Entity, amount int) {
	eng.NewCtx(w).Hurt(m, rules.DamagePhysical, amount)
}

func TestAGoblinFleesOnlyWhenAHitLeavesItAtTheThreshold(t *testing.T) {
	eng, w, g := scene(t, rulestest.TwoRooms(), "goblin", 10, 10, 11, 10)
	hurt(eng, w, g, 8) // 10 -> 2, above 1
	if g.Mind.Fleeing {
		t.Fatal("fled above the threshold")
	}
	hurt(eng, w, g, 1) // -> 1
	if !g.Mind.Fleeing || g.Mind.FleeRoom != -1 {
		t.Fatalf("should start fleeing with no destination yet, got %+v", g.Mind)
	}

	g.Mind.FleeRoom = 1
	hurt(eng, w, g, 0)
	if g.Mind.FleeRoom != 1 {
		t.Error("a hit while already fleeing must not reset the destination")
	}

	eng, w, g = scene(t, rulestest.TwoRooms(), "goblin", 10, 10, 11, 10)
	hurt(eng, w, g, 10)
	if !g.Dead || g.Mind.Fleeing {
		t.Error("a killing blow does not start a flee")
	}
}

func TestAMummyNeverFlees(t *testing.T) {
	eng, w, m := scene(t, rulestest.TwoRooms(), "mummy", 10, 10, 11, 10)
	hurt(eng, w, m, 9)
	if m.Mind.Fleeing {
		t.Error("a mummy fled")
	}
}

// TestAFleeingGoblinRunsToAnotherRoomRegardlessOfThePlayer: it picks a
// cell in a room other than the player's, heads there turn after turn even
// once the player is invisible, and on arrival stops fleeing and forgets
// the player.
func TestAFleeingGoblinRunsToAnotherRoomRegardlessOfThePlayer(t *testing.T) {
	eng, w, g := scene(t, rulestest.TwoRooms(), "goblin", 13, 9, 10, 9)
	g.HP = 1
	g.Mind = rules.Mind{Aware: true, Fleeing: true, FleeRoom: -1}
	w.Player.AddTrait(&rules.TraitInstance{Trait: "invisible", Turns: 100})

	turn(eng, w, g)
	if g.Mind.FleeRoom != 1 {
		t.Fatalf("should flee to room 1, the only other room; got %+v", g.Mind)
	}
	dest := mapdata.Point{X: g.Mind.FleeX, Y: g.Mind.FleeY}
	if r, _ := w.Map.RoomByID(1); dest.X < r.X || dest.X >= r.X+r.W || dest.Y < r.Y || dest.Y >= r.Y+r.H {
		t.Fatalf("flee cell %v is not inside room 1", dest)
	}
	for i := 0; i < 60 && g.Mind.Fleeing; i++ {
		turn(eng, w, g)
	}
	if g.Mind.Fleeing || g.Mind.Aware || g.X != dest.X || g.Y != dest.Y {
		t.Errorf("should have arrived at %v and calmed down, got (%d,%d) %+v", dest, g.X, g.Y, g.Mind)
	}
}

func TestAFleeingGoblinWithNowhereToGoFightsOn(t *testing.T) {
	eng, w, g := scene(t, rulestest.Room(), "goblin", 10, 10, 11, 10)
	g.HP = 1
	g.Mind = rules.Mind{Aware: true, Fleeing: true, FleeRoom: -1}
	if _, attacked := turn(eng, w, g); !attacked || g.Mind.Fleeing {
		t.Error("with only one room there is nowhere to flee: it should stop fleeing and attack")
	}
}

func TestAFleeingGoblinAttacksWhoeverBlocksItsPath(t *testing.T) {
	eng, w, g := scene(t, rulestest.TwoRooms(), "goblin", 13, 9, 14, 9)
	g.HP = 1
	g.Mind = rules.Mind{Fleeing: true, FleeRoom: 1, FleeX: 32, FleeY: 7}
	if _, attacked := turn(eng, w, g); !attacked {
		t.Error("cornered, it should attack the player in its way")
	}
}

func TestAGoblinStopsFleeingOnceHealedAboveTheThreshold(t *testing.T) {
	eng, w, g := scene(t, rulestest.TwoRooms(), "goblin", 10, 10, 32, 7)
	g.Mind = rules.Mind{Aware: true, Fleeing: true, FleeRoom: 1, FleeX: 32, FleeY: 7}
	turn(eng, w, g) // HP 10: well above
	if g.Mind.Fleeing || g.Mind.Aware {
		t.Errorf("should stop fleeing and forget the player, got %+v", g.Mind)
	}
}
