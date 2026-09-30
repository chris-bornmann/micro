package mummy

import (
	"encoding/json"
	"regexp"
	"testing"

	"map_api/action"
	"map_api/attributes"
	"map_api/mapdata"
	"map_api/overlay"
	"map_api/world"
)

// testMap: one 10x10 room (interior x:5-14, y:5-14) big enough for chase
// tests, connected by a corridor to a separate small room (interior
// x:30-34, y:5-9) used for the "different room" case — mirrors
// goblin_test.go's own testMap exactly.
func testMap() mapdata.Map {
	roomA := mapdata.Room{ID: 0, X: 5, Y: 5, W: 10, H: 10}
	roomB := mapdata.Room{ID: 1, X: 30, Y: 5, W: 5, H: 5}
	corridor := mapdata.Corridor{
		ID: 0, RoomA: 0, RoomB: 1,
		DoorA: mapdata.Door{X: 15, Y: 9},
		DoorB: mapdata.Door{X: 29, Y: 7},
		Path:  corridorPath(),
	}
	return mapdata.Map{
		Width: 40, Height: 20,
		Rooms:     []mapdata.Room{roomA, roomB},
		Corridors: []mapdata.Corridor{corridor},
	}
}

func corridorPath() []mapdata.Point {
	var path []mapdata.Point
	for x := 15; x <= 29; x++ {
		path = append(path, mapdata.Point{X: x, Y: 9})
	}
	for y := 8; y >= 7; y-- {
		path = append(path, mapdata.Point{X: 29, Y: y})
	}
	return path
}

func selfJSON(t *testing.T, id string, x, y int, aware bool) []byte {
	t.Helper()
	data, err := json.Marshal(creature{
		Object: overlay.Object{ID: id, X: x, Y: y, Glyph: 'm', Color: overlay.Color{R: 200, G: 190, B: 150}, Layer: 1},
		Aware:  aware,
	})
	if err != nil {
		t.Fatalf("marshal self: %v", err)
	}
	return data
}

func selfJSONWithHP(t *testing.T, id string, x, y, hp int) []byte {
	t.Helper()
	data, err := json.Marshal(creature{
		Object: overlay.Object{ID: id, X: x, Y: y, Glyph: 'm', Color: overlay.Color{R: 200, G: 190, B: 150}, Layer: 1},
		HP:     hp,
	})
	if err != nil {
		t.Fatalf("marshal self: %v", err)
	}
	return data
}

func selfJSONFrom(t *testing.T, c creature) []byte {
	t.Helper()
	data, err := json.Marshal(c)
	if err != nil {
		t.Fatalf("marshal self: %v", err)
	}
	return data
}

func augmentedJSON(t *testing.T, m mapdata.Map, objects ...overlay.Object) []byte {
	t.Helper()
	data, err := json.Marshal(world.AugmentedMap{Map: m, Objects: objects})
	if err != nil {
		t.Fatalf("marshal augmented map: %v", err)
	}
	return data
}

// actionResponse mirrors Action's {"self": ..., "result": ...} envelope.
type actionResponse struct {
	Self   creature      `json:"self"`
	Result action.Result `json:"result"`
}

func actionResult(t *testing.T, out []byte) actionResponse {
	t.Helper()
	var got actionResponse
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("unmarshal Action output: %v", err)
	}
	return got
}

func initResult(t *testing.T, out []byte) creature {
	t.Helper()
	var got creature
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("unmarshal Init output: %v", err)
	}
	return got
}

// defendResult mirrors Defend's {"self": ..., "outcome": ...} envelope.
type defendResultResponse struct {
	Self    creature             `json:"self"`
	Outcome action.DefendOutcome `json:"outcome"`
}

func defendResult(t *testing.T, out []byte) defendResultResponse {
	t.Helper()
	var got defendResultResponse
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("unmarshal Defend output: %v", err)
	}
	return got
}

