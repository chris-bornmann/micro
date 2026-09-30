package player

import (
	"encoding/json"
	"regexp"
	"testing"

	"micro/server/internal/action"
	"micro/server/internal/attributes"
	"micro/server/internal/item"
	"micro/server/internal/mapdata"
	"micro/server/internal/mapgen"
	"micro/server/internal/overlay"
	"micro/server/internal/world"
)

// testMap is a small, hand-built map (rather than a randomly generated one)
// so Action tests can assert exact before/after coordinates: one 5x5 room
// (interior x:5-9, y:5-9) with a corridor exiting its east wall at (10,7)
// and running to (12,7).
func testMap() mapdata.Map {
	room := mapdata.Room{ID: 0, X: 5, Y: 5, W: 5, H: 5}
	corridor := mapdata.Corridor{
		ID:    0,
		RoomA: 0,
		RoomB: 0,
		DoorA: mapdata.Door{X: 10, Y: 7},
		DoorB: mapdata.Door{X: 12, Y: 7},
		Path: []mapdata.Point{
			{X: 10, Y: 7}, {X: 11, Y: 7}, {X: 12, Y: 7},
		},
	}
	return mapdata.Map{
		Width: 20, Height: 20,
		Rooms:     []mapdata.Room{room},
		Corridors: []mapdata.Corridor{corridor},
	}
}

// bendingCorridorTestMap has a corridor that bends immediately after its
// door and runs alongside the room's wall for a couple of tiles before
// turning away — reproducing the generation shape where a corridor tile
// (not the door) ends up adjacent to a room's interior. Room interior is
// x:5-9, y:5-9; the door is at (10,7); the corridor then runs down along
// x=10 (parallel to the room's east wall) before turning right at y=10.
func bendingCorridorTestMap() mapdata.Map {
	room := mapdata.Room{ID: 0, X: 5, Y: 5, W: 5, H: 5}
	corridor := mapdata.Corridor{
		ID:    0,
		RoomA: 0,
		RoomB: 0,
		DoorA: mapdata.Door{X: 10, Y: 7},
		DoorB: mapdata.Door{X: 12, Y: 10},
		Path: []mapdata.Point{
			{X: 10, Y: 7}, {X: 10, Y: 8}, {X: 10, Y: 9}, {X: 10, Y: 10}, {X: 11, Y: 10}, {X: 12, Y: 10},
		},
	}
	return mapdata.Map{
		Width: 20, Height: 20,
		Rooms:     []mapdata.Room{room},
		Corridors: []mapdata.Corridor{corridor},
	}
}

