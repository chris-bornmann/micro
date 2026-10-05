package rules_test

import (
	"strings"
	"testing"
	"testing/fstest"

	"micro/server/internal/content"
	"micro/server/internal/rules"
	"micro/server/internal/rules/builtin"
	"micro/server/internal/rules/rulestest"
)

// TestAMonsterUsesItemsInItsOwnWords: the same actions the player uses,
// told of the monster.
func TestAMonsterUsesItemsInItsOwnWords(t *testing.T) {
	eng, w, _ := scene(t)
	g := rulestest.New(t, eng, "goblin", 8, 8, 20)
	g.HP = 5
	w.Monsters = []*rules.Entity{g}

	cases := []struct {
		item, want string
	}{
		{"healing_potion", "The goblin drinks a healing potion and looks healthier."},
		{"invisibility_potion", "The goblin drinks an invisibility potion and vanishes from sight."},
		{"teleport_scroll", "The goblin reads a scroll of teleportation and vanishes!"},
	}
	for _, tc := range cases {
		c := eng.NewCtx(w)
		c.Use(g, rulestest.Give(g, rulestest.New(t, eng, tc.item, 0, 0, 0)))
		if got := strings.Join(c.Messages(), " | "); got != tc.want {
			t.Errorf("%s: %q, want %q", tc.item, got, tc.want)
		}
	}
	if g.HP <= 5 || len(g.Inventory) != 0 {
		t.Errorf("HP %d, carrying %d: the potion should heal and the items be used up", g.HP, len(g.Inventory))
	}
}

func TestAPlayersUseIsNotAlsoAnEvent(t *testing.T) {
	eng, w, p := scene(t)
	c := eng.NewCtx(w)
	res := c.Use(p, rulestest.Give(p, rulestest.New(t, eng, "healing_potion", 0, 0, 0)))
	if !strings.HasPrefix(res.Msg, "You drink the healing potion") || len(c.Events) != 0 {
		t.Errorf("the player's use is the caller's to report: %q, events %v", res.Msg, c.Messages())
	}
}

const packContent = `
player:
  category: player
  name: Player
  glyph: "@"
  color: [255, 255, 255]
  weight: 100
  hp: 10
  attributes: 0
  attack: {name: hands, damage: {bludgeoning: 1}}
potion:
  category: potion
  name: potion
  glyph: "!"
  color: [1, 1, 1]
  weight: 1
  on_use: [{action: heal, amount: 5}]
corpse: {category: corpse, name: corpse, glyph: "%", color: [1, 1, 1], weight: 1}
peddler:
  category: monster
  name: Peddler
  glyph: p
  color: [1, 1, 1]
  weight: 50
  level: 1
  hp: 10
  attributes: 0
  attack: {name: fists, damage: {bludgeoning: 1}}
  ai: {behavior: chase}
  spawn: {model: common, density: 1, sigma_up: 1, sigma_down: 1, max_above: 1}
  inventory:
    - {item: potion}
    - {item: potion, chance: 0}
`

func TestStartingInventory(t *testing.T) {
	reg := builtin.Registry()
	cat, err := content.LoadFS(fstest.MapFS{"c.yaml": {Data: []byte(packContent)}}, reg)
	if err != nil {
		t.Fatal(err)
	}
	eng := &rules.Engine{Reg: reg, Cat: cat}
	for i := 0; i < 20; i++ {
		p := eng.Spawn(cat.Get("peddler"), rulestest.Room(), i, nil)
		if len(p.Inventory) != 1 || p.Inventory[0].Name != "potion" || len(p.Equipped) != 0 {
			t.Fatalf("want exactly the certain potion, carried not equipped; got %d items", len(p.Inventory))
		}
	}
}