func TestInitPlacesMummyInARoom(t *testing.T) {
	m := testMap()
	mapJSON, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal map: %v", err)
	}

	out, err := Init(mapJSON, 0)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	got := initResult(t, out)

	if ok, _ := regexp.MatchString(`^mummy:\d+$`, got.ID); !ok {
		t.Errorf("expected ID matching mummy:<N>, got %q", got.ID)
	}
	if got.Glyph != 'm' {
		t.Errorf("expected glyph 'm', got %q", got.Glyph)
	}
	if got.Aware {
		t.Error("expected a freshly spawned mummy to not be aware of the Player yet")
	}
	if got.HP < 20 || got.HP > 30 {
		t.Errorf("expected HP in [20,30], got %d", got.HP)
	}
	if got.MaxHP != got.HP {
		t.Errorf("expected MaxHP to equal starting HP, got MaxHP=%d HP=%d", got.MaxHP, got.HP)
	}
	if got.Kind != "mummy" {
		t.Errorf("expected Kind %q, got %q", "mummy", got.Kind)
	}
	if got.Weight != 60 {
		t.Errorf("expected Weight 60, got %d", got.Weight)
	}
	if got.Level != 5 {
		t.Errorf("expected a mummy to be level 5, got %d", got.Level)
	}
	for name, v := range map[string]int{
		"Strength": got.Attributes.Strength, "Intelligence": got.Attributes.Intelligence,
		"Dexterity": got.Attributes.Dexterity, "Stamina": got.Attributes.Stamina,
	} {
		if v < 4 || v > 10 {
			t.Errorf("expected %s in [4,10], got %d", name, v)
		}
	}

	inARoom := false
	for _, r := range m.Rooms {
		if got.X >= r.X && got.X < r.X+r.W && got.Y >= r.Y && got.Y < r.Y+r.H {
			inARoom = true
			break
		}
	}
	if !inARoom {
		t.Errorf("expected mummy at (%d,%d) to be inside some room", got.X, got.Y)
	}
}

func TestInitRejectsMapWithNoRooms(t *testing.T) {
	empty, err := json.Marshal(mapdata.Map{Width: 10, Height: 10})
	if err != nil {
		t.Fatalf("marshal empty map: %v", err)
	}
	if _, err := Init(empty, 0); err == nil {
		t.Error("expected an error for a map with no rooms, got nil")
	}
}

func TestInitIsDeterministicForTheSameMapSeed(t *testing.T) {
	m := testMap()
	m.Seed = 123
	mapJSON, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal map: %v", err)
	}

	out1, err := Init(mapJSON, 0)
	if err != nil {
		t.Fatalf("Init (1): %v", err)
	}
	out2, err := Init(mapJSON, 0)
	if err != nil {
		t.Fatalf("Init (2): %v", err)
	}
	c1, c2 := initResult(t, out1), initResult(t, out2)
	if c1.X != c2.X || c1.Y != c2.Y || c1.HP != c2.HP || c1.Attributes != c2.Attributes {
		t.Errorf("expected the same map seed to spawn identically, got %+v and %+v", c1, c2)
	}
}

func TestActionStaysPutAndUnawareWhenPlayerElsewhere(t *testing.T) {
	m := testMap()
	p := overlay.Object{ID: "player:1", X: 32, Y: 7} // room B, mummy starts in room A

	out, err := Action(selfJSON(t, "mummy:1", 6, 6, false), augmentedJSON(t, m, p))
	if err != nil {
		t.Fatalf("Action: %v", err)
	}
	got := actionResult(t, out)
	if got.Self.Aware {
		t.Error("expected the mummy to remain unaware when the Player is in a different room")
	}
	if got.Self.X != 6 || got.Self.Y != 6 {
		t.Errorf("expected the mummy to stay put, got (%d,%d)", got.Self.X, got.Self.Y)
	}
}