// selfJSON marshals a person (this package's private self-state shape) for
// use as Action/Defend's selfJSON argument.
func selfJSON(t *testing.T, obj overlay.Object, hp int) []byte {
	t.Helper()
	data, err := json.Marshal(person{Object: obj, HP: hp})
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
// Self is decoded as a full person so tests can assert on HP too, not just
// the generic overlay.Object shape.
type actionResponse struct {
	Self   person        `json:"self"`
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

// defendResult mirrors Defend's {"self": ..., "outcome": ...} envelope.
type defendResultResponse struct {
	Self    person               `json:"self"`
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

func initResult(t *testing.T, out []byte) person {
	t.Helper()
	var got person
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("unmarshal Init output: %v", err)
	}
	return got
}

func TestInitPlacesPlayerInARoom(t *testing.T) {
	cfg := mapgen.DefaultConfig()
	cfg.Seed = 55
	cfg.NumRooms = 8
	m, err := mapgen.Generate(cfg)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	mapJSON, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal map: %v", err)
	}

	out, err := Init(mapJSON)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	got := initResult(t, out)

	if ok, _ := regexp.MatchString(`^player:\d+$`, got.ID); !ok {
		t.Errorf("expected ID matching player:<N>, got %q", got.ID)
	}
	if got.Glyph != '@' {
		t.Errorf("expected glyph '@', got %q", got.Glyph)
	}
	if got.Color != (overlay.Color{R: 255, G: 255, B: 255}) {
		t.Errorf("expected white color, got %+v", got.Color)
	}
	if got.HP < 10 || got.HP > 20 {
		t.Errorf("expected HP in [10,20], got %d", got.HP)
	}
	if got.MaxHP != got.HP {
		t.Errorf("expected MaxHP to equal starting HP, got MaxHP=%d HP=%d", got.MaxHP, got.HP)
	}
	if got.Weight != 100 {
		t.Errorf("expected Weight 100 (the baseline), got %d", got.Weight)
	}
	if got.Level != 1 {
		t.Errorf("expected the Player to start at level 1, got %d", got.Level)
	}
	for name, v := range map[string]int{
		"Strength": got.Attributes.Strength, "Intelligence": got.Attributes.Intelligence,
		"Dexterity": got.Attributes.Dexterity, "Stamina": got.Attributes.Stamina,
	} {
		if v < 6 || v > 20 {
			t.Errorf("expected %s in [6,20], got %d", name, v)
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
		t.Errorf("expected player at (%d,%d) to be inside some room", got.X, got.Y)
	}
}

func TestInitRejectsMapWithNoRooms(t *testing.T) {
	empty, err := json.Marshal(mapdata.Map{Width: 10, Height: 10})
	if err != nil {
		t.Fatalf("marshal empty map: %v", err)
	}
	if _, err := Init(empty); err == nil {
		t.Error("expected an error for a map with no rooms, got nil")
	}
}

func TestActionMovesInEachDirection(t *testing.T) {
	m := testMap()
	start := overlay.Object{ID: "player:1", X: 7, Y: 7, Glyph: '@'}

	cases := []struct {
		key   string
		wantX int
		wantY int
	}{
		{"Up", 7, 6},
		{"Down", 7, 8},
		{"Left", 6, 7},
		{"Right", 8, 7},
	}
	for _, c := range cases {
		out, err := Action(selfJSON(t, start, 15), augmentedJSON(t, m), c.key)
		if err != nil {
			t.Fatalf("Action(%q): %v", c.key, err)
		}
		got := actionResult(t, out)
		if got.Self.X != c.wantX || got.Self.Y != c.wantY {
			t.Errorf("Action(%q) from (7,7): got (%d,%d), want (%d,%d)", c.key, got.Self.X, got.Self.Y, c.wantX, c.wantY)
		}
		if got.Result.Action != "move" || got.Result.X != c.wantX || got.Result.Y != c.wantY {
			t.Errorf("Action(%q): expected move result at (%d,%d), got %+v", c.key, c.wantX, c.wantY, got.Result)
		}
		if got.Self.HP != 15 {
			t.Errorf("Action(%q): expected HP to be untouched by a move, got %d", c.key, got.Self.HP)
		}
	}
}

func TestActionSlidesOnOilyDestination(t *testing.T) {
	m := testMap()
	start := overlay.Object{ID: "player:1", X: 7, Y: 7, Glyph: '@'}
	// Moving Right from (7,7) targets (8,7), inside the room's open
	// interior (5-9,5-9) — every one of the 4 slide candidates (stop,
	// forward, and both perpendiculars) is legal and unobstructed.
	aug := world.AugmentedMap{
		Map:       m,
		OilyTiles: []mapdata.Point{{X: 8, Y: 7}},
	}
	augJSON, err := json.Marshal(aug)
	if err != nil {
		t.Fatalf("marshal augmented map: %v", err)
	}

	seen := map[mapdata.Point]bool{}
	for i := 0; i < 400; i++ {
		out, err := Action(selfJSON(t, start, 15), augJSON, "Right")
		if err != nil {
			t.Fatalf("Action: %v", err)
		}
		got := actionResult(t, out)
		p := mapdata.Point{X: got.Self.X, Y: got.Self.Y}
		switch p {
		case mapdata.Point{X: 8, Y: 7}, mapdata.Point{X: 9, Y: 7}, mapdata.Point{X: 8, Y: 6}, mapdata.Point{X: 8, Y: 8}:
		default:
			t.Fatalf("unexpected resulting position %+v", p)
		}
		if got.Result.X != got.Self.X || got.Result.Y != got.Self.Y {
			t.Errorf("expected Result position to match Self position, got Self=%+v Result=%+v", got.Self, got.Result)
		}
		seen[p] = true
	}
	if len(seen) < 2 {
		t.Errorf("expected the oily destination to sometimes divert the Player elsewhere across 400 attempts, only ever saw %+v", seen)
	}
}

func TestActionBlockedByWall(t *testing.T) {
	m := testMap()
	// (5,5) is the room's top-left interior corner; both Up and Left exit
	// the room into unmapped space with no corridor there.
	start := overlay.Object{ID: "player:1", X: 5, Y: 5, Glyph: '@'}

	for _, key := range []string{"Up", "Left"} {
		out, err := Action(selfJSON(t, start, 15), augmentedJSON(t, m), key)
		if err != nil {
			t.Fatalf("Action(%q): %v", key, err)
		}
		got := actionResult(t, out)
		if got.Self.X != start.X || got.Self.Y != start.Y {
			t.Errorf("Action(%q) should be blocked: got (%d,%d), want unchanged (%d,%d)", key, got.Self.X, got.Self.Y, start.X, start.Y)
		}
		if got.Result.Action != "move" || got.Result.X != start.X || got.Result.Y != start.Y {
			t.Errorf("Action(%q): expected no-op move result at (%d,%d), got %+v", key, start.X, start.Y, got.Result)
		}
	}
}

func TestActionThroughDoorIntoCorridor(t *testing.T) {
	m := testMap()
	// (9,7) is the room's rightmost interior cell; moving Right exits
	// through the door at (10,7), the corridor's first Path point.
	start := overlay.Object{ID: "player:1", X: 9, Y: 7, Glyph: '@'}

	out, err := Action(selfJSON(t, start, 15), augmentedJSON(t, m), "Right")
	if err != nil {
		t.Fatalf("Action: %v", err)
	}
	got := actionResult(t, out)
	if got.Self.X != 10 || got.Self.Y != 7 {
		t.Errorf("expected player to move through the door to (10,7), got (%d,%d)", got.Self.X, got.Self.Y)
	}
}

func TestActionIgnoresUnrecognizedKey(t *testing.T) {
	m := testMap()
	start := overlay.Object{ID: "player:1", X: 7, Y: 7, Glyph: '@'}

	out, err := Action(selfJSON(t, start, 15), augmentedJSON(t, m), "z")
	if err != nil {
		t.Fatalf("Action: %v", err)
	}
	got := actionResult(t, out)
	if got.Self.X != start.X || got.Self.Y != start.Y {
		t.Errorf("unrecognized key should not move the player: got (%d,%d), want (%d,%d)", got.Self.X, got.Self.Y, start.X, start.Y)
	}
}

func TestActionDotWaitsInPlace(t *testing.T) {
	m := testMap()
	start := overlay.Object{ID: "player:1", X: 7, Y: 7, Glyph: '@'}

	out, err := Action(selfJSON(t, start, 15), augmentedJSON(t, m), ".")
	if err != nil {
		t.Fatalf("Action: %v", err)
	}
	got := actionResult(t, out)
	if got.Self.X != start.X || got.Self.Y != start.Y {
		t.Errorf("\".\" should not move the player: got (%d,%d), want (%d,%d)", got.Self.X, got.Self.Y, start.X, start.Y)
	}
	if got.Result.Action != "move" || got.Result.X != start.X || got.Result.Y != start.Y {
		t.Errorf("\".\": expected no-op move result at (%d,%d), got %+v", start.X, start.Y, got.Result)
	}
}

func TestActionBlocksRoomToCorridorSideEntry(t *testing.T) {
	m := bendingCorridorTestMap()
	// (9,8) is inside the room; (10,8) is a corridor tile running parallel
	// to the room's wall, but it is not a door — stepping right must be
	// blocked even though (10,8) is itself a walkable corridor cell.
	start := overlay.Object{ID: "player:1", X: 9, Y: 8, Glyph: '@'}

	out, err := Action(selfJSON(t, start, 15), augmentedJSON(t, m), "Right")
	if err != nil {
		t.Fatalf("Action: %v", err)
	}
	got := actionResult(t, out)
	if got.Self.X != start.X || got.Self.Y != start.Y {
		t.Errorf("expected room-to-corridor side entry to be blocked: got (%d,%d), want unchanged (%d,%d)", got.Self.X, got.Self.Y, start.X, start.Y)
	}
}

func TestActionBlocksCorridorToRoomSideEntry(t *testing.T) {
	m := bendingCorridorTestMap()
	// The reverse of the above: standing in the corridor at (10,8),
	// stepping left into the room at (9,8) must also be blocked, since
	// (10,8) is not this room's door.
	start := overlay.Object{ID: "player:1", X: 10, Y: 8, Glyph: '@'}

	out, err := Action(selfJSON(t, start, 15), augmentedJSON(t, m), "Left")
	if err != nil {
		t.Fatalf("Action: %v", err)
	}
	got := actionResult(t, out)
	if got.Self.X != start.X || got.Self.Y != start.Y {
		t.Errorf("expected corridor-to-room side entry to be blocked: got (%d,%d), want unchanged (%d,%d)", got.Self.X, got.Self.Y, start.X, start.Y)
	}
}

func TestActionAllowsMovementThroughTheActualDoor(t *testing.T) {
	m := bendingCorridorTestMap()
	// (9,7) is inside the room, aligned with the real door at (10,7); this
	// crossing must still be allowed.
	start := overlay.Object{ID: "player:1", X: 9, Y: 7, Glyph: '@'}

	out, err := Action(selfJSON(t, start, 15), augmentedJSON(t, m), "Right")
	if err != nil {
		t.Fatalf("Action: %v", err)
	}
	got := actionResult(t, out)
	if got.Self.X != 10 || got.Self.Y != 7 {
		t.Errorf("expected movement through the real door to succeed, got (%d,%d)", got.Self.X, got.Self.Y)
	}

	// And the reverse: from the door back into the room.
	back, err := Action(selfJSON(t, got.Self.Object, got.Self.HP), augmentedJSON(t, m), "Left")
	if err != nil {
		t.Fatalf("Action: %v", err)
	}
	gotBack := actionResult(t, back)
	if gotBack.Self.X != 9 || gotBack.Self.Y != 7 {
		t.Errorf("expected movement back through the door into the room to succeed, got (%d,%d)", gotBack.Self.X, gotBack.Self.Y)
	}
}

func TestActionAllowsMovementAlongCorridor(t *testing.T) {
	m := bendingCorridorTestMap()
	// Moving between two corridor tiles (no room involved) is unrestricted.
	start := overlay.Object{ID: "player:1", X: 10, Y: 8, Glyph: '@'}

	out, err := Action(selfJSON(t, start, 15), augmentedJSON(t, m), "Down")
	if err != nil {
		t.Fatalf("Action: %v", err)
	}
	got := actionResult(t, out)
	if got.Self.X != 10 || got.Self.Y != 9 {
		t.Errorf("expected free movement along the corridor, got (%d,%d)", got.Self.X, got.Self.Y)
	}
}

func TestActionAttacksAnOccupiedButLegalDestination(t *testing.T) {
	m := testMap()
	start := overlay.Object{ID: "player:1", X: 7, Y: 7, Glyph: '@'}
	occupant := overlay.Object{ID: "goblin:1", X: 8, Y: 7, Glyph: 'g'}

	out, err := Action(selfJSON(t, start, 15), augmentedJSON(t, m, occupant), "Right")
	if err != nil {
		t.Fatalf("Action: %v", err)
	}
	got := actionResult(t, out)

	if got.Self.X != start.X || got.Self.Y != start.Y {
		t.Errorf("expected the player to stay put when attacking, got (%d,%d)", got.Self.X, got.Self.Y)
	}
	want := action.Result{Action: "attack", X: occupant.X, Y: occupant.Y, Weapon: "hands", DamageMin: 1, DamageMax: 2}
	if got.Result != want {
		t.Errorf("got result %+v, want %+v", got.Result, want)
	}
}

func TestActionAttackCarriesAttackersAttributesOnTheResult(t *testing.T) {
	m := testMap()
	occupant := overlay.Object{ID: "goblin:1", X: 8, Y: 7, Glyph: 'g'}
	attrs := attributes.Attributes{Strength: 23, Intelligence: 5, Dexterity: 7, Stamina: 9}
	self, err := json.Marshal(person{Object: overlay.Object{ID: "player:1", X: 7, Y: 7, Glyph: '@'}, HP: 15, Attributes: attrs})
	if err != nil {
		t.Fatalf("marshal self: %v", err)
	}

	out, err := Action(self, augmentedJSON(t, m, occupant), "Right")
	if err != nil {
		t.Fatalf("Action: %v", err)
	}
	got := actionResult(t, out)

	if got.Result.Attributes != attrs {
		t.Errorf("expected the attack result to carry the attacker's Attributes %+v, got %+v", attrs, got.Result.Attributes)
	}
}

func TestActionMovesNormallyWhenDestinationIsUnoccupied(t *testing.T) {
	m := testMap()
	start := overlay.Object{ID: "player:1", X: 7, Y: 7, Glyph: '@'}
	elsewhere := overlay.Object{ID: "goblin:1", X: 6, Y: 6, Glyph: 'g'}

	out, err := Action(selfJSON(t, start, 15), augmentedJSON(t, m, elsewhere), "Right")
	if err != nil {
		t.Fatalf("Action: %v", err)
	}
	got := actionResult(t, out)
	want := action.Result{Action: "move", X: 8, Y: 7}
	if got.Result != want {
		t.Errorf("got result %+v, want %+v", got.Result, want)
	}
}

func TestInitIsDeterministicForTheSameMapSeed(t *testing.T) {
	cfg := mapgen.DefaultConfig()
	cfg.Seed = 123
	cfg.NumRooms = 10
	m, err := mapgen.Generate(cfg)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	mapJSON, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal map: %v", err)
	}

	out1, err := Init(mapJSON)
	if err != nil {
		t.Fatalf("Init (1): %v", err)
	}
	out2, err := Init(mapJSON)
	if err != nil {
		t.Fatalf("Init (2): %v", err)
	}

	p1, p2 := initResult(t, out1), initResult(t, out2)
	if p1.X != p2.X || p1.Y != p2.Y {
		t.Errorf("expected the same map seed to always spawn the player at the same position, got (%d,%d) and (%d,%d)",
			p1.X, p1.Y, p2.X, p2.Y)
	}
	if p1.HP != p2.HP {
		t.Errorf("expected the same map seed to always roll the same starting HP, got %d and %d", p1.HP, p2.HP)
	}
	if p1.Attributes != p2.Attributes {
		t.Errorf("expected the same map seed to always roll the same attributes, got %+v and %+v", p1.Attributes, p2.Attributes)
	}
}

func TestInitRollsAttributesWithinRange(t *testing.T) {
	for seed := int64(0); seed < 30; seed++ {
		cfg := mapgen.DefaultConfig()
		cfg.Seed = seed
		cfg.NumRooms = 5
		m, err := mapgen.Generate(cfg)
		if err != nil {
			t.Fatalf("seed %d: Generate: %v", seed, err)
		}
		mapJSON, err := json.Marshal(m)
		if err != nil {
			t.Fatalf("seed %d: marshal map: %v", seed, err)
		}
		out, err := Init(mapJSON)
		if err != nil {
			t.Fatalf("seed %d: Init: %v", seed, err)
		}
		got := initResult(t, out).Attributes
		for name, v := range map[string]int{
			"Strength": got.Strength, "Intelligence": got.Intelligence,
			"Dexterity": got.Dexterity, "Stamina": got.Stamina,
		} {
			if v < 6 || v > 20 {
				t.Fatalf("seed %d: expected %s in [6,20], got %d", seed, name, v)
			}
		}
	}
}

func TestDefendAppliesDamageWithinRange(t *testing.T) {
	start := overlay.Object{ID: "player:1", X: 7, Y: 7, Glyph: '@'}
	attack, err := json.Marshal(action.Result{Action: "attack", Weapon: "hands", DamageMin: 1, DamageMax: 2})
	if err != nil {
		t.Fatalf("marshal attack: %v", err)
	}

	for i := 0; i < 50; i++ {
		out, err := Defend(selfJSON(t, start, 15), attack)
		if err != nil {
			t.Fatalf("Defend: %v", err)
		}
		got := defendResult(t, out)
		if got.Outcome.Damage < 1 || got.Outcome.Damage > 2 {
			t.Fatalf("expected damage in [1,2], got %d", got.Outcome.Damage)
		}
		if got.Self.HP != 15-got.Outcome.Damage {
			t.Errorf("expected HP %d, got %d", 15-got.Outcome.Damage, got.Self.HP)
		}
		if got.Outcome.RemainingHP != got.Self.HP {
			t.Errorf("expected outcome.RemainingHP to match self.HP, got %d vs %d", got.Outcome.RemainingHP, got.Self.HP)
		}
		if got.Outcome.Dead {
			t.Errorf("expected player to survive a 1-2 damage hit from 15 HP")
		}
		if got.Self.X != start.X || got.Self.Y != start.Y {
			t.Errorf("expected position to be untouched by Defend, got (%d,%d)", got.Self.X, got.Self.Y)
		}
	}
}

func TestDefendAddsFlatStrengthBonusFromAttacker(t *testing.T) {
	start := overlay.Object{ID: "player:1", X: 7, Y: 7, Glyph: '@'}

	cases := []struct {
		strength  int
		wantBonus int
	}{
		{strength: 0, wantBonus: 0},
		{strength: 9, wantBonus: 0},
		{strength: 10, wantBonus: 1},
		{strength: 23, wantBonus: 2},
	}
	for _, c := range cases {
		attack, err := json.Marshal(action.Result{
			Action: "attack", Weapon: "hands", DamageMin: 5, DamageMax: 5,
			Attributes: attributes.Attributes{Strength: c.strength},
		})
		if err != nil {
			t.Fatalf("marshal attack: %v", err)
		}

		out, err := Defend(selfJSON(t, start, 50), attack)
		if err != nil {
			t.Fatalf("Defend: %v", err)
		}
		got := defendResult(t, out)
		want := 5 + c.wantBonus
		if got.Outcome.Damage != want {
			t.Errorf("strength %d: expected damage %d (5 base + %d bonus), got %d", c.strength, want, c.wantBonus, got.Outcome.Damage)
		}
	}
}

func TestDefendKillsAtOrBelowZeroHP(t *testing.T) {
	start := overlay.Object{ID: "player:1", X: 7, Y: 7, Glyph: '@'}
	attack, err := json.Marshal(action.Result{Action: "attack", Weapon: "hands", DamageMin: 5, DamageMax: 5})
	if err != nil {
		t.Fatalf("marshal attack: %v", err)
	}

	out, err := Defend(selfJSON(t, start, 5), attack)
	if err != nil {
		t.Fatalf("Defend: %v", err)
	}
	got := defendResult(t, out)
	if got.Outcome.Damage != 5 {
		t.Errorf("expected damage 5, got %d", got.Outcome.Damage)
	}
	if got.Self.HP != 0 {
		t.Errorf("expected HP 0, got %d", got.Self.HP)
	}
	if !got.Outcome.Dead || !got.Self.Dead {
		t.Errorf("expected player to be dead at 0 HP, got outcome.Dead=%v self.Dead=%v", got.Outcome.Dead, got.Self.Dead)
	}
}

func TestDefendAddsOilinessIndependentlyOfDamage(t *testing.T) {
	start := overlay.Object{ID: "player:1", X: 7, Y: 7, Glyph: '@'}
	attack, err := json.Marshal(action.Result{Action: "attack", Weapon: "flask of oil", DamageMin: 1, DamageMax: 1, AddsOiliness: true})
	if err != nil {
		t.Fatalf("marshal attack: %v", err)
	}

	out, err := Defend(selfJSON(t, start, 15), attack)
	if err != nil {
		t.Fatalf("Defend: %v", err)
	}
	got := defendResult(t, out)
	if got.Self.Oiliness != 1 {
		t.Errorf("expected Oiliness 1 after a single AddsOiliness hit, got %d", got.Self.Oiliness)
	}
	if got.Outcome.Damage != 1 {
		t.Errorf("expected damage to still apply normally (1), got %d", got.Outcome.Damage)
	}

	// A second hit stacks the count.
	selfAfterFirstHit, err := json.Marshal(got.Self)
	if err != nil {
		t.Fatalf("marshal self after first hit: %v", err)
	}
	out2, err := Defend(selfAfterFirstHit, attack)
	if err != nil {
		t.Fatalf("Defend (2nd hit): %v", err)
	}
	got2 := defendResult(t, out2)
	if got2.Self.Oiliness != 2 {
		t.Errorf("expected Oiliness 2 after a second AddsOiliness hit, got %d", got2.Self.Oiliness)
	}
}

func TestDefendWithoutAddsOilinessLeavesOilinessUnchanged(t *testing.T) {
	start := overlay.Object{ID: "player:1", X: 7, Y: 7, Glyph: '@'}
	attack, err := json.Marshal(action.Result{Action: "attack", Weapon: "hands", DamageMin: 1, DamageMax: 1})
	if err != nil {
		t.Fatalf("marshal attack: %v", err)
	}

	out, err := Defend(selfJSON(t, start, 15), attack)
	if err != nil {
		t.Fatalf("Defend: %v", err)
	}
	got := defendResult(t, out)
	if got.Self.Oiliness != 0 {
		t.Errorf("expected Oiliness to stay 0 without AddsOiliness, got %d", got.Self.Oiliness)
	}
}

func TestDefendIgnitesAnOilyTargetWithinExpectedDurationRange(t *testing.T) {
	start := overlay.Object{ID: "player:1", X: 7, Y: 7, Glyph: '@', Oiliness: 2}
	attack, err := json.Marshal(action.Result{Action: "attack", Weapon: "torch", DamageMin: 1, DamageMax: 1, Ignites: true})
	if err != nil {
		t.Fatalf("marshal attack: %v", err)
	}

	out, err := Defend(selfJSON(t, start, 15), attack)
	if err != nil {
		t.Fatalf("Defend: %v", err)
	}
	got := defendResult(t, out)
	if !got.Self.Burning {
		t.Error("expected Burning true after an ignite hit on an oily target")
	}
	// Oiliness 2 => fire.Duration is the sum of 2 independent 1-3 rolls,
	// so the range is [2,6].
	if got.Self.BurningTurnsRemaining < 2 || got.Self.BurningTurnsRemaining > 6 {
		t.Errorf("expected BurningTurnsRemaining in [2,6], got %d", got.Self.BurningTurnsRemaining)
	}
	if !got.Self.BurningStartsNextTurn {
		t.Error("expected BurningStartsNextTurn true after a fresh ignition, so damage doesn't start until next turn")
	}
}

func TestDefendReignitionDoesNotResetTheGraceTurn(t *testing.T) {
	// Already burning (BurningTurnsRemaining > 0), past its own grace
	// turn (BurningStartsNextTurn already false) — a second ignition
	// should extend the duration but not re-delay damage.
	start := overlay.Object{ID: "player:1", X: 7, Y: 7, Glyph: '@', Oiliness: 1}
	self, err := json.Marshal(person{Object: start, HP: 15, BurningTurnsRemaining: 2})
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
	if got.Self.BurningStartsNextTurn {
		t.Error("expected BurningStartsNextTurn to stay false on a re-ignition while already burning")
	}
	if got.Self.BurningTurnsRemaining <= 2 {
		t.Errorf("expected the re-ignition to extend BurningTurnsRemaining beyond 2, got %d", got.Self.BurningTurnsRemaining)
	}
}

func TestDefendIgnitesDoesNothingWithoutOiliness(t *testing.T) {
	start := overlay.Object{ID: "player:1", X: 7, Y: 7, Glyph: '@'} // Oiliness 0
	attack, err := json.Marshal(action.Result{Action: "attack", Weapon: "torch", DamageMin: 1, DamageMax: 1, Ignites: true})
	if err != nil {
		t.Fatalf("marshal attack: %v", err)
	}

	out, err := Defend(selfJSON(t, start, 15), attack)
	if err != nil {
		t.Fatalf("Defend: %v", err)
	}
	got := defendResult(t, out)
	if got.Self.Burning || got.Self.BurningTurnsRemaining != 0 {
		t.Errorf("expected no ignition on a dry (non-oily) target, got Burning=%v BurningTurnsRemaining=%d", got.Self.Burning, got.Self.BurningTurnsRemaining)
	}
}

// throwItemJSON marshals a minimal item.Item-shaped inventory entry (just
// the generic overlay.Object subset plus Name, which is all Throw reads)
// with the given ID and Weight.
func throwItemJSON(t *testing.T, id string, weight int) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(struct {
		overlay.Object
		Name string
	}{Object: overlay.Object{ID: id, Weight: weight}, Name: "rock"})
	if err != nil {
		t.Fatalf("marshal thrown item: %v", err)
	}
	return data
}

