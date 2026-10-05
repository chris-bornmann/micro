package rules_test

import (
	"strconv"
	"strings"
	"testing"

	"micro/server/internal/rules"
	"micro/server/internal/rules/rulestest"
)

// duel is the player against a goblin beside them, on an engine whose
// to-hit roll is roll (nil for the real one).
func duel(t *testing.T, roll func() int) (*rules.Ctx, *rules.Entity, *rules.Entity) {
	t.Helper()
	eng := rulestest.Engine(t)
	eng.HitRoll = roll
	p := rulestest.New(t, eng, "player", 6, 6, 1000)
	g := rulestest.New(t, eng, "goblin", 7, 6, 1000)
	g.Equipped = nil
	return eng.NewCtx(&rules.World{Map: rulestest.Room(), Player: p, Monsters: []*rules.Entity{g}}), p, g
}

func TestHitChanceIsFiftyPlusDexterityDifferenceClamped(t *testing.T) {
	c, p, g := duel(t, nil)
	cases := []struct{ attacker, defender, want int }{
		{0, 0, 50}, {13, 4, 59}, {4, 13, 41},
		{100, 0, 90}, // the ceiling: a blow can always miss
		{0, 100, 20}, // the floor: a blow can always land
	}
	for _, tc := range cases {
		p.Attrs.Dexterity, g.Attrs.Dexterity = tc.attacker, tc.defender
		if got := c.HitChance(p, g); got != tc.want {
			t.Errorf("Dexterity %d vs %d: %d%%, want %d%%", tc.attacker, tc.defender, got, tc.want)
		}
	}
}

func TestAccuracyAndEvasionCountFromWhereverTheyAreCarried(t *testing.T) {
	c, p, g := duel(t, nil)
	eng := rulestest.Engine(t)
	sword := rulestest.New(t, eng, "fine_dagger", 0, 0, 0)
	sword.AddTrait(&rules.TraitInstance{Trait: "accuracy", Params: rules.Params{"amount": 10}})
	p.Equipped = map[rules.Slot]*rules.Entity{rules.SlotWeapon: sword}
	if got := c.HitChance(p, g); got != 60 {
		t.Errorf("an accurate weapon: %d%%, want 60%%", got)
	}
	cloak := rulestest.New(t, eng, "shoes_of_sneaking", 0, 0, 0)
	cloak.AddTrait(&rules.TraitInstance{Trait: "evasion", Params: rules.Params{"amount": 15}})
	g.Equipped = map[rules.Slot]*rules.Entity{rules.SlotFeet: cloak}
	if got := c.HitChance(p, g); got != 45 {
		t.Errorf("against evasive gear: %d%%, want 45%%", got)
	}
}

// TestAnUnseenDefenderIsHarderToHit: invisibility's evasion counts only
// while it is actually in effect.
func TestAnUnseenDefenderIsHarderToHit(t *testing.T) {
	c, p, g := duel(t, nil)
	inv := g.AddTrait(&rules.TraitInstance{Trait: "invisible", Params: rules.Params{"visible_below": 4, "evasion": 20}})
	if got := c.HitChance(p, g); got != 30 {
		t.Errorf("unseen: %d%%, want 30%%", got)
	}
	inv.Suspended = true
	if got := c.HitChance(p, g); got != 50 {
		t.Errorf("glimpsed this turn: %d%%, want 50%%", got)
	}
	inv.Suspended = false
	g.HP = 3
	if got := c.HitChance(p, g); got != 50 {
		t.Errorf("wounded below its threshold: %d%%, want 50%%", got)
	}
}

func TestTheRealRollHitsAboutAsOftenAsTheChanceSays(t *testing.T) {
	c, p, g := duel(t, nil)
	p.Attrs.Dexterity, g.Attrs.Dexterity = 0, 100 // 20%
	hits := 0
	for i := 0; i < 2000; i++ {
		g.HP = 1000
		if !c.Melee(p, g).Missed {
			hits++
		}
	}
	if hits < 300 || hits > 500 {
		t.Errorf("hit %d of 2000 at 20%%", hits)
	}
}

// TestAMissDoesNothing: no damage, and nothing a hit would set off — a
// vampire gains no blood from a bite that missed.
func TestAMissDoesNothing(t *testing.T) {
	eng := rulestest.Engine(t)
	eng.HitRoll = rulestest.AlwaysMiss
	v := rulestest.New(t, eng, "vampire", 7, 6, 100)
	v.HP = 10
	p := rulestest.New(t, eng, "player", 6, 6, 50)
	c := eng.NewCtx(&rules.World{Map: rulestest.Room(), Player: p, Monsters: []*rules.Entity{v}})
	out := c.Melee(v, p)
	if !out.Missed || out.Damage != 0 || p.HP != 50 || v.HP != 10 {
		t.Errorf("outcome %+v, player HP %d, vampire HP %d", out, p.HP, v.HP)
	}
}

