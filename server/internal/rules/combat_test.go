package rules_test

import (
	"math/rand"
	"testing"
	"testing/fstest"

	"micro/server/internal/content"
	"micro/server/internal/rules"
	"micro/server/internal/rules/builtin"
	"micro/server/internal/rules/rulestest"
)

// spread runs f many times and reports the smallest and largest result, so
// a test can check a random range's bounds are both reachable and never
// exceeded.
func spread(f func() int) (lo, hi int) {
	lo, hi = 1<<30, -1<<30
	for i := 0; i < 400; i++ {
		v := f()
		lo, hi = min(lo, v), max(hi, v)
	}
	return lo, hi
}

// meleeSpread is the damage range goblin-killing blows actually land in:
// attacker hits a fresh 1000-HP defender of template defender, many times.
func meleeSpread(t *testing.T, eng *rules.Engine, attacker *rules.Entity, defender string) (int, int) {
	t.Helper()
	return spread(func() int {
		d := rulestest.New(t, eng, defender, 6, 5, 1000)
		w := &rules.World{Map: rulestest.Room(), Player: attacker, Monsters: []*rules.Entity{d}}
		return eng.NewCtx(w).Melee(attacker, d).Damage
	})
}

func TestBareHandsDoOneToTwo(t *testing.T) {
	eng := rulestest.Engine(t)
	p := rulestest.New(t, eng, "player", 5, 5, 10)
	if lo, hi := meleeSpread(t, eng, p, "goblin"); lo != 1 || hi != 2 {
		t.Errorf("bare hands: got [%d, %d], want [1, 2]", lo, hi)
	}
}

// TestFineDaggerAddsEnchantmentAndGoblinBonus: 1-4, +1 enchantment on
// both ends, and +1 more against goblins only.
func TestFineDaggerAddsEnchantmentAndGoblinBonus(t *testing.T) {
	eng := rulestest.Engine(t)
	p := rulestest.New(t, eng, "player", 5, 5, 10)
	p.Equipped = map[rules.Slot]*rules.Entity{rules.SlotWeapon: rulestest.New(t, eng, "fine_dagger", 0, 0, 0)}

	if lo, hi := meleeSpread(t, eng, p, "goblin"); lo != 3 || hi != 6 {
		t.Errorf("vs goblin: got [%d, %d], want [3, 6]", lo, hi)
	}
	if lo, hi := meleeSpread(t, eng, p, "mummy"); lo != 2 || hi != 5 {
		t.Errorf("vs mummy: got [%d, %d], want [2, 5]", lo, hi)
	}
}

// TestRustySwordCurseCanReachZero: 1-6 shifted by the curse's -1 is 0-5.
// The low end clamps at 0; a curse never heals.
func TestRustySwordCurseCanReachZero(t *testing.T) {
	eng := rulestest.Engine(t)
	p := rulestest.New(t, eng, "player", 5, 5, 10)
	p.Equipped = map[rules.Slot]*rules.Entity{rules.SlotWeapon: rulestest.New(t, eng, "rusty_sword", 0, 0, 0)}
	if lo, hi := meleeSpread(t, eng, p, "goblin"); lo != 0 || hi != 5 {
		t.Errorf("got [%d, %d], want [0, 5]", lo, hi)
	}
}

func TestMeleeAddsStrengthOverTen(t *testing.T) {
	eng := rulestest.Engine(t)
	p := rulestest.New(t, eng, "player", 5, 5, 10)
	p.Attrs.Strength = 25
	if lo, hi := meleeSpread(t, eng, p, "goblin"); lo != 3 || hi != 4 {
		t.Errorf("Strength 25 bare-handed: got [%d, %d], want [3, 4]", lo, hi)
	}
}

// TestThrownDamageDoesNotAddStrengthTwice: throw.Damage already counts
// Strength, so a thrown hit must not add Strength/10 again.
func TestThrownDamageDoesNotAddStrengthTwice(t *testing.T) {
	eng := rulestest.Engine(t)
	p := rulestest.New(t, eng, "player", 5, 5, 10)
	p.Attrs.Strength = 20
	lo, hi := spread(func() int {
		g := rulestest.New(t, eng, "goblin", 6, 5, 1000)
		w := &rules.World{Map: rulestest.Room(), Player: p, Monsters: []*rules.Entity{g}}
		return eng.NewCtx(w).ThrownHit(p, rulestest.New(t, eng, "fine_dagger", 0, 0, 0), g).Damage
	})
	// throw.Damage(weight 3, Strength 20) is [1, ceil(3/10)+20/10] = [1, 3].
	if lo != 1 || hi != 3 {
		t.Errorf("got [%d, %d], want [1, 3]", lo, hi)
	}
}

func TestMeleeKillsAtZeroHP(t *testing.T) {
	eng := rulestest.Engine(t)
	p := rulestest.New(t, eng, "player", 5, 5, 10)
	p.Attrs.Strength = 100 // +10: certain to kill
	g := rulestest.New(t, eng, "goblin", 6, 5, 5)
	out := eng.NewCtx(&rules.World{Map: rulestest.Room(), Player: p, Monsters: []*rules.Entity{g}}).Melee(p, g)
	if !out.Dead || !g.Dead || out.RemainingHP != g.HP || g.HP > 0 {
		t.Errorf("expected a dead goblin at or below 0 HP, got outcome %+v, HP %d", out, g.HP)
	}
}

