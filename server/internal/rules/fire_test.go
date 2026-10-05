package rules_test

import (
	"strings"
	"testing"

	"micro/server/internal/rules"
	"micro/server/internal/rules/rulestest"
)

// throwAt throws item from p at the monster one tile to p's right.
func throwAt(eng *rules.Engine, w *rules.World, p, item *rules.Entity) rules.ThrowResult {
	return eng.NewCtx(w).Throw(p, item, 1, 0)
}

func oilyScene(t *testing.T, target string) (*rules.Engine, *rules.World, *rules.Entity, *rules.Entity) {
	eng := rulestest.Engine(t)
	p := rulestest.New(t, eng, "player", 6, 6, 20)
	m := rulestest.New(t, eng, target, 7, 6, 1000)
	return eng, &rules.World{Map: rulestest.Room(), Player: p, Monsters: []*rules.Entity{m}}, p, m
}

func TestFlaskHitAddsOneStackOfOilPerHit(t *testing.T) {
	eng, w, p, g := oilyScene(t, "goblin")
	for want := 1; want <= 2; want++ {
		res := throwAt(eng, w, p, rulestest.Give(p, rulestest.New(t, eng, "flask_of_oil", 0, 0, 0)))
		if res.Target != g || res.Landed != nil {
			t.Fatalf("the flask should hit the goblin and shatter, got %+v", res)
		}
		if got := g.Stacks("oily"); got != want {
			t.Errorf("after %d flasks oiliness = %d", want, got)
		}
	}
	if len(g.Traits) != 1 {
		t.Errorf("oil should stack on one instance, got %d instances", len(g.Traits))
	}
}

func TestLitTorchDoesNotIgniteSomethingDry(t *testing.T) {
	eng, w, p, g := oilyScene(t, "goblin")
	torch := rulestest.Give(p, rulestest.New(t, eng, "torch", 0, 0, 0))
	rules.Light(p, torch, 10)
	throwAt(eng, w, p, torch)
	if g.FindTrait("on_fire") != nil {
		t.Error("a dry goblin must not catch fire")
	}
}

func TestUnlitTorchDoesNotIgnite(t *testing.T) {
	eng, w, p, g := oilyScene(t, "goblin")
	g.AddStacks("oily", 1)
	res := throwAt(eng, w, p, rulestest.Give(p, rulestest.New(t, eng, "torch", 0, 0, 0)))
	if g.FindTrait("on_fire") != nil {
		t.Error("an unlit torch must not ignite anything")
	}
	if res.Landed == nil {
		t.Error("an unlit torch is an ordinary item and should land")
	}
}

// TestLitTorchIgnitesOilyTargetAfterAGraceTurn pins the whole burn cycle:
// 1-3 turns per stack of oil; no damage on the turn it catches; 2-4 fire
// damage each turn after; "the flames die out" on the last.
func TestLitTorchIgnitesOilyTargetAfterAGraceTurn(t *testing.T) {
	eng, w, p, g := oilyScene(t, "goblin")
	g.AddStacks("oily", 2)
	torch := rulestest.Give(p, rulestest.New(t, eng, "torch", 0, 0, 0))
	rules.Light(p, torch, 10)

	res := throwAt(eng, w, p, torch)
	if res.Landed != nil || p.Lit() != nil {
		t.Error("a thrown lit torch goes out and is gone")
	}
	fire := g.FindTrait("on_fire")
	if fire == nil {
		t.Fatal("an oily goblin hit by a lit torch should be on fire")
	}
	if fire.Turns < 2 || fire.Turns > 6 {
		t.Errorf("two stacks of oil should burn 2-6 turns, got %d", fire.Turns)
	}
	turns := fire.Turns

	hp := g.HP
	c := eng.NewCtx(w)
	c.Tick(g)
	if g.HP != hp || fire.Turns != turns {
		t.Fatalf("the turn it catches, the fire does nothing yet: no damage, no countdown (HP %d->%d, turns %d->%d)", hp, g.HP, turns, fire.Turns)
	}
	for i := 0; i < turns; i++ {
		w.Turn++
		before := g.HP
		c = eng.NewCtx(w)
		c.Tick(g)
		if d := before - g.HP; d < 2 || d > 4 {
			t.Errorf("burn tick %d did %d damage, want 2-4", i, d)
		}
		msg := strings.Join(c.Messages(), " | ")
		last := i == turns-1
		if last != strings.Contains(msg, "flames die out") || !strings.HasPrefix(msg, "Goblin takes ") {
			t.Errorf("tick %d message %q", i, msg)
		}
	}
	if g.FindTrait("on_fire") != nil {
		t.Error("the fire should be out")
	}
}

