package ai_test

import (
	"testing"

	"micro/server/internal/rules"
	"micro/server/internal/rules/rulestest"
)

// thiefScene is the player at (6,6) carrying a potion and a torch and
// wielding a dagger, with the Thief right beside them at (7,6).
func thiefScene(t *testing.T) (*rules.Engine, *rules.World, *rules.Entity, *rules.Entity) {
	t.Helper()
	eng := rulestest.Engine(t)
	p := rulestest.New(t, eng, "player", 6, 6, 1000)
	rulestest.Give(p, rulestest.New(t, eng, "healing_potion", 0, 0, 0))
	rulestest.Give(p, rulestest.New(t, eng, "torch", 0, 0, 0))
	p.Equipped = map[rules.Slot]*rules.Entity{rules.SlotWeapon: rulestest.New(t, eng, "fine_dagger", 0, 0, 0)}
	thief := rulestest.New(t, eng, "thief", 7, 6, 20)
	return eng, &rules.World{Map: rulestest.Room(), Player: p, Monsters: []*rules.Entity{thief}}, p, thief
}

func TestTheThiefNeverAttacksUnprovoked(t *testing.T) {
	eng, w, p, thief := thiefScene(t)
	p.Inventory = nil // nothing to steal, so he just hangs about
	for i := 0; i < 100; i++ {
		if _, attacked := eng.NewCtx(w).MonsterTurn(thief); attacked {
			t.Fatal("the thief attacked an unprovoking player")
		}
	}
	if p.HP != 1000 {
		t.Errorf("player HP %d", p.HP)
	}
}

// TestTheThiefStealsSilently: from the pack only, without a word, and
// then he is afraid.
func TestTheThiefStealsSilently(t *testing.T) {
	eng, w, p, thief := thiefScene(t)
	for i := 0; i < 100 && thief.Mind.Count("stolen") == 0; i++ {
		c := eng.NewCtx(w)
		thief.X, thief.Y = 7, 6 // keep him beside the player
		c.MonsterTurn(thief)
		if len(c.Messages()) != 0 {
			t.Fatalf("stealing must be silent, got %q", c.Messages())
		}
	}
	// (rulestest.New makes him without his starting potion: he carries
	// only what he took.)
	if thief.Mind.Count("stolen") != 1 || len(p.Inventory) != 1 || len(thief.Inventory) != 1 {
		t.Fatalf("one item should have moved: player %d, thief %d", len(p.Inventory), len(thief.Inventory))
	}
	if p.Weapon() == nil {
		t.Error("equipped things are safe")
	}
	if thief.Mind.Attitude != rules.Afraid {
		t.Errorf("after stealing he should be afraid, is %q", thief.Mind.Attitude)
	}
}

func TestAnAfraidThiefEscapesOnlyOnceOutOfSight(t *testing.T) {
	eng, w, _, thief := thiefScene(t)
	thief.Mind.Attitude = rules.Afraid
	w.Visible = func(x, y int) bool { return true }
	c := eng.NewCtx(w)
	c.MonsterTurn(thief)
	if len(c.Departed) != 0 {
		t.Fatal("in sight, he must run, not vanish")
	}
	if thief.X <= 7 && thief.Y == 6 {
		t.Errorf("he should have stepped away, at (%d,%d)", thief.X, thief.Y)
	}

	w.Visible = func(x, y int) bool { return false }
	c = eng.NewCtx(w)
	c.MonsterTurn(thief)
	if len(c.Departed) != 1 || c.Departed[0] != thief {
		t.Error("out of sight, he should leave the level")
	}
}

// attack records that the player has just attacked thief, this turn.
func attack(w *rules.World, thief *rules.Entity) {
	thief.Mind.Attacked++
	thief.Mind.AttackedTurn = w.Turn
}

// decide has thief answer an attack, 200 times over from a fresh scene
// set up by prep, and counts how often he fought.
func decide(t *testing.T, prep func(thief *rules.Entity)) (fought int) {
	t.Helper()
	for i := 0; i < 200; i++ {
		eng, w, p, thief := thiefScene(t)
		prep(thief)
		attack(w, thief)
		eng.NewCtx(w).MonsterTurn(thief)
		switch thief.Mind.Attitude {
		case rules.Hostile:
			fought++
			if p.HP == 1000 {
				t.Fatal("deciding to fight beside the player, he should strike at once")
			}
		case rules.Afraid:
		default:
			t.Fatalf("still %q after being attacked", thief.Mind.Attitude)
		}
	}
	return fought
}

