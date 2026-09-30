package main

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"map_api/attributes"
	"map_api/flask"
	"map_api/item"
	"map_api/mapdata"
	"map_api/overlay"
	"map_api/potion"
	"map_api/weapon"
)

// testPerson/testMonster mirror player.person/goblin.monster's JSON
// shape closely enough for these tests to build and inspect PlayerState/
// GoblinState directly — hack never imports either package's private
// type, so these tests can't either; this is the same technique
// playerObject/goblinObject already rely on (decoding only the fields
// they need) taken one step further, to also construct fixtures.
type testPerson struct {
	ID            string
	X, Y          int
	Glyph         rune
	Color         overlay.Color
	Layer         int
	Dead          bool `json:"dead,omitempty"`
	Invisible     bool `json:"invisible,omitempty"`
	Lit           bool `json:"lit,omitempty"`
	Sneaking      bool `json:"sneaking,omitempty"`
	HP            int
	MaxHP         int
	Inventory     []json.RawMessage       `json:"inventory,omitempty"`
	ActiveEffects []item.ActiveEffect     `json:"active_effects,omitempty"`
	Wielded       *testWielded            `json:"wielded,omitempty"`
	LitTorch      *testLitTorch           `json:"lit_torch,omitempty"`
	Worn          map[item.Slot]*testWorn `json:"worn,omitempty"`
	Attributes    attributes.Attributes   `json:"attributes"`
}

// testWorn mirrors player.wornItem's JSON shape, for tests that need to
// inspect a worn item's stats through PlayerState directly.
type testWorn struct {
	Item  json.RawMessage `json:"item"`
	Stats item.ArmorStats `json:"stats"`
}

// testWielded mirrors player.wieldedWeapon's JSON shape, for tests that
// need to inspect a wielded weapon's stats through PlayerState directly.
type testWielded struct {
	Item  json.RawMessage  `json:"item"`
	Stats item.WeaponStats `json:"stats"`
}

// testLitTorch mirrors player.litTorch's JSON shape, for tests that need
// to inspect a lit torch's remaining turns through PlayerState directly.
type testLitTorch struct {
	Item           json.RawMessage `json:"item"`
	TurnsRemaining int             `json:"turns_remaining"`
}

func marshalPerson(t *testing.T, p testPerson) []byte {
	t.Helper()
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal person: %v", err)
	}
	return data
}

func unmarshalPerson(t *testing.T, data []byte) testPerson {
	t.Helper()
	var p testPerson
	if err := json.Unmarshal(data, &p); err != nil {
		t.Fatalf("unmarshal person: %v", err)
	}
	return p
}

// deadGoblinState is a Goblin JSON state that's already dead, so tests
// focused on items don't also have to account for the Goblin's turn.
func deadGoblinState(t *testing.T) []byte {
	t.Helper()
	return marshalPerson(t, testPerson{ID: "goblin:1", Dead: true})
}

// deadMonster is a GameState.Monsters entry for a monster of the given
// kind that's already dead, so tests focused on items/other concerns
// don't also have to account for that monster's own turn.
func deadMonster(t *testing.T, kind string) MonsterState {
	t.Helper()
	return MonsterState{Kind: kind, State: deadGoblinState(t)}
}

// monster wraps raw monster state (e.g. from marshalMonster) as a
// GameState.Monsters entry of the given kind — the test-construction
// counterpart to production code's own MonsterState literals.
func monster(kind string, state []byte) MonsterState {
	return MonsterState{Kind: kind, State: state}
}

// monsterStateOf returns the raw State of the first monster of the
// given kind in state.Monsters — a test convenience for the many
// existing single-goblin tests that predate GameState.Monsters becoming
// a polymorphic list. Fails the test if no such monster is present.
func monsterStateOf(t *testing.T, state *GameState, kind string) []byte {
	t.Helper()
	for _, ms := range state.Monsters {
		if ms.Kind == kind {
			return ms.State
		}
	}
	t.Fatalf("no %s monster found in state.Monsters", kind)
	return nil
}

// newTestGameState builds a single-level GameState for test fixtures —
// the construction-site counterpart to production code's own
// populateLevel. floorItems may be nil. Tests that also need OilyTiles
// or PendingSelection set them directly on the returned *GameState
// afterward (state.OilyTiles = ...) — both still work unchanged,
// OilyTiles via GameState's embedded *levelState field promotion and
// PendingSelection as GameState's own direct field.
func newTestGameState(m *mapdata.Map, playerState []byte, monsters []MonsterState, floorItems []json.RawMessage) *GameState {
	ls := &levelState{Map: m, Monsters: monsters, FloorItems: floorItems, Visited: newVisibility()}
	return &GameState{PlayerState: playerState, CurrentLevel: 1, Levels: []*levelState{ls}, levelState: ls}
}

// itemsTestMap is a single room big enough to place a player and an item
// without any corridor/movement concerns.
func itemsTestMap() *mapdata.Map {
	return &mapdata.Map{
		Width: 20, Height: 20,
		Rooms: []mapdata.Room{{ID: 0, X: 5, Y: 5, W: 10, H: 10}},
	}
}

// potionAt builds a real potion (via potion.Init, so its JSON shape is
// exactly what the potion package actually produces) and relocates it to
// (x,y) by editing its generic position fields directly.
func potionAt(t *testing.T, m *mapdata.Map, x, y int) json.RawMessage {
	t.Helper()
	mapJSON, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal map: %v", err)
	}
	raw, err := potion.Init(mapJSON)
	if err != nil {
		t.Fatalf("potion.Init: %v", err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatalf("unmarshal potion: %v", err)
	}
	xJSON, err := json.Marshal(x)
	if err != nil {
		t.Fatalf("marshal x: %v", err)
	}
	yJSON, err := json.Marshal(y)
	if err != nil {
		t.Fatalf("marshal y: %v", err)
	}
	fields["X"] = xJSON
	fields["Y"] = yJSON
	out, err := json.Marshal(fields)
	if err != nil {
		t.Fatalf("marshal relocated potion: %v", err)
	}
	return out
}

func healAmountOf(t *testing.T, raw json.RawMessage) int {
	t.Helper()
	var got struct{ HealAmount int }
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal heal amount: %v", err)
	}
	return got.HealAmount
}

// invisibilityPotionAt is potionAt's counterpart for an invisibility
// potion, built via potion.InitInvisibility.
func invisibilityPotionAt(t *testing.T, m *mapdata.Map, x, y int) json.RawMessage {
	t.Helper()
	mapJSON, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal map: %v", err)
	}
	raw, err := potion.InitInvisibility(mapJSON)
	if err != nil {
		t.Fatalf("potion.InitInvisibility: %v", err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatalf("unmarshal potion: %v", err)
	}
	xJSON, err := json.Marshal(x)
	if err != nil {
		t.Fatalf("marshal x: %v", err)
	}
	yJSON, err := json.Marshal(y)
	if err != nil {
		t.Fatalf("marshal y: %v", err)
	}
	fields["X"] = xJSON
	fields["Y"] = yJSON
	out, err := json.Marshal(fields)
	if err != nil {
		t.Fatalf("marshal relocated potion: %v", err)
	}
	return out
}

func durationOf(t *testing.T, raw json.RawMessage) int {
	t.Helper()
	var got struct{ Duration int }
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal duration: %v", err)
	}
	return got.Duration
}

// weaponAt is potionAt's counterpart for a weapon, built via weapon.Init.
func weaponAt(t *testing.T, m *mapdata.Map, x, y int) json.RawMessage {
	t.Helper()
	mapJSON, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal map: %v", err)
	}
	raw, err := weapon.Init(mapJSON)
	if err != nil {
		t.Fatalf("weapon.Init: %v", err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatalf("unmarshal weapon: %v", err)
	}
	xJSON, err := json.Marshal(x)
	if err != nil {
		t.Fatalf("marshal x: %v", err)
	}
	yJSON, err := json.Marshal(y)
	if err != nil {
		t.Fatalf("marshal y: %v", err)
	}
	fields["X"] = xJSON
	fields["Y"] = yJSON
	out, err := json.Marshal(fields)
	if err != nil {
		t.Fatalf("marshal relocated weapon: %v", err)
	}
	return out
}

