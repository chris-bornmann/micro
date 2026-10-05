package rules_test

import (
	"strings"
	"testing"

	"micro/server/internal/rules"
	"micro/server/internal/rules/rulestest"
	"micro/server/internal/rules/traits"
)

// trapScene is the player at (6, 6) with a trap of template id at (7, 6),
// on a level of the given depth.
func trapScene(t *testing.T, id string, depth int) (*rules.Engine, *rules.World, *rules.Entity, *rules.Entity) {
	t.Helper()
	eng := rulestest.Engine(t)
	p := rulestest.New(t, eng, "player", 6, 6, 1000)
	trap := rulestest.New(t, eng, id, 7, 6, 0)
	return eng, &rules.World{Map: rulestest.Room(), Player: p, Floor: []*rules.Entity{trap}, Depth: depth}, p, trap
}

// stepOn moves the player right, onto the trap, and returns the messages.
func stepOn(eng *rules.Engine, w *rules.World, p *rules.Entity) (*rules.Ctx, string) {
	c := eng.NewCtx(w)
	p.X, p.Y = 6, 6
	c.Step(p, 7, 6)
	return c, strings.Join(c.Messages(), " | ")
}

// setAvoid sets how often a known trap is stepped around.
func setAvoid(trap *rules.Entity, percent int) {
	for _, inst := range trap.Traits {
		if inst.Params.Has("avoid_known") {
			inst.Params["avoid_known"] = percent
		}
	}
}

func TestTrapsLeaveMonstersAlone(t *testing.T) {
	for _, id := range []string{"pit", "dart_trap", "trap_door", "teleport_trap"} {
		eng, w, _, trap := trapScene(t, id, 5)
		g := rulestest.New(t, eng, "goblin", 6, 6, 10)
		w.Monsters = []*rules.Entity{g}
		w.Player.X, w.Player.Y = 12, 12
		c := eng.NewCtx(w)
		c.Step(g, 7, 6)
		if g.X != 7 || g.HP != 10 || len(g.Traits) != 0 || c.Descend || len(c.Messages()) != 0 {
			t.Errorf("%s touched a goblin: at (%d,%d), HP %d, %v", id, g.X, g.Y, g.HP, c.Messages())
		}
		if !eng.Reg.Has(trap, "hidden") {
			t.Errorf("%s was found by a goblin", id)
		}
	}
}

func TestAHiddenTrapAlwaysSpringsAndIsFound(t *testing.T) {
	eng, w, p, trap := trapScene(t, "teleport_trap", 2)
	setAvoid(trap, 100) // even a player who always avoids known traps
	if _, msg := stepOn(eng, w, p); !strings.Contains(msg, "flash of light") {
		t.Errorf("a hidden trap should spring: %q", msg)
	}
	if eng.Reg.Has(trap, "hidden") {
		t.Error("springing it should reveal it")
	}
}

func TestAKnownTrapIsUsuallyAvoided(t *testing.T) {
	eng, w, p, trap := trapScene(t, "teleport_trap", 2)
	stepOn(eng, w, p) // found
	setAvoid(trap, 100)
	if _, msg := stepOn(eng, w, p); msg != "You carefully avoid the teleport trap." || p.X != 7 {
		t.Errorf("always avoided: %q at x=%d", msg, p.X)
	}
	setAvoid(trap, 0)
	if _, msg := stepOn(eng, w, p); !strings.Contains(msg, "flash of light") {
		t.Errorf("never avoided: %q", msg)
	}
	setAvoid(trap, 75)
	avoided := 0
	for i := 0; i < 1000; i++ {
		if _, msg := stepOn(eng, w, p); strings.Contains(msg, "avoid") {
			avoided++
		}
	}
	if avoided < 680 || avoided > 820 {
		t.Errorf("avoided %d of 1000 at 75%%", avoided)
	}
}

func TestAPitHurtsMoreTheDeeperItIs(t *testing.T) {
	for _, depth := range []int{1, 7} {
		lo, hi := spread(func() int {
			eng, w, p, _ := trapScene(t, "pit", depth)
			stepOn(eng, w, p)
			return 1000 - p.HP
		})
		if lo != 1 || hi != depth {
			t.Errorf("depth %d: pit damage [%d, %d], want [1, %d]", depth, lo, hi, depth)
		}
	}
}

func TestAPitCanKill(t *testing.T) {
	eng, w, p, _ := trapScene(t, "pit", 25)
	p.HP = 1
	_, msg := stepOn(eng, w, p)
	if !p.Dead || !strings.HasSuffix(msg, "and die.") || p.FindTrait("in_pit") != nil {
		t.Errorf("dead %v, %q", p.Dead, msg)
	}
}

func TestClimbingOutOfAPit(t *testing.T) {
	eng, w, p, _ := trapScene(t, "pit", 3)
	if _, msg := stepOn(eng, w, p); !strings.HasPrefix(msg, "You fall into a pit and take ") || p.FindTrait("in_pit") == nil {
		t.Fatalf("expected to be in the pit: %q", msg)
	}
	if got := statusOf(eng, p); !strings.Contains(got, "In a pit: yes") {
		t.Errorf("status %q", got)
	}

	// Each try is a coin flip; the player stays put until one succeeds.
	tries := 0
	for p.FindTrait("in_pit") != nil {
		tries++
		c := eng.NewCtx(w)
		res := c.Step(p, 8, 6)
		msg := strings.Join(c.Messages(), "")
		if p.FindTrait("in_pit") != nil {
			if res.Moved || p.X != 7 || msg != "You try to climb out of the pit, but slip back." {
				t.Fatalf("try %d: moved %v, %q", tries, res.Moved, msg)
			}
			continue
		}
		if !res.Moved || p.X != 8 || msg != "You climb out of the pit." {
			t.Fatalf("try %d: moved %v, %q", tries, res.Moved, msg)
		}
	}
}

