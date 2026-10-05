package content

import (
	"strings"
	"testing"
	"testing/fstest"

	"micro/server/internal/rules"
	"micro/server/internal/rules/builtin"
)

func TestBuiltInContentLoads(t *testing.T) {
	cat, err := Load(builtin.Registry())
	if err != nil {
		t.Fatalf("built-in content failed to load:\n%v", err)
	}

	mummy := cat.Get("mummy")
	if mummy == nil || len(mummy.Traits) == 0 || mummy.Traits[0].Trait != "vulnerable" {
		t.Fatalf("mummy should lead with its fire vulnerability, got %+v", mummy)
	}
	if got := mummy.Traits[0].Params.Float("multiplier"); got != 2 {
		t.Errorf("mummy fire multiplier = %v, want 2", got)
	}

	goblin := cat.Get("goblin")
	if len(goblin.Equipment) < 2 || goblin.Equipment[0].Item != cat.Get("rusty_sword") || goblin.Equipment[1].Item != cat.Get("poisoned_dagger") {
		t.Errorf("goblin equipment should resolve to the rusty sword then the poisoned dagger first; got %+v", goblin.Equipment)
	}
	if goblin.Layer != 1 || cat.Get("player").Layer != 2 || cat.Get("torch").Layer != 0 {
		t.Error("default layers should put the player over monsters over items")
	}

	heal := cat.Get("healing_potion").OnUse[0].Params["amount"]
	if heal != (rules.Range{Min: 8, Max: 15}) {
		t.Errorf("healing amount should stay a range until a potion is created, got %v", heal)
	}
	if !cat.Get("goblin").LeavesCorpse {
		t.Error("monsters leave a corpse unless told otherwise")
	}
}

// base is the smallest valid content: a player, and nothing that needs a
// corpse.
const base = `
player:
  category: player
  name: Player
  glyph: "@"
  color: [255, 255, 255]
  weight: 100
  hp: [10, 20]
  attributes: [6, 20]
  attack: {name: hands, damage: {bludgeoning: [1, 2]}}
`

func load(t *testing.T, files map[string]string) error {
	t.Helper()
	fsys := fstest.MapFS{}
	for name, body := range files {
		fsys[name] = &fstest.MapFile{Data: []byte(body)}
	}
	_, err := LoadFS(fsys, builtin.Registry())
	return err
}

func TestMinimalContentLoads(t *testing.T) {
	if err := load(t, map[string]string{"player.yaml": base}); err != nil {
		t.Fatalf("minimal content should load: %v", err)
	}
}

// TestWeightlessThingsLoad: a phantom weighs nothing, and that is allowed.
func TestWeightlessThingsLoad(t *testing.T) {
	if err := load(t, map[string]string{"a.yaml": base + "\nwisp: {category: corpse, name: wisp, glyph: w, color: [1, 1, 1], weight: 0}\n"}); err != nil {
		t.Fatalf("weight 0 should load: %v", err)
	}
}