// flaskAt is potionAt's counterpart for a flask of oil, built via
// flask.Init.
func flaskAt(t *testing.T, m *mapdata.Map, x, y int) json.RawMessage {
	t.Helper()
	mapJSON, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal map: %v", err)
	}
	raw, err := flask.Init(mapJSON)
	if err != nil {
		t.Fatalf("flask.Init: %v", err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatalf("unmarshal flask: %v", err)
	}
	xJSON, err := json.Marshal(x)
	if err != nil {
		t.Fatalf("marshal x: %v", err)
	}
	yJSON, err := json.Marshal(y)
	if err != nil {
		t.Fatalf("marshal y: %v", err)
	}
	fields["X"] = xJSON
	fields["Y"] = yJSON
	out, err := json.Marshal(fields)
	if err != nil {
		t.Fatalf("marshal relocated flask: %v", err)
	}
	return out
}

func TestPickUpMovesItemFromFloorToPlayerInventory(t *testing.T) {
	m := itemsTestMap()
	playerJSON := marshalPerson(t, testPerson{ID: "player:1", X: 7, Y: 7, HP: 15})
	itemJSON := potionAt(t, m, 7, 7)

	state := newTestGameState(m, playerJSON, []MonsterState{deadMonster(t, "goblin")}, []json.RawMessage{itemJSON})

	playerObj, err := playerObject(state.PlayerState)
	if err != nil {
		t.Fatalf("playerObject: %v", err)
	}
	msg, done, err := pickUp(state, playerObj)
	if err != nil {
		t.Fatalf("pickUp: %v", err)
	}
	if !done {
		t.Fatal("expected done=true with a single item at the tile")
	}
	if msg != "You pick up the healing potion." {
		t.Errorf("got message %q, want %q", msg, "You pick up the healing potion.")
	}
	if len(state.FloorItems) != 0 {
		t.Errorf("expected FloorItems empty after pickup, got %d", len(state.FloorItems))
	}
	got := unmarshalPerson(t, state.PlayerState)
	if len(got.Inventory) != 1 {
		t.Fatalf("expected 1 inventory item, got %d", len(got.Inventory))
	}
}

func TestPickUpReportsNothingHereWhenTileIsEmpty(t *testing.T) {
	m := itemsTestMap()
	playerJSON := marshalPerson(t, testPerson{ID: "player:1", X: 7, Y: 7, HP: 15})
	itemJSON := potionAt(t, m, 9, 9) // not at the player's tile

	state := newTestGameState(m, playerJSON, []MonsterState{deadMonster(t, "goblin")}, []json.RawMessage{itemJSON})

	playerObj, err := playerObject(state.PlayerState)
	if err != nil {
		t.Fatalf("playerObject: %v", err)
	}
	msg, done, err := pickUp(state, playerObj)
	if err != nil {
		t.Fatalf("pickUp: %v", err)
	}
	if !done {
		t.Fatal("expected done=true with nothing at the tile")
	}
	if msg != "There is nothing here to pick up." {
		t.Errorf("got message %q, want %q", msg, "There is nothing here to pick up.")
	}
	if len(state.FloorItems) != 1 {
		t.Errorf("expected FloorItems untouched, got %d", len(state.FloorItems))
	}
}

func TestUseItemAppliesEffectRemovesItFromInventoryAndRaisesHP(t *testing.T) {
	m := itemsTestMap()
	itemJSON := potionAt(t, m, 0, 0) // position irrelevant once carried
	heal := healAmountOf(t, itemJSON)

	playerJSON := marshalPerson(t, testPerson{ID: "player:1", X: 7, Y: 7, HP: 10, MaxHP: 100, Inventory: []json.RawMessage{itemJSON}})
	state := newTestGameState(m, playerJSON, []MonsterState{deadMonster(t, "goblin")}, nil)

	msg, done, err := useItem(state)
	if err != nil {
		t.Fatalf("useItem: %v", err)
	}
	if !done {
		t.Fatal("expected done=true with a single candidate")
	}
	wantMsg := "You drink the healing potion and recover " + strconv.Itoa(heal) + " HP (" + strconv.Itoa(10+heal) + " HP now)."
	if msg != wantMsg {
		t.Errorf("got message %q, want %q", msg, wantMsg)
	}
	got := unmarshalPerson(t, state.PlayerState)
	if got.HP != 10+heal {
		t.Errorf("got HP %d, want %d", got.HP, 10+heal)
	}
	if len(got.Inventory) != 0 {
		t.Errorf("expected inventory empty after use, got %d", len(got.Inventory))
	}
}

func TestUseItemReportsNothingToUseWhenInventoryIsEmpty(t *testing.T) {
	playerJSON := marshalPerson(t, testPerson{ID: "player:1", X: 7, Y: 7, HP: 10})
	state := newTestGameState(itemsTestMap(), playerJSON, []MonsterState{deadMonster(t, "goblin")}, nil)

	msg, done, err := useItem(state)
	if err != nil {
		t.Fatalf("useItem: %v", err)
	}
	if !done {
		t.Fatal("expected done=true with zero candidates")
	}
	if msg != "You have nothing to use." {
		t.Errorf("got message %q, want %q", msg, "You have nothing to use.")
	}
}

func TestHandleKeyPickupThenUseEndToEnd(t *testing.T) {
	m := itemsTestMap()
	itemJSON := potionAt(t, m, 7, 7)
	heal := healAmountOf(t, itemJSON)

	playerJSON := marshalPerson(t, testPerson{ID: "player:1", X: 7, Y: 7, HP: 10, MaxHP: 100})
	state := newTestGameState(m, playerJSON, []MonsterState{deadMonster(t, "goblin")}, []json.RawMessage{itemJSON})

	if _, err := handleKey(state, ","); err != nil {
		t.Fatalf("handleKey(\",\"): %v", err)
	}
	if len(state.FloorItems) != 0 {
		t.Fatalf("expected FloorItems empty after pickup, got %d", len(state.FloorItems))
	}
	afterPickup := unmarshalPerson(t, state.PlayerState)
	if len(afterPickup.Inventory) != 1 {
		t.Fatalf("expected 1 inventory item after pickup, got %d", len(afterPickup.Inventory))
	}

	if _, err := handleKey(state, "A"); err != nil {
		t.Fatalf("handleKey(\"A\"): %v", err)
	}
	afterUse := unmarshalPerson(t, state.PlayerState)
	if len(afterUse.Inventory) != 0 {
		t.Errorf("expected inventory empty after use, got %d", len(afterUse.Inventory))
	}
	if afterUse.HP != 10+heal {
		t.Errorf("got HP %d, want %d", afterUse.HP, 10+heal)
	}
}

func TestHandleKeyUseInvisibilityPotionSetsInvisibleImmediately(t *testing.T) {
	m := itemsTestMap()
	itemJSON := invisibilityPotionAt(t, m, 7, 7)
	duration := durationOf(t, itemJSON)

	playerJSON := marshalPerson(t, testPerson{ID: "player:1", X: 7, Y: 7, HP: 10})
	state := newTestGameState(m, playerJSON, []MonsterState{deadMonster(t, "goblin")}, []json.RawMessage{itemJSON})

	if _, err := handleKey(state, ","); err != nil {
		t.Fatalf("handleKey(\",\"): %v", err)
	}
	msg, err := handleKey(state, "A")
	if err != nil {
		t.Fatalf("handleKey(\"A\"): %v", err)
	}
	wantMsg := "You drink the invisibility potion and turn invisible for " + strconv.Itoa(duration) + " turns."
	if msg != wantMsg {
		t.Errorf("got message %q, want %q", msg, wantMsg)
	}

	got := unmarshalPerson(t, state.PlayerState)
	if !got.Invisible {
		t.Error("expected Invisible to be true")
	}
	if len(got.ActiveEffects) != 1 || got.ActiveEffects[0].TurnsRemaining != duration-1 {
		t.Errorf("expected 1 active effect with TurnsRemaining %d (one tick already elapsed this turn), got %+v", duration-1, got.ActiveEffects)
	}
}