func TestActionBecomesAwareAndMovesWhenPlayerEntersTheRoom(t *testing.T) {
	m := testMap()
	p := overlay.Object{ID: "player:1", X: 12, Y: 7} // same room as the mummy

	out, err := Action(selfJSON(t, "mummy:1", 6, 6, false), augmentedJSON(t, m, p))
	if err != nil {
		t.Fatalf("Action: %v", err)
	}
	got := actionResult(t, out)
	if !got.Self.Aware {
		t.Error("expected the mummy to become aware once the Player shares its room")
	}
	if got.Self.X != 7 || got.Self.Y != 6 {
		t.Errorf("expected the mummy to take its first chase step to (7,6), got (%d,%d)", got.Self.X, got.Self.Y)
	}
}

func TestActionAttacksWhenAdjacentToPlayer(t *testing.T) {
	m := testMap()
	self := selfJSON(t, "mummy:1", 6, 6, false)
	p := overlay.Object{ID: "player:1", X: 7, Y: 6}

	out, err := Action(self, augmentedJSON(t, m, p))
	if err != nil {
		t.Fatalf("Action: %v", err)
	}
	got := actionResult(t, out)
	if got.Self.X != 6 || got.Self.Y != 6 {
		t.Errorf("expected the mummy to stay put while attacking, got (%d,%d)", got.Self.X, got.Self.Y)
	}
	want := action.Result{Action: "attack", X: p.X, Y: p.Y, Weapon: "rotting touch", DamageMin: 4, DamageMax: 10}
	if got.Result != want {
		t.Errorf("got result %+v, want %+v", got.Result, want)
	}
}

func TestActionDoesNotBecomeAwareOfInvisiblePlayerEvenInSameRoom(t *testing.T) {
	m := testMap()
	p := overlay.Object{ID: "player:1", X: 9, Y: 9, Invisible: true}

	out, err := Action(selfJSON(t, "mummy:1", 6, 6, false), augmentedJSON(t, m, p))
	if err != nil {
		t.Fatalf("Action: %v", err)
	}
	got := actionResult(t, out)
	if got.Self.Aware {
		t.Error("expected the mummy to not become aware of an Invisible Player")
	}
}

func TestActionLosesAwarenessWhenAlreadyAwarePlayerTurnsInvisible(t *testing.T) {
	m := testMap()
	self := selfJSON(t, "mummy:1", 6, 6, true) // already aware
	p := overlay.Object{ID: "player:1", X: 9, Y: 9, Invisible: true}

	out, err := Action(self, augmentedJSON(t, m, p))
	if err != nil {
		t.Fatalf("Action: %v", err)
	}
	got := actionResult(t, out)
	if got.Self.Aware {
		t.Error("expected the mummy to lose awareness once the Player turns invisible")
	}
	if got.Self.X != 6 || got.Self.Y != 6 {
		t.Errorf("expected the mummy to stay put, got (%d,%d)", got.Self.X, got.Self.Y)
	}
}

func TestActionDoesNotBecomeAwareOfSneakingPlayerOnThreshold(t *testing.T) {
	m := testMap()
	p := overlay.Object{ID: "player:1", X: 15, Y: 9, Sneaking: true} // exactly on the door threshold

	out, err := Action(selfJSON(t, "mummy:1", 6, 6, false), augmentedJSON(t, m, p))
	if err != nil {
		t.Fatalf("Action: %v", err)
	}
	got := actionResult(t, out)
	if got.Self.Aware {
		t.Error("expected the mummy to not become aware of a Sneaking Player standing only on the threshold")
	}
}

func TestActionBecomesAwareOfNonSneakingPlayerOnThreshold(t *testing.T) {
	m := testMap()
	p := overlay.Object{ID: "player:1", X: 15, Y: 9} // same threshold, not sneaking

	out, err := Action(selfJSON(t, "mummy:1", 6, 6, false), augmentedJSON(t, m, p))
	if err != nil {
		t.Fatalf("Action: %v", err)
	}
	got := actionResult(t, out)
	if !got.Self.Aware {
		t.Error("expected the mummy to become aware of a non-Sneaking Player on the threshold")
	}
}

