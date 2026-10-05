package rules_test

import (
	"testing"

	"micro/server/internal/rules"
	"micro/server/internal/rules/rulestest"
)

// holding is a player carrying the given items in the pack, nothing
// equipped, with a goblin beside them.
func holding(t *testing.T, ids ...string) (*rules.Engine, *rules.World, *rules.Entity, []*rules.Entity) {
	t.Helper()
	eng := rulestest.Engine(t)
	p := rulestest.New(t, eng, "player", 6, 6, 1000)
	var items []*rules.Entity
	for _, id := range ids {
		items = append(items, rulestest.Give(p, rulestest.New(t, eng, id, 0, 0, 0)))
	}
	g := rulestest.New(t, eng, "goblin", 7, 6, 1000)
	g.Equipped = nil
	return eng, &rules.World{Map: rulestest.Room(), Player: p, Monsters: []*rules.Entity{g}}, p, items
}

func in(p *rules.Entity, slot rules.Slot, item *rules.Entity) bool { return p.Equipped[slot] == item }

func TestAShieldIsHeldInTheOffHand(t *testing.T) {
	eng, w, p, it := holding(t, "small_shield", "fine_dagger")
	c := eng.NewCtx(w)
	if res := c.Equip(p, it[0]); res.Msg != "You hold the small shield." || !in(p, rules.SlotOffHand, it[0]) {
		t.Errorf("%q", res.Msg)
	}
	c.Equip(p, it[1])
	if !in(p, rules.SlotWeapon, it[1]) || !in(p, rules.SlotOffHand, it[0]) {
		t.Error("a dagger and a shield together")
	}
}

// TestLightingATorchNeedsAFreeHand: the off hand first, then the main
// hand; with both full, no.
func TestLightingATorchNeedsAFreeHand(t *testing.T) {
	cases := []struct {
		name  string
		equip []string
		want  rules.Slot // "" refused
	}{
		{"empty-handed", nil, rules.SlotOffHand},
		{"with a weapon", []string{"fine_dagger"}, rules.SlotOffHand},
		{"with a shield", []string{"small_shield"}, rules.SlotWeapon},
		{"with both", []string{"fine_dagger", "small_shield"}, ""},
	}
	for _, tc := range cases {
		eng, w, p, it := holding(t, append([]string{"torch"}, tc.equip...)...)
		c := eng.NewCtx(w)
		for _, e := range it[1:] {
			c.Equip(p, e)
		}
		res := c.Use(p, it[0])
		if tc.want == "" {
			if !res.Refused || res.Msg != "You have no free hand to hold the torch." || p.Lit() != nil || p.Inventory[0] != it[0] {
				t.Errorf("%s: %+v", tc.name, res)
			}
			continue
		}
		if res.Refused || !in(p, tc.want, it[0]) || p.Lit() != it[0] || !eng.Reg.Has(p, "light") {
			t.Errorf("%s: lit into %v? %+v", tc.name, tc.want, res)
		}
	}
}

func TestOnlyOneTorchBurnsAtATime(t *testing.T) {
	eng, w, p, it := holding(t, "torch", "torch")
	c := eng.NewCtx(w)
	c.Use(p, it[0])
	if res := c.Use(p, it[1]); !res.Refused || res.Msg != "Your torch is already lit." {
		t.Errorf("%+v", res)
	}
}

// TestAWieldedTorchIsLitWhereItIs, and the shield still fits.
func TestAWieldedTorchIsLitWhereItIs(t *testing.T) {
	eng, w, p, it := holding(t, "torch", "small_shield")
	c := eng.NewCtx(w)
	c.Equip(p, it[0])
	c.Equip(p, it[1])
	c.Use(p, it[0])
	if !in(p, rules.SlotWeapon, it[0]) || !in(p, rules.SlotOffHand, it[1]) || p.Lit() != it[0] {
		t.Error("a shield and a wielded lit torch")
	}
}

// TestMakingWayPutsATorchOut: what takes a lit torch's hand sends it back
// to the pack, out.
func TestMakingWayPutsATorchOut(t *testing.T) {
	for _, other := range []string{"fine_dagger", "small_shield"} {
		eng, w, p, it := holding(t, "torch", other)
		c := eng.NewCtx(w)
		if other == "fine_dagger" {
			c.Equip(p, it[0]) // wielded
		}
		c.Use(p, it[0])
		res := c.Equip(p, it[1])
		if res.Msg[:len("You put out the torch.")] != "You put out the torch." || p.Lit() != nil || it[0].FindTrait("light_source") != nil || p.Inventory[0] != it[0] {
			t.Errorf("%s over a lit torch: %q", other, res.Msg)
		}
	}
}