func TestHandleKeyPickupUseInvisibilityThenTickExpiresEndToEnd(t *testing.T) {
	m := itemsTestMap()
	itemJSON := invisibilityPotionAt(t, m, 7, 7)
	duration := durationOf(t, itemJSON)

	playerJSON := marshalPerson(t, testPerson{ID: "player:1", X: 7, Y: 7, HP: 10})
	state := newTestGameState(m, playerJSON, []MonsterState{deadMonster(t, "goblin")}, []json.RawMessage{itemJSON})

	if _, err := handleKey(state, ","); err != nil {
		t.Fatalf("handleKey(\",\"): %v", err)
	}
	if _, err := handleKey(state, "A"); err != nil {
		t.Fatalf("handleKey(\"A\"): %v", err)
	}

	// One tick already elapsed on the turn the potion was drunk, so
	// duration-1 further waits keep it active; the next one expires it.
	for i := 0; i < duration-2; i++ {
		if _, err := handleKey(state, "."); err != nil {
			t.Fatalf("handleKey(\".\") wait %d: %v", i, err)
		}
		got := unmarshalPerson(t, state.PlayerState)
		if !got.Invisible {
			t.Fatalf("wait %d: expected Invisible to still be true", i)
		}
	}

	msg, err := handleKey(state, ".")
	if err != nil {
		t.Fatalf("handleKey(\".\") final wait: %v", err)
	}
	if msg != "Your invisibility wears off." {
		t.Errorf("got message %q, want %q", msg, "Your invisibility wears off.")
	}
	got := unmarshalPerson(t, state.PlayerState)
	if got.Invisible {
		t.Error("expected Invisible to be false after expiry")
	}
	if len(got.ActiveEffects) != 0 {
		t.Errorf("expected ActiveEffects empty after expiry, got %d", len(got.ActiveEffects))
	}
}

func TestEquipItemEquipsWeaponFromInventoryAndRemovesItFromInventory(t *testing.T) {
	m := itemsTestMap()
	itemJSON := weaponAt(t, m, 0, 0) // position irrelevant once carried

	playerJSON := marshalPerson(t, testPerson{ID: "player:1", X: 7, Y: 7, HP: 15, Inventory: []json.RawMessage{itemJSON}})
	state := newTestGameState(m, playerJSON, []MonsterState{deadMonster(t, "goblin")}, nil)

	msg, done, err := equipItem(state)
	if err != nil {
		t.Fatalf("equipItem: %v", err)
	}
	if !done {
		t.Fatal("expected done=true with a single candidate")
	}
	if msg != "You wield the fine dagger." {
		t.Errorf("got message %q, want %q", msg, "You wield the fine dagger.")
	}
	got := unmarshalPerson(t, state.PlayerState)
	if len(got.Inventory) != 0 {
		t.Errorf("expected inventory empty after wielding, got %d", len(got.Inventory))
	}
	if got.Wielded == nil || got.Wielded.Stats.Name != "fine dagger" {
		t.Fatalf("expected fine dagger wielded, got %+v", got.Wielded)
	}
}

func TestEquipItemReportsNothingToEquipWhenInventoryHasNothingEquippable(t *testing.T) {
	m := itemsTestMap()
	itemJSON := potionAt(t, m, 0, 0)

	playerJSON := marshalPerson(t, testPerson{ID: "player:1", X: 7, Y: 7, HP: 15, Inventory: []json.RawMessage{itemJSON}})
	state := newTestGameState(m, playerJSON, []MonsterState{deadMonster(t, "goblin")}, nil)

	msg, done, err := equipItem(state)
	if err != nil {
		t.Fatalf("equipItem: %v", err)
	}
	if !done {
		t.Fatal("expected done=true with zero candidates")
	}
	if msg != "You have nothing to equip." {
		t.Errorf("got message %q, want %q", msg, "You have nothing to equip.")
	}
}

func TestHandleKeyPickupThenWieldEndToEnd(t *testing.T) {
	m := itemsTestMap()
	itemJSON := weaponAt(t, m, 7, 7)

	playerJSON := marshalPerson(t, testPerson{ID: "player:1", X: 7, Y: 7, HP: 15})
	state := newTestGameState(m, playerJSON, []MonsterState{deadMonster(t, "goblin")}, []json.RawMessage{itemJSON})

	if _, err := handleKey(state, ","); err != nil {
		t.Fatalf("handleKey(\",\"): %v", err)
	}
	msg, err := handleKey(state, "E")
	if err != nil {
		t.Fatalf("handleKey(\"E\"): %v", err)
	}
	if msg != "You wield the fine dagger." {
		t.Errorf("got message %q, want %q", msg, "You wield the fine dagger.")
	}
	got := unmarshalPerson(t, state.PlayerState)
	if got.Wielded == nil {
		t.Fatal("expected a wielded weapon after pickup+wield")
	}
}

func TestUseItemSkipsPersistentWeaponAndUsesFirstOneShotItem(t *testing.T) {
	m := itemsTestMap()
	weaponJSON := weaponAt(t, m, 0, 0)
	potionJSON := potionAt(t, m, 0, 0)
	heal := healAmountOf(t, potionJSON)

	playerJSON := marshalPerson(t, testPerson{
		ID: "player:1", X: 7, Y: 7, HP: 10, MaxHP: 100,
		Inventory: []json.RawMessage{weaponJSON, potionJSON},
	})
	state := newTestGameState(m, playerJSON, []MonsterState{deadMonster(t, "goblin")}, nil)

	msg, done, err := useItem(state)
	if err != nil {
		t.Fatalf("useItem: %v", err)
	}
	if !done {
		t.Fatal("expected done=true with a single one-shot candidate")
	}
	wantMsg := "You drink the healing potion and recover " + strconv.Itoa(heal) + " HP (" + strconv.Itoa(10+heal) + " HP now)."
	if msg != wantMsg {
		t.Errorf("got message %q, want %q", msg, wantMsg)
	}
	got := unmarshalPerson(t, state.PlayerState)
	if len(got.Inventory) != 1 {
		t.Fatalf("expected the weapon to remain in inventory (only the potion consumed), got %d entries", len(got.Inventory))
	}
	var remaining item.Item
	if err := json.Unmarshal(got.Inventory[0], &remaining); err != nil {
		t.Fatalf("unmarshal remaining item: %v", err)
	}
	if remaining.Category != item.CategoryWeapon {
		t.Errorf("expected the remaining item to be the weapon, got category %q", remaining.Category)
	}
}

func TestHandleKeyAttackDamageChangesAfterWieldingWeapon(t *testing.T) {
	m := itemsTestMap()
	weaponJSON := weaponAt(t, m, 7, 7)

	playerJSON := marshalPerson(t, testPerson{ID: "player:1", X: 7, Y: 7, HP: 100, MaxHP: 100})
	goblinJSON := marshalPerson(t, testPerson{ID: "goblin:1", X: 8, Y: 7, HP: 100, MaxHP: 100})
	state := newTestGameState(m, playerJSON, []MonsterState{monster("goblin", goblinJSON)}, []json.RawMessage{weaponJSON})

	if _, err := handleKey(state, ","); err != nil {
		t.Fatalf("handleKey(\",\"): %v", err)
	}
	if _, err := handleKey(state, "E"); err != nil {
		t.Fatalf("handleKey(\"E\"): %v", err)
	}

	// Attack the goblin repeatedly (moving Right re-attacks since it's
	// adjacent and occupied) and confirm every hit falls in the
	// enchanted+vs-goblin range (2-5: base 1-4, +1 enchant, +1 vs goblin),
	// never the old bare-handed 1-2.
	for i := 0; i < 20; i++ {
		before := unmarshalMonster(t, monsterStateOf(t, state, "goblin"))
		if _, err := handleKey(state, "Right"); err != nil {
			t.Fatalf("attack %d: handleKey: %v", i, err)
		}
		after := unmarshalMonster(t, monsterStateOf(t, state, "goblin"))
		damage := before.HP - after.HP
		if damage < 2 || damage > 5 {
			t.Fatalf("attack %d: damage %d out of expected [2,5] range", i, damage)
		}
	}
}

