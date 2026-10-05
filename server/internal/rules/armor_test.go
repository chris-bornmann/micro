package rules_test

import (
	"strings"
	"testing"

	"micro/server/internal/rules"
	"micro/server/internal/rules/rulestest"
)

// wearing is a player in a room wearing the given items, each in the slot
// its template names.
func wearing(t *testing.T, ids ...string) (*rules.Engine, *rules.World, *rules.Entity) {
	t.Helper()
	eng := rulestest.Engine(t)
	p := rulestest.New(t, eng, "player", 6, 6, 1000)
	p.Equipped = map[rules.Slot]*rules.Entity{}
	for _, id := range ids {
		it := rulestest.New(t, eng, id, 0, 0, 0)
		p.Equipped[rules.EquipSlotsFor(it.T.Slot)[0]] = it
	}
	return eng, &rules.World{Map: rulestest.Room(), Player: p}, p
}

func TestArmorTakesOffPhysicalDamageOnly(t *testing.T) {
	eng, w, p := wearing(t, "chain_mail")
	c := eng.NewCtx(w)
	for typ, want := range map[rules.DamageType]int{
		rules.DamageSlashing: 3, rules.DamagePiercing: 3, rules.DamageBludgeoning: 3,
		rules.DamageFire: 5, rules.DamagePoison: 5,
	} {
		p.HP = 1000
		if got := c.Hurt(p, typ, 5).Damage; got != want {
			t.Errorf("5 %s through chain mail (2): %d, want %d", typ, got, want)
		}
	}
}

func TestArmorStacksAndStopsAtNothing(t *testing.T) {
	eng, w, p := wearing(t, "chain_mail", "helm", "gauntlets", "cloak_of_protection") // 2 + 1 + 1 + 1
	c := eng.NewCtx(w)
	if got := c.Hurt(p, rules.DamageSlashing, 8).Damage; got != 3 {
		t.Errorf("8 through 5 armor: %d", got)
	}
	if got := c.Hurt(p, rules.DamageSlashing, 2).Damage; got != 0 {
		t.Errorf("2 through 5 armor: %d, want 0", got)
	}
}

func TestCursedArmorMakesBlowsLandHarder(t *testing.T) {
	eng, w, p := wearing(t, "cursed_mail")
	if got := eng.NewCtx(w).Hurt(p, rules.DamageBludgeoning, 4).Damage; got != 5 {
		t.Errorf("4 through cursed mail: %d, want 5", got)
	}
}

// TestArmorIsATraitAnywhere: a ring can armor its wearer too, and armor
// comes off before resistance halves what is left.
func TestArmorIsATraitAnywhere(t *testing.T) {
	eng, w, p := wearing(t, "ring_of_true_sight", "helm")
	p.Equipped[rules.SlotRightHand].AddTrait(&rules.TraitInstance{Trait: "armor", Params: rules.Params{"amount": 2}})
	p.AddTrait(&rules.TraitInstance{Trait: "resist", Params: rules.Params{"type": "slashing", "multiplier": 0.5}})
	if got := eng.NewCtx(w).Hurt(p, rules.DamageSlashing, 9).Damage; got != 3 {
		t.Errorf("(9 - 3) × 0.5: %d, want 3", got)
	}
}

func TestAShieldMakesYouHarderToHitAndCursedArmorEasier(t *testing.T) {
	eng, w, p := wearing(t)
	g := rulestest.New(t, eng, "goblin", 7, 6, 10)
	base := eng.NewCtx(w).HitChance(g, p)
	_, _, shielded := wearing(t, "small_shield")
	_, _, cursed := wearing(t, "cursed_mail", "cursed_helm")
	if got := eng.NewCtx(w).HitChance(g, shielded); got != base-10 {
		t.Errorf("against a shield: %d%%, want %d%%", got, base-10)
	}
	if got := eng.NewCtx(w).HitChance(g, cursed); got != base+15 {
		t.Errorf("against cursed mail and helm: %d%%, want %d%%", got, base+15)
	}
}

func TestABlowThatArmorStopsGlancesOff(t *testing.T) {
	eng, w, p := wearing(t)
	p.AddTrait(&rules.TraitInstance{Trait: "armor", Params: rules.Params{"amount": 10}})
	g := rulestest.New(t, eng, "goblin", 7, 6, 10)
	g.Equipped = nil
	w.Monsters = []*rules.Entity{g}
	c := eng.NewCtx(w)
	if out := c.Melee(g, p); out.Damage != 0 || strings.Join(c.Messages(), "") != "The goblin hits you, but the blow glances off." {
		t.Errorf("%+v, %q", out, c.Messages())
	}
}