func TestDefendAppliesDamageWithinRange(t *testing.T) {
	self := selfJSONWithHP(t, "mummy:1", 6, 6, 30)
	attack, err := json.Marshal(action.Result{Action: "attack", Weapon: "hands", DamageMin: 1, DamageMax: 2})
	if err != nil {
		t.Fatalf("marshal attack: %v", err)
	}

	for i := 0; i < 50; i++ {
		out, err := Defend(self, attack)
		if err != nil {
			t.Fatalf("Defend: %v", err)
		}
		got := defendResult(t, out)
		if got.Outcome.Damage < 1 || got.Outcome.Damage > 2 {
			t.Fatalf("expected damage in [1,2], got %d", got.Outcome.Damage)
		}
		if got.Self.HP != 30-got.Outcome.Damage {
			t.Errorf("expected HP %d, got %d", 30-got.Outcome.Damage, got.Self.HP)
		}
	}
}

func TestDefendKillsAtOrBelowZeroHP(t *testing.T) {
	self := selfJSONWithHP(t, "mummy:1", 6, 6, 3)
	attack, err := json.Marshal(action.Result{Action: "attack", Weapon: "hands", DamageMin: 5, DamageMax: 5})
	if err != nil {
		t.Fatalf("marshal attack: %v", err)
	}

	out, err := Defend(self, attack)
	if err != nil {
		t.Fatalf("Defend: %v", err)
	}
	got := defendResult(t, out)
	if !got.Outcome.Dead || !got.Self.Dead {
		t.Errorf("expected mummy to be dead at negative HP, got outcome.Dead=%v self.Dead=%v", got.Outcome.Dead, got.Self.Dead)
	}
}

func TestDefendAddsFlatStrengthBonusFromAttacker(t *testing.T) {
	self := selfJSONWithHP(t, "mummy:1", 6, 6, 30)
	attack, err := json.Marshal(action.Result{Action: "attack", Weapon: "hands", DamageMin: 5, DamageMax: 5, Attributes: attributes.Attributes{Strength: 23}})
	if err != nil {
		t.Fatalf("marshal attack: %v", err)
	}

	out, err := Defend(self, attack)
	if err != nil {
		t.Fatalf("Defend: %v", err)
	}
	got := defendResult(t, out)
	if got.Outcome.Damage != 7 {
		t.Errorf("expected damage 7 (5 base + floor(23/10)=2 bonus), got %d", got.Outcome.Damage)
	}
}

func TestDefendAddsOilinessIndependentlyOfDamage(t *testing.T) {
	self := selfJSONWithHP(t, "mummy:1", 6, 6, 30)
	attack, err := json.Marshal(action.Result{Action: "attack", Weapon: "flask of oil", DamageMin: 1, DamageMax: 1, AddsOiliness: true})
	if err != nil {
		t.Fatalf("marshal attack: %v", err)
	}

	out, err := Defend(self, attack)
	if err != nil {
		t.Fatalf("Defend: %v", err)
	}
	got := defendResult(t, out)
	if got.Self.Oiliness != 1 {
		t.Errorf("expected Oiliness 1 after a single AddsOiliness hit, got %d", got.Self.Oiliness)
	}
}

func TestDefendIgnitesAnOilyTargetWithinExpectedDurationRangeAndDelaysDamage(t *testing.T) {
	self, err := json.Marshal(creature{
		Object: overlay.Object{ID: "mummy:1", X: 6, Y: 6, Glyph: 'm', Oiliness: 2},
		HP:     30, MaxHP: 30,
	})
	if err != nil {
		t.Fatalf("marshal self: %v", err)
	}
	attack, err := json.Marshal(action.Result{Action: "attack", Weapon: "torch", DamageMin: 1, DamageMax: 1, Ignites: true})
	if err != nil {
		t.Fatalf("marshal attack: %v", err)
	}

	out, err := Defend(self, attack)
	if err != nil {
		t.Fatalf("Defend: %v", err)
	}
	got := defendResult(t, out)
	if !got.Self.Burning {
		t.Error("expected Burning true after an ignite hit on an oily target")
	}
	if got.Self.BurningTurnsRemaining < 2 || got.Self.BurningTurnsRemaining > 6 {
		t.Errorf("expected BurningTurnsRemaining in [2,6] (Oiliness 2), got %d", got.Self.BurningTurnsRemaining)
	}
	if !got.Self.BurningStartsNextTurn {
		t.Error("expected BurningStartsNextTurn true after a fresh ignition")
	}
}