func TestInitGoblinArmedProducesGoblinWithWieldedWeapon(t *testing.T) {
	m := itemsTestMap()
	out, err := initGoblinArmed(m, 0)
	if err != nil {
		t.Fatalf("initGoblinArmed: %v", err)
	}
	// Unlike the Player's Wielded (a {item, stats} wrapper, since it needs
	// to remember the raw item to swap back into Inventory later), the
	// Goblin's Wielded is a bare item.WeaponStats — it has no Inventory to
	// swap anything back into (see goblin.monster.Wielded).
	var got struct {
		Wielded *item.WeaponStats `json:"wielded,omitempty"`
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Wielded == nil {
		t.Fatal("expected initGoblinArmed to produce a goblin with a wielded weapon")
	}
	if got.Wielded.Name != "rusty sword" {
		t.Errorf("expected wielded weapon %q, got %q", "rusty sword", got.Wielded.Name)
	}
}

func TestPickUpReportsInventoryFullAndLeavesItemOnFloor(t *testing.T) {
	m := itemsTestMap()
	full := make([]json.RawMessage, item.InventoryCapacity)
	for i := range full {
		full[i] = potionAt(t, m, 0, 0)
	}
	playerJSON := marshalPerson(t, testPerson{ID: "player:1", X: 7, Y: 7, HP: 15, Inventory: full})
	floorItem := potionAt(t, m, 7, 7)
	state := newTestGameState(m, playerJSON, []MonsterState{deadMonster(t, "goblin")}, []json.RawMessage{floorItem})

	playerObj, err := playerObject(state.PlayerState)
	if err != nil {
		t.Fatalf("playerObject: %v", err)
	}
	msg, done, err := pickUp(state, playerObj)
	if err != nil {
		t.Fatalf("pickUp: %v", err)
	}
	if !done {
		t.Fatal("expected done=true with a single item at the tile")
	}
	if msg != "Your inventory is full." {
		t.Errorf("got message %q, want %q", msg, "Your inventory is full.")
	}
	if len(state.FloorItems) != 1 {
		t.Errorf("expected the item to remain on the floor, got %d FloorItems", len(state.FloorItems))
	}
	got := unmarshalPerson(t, state.PlayerState)
	if len(got.Inventory) != item.InventoryCapacity {
		t.Errorf("expected Inventory unchanged at capacity %d, got %d", item.InventoryCapacity, len(got.Inventory))
	}
}

func TestQuaffCandidatesFiltersToOneShotItems(t *testing.T) {
	m := itemsTestMap()
	cases := []struct {
		name      string
		inventory []json.RawMessage
		want      int
	}{
		{"none", nil, 0},
		{"one potion, one weapon", []json.RawMessage{potionAt(t, m, 0, 0), weaponAt(t, m, 0, 0)}, 1},
		{"two potions", []json.RawMessage{potionAt(t, m, 0, 0), invisibilityPotionAt(t, m, 0, 0)}, 2},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			playerJSON := marshalPerson(t, testPerson{ID: "player:1", X: 7, Y: 7, HP: 15, Inventory: c.inventory})
			state := newTestGameState(m, playerJSON, []MonsterState{deadMonster(t, "goblin")}, nil)
			got, err := quaffCandidates(state)
			if err != nil {
				t.Fatalf("quaffCandidates: %v", err)
			}
			if len(got) != c.want {
				t.Errorf("got %d candidates, want %d", len(got), c.want)
			}
		})
	}
}

func TestEquipCandidatesFiltersToEquippableCategories(t *testing.T) {
	m := itemsTestMap()
	cases := []struct {
		name      string
		inventory []json.RawMessage
		want      int
	}{
		{"none", nil, 0},
		{"one potion, one weapon", []json.RawMessage{potionAt(t, m, 0, 0), weaponAt(t, m, 0, 0)}, 1},
		{"two weapons", []json.RawMessage{weaponAt(t, m, 0, 0), weaponAt(t, m, 0, 0)}, 2},
		{"weapon and armor, one potion", []json.RawMessage{potionAt(t, m, 0, 0), weaponAt(t, m, 0, 0), armorAt(t, m, 0, 0)}, 2},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			playerJSON := marshalPerson(t, testPerson{ID: "player:1", X: 7, Y: 7, HP: 15, Inventory: c.inventory})
			state := newTestGameState(m, playerJSON, []MonsterState{deadMonster(t, "goblin")}, nil)
			got, err := equipCandidates(state)
			if err != nil {
				t.Fatalf("equipCandidates: %v", err)
			}
			if len(got) != c.want {
				t.Errorf("got %d candidates, want %d", len(got), c.want)
			}
		})
	}
}

func TestUseItemOpensPickerWithTwoOneShotCandidates(t *testing.T) {
	m := itemsTestMap()
	inv := []json.RawMessage{potionAt(t, m, 0, 0), invisibilityPotionAt(t, m, 0, 0)}
	playerJSON := marshalPerson(t, testPerson{ID: "player:1", X: 7, Y: 7, HP: 15, Inventory: inv})
	state := newTestGameState(m, playerJSON, []MonsterState{deadMonster(t, "goblin")}, nil)

	msg, done, err := useItem(state)
	if err != nil {
		t.Fatalf("useItem: %v", err)
	}
	if done {
		t.Fatal("expected done=false when a picker is opened")
	}
	if msg != "" {
		t.Errorf("expected empty message when opening a picker, got %q", msg)
	}
	if state.PendingSelection == nil {
		t.Fatal("expected PendingSelection to be set")
	}
	if state.PendingSelection.Action != pendingQuaff {
		t.Errorf("expected pendingQuaff, got %v", state.PendingSelection.Action)
	}
	if len(state.PendingSelection.Candidates) != 2 {
		t.Errorf("expected 2 candidates, got %d", len(state.PendingSelection.Candidates))
	}
	got := unmarshalPerson(t, state.PlayerState)
	if len(got.Inventory) != 2 {
		t.Errorf("expected Inventory untouched (2 items), got %d", len(got.Inventory))
	}
}

func TestEquipItemOpensPickerWithTwoWeaponCandidates(t *testing.T) {
	m := itemsTestMap()
	inv := []json.RawMessage{weaponAt(t, m, 0, 0), weaponAt(t, m, 0, 0)}
	playerJSON := marshalPerson(t, testPerson{ID: "player:1", X: 7, Y: 7, HP: 15, Inventory: inv})
	state := newTestGameState(m, playerJSON, []MonsterState{deadMonster(t, "goblin")}, nil)

	msg, done, err := equipItem(state)
	if err != nil {
		t.Fatalf("equipItem: %v", err)
	}
	if done {
		t.Fatal("expected done=false when a picker is opened")
	}
	if msg != "" {
		t.Errorf("expected empty message when opening a picker, got %q", msg)
	}
	if state.PendingSelection == nil {
		t.Fatal("expected PendingSelection to be set")
	}
	if state.PendingSelection.Action != pendingEquip {
		t.Errorf("expected pendingEquip, got %v", state.PendingSelection.Action)
	}
	if len(state.PendingSelection.Candidates) != 2 {
		t.Errorf("expected 2 candidates, got %d", len(state.PendingSelection.Candidates))
	}
}

func TestHandleKeyPendingSelectionValidLetterCompletesChosenItem(t *testing.T) {
	m := itemsTestMap()
	first := weaponAt(t, m, 0, 0)
	second := weaponAt(t, m, 0, 0)
	firstID, err := itemInfo(first)
	if err != nil {
		t.Fatalf("itemInfo: %v", err)
	}
	secondID, err := itemInfo(second)
	if err != nil {
		t.Fatalf("itemInfo: %v", err)
	}
	if firstID.ID == secondID.ID {
		t.Fatalf("expected distinct weapon IDs, got %q twice", firstID.ID)
	}

	playerJSON := marshalPerson(t, testPerson{ID: "player:1", X: 7, Y: 7, HP: 15, Inventory: []json.RawMessage{first, second}})
	state := newTestGameState(m, playerJSON, []MonsterState{deadMonster(t, "goblin")}, nil)

	if _, err := handleKey(state, "E"); err != nil {
		t.Fatalf("handleKey(\"E\"): %v", err)
	}
	if state.PendingSelection == nil {
		t.Fatal("expected a pending selection after \"E\" with 2 candidates")
	}

	msg, err := handleKey(state, "B") // second candidate
	if err != nil {
		t.Fatalf("handleKey(\"B\"): %v", err)
	}
	if msg != "You wield the fine dagger." {
		t.Errorf("got message %q, want %q", msg, "You wield the fine dagger.")
	}
	if state.PendingSelection != nil {
		t.Error("expected PendingSelection to be cleared after resolving")
	}
	got := unmarshalPerson(t, state.PlayerState)
	if got.Wielded == nil {
		t.Fatal("expected a wielded weapon")
	}
	if got.Wielded.Stats.Source != secondID.ID {
		t.Errorf("expected the SECOND weapon (%q) wielded, got %q", secondID.ID, got.Wielded.Stats.Source)
	}
	if len(got.Inventory) != 1 {
		t.Fatalf("expected 1 remaining inventory item, got %d", len(got.Inventory))
	}
	var remaining item.Item
	if err := json.Unmarshal(got.Inventory[0], &remaining); err != nil {
		t.Fatalf("unmarshal remaining item: %v", err)
	}
	if remaining.ID != firstID.ID {
		t.Errorf("expected the first weapon (%q) still in inventory, got %q", firstID.ID, remaining.ID)
	}
}

