package game

import (
	"regexp"
	"strconv"
	"strings"
	"testing"

	"micro/server/internal/rules"
	"micro/server/internal/rules/rulestest"
)

// dyingGoblin is a goblin on 1 HP next to the Player at (7,7): any hit
// kills it.
func dyingGoblin(t *testing.T) *rules.Entity {
	g := testMonster(t, "goblin", 8, 7, 1)
	g.MaxHP = 9
	g.Mind.Aware = true
	return g
}

func TestHandleKeyAttackKillingBareHandedGoblinAddsCorpseAtDeathTile(t *testing.T) {
	g := dyingGoblin(t)
	state := newTestGameState(t, itemsTestMap(), testPlayer(t, 7, 7, 100), []*rules.Entity{g}, nil)

	// Bare hands, described by the player's own lines, and the kill noted.
	if msg := mustKey(t, state, "Right"); !strings.HasSuffix(msg, " The goblin dies!") {
		t.Errorf("got %q", msg)
	}
	if !g.Dead || len(state.FloorItems) != 1 {
		t.Fatalf("expected a dead goblin and exactly its corpse on the floor, got %v", names(state.FloorItems))
	}
	corpse := state.FloorItems[0]
	if corpse.T.Category != rules.CategoryCorpse || corpse.Name != "goblin corpse" || corpse.X != 8 || corpse.Y != 7 {
		t.Errorf("corpse %s at (%d,%d)", corpse.Name, corpse.X, corpse.Y)
	}
}

func TestHandleKeyAttackKillingArmedGoblinDropsCorpseAndWeapon(t *testing.T) {
	g := dyingGoblin(t)
	g.Equipped = map[rules.Slot]*rules.Entity{rules.SlotWeapon: testItem(t, "rusty_sword", 0, 0)}
	state := newTestGameState(t, itemsTestMap(), testPlayer(t, 7, 7, 100), []*rules.Entity{g}, nil)

	mustKey(t, state, "Right")
	if got := names(state.FloorItems); len(got) != 2 || got[0] != "goblin corpse" || got[1] != "rusty sword" {
		t.Fatalf("floor %v, want the corpse then the sword", got)
	}
	for _, it := range state.FloorItems {
		if it.X != 8 || it.Y != 7 {
			t.Errorf("%s at (%d,%d), want the death tile", it.Name, it.X, it.Y)
		}
	}
	if g.Weapon() != nil {
		t.Error("the dead goblin should no longer hold the sword")
	}
}

func TestHandleKeyAttackKillingGoblinPreservesItsOwnState(t *testing.T) {
	g := dyingGoblin(t)
	id := g.ID
	state := newTestGameState(t, itemsTestMap(), testPlayer(t, 7, 7, 100), []*rules.Entity{g}, nil)
	mustKey(t, state, "Right")
	if g.ID != id || g.MaxHP != 9 || !g.Mind.Aware || g.Kind() != "goblin" || !g.Dead || g.HP > 0 {
		t.Errorf("expected the dead goblin's own state kept for a future resurrection, got %+v", g)
	}
	if len(state.Monsters) != 1 {
		t.Error("a dead monster stays in the monster list")
	}
}

func TestPlayerCanMoveOntoDeadGoblinsTileAndSeeTheCorpse(t *testing.T) {
	state := newTestGameState(t, itemsTestMap(), testPlayer(t, 7, 7, 100), []*rules.Entity{dyingGoblin(t)}, nil)
	mustKey(t, state, "Right")
	mustKey(t, state, "Right")
	if state.Player.X != 8 || state.Player.Y != 7 {
		t.Fatalf("expected to step onto the dead goblin's tile, at (%d,%d)", state.Player.X, state.Player.Y)
	}
	if lines := lookHereLines(state); len(lines) != 1 || lines[0] != "A) goblin corpse" {
		t.Errorf("look: %v", lines)
	}
	if msg := mustKey(t, state, ","); msg != "You pick up the goblin corpse." || len(state.FloorItems) != 0 {
		t.Errorf("pickup: %q", msg)
	}
}

func TestHandleKeyAttackHitsOnlyTheAdjacentMonster(t *testing.T) {
	g := testMonster(t, "goblin", 8, 7, 5)
	mummy := testMonster(t, "mummy", 13, 13, 25)
	state := newTestGameState(t, itemsTestMap(), testPlayer(t, 7, 7, 100), []*rules.Entity{g, mummy}, nil)
	mustKey(t, state, "Right")
	if g.HP >= 5 {
		t.Error("expected the adjacent goblin to take damage")
	}
	if mummy.HP != 25 || mummy.Dead {
		t.Error("expected the distant mummy untouched")
	}
}

