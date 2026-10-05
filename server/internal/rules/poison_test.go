package rules_test

import (
	"strings"
	"testing"

	"micro/server/internal/rules"
	"micro/server/internal/rules/rulestest"
)

// stabbed is the player, struck once by a goblin wielding the poisoned
// dagger, on an engine where every blow lands.
func stabbed(t *testing.T, stamina int) (*rules.Engine, *rules.Ctx, *rules.Entity) {
	t.Helper()
	eng := rulestest.Engine(t)
	p := rulestest.New(t, eng, "player", 6, 6, 1000)
	p.Attrs.Stamina = stamina
	g := rulestest.New(t, eng, "goblin", 7, 6, 10)
	g.Equipped = map[rules.Slot]*rules.Entity{rules.SlotWeapon: rulestest.New(t, eng, "poisoned_dagger", 0, 0, 0)}
	c := eng.NewCtx(&rules.World{Map: rulestest.Room(), Player: p, Monsters: []*rules.Entity{g}})
	c.Melee(g, p)
	return eng, c, p
}

// TestStaminaFightsPoison: 4 a turn, less Stamina/5 rounded up; at
// Stamina 16+ there is nothing left and it doesn't take.
func TestStaminaFightsPoison(t *testing.T) {
	for _, tc := range []struct{ stamina, perTurn int }{{0, 4}, {5, 3}, {6, 2}, {15, 1}, {16, 0}, {20, 0}} {
		_, c, p := stabbed(t, tc.stamina)
		inst := p.FindTrait("poisoned")
		msgs := strings.Join(c.Messages(), " | ")
		if tc.perTurn == 0 {
			if inst != nil || !strings.HasSuffix(msgs, "You shrug off the poison.") {
				t.Errorf("Stamina %d should shrug it off: %v, %q", tc.stamina, inst, msgs)
			}
			continue
		}
		if inst == nil || inst.Params.Int("damage") != tc.perTurn || inst.Turns != 6 {
			t.Errorf("Stamina %d: want %d a turn for 6 turns, got %+v", tc.stamina, tc.perTurn, inst)
			continue
		}
		if !strings.HasSuffix(msgs, "You feel poison burning in your veins!") {
			t.Errorf("Stamina %d: %q", tc.stamina, msgs)
		}
	}
}

func TestPoisonTicksThenWearsOff(t *testing.T) {
	eng, _, p := stabbed(t, 0) // 4 a turn
	w := &rules.World{Map: rulestest.Room(), Player: p}
	hp := p.HP
	eng.NewCtx(w).Tick(p)
	if p.HP != hp || p.FindTrait("poisoned").Turns != 6 {
		t.Fatalf("the turn of the stab, the poison does nothing yet: HP %d -> %d", hp, p.HP)
	}
	for i := 1; i <= 6; i++ {
		w.Turn = i
		hp := p.HP
		c := eng.NewCtx(w)
		c.Tick(p)
		if hp-p.HP != 4 {
			t.Fatalf("tick %d took %d", i, hp-p.HP)
		}
		want := "You take 4 poison damage."
		if i == 6 {
			want += " | The poison wears off."
		}
		if got := strings.Join(c.Messages(), " | "); got != want {
			t.Errorf("tick %d: %q, want %q", i, got, want)
		}
	}
	if p.FindTrait("poisoned") != nil {
		t.Error("should have worn off")
	}
}

func TestPoisonCanKill(t *testing.T) {
	eng, _, p := stabbed(t, 0)
	p.HP = 3
	c := eng.NewCtx(&rules.World{Map: rulestest.Room(), Player: p, Turn: 1})
	c.Tick(p)
	if !p.Dead || strings.Join(c.Messages(), "") != "You take 4 poison damage and die." {
		t.Errorf("dead %v, %q", p.Dead, c.Messages())
	}
}

func TestRepoisoningKeepsTheWorseOfTheTwo(t *testing.T) {
	eng := rulestest.Engine(t)
	g := rulestest.New(t, eng, "goblin", 7, 6, 100)
	c := eng.NewCtx(&rules.World{Map: rulestest.Room(), Monsters: []*rules.Entity{g}})
	g.AddTrait(&rules.TraitInstance{Trait: "poisoned", Params: rules.Params{"damage": 1}, Turns: 9})
	poisonGoblin(t, c, g, 4, 3)
	inst := g.FindTrait("poisoned")
	if len(g.Traits) != 1 || inst.Params.Int("damage") != 4 || inst.Turns != 9 {
		t.Errorf("want one poisoning at 4 a turn with 9 turns left, got %d instances, %+v", len(g.Traits), inst)
	}
}

