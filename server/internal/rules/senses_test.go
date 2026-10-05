package rules_test

import (
	"testing"

	"micro/server/internal/rules"
	"micro/server/internal/rules/rulestest"
)

func TestProvocationIsRememberedHitOrMiss(t *testing.T) {
	for _, roll := range []func() int{rulestest.AlwaysHit, rulestest.AlwaysMiss} {
		eng := rulestest.Engine(t)
		eng.HitRoll = roll
		p := rulestest.New(t, eng, "player", 6, 6, 100)
		g := rulestest.New(t, eng, "goblin", 7, 6, 100)
		c := eng.NewCtx(&rules.World{Map: rulestest.Room(), Player: p, Monsters: []*rules.Entity{g}})
		if g.Mind.Provoked() {
			t.Fatal("provoked before anything happened")
		}
		c.Melee(p, g)
		if !g.Mind.Provoked() {
			t.Error("attacking a monster should provoke it, hit or miss")
		}
		c.Melee(g, p)
		if p.Mind.Provoked() {
			t.Error("the player is never 'provoked'")
		}
	}
}

func TestAThrowProvokesToo(t *testing.T) {
	eng := rulestest.Engine(t)
	p := rulestest.New(t, eng, "player", 6, 6, 100)
	p.Attrs.Strength = 10 // enough to throw it two tiles
	g := rulestest.New(t, eng, "goblin", 8, 6, 100)
	c := eng.NewCtx(&rules.World{Map: rulestest.Room(), Player: p, Monsters: []*rules.Entity{g}})
	c.Throw(p, rulestest.Give(p, rulestest.New(t, eng, "fine_dagger", 0, 0, 0)), 1, 0)
	if !g.Mind.Provoked() {
		t.Error("a thrown dagger should provoke")
	}
}

func TestNoticeRemembersWhereThePlayerWas(t *testing.T) {
	eng := rulestest.Engine(t)
	p := rulestest.New(t, eng, "player", 6, 6, 100)
	g := rulestest.New(t, eng, "goblin", 10, 10, 100)
	c := eng.NewCtx(&rules.World{Map: rulestest.TwoRooms(), Player: p, Monsters: []*rules.Entity{g}})
	if !c.Notice(g) || !g.Mind.Seen || g.Mind.LastSeen.X != 6 || g.Mind.LastSeen.Y != 6 {
		t.Fatalf("should see the player in its room: %+v", g.Mind)
	}
	p.X, p.Y = 32, 7 // into the other room
	if c.Notice(g) {
		t.Error("cannot see into another room")
	}
	if g.Mind.LastSeen.X != 6 {
		t.Error("out of sight, it should remember where the player was last seen")
	}
}

func TestAttitudeStartsAsTheTemplateSays(t *testing.T) {
	eng := rulestest.Engine(t)
	if g := rulestest.New(t, eng, "goblin", 0, 0, 1); g.Mind.Attitude != rules.Hostile {
		t.Errorf("a goblin should start hostile, got %q", g.Mind.Attitude)
	}
}

func TestPlayerSeesDefaultsToEverything(t *testing.T) {
	w := &rules.World{Map: rulestest.Room()}
	if !w.PlayerSees(1, 1) {
		t.Error("with no Visible, everything is in sight")
	}
	w.Visible = func(x, y int) bool { return x == 1 }
	if !w.PlayerSees(1, 9) || w.PlayerSees(2, 2) {
		t.Error("Visible should decide")
	}
}