func TestKillingOneMonsterDoesNotDisturbTheOther(t *testing.T) {
	g := dyingGoblin(t)
	mummy := testMonster(t, "mummy", 13, 13, 25)
	state := newTestGameState(t, itemsTestMap(), testPlayer(t, 7, 7, 100), []*rules.Entity{g, mummy}, nil)
	mustKey(t, state, "Right")
	if !g.Dead || len(state.FloorItems) != 1 || mummy.HP != 25 || mummy.Dead {
		t.Errorf("goblin dead %v, floor %v, mummy HP %d", g.Dead, names(state.FloorItems), mummy.HP)
	}
}

// TestBurningMonstersTickAtTheEndOfTheTurn: a monster's burn damage lands
// in finishTurn and is reported, and the mummy's comes out doubled.
func TestBurningMonstersTickAtTheEndOfTheTurn(t *testing.T) {
	burn := regexp.MustCompile(`^(Goblin|Mummy) takes (\d+) fire damage from burning\.$`)
	for i := 0; i < 30; i++ {
		// Far corners of a big room, out of each other's way; the Player
		// is invisible so neither monster moves.
		g := testMonster(t, "goblin", 5, 5, 100)
		mummy := testMonster(t, "mummy", 14, 14, 100)
		for _, m := range []*rules.Entity{g, mummy} {
			m.AddTrait(&rules.TraitInstance{Trait: "on_fire", Turns: 3})
		}
		p := testPlayer(t, 9, 9, 100)
		p.AddTrait(&rules.TraitInstance{Trait: "invisible", Turns: 100})
		state := newTestGameState(t, itemsTestMap(), p, []*rules.Entity{g, mummy}, nil)

		msgs := handleKeyMessagesOK(t, state, ".")
		if len(msgs) != 2 {
			t.Fatalf("expected one burn message per monster, got %q", msgs)
		}
		for _, msg := range msgs {
			m := burn.FindStringSubmatch(msg)
			if m == nil {
				t.Fatalf("unexpected message %q", msg)
			}
			d, _ := strconv.Atoi(m[2])
			lo, hi := 2, 4
			if m[1] == "Mummy" {
				lo, hi = 4, 8
			}
			if d < lo || d > hi {
				t.Errorf("%s burned for %d, want %d-%d", m[1], d, lo, hi)
			}
		}
	}
}

func handleKeyMessagesOK(t *testing.T, state *GameState, key string) []string {
	t.Helper()
	msgs, err := handleKeyMessages(state, key)
	if err != nil {
		t.Fatalf("handleKeyMessages(%q): %v", key, err)
	}
	return msgs
}

// TestAVampiresBiteSpeaksForItsDrain: the vampire's own bite lines say it
// feeds, so its drain adds no line of its own — but still heals it.
func TestAVampiresBiteSpeaksForItsDrain(t *testing.T) {
	re := regexp.MustCompile(`^The vampire (sucks your blood|sinks his fangs into your neck) for (\d+) hp of damage`)
	for i := 0; i < 30; i++ {
		v := testMonster(t, "vampire", 8, 7, 100)
		v.HP = 10
		state := newTestGameState(t, itemsTestMap(), testPlayer(t, 7, 7, 1000), []*rules.Entity{v}, nil)
		msgs := handleKeyMessagesOK(t, state, ".")
		if len(msgs) != 1 {
			t.Fatalf("expected the bite alone, got %q", msgs)
		}
		m := re.FindStringSubmatch(msgs[0])
		if m == nil {
			t.Fatalf("got %q", msgs[0])
		}
		d, _ := strconv.Atoi(m[2])
		if v.HP != 10+(d*20+50)/100 {
			t.Errorf("%d damage, vampire HP %d", d, v.HP)
		}
	}
}

// TestAMissStillSpendsTheTurn: swinging and missing is an action like any
// other, told in the player's own miss lines.
func TestAMissStillSpendsTheTurn(t *testing.T) {
	g := testMonster(t, "goblin", 8, 7, 5)
	state := newTestGameState(t, itemsTestMap(), testPlayer(t, 7, 7, 100), []*rules.Entity{g}, nil)
	state.eng.HitRoll = rulestest.AlwaysMiss

	msgs := handleKeyMessagesOK(t, state, "Right")
	if msgs[0] != "You swing at the goblin, but miss!" && msgs[0] != "The goblin dodges your fist." {
		t.Errorf("got %q", msgs[0])
	}
	if g.HP != 5 || state.Turn != 1 {
		t.Errorf("goblin HP %d, turn %d", g.HP, state.Turn)
	}
}

