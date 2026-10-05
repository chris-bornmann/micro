package rules_test

import (
	"slices"
	"testing"

	"micro/server/internal/rules"
	"micro/server/internal/rules/rulestest"
)

func ring(t *testing.T, eng *rules.Engine, cursed bool) *rules.Entity {
	r := rulestest.New(t, eng, "ring_of_true_sight", 0, 0, 0)
	if cursed {
		r.AddTrait(&rules.TraitInstance{Trait: "cursed", Params: rules.Params{"damage": 0}})
	}
	return r
}

func ringsWorn(p *rules.Entity) int {
	n := 0
	for _, s := range []rules.Slot{rules.SlotRightHand, rules.SlotLeftHand} {
		if p.Equipped[s] != nil {
			n++
		}
	}
	return n
}

func TestRingsGoOnEachHandAndNeverMoreThanTwo(t *testing.T) {
	eng, w, p := scene(t)
	a, b, c3 := ring(t, eng, false), ring(t, eng, false), ring(t, eng, false)
	p.Inventory = []*rules.Entity{a, b, c3}
	c := eng.NewCtx(w)

	if res := c.Equip(p, a); res.Msg != "You put the ring of true sight on your right hand." {
		t.Errorf("%q", res.Msg)
	}
	if res := c.Equip(p, b); res.Msg != "You put the ring of true sight on your left hand." {
		t.Errorf("%q", res.Msg)
	}
	// Both hands full: the third swaps out the first that will come off.
	if res := c.Equip(p, c3); res.Refused || p.Equipped[rules.SlotRightHand] != c3 {
		t.Errorf("%+v", res)
	}
	if ringsWorn(p) != 2 || !slices.Equal(p.Inventory, []*rules.Entity{a}) {
		t.Errorf("worn %d, carried %d; want 2 worn and the swapped ring back in the pack", ringsWorn(p), len(p.Inventory))
	}
}

func TestACursedRingStaysOnItsFinger(t *testing.T) {
	eng, w, p := scene(t)
	cursed, plain, extra := ring(t, eng, true), ring(t, eng, false), ring(t, eng, false)
	p.Inventory = []*rules.Entity{cursed, plain, extra}
	c := eng.NewCtx(w)
	c.Equip(p, cursed) // right hand
	c.Equip(p, plain)  // left hand

	const stuck = "The ring of true sight won't come off your finger!"
	if res, _ := c.Drop(p, cursed); !res.Refused || res.Msg != stuck {
		t.Errorf("drop: %+v", res)
	}
	// A third ring passes over the cursed right hand and swaps the left.
	if res := c.Equip(p, extra); res.Refused || p.Equipped[rules.SlotLeftHand] != extra || p.Equipped[rules.SlotRightHand] != cursed {
		t.Errorf("third ring: %+v", res)
	}
	// With a cursed ring on each hand there is nowhere to put another.
	p.Equipped[rules.SlotLeftHand] = ring(t, eng, true)
	if res := c.Equip(p, plain); !res.Refused || res.Msg != stuck {
		t.Errorf("both cursed: %+v", res)
	}
	if ringsWorn(p) != 2 {
		t.Errorf("%d rings worn", ringsWorn(p))
	}
}

// TestSeeingInvisibleUndoesInvisibility: an unseen defender is no harder
// to hit for an attacker who can see it, and a monster that can see
// invisible chases an invisible player.
func TestSeeingInvisibleUndoesInvisibility(t *testing.T) {
	c, p, g := duel(t, nil)
	g.AddTrait(&rules.TraitInstance{Trait: "invisible", Params: rules.Params{"evasion": 20}})
	if got := c.HitChance(p, g); got != 30 {
		t.Fatalf("without the ring: %d%%, want 30%%", got)
	}
	p.Equipped = map[rules.Slot]*rules.Entity{rules.SlotRightHand: ring(t, rulestest.Engine(t), false)}
	if got := c.HitChance(p, g); got != 50 {
		t.Errorf("with the ring: %d%%, want 50%%", got)
	}

	// A goblin that sees invisible notices and closes on an invisible
	// player in its room.
	p.Equipped = nil
	p.AddTrait(&rules.TraitInstance{Trait: "invisible", Turns: 10})
	p.X, p.Y, g.X, g.Y = 6, 6, 9, 6
	g.AddTrait(&rules.TraitInstance{Trait: "see_invisible"})
	c.MonsterTurn(g)
	if !g.Mind.Aware || g.X != 8 {
		t.Errorf("should see the invisible player and close in; aware %v at x=%d", g.Mind.Aware, g.X)
	}
}