// throwSelfJSON marshals a person with the given position, Attributes,
// and a single Inventory entry (the item Throw will remove).
func throwSelfJSON(t *testing.T, obj overlay.Object, strength int, invItem json.RawMessage) []byte {
	t.Helper()
	data, err := json.Marshal(person{
		Object:     obj,
		Attributes: attributes.Attributes{Strength: strength},
		Inventory:  []json.RawMessage{invItem},
	})
	if err != nil {
		t.Fatalf("marshal self: %v", err)
	}
	return data
}

// throwSelfJSONWithLitTorch marshals a person carrying a lit torch (and
// nothing in Inventory), for testing Throw's lit-torch-specific path.
func throwSelfJSONWithLitTorch(t *testing.T, obj overlay.Object, strength int, litTorchItem json.RawMessage) []byte {
	t.Helper()
	data, err := json.Marshal(person{
		Object:     obj,
		Attributes: attributes.Attributes{Strength: strength},
		LitTorch:   &litTorch{Item: litTorchItem, TurnsRemaining: 5},
	})
	if err != nil {
		t.Fatalf("marshal self: %v", err)
	}
	return data
}

// throwTorchItemJSON mirrors throwItemJSON, for a torch-shaped thrown
// item (ID/Weight/Name only — all Throw reads beyond Category, which
// only throwFlaskItemJSON needs).
func throwTorchItemJSON(t *testing.T, id string, weight int) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(struct {
		overlay.Object
		Name string
	}{Object: overlay.Object{ID: id, Weight: weight}, Name: "torch"})
	if err != nil {
		t.Fatalf("marshal thrown torch item: %v", err)
	}
	return data
}