func TestPitEscapesSucceedAboutHalfTheTime(t *testing.T) {
	eng, w, p, _ := trapScene(t, "pit", 3)
	out := 0
	for i := 0; i < 1000; i++ {
		p.X, p.Y = 7, 6
		p.Traits = []*rules.TraitInstance{{Trait: "in_pit", Params: rules.Params{"x": 7, "y": 6}}}
		if eng.NewCtx(w).Step(p, 8, 6).Moved {
			out++
		}
	}
	if out < 420 || out > 580 {
		t.Errorf("climbed out %d of 1000 tries, want about half", out)
	}
}

// TestStuckInAPitYouStillFight: a monster next to you is fought, whatever
// the pit — only a real move needs climbing out.
func TestStuckInAPitYouStillFight(t *testing.T) {
	eng, w, p, _ := trapScene(t, "pit", 3)
	stepOn(eng, w, p)
	g := rulestest.New(t, eng, "goblin", 8, 6, 1000)
	w.Monsters = []*rules.Entity{g}
	c := eng.NewCtx(w)
	if res := c.Step(p, 8, 6); res.Target != g || p.FindTrait("in_pit") == nil {
		t.Errorf("should attack the goblin and stay in the pit: %+v", res)
	}
	// And the goblin attacks you as usual.
	if res := c.Step(g, 7, 6); res.Target != p {
		t.Errorf("the goblin should attack: %+v", res)
	}
}

func TestLeavingAPitAnyOtherWayEndsIt(t *testing.T) {
	eng, w, p, _ := trapScene(t, "pit", 3)
	stepOn(eng, w, p)
	p.X, p.Y = 12, 12 // teleported, say
	eng.NewCtx(w).Tick(p)
	if p.FindTrait("in_pit") != nil {
		t.Error("in_pit should go with the pit")
	}
}

func TestDartHitChanceFallsWithDexterity(t *testing.T) {
	cases := []struct{ chance, dex, want int }{{70, 0, 70}, {70, 20, 50}, {70, 100, 5}, {100, 0, 95}}
	for _, tc := range cases {
		if got := traits.DartHitChance(tc.chance, tc.dex); got != tc.want {
			t.Errorf("DartHitChance(%d, %d) = %d, want %d", tc.chance, tc.dex, got, tc.want)
		}
	}
}

// TestADartHurtsAndPoisons checks a dart against its own numbers in
// traps.yaml, so retuning the trap there doesn't break the test.
func TestADartHurtsAndPoisons(t *testing.T) {
	hits := 0
	var chance int
	for i := 0; i < 200; i++ {
		eng, w, p, trap := trapScene(t, "dart_trap", 2)
		dart := trap.FindTrait("dart").Params
		dice := dart["damage"].(rules.Dice)
		chance = dart.Int("hit_chance")
		_, msg := stepOn(eng, w, p)
		if strings.Contains(msg, "misses you") {
			if p.HP != 1000 || p.FindTrait("poisoned") != nil {
				t.Fatalf("a miss did something: %q", msg)
			}
			continue
		}
		hits++
		if d := 1000 - p.HP; d < dice.Min || d > dice.Max {
			t.Fatalf("dart damage %d, want %d-%d", d, dice.Min, dice.Max)
		}
		// Stamina 0: the full poison_damage a turn.
		dmg, turns := dart.Int("poison_damage"), dart.Int("poison_turns")
		if inst := p.FindTrait("poisoned"); inst == nil || inst.Params.Int("damage") != dmg || inst.Turns != turns {
			t.Fatalf("expected poison %d for %d turns, got %+v (%q)", dmg, turns, inst, msg)
		}
	}
	if want := 2 * chance; hits < want-30 || hits > want+30 {
		t.Errorf("hit %d of 200 at %d%%", hits, chance)
	}
}

func TestATrapDoorAsksToDescend(t *testing.T) {
	eng, w, p, _ := trapScene(t, "trap_door", 3)
	if c, msg := stepOn(eng, w, p); !c.Descend || msg != "A trap door opens beneath you!" {
		t.Errorf("descend %v, %q", c.Descend, msg)
	}
	eng, w, p, _ = trapScene(t, "trap_door", rules.DeepestLevel)
	if c, _ := stepOn(eng, w, p); c.Descend {
		t.Error("there is nowhere to fall from the deepest level")
	}
}

func TestATeleportTrapMovesYouToAFreeTile(t *testing.T) {
	eng, w, p, _ := trapScene(t, "teleport_trap", 2)
	stepOn(eng, w, p)
	if _, ok := w.Map.RoomAt(p.X, p.Y); !ok || (p.X == 7 && p.Y == 6) {
		t.Errorf("landed at (%d,%d)", p.X, p.Y)
	}
}

func statusOf(eng *rules.Engine, e *rules.Entity) string {
	var s []string
	for _, f := range eng.Reg.Status(e) {
		s = append(s, f.Label+": "+f.Value)
	}
	return strings.Join(s, " | ")
}
