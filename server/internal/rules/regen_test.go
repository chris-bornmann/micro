package rules_test

import (
	"strings"
	"testing"

	"micro/server/internal/rules"
	"micro/server/internal/rules/rulestest"
)

// hurt is a 100-HP creature of template id down to 50, with Strength and
// Stamina summing to str+sta.
func hurt(t *testing.T, eng *rules.Engine, id string, str, sta int) *rules.Entity {
	t.Helper()
	e := rulestest.New(t, eng, id, 6, 6, 100)
	e.HP, e.Attrs.Strength, e.Attrs.Stamina = 50, str, sta
	return e
}

// ticks advances e n turns and returns its HP gain.
func ticks(eng *rules.Engine, e *rules.Entity, n int) int {
	w := &rules.World{Map: rulestest.Room(), Player: e}
	if !e.IsPlayer() {
		w = &rules.World{Map: rulestest.Room(), Monsters: []*rules.Entity{e}}
	}
	before := e.HP
	for i := 0; i < n; i++ {
		w.Turn++
		eng.NewCtx(w).Tick(e)
	}
	return e.HP - before
}

func TestTheNaturalRateRisesWithStrengthAndStaminaToACap(t *testing.T) {
	eng := rulestest.Engine(t)
	for sum, want := range map[int]int{0: 5, 8: 13, 25: 30, 40: 45, 55: 60, 70: 60} {
		e := hurt(t, eng, "player", sum, 0)
		if got := rules.NaturalRegeneration(e); got != want {
			t.Errorf("Strength + Stamina %d: %d points a turn, want %d", sum, got, want)
		}
	}
}

// TestATypicalPlayerHealsEveryTwentyTurns: 25 is 30 points a turn, 600 to
// the HP — one HP on exactly the 20th turn.
func TestATypicalPlayerHealsEveryTwentyTurns(t *testing.T) {
	eng := rulestest.Engine(t)
	p := hurt(t, eng, "player", 12, 13)
	if got := ticks(eng, p, 19); got != 0 {
		t.Fatalf("healed %d in 19 turns", got)
	}
	if got := ticks(eng, p, 1); got != 1 {
		t.Fatalf("the 20th turn should heal 1, healed %d", got)
	}
	if got := ticks(eng, p, 40); got != 2 {
		t.Errorf("40 more turns should heal 2, healed %d", got)
	}
}

func TestNoHealingBuildsUpAtFullHealth(t *testing.T) {
	eng := rulestest.Engine(t)
	p := hurt(t, eng, "player", 12, 13)
	p.HP = p.MaxHP
	ticks(eng, p, 100)
	if p.Healing != 0 {
		t.Fatalf("banked %d points at full health", p.Healing)
	}
	p.HP = 50
	if got := ticks(eng, p, 19); got != 0 {
		t.Errorf("a fresh wound should take the full 20 turns, healed %d in 19", got)
	}
}

func TestTheDeadDoNotHeal(t *testing.T) {
	eng := rulestest.Engine(t)
	g := hurt(t, eng, "goblin", 30, 30)
	g.HP, g.Dead = 0, true
	if ticks(eng, g, 50) != 0 {
		t.Error("a dead goblin healed")
	}
}

// TestARingOfRegenerationBeatsTheCap: the cap binds attributes, not magic.
func TestARingOfRegenerationBeatsTheCap(t *testing.T) {
	eng := rulestest.Engine(t)
	p := hurt(t, eng, "player", 30, 30) // the cap: every 10 turns
	p.Equipped = map[rules.Slot]*rules.Entity{rules.SlotRightHand: rulestest.New(t, eng, "ring_of_regeneration", 0, 0, 0)}
	if got := eng.Reg.Regeneration(p); got != 120 {
		t.Errorf("with the ring: %d points a turn, want 120", got)
	}
	if got := ticks(eng, p, 10); got != 2 {
		t.Errorf("10 turns should heal 2, healed %d", got)
	}
}

func TestPoisonStopsHealingRingOrNot(t *testing.T) {
	eng := rulestest.Engine(t)
	p := hurt(t, eng, "player", 12, 13)
	p.Equipped = map[rules.Slot]*rules.Entity{rules.SlotRightHand: rulestest.New(t, eng, "ring_of_regeneration", 0, 0, 0)}
	p.AddTrait(&rules.TraitInstance{Trait: "poisoned", Params: rules.Params{"damage": 1}, Turns: 5})
	if got := eng.Reg.Regeneration(p); got != 0 {
		t.Errorf("poisoned: %d points a turn, want 0", got)
	}
}

func TestWhoHealsHowFast(t *testing.T) {
	eng := rulestest.Engine(t)
	for id, times := range map[string]int{"mummy": 0, "phantom": 0, "vampire": 0, "slime": 3, "goblin": 1} {
		e := hurt(t, eng, id, 4, 4)
		if got, want := eng.Reg.Regeneration(e), times*rules.NaturalRegeneration(e); got != want {
			t.Errorf("%s: %d points a turn, want %d", id, got, want)
		}
	}
}

// TestTheAmuletOfWasting: worn, every 25th turn it takes an HP; between,
// nothing; and its curse keeps it on.
func TestTheAmuletOfWasting(t *testing.T) {
	eng := rulestest.Engine(t)
	p := hurt(t, eng, "player", 12, 13)
	amulet := rulestest.New(t, eng, "amulet_of_wasting", 0, 0, 0)
	p.Equipped = map[rules.Slot]*rules.Entity{rules.SlotNeck: amulet}
	w := &rules.World{Map: rulestest.Room(), Player: p, Turn: 25}
	c := eng.NewCtx(w)
	c.Tick(p)
	if p.HP != 49 || strings.Join(c.Messages(), "") != "The amulet saps your life. You lose 1 HP." {
		t.Errorf("turn 25: HP %d, %q", p.HP, c.Messages())
	}
	w.Turn = 26
	c = eng.NewCtx(w)
	c.Tick(p)
	if p.HP != 49 || len(c.Messages()) != 0 {
		t.Errorf("turn 26: HP %d, %q", p.HP, c.Messages())
	}
	if got := eng.Reg.Regeneration(p); got != 0 {
		t.Errorf("wearing it, no healing: %d", got)
	}
	if res, _ := eng.NewCtx(w).Drop(p, amulet); !res.Refused {
		t.Error("the curse should keep it on")
	}

	p.HP, w.Turn = 1, 50
	c = eng.NewCtx(w)
	c.Tick(p)
	if !p.Dead || !strings.HasSuffix(strings.Join(c.Messages(), ""), "You lose 1 HP and die.") {
		t.Errorf("at 1 HP it should kill: dead %v, %q", p.Dead, c.Messages())
	}
}