func TestThrowLitTorchClearsLitTorchAndLitBit(t *testing.T) {
	m := testMap()
	start := overlay.Object{ID: "player:1", X: 5, Y: 7, Lit: true}
	self := throwSelfJSONWithLitTorch(t, start, 20, throwTorchItemJSON(t, "torch:1", 2))

	out, err := Throw(self, augmentedJSON(t, m), "torch:1", "Right")
	if err != nil {
		t.Fatalf("Throw: %v", err)
	}
	got := actionResult(t, out)
	if got.Self.LitTorch != nil {
		t.Errorf("expected LitTorch cleared, got %+v", got.Self.LitTorch)
	}
	if got.Self.Lit {
		t.Error("expected Lit cleared after throwing the lit torch")
	}
}

func TestThrowLitTorchHittingAnOccupantIgnites(t *testing.T) {
	m := testMap()
	start := overlay.Object{ID: "player:1", X: 5, Y: 7}
	target := overlay.Object{ID: "goblin:1", X: 7, Y: 7, Kind: "goblin"}
	self := throwSelfJSONWithLitTorch(t, start, 20, throwTorchItemJSON(t, "torch:1", 50))

	out, err := Throw(self, augmentedJSON(t, m, target), "torch:1", "Right")
	if err != nil {
		t.Fatalf("Throw: %v", err)
	}
	got := actionResult(t, out)
	if got.Result.Action != "attack" {
		t.Fatalf("expected Result.Action %q, got %q", "attack", got.Result.Action)
	}
	if !got.Result.Ignites {
		t.Error("expected Ignites true when the lit torch hits an occupant")
	}
}

