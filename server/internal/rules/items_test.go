package rules_test

import (
	"strconv"
	"strings"
	"testing"

	"micro/server/internal/rules"
	"micro/server/internal/rules/rulestest"
)

func scene(t *testing.T) (*rules.Engine, *rules.World, *rules.Entity) {
	eng := rulestest.Engine(t)
	p := rulestest.New(t, eng, "player", 6, 6, 20)
	return eng, &rules.World{Map: rulestest.Room(), Player: p}, p
}

func TestHealingPotionHealsUpToMaxHPAndIsUsedUp(t *testing.T) {
	eng, w, p := scene(t)
	potion := rulestest.Give(p, rulestest.New(t, eng, "healing_potion", 0, 0, 0))
	amount := potion.Uses[0].Params.Int("amount")
	if amount < 8 || amount > 15 {
		t.Fatalf("healing amount %d should be rolled from 8-15 when the potion is made", amount)
	}

	p.HP = 1
	res := eng.NewCtx(w).Use(p, potion)
	if p.HP != 1+amount || len(p.Inventory) != 0 {
		t.Errorf("HP %d (want %d), inventory %v", p.HP, 1+amount, p.Inventory)
	}
	want := "You drink the healing potion and recover "
	if !strings.HasPrefix(res.Msg, want) {
		t.Errorf("message %q", res.Msg)
	}

	p.HP = p.MaxHP - 1
	potion = rulestest.Give(p, rulestest.New(t, eng, "healing_potion", 0, 0, 0))
	res = eng.NewCtx(w).Use(p, potion)
	if p.HP != p.MaxHP || !strings.Contains(res.Msg, "recover 1 HP") {
		t.Errorf("healing near full: HP %d/%d, %q", p.HP, p.MaxHP, res.Msg)
	}
}

// TestInvisibilityWearsOffAfterItsTurns: granted for N turns, it counts
// down from the turn it is drunk and is gone after N ticks.
func TestInvisibilityWearsOffAfterItsTurns(t *testing.T) {
	eng, w, p := scene(t)
	potion := rulestest.Give(p, rulestest.New(t, eng, "invisibility_potion", 0, 0, 0))
	turns := potion.Uses[0].Params.Int("turns")

	res := eng.NewCtx(w).Use(p, potion)
	if !eng.Reg.Has(p, "invisible") {
		t.Fatal("drinking should make the player invisible at once")
	}
	if res.Msg != "You drink the invisibility potion and turn invisible." {
		t.Errorf("message %q", res.Msg)
	}
	for i := 1; i < turns; i++ {
		c := eng.NewCtx(w)
		c.Tick(p)
		if !eng.Reg.Has(p, "invisible") || len(c.Messages()) != 0 {
			t.Fatalf("still invisible after %d of %d ticks, quietly", i, turns)
		}
	}
	c := eng.NewCtx(w)
	c.Tick(p)
	if eng.Reg.Has(p, "invisible") {
		t.Error("invisibility should have worn off")
	}
	if got := strings.Join(c.Messages(), ""); got != "Your invisibility wears off." {
		t.Errorf("expiry message %q", got)
	}
}

func TestOverlappingInvisibilityLastsUntilTheLongerOneEnds(t *testing.T) {
	eng, w, p := scene(t)
	p.AddTrait(&rules.TraitInstance{Trait: "invisible", Turns: 1})
	p.AddTrait(&rules.TraitInstance{Trait: "invisible", Turns: 3})
	eng.NewCtx(w).Tick(p)
	if !eng.Reg.Has(p, "invisible") {
		t.Error("one potion expiring must not end the other")
	}
}

