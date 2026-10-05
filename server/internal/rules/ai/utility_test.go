package ai_test

import (
	"io/fs"
	"os"
	"strings"
	"testing"
	"testing/fstest"

	"micro/server/internal/content"
	"micro/server/internal/rules"
	"micro/server/internal/rules/builtin"
	"micro/server/internal/rules/rulestest"
)

// withBrain is the game's content plus a monster "brawler" whose brain is
// the YAML list given.
func withBrain(t *testing.T, brain string) *rules.Engine {
	t.Helper()
	fsys := fstest.MapFS{"brawler.yaml": {Data: []byte(`
brawler:
  category: monster
  name: Brawler
  glyph: b
  color: [1, 1, 1]
  weight: 50
  level: 1
  hp: 20
  attributes: 0
  attack: {name: fists, damage: {bludgeoning: 1}}
  ai: {behavior: utility}
  spawn: {model: common, density: 1, sigma_up: 1, sigma_down: 1, max_above: 1}
  brain:
` + brain)}}
	real := os.DirFS("../../content/data")
	names, _ := fs.Glob(real, "*.yaml")
	for _, n := range names {
		b, _ := fs.ReadFile(real, n)
		fsys[n] = &fstest.MapFile{Data: b}
	}
	reg := builtin.Registry()
	cat, err := content.LoadFS(fsys, reg)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	return &rules.Engine{Reg: reg, Cat: cat, HitRoll: rulestest.AlwaysHit}
}

const fighter = `
    - {option: drink_to_heal, below_hp_percent: 40}
    - {option: read_to_escape, below_hp_percent: 25}
    - {option: attack}
    - {option: approach}
    - {option: flee, below_hp_percent: 15}
    - {option: wander}
`

// brawlScene puts the player at (6,6) and a brawler at (bx, 6) in the
// one-room test level.
func brawlScene(t *testing.T, eng *rules.Engine, bx int) (*rules.Ctx, *rules.Entity, *rules.Entity) {
	t.Helper()
	p := rulestest.New(t, eng, "player", 6, 6, 1000)
	b := rulestest.New(t, eng, "brawler", bx, 6, 20)
	return eng.NewCtx(&rules.World{Map: rulestest.Room(), Player: p, Monsters: []*rules.Entity{b}}), p, b
}

func TestAHostileUtilityMonsterClosesInAndAttacks(t *testing.T) {
	c, p, b := brawlScene(t, withBrain(t, fighter), 9)
	c.MonsterTurn(b)
	if b.X != 8 {
		t.Fatalf("should approach, at x=%d", b.X)
	}
	c.MonsterTurn(b)
	c.MonsterTurn(b)
	if p.HP == 1000 || b.X != 7 {
		t.Errorf("should be next to the player and hitting: x=%d, player HP %d", b.X, p.HP)
	}
}

func TestHurtItDrinksAHealingPotion(t *testing.T) {
	eng := withBrain(t, fighter)
	c, _, b := brawlScene(t, eng, 7)
	b.HP = 5
	rulestest.Give(b, rulestest.New(t, eng, "healing_potion", 0, 0, 0))
	rulestest.Give(b, rulestest.New(t, eng, "teleport_scroll", 0, 0, 0))
	c.MonsterTurn(b)
	if b.HP <= 5 || len(b.Inventory) != 1 || b.Inventory[0].T.ID != "teleport_scroll" {
		t.Errorf("should drink the potion first: HP %d, carrying %d", b.HP, len(b.Inventory))
	}
	if msg := strings.Join(c.Messages(), ""); msg != "The brawler drinks a healing potion and looks healthier." {
		t.Errorf("%q", msg)
	}
}

func TestBadlyHurtWithoutAPotionItReadsItselfAway(t *testing.T) {
	eng := withBrain(t, fighter)
	c, _, b := brawlScene(t, eng, 7)
	b.HP = 4 // 20%: below the scroll's 25%
	rulestest.Give(b, rulestest.New(t, eng, "teleport_scroll", 0, 0, 0))
	c.MonsterTurn(b)
	if len(b.Inventory) != 0 || (b.X == 7 && b.Y == 6) {
		t.Errorf("should have read the scroll and gone: at (%d,%d)", b.X, b.Y)
	}
}

func TestBadlyHurtWithNothingItFlees(t *testing.T) {
	c, _, b := brawlScene(t, withBrain(t, fighter), 7)
	b.HP = 2
	c.MonsterTurn(b)
	if b.X <= 7 && b.Y == 6 {
		t.Errorf("should step away from the player, at (%d,%d)", b.X, b.Y)
	}
	if !b.Mind.Fleeing {
		t.Error("should be marked fleeing")
	}
}

func TestWeightsDecide(t *testing.T) {
	// wait scores 0.01; weighted by 200 it beats attack's 1.
	c, p, b := brawlScene(t, withBrain(t, "    - {option: wait, weight: 200}\n    - {option: attack}\n"), 7)
	c.MonsterTurn(b)
	if p.HP != 1000 || b.X != 7 {
		t.Error("a heavily weighted wait should win over attacking")
	}
}

func TestTiesGoToTheFirstListed(t *testing.T) {
	// wander weighted 10 scores 1.0, the same as attack: wander is first.
	c, p, b := brawlScene(t, withBrain(t, "    - {option: wander, weight: 10}\n    - {option: attack}\n"), 7)
	c.MonsterTurn(b)
	if p.HP != 1000 {
		t.Error("on a tie the first listed option should win")
	}
}

func TestBrainsAreChecked(t *testing.T) {
	reg := builtin.Registry()
	load := func(extra string) error {
		_, err := content.LoadFS(fstest.MapFS{"a.yaml": {Data: []byte(`
player: {category: player, name: P, glyph: "@", color: [1, 1, 1], weight: 1, hp: 1, attributes: 0, attack: {name: h, damage: {bludgeoning: 1}}}
corpse: {category: corpse, name: c, glyph: "%", color: [1, 1, 1], weight: 1}
m:
  category: monster
  name: M
  glyph: m
  color: [1, 1, 1]
  weight: 1
  level: 1
  hp: 1
  attributes: 0
  attack: {name: h, damage: {bludgeoning: 1}}
  spawn: {model: common, density: 1, sigma_up: 1, sigma_down: 1, max_above: 1}
` + extra)}}, reg)
		return err
	}
	for extra, want := range map[string]string{
		"  ai: {behavior: utility}\n  brain: [{option: dance}]\n":                       `unknown option "dance"`,
		"  ai: {behavior: utility}\n  brain: [{option: flee, below_hp_percent: 150}]\n": "150 is above the maximum 100",
		"  ai: {behavior: utility}\n":                                                   "needs a brain",
		"  ai: {behavior: chase}\n  brain: [{option: wait}]\n":                          "brain is only for ai behavior utility",
	} {
		if err := load(extra); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: want an error mentioning %q, got %v", extra, want, err)
		}
	}
}