func TestAThrownItemStillAlwaysHits(t *testing.T) {
	c, p, g := duel(t, rulestest.AlwaysMiss)
	res := c.Throw(p, rulestest.Give(p, rulestest.New(t, rulestest.Engine(t), "fine_dagger", 0, 0, 0)), 1, 0)
	if res.Target != g || res.Outcome.Missed {
		t.Errorf("a throw should hit whatever is in its path: %+v", res.Outcome)
	}
}

// lines collects every distinct message produced by n blows.
func lines(c *rules.Ctx, attacker, defender *rules.Entity, n int) map[string]bool {
	seen := map[string]bool{}
	for i := 0; i < n; i++ {
		defender.HP = 1000
		before := len(c.Events)
		c.Melee(attacker, defender)
		seen[c.Messages()[before]] = true
	}
	return seen
}

func TestAWieldedWeaponDescribesBlowsFromEitherSide(t *testing.T) {
	c, p, g := duel(t, rulestest.AlwaysHit)
	eng := rulestest.Engine(t)
	g.Equipped = map[rules.Slot]*rules.Entity{rules.SlotWeapon: rulestest.New(t, eng, "rusty_sword", 0, 0, 0)}

	// The goblin swings the sword at you: the sword's hit lines, in its
	// words, with the goblin's pronoun — both lines, sooner or later.
	struck, sliced := false, false
	for l := range lines(c, g, p, 200) {
		switch {
		case strings.HasPrefix(l, "The goblin strikes you with his rusty sword for "):
			struck = true
		case strings.HasPrefix(l, "You are sliced by the goblin's rusty sword for "):
			sliced = true
		case l == "The goblin hits you, but the blow glances off.":
			// The curse takes a point off: a 1 does nothing.
		default:
			t.Errorf("unexpected line %q", l)
		}
	}
	if !struck || !sliced {
		t.Errorf("both lines should come up in 200 blows: struck %v, sliced %v", struck, sliced)
	}

	// You swing it: the sword's you_hit lines, not the player's fists.
	p.Equipped = map[rules.Slot]*rules.Entity{rules.SlotWeapon: rulestest.New(t, eng, "rusty_sword", 0, 0, 0)}
	for l := range lines(c, p, g, 50) {
		if (!strings.Contains(l, "rusty sword") || !strings.Contains(l, "the goblin")) && l != "You hit the goblin, but the blow glances off." {
			t.Errorf("unexpected line %q", l)
		}
	}

	// Missing it: the sword's miss lines.
	c.HitRoll = rulestest.AlwaysMiss
	for l := range lines(c, g, p, 50) {
		if l != "The goblin swings his rusty sword at you, but misses!" && l != "The goblin's rusty sword whistles past your ear." {
			t.Errorf("unexpected miss %q", l)
		}
	}
}

// TestWithoutMessagesTheOldLinesRemain: a creature and weapon with no
// messages of their own fall back to the game's plain lines.
func TestWithoutMessagesTheOldLinesRemain(t *testing.T) {
	c, p, g := duel(t, rulestest.AlwaysHit)
	plain := *g.T
	plain.Messages = rules.AttackMessages{}
	g.T = &plain
	playerPlain := *p.T
	playerPlain.Messages = rules.AttackMessages{}
	p.T = &playerPlain

	out := c.Melee(g, p)
	if got := c.Messages()[0]; got != "You take "+strconv.Itoa(out.Damage)+" damage ("+strconv.Itoa(p.HP)+" HP left)" {
		t.Errorf("hit: %q", got)
	}
	c.Events = nil
	c.HitRoll = rulestest.AlwaysMiss
	c.Melee(g, p)
	c.Melee(p, g)
	if got := c.Messages(); got[0] != "The goblin misses you." || got[1] != "You miss the goblin." {
		t.Errorf("misses: %q", got)
	}
}

func TestAKillingBlowSaysSo(t *testing.T) {
	c, p, g := duel(t, rulestest.AlwaysHit)
	p.HP = 1
	g.Attrs.Strength = 100
	c.Melee(g, p)
	if got := c.Messages()[0]; !strings.HasSuffix(got, " You die!") {
		t.Errorf("got %q", got)
	}
}