func TestTorchLightsBurnsDownAndBurnsOut(t *testing.T) {
	eng, w, p := scene(t)
	torch := rulestest.Give(p, rulestest.New(t, eng, "torch", 0, 0, 0))
	turns := torch.Uses[0].Params.Int("turns")
	if turns < 6 || turns > 20 {
		t.Fatalf("lifespan %d should be rolled from 6-20", turns)
	}

	res := eng.NewCtx(w).Use(p, torch)
	if p.Lit() != torch || len(p.Inventory) != 0 || !eng.Reg.Has(p, "light") {
		t.Fatal("lighting should move the torch into the light slot and light the player")
	}
	if res.Msg != "You light the torch. It should burn for "+strconv.Itoa(turns)+" turns." {
		t.Errorf("message %q", res.Msg)
	}

	second := rulestest.Give(p, rulestest.New(t, eng, "torch", 0, 0, 0))
	if res := eng.NewCtx(w).Use(p, second); res.Msg != "Your torch is already lit." || len(p.Inventory) != 1 {
		t.Errorf("a second torch should be refused and kept: %q", res.Msg)
	}

	for i := 1; i < turns; i++ {
		eng.NewCtx(w).Tick(p)
	}
	if !eng.Reg.Has(p, "light") {
		t.Fatal("burned out a turn early")
	}
	c := eng.NewCtx(w)
	c.Tick(p)
	if eng.Reg.Has(p, "light") || p.Lit() != nil {
		t.Error("the torch should be gone")
	}
	if got := strings.Join(c.Messages(), ""); got != "Your torch burns out." {
		t.Errorf("message %q", got)
	}
}

func TestTeleportMovesToAFreeWalkableTile(t *testing.T) {
	eng, w, p := scene(t)
	g := rulestest.New(t, eng, "goblin", 8, 8, 5)
	w.Monsters = []*rules.Entity{g}
	for i := 0; i < 50; i++ {
		x, y := p.X, p.Y
		scroll := rulestest.Give(p, rulestest.New(t, eng, "teleport_scroll", 0, 0, 0))
		res := eng.NewCtx(w).Use(p, scroll)
		if res.Msg != "You read the scroll of teleportation and are teleported away!" || len(p.Inventory) != 0 {
			t.Fatalf("message %q, inventory %v", res.Msg, p.Inventory)
		}
		if (p.X == x && p.Y == y) || (p.X == g.X && p.Y == g.Y) {
			t.Fatalf("teleported onto an occupied tile (%d,%d)", p.X, p.Y)
		}
		if _, ok := w.Map.RoomAt(p.X, p.Y); !ok {
			t.Fatalf("teleported off the floor to (%d,%d)", p.X, p.Y)
		}
	}
}

func TestEquipSwapsAndWearingSneakingShoesMakesYouSneak(t *testing.T) {
	eng, w, p := scene(t)
	dagger := rulestest.Give(p, rulestest.New(t, eng, "fine_dagger", 0, 0, 0))
	other := rulestest.Give(p, rulestest.New(t, eng, "fine_dagger", 0, 0, 0))
	shoes := rulestest.Give(p, rulestest.New(t, eng, "shoes_of_sneaking", 0, 0, 0))
	c := eng.NewCtx(w)

	if res := c.Equip(p, dagger); res.Msg != "You wield the fine dagger." {
		t.Errorf("%q", res.Msg)
	}
	c.Equip(p, other)
	if p.Weapon() != other || len(p.Inventory) != 2 || p.Inventory[1] != dagger {
		t.Error("wielding a second weapon should put the first back in the pack")
	}
	if res := c.Equip(p, shoes); res.Msg != "You put on the shoes of sneaking." || !eng.Reg.Has(p, "sneaking") {
		t.Errorf("%q, sneaking %v", res.Msg, eng.Reg.Has(p, "sneaking"))
	}

	res, dropped := c.Drop(p, shoes)
	if res.Msg != "You take off the shoes of sneaking and drop it." || dropped != shoes || eng.Reg.Has(p, "sneaking") {
		t.Errorf("%q, dropped %v, sneaking %v", res.Msg, dropped, eng.Reg.Has(p, "sneaking"))
	}
	if shoes.X != p.X || shoes.Y != p.Y {
		t.Error("a dropped item lands at the dropper's feet")
	}
	res, _ = c.Drop(p, other)
	if res.Msg != "You drop the fine dagger. You are now fighting bare-handed." || p.Weapon() != nil {
		t.Errorf("%q", res.Msg)
	}
}