func TestHandleKeyPendingSelectionInvalidKeyCancelsWithoutConsumingTurn(t *testing.T) {
	m := itemsTestMap()
	inv := []json.RawMessage{weaponAt(t, m, 0, 0), weaponAt(t, m, 0, 0)}
	playerJSON := marshalPerson(t, testPerson{ID: "player:1", X: 7, Y: 7, HP: 15, Inventory: inv})
	state := newTestGameState(m, playerJSON, []MonsterState{deadMonster(t, "goblin")}, nil)

	if _, err := handleKey(state, "E"); err != nil {
		t.Fatalf("handleKey(\"E\"): %v", err)
	}
	if state.PendingSelection == nil {
		t.Fatal("expected a pending selection after \"E\" with 2 candidates")
	}

	msg, err := handleKey(state, "Z") // out of range
	if err != nil {
		t.Fatalf("handleKey(\"Z\"): %v", err)
	}
	if msg != "" {
		t.Errorf("expected empty message on cancel, got %q", msg)
	}
	if state.PendingSelection != nil {
		t.Error("expected PendingSelection to be cleared (canceled)")
	}
	got := unmarshalPerson(t, state.PlayerState)
	if got.Wielded != nil {
		t.Error("expected nothing wielded after a canceled selection")
	}
	if len(got.Inventory) != 2 {
		t.Errorf("expected Inventory untouched (2 items), got %d", len(got.Inventory))
	}
}

func TestHandleKeyPickupTwoPotionsTriggerPickerSelectQuaffCorrectOne(t *testing.T) {
	m := itemsTestMap()
	// Built directly into Inventory (not picked up off the floor): this
	// test is about the quaff picker, not the pickup picker, which has
	// its own dedicated tests — two items on the same floor tile would
	// otherwise trigger the pickup picker instead on the first ",".
	healJSON := potionAt(t, m, 0, 0)
	invisJSON := invisibilityPotionAt(t, m, 0, 0)
	duration := durationOf(t, invisJSON)

	playerJSON := marshalPerson(t, testPerson{ID: "player:1", X: 7, Y: 7, HP: 10, MaxHP: 100, Inventory: []json.RawMessage{healJSON, invisJSON}})
	state := newTestGameState(m, playerJSON, []MonsterState{deadMonster(t, "goblin")}, nil)

	// Candidate order matches Inventory order: healing potion first,
	// invisibility potion second (letter B).
	if _, err := handleKey(state, "A"); err != nil {
		t.Fatalf("handleKey(\"A\"): %v", err)
	}
	if state.PendingSelection == nil {
		t.Fatal("expected a pending selection after \"A\" with 2 candidates")
	}

	msg, err := handleKey(state, "B")
	if err != nil {
		t.Fatalf("handleKey(\"B\"): %v", err)
	}
	wantMsg := "You drink the invisibility potion and turn invisible for " + strconv.Itoa(duration) + " turns."
	if msg != wantMsg {
		t.Errorf("got message %q, want %q", msg, wantMsg)
	}
	got := unmarshalPerson(t, state.PlayerState)
	if !got.Invisible {
		t.Error("expected Invisible true after quaffing the invisibility potion")
	}
	if got.HP != 10 {
		t.Errorf("expected HP untouched at 10 (healing potion not drunk), got %d", got.HP)
	}
	if len(got.Inventory) != 1 {
		t.Fatalf("expected 1 remaining inventory item (the healing potion), got %d", len(got.Inventory))
	}
	var remaining item.Item
	if err := json.Unmarshal(got.Inventory[0], &remaining); err != nil {
		t.Fatalf("unmarshal remaining item: %v", err)
	}
	if remaining.Name != "healing potion" {
		t.Errorf("expected the healing potion still in inventory, got %q", remaining.Name)
	}
}

func TestHandleKeyPickupTwoWeaponsTriggerPickerSelectWieldCorrectOne(t *testing.T) {
	m := itemsTestMap()
	// Built directly into Inventory (not picked up off the floor): this
	// test is about the wield picker, not the pickup picker, which has
	// its own dedicated tests — two items on the same floor tile would
	// otherwise trigger the pickup picker instead on the first ",".
	first := weaponAt(t, m, 0, 0)
	firstInfo, err := itemInfo(first)
	if err != nil {
		t.Fatalf("itemInfo: %v", err)
	}
	second := weaponAt(t, m, 0, 0)
	secondInfo, err := itemInfo(second)
	if err != nil {
		t.Fatalf("itemInfo: %v", err)
	}

	playerJSON := marshalPerson(t, testPerson{ID: "player:1", X: 7, Y: 7, HP: 15, Inventory: []json.RawMessage{first, second}})
	state := newTestGameState(m, playerJSON, []MonsterState{deadMonster(t, "goblin")}, nil)

	if _, err := handleKey(state, "E"); err != nil {
		t.Fatalf("handleKey(\"E\"): %v", err)
	}
	if state.PendingSelection == nil {
		t.Fatal("expected a pending selection after \"E\" with 2 candidates")
	}

	if _, err := handleKey(state, "B"); err != nil {
		t.Fatalf("handleKey(\"B\"): %v", err)
	}
	got := unmarshalPerson(t, state.PlayerState)
	if got.Wielded == nil {
		t.Fatal("expected a wielded weapon")
	}
	if got.Wielded.Stats.Source != secondInfo.ID {
		t.Errorf("expected the SECOND weapon (%q) wielded, got %q", secondInfo.ID, got.Wielded.Stats.Source)
	}
	if len(got.Inventory) != 1 {
		t.Fatalf("expected 1 remaining inventory item, got %d", len(got.Inventory))
	}
	var remaining item.Item
	if err := json.Unmarshal(got.Inventory[0], &remaining); err != nil {
		t.Fatalf("unmarshal remaining item: %v", err)
	}
	if remaining.ID != firstInfo.ID {
		t.Errorf("expected the first weapon (%q) still in inventory, got %q", firstInfo.ID, remaining.ID)
	}
}

func TestInventoryLinesFormatsLetteredNames(t *testing.T) {
	m := itemsTestMap()

	playerJSON := marshalPerson(t, testPerson{ID: "player:1", X: 7, Y: 7, HP: 15})
	state := newTestGameState(m, playerJSON, []MonsterState{deadMonster(t, "goblin")}, nil)
	lines, err := inventoryLines(state)
	if err != nil {
		t.Fatalf("inventoryLines: %v", err)
	}
	if len(lines) != 1 || lines[0] != "You are carrying nothing." {
		t.Errorf("got %v, want empty-inventory message", lines)
	}

	inv := []json.RawMessage{weaponAt(t, m, 0, 0), potionAt(t, m, 0, 0)}
	playerJSON2 := marshalPerson(t, testPerson{ID: "player:1", X: 7, Y: 7, HP: 15, Inventory: inv})
	state2 := newTestGameState(m, playerJSON2, []MonsterState{deadMonster(t, "goblin")}, nil)
	lines2, err := inventoryLines(state2)
	if err != nil {
		t.Fatalf("inventoryLines: %v", err)
	}
	want := []string{"A) fine dagger", "B) healing potion"}
	if len(lines2) != len(want) || lines2[0] != want[0] || lines2[1] != want[1] {
		t.Errorf("got %v, want %v", lines2, want)
	}
}

