package content_test

import (
	"io/fs"
	"math/rand"
	"os"
	"testing"
	"testing/fstest"

	"micro/server/internal/content"
	"micro/server/internal/rules"
	"micro/server/internal/rules/builtin"
	"micro/server/internal/rules/rulestest"
)

// newContent is what someone adding to the game would write: a new
// monster and a new weapon, using only traits, actions and AI that already
// exist. No Go.
const newContent = `
giant_rat:
  category: monster
  name: Giant rat
  glyph: r
  color: [150, 120, 90]
  weight: 10
  level: 2
  hp: [3, 6]
  attributes: [1, 4]
  attack: {name: bite, damage: {piercing: [1, 3]}}
  ai: {behavior: chase_and_flee, flee_at_percent: 30}
  spawn: {model: common, density: 4, sigma_up: 3, sigma_down: 1, max_above: 1}
  traits:
    - {trait: resist, type: fire, multiplier: 0.5}

flaming_sword:
  category: weapon
  name: flaming sword
  glyph: ")"
  color: [255, 120, 0]
  weight: 5
  slot: weapon
  # Two parts: its slashing damage is what enchantments shift; the fire
  # is its own type, which vulnerabilities and resistances act on.
  damage: {slashing: 1, fire: 2}
`

// withNewContent is the game's own content plus newContent.
func withNewContent(t *testing.T) *rules.Engine {
	t.Helper()
	fsys := fstest.MapFS{"new.yaml": {Data: []byte(newContent)}}
	real := os.DirFS("data")
	names, _ := fs.Glob(real, "*.yaml")
	for _, n := range names {
		b, err := fs.ReadFile(real, n)
		if err != nil {
			t.Fatal(err)
		}
		fsys[n] = &fstest.MapFile{Data: b}
	}
	reg := builtin.Registry()
	cat, err := content.LoadFS(fsys, reg)
	if err != nil {
		t.Fatalf("new content should load with no code changes:\n%v", err)
	}
	return &rules.Engine{Reg: reg, Cat: cat, HitRoll: rulestest.AlwaysHit}
}

// TestNewMonsterAndWeaponNeedNoCode is the point of the whole design: a
// monster and a weapon that exist only as data spawn, fight and are
// fought, and the typed fire damage meets the mummy's vulnerability and
// the rat's resistance with no code anywhere knowing about either.
func TestNewMonsterAndWeaponNeedNoCode(t *testing.T) {
	eng := withNewContent(t)
	rat := eng.Cat.Get("giant_rat")

	// It spawns by its own curve: some rats on its native level.
	spawned := 0
	for seed := int64(0); seed < 20; seed++ {
		spawned += rules.SpawnCount(rat, 2, false, rand.New(rand.NewSource(seed)))
	}
	if spawned == 0 {
		t.Error("giant rats never spawn on their native level")
	}
	r := eng.Spawn(rat, rulestest.Room(), 0, nil)
	if r.HP < 3 || r.HP > 6 || r.Name != "Giant rat" {
		t.Errorf("spawned rat %+v", r)
	}

	// It hunts and bites the player like any other monster.
	p := rulestest.New(t, eng, "player", 6, 6, 50)
	r.X, r.Y = 7, 6
	w := &rules.World{Map: rulestest.Room(), Player: p, Monsters: []*rules.Entity{r}}
	out, attacked := eng.NewCtx(w).MonsterTurn(r)
	if !attacked || out.Damage < 1 || out.Damage > 3 {
		t.Errorf("the rat should bite for 1-3, attacked %v, %+v", attacked, out)
	}

	// The flaming sword's 1 slashing + 2 fire lands as 1 + 4 on a mummy
	// (double fire), 1 + 2 on a goblin, and 1 + 1 on a rat (half fire).
	p.Equipped = map[rules.Slot]*rules.Entity{rules.SlotWeapon: rulestest.New(t, eng, "flaming_sword", 0, 0, 0)}
	for id, want := range map[string]int{"mummy": 5, "goblin": 3, "giant_rat": 2} {
		target := rulestest.New(t, eng, id, 7, 6, 100)
		w := &rules.World{Map: rulestest.Room(), Player: p, Monsters: []*rules.Entity{target}}
		if got := eng.NewCtx(w).Melee(p, target).Damage; got != want {
			t.Errorf("flaming sword vs %s: %d damage, want %d", id, got, want)
		}
	}
}