// TestCursedSwordCannotBeLetGo is the curse's second half: in combat it
// weakens the sword, and out of combat, once wielded, it refuses to leave
// the hand — not by dropping, not by throwing, not by swapping for another
// weapon. Refusals do not spend a turn.
func TestCursedSwordCannotBeLetGo(t *testing.T) {
	eng, w, p := scene(t)
	sword := rulestest.Give(p, rulestest.New(t, eng, "rusty_sword", 0, 0, 0))
	dagger := rulestest.Give(p, rulestest.New(t, eng, "fine_dagger", 0, 0, 0))
	c := eng.NewCtx(w)

	if res, _ := c.Drop(p, sword); res.Refused {
		t.Fatal("an unwielded cursed sword is just a sword: it can be dropped")
	}
	rulestest.Give(p, sword)
	c.Equip(p, sword)

	const want = "The rusty sword is welded to your hand!"
	if res := c.Equip(p, dagger); !res.Refused || res.Msg != want || p.Weapon() != sword {
		t.Errorf("swap: %+v", res)
	}
	if res, dropped := c.Drop(p, sword); !res.Refused || res.Msg != want || dropped != nil || p.Weapon() != sword {
		t.Errorf("drop: %+v", res)
	}
	if res := c.Throw(p, sword, 1, 0); !res.Refused || res.Msg != want || p.Weapon() != sword {
		t.Errorf("throw: %+v", res)
	}
}

func TestDroppingALitTorchDestroysIt(t *testing.T) {
	eng, w, p := scene(t)
	torch := rulestest.Give(p, rulestest.New(t, eng, "torch", 0, 0, 0))
	rules.Light(p, torch, 10)
	res, dropped := eng.NewCtx(w).Drop(p, torch)
	if dropped != nil || res.Msg != "You drop the torch. It goes out and falls apart." {
		t.Errorf("dropped %v, %q", dropped, res.Msg)
	}
}

func TestStairsCannotBePickedUp(t *testing.T) {
	eng, w, p := scene(t)
	stairs := rulestest.New(t, eng, "stairs_down", p.X, p.Y, 0)
	if !eng.Reg.Has(stairs, "fixed") {
		t.Error("stairs should be fixed")
	}
	if res, ok := eng.NewCtx(w).PickUp(p, stairs); ok || !res.Refused {
		t.Errorf("picked up the stairs: %+v", res)
	}
}

func TestPickUpStopsAtInventoryCapacity(t *testing.T) {
	eng, w, p := scene(t)
	c := eng.NewCtx(w)
	for i := 0; i < rules.InventoryCapacity; i++ {
		if _, ok := c.PickUp(p, rulestest.New(t, eng, "torch", 0, 0, 0)); !ok {
			t.Fatalf("pickup %d refused", i)
		}
	}
	res, ok := c.PickUp(p, rulestest.New(t, eng, "torch", 0, 0, 0))
	if ok || res.Refused || res.Msg != "Your inventory is full." {
		t.Errorf("a full pack is not a refusal, it is a wasted turn: %+v", res)
	}
}

func TestPotionShattersWhenThrownButLandsWhenDropped(t *testing.T) {
	eng, w, p := scene(t)
	res := eng.NewCtx(w).Throw(p, rulestest.Give(p, rulestest.New(t, eng, "healing_potion", 0, 0, 0)), 1, 0)
	if res.Landed != nil || strings.Join(res.Notes, "") != "It shatters!" {
		t.Errorf("thrown: %+v", res)
	}
	_, dropped := eng.NewCtx(w).Drop(p, rulestest.Give(p, rulestest.New(t, eng, "healing_potion", 0, 0, 0)))
	if dropped == nil {
		t.Error("a dropped potion should land intact")
	}
}

