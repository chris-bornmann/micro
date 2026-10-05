package rules_test

import (
	"testing"

	"micro/server/internal/rules"
	"micro/server/internal/rules/rulestest"
)

// cloaked is a goblin carrying an invisible trait with the given
// parameters, alone on a map with the player.
func cloaked(t *testing.T, visibleBelow, periodic int) (*rules.Engine, *rules.World, *rules.Entity) {
	t.Helper()
	eng := rulestest.Engine(t)
	g := rulestest.New(t, eng, "goblin", 10, 10, 10)
	g.AddTrait(&rules.TraitInstance{Trait: "invisible", Params: rules.Params{"visible_below": visibleBelow, "periodic_visible": periodic}})
	p := rulestest.New(t, eng, "player", 6, 6, 20)
	return eng, &rules.World{Map: rulestest.Room(), Player: p, Monsters: []*rules.Entity{g}}, g
}

func TestInvisibleShowsOnceHPDropsBelowTheThreshold(t *testing.T) {
	eng, _, g := cloaked(t, 4, 0)
	g.HP = 4
	if !eng.Reg.Has(g, "invisible") {
		t.Error("at 4 HP, not below 4: still invisible")
	}
	g.HP = 3
	if eng.Reg.Has(g, "invisible") {
		t.Error("at 3 HP it should show")
	}
}

func TestInvisibleWithAZeroThresholdNeverShowsWhileAlive(t *testing.T) {
	eng, _, g := cloaked(t, 0, 0)
	g.HP = 1
	if !eng.Reg.Has(g, "invisible") {
		t.Error("visible_below 0 should keep it hidden down to its last HP")
	}
}

// visibleTurns ticks g n times and counts the turns it could be seen on,
// checking along the way that asking again within a turn never changes
// the answer.
func visibleTurns(t *testing.T, eng *rules.Engine, w *rules.World, g *rules.Entity, n int) int {
	t.Helper()
	seen := 0
	for i := 0; i < n; i++ {
		eng.NewCtx(w).Tick(g)
		v := !eng.Reg.Has(g, "invisible")
		for j := 0; j < 5; j++ {
			if !eng.Reg.Has(g, "invisible") != v {
				t.Fatal("visibility changed within a single turn")
			}
		}
		if v {
			seen++
		}
	}
	return seen
}

func TestPeriodicVisibilityIsRolledOncePerTurn(t *testing.T) {
	eng, w, g := cloaked(t, 0, 0)
	if n := visibleTurns(t, eng, w, g, 200); n != 0 {
		t.Errorf("periodic_visible 0: seen on %d of 200 turns", n)
	}
	eng, w, g = cloaked(t, 0, 100)
	if n := visibleTurns(t, eng, w, g, 200); n != 200 {
		t.Errorf("periodic_visible 100: seen on %d of 200 turns", n)
	}
	eng, w, g = cloaked(t, 0, 10)
	if n := visibleTurns(t, eng, w, g, 2000); n < 100 || n > 300 {
		t.Errorf("periodic_visible 10: seen on %d of 2000 turns, want about 200", n)
	}
}

// TestAGlimpsedMonsterIsNotReportedAsInvisible: the status view agrees
// with what others can see.
func TestAGlimpsedMonsterIsNotReportedAsInvisible(t *testing.T) {
	eng, w, g := cloaked(t, 0, 100)
	has := func() bool {
		for _, f := range eng.Reg.Status(g) {
			if f.Label == "Invisible" {
				return true
			}
		}
		return false
	}
	if !has() {
		t.Error("before its first roll it is hidden, and says so")
	}
	eng.NewCtx(w).Tick(g)
	if has() {
		t.Error("glimpsed this turn, it should not be listed as invisible")
	}
}

func TestAnInvisibleMonstersCorpseIsVisible(t *testing.T) {
	eng, w, g := cloaked(t, 0, 0)
	g.Dead = true
	for _, it := range eng.NewCtx(w).Kill(g) {
		if eng.Reg.Has(it, "invisible") {
			t.Errorf("%s is invisible", it.Name)
		}
	}
}