// poisonGoblin hits g (any monster) with a blade that poisons for damage
// over turns.
func poisonGoblin(t *testing.T, c *rules.Ctx, g *rules.Entity, damage, turns int) {
	t.Helper()
	p := rulestest.New(t, c.Engine, "player", 6, 6, 100)
	blade := rulestest.New(t, c.Engine, "fine_dagger", 0, 0, 0)
	blade.Traits = []*rules.TraitInstance{{Trait: "poisons", Params: rules.Params{"damage": damage, "turns": turns}}}
	p.Equipped = map[rules.Slot]*rules.Entity{rules.SlotWeapon: blade}
	g.HP = 100
	c.Melee(p, g)
}

func TestAMissOrAThrowDoesNotPoison(t *testing.T) {
	eng := rulestest.Engine(t)
	eng.HitRoll = rulestest.AlwaysMiss
	p := rulestest.New(t, eng, "player", 6, 6, 100)
	g := rulestest.New(t, eng, "goblin", 7, 6, 10)
	g.Equipped = map[rules.Slot]*rules.Entity{rules.SlotWeapon: rulestest.New(t, eng, "poisoned_dagger", 0, 0, 0)}
	c := eng.NewCtx(&rules.World{Map: rulestest.Room(), Player: p, Monsters: []*rules.Entity{g}})
	c.Melee(g, p)
	if p.FindTrait("poisoned") != nil {
		t.Error("a miss poisoned")
	}

	g.Equipped, g.X = nil, 8
	c.Throw(p, rulestest.Give(p, rulestest.New(t, eng, "poisoned_dagger", 0, 0, 0)), 1, 0)
	if g.FindTrait("poisoned") != nil {
		t.Error("a thrown poisoned dagger poisoned")
	}
}

func TestTheUndeadShrugOffPoison(t *testing.T) {
	for _, id := range []string{"mummy", "phantom"} {
		eng := rulestest.Engine(t)
		m := rulestest.New(t, eng, id, 7, 6, 100)
		c := eng.NewCtx(&rules.World{Map: rulestest.Room(), Monsters: []*rules.Entity{m}})
		poisonGoblin(t, c, m, 10, 6)
		if m.FindTrait("poisoned") != nil {
			t.Errorf("%s was poisoned", id)
		}
	}
}

func TestAHealingPotionCuresPoison(t *testing.T) {
	eng, _, p := stabbed(t, 0)
	p.HP = 10
	w := &rules.World{Map: rulestest.Room(), Player: p}
	res := eng.NewCtx(w).Use(p, rulestest.Give(p, rulestest.New(t, eng, "healing_potion", 0, 0, 0)))
	if p.FindTrait("poisoned") != nil || p.HP <= 10 {
		t.Errorf("poisoned %v, HP %d", p.FindTrait("poisoned") != nil, p.HP)
	}
	if !strings.HasPrefix(res.Msg, "You drink the healing potion and recover ") || !strings.HasSuffix(res.Msg, " The poison leaves your body.") {
		t.Errorf("%q", res.Msg)
	}

	// Healthy, the potion only heals, and says nothing of poison.
	res = eng.NewCtx(w).Use(p, rulestest.Give(p, rulestest.New(t, eng, "healing_potion", 0, 0, 0)))
	if strings.Contains(res.Msg, "poison") {
		t.Errorf("%q", res.Msg)
	}
}

// TestAKillingBlowDoesNotPoison: "The goblin dies! The goblin looks sick."
// made no sense — the dead aren't poisoned, and nothing says they are.
func TestAKillingBlowDoesNotPoison(t *testing.T) {
	eng := rulestest.Engine(t)
	p := rulestest.New(t, eng, "player", 6, 6, 100)
	p.Attrs.Strength = 100 // one blow kills
	p.Equipped = map[rules.Slot]*rules.Entity{rules.SlotWeapon: rulestest.New(t, eng, "poisoned_dagger", 0, 0, 0)}
	g := rulestest.New(t, eng, "goblin", 7, 6, 5)
	c := eng.NewCtx(&rules.World{Map: rulestest.Room(), Player: p, Monsters: []*rules.Entity{g}})

	c.Melee(p, g)
	if !g.Dead {
		t.Fatal("expected the goblin dead")
	}
	if g.FindTrait("poisoned") != nil {
		t.Error("a dead goblin was poisoned")
	}
	if msgs := c.Messages(); len(msgs) != 1 || !strings.HasSuffix(msgs[0], "The goblin dies!") {
		t.Errorf("only the killing blow should be told, got %q", msgs)
	}
}