// TestHelpLinesMentionsEveryCommand is a lightweight guard against
// helpLines drifting out of sync with handleKey's switch/player.Action's
// key cases/the SetOnTypedKey|SetOnTypedRune callbacks — those are the
// actual source of truth (helpLines is maintained by hand against them),
// so this only checks each documented command's key still appears
// somewhere in the output, not that the descriptions themselves stay
// accurate.
func TestHelpLinesMentionsEveryCommand(t *testing.T) {
	lines := helpLines()
	if len(lines) == 0 {
		t.Fatal("expected helpLines to return at least one line")
	}
	joined := strings.Join(lines, "\n")
	for _, key := range []string{"Arrow keys", ".", ",", "<", ">", "A", "E", "D", "T", "I", "L", "S", "?", "Escape", "Q"} {
		if !strings.Contains(joined, key) {
			t.Errorf("expected helpLines output to mention %q, got:\n%s", key, joined)
		}
	}
}

func TestCandidateLettersProducesUppercaseSequenceUpToCapacity(t *testing.T) {
	if got := candidateLetters(0); len(got) != 0 {
		t.Errorf("candidateLetters(0) = %v, want empty", got)
	}
	if got := candidateLetters(3); len(got) != 3 || got[0] != "A" || got[1] != "B" || got[2] != "C" {
		t.Errorf("candidateLetters(3) = %v, want [A B C]", got)
	}
	got := candidateLetters(item.InventoryCapacity)
	if len(got) != item.InventoryCapacity {
		t.Fatalf("candidateLetters(%d) has %d entries, want %d", item.InventoryCapacity, len(got), item.InventoryCapacity)
	}
	if got[len(got)-1] != "J" {
		t.Errorf("expected last letter %q, got %q", "J", got[len(got)-1])
	}
}

func TestInventoryLinesAnnotatesWieldedItem(t *testing.T) {
	m := itemsTestMap()
	weaponJSON := weaponAt(t, m, 7, 7)

	playerJSON := marshalPerson(t, testPerson{ID: "player:1", X: 7, Y: 7, HP: 15})
	state := newTestGameState(m, playerJSON, []MonsterState{deadMonster(t, "goblin")}, []json.RawMessage{weaponJSON})

	if _, err := handleKey(state, ","); err != nil {
		t.Fatalf("handleKey(\",\"): %v", err)
	}
	if _, err := handleKey(state, "E"); err != nil {
		t.Fatalf("handleKey(\"E\"): %v", err)
	}

	lines, err := inventoryLines(state)
	if err != nil {
		t.Fatalf("inventoryLines: %v", err)
	}
	want := []string{"A) fine dagger (wielded)"}
	if len(lines) != len(want) || lines[0] != want[0] {
		t.Errorf("got %v, want %v", lines, want)
	}
}

func TestCarriedItemsIncludesWieldedFirstThenInventory(t *testing.T) {
	m := itemsTestMap()

	// 0 items.
	playerJSON := marshalPerson(t, testPerson{ID: "player:1", X: 7, Y: 7, HP: 15})
	state := newTestGameState(m, playerJSON, []MonsterState{deadMonster(t, "goblin")}, nil)
	items, err := carriedItems(state)
	if err != nil {
		t.Fatalf("carriedItems: %v", err)
	}
	if len(items) != 0 {
		t.Errorf("expected 0 carried items, got %d", len(items))
	}

	// Wielded + Inventory, built directly (not via floor pickup — this
	// test is about carriedItems' ordering, not the pickup picker).
	weaponJSON := weaponAt(t, m, 0, 0)
	weaponInfo, err := itemInfo(weaponJSON)
	if err != nil {
		t.Fatalf("itemInfo: %v", err)
	}
	potionJSON := potionAt(t, m, 0, 0)
	playerJSON2 := marshalPerson(t, testPerson{
		ID: "player:1", X: 7, Y: 7, HP: 15,
		Wielded:   &testWielded{Item: weaponJSON, Stats: item.WeaponStats{Name: "fine dagger", Source: weaponInfo.ID, BaseDamageMax: 4, Enchantment: 1}},
		Inventory: []json.RawMessage{potionJSON},
	})
	state2 := newTestGameState(m, playerJSON2, []MonsterState{deadMonster(t, "goblin")}, nil)
	items2, err := carriedItems(state2)
	if err != nil {
		t.Fatalf("carriedItems: %v", err)
	}
	if len(items2) != 2 {
		t.Fatalf("expected 2 carried items, got %d", len(items2))
	}
	if !items2[0].IsWielded {
		t.Error("expected the wielded item first")
	}
	if items2[1].IsWielded {
		t.Error("expected the second (Inventory) item not marked wielded")
	}
}

func TestDropItemAlwaysOpensPickerEvenWithOneItem(t *testing.T) {
	m := itemsTestMap()
	playerJSON := marshalPerson(t, testPerson{ID: "player:1", X: 7, Y: 7, HP: 15, Inventory: []json.RawMessage{potionAt(t, m, 0, 0)}})
	state := newTestGameState(m, playerJSON, []MonsterState{deadMonster(t, "goblin")}, nil)

	msg, done, err := dropItem(state)
	if err != nil {
		t.Fatalf("dropItem: %v", err)
	}
	if done {
		t.Fatal("expected done=false even with a single carried item")
	}
	if msg != "" {
		t.Errorf("expected empty message when opening a picker, got %q", msg)
	}
	if state.PendingSelection == nil || state.PendingSelection.Action != pendingDrop {
		t.Fatalf("expected a pending drop selection, got %+v", state.PendingSelection)
	}
	if len(state.PendingSelection.Candidates) != 1 {
		t.Errorf("expected 1 candidate, got %d", len(state.PendingSelection.Candidates))
	}
}

func TestDropItemReportsNothingToDropWhenEmpty(t *testing.T) {
	playerJSON := marshalPerson(t, testPerson{ID: "player:1", X: 7, Y: 7, HP: 15})
	state := newTestGameState(itemsTestMap(), playerJSON, []MonsterState{deadMonster(t, "goblin")}, nil)

	msg, done, err := dropItem(state)
	if err != nil {
		t.Fatalf("dropItem: %v", err)
	}
	if !done {
		t.Fatal("expected done=true with nothing carried")
	}
	if msg != "You have nothing to drop." {
		t.Errorf("got message %q, want %q", msg, "You have nothing to drop.")
	}
}

func TestApplyDropPlacesItemAtPlayersCurrentTile(t *testing.T) {
	m := itemsTestMap()
	potionJSON := potionAt(t, m, 0, 0) // carried, position irrelevant until dropped
	playerJSON := marshalPerson(t, testPerson{ID: "player:1", X: 9, Y: 11, HP: 15, Inventory: []json.RawMessage{potionJSON}})
	state := newTestGameState(m, playerJSON, []MonsterState{deadMonster(t, "goblin")}, nil)

	it, err := itemInfo(potionJSON)
	if err != nil {
		t.Fatalf("itemInfo: %v", err)
	}
	msg, err := applyDrop(state, it, potionJSON)
	if err != nil {
		t.Fatalf("applyDrop: %v", err)
	}
	if msg != "You drop the healing potion." {
		t.Errorf("got message %q, want %q", msg, "You drop the healing potion.")
	}
	if len(state.FloorItems) != 1 {
		t.Fatalf("expected 1 floor item after drop, got %d", len(state.FloorItems))
	}
	dropped, err := itemInfo(state.FloorItems[0])
	if err != nil {
		t.Fatalf("itemInfo: %v", err)
	}
	if dropped.X != 9 || dropped.Y != 11 {
		t.Errorf("expected dropped item at (9,11), got (%d,%d)", dropped.X, dropped.Y)
	}
	got := unmarshalPerson(t, state.PlayerState)
	if len(got.Inventory) != 0 {
		t.Errorf("expected Inventory empty after drop, got %d", len(got.Inventory))
	}
}