// TestAGoblinCanDieOfPoison: poison is worked out at the end of the turn
// like burning, and a goblin it kills leaves its corpse and belongings.
func TestAGoblinCanDieOfPoison(t *testing.T) {
	g := testMonster(t, "goblin", 12, 12, 2)
	g.Equipped = map[rules.Slot]*rules.Entity{rules.SlotWeapon: testItem(t, "poisoned_dagger", 0, 0)}
	g.AddTrait(&rules.TraitInstance{Trait: "poisoned", Params: rules.Params{"damage": 3}, Turns: 4})
	p := testPlayer(t, 5, 5, 100)
	p.AddTrait(&rules.TraitInstance{Trait: "invisible", Turns: 100}) // keep it out of the fight
	state := newTestGameState(t, itemsTestMap(), p, []*rules.Entity{g}, nil)

	if msg := mustKey(t, state, "."); msg != "Goblin takes 3 poison damage and dies." {
		t.Errorf("got %q", msg)
	}
	if !g.Dead || len(state.FloorItems) != 2 || state.FloorItems[1].Name != "poisoned dagger" {
		t.Errorf("dead %v, floor %v", g.Dead, names(state.FloorItems))
	}
}

func TestStabbedByAPoisonedDaggerThenCured(t *testing.T) {
	g := testMonster(t, "goblin", 8, 7, 100)
	g.Equipped = map[rules.Slot]*rules.Entity{rules.SlotWeapon: testItem(t, "poisoned_dagger", 0, 0)}
	p := carrying(testPlayer(t, 7, 7, 100), testItem(t, "healing_potion", 0, 0))
	state := newTestGameState(t, itemsTestMap(), p, []*rules.Entity{g}, nil)

	msgs := handleKeyMessagesOK(t, state, ".") // the goblin stabs: Stamina 0, so 4 a turn
	if p.FindTrait("poisoned") == nil || msgs[len(msgs)-1] != "You feel poison burning in your veins!" {
		t.Fatalf("expected to be poisoned, got %q", msgs)
	}
	if lines := hudLines(state); lines[len(lines)-1] != "Poisoned: yes" {
		t.Errorf("HUD %v", lines)
	}
	g.Dead = true // no more stabbing
	if msg := mustKey(t, state, "A"); !strings.Contains(msg, "The poison leaves your body.") {
		t.Errorf("got %q", msg)
	}
	if p.FindTrait("poisoned") != nil {
		t.Error("still poisoned")
	}
}

// TestPoisonStartsTheTurnAfterTheStab: the turn the Player stabs a goblin
// with a poisoned dagger, the stab is all it takes; the poison starts
// biting the next turn.
func TestPoisonStartsTheTurnAfterTheStab(t *testing.T) {
	p := testPlayer(t, 7, 7, 1000)
	p.Equipped = map[rules.Slot]*rules.Entity{rules.SlotWeapon: testItem(t, "poisoned_dagger", 0, 0)}
	g := testMonster(t, "goblin", 8, 7, 1000)
	state := newTestGameState(t, itemsTestMap(), p, []*rules.Entity{g}, nil)

	msgs := strings.Join(handleKeyMessagesOK(t, state, "Right"), " | ")
	inst := g.FindTrait("poisoned")
	if inst == nil || !strings.Contains(msgs, "looks sick") || strings.Contains(msgs, "poison damage") {
		t.Fatalf("the stab's turn should poison but not yet hurt: %q", msgs)
	}
	turns, hp := inst.Turns, g.HP

	msgs = strings.Join(handleKeyMessagesOK(t, state, "."), " | ")
	if want := inst.Params.Int("damage"); hp-g.HP != want || inst.Turns != turns-1 || !strings.Contains(msgs, "poison damage") {
		t.Errorf("next turn the poison should bite for %d: HP %d -> %d, turns %d -> %d, %q", want, hp, g.HP, turns, inst.Turns, msgs)
	}
}

// TestPoisonFromAMonsterStartsTheNextTurnToo: a monster strikes after the
// turn's ticks, so its poison must bite at the very next tick — not wait a
// turn longer than the Player's does.
func TestPoisonFromAMonsterStartsTheNextTurnToo(t *testing.T) {
	p := testPlayer(t, 7, 7, 1000)
	g := testMonster(t, "goblin", 8, 7, 1000)
	g.Equipped = map[rules.Slot]*rules.Entity{rules.SlotWeapon: testItem(t, "poisoned_dagger", 0, 0)}
	state := newTestGameState(t, itemsTestMap(), p, []*rules.Entity{g}, nil)

	msgs := strings.Join(handleKeyMessagesOK(t, state, "."), " | ")
	if p.FindTrait("poisoned") == nil || strings.Contains(msgs, "poison damage") {
		t.Fatalf("the goblin's stab should poison but not yet hurt: %q", msgs)
	}
	msgs = strings.Join(handleKeyMessagesOK(t, state, "."), " | ")
	if !strings.Contains(msgs, "You take ") || !strings.Contains(msgs, "poison damage") {
		t.Errorf("the next turn the poison should bite: %q", msgs)
	}
}