func TestThrowPlainInventoryTorchDoesNotIgnite(t *testing.T) {
	m := testMap()
	start := overlay.Object{ID: "player:1", X: 5, Y: 7}
	target := overlay.Object{ID: "goblin:1", X: 7, Y: 7, Kind: "goblin"}
	self := throwSelfJSON(t, start, 20, throwTorchItemJSON(t, "torch:1", 50))

	out, err := Throw(self, augmentedJSON(t, m, target), "torch:1", "Right")
	if err != nil {
		t.Fatalf("Throw: %v", err)
	}
	got := actionResult(t, out)
	if got.Result.Action != "attack" {
		t.Fatalf("expected Result.Action %q, got %q", "attack", got.Result.Action)
	}
	if got.Result.Ignites {
		t.Error("expected Ignites false for a plain (unlit) Inventory torch")
	}
}

func TestThrowRemovesItemFromInventory(t *testing.T) {
	m := testMap()
	self := throwSelfJSON(t, overlay.Object{ID: "player:1", X: 5, Y: 7}, 20, throwItemJSON(t, "rock:1", 50))

	out, err := Throw(self, augmentedJSON(t, m), "rock:1", "Right")
	if err != nil {
		t.Fatalf("Throw: %v", err)
	}
	got := actionResult(t, out)
	if len(got.Self.Inventory) != 0 {
		t.Errorf("expected the thrown item removed from Inventory, got %+v", got.Self.Inventory)
	}
}