func TestDefendIgnitesDoesNothingWithoutOiliness(t *testing.T) {
	self := selfJSONWithHP(t, "mummy:1", 6, 6, 30) // Oiliness 0
	attack, err := json.Marshal(action.Result{Action: "attack", Weapon: "torch", DamageMin: 1, DamageMax: 1, Ignites: true})
	if err != nil {
		t.Fatalf("marshal attack: %v", err)
	}

	out, err := Defend(self, attack)
	if err != nil {
		t.Fatalf("Defend: %v", err)
	}
	got := defendResult(t, out)
	if got.Self.Burning {
		t.Error("expected no ignition on a dry (non-oily) target")
	}
}

func TestCorpsePreservesNonInventoryAttributesInSelf(t *testing.T) {
	self := creature{
		Object:     overlay.Object{ID: "mummy:9", X: 3, Y: 5, Glyph: 'm', Layer: 1, Kind: "mummy", Weight: 60, Dead: true},
		Aware:      true,
		HP:         -1,
		MaxHP:      25,
		Attributes: attributes.Attributes{Strength: 8, Intelligence: 5, Dexterity: 6, Stamina: 9},
	}
	selfBytes, err := json.Marshal(self)
	if err != nil {
		t.Fatalf("marshal self: %v", err)
	}

	out, err := Corpse(selfBytes)
	if err != nil {
		t.Fatalf("Corpse: %v", err)
	}
	var cr CorpseResult
	if err := json.Unmarshal(out, &cr); err != nil {
		t.Fatalf("unmarshal CorpseResult: %v", err)
	}
	var gotSelf creature
	if err := json.Unmarshal(cr.Self, &gotSelf); err != nil {
		t.Fatalf("unmarshal self: %v", err)
	}
	if gotSelf.ID != self.ID || gotSelf.X != self.X || gotSelf.Y != self.Y || gotSelf.Kind != self.Kind {
		t.Errorf("expected identity/position preserved, got %+v", gotSelf)
	}
	if gotSelf.Attributes != self.Attributes {
		t.Errorf("expected Attributes preserved, got %+v, want %+v", gotSelf.Attributes, self.Attributes)
	}
	if !gotSelf.Dead {
		t.Error("expected Dead to remain true")
	}
	if cr.Wielded != nil {
		t.Errorf("expected Wielded always nil for a mummy corpse, got %+v", cr.Wielded)
	}
}

func TestCorpseBuildsFloorItemAtDeathPosition(t *testing.T) {
	self := creature{
		Object: overlay.Object{ID: "mummy:7", X: 12, Y: 4, Glyph: 'm', Layer: 1, Kind: "mummy", Weight: 60, Dead: true},
		HP:     -3, MaxHP: 25,
	}
	selfBytes, err := json.Marshal(self)
	if err != nil {
		t.Fatalf("marshal self: %v", err)
	}

	out, err := Corpse(selfBytes)
	if err != nil {
		t.Fatalf("Corpse: %v", err)
	}
	var cr CorpseResult
	if err := json.Unmarshal(out, &cr); err != nil {
		t.Fatalf("unmarshal CorpseResult: %v", err)
	}
	var it corpseItem
	if err := json.Unmarshal(cr.Item, &it); err != nil {
		t.Fatalf("unmarshal corpse item: %v", err)
	}
	if it.X != 12 || it.Y != 4 {
		t.Errorf("expected corpse at (12,4), got (%d,%d)", it.X, it.Y)
	}
	if it.Name != "mummy corpse" {
		t.Errorf("expected Name %q, got %q", "mummy corpse", it.Name)
	}
	if it.Weight != 60 {
		t.Errorf("expected corpse Weight 60, got %d", it.Weight)
	}
}