func TestThrownItemLandsWhereItStops(t *testing.T) {
	eng, w, p := scene(t)
	p.Attrs.Strength = 10
	dagger := rulestest.Give(p, rulestest.New(t, eng, "fine_dagger", 0, 0, 0))
	res := eng.NewCtx(w).Throw(p, dagger, 0, 1)
	if res.Landed != dagger || dagger.X != p.X || dagger.Y <= p.Y {
		t.Errorf("landed at (%d,%d) from (%d,%d)", dagger.X, dagger.Y, p.X, p.Y)
	}
}

func TestDeathLeavesACorpseAndDropsWhatWasCarried(t *testing.T) {
	eng, w, _ := scene(t)
	// Only the sword: a goblin may also spawn in a helm or with a shield.
	g := eng.Spawn(eng.Cat.Get("goblin"), rulestest.Room(), 0, func(es rules.EquipSpec) bool { return es.Item.ID == "rusty_sword" })
	g.AddStacks("oily", 2)
	g.Dead = true
	dropped := eng.NewCtx(w).Kill(g)
	if len(dropped) != 2 {
		t.Fatalf("want a corpse and a sword, got %d items", len(dropped))
	}
	corpse, sword := dropped[0], dropped[1]
	if corpse.Name != "goblin corpse" || corpse.Weight != 50 || corpse.Source != g.ID || corpse.Stacks("oily") != 2 {
		t.Errorf("corpse %+v", corpse)
	}
	if sword.Name != "rusty sword" || sword.X != g.X || sword.Y != g.Y || g.Weapon() != nil {
		t.Error("the goblin's sword should drop where it fell")
	}
	if g.HP == 0 && g.MaxHP == 0 {
		t.Error("the dead goblin keeps its own stats")
	}
}

func TestStatusListsEachConditionOnce(t *testing.T) {
	eng, _, p := scene(t)
	p.AddStacks("oily", 2)
	p.AddTrait(&rules.TraitInstance{Trait: "on_fire", Turns: 2})
	p.AddTrait(&rules.TraitInstance{Trait: "invisible", Turns: 5})
	p.AddTrait(&rules.TraitInstance{Trait: "invisible", Turns: 9})
	p.Equipped = map[rules.Slot]*rules.Entity{rules.SlotFeet: rulestest.New(t, eng, "shoes_of_sneaking", 0, 0, 0)}

	var got []string
	for _, f := range eng.Reg.Status(p) {
		got = append(got, f.Label+": "+f.Value)
	}
	want := "Level: 1 | HP: 20/20 | Strength: 0 | Intelligence: 0 | Dexterity: 0 | Stamina: 0 | Healing: 1 HP / 120 turns | Oily: x2 | Burning: yes | Invisible: yes | Sneaking: yes"
	if s := strings.Join(got, " | "); s != want {
		t.Errorf("status\n got %s\nwant %s", s, want)
	}
}

// TestTheScrollOfAwarenessGrantsMonsterVisionForAFewTurns: up to 10
// turns, rolled per scroll, then it fades with a message.
func TestTheScrollOfAwarenessGrantsMonsterVisionForAFewTurns(t *testing.T) {
	eng, w, p := scene(t)
	scroll := rulestest.Give(p, rulestest.New(t, eng, "awareness_scroll", 0, 0, 0))
	turns := scroll.Uses[0].Params.Int("turns")
	if turns < 5 || turns > 10 {
		t.Fatalf("lasts %d turns, want 5-10", turns)
	}

	res := eng.NewCtx(w).Use(p, scroll)
	if res.Msg != "You read the scroll of awareness and sense every monster on the level." {
		t.Errorf("message %q", res.Msg)
	}
	if !eng.Reg.Has(p, "monster_vision") || len(p.Inventory) != 0 {
		t.Fatal("expected monster vision, and the scroll used up")
	}
	for i := 1; i < turns; i++ {
		eng.NewCtx(w).Tick(p)
	}
	if !eng.Reg.Has(p, "monster_vision") {
		t.Fatal("faded a turn early")
	}
	c := eng.NewCtx(w)
	c.Tick(p)
	if eng.Reg.Has(p, "monster_vision") || strings.Join(c.Messages(), "") != "Your monster vision fades." {
		t.Errorf("should have faded, saying so: %q", c.Messages())
	}
}