// TestLoadErrors pins that each class of mistake is caught at load time
// and explained well enough to fix: each case's error must mention every
// listed fragment.
func TestLoadErrors(t *testing.T) {
	item := func(extra string) string {
		return base + `
thing:
  category: weapon
  name: thing
  glyph: ")"
  color: [1, 2, 3]
  weight: 1
` + extra
	}
	cases := []struct {
		name  string
		files map[string]string
		want  []string
	}{
		{"unknown field", map[string]string{"a.yaml": item("  damge: {physical: [1, 2]}\n")},
			[]string{"a.yaml", "line 18", `unknown field "damge"`}},
		{"unknown trait", map[string]string{"a.yaml": item("  traits: [{trait: blessed}]\n")},
			[]string{"a.yaml:18", "thing", `unknown trait "blessed"`, "cursed"}},
		{"unknown parameter", map[string]string{"a.yaml": item("  traits: [{trait: cursed, damag: -1}]\n")},
			[]string{"traits[0] (cursed)", `unknown parameter "damag"`, "accepted: damage"}},
		{"wrong parameter type", map[string]string{"a.yaml": item("  traits: [{trait: cursed, damage: lots}]\n")},
			[]string{`parameter "damage"`, "want integer or [min, max]"}},
		{"missing required parameter", map[string]string{"a.yaml": item("  traits: [{trait: enchant}]\n")},
			[]string{`missing required parameter "amount"`}},
		{"bad damage type in a parameter", map[string]string{"a.yaml": item("  traits: [{trait: resist, type: acid}]\n")},
			[]string{`unknown damage type "acid"`}},
		{"parameter above its maximum", map[string]string{"a.yaml": item("  traits: [{trait: invisible, periodic_visible: 150}]\n")},
			[]string{`parameter "periodic_visible": 150 is above the maximum 100`}},
		{"parameter below its minimum", map[string]string{"a.yaml": item("  traits: [{trait: invisible, visible_below: -1}]\n")},
			[]string{`parameter "visible_below": -1 is below the minimum 0`}},
		{"range reaching past a bound", map[string]string{"a.yaml": item("  traits: [{trait: invisible, periodic_visible: [50, 101]}]\n")},
			[]string{"101 is above the maximum 100"}},
		{"negative weight", map[string]string{"a.yaml": base + "\nx: {category: corpse, name: x, glyph: x, color: [1, 1, 1], weight: -1}\n"},
			[]string{"weight must not be negative"}},
		{"unknown placeholder", map[string]string{"a.yaml": item("  messages: {hit: [\"{The attacker} bonks you for {dmg}!\"]}\n")},
			[]string{"messages.hit[0]", "unknown placeholder {dmg}", "{damage}"}},
		{"monster lines on the player", map[string]string{"a.yaml": base + "  messages: {hit: [\"Ouch.\"]}\n"},
			[]string{"messages.hit", "described by you_hit and you_miss"}},
		{"bad pronoun", map[string]string{"a.yaml": base + "  pronoun: xe\n"},
			[]string{`pronoun must be one of he, she, it, they, got "xe"`}},
		{"physical as an attack's damage type", map[string]string{"a.yaml": item("  slot: weapon\n  damage: {physical: [1, 6]}\n")},
			[]string{`unknown damage type "physical"`, "an attack deals slashing, piercing, bludgeoning, fire"}},
		{"poison that never lasts", map[string]string{"a.yaml": item("  traits: [{trait: poisons, damage: 4, turns: 0}]\n")},
			[]string{`parameter "turns": 0 is below the minimum 1`}},
		{"curing an unknown trait", map[string]string{"a.yaml": item("  on_use: [{action: cure, trait: plague}]\n")},
			[]string{`unknown trait "plague"`}},
		{"a trap without a spawn", map[string]string{"a.yaml": base + "\nt:\n  category: trap\n  name: t\n  glyph: \"^\"\n  color: [1, 1, 1]\n  weight: 0\n  traits: [{trait: pit}]\n"},
			[]string{"a trap needs spawn"}},
		{"a trap spawned like an item", map[string]string{"a.yaml": base + "\nt:\n  category: trap\n  name: t\n  glyph: \"^\"\n  color: [1, 1, 1]\n  weight: 0\n  traits: [{trait: pit}]\n" + "  spawn: {model: fixed, count: 1}\n"},
			[]string{"traps spawn with model per_level"}},
		{"per_level out of bounds", map[string]string{"a.yaml": base + "\nt:\n  category: trap\n  name: t\n  glyph: \"^\"\n  color: [1, 1, 1]\n  weight: 0\n  traits: [{trait: pit}]\n" + "  spawn: {model: per_level, levels: [0, 30], chance: 150, count: 0, always_on: [40]}\n"},
			[]string{"levels: [first, last] within 1-25", "chance must be 0-100", "count: n or [min, max]", "always_on level 40 is outside"}},
		{"per_level on an item", map[string]string{"a.yaml": item("  spawn: {model: per_level, levels: [1, 2], chance: 5, count: 1}\n")},
			[]string{"only traps spawn with model per_level"}},
		{"a fixed spawn with a range", map[string]string{"a.yaml": item("  spawn: {model: fixed, count: [1, 2]}\n")},
			[]string{"fixed needs a single count"}},
		{"a unique monster in lower case", map[string]string{"a.yaml": base + "\ncorpse: {category: corpse, name: c, glyph: \"%\", color: [1, 1, 1], weight: 1}\nx:\n  category: monster\n  name: X\n  color: [1, 1, 1]\n  weight: 1\n  level: 1\n  hp: 1\n  attributes: 0\n  attack: {name: h, damage: {bludgeoning: 1}}\n  ai: {behavior: chase}\n" + "  unique: true\n  glyph: x\n  spawn: {model: single_shot, earliest: 1, force_by: 2, chance_scale: 1, sigma_up: 1, sigma_down: 1, max_above: 1}\n"},
			[]string{"a unique monster's glyph is an upper-case letter"}},
		{"an ordinary monster in upper case", map[string]string{"a.yaml": base + "\ncorpse: {category: corpse, name: c, glyph: \"%\", color: [1, 1, 1], weight: 1}\nx:\n  category: monster\n  name: X\n  color: [1, 1, 1]\n  weight: 1\n  level: 1\n  hp: 1\n  attributes: 0\n  attack: {name: h, damage: {bludgeoning: 1}}\n  ai: {behavior: chase}\n" + "  glyph: X\n  spawn: {model: common, density: 1, sigma_up: 1, sigma_down: 1, max_above: 1}\n"},
			[]string{"an upper-case glyph marks a unique monster"}},
		{"a unique monster spawning commonly", map[string]string{"a.yaml": base + "\ncorpse: {category: corpse, name: c, glyph: \"%\", color: [1, 1, 1], weight: 1}\nx:\n  category: monster\n  name: X\n  color: [1, 1, 1]\n  weight: 1\n  level: 1\n  hp: 1\n  attributes: 0\n  attack: {name: h, damage: {bludgeoning: 1}}\n  ai: {behavior: chase}\n" + "  unique: true\n  glyph: X\n  spawn: {model: common, density: 1, sigma_up: 1, sigma_down: 1, max_above: 1}\n"},
			[]string{"a unique monster spawns with model single_shot"}},
		{"bad slot", map[string]string{"a.yaml": item("  slot: tail\n")},
			[]string{`unknown slot "tail"`, "feet"}},
		{"weapon without damage", map[string]string{"a.yaml": item("  slot: weapon\n")},
			[]string{"needs damage"}},
		{"unknown action", map[string]string{"a.yaml": item("  on_use: [{action: explode}]\n")},
			[]string{`unknown action "explode"`, "teleport"}},
		{"granting a trait that needs parameters", map[string]string{"a.yaml": item("  on_use: [{action: grant, trait: enchant, turns: 3, describe: glow}]\n")},
			[]string{`trait "enchant" needs parameter "amount"`}},
		{"bad category", map[string]string{"a.yaml": base + "\nthing:\n  category: gadget\n  name: thing\n"},
			[]string{`unknown category "gadget"`}},
		{"reference to a missing template", map[string]string{"a.yaml": base + `
corpse: {category: corpse, name: corpse, glyph: "%", color: [1, 1, 1], weight: 1}
orc:
  category: monster
  name: Orc
  glyph: o
  color: [1, 1, 1]
  weight: 50
  level: 2
  hp: [5, 5]
  attributes: [2, 2]
  attack: {name: claws, damage: {slashing: [1, 3]}}
  ai: {behavior: chase}
  spawn: {model: common, density: 1, sigma_up: 1, sigma_down: 1, max_above: 1}
  equipment: [{item: axe}]
`}, []string{"orc", `no template named "axe"`}},
		{"monster needs a corpse template", map[string]string{"a.yaml": base + `
orc:
  category: monster
  name: Orc
  glyph: o
  color: [1, 1, 1]
  weight: 50
  level: 2
  hp: [5, 5]
  attributes: [2, 2]
  attack: {name: claws, damage: {slashing: [1, 3]}}
  ai: {behavior: sulk}
  spawn: {model: common, density: 1, sigma_up: 1, sigma_down: 1, max_above: 1}
`}, []string{`unknown behavior "sulk"`, "chase_and_flee", "template named corpse"}},
		{"duplicate across files", map[string]string{"a.yaml": base, "b.yaml": base},
			[]string{"player", "already defined at a.yaml"}},
		{"no player", map[string]string{"a.yaml": "x: {category: corpse, name: x, glyph: x, color: [1, 1, 1], weight: 1}\n"},
			[]string{"exactly one template with category player"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := load(t, tc.files)
			if err == nil {
				t.Fatal("expected a load error, got none")
			}
			for _, w := range tc.want {
				if !strings.Contains(err.Error(), w) {
					t.Errorf("error should mention %q; got:\n%v", w, err)
				}
			}
		})
	}
}