func TestThrowStopsAtWallOnTheFirstStep(t *testing.T) {
	m := testMap()
	// (5,5) is the room's top-left interior corner; Up immediately exits
	// the room into unmapped space with no corridor there.
	start := overlay.Object{ID: "player:1", X: 5, Y: 5}
	self := throwSelfJSON(t, start, 20, throwItemJSON(t, "rock:1", 1)) // distance 8, but blocked immediately

	out, err := Throw(self, augmentedJSON(t, m), "rock:1", "Up")
	if err != nil {
		t.Fatalf("Throw: %v", err)
	}
	got := actionResult(t, out)
	if got.Result.Action != "throw" {
		t.Errorf("expected Result.Action %q, got %q", "throw", got.Result.Action)
	}
	if got.Result.X != start.X || got.Result.Y != start.Y {
		t.Errorf("expected the item to land back at the thrower's own tile (%d,%d), got (%d,%d)", start.X, start.Y, got.Result.X, got.Result.Y)
	}
	if got.Self.X != start.X || got.Self.Y != start.Y {
		t.Errorf("expected the thrower's own position untouched by a throw, got (%d,%d)", got.Self.X, got.Self.Y)
	}
	if !got.Result.BlockedByWall {
		t.Error("expected BlockedByWall true when the throw is blocked on its very first step")
	}
}