func TestReignitionExtendsWithoutAnotherGraceTurn(t *testing.T) {
	eng, w, p, g := oilyScene(t, "goblin")
	g.AddStacks("oily", 1)
	for i := 0; i < 2; i++ {
		torch := rulestest.Give(p, rulestest.New(t, eng, "torch", 0, 0, 0))
		rules.Light(p, torch, 10)
		throwAt(eng, w, p, torch)
		w.Turn++
	}
	fire := g.FindTrait("on_fire")
	if fire.From != 1 || fire.Turns < 2 || fire.Turns > 6 {
		t.Errorf("second ignition should add 1-3 turns without moving the start, got %+v", fire)
	}
}

// TestMummyTakesDoubleFireDamage: the mummy's vulnerability to fire is a
// trait in its template, and burning is fire damage, so it burns for 4-8.
func TestMummyTakesDoubleFireDamage(t *testing.T) {
	eng, w, _, m := oilyScene(t, "mummy")
	lo, hi := spread(func() int {
		m.HP, m.Healing = 1000, 0 // only the burn: no healing built up
		if f := m.FindTrait("on_fire"); f != nil {
			m.RemoveTrait(f)
		}
		m.AddTrait(&rules.TraitInstance{Trait: "on_fire", Turns: 5})
		eng.NewCtx(w).Tick(m)
		return 1000 - m.HP
	})
	if lo != 4 || hi != 8 {
		t.Errorf("mummy burn damage spread [%d, %d], want [4, 8]", lo, hi)
	}
}

func TestBurningCanKill(t *testing.T) {
	eng, w, _, g := oilyScene(t, "goblin")
	g.HP = 1
	g.AddTrait(&rules.TraitInstance{Trait: "on_fire", Turns: 3})
	eng.NewCtx(w).Tick(g)
	if !g.Dead {
		t.Error("2+ fire damage should kill a goblin on 1 HP")
	}
}

func TestBurnDamageMakesAGoblinFlee(t *testing.T) {
	eng, w, _, g := oilyScene(t, "goblin")
	g.HP, g.MaxHP = 5, 10 // 2-4 damage leaves 1-3; the threshold is 1
	for !g.Mind.Fleeing {
		g.HP = 5
		g.Traits = nil
		g.AddTrait(&rules.TraitInstance{Trait: "on_fire", Turns: 3})
		eng.NewCtx(w).Tick(g)
		if g.HP > 1 {
			if g.Mind.Fleeing {
				t.Fatal("fled above the threshold")
			}
			continue
		}
		if !g.Mind.Fleeing || g.Mind.FleeRoom != -1 {
			t.Fatalf("burning to the threshold should start a flee with no destination yet, got %+v", g.Mind)
		}
	}
}

func TestFlaskOnOpenFloorSpillsOilAndOnAWallDoesNot(t *testing.T) {
	eng := rulestest.Engine(t)
	p := rulestest.New(t, eng, "player", 6, 6, 20)
	w := &rules.World{Map: rulestest.Room(), Player: p}

	res := eng.NewCtx(w).Throw(p, rulestest.Give(p, rulestest.New(t, eng, "flask_of_oil", 0, 0, 0)), 1, 0)
	if res.Landed != nil || len(w.Oily) != 1 || strings.Join(res.Notes, "") != "It shatters, spreading oil across the floor!" {
		t.Errorf("flask on open floor: landed %v, oily %v, notes %q", res.Landed, w.Oily, res.Notes)
	}

	p.X = 14 // against the east wall
	w.Oily = nil
	res = eng.NewCtx(w).Throw(p, rulestest.Give(p, rulestest.New(t, eng, "flask_of_oil", 0, 0, 0)), 1, 0)
	if res.Landed != nil || len(w.Oily) != 0 || strings.Join(res.Notes, "") != "It shatters against the wall!" {
		t.Errorf("flask into a wall: landed %v, oily %v, notes %q", res.Landed, w.Oily, res.Notes)
	}
}