// boost is a test-only trait: +amount to the main damage of any attack
// made by whoever wears it. It exists to show that gear which is not the
// weapon also gets a say in an attack.
type boost struct{}

func (boost) Params() []rules.ParamDef {
	return []rules.ParamDef{{Name: "amount", Kind: rules.ParamInt, Required: true}}
}

func (boost) Modify(q *rules.Query, h rules.Holder) {
	if q.Kind == rules.QueryDamageDealt && q.Primary && h.Role == rules.RoleAttackerGear {
		q.Add += h.Params().Int("amount")
	}
}

const aggregationContent = `
player:
  category: player
  name: Player
  glyph: "@"
  color: [255, 255, 255]
  weight: 100
  hp: 10
  attributes: 0
  attack: {name: hands, damage: {bludgeoning: 1}}
gauntlets:
  category: armor
  name: gauntlets
  glyph: "["
  color: [1, 1, 1]
  weight: 1
  slot: hands
  traits: [{trait: boost, amount: 1}]
maul:
  category: weapon
  name: maul
  glyph: ")"
  color: [1, 1, 1]
  weight: 1
  slot: weapon
  damage: {bludgeoning: 4}
  traits: [{trait: enchant, amount: 2}, {trait: bonus_vs, kind: troll, amount: 1}]
plate:
  category: armor
  name: plate
  glyph: "["
  color: [1, 1, 1]
  weight: 1
  slot: body
  traits: [{trait: resist, type: physical}]
troll:
  category: monster
  name: Troll
  glyph: t
  color: [1, 1, 1]
  weight: 1
  level: 1
  hp: 100
  attributes: 0
  attack: {name: fists, damage: {bludgeoning: 1}}
  ai: {behavior: chase}
  spawn: {model: fixed, count: 1}
  leaves_corpse: false
  traits: [{trait: resist, type: physical}]
`

// TestAttackConsultsEveryParticipant is the engine's central promise: the
// attacker's gear, the weapon, the defender and the defender's armor all
// count before damage is decided. Maul 4, +2 enchantment, +1 against
// trolls, +1 from gauntlets = 8; the troll's hide and its plate each halve
// physical damage — which covers the maul's bludgeoning — so it takes 2. Every range here is a single value, so
// the result is exact.
func TestAttackConsultsEveryParticipant(t *testing.T) {
	reg := builtin.Registry()
	reg.AddTrait("boost", boost{})
	cat, err := content.LoadFS(fstest.MapFS{"c.yaml": {Data: []byte(aggregationContent)}}, reg)
	if err != nil {
		t.Fatal(err)
	}
	eng := &rules.Engine{Reg: reg, Cat: cat, HitRoll: rulestest.AlwaysHit}

	for trial := 0; trial < 20; trial++ {
		p := rulestest.New(t, eng, "player", 5, 5, 10)
		troll := rulestest.New(t, eng, "troll", 6, 5, 100)
		gear := []*rules.Entity{rulestest.New(t, eng, "gauntlets", 0, 0, 0), rulestest.New(t, eng, "maul", 0, 0, 0)}
		p.Equipped = map[rules.Slot]*rules.Entity{rules.SlotHands: gear[0], rules.SlotWeapon: gear[1]}
		troll.Equipped = map[rules.Slot]*rules.Entity{rules.SlotBody: rulestest.New(t, eng, "plate", 0, 0, 0)}

		// Shuffle every trait list: the order traits are visited in must
		// never change the answer.
		for _, e := range []*rules.Entity{p, troll, gear[0], gear[1], troll.Equipped[rules.SlotBody]} {
			rand.Shuffle(len(e.Traits), func(i, j int) { e.Traits[i], e.Traits[j] = e.Traits[j], e.Traits[i] })
		}

		w := &rules.World{Map: rulestest.Room(), Player: p, Monsters: []*rules.Entity{troll}}
		if got := eng.NewCtx(w).Melee(p, troll).Damage; got != 2 {
			t.Fatalf("trial %d: damage = %d, want 2", trial, got)
		}
	}
}

func TestIronGlovesDoubleAFist(t *testing.T) {
	eng := rulestest.Engine(t)
	p := rulestest.New(t, eng, "player", 5, 5, 10)
	fistLo, fistHi := meleeSpread(t, eng, p, "goblin")
	p.Equipped = map[rules.Slot]*rules.Entity{rules.SlotWeapon: rulestest.New(t, eng, "iron_gloves", 0, 0, 0)}
	if lo, hi := meleeSpread(t, eng, p, "goblin"); lo != 2*fistLo || hi != 2*fistHi {
		t.Errorf("iron gloves: [%d, %d], want double a fist's [%d, %d]", lo, hi, fistLo, fistHi)
	}
}