func TestApplyDropOfWieldedItemClearsWieldedAndReportsBareHanded(t *testing.T) {
	m := itemsTestMap()
	weaponJSON := weaponAt(t, m, 7, 7)
	playerJSON := marshalPerson(t, testPerson{ID: "player:1", X: 7, Y: 7, HP: 15})
	state := newTestGameState(m, playerJSON, []MonsterState{deadMonster(t, "goblin")}, []json.RawMessage{weaponJSON})

	if _, err := handleKey(state, ","); err != nil {
		t.Fatalf("handleKey(\",\"): %v", err)
	}
	if _, err := handleKey(state, "E"); err != nil {
		t.Fatalf("handleKey(\"E\"): %v", err)
	}

	items, err := carriedItems(state)
	if err != nil {
		t.Fatalf("carriedItems: %v", err)
	}
	if len(items) != 1 || !items[0].IsWielded {
		t.Fatalf("expected 1 wielded carried item, got %+v", items)
	}
	it, err := itemInfo(items[0].Raw)
	if err != nil {
		t.Fatalf("itemInfo: %v", err)
	}

	msg, err := applyDrop(state, it, items[0].Raw)
	if err != nil {
		t.Fatalf("applyDrop: %v", err)
	}
	wantMsg := "You drop the fine dagger. You are now fighting bare-handed."
	if msg != wantMsg {
		t.Errorf("got message %q, want %q", msg, wantMsg)
	}
	got := unmarshalPerson(t, state.PlayerState)
	if got.Wielded != nil {
		t.Error("expected Wielded cleared after dropping it")
	}
	if len(state.FloorItems) != 1 {
		t.Fatalf("expected 1 floor item after drop, got %d", len(state.FloorItems))
	}
}

func TestHandleKeyPickupWieldThenDropUnwieldsAndRevertsToBareHandedDamage(t *testing.T) {
	m := itemsTestMap()
	weaponJSON := weaponAt(t, m, 7, 7)

	playerJSON := marshalPerson(t, testPerson{ID: "player:1", X: 7, Y: 7, HP: 100, MaxHP: 100})
	goblinJSON := marshalPerson(t, testPerson{ID: "goblin:1", X: 8, Y: 7, HP: 100, MaxHP: 100})
	state := newTestGameState(m, playerJSON, []MonsterState{monster("goblin", goblinJSON)}, []json.RawMessage{weaponJSON})

	if _, err := handleKey(state, ","); err != nil {
		t.Fatalf("handleKey(\",\"): %v", err)
	}
	if _, err := handleKey(state, "E"); err != nil {
		t.Fatalf("handleKey(\"E\"): %v", err)
	}

	// Confirm the weapon is actually in effect before dropping it.
	beforeDrop := unmarshalMonster(t, monsterStateOf(t, state, "goblin"))
	if _, err := handleKey(state, "Right"); err != nil {
		t.Fatalf("attack with weapon: %v", err)
	}
	afterWeaponAttack := unmarshalMonster(t, monsterStateOf(t, state, "goblin"))
	weaponDamage := beforeDrop.HP - afterWeaponAttack.HP
	if weaponDamage < 2 || weaponDamage > 5 {
		t.Fatalf("expected enchanted weapon damage in [2,5], got %d", weaponDamage)
	}

	// Drop it: D opens the picker (only one thing carried), then A
	// selects it.
	if _, err := handleKey(state, "D"); err != nil {
		t.Fatalf("handleKey(\"D\"): %v", err)
	}
	if state.PendingSelection == nil {
		t.Fatal("expected a pending drop selection")
	}
	msg, err := handleKey(state, "A")
	if err != nil {
		t.Fatalf("handleKey(\"A\") to select: %v", err)
	}
	// The Goblin is alive and adjacent, so it may act (and append its own
	// message after " | ") in the same finishTurn — only assert the
	// drop's own message leads.
	wantMsg := "You drop the fine dagger. You are now fighting bare-handed."
	if !strings.HasPrefix(msg, wantMsg) {
		t.Errorf("got message %q, want it to start with %q", msg, wantMsg)
	}
	got := unmarshalPerson(t, state.PlayerState)
	if got.Wielded != nil {
		t.Error("expected Wielded cleared after dropping it")
	}
	if len(state.FloorItems) != 1 {
		t.Fatalf("expected the dagger back on the floor, got %d FloorItems", len(state.FloorItems))
	}
	droppedInfo, err := itemInfo(state.FloorItems[0])
	if err != nil {
		t.Fatalf("itemInfo: %v", err)
	}
	if droppedInfo.X != got.X || droppedInfo.Y != got.Y {
		t.Errorf("expected dropped weapon at Player's tile (%d,%d), got (%d,%d)", got.X, got.Y, droppedInfo.X, droppedInfo.Y)
	}

	// Attack again: should now be back to bare-handed 1-2 damage.
	beforeBareAttack := unmarshalMonster(t, monsterStateOf(t, state, "goblin"))
	if _, err := handleKey(state, "Right"); err != nil {
		t.Fatalf("bare-handed attack: %v", err)
	}
	afterBareAttack := unmarshalMonster(t, monsterStateOf(t, state, "goblin"))
	bareDamage := beforeBareAttack.HP - afterBareAttack.HP
	if bareDamage < 1 || bareDamage > 2 {
		t.Errorf("expected bare-handed damage in [1,2] after dropping the weapon, got %d", bareDamage)
	}
}

func TestHandleKeyDropInventoryItemLeavesWieldedIntact(t *testing.T) {
	m := itemsTestMap()
	// Built directly into Wielded/Inventory (not picked up off the floor):
	// this test is about drop, not the pickup picker, which has its own
	// dedicated tests — two items on the same floor tile would otherwise
	// trigger the pickup picker instead on the first ",".
	weaponJSON := weaponAt(t, m, 0, 0)
	weaponInfo, err := itemInfo(weaponJSON)
	if err != nil {
		t.Fatalf("itemInfo: %v", err)
	}
	potionJSON := potionAt(t, m, 0, 0)

	playerJSON := marshalPerson(t, testPerson{
		ID: "player:1", X: 7, Y: 7, HP: 15,
		Wielded:   &testWielded{Item: weaponJSON, Stats: item.WeaponStats{Name: "fine dagger", Source: weaponInfo.ID, BaseDamageMax: 4, Enchantment: 1}},
		Inventory: []json.RawMessage{potionJSON},
	})
	state := newTestGameState(m, playerJSON, []MonsterState{deadMonster(t, "goblin")}, nil)

	// Carried items are now: A) wielded dagger, B) potion.
	if _, err := handleKey(state, "D"); err != nil {
		t.Fatalf("handleKey(\"D\"): %v", err)
	}
	if _, err := handleKey(state, "B"); err != nil {
		t.Fatalf("handleKey(\"B\") to select the potion: %v", err)
	}

	got := unmarshalPerson(t, state.PlayerState)
	if got.Wielded == nil {
		t.Error("expected the wielded weapon to remain wielded")
	}
	if len(got.Inventory) != 0 {
		t.Errorf("expected Inventory empty after dropping its only item, got %d", len(got.Inventory))
	}
	if len(state.FloorItems) != 1 {
		t.Fatalf("expected the dropped potion on the floor, got %d FloorItems", len(state.FloorItems))
	}
	droppedInfo, err := itemInfo(state.FloorItems[0])
	if err != nil {
		t.Fatalf("itemInfo: %v", err)
	}
	if droppedInfo.Category != item.CategoryPotion {
		t.Errorf("expected the dropped item to be the potion, got category %q", droppedInfo.Category)
	}
}

func TestFloorItemsAtFiltersByPosition(t *testing.T) {
	m := itemsTestMap()
	cases := []struct {
		name       string
		floorItems []json.RawMessage
		x, y       int
		want       int
	}{
		{"none", nil, 7, 7, 0},
		{"one at target, one elsewhere", []json.RawMessage{potionAt(t, m, 7, 7), potionAt(t, m, 9, 9)}, 7, 7, 1},
		{"two at target", []json.RawMessage{potionAt(t, m, 7, 7), weaponAt(t, m, 7, 7)}, 7, 7, 2},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			playerJSON := marshalPerson(t, testPerson{ID: "player:1", X: 7, Y: 7, HP: 15})
			state := newTestGameState(m, playerJSON, []MonsterState{deadMonster(t, "goblin")}, c.floorItems)
			got, err := floorItemsAt(state, c.x, c.y)
			if err != nil {
				t.Fatalf("floorItemsAt: %v", err)
			}
			if len(got) != c.want {
				t.Errorf("got %d items, want %d", len(got), c.want)
			}
		})
	}
}

