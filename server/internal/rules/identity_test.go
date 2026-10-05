package rules_test

import (
	"math/rand"
	"strings"
	"testing"

	"micro/server/internal/rules"
	"micro/server/internal/rules/rulestest"
)

// unknownAll is the real content with every identifiable type made to
// start unknown, so a test can rely on it, dealt from seed.
func unknownAll(t *testing.T, eng *rules.Engine, seed int64) *rules.Identity {
	t.Helper()
	for _, tmpl := range eng.Cat.All() {
		if tmpl.Unidentified != nil {
			u := *tmpl.Unidentified
			u.Chance = 100
			tmpl.Unidentified = &u
		}
	}
	return rules.NewIdentity(eng.Cat, rand.New(rand.NewSource(seed)))
}

func item(t *testing.T, eng *rules.Engine, id string) *rules.Entity {
	return rulestest.New(t, eng, id, 0, 0, 0)
}

func TestTheSameSeedDealsTheSameLooks(t *testing.T) {
	eng := rulestest.Engine(t)
	a, b := unknownAll(t, eng, 7), unknownAll(t, eng, 7)
	differs := false
	for seed := int64(1); seed <= 20; seed++ {
		other := unknownAll(t, eng, seed)
		r := item(t, eng, "ring_of_true_sight")
		differs = differs || other.Name(r) != a.Name(r)
	}
	for _, tmpl := range eng.Cat.All() {
		if tmpl.Unidentified == nil {
			continue
		}
		e := item(t, eng, tmpl.ID)
		if a.Name(e) != b.Name(e) {
			t.Errorf("%s: %q vs %q from the same seed", tmpl.ID, a.Name(e), b.Name(e))
		}
	}
	if !differs {
		t.Error("twenty games all dealt the ring of true sight the same look")
	}
}

func TestEveryTypeInAPoolLooksDifferent(t *testing.T) {
	eng := rulestest.Engine(t)
	for seed := int64(1); seed <= 20; seed++ {
		id := unknownAll(t, eng, seed)
		seen := map[string]string{}
		for _, tmpl := range eng.Cat.All() {
			if tmpl.Unidentified == nil {
				continue
			}
			name := id.Name(item(t, eng, tmpl.ID))
			if prev, dup := seen[name]; dup {
				t.Fatalf("seed %d: %s and %s both look like %q", seed, prev, tmpl.ID, name)
			}
			seen[name] = tmpl.ID
			if name == tmpl.Name {
				t.Errorf("seed %d: %s shows its true name while unknown", seed, tmpl.ID)
			}
		}
	}
}

func TestChanceDecidesWhetherATypeStartsUnknown(t *testing.T) {
	eng := rulestest.Engine(t)
	r := eng.Cat.Get("ring_of_true_sight")
	for _, tc := range []struct{ chance, wantUnknown int }{{0, 0}, {100, 50}} {
		u := *r.Unidentified
		u.Chance = tc.chance
		r.Unidentified = &u
		unknown := 0
		for seed := int64(0); seed < 50; seed++ {
			if !rules.NewIdentity(eng.Cat, rand.New(rand.NewSource(seed))).Known(r.ID) {
				unknown++
			}
		}
		if unknown != tc.wantUnknown {
			t.Errorf("chance %d: unknown in %d of 50 games, want %d", tc.chance, unknown, tc.wantUnknown)
		}
	}
}

func TestIdentifyingOneIdentifiesEveryCopy(t *testing.T) {
	eng := rulestest.Engine(t)
	id := unknownAll(t, eng, 3)
	a, b := item(t, eng, "healing_potion"), item(t, eng, "healing_potion")
	if id.Name(a) != id.Name(b) || !strings.HasSuffix(id.Name(a), " potion") || id.Known("healing_potion") {
		t.Fatalf("two unknown healing potions: %q and %q", id.Name(a), id.Name(b))
	}
	id.Identify("healing_potion")
	if id.Name(a) != "healing potion" || id.Name(b) != "healing potion" {
		t.Errorf("after identifying: %q and %q", id.Name(a), id.Name(b))
	}
	if id.Color(a) != a.T.Color {
		t.Error("identified, it should be drawn in its own color")
	}
}

func TestArticles(t *testing.T) {
	for name, want := range map[string]string{"agate ring": "an agate ring", "jade ring": "a jade ring", "opal ring": "an opal ring"} {
		if got := rules.A(name); got != want {
			t.Errorf("A(%q) = %q", name, got)
		}
	}
}

// TestMessagesNameTheLook: picking up, putting on, attacking with and
// failing to take off an unknown item all call it by its look.
func TestMessagesNameTheLook(t *testing.T) {
	eng := rulestest.Engine(t)
	id := unknownAll(t, eng, 5)
	p := rulestest.New(t, eng, "player", 6, 6, 100)
	g := rulestest.New(t, eng, "goblin", 7, 6, 100)
	w := &rules.World{Map: rulestest.Room(), Player: p, Monsters: []*rules.Entity{g}, Identity: id}

	ringItem := item(t, eng, "ring_of_true_sight")
	look := id.Name(ringItem)
	ringItem.X, ringItem.Y = 6, 6
	w.Floor = []*rules.Entity{ringItem}
	if res, _ := eng.NewCtx(w).PickUp(p, ringItem); res.Msg != "You pick up the "+look+"." {
		t.Errorf("pick up: %q", res.Msg)
	}

	sword := rulestest.Give(p, item(t, eng, "rusty_sword"))
	c := eng.NewCtx(w)
	if res := c.Equip(p, sword); res.Msg != "You wield the "+id.Name(sword)+"." {
		t.Errorf("wield: %q", res.Msg)
	}
	if res, _ := eng.NewCtx(w).Drop(p, sword); res.Msg != "The "+id.Name(sword)+" is welded to your hand!" {
		t.Errorf("curse: %q", res.Msg)
	}
	c = eng.NewCtx(w)
	p.Attrs.Strength = 30 // +3: never a glancing 0, which names no weapon
	c.Melee(p, g)
	if msg := strings.Join(c.Messages(), " "); !strings.Contains(msg, id.Name(sword)) || strings.Contains(msg, "rusty") {
		t.Errorf("attack: %q should name the sword by its look, %q", msg, id.Name(sword))
	}

	potion := rulestest.Give(g, item(t, eng, "healing_potion"))
	c = eng.NewCtx(w)
	c.Use(g, potion)
	if msg := strings.Join(c.Messages(), ""); !strings.HasPrefix(msg, "The goblin drinks "+rules.A(id.Name(potion))) {
		t.Errorf("a monster's use: %q", msg)
	}
}

func TestAnUnknownItemIsDrawnInItsLooksColor(t *testing.T) {
	eng := rulestest.Engine(t)
	id := unknownAll(t, eng, 9)
	potion := item(t, eng, "healing_potion")
	if got := eng.Reg.Object(potion, id).Color; got != id.Color(potion) || got == potion.T.Color {
		t.Errorf("drawn %v, look %v, own %v", got, id.Color(potion), potion.T.Color)
	}
}
