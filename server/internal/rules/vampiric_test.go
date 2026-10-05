package rules_test

import (
	"strconv"
	"testing"

	"micro/server/internal/rules"
	"micro/server/internal/rules/rulestest"
)

// wounded is a vampire far below its maximum HP, next to a sturdy player.
func wounded(t *testing.T) (*rules.Engine, *rules.World, *rules.Entity, *rules.Entity) {
	t.Helper()
	eng := rulestest.Engine(t)
	v := rulestest.New(t, eng, "vampire", 7, 6, 100)
	v.HP = 10
	// The real vampire's bites describe its feeding and keep the drain
	// quiet; these tests want the trait's own default line.
	v.Traits[0].Params = rules.Params{"drain_percent": 20}
	p := rulestest.New(t, eng, "player", 6, 6, 10000)
	return eng, &rules.World{Map: rulestest.Room(), Player: p, Monsters: []*rules.Entity{v}}, v, p
}

// drainOf is what drain_percent 20 gives back for damage d: a fifth,
// rounded to the nearest point.
func drainOf(d int) int { return (d*20 + 50) / 100 }

func TestAVampiresBiteHealsItByAFifthOfTheDamage(t *testing.T) {
	for i := 0; i < 100; i++ {
		eng, w, v, p := wounded(t)
		c := eng.NewCtx(w)
		out := c.Melee(v, p)
		if out.Damage < 3 || out.Damage > 9 {
			t.Fatalf("fangs did %d, want 3-8 plus at most 1 for Strength", out.Damage)
		}
		want := drainOf(out.Damage)
		if v.HP != 10+want {
			t.Fatalf("%d damage should drain %d, vampire went 10 -> %d", out.Damage, want, v.HP)
		}
		// The bite is described first; the drain, if any, follows it.
		msgs := c.Messages()
		drained := len(msgs) == 2 && msgs[1] == "Vampire drains "+strconv.Itoa(want)+" HP."
		if (want > 0) != drained {
			t.Fatalf("%d drained, messages %q", want, msgs)
		}
	}
}

func TestDrainNeverHealsPastMaximumHP(t *testing.T) {
	eng, w, v, p := wounded(t)
	v.HP = v.MaxHP
	c := eng.NewCtx(w)
	c.Melee(v, p)
	if v.HP != v.MaxHP || len(c.Messages()) != 1 {
		t.Errorf("a vampire at full health gains nothing, and says nothing beyond the bite: HP %d/%d, %q", v.HP, v.MaxHP, c.Messages())
	}
}

// vampiricSword is a rusty sword with half its damage flowing back —
// the trait on a weapon rather than a monster — minus its curse, so its
// damage is simply 1-6.
func vampiricSword(t *testing.T, eng *rules.Engine) *rules.Entity {
	s := rulestest.New(t, eng, "rusty_sword", 0, 0, 0)
	s.Traits = []*rules.TraitInstance{{Trait: "vampiric_attack", Params: rules.Params{"drain_percent": 50}}}
	return s
}

func TestAVampiricWeaponDrainsForWhoeverWieldsIt(t *testing.T) {
	eng := rulestest.Engine(t)
	p := rulestest.New(t, eng, "player", 6, 6, 1000)
	p.HP = 1
	p.Equipped = map[rules.Slot]*rules.Entity{rules.SlotWeapon: vampiricSword(t, eng)}
	g := rulestest.New(t, eng, "goblin", 7, 6, 1000)
	c := eng.NewCtx(&rules.World{Map: rulestest.Room(), Player: p, Monsters: []*rules.Entity{g}})

	out := c.Melee(p, g)
	want := (out.Damage*50 + 50) / 100
	if p.HP != 1+want {
		t.Errorf("%d damage at 50%% should drain %d, player HP 1 -> %d", out.Damage, want, p.HP)
	}
	if msgs := c.Messages(); len(msgs) != 2 || msgs[1] != "You drain "+strconv.Itoa(want)+" HP." {
		t.Errorf("messages %q", msgs)
	}
}

func TestAThrownVampiricWeaponDrainsNothing(t *testing.T) {
	eng := rulestest.Engine(t)
	p := rulestest.New(t, eng, "player", 6, 6, 1000)
	p.HP = 1
	p.Attrs.Strength = 20
	g := rulestest.New(t, eng, "goblin", 7, 6, 1000)
	w := &rules.World{Map: rulestest.Room(), Player: p, Monsters: []*rules.Entity{g}}
	res := eng.NewCtx(w).Throw(p, rulestest.Give(p, vampiricSword(t, eng)), 1, 0)
	if res.Target != g || res.Outcome.Damage == 0 || p.HP != 1 {
		t.Errorf("hit %v for %d; thrower HP %d, want 1", res.Target != nil, res.Outcome.Damage, p.HP)
	}
}

// TestAnEmptyDrainMessageSaysNothing: the vampire as written has
// message: "" on its drain, so only its bite is described.
func TestAnEmptyDrainMessageSaysNothing(t *testing.T) {
	eng := rulestest.Engine(t)
	v := rulestest.New(t, eng, "vampire", 7, 6, 100)
	v.HP = 10
	p := rulestest.New(t, eng, "player", 6, 6, 10000)
	c := eng.NewCtx(&rules.World{Map: rulestest.Room(), Player: p, Monsters: []*rules.Entity{v}})
	out := c.Melee(v, p)
	if v.HP != 10+drainOf(out.Damage) || len(c.Messages()) != 1 {
		t.Errorf("drained to %d, messages %q", v.HP, c.Messages())
	}
}