func TestAnAttackedThiefUsuallyFightsBack(t *testing.T) {
	// 75%.
	if fought := decide(t, func(*rules.Entity) {}); fought < 125 || fought > 175 {
		t.Errorf("fought %d of 200", fought)
	}
}

// TestAThiefRunningOffMayTurnAndFight: caught running with your things
// and attacked, he sometimes turns to defend himself.
func TestAThiefRunningOffMayTurnAndFight(t *testing.T) {
	// 40%.
	fought := decide(t, func(th *rules.Entity) { th.Mind.Attitude = rules.Afraid })
	if fought < 50 || fought > 110 {
		t.Errorf("fought %d of 200", fought)
	}
}

func TestABadlyHurtThiefNeverStandsAndFights(t *testing.T) {
	for _, attitude := range []rules.Attitude{rules.Neutral, rules.Afraid} {
		fought := decide(t, func(th *rules.Entity) {
			th.Mind.Attitude = attitude
			th.HP = th.MaxHP / 5 // below his flee threshold of 25%
			th.Inventory = nil   // and no potion to drink instead
		})
		if fought != 0 {
			t.Errorf("%s and badly hurt, he fought %d times", attitude, fought)
		}
	}
}

// TestAnOldAttackIsNotReconsidered: he weighs fighting back when attacked,
// not every turn after.
func TestAnOldAttackIsNotReconsidered(t *testing.T) {
	for i := 0; i < 50; i++ {
		eng, w, _, thief := thiefScene(t)
		thief.Mind.Attitude = rules.Afraid
		attack(w, thief)
		w.Turn++
		eng.NewCtx(w).MonsterTurn(thief)
		if thief.Mind.Attitude != rules.Afraid {
			t.Fatal("an attack last turn should not make him reconsider now")
		}
	}
}

// TestAHurtHostileThiefRunsAndEscapes: fighting, and hurt below his flee
// threshold, he runs no matter what — afraid now, so out of sight he is
// gone.
func TestAHurtHostileThiefRunsAndEscapes(t *testing.T) {
	eng, w, p, thief := thiefScene(t)
	thief.Mind.Attitude = rules.Hostile
	thief.HP = thief.MaxHP / 5
	thief.Inventory = nil
	eng.NewCtx(w).MonsterTurn(thief)
	if p.HP != 1000 || thief.Mind.Attitude != rules.Afraid || (thief.X == 7 && thief.Y == 6) {
		t.Errorf("he should have run, afraid: player HP %d, %q at (%d,%d)", p.HP, thief.Mind.Attitude, thief.X, thief.Y)
	}
}

// TestAnAfraidThiefRunsForTheExit: in a room he is always in sight, so he
// heads out of it — through the door and down the corridor — and, once
// the player can no longer see him, he is gone.
func TestAnAfraidThiefRunsForTheExit(t *testing.T) {
	eng := rulestest.Engine(t)
	p := rulestest.New(t, eng, "player", 6, 9, 1000)
	thief := rulestest.New(t, eng, "thief", 8, 9, 20)
	thief.Mind.Attitude = rules.Afraid
	m := rulestest.TwoRooms()
	roomA, _ := m.RoomAt(6, 9)
	// The player sees all of room A, its doorway at (15,9), and nothing
	// else.
	w := &rules.World{Map: m, Player: p, Monsters: []*rules.Entity{thief}, Visible: func(x, y int) bool {
		r, in := m.RoomAt(x, y)
		return in && r.ID == roomA.ID || x == 15 && y == 9
	}}
	for turn := 0; turn < 40; turn++ {
		c := eng.NewCtx(w)
		c.MonsterTurn(thief)
		if len(c.Departed) == 1 {
			if w.PlayerSees(thief.X, thief.Y) {
				t.Fatalf("he vanished in plain sight at (%d,%d)", thief.X, thief.Y)
			}
			return
		}
	}
	t.Errorf("after 40 turns he is still here, at (%d,%d)", thief.X, thief.Y)
}

func TestAKilledThiefDropsWhatHeStole(t *testing.T) {
	eng, w, p, thief := thiefScene(t)
	stolen := p.Inventory[0]
	p.Inventory = p.Inventory[1:]
	thief.Inventory = append(thief.Inventory, stolen)
	thief.Dead = true
	found := false
	for _, it := range eng.NewCtx(w).Kill(thief) {
		found = found || it == stolen
	}
	if !found {
		t.Error("the stolen item should be on the floor with his corpse")
	}
}
