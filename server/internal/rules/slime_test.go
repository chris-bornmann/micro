package rules_test

import (
	"testing"

	"micro/server/internal/rules"
	"micro/server/internal/rules/rulestest"
)

// againstSlime is the spread of damage a fresh slime takes from the
// player, carrying weapon (or bare-handed if ""), over many blows.
func againstSlime(t *testing.T, weapon string) (int, int) {
	t.Helper()
	eng := rulestest.Engine(t)
	p := rulestest.New(t, eng, "player", 5, 5, 10)
	if weapon != "" {
		p.Equipped = map[rules.Slot]*rules.Entity{rules.SlotWeapon: rulestest.New(t, eng, weapon, 0, 0, 0)}
	}
	return meleeSpread(t, eng, p, "slime")
}

// TestTheSlimeCaresHowItIsHit: blades pass through it, a point does a
// little better, a fist does best — all from resist and vulnerable traits
// on damage types.
func TestTheSlimeCaresHowItIsHit(t *testing.T) {
	cases := []struct {
		weapon string
		lo, hi int
	}{
		{"", 2, 3},            // fists: 1-2 bludgeoning x1.5
		{"fine_dagger", 1, 3}, // 2-5 piercing (enchanted) x0.5
		{"rusty_sword", 0, 1}, // 0-5 slashing (cursed) x0.25
		{"iron_gloves", 3, 6}, // 2-4 bludgeoning x1.5
	}
	for _, tc := range cases {
		if lo, hi := againstSlime(t, tc.weapon); lo != tc.lo || hi != tc.hi {
			t.Errorf("%q against a slime: [%d, %d], want [%d, %d]", tc.weapon, lo, hi, tc.lo, tc.hi)
		}
	}
}

func TestTheSlimeBoils(t *testing.T) {
	eng := rulestest.Engine(t)
	s := rulestest.New(t, eng, "slime", 7, 6, 1000)
	w := &rules.World{Map: rulestest.Room(), Player: rulestest.New(t, eng, "player", 5, 5, 10), Monsters: []*rules.Entity{s}}
	lo, hi := spread(func() int {
		s.HP, s.Healing = 1000, 0 // only the burn: no healing built up
		s.AddTrait(&rules.TraitInstance{Trait: "on_fire", Turns: 5})
		eng.NewCtx(w).Tick(s)
		s.RemoveTrait(s.FindTrait("on_fire"))
		return 1000 - s.HP
	})
	if lo != 6 || hi != 12 {
		t.Errorf("burning a slime: [%d, %d], want [6, 12] (2-4 fire x3)", lo, hi)
	}
}

// TestAThrownItemIsUntypedPhysical: the slime's blade and point
// resistances don't touch a throw, and nor does its weakness to blows.
func TestAThrownItemIsUntypedPhysical(t *testing.T) {
	eng := rulestest.Engine(t)
	p := rulestest.New(t, eng, "player", 6, 6, 10)
	p.Attrs.Strength = 20
	lo, hi := spread(func() int {
		s := rulestest.New(t, eng, "slime", 7, 6, 1000)
		w := &rules.World{Map: rulestest.Room(), Player: p, Monsters: []*rules.Entity{s}}
		return eng.NewCtx(w).ThrownHit(p, rulestest.New(t, eng, "rusty_sword", 0, 0, 0), s).Damage
	})
	// throw.Damage(weight 6, Strength 20) is [1, 1+2]: unscaled.
	if lo != 1 || hi != 3 {
		t.Errorf("thrown sword at a slime: [%d, %d], want [1, 3]", lo, hi)
	}
}

func TestPhysicalCoversTheThreePhysicalTypes(t *testing.T) {
	for _, typ := range []rules.DamageType{rules.DamageSlashing, rules.DamagePiercing, rules.DamageBludgeoning, rules.DamagePhysical} {
		if !rules.DamagePhysical.Covers(typ) {
			t.Errorf("physical should cover %s", typ)
		}
	}
	if rules.DamagePhysical.Covers(rules.DamageFire) {
		t.Error("physical does not cover fire")
	}
	if rules.DamageSlashing.Covers(rules.DamageBludgeoning) || rules.DamageSlashing.Covers(rules.DamagePhysical) {
		t.Error("slashing covers only slashing")
	}
}