func TestThrowHitsAnOccupantAndReportsAnAttack(t *testing.T) {
	m := testMap()
	start := overlay.Object{ID: "player:1", X: 5, Y: 7}
	target := overlay.Object{ID: "goblin:1", X: 7, Y: 7, Kind: "goblin"}
	// weight 50, strength 20 => throw.Distance == 3, enough to reach a
	// target two cells away.
	self := throwSelfJSON(t, start, 20, throwItemJSON(t, "rock:1", 50))

	out, err := Throw(self, augmentedJSON(t, m, target), "rock:1", "Right")
	if err != nil {
		t.Fatalf("Throw: %v", err)
	}
	got := actionResult(t, out)
	if got.Result.Action != "attack" {
		t.Fatalf("expected Result.Action %q, got %q", "attack", got.Result.Action)
	}
	if got.Result.X != target.X || got.Result.Y != target.Y {
		t.Errorf("expected the attack targeted (%d,%d), got (%d,%d)", target.X, target.Y, got.Result.X, got.Result.Y)
	}
	if got.Result.Weapon != "rock" {
		t.Errorf("expected Weapon %q, got %q", "rock", got.Result.Weapon)
	}
	// throw.Damage(50, 20): base ceil(50/10)=5, bonus floor(20/10)=2, so
	// max is 7.
	if got.Result.DamageMin != 1 || got.Result.DamageMax != 7 {
		t.Errorf("expected damage range [1,7] (throw.Damage(50, 20)), got [%d,%d]", got.Result.DamageMin, got.Result.DamageMax)
	}
	// Attributes is deliberately left unset on a thrown attack's Result —
	// throw.Damage already folded Strength into DamageMax, so Defend must
	// not also apply its usual post-roll Strength/10 bonus on top.
	if got.Result.Attributes.Strength != 0 {
		t.Errorf("expected Attributes NOT carried on a thrown attack's result (Strength already baked into DamageMax), got %+v", got.Result.Attributes)
	}
	if got.Result.AddsOiliness {
		t.Error("expected AddsOiliness false for a non-flask thrown item")
	}
	if got.Result.BlockedByWall {
		t.Error("expected BlockedByWall false when the throw hit an occupant, not a wall")
	}
}