// TestIronGlovesCoverTheHands: gauntlets and wielded iron gloves never go
// together; a cursed one keeps the other off.
func TestIronGlovesCoverTheHands(t *testing.T) {
	eng, w, p, it := holding(t, "iron_gloves", "gauntlets")
	c := eng.NewCtx(w)
	c.Equip(p, it[1])
	c.Equip(p, it[0])
	if !in(p, rules.SlotWeapon, it[0]) || p.Equipped[rules.SlotHands] != nil {
		t.Fatal("wielding iron gloves should take the gauntlets off")
	}
	c.Equip(p, it[1])
	if !in(p, rules.SlotHands, it[1]) || p.Weapon() != nil {
		t.Fatal("putting on gauntlets should put the iron gloves away")
	}
	it[1].AddTrait(&rules.TraitInstance{Trait: "cursed", Params: rules.Params{"damage": 0}})
	if res := c.Equip(p, it[0]); !res.Refused || p.Weapon() != nil {
		t.Errorf("cursed gauntlets should keep the iron gloves off: %+v", res)
	}
}

// TestATorchIsAClubAndLitItBurns: 1-2 unlit; lit, 1-2 more of fire.
func TestATorchIsAClubAndLitItBurns(t *testing.T) {
	eng, w, p, it := holding(t, "torch")
	g := w.Monsters[0]
	c := eng.NewCtx(w)
	c.Equip(p, it[0])
	if lo, hi := spread(func() int { g.HP = 1000; return eng.NewCtx(w).Melee(p, g).Damage }); lo != 1 || hi != 2 {
		t.Errorf("unlit: [%d, %d], want [1, 2]", lo, hi)
	}
	c.Use(p, it[0])
	if lo, hi := spread(func() int {
		g.HP = 1000
		it[0].FindTrait("light_source").Turns = 100
		return eng.NewCtx(w).Melee(p, g).Damage
	}); lo != 2 || hi != 4 {
		t.Errorf("lit: [%d, %d], want [2, 4]", lo, hi)
	}
	if g.FindTrait("on_fire") != nil {
		t.Error("a goblin is neither oily nor flammable")
	}
}

// TestALitTorchSetsAMummyAlight: fire, doubled, and burning after.
func TestALitTorchSetsAMummyAlight(t *testing.T) {
	eng, w, p, it := holding(t, "torch")
	m := rulestest.New(t, eng, "mummy", 7, 6, 1000)
	w.Monsters = []*rules.Entity{m}
	c := eng.NewCtx(w)
	c.Equip(p, it[0])
	c.Use(p, it[0])
	out := c.Melee(p, m)
	if out.Damage < 3 || out.Damage > 6 {
		t.Errorf("1-2 bludgeoning plus 1-2 fire doubled: %d", out.Damage)
	}
	if f := m.FindTrait("on_fire"); f == nil || f.Turns < 2 || f.Turns > 4 {
		t.Errorf("a flammable mummy should burn 2-4 turns: %+v", f)
	}
}

// TestAWieldedTorchStillLightsBurnsDownAndThrows.
func TestAWieldedTorchStillLightsBurnsDownAndThrows(t *testing.T) {
	eng, w, p, it := holding(t, "torch")
	c := eng.NewCtx(w)
	c.Equip(p, it[0])
	c.Use(p, it[0])
	if ts := rules.Throwable(p); len(ts) == 0 || ts[0] != it[0] {
		t.Error("a lit torch in hand can be thrown")
	}
	it[0].FindTrait("light_source").Turns = 2
	c.Tick(p)
	c.Tick(p)
	if p.Weapon() != nil || eng.Reg.Has(p, "light") {
		t.Error("it should have burned out in hand")
	}
}

func TestEquipmentChance(t *testing.T) {
	eng := rulestest.Engine(t)
	goblin := eng.Cat.Get("goblin")
	helms := 0
	for i := 0; i < 200; i++ {
		g := eng.Spawn(goblin, rulestest.Room(), i, nil)
		if h := g.Equipped[rules.SlotHead]; h != nil && h.T.ID == "helm" {
			helms++
		}
	}
	// 30%.
	if helms < 30 || helms > 95 {
		t.Errorf("%d of 200 goblins in a helm", helms)
	}
}