func TestPickUpOpensPickerWithTwoItemsAtSameTile(t *testing.T) {
	m := itemsTestMap()
	first := potionAt(t, m, 7, 7)
	second := invisibilityPotionAt(t, m, 7, 7)

	playerJSON := marshalPerson(t, testPerson{ID: "player:1", X: 7, Y: 7, HP: 15})
	state := newTestGameState(m, playerJSON, []MonsterState{deadMonster(t, "goblin")}, []json.RawMessage{first, second})

	playerObj, err := playerObject(state.PlayerState)
	if err != nil {
		t.Fatalf("playerObject: %v", err)
	}
	msg, done, err := pickUp(state, playerObj)
	if err != nil {
		t.Fatalf("pickUp: %v", err)
	}
	if done {
		t.Fatal("expected done=false when a picker is opened")
	}
	if msg != "" {
		t.Errorf("expected empty message when opening a picker, got %q", msg)
	}
	if state.PendingSelection == nil || state.PendingSelection.Action != pendingPickup {
		t.Fatalf("expected a pending pickup selection, got %+v", state.PendingSelection)
	}
	if len(state.PendingSelection.Candidates) != 2 {
		t.Errorf("expected 2 candidates, got %d", len(state.PendingSelection.Candidates))
	}
	if len(state.FloorItems) != 2 {
		t.Errorf("expected FloorItems untouched while the picker is open, got %d", len(state.FloorItems))
	}
}

func TestHandleKeyPendingPickupValidLetterPicksUpChosenItem(t *testing.T) {
	m := itemsTestMap()
	first := potionAt(t, m, 7, 7)
	second := invisibilityPotionAt(t, m, 7, 7)
	firstInfo, err := itemInfo(first)
	if err != nil {
		t.Fatalf("itemInfo: %v", err)
	}
	secondInfo, err := itemInfo(second)
	if err != nil {
		t.Fatalf("itemInfo: %v", err)
	}

	playerJSON := marshalPerson(t, testPerson{ID: "player:1", X: 7, Y: 7, HP: 15})
	state := newTestGameState(m, playerJSON, []MonsterState{deadMonster(t, "goblin")}, []json.RawMessage{first, second})

	if _, err := handleKey(state, ","); err != nil {
		t.Fatalf("handleKey(\",\"): %v", err)
	}
	if state.PendingSelection == nil {
		t.Fatal("expected a pending pickup selection")
	}

	msg, err := handleKey(state, "B") // second item
	if err != nil {
		t.Fatalf("handleKey(\"B\"): %v", err)
	}
	if msg != "You pick up the invisibility potion." {
		t.Errorf("got message %q, want %q", msg, "You pick up the invisibility potion.")
	}
	if state.PendingSelection != nil {
		t.Error("expected PendingSelection to be cleared after resolving")
	}
	got := unmarshalPerson(t, state.PlayerState)
	if len(got.Inventory) != 1 {
		t.Fatalf("expected 1 inventory item, got %d", len(got.Inventory))
	}
	var picked item.Item
	if err := json.Unmarshal(got.Inventory[0], &picked); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if picked.ID != secondInfo.ID {
		t.Errorf("expected the SECOND item (%q) picked up, got %q", secondInfo.ID, picked.ID)
	}
	if len(state.FloorItems) != 1 {
		t.Fatalf("expected 1 item remaining on the floor, got %d", len(state.FloorItems))
	}
	remainingInfo, err := itemInfo(state.FloorItems[0])
	if err != nil {
		t.Fatalf("itemInfo: %v", err)
	}
	if remainingInfo.ID != firstInfo.ID {
		t.Errorf("expected the first item (%q) still on the floor, got %q", firstInfo.ID, remainingInfo.ID)
	}
}

func TestApplyPickupReportsInventoryFullAndLeavesChosenItemOnFloor(t *testing.T) {
	m := itemsTestMap()
	full := make([]json.RawMessage, item.InventoryCapacity)
	for i := range full {
		full[i] = potionAt(t, m, 0, 0)
	}
	first := potionAt(t, m, 7, 7)
	second := invisibilityPotionAt(t, m, 7, 7)

	playerJSON := marshalPerson(t, testPerson{ID: "player:1", X: 7, Y: 7, HP: 15, Inventory: full})
	state := newTestGameState(m, playerJSON, []MonsterState{deadMonster(t, "goblin")}, []json.RawMessage{first, second})

	if _, err := handleKey(state, ","); err != nil {
		t.Fatalf("handleKey(\",\"): %v", err)
	}
	if state.PendingSelection == nil {
		t.Fatal("expected a pending pickup selection")
	}

	msg, err := handleKey(state, "B")
	if err != nil {
		t.Fatalf("handleKey(\"B\"): %v", err)
	}
	if msg != "Your inventory is full." {
		t.Errorf("got message %q, want %q", msg, "Your inventory is full.")
	}
	got := unmarshalPerson(t, state.PlayerState)
	if len(got.Inventory) != item.InventoryCapacity {
		t.Errorf("expected Inventory unchanged at capacity %d, got %d", item.InventoryCapacity, len(got.Inventory))
	}
	if len(state.FloorItems) != 2 {
		t.Errorf("expected both floor items to remain, got %d", len(state.FloorItems))
	}
}

func TestLookHereListsAllItemsAtPlayersTile(t *testing.T) {
	m := itemsTestMap()

	// Empty.
	playerJSON := marshalPerson(t, testPerson{ID: "player:1", X: 7, Y: 7, HP: 15})
	state := newTestGameState(m, playerJSON, []MonsterState{deadMonster(t, "goblin")}, nil)
	lines, err := lookHereLines(state)
	if err != nil {
		t.Fatalf("lookHereLines: %v", err)
	}
	if len(lines) != 1 || lines[0] != "There is nothing here." {
		t.Errorf("got %v, want empty-tile message", lines)
	}

	// One item.
	oneItemJSON := potionAt(t, m, 7, 7)
	state2 := newTestGameState(m, playerJSON, []MonsterState{deadMonster(t, "goblin")}, []json.RawMessage{oneItemJSON})
	lines2, err := lookHereLines(state2)
	if err != nil {
		t.Fatalf("lookHereLines: %v", err)
	}
	if len(lines2) != 1 || lines2[0] != "A) healing potion" {
		t.Errorf("got %v, want [\"A) healing potion\"]", lines2)
	}

	// Two items — lettering must match what the pickup picker would show.
	first := potionAt(t, m, 7, 7)
	second := weaponAt(t, m, 7, 7)
	state3 := newTestGameState(m, playerJSON, []MonsterState{deadMonster(t, "goblin")}, []json.RawMessage{first, second})
	lines3, err := lookHereLines(state3)
	if err != nil {
		t.Fatalf("lookHereLines: %v", err)
	}
	want := []string{"A) healing potion", "B) fine dagger"}
	if len(lines3) != len(want) || lines3[0] != want[0] || lines3[1] != want[1] {
		t.Errorf("got %v, want %v", lines3, want)
	}
}

func TestApplyDropOntoOccupiedCellLeavesBothItemsPresent(t *testing.T) {
	m := itemsTestMap()
	existingJSON := potionAt(t, m, 7, 7)
	droppedJSON := potionAt(t, m, 0, 0) // carried, relocated to (7,7) on drop

	playerJSON := marshalPerson(t, testPerson{ID: "player:1", X: 7, Y: 7, HP: 15, Inventory: []json.RawMessage{droppedJSON}})
	state := newTestGameState(m, playerJSON, []MonsterState{deadMonster(t, "goblin")}, []json.RawMessage{existingJSON})

	it, err := itemInfo(droppedJSON)
	if err != nil {
		t.Fatalf("itemInfo: %v", err)
	}
	if _, err := applyDrop(state, it, droppedJSON); err != nil {
		t.Fatalf("applyDrop: %v", err)
	}

	if len(state.FloorItems) != 2 {
		t.Fatalf("expected both items present on the tile, got %d", len(state.FloorItems))
	}
	atTile, err := floorItemsAt(state, 7, 7)
	if err != nil {
		t.Fatalf("floorItemsAt: %v", err)
	}
	if len(atTile) != 2 {
		t.Errorf("expected 2 items at (7,7), got %d", len(atTile))
	}
}