// throwFlaskItemJSON mirrors throwItemJSON, but with Category set to
// item.CategoryFlask — the one field player.Throw actually reads off a
// thrown item beyond the generic overlay.Object subset and Name.
func throwFlaskItemJSON(t *testing.T, id string, weight int) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(struct {
		overlay.Object
		Category item.Category
		Name     string
	}{Object: overlay.Object{ID: id, Weight: weight}, Category: item.CategoryFlask, Name: "flask of oil"})
	if err != nil {
		t.Fatalf("marshal thrown flask item: %v", err)
	}
	return data
}

func TestThrowFlaskOfOilHittingAnOccupantAddsOiliness(t *testing.T) {
	m := testMap()
	start := overlay.Object{ID: "player:1", X: 5, Y: 7}
	target := overlay.Object{ID: "goblin:1", X: 7, Y: 7, Kind: "goblin"}
	self := throwSelfJSON(t, start, 20, throwFlaskItemJSON(t, "flask:1", 50))

	out, err := Throw(self, augmentedJSON(t, m, target), "flask:1", "Right")
	if err != nil {
		t.Fatalf("Throw: %v", err)
	}
	got := actionResult(t, out)
	if got.Result.Action != "attack" {
		t.Fatalf("expected Result.Action %q, got %q", "attack", got.Result.Action)
	}
	if !got.Result.AddsOiliness {
		t.Error("expected AddsOiliness true when a flask of oil hits an occupant")
	}
}

func TestThrowTravelsFullDistanceReportsNotBlockedByWall(t *testing.T) {
	m := testMap()
	start := overlay.Object{ID: "player:1", X: 5, Y: 7}
	self := throwSelfJSON(t, start, 20, throwItemJSON(t, "rock:1", 50))

	out, err := Throw(self, augmentedJSON(t, m), "rock:1", "Right")
	if err != nil {
		t.Fatalf("Throw: %v", err)
	}
	got := actionResult(t, out)
	if got.Result.BlockedByWall {
		t.Error("expected BlockedByWall false when the throw simply ran out of range over open ground")
	}
}

func TestThrowTravelsTheFullDistanceWhenUnobstructed(t *testing.T) {
	m := testMap()
	start := overlay.Object{ID: "player:1", X: 5, Y: 7}
	// weight 50, strength 20 => throw.Distance == 3, well within the
	// room's open interior (x:5-9) with nothing blocking it.
	self := throwSelfJSON(t, start, 20, throwItemJSON(t, "rock:1", 50))

	out, err := Throw(self, augmentedJSON(t, m), "rock:1", "Right")
	if err != nil {
		t.Fatalf("Throw: %v", err)
	}
	got := actionResult(t, out)
	if got.Result.Action != "throw" {
		t.Errorf("expected Result.Action %q, got %q", "throw", got.Result.Action)
	}
	wantX := start.X + 3
	if got.Result.X != wantX || got.Result.Y != start.Y {
		t.Errorf("expected the item to land at (%d,%d), got (%d,%d)", wantX, start.Y, got.Result.X, got.Result.Y)
	}
}

func TestThrowInvalidDirectionErrors(t *testing.T) {
	m := testMap()
	self := throwSelfJSON(t, overlay.Object{ID: "player:1", X: 5, Y: 7}, 20, throwItemJSON(t, "rock:1", 1))

	if _, err := Throw(self, augmentedJSON(t, m), "rock:1", "."); err == nil {
		t.Error("expected an error for a non-directional throw key, got nil")
	}
}

func TestThrowErrorsWhenItemNotInInventory(t *testing.T) {
	m := testMap()
	self := throwSelfJSON(t, overlay.Object{ID: "player:1", X: 5, Y: 7}, 20, throwItemJSON(t, "rock:1", 1))

	if _, err := Throw(self, augmentedJSON(t, m), "rock:does-not-exist", "Right"); err == nil {
		t.Error("expected an error for an item not in Inventory, got nil")
	}
}
