package goblin

import (
	"encoding/json"
	"regexp"
	"testing"

	"micromap/server/internal/action"
	"micromap/server/internal/attributes"
	"micromap/server/internal/item"
	"micromap/server/internal/mapdata"
	"micromap/server/internal/overlay"
	"micromap/server/internal/world"
)

// testMap: one 10x10 room (interior x:5-14, y:5-14) big enough for chase
// tests, connected by a corridor to a separate small room (interior
// x:30-34, y:5-9) used for the "different room" case.
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

// corridorPath lists every unit-step tile from the door at (15,9) east to
// (29,9), then north to (29,7) — matching how a real generated corridor's
// Path always includes every intermediate tile (see mapgen's appendRun),
// not just waypoints. CanMove/NextStep only step to direct 4-neighbors, so
// a Path with gaps would make those skipped tiles unrecognized as corridor
// cells at all.
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
	data, err := json.Marshal(monster{
		Object: overlay.Object{ID: id, X: x, Y: y, Glyph: 'g', Color: overlay.Color{G: 200}, Layer: 1},
		Aware:  aware,
	})
	if err != nil {
		t.Fatalf("marshal self: %v", err)
	}
	return data
}

func selfJSONFrom(t *testing.T, m monster) []byte {
	t.Helper()
	data, err := json.Marshal(m)
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
	Self   monster       `json:"self"`
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

func initResult(t *testing.T, out []byte) monster {
	t.Helper()
	var got monster
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("unmarshal Init output: %v", err)
	}
	return got
}

func TestInitPlacesGoblinInARoom(t *testing.T) {
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

	if ok, _ := regexp.MatchString(`^goblin:\d+$`, got.ID); !ok {
		t.Errorf("expected ID matching goblin:<N>, got %q", got.ID)
	}
	if got.Glyph != 'g' {
		t.Errorf("expected glyph 'g', got %q", got.Glyph)
	}
	if got.Color != (overlay.Color{G: 200}) {
		t.Errorf("expected green color, got %+v", got.Color)
	}
	if got.Aware {
		t.Error("expected a freshly spawned goblin to not be aware of the Player yet")
	}
	if got.HP < 5 || got.HP > 10 {
		t.Errorf("expected HP in [5,10], got %d", got.HP)
	}
	if got.MaxHP != got.HP {
		t.Errorf("expected MaxHP to equal starting HP, got MaxHP=%d HP=%d", got.MaxHP, got.HP)
	}
	if got.Kind != "goblin" {
		t.Errorf("expected Kind %q, got %q", "goblin", got.Kind)
	}
	if got.Weight != 50 {
		t.Errorf("expected Weight 50, got %d", got.Weight)
	}
	if got.Level != 1 {
		t.Errorf("expected a goblin to be level 1, got %d", got.Level)
	}
	if got.Wielded != nil {
		t.Errorf("expected a plain Init goblin to be unarmed, got Wielded=%+v", got.Wielded)
	}
	for name, v := range map[string]int{
		"Strength": got.Attributes.Strength, "Intelligence": got.Attributes.Intelligence,
		"Dexterity": got.Attributes.Dexterity, "Stamina": got.Attributes.Stamina,
	} {
		if v < 2 || v > 6 {
			t.Errorf("expected %s in [2,6], got %d", name, v)
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
		t.Errorf("expected goblin at (%d,%d) to be inside some room", got.X, got.Y)
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

func TestActionStaysPutAndUnawareWhenPlayerElsewhere(t *testing.T) {
	m := testMap()
	cases := []struct {
		name   string
		player overlay.Object
	}{
		{"player in a different room", overlay.Object{ID: "player:1", X: 32, Y: 7}},
		{"player in the corridor", overlay.Object{ID: "player:1", X: 20, Y: 9}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out, err := Action(selfJSON(t, "goblin:1", 6, 6, false), augmentedJSON(t, m, c.player))
			if err != nil {
				t.Fatalf("Action: %v", err)
			}
			got := actionResult(t, out)
			if got.Self.X != 6 || got.Self.Y != 6 {
				t.Errorf("expected goblin to stay put, got (%d,%d)", got.Self.X, got.Self.Y)
			}
			if got.Self.Aware {
				t.Error("expected goblin to remain unaware when the Player isn't in its room")
			}
			if got.Result.Action != "move" || got.Result.X != 6 || got.Result.Y != 6 {
				t.Errorf("expected no-op move result at (6,6), got %+v", got.Result)
			}
		})
	}
}

func TestActionBecomesAwareAndMovesWhenPlayerEntersTheRoom(t *testing.T) {
	m := testMap()
	cases := []struct {
		name         string
		player       overlay.Object
		wantX, wantY int
	}{
		{"horizontal dominant", overlay.Object{ID: "player:1", X: 12, Y: 7}, 7, 6},
		{"vertical dominant", overlay.Object{ID: "player:1", X: 7, Y: 12}, 6, 7},
		{"tie breaks horizontal", overlay.Object{ID: "player:1", X: 9, Y: 9}, 7, 6},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out, err := Action(selfJSON(t, "goblin:1", 6, 6, false), augmentedJSON(t, m, c.player))
			if err != nil {
				t.Fatalf("Action: %v", err)
			}
			got := actionResult(t, out)
			if !got.Self.Aware {
				t.Error("expected goblin to become aware once the Player shares its room")
			}
			if got.Self.X != c.wantX || got.Self.Y != c.wantY {
				t.Errorf("got (%d,%d), want (%d,%d)", got.Self.X, got.Self.Y, c.wantX, c.wantY)
			}
			if got.Result.Action != "move" || got.Result.X != c.wantX || got.Result.Y != c.wantY {
				t.Errorf("expected move result at (%d,%d), got %+v", c.wantX, c.wantY, got.Result)
			}
		})
	}
}

func TestActionBecomesAwareWhenPlayerIsOnTheDoorThreshold(t *testing.T) {
	m := testMap()
	// (15,9) is room A's door — an open threshold, not a barrier. Standing
	// there should count as being in the room for visibility, same as the
	// Player being able to see the goblin from that same spot.
	p := overlay.Object{ID: "player:1", X: 15, Y: 9}

	out, err := Action(selfJSON(t, "goblin:1", 6, 6, false), augmentedJSON(t, m, p))
	if err != nil {
		t.Fatalf("Action: %v", err)
	}
	got := actionResult(t, out)
	if !got.Self.Aware {
		t.Error("expected goblin to become aware of a Player standing on its room's door threshold")
	}
}

func TestActionChasesPlayerThroughDoorsIntoAnotherRoom(t *testing.T) {
	m := testMap()
	self := selfJSON(t, "goblin:1", 6, 6, true) // already aware
	p := overlay.Object{ID: "player:1", X: 32, Y: 7}
	gx, gy := 6, 6

	reachedRoomB := false
	for turn := 0; turn < 40; turn++ {
		out, err := Action(self, augmentedJSON(t, m, p))
		if err != nil {
			t.Fatalf("turn %d: Action: %v", turn, err)
		}
		next := actionResult(t, out)
		if next.Self.X == gx && next.Self.Y == gy {
			t.Fatalf("turn %d: goblin stopped making progress at (%d,%d)", turn, gx, gy)
		}
		gx, gy = next.Self.X, next.Self.Y
		self = selfJSONFrom(t, next.Self)
		if r, ok := m.RoomAt(gx, gy); ok && r.ID == 1 {
			reachedRoomB = true
			break
		}
	}
	if !reachedRoomB {
		t.Fatalf("expected goblin to follow the corridor into room B, ended at (%d,%d)", gx, gy)
	}
}

func TestActionChasesPlayerIntoCorridor(t *testing.T) {
	m := testMap()
	self := selfJSON(t, "goblin:1", 6, 6, true) // already aware
	p := overlay.Object{ID: "player:1", X: 20, Y: 9}
	gx, gy := 6, 6

	for turn := 0; turn < 40; turn++ {
		out, err := Action(self, augmentedJSON(t, m, p))
		if err != nil {
			t.Fatalf("turn %d: Action: %v", turn, err)
		}
		next := actionResult(t, out)
		if next.Result.Action == "attack" {
			break // reached and attacked the player
		}
		if next.Self.X == gx && next.Self.Y == gy {
			t.Fatalf("turn %d: goblin stopped making progress at (%d,%d)", turn, gx, gy)
		}
		gx, gy = next.Self.X, next.Self.Y
		self = selfJSONFrom(t, next.Self)
	}

	dx, dy := gx-p.X, gy-p.Y
	if dx < 0 {
		dx = -dx
	}
	if dy < 0 {
		dy = -dy
	}
	if dx+dy != 1 {
		t.Errorf("expected goblin to end up adjacent to the player at (%d,%d), got (%d,%d)", p.X, p.Y, gx, gy)
	}
}

func TestActionDoesNotMoveWhenNoPathExists(t *testing.T) {
	roomA := mapdata.Room{ID: 0, X: 5, Y: 5, W: 5, H: 5}
	roomB := mapdata.Room{ID: 1, X: 20, Y: 5, W: 5, H: 5}
	m := mapdata.Map{Width: 30, Height: 20, Rooms: []mapdata.Room{roomA, roomB}} // no corridor at all

	p := overlay.Object{ID: "player:1", X: 21, Y: 6}

	out, err := Action(selfJSON(t, "goblin:1", 6, 6, true), augmentedJSON(t, m, p))
	if err != nil {
		t.Fatalf("Action: %v", err)
	}
	got := actionResult(t, out)
	if got.Self.X != 6 || got.Self.Y != 6 {
		t.Errorf("expected goblin to stay put with no path to the player, got (%d,%d)", got.Self.X, got.Self.Y)
	}
	if got.Result.Action != "move" || got.Result.X != 6 || got.Result.Y != 6 {
		t.Errorf("expected no-op move result at (6,6), got %+v", got.Result)
	}
}

func TestActionSlidesOnOilyDestination(t *testing.T) {
	m := testMap()
	self := selfJSON(t, "goblin:1", 6, 6, true) // already aware
	p := overlay.Object{ID: "player:1", X: 12, Y: 6}
	// The goblin's first chase step from (6,6) toward (12,6) is (7,6),
	// well inside the room's open interior (5-14,5-14) — every one of
	// the 4 slide candidates (stop, forward, and both perpendiculars) is
	// legal and unobstructed.
	aug := world.AugmentedMap{
		Map:       m,
		Objects:   []overlay.Object{p},
		OilyTiles: []mapdata.Point{{X: 7, Y: 6}},
	}
	augJSON, err := json.Marshal(aug)
	if err != nil {
		t.Fatalf("marshal augmented map: %v", err)
	}

	seen := map[mapdata.Point]bool{}
	for i := 0; i < 400; i++ {
		out, err := Action(self, augJSON)
		if err != nil {
			t.Fatalf("Action: %v", err)
		}
		got := actionResult(t, out)
		pos := mapdata.Point{X: got.Self.X, Y: got.Self.Y}
		switch pos {
		case mapdata.Point{X: 7, Y: 6}, mapdata.Point{X: 8, Y: 6}, mapdata.Point{X: 7, Y: 5}, mapdata.Point{X: 7, Y: 7}:
		default:
			t.Fatalf("unexpected resulting position %+v", pos)
		}
		seen[pos] = true
	}
	if len(seen) < 2 {
		t.Errorf("expected the oily destination to sometimes divert the goblin elsewhere across 400 attempts, only ever saw %+v", seen)
	}
}

func TestActionAttacksWhenAdjacentToPlayer(t *testing.T) {
	m := testMap()
	self := selfJSON(t, "goblin:1", 6, 6, false) // not yet aware; player starts adjacent
	p := overlay.Object{ID: "player:1", X: 7, Y: 6}

	for i := 0; i < 3; i++ {
		out, err := Action(self, augmentedJSON(t, m, p))
		if err != nil {
			t.Fatalf("turn %d: Action: %v", i, err)
		}
		got := actionResult(t, out)
		if got.Self.X != 6 || got.Self.Y != 6 {
			t.Fatalf("turn %d: expected goblin to stay put at (6,6), got (%d,%d)", i, got.Self.X, got.Self.Y)
		}
		if !got.Self.Aware {
			t.Fatalf("turn %d: expected goblin to have become aware after sharing a room with the Player", i)
		}
		want := action.Result{Action: "attack", X: p.X, Y: p.Y, Weapon: "hands", DamageMin: 1, DamageMax: 2}
		if got.Result != want {
			t.Fatalf("turn %d: got result %+v, want %+v", i, got.Result, want)
		}
		self = selfJSONFrom(t, got.Self)
	}
}

func TestActionAttackCarriesAttackersAttributesOnTheResult(t *testing.T) {
	m := testMap()
	attrs := attributes.Attributes{Strength: 23, Intelligence: 5, Dexterity: 4, Stamina: 3}
	self, err := json.Marshal(monster{
		Object:     overlay.Object{ID: "goblin:1", X: 6, Y: 6, Glyph: 'g', Color: overlay.Color{G: 200}, Layer: 1},
		Aware:      true,
		Attributes: attrs,
	})
	if err != nil {
		t.Fatalf("marshal self: %v", err)
	}
	p := overlay.Object{ID: "player:1", X: 7, Y: 6}

	out, err := Action(self, augmentedJSON(t, m, p))
	if err != nil {
		t.Fatalf("Action: %v", err)
	}
	got := actionResult(t, out)

	if got.Result.Attributes != attrs {
		t.Errorf("expected the attack result to carry the attacker's Attributes %+v, got %+v", attrs, got.Result.Attributes)
	}
}

func TestActionFollowsPlayerAcrossMultipleTurns(t *testing.T) {
	m := testMap()
	self := selfJSON(t, "goblin:1", 6, 6, false)
	p := overlay.Object{ID: "player:1", X: 6, Y: 10} // fixed target, in the same room, 4 tiles away

	// Three turns close the distance to adjacent (6,9) (the first also
	// triggers awareness); a fourth turn would step onto the player's own
	// tile, so it attacks instead and stays at (6,9).
	var got actionResponse
	for turn := 0; turn < 3; turn++ {
		out, err := Action(self, augmentedJSON(t, m, p))
		if err != nil {
			t.Fatalf("turn %d: Action: %v", turn, err)
		}
		got = actionResult(t, out)
		self = selfJSONFrom(t, got.Self)
	}
	if got.Self.X != 6 || got.Self.Y != 9 {
		t.Fatalf("expected goblin to have closed in to (6,9) after 3 turns, got (%d,%d)", got.Self.X, got.Self.Y)
	}
	if !got.Self.Aware {
		t.Fatal("expected goblin to be aware after sharing a room with the Player")
	}

	out, err := Action(self, augmentedJSON(t, m, p))
	if err != nil {
		t.Fatalf("Action: %v", err)
	}
	attacked := actionResult(t, out)
	if attacked.Self.X != 6 || attacked.Self.Y != 9 {
		t.Errorf("expected goblin to attack and stay at (6,9), got (%d,%d)", attacked.Self.X, attacked.Self.Y)
	}
	want := action.Result{Action: "attack", X: p.X, Y: p.Y, Weapon: "hands", DamageMin: 1, DamageMax: 2}
	if attacked.Result != want {
		t.Errorf("got result %+v, want %+v", attacked.Result, want)
	}
}

func TestActionErrorsWithInvalidSelfJSON(t *testing.T) {
	m := testMap()
	p := overlay.Object{ID: "player:1", X: 7, Y: 6}
	if _, err := Action([]byte("not json"), augmentedJSON(t, m, p)); err == nil {
		t.Error("expected an error for invalid self JSON, got nil")
	}
}

func TestActionErrorsWithoutPlayerObject(t *testing.T) {
	m := testMap()
	if _, err := Action(selfJSON(t, "goblin:1", 6, 6, false), augmentedJSON(t, m)); err == nil {
		t.Error("expected an error when no player object is present, got nil")
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

	g1, g2 := initResult(t, out1), initResult(t, out2)
	if g1.X != g2.X || g1.Y != g2.Y {
		t.Errorf("expected the same map seed and spawnIndex to always spawn the goblin at the same position, got (%d,%d) and (%d,%d)",
			g1.X, g1.Y, g2.X, g2.Y)
	}
	if g1.HP != g2.HP {
		t.Errorf("expected the same map seed and spawnIndex to always roll the same starting HP, got %d and %d", g1.HP, g2.HP)
	}
	if g1.Attributes != g2.Attributes {
		t.Errorf("expected the same map seed and spawnIndex to always roll the same attributes, got %+v and %+v", g1.Attributes, g2.Attributes)
	}
}

// TestInitDistinguishesGoblinsByIndexOnTheSameLevel is the direct
// regression test for the bug where every goblin populateLevel spawned on
// one level landed on the identical cell with identical stats: since they
// all re-seeded from the same m.Seed with nothing else varying, every
// call drew the exact same sequence. spawnIndex is what breaks that tie —
// same map, different index, should (overwhelmingly likely) land
// somewhere else. A single coincidental collision on one probe is
// possible by chance (many rooms, but not infinitely many), so this
// checks a spread of indices and only fails if none of them differ.
func TestInitDistinguishesGoblinsByIndexOnTheSameLevel(t *testing.T) {
	m := testMap()
	m.Seed = 42
	mapJSON, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal map: %v", err)
	}

	out0, err := Init(mapJSON, 0)
	if err != nil {
		t.Fatalf("Init (index 0): %v", err)
	}
	g0 := initResult(t, out0)

	anyDifferent := false
	for i := 1; i <= 5; i++ {
		out, err := Init(mapJSON, i)
		if err != nil {
			t.Fatalf("Init (index %d): %v", i, err)
		}
		gi := initResult(t, out)
		if gi.X != g0.X || gi.Y != g0.Y || gi.HP != g0.HP || gi.Attributes != g0.Attributes {
			anyDifferent = true
			break
		}
	}
	if !anyDifferent {
		t.Error("expected at least one of spawnIndex 1-5 to differ from spawnIndex 0 in position, HP, or attributes, but all matched")
	}
}

func TestInitRollsAttributesWithinRange(t *testing.T) {
	m := testMap()
	for seed := int64(0); seed < 30; seed++ {
		m.Seed = seed
		mapJSON, err := json.Marshal(m)
		if err != nil {
			t.Fatalf("seed %d: marshal map: %v", seed, err)
		}
		out, err := Init(mapJSON, 0)
		if err != nil {
			t.Fatalf("seed %d: Init: %v", seed, err)
		}
		got := initResult(t, out).Attributes
		for name, v := range map[string]int{
			"Strength": got.Strength, "Intelligence": got.Intelligence,
			"Dexterity": got.Dexterity, "Stamina": got.Stamina,
		} {
			if v < 2 || v > 6 {
				t.Fatalf("seed %d: expected %s in [2,6], got %d", seed, name, v)
			}
		}
	}
}

// selfJSONWithHP is selfJSON's counterpart for Defend tests, which need
// control over starting HP.
func selfJSONWithHP(t *testing.T, id string, x, y, hp int) []byte {
	t.Helper()
	data, err := json.Marshal(monster{
		Object: overlay.Object{ID: id, X: x, Y: y, Glyph: 'g', Color: overlay.Color{G: 200}, Layer: 1},
		HP:     hp,
	})
	if err != nil {
		t.Fatalf("marshal self: %v", err)
	}
	return data
}

func defendResult(t *testing.T, out []byte) defendResponse {
	t.Helper()
	var got defendResponse
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("unmarshal Defend output: %v", err)
	}
	return got
}

func TestDefendAppliesDamageWithinRange(t *testing.T) {
	self := selfJSONWithHP(t, "goblin:1", 6, 6, 8)
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
		if got.Self.HP != 8-got.Outcome.Damage {
			t.Errorf("expected HP %d, got %d", 8-got.Outcome.Damage, got.Self.HP)
		}
		if got.Outcome.RemainingHP != got.Self.HP {
			t.Errorf("expected outcome.RemainingHP to match self.HP, got %d vs %d", got.Outcome.RemainingHP, got.Self.HP)
		}
		if got.Outcome.Dead {
			t.Errorf("expected goblin to survive a 1-2 damage hit from 8 HP")
		}
		if got.Self.X != 6 || got.Self.Y != 6 {
			t.Errorf("expected position to be untouched by Defend, got (%d,%d)", got.Self.X, got.Self.Y)
		}
	}
}

func TestDefendAddsFlatStrengthBonusFromAttacker(t *testing.T) {
	self := selfJSONWithHP(t, "goblin:1", 6, 6, 50)

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

		out, err := Defend(self, attack)
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
	self := selfJSONWithHP(t, "goblin:1", 6, 6, 3)
	attack, err := json.Marshal(action.Result{Action: "attack", Weapon: "hands", DamageMin: 5, DamageMax: 5})
	if err != nil {
		t.Fatalf("marshal attack: %v", err)
	}

	out, err := Defend(self, attack)
	if err != nil {
		t.Fatalf("Defend: %v", err)
	}
	got := defendResult(t, out)
	if got.Outcome.Damage != 5 {
		t.Errorf("expected damage 5, got %d", got.Outcome.Damage)
	}
	if got.Self.HP != -2 {
		t.Errorf("expected HP -2, got %d", got.Self.HP)
	}
	if !got.Outcome.Dead || !got.Self.Dead {
		t.Errorf("expected goblin to be dead at negative HP, got outcome.Dead=%v self.Dead=%v", got.Outcome.Dead, got.Self.Dead)
	}
}

func TestDefendAddsOilinessIndependentlyOfDamage(t *testing.T) {
	self := selfJSONWithHP(t, "goblin:1", 6, 6, 8)
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
	if got.Outcome.Damage != 1 {
		t.Errorf("expected damage to still apply normally (1), got %d", got.Outcome.Damage)
	}
}

func TestDefendWithoutAddsOilinessLeavesOilinessUnchanged(t *testing.T) {
	self := selfJSONWithHP(t, "goblin:1", 6, 6, 8)
	attack, err := json.Marshal(action.Result{Action: "attack", Weapon: "hands", DamageMin: 1, DamageMax: 1})
	if err != nil {
		t.Fatalf("marshal attack: %v", err)
	}

	out, err := Defend(self, attack)
	if err != nil {
		t.Fatalf("Defend: %v", err)
	}
	got := defendResult(t, out)
	if got.Self.Oiliness != 0 {
		t.Errorf("expected Oiliness to stay 0 without AddsOiliness, got %d", got.Self.Oiliness)
	}
}

func TestDefendIgnitesAnOilyTargetWithinExpectedDurationRange(t *testing.T) {
	self, err := json.Marshal(monster{
		Object: overlay.Object{ID: "goblin:1", X: 6, Y: 6, Glyph: 'g', Color: overlay.Color{G: 200}, Layer: 1, Oiliness: 2},
		HP:     8, MaxHP: 8,
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
	// turn — a second ignition should extend the duration but not
	// re-delay damage.
	self, err := json.Marshal(monster{
		Object: overlay.Object{ID: "goblin:1", X: 6, Y: 6, Glyph: 'g', Color: overlay.Color{G: 200}, Layer: 1, Oiliness: 1},
		HP:     8, MaxHP: 8, BurningTurnsRemaining: 2,
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
	if got.Self.BurningStartsNextTurn {
		t.Error("expected BurningStartsNextTurn to stay false on a re-ignition while already burning")
	}
	if got.Self.BurningTurnsRemaining <= 2 {
		t.Errorf("expected the re-ignition to extend BurningTurnsRemaining beyond 2, got %d", got.Self.BurningTurnsRemaining)
	}
}

func TestDefendIgnitesDoesNothingWithoutOiliness(t *testing.T) {
	self := selfJSONWithHP(t, "goblin:1", 6, 6, 8) // Oiliness 0
	attack, err := json.Marshal(action.Result{Action: "attack", Weapon: "torch", DamageMin: 1, DamageMax: 1, Ignites: true})
	if err != nil {
		t.Fatalf("marshal attack: %v", err)
	}

	out, err := Defend(self, attack)
	if err != nil {
		t.Fatalf("Defend: %v", err)
	}
	got := defendResult(t, out)
	if got.Self.Burning || got.Self.BurningTurnsRemaining != 0 {
		t.Errorf("expected no ignition on a dry (non-oily) target, got Burning=%v BurningTurnsRemaining=%d", got.Self.Burning, got.Self.BurningTurnsRemaining)
	}
}

func TestActionDoesNotBecomeAwareOfInvisiblePlayerEvenInSameRoom(t *testing.T) {
	m := testMap()
	p := overlay.Object{ID: "player:1", X: 9, Y: 9, Invisible: true} // in the goblin's own room

	out, err := Action(selfJSON(t, "goblin:1", 6, 6, false), augmentedJSON(t, m, p))
	if err != nil {
		t.Fatalf("Action: %v", err)
	}
	got := actionResult(t, out)
	if got.Self.Aware {
		t.Error("expected goblin to remain unaware of an Invisible Player even sharing its room")
	}
	if got.Self.X != 6 || got.Self.Y != 6 {
		t.Errorf("expected goblin to stay put, got (%d,%d)", got.Self.X, got.Self.Y)
	}
}

func TestActionDoesNotAttackInvisiblePlayerAdjacent(t *testing.T) {
	m := testMap()
	self := selfJSON(t, "goblin:1", 6, 6, true) // already aware
	p := overlay.Object{ID: "player:1", X: 7, Y: 6, Invisible: true}

	out, err := Action(self, augmentedJSON(t, m, p))
	if err != nil {
		t.Fatalf("Action: %v", err)
	}
	got := actionResult(t, out)
	if got.Result.Action != "move" || got.Result.X != 6 || got.Result.Y != 6 {
		t.Errorf("expected a no-op move result at (6,6), got %+v", got.Result)
	}
	if got.Self.X != 6 || got.Self.Y != 6 {
		t.Errorf("expected goblin to stay put rather than attack or step onto an Invisible Player's tile, got (%d,%d)", got.Self.X, got.Self.Y)
	}
}

func TestActionLosesAwarenessAndStaysPutWhenAlreadyAwarePlayerTurnsInvisible(t *testing.T) {
	m := testMap()
	self := selfJSON(t, "goblin:1", 6, 6, true) // already aware
	p := overlay.Object{ID: "player:1", X: 6, Y: 10, Invisible: true}

	out, err := Action(self, augmentedJSON(t, m, p))
	if err != nil {
		t.Fatalf("Action: %v", err)
	}
	got := actionResult(t, out)
	if got.Self.Aware {
		t.Error("expected an already-aware goblin to lose awareness once the Player turns Invisible")
	}
	if got.Self.X != 6 || got.Self.Y != 6 {
		t.Errorf("expected goblin to stay put rather than keep closing in on a Player it can no longer perceive, got (%d,%d)", got.Self.X, got.Self.Y)
	}
}

func TestInitArmedSpawnsGoblinWithWieldedWeaponStats(t *testing.T) {
	m := testMap()
	mapJSON, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal map: %v", err)
	}
	stats := item.WeaponStats{Name: "rusty sword", BaseDamageMax: 6, Enchantment: -1}
	statsJSON, err := json.Marshal(stats)
	if err != nil {
		t.Fatalf("marshal stats: %v", err)
	}

	out, err := InitArmed(mapJSON, statsJSON, 0)
	if err != nil {
		t.Fatalf("InitArmed: %v", err)
	}
	got := initResult(t, out)

	if got.Kind != "goblin" {
		t.Errorf("expected Kind %q, got %q", "goblin", got.Kind)
	}
	if got.Wielded == nil {
		t.Fatal("expected Wielded to be set")
	}
	if got.Wielded.Name != "rusty sword" || got.Wielded.BaseDamageMax != 6 || got.Wielded.Enchantment != -1 {
		t.Errorf("got Wielded %+v, want matching rusty sword stats", got.Wielded)
	}
}

func TestActionAttacksWithWieldedWeaponUsesItsStatsRange(t *testing.T) {
	m := testMap()
	stats := item.WeaponStats{Name: "rusty sword", BaseDamageMax: 6, Enchantment: -1}
	self, err := json.Marshal(monster{
		Object:  overlay.Object{ID: "goblin:1", X: 6, Y: 6, Glyph: 'g', Color: overlay.Color{G: 200}, Layer: 1},
		Aware:   true,
		Wielded: &stats,
	})
	if err != nil {
		t.Fatalf("marshal self: %v", err)
	}
	p := overlay.Object{ID: "player:1", X: 7, Y: 6, Kind: "player"}

	out, err := Action(self, augmentedJSON(t, m, p))
	if err != nil {
		t.Fatalf("Action: %v", err)
	}
	got := actionResult(t, out)

	wantMin, wantMax := stats.Range("player")
	want := action.Result{Action: "attack", X: p.X, Y: p.Y, Weapon: "rusty sword", DamageMin: wantMin, DamageMax: wantMax}
	if got.Result != want {
		t.Errorf("got result %+v, want %+v", got.Result, want)
	}
}

func TestCorpseBuildsFloorItemAtDeathPosition(t *testing.T) {
	self := monster{
		Object: overlay.Object{ID: "goblin:7", X: 12, Y: 4, Glyph: 'g', Color: overlay.Color{G: 200}, Layer: 1, Kind: "goblin", Weight: 50, Dead: true},
		Aware:  true,
		HP:     -3,
		MaxHP:  8,
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
	if it.Glyph != '%' {
		t.Errorf("expected corpse glyph '%%', got %q", it.Glyph)
	}
	if it.Category != item.CategoryCorpse {
		t.Errorf("expected Category CategoryCorpse, got %q", it.Category)
	}
	if it.Usage != item.UsagePersistent {
		t.Errorf("expected Usage UsagePersistent, got %q", it.Usage)
	}
	if it.Kind != "goblin" {
		t.Errorf("expected Kind %q, got %q", "goblin", it.Kind)
	}
	if it.Name != "goblin corpse" {
		t.Errorf("expected Name %q, got %q", "goblin corpse", it.Name)
	}
	if it.Source != "goblin:7" {
		t.Errorf("expected Source %q, got %q", "goblin:7", it.Source)
	}
	if it.ID == "" || it.ID == "goblin:7" {
		t.Errorf("expected a freshly minted corpse ID distinct from the goblin's own, got %q", it.ID)
	}
	if it.Weight != 50 {
		t.Errorf("expected corpse Weight to carry over from the goblin's own (50), got %d", it.Weight)
	}
}

func TestCorpseIncludesWieldedStatsWhenArmed(t *testing.T) {
	stats := item.WeaponStats{Name: "rusty sword", BaseDamageMax: 6, Enchantment: -1}
	self := monster{
		Object:  overlay.Object{ID: "goblin:1", X: 6, Y: 6, Glyph: 'g', Kind: "goblin", Dead: true},
		Wielded: &stats,
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

	if cr.Wielded == nil {
		t.Fatal("expected Wielded to be returned for an armed goblin")
	}
	if cr.Wielded.Name != "rusty sword" || cr.Wielded.BaseDamageMax != 6 || cr.Wielded.Enchantment != -1 {
		t.Errorf("got Wielded %+v, want matching rusty sword stats", cr.Wielded)
	}
}

func TestCorpseWieldedNilWhenBareHanded(t *testing.T) {
	self := monster{Object: overlay.Object{ID: "goblin:1", X: 6, Y: 6, Kind: "goblin", Dead: true}}
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
	if cr.Wielded != nil {
		t.Errorf("expected Wielded nil for a bare-handed goblin, got %+v", cr.Wielded)
	}
}

func TestCorpseClearsWieldedInReturnedSelf(t *testing.T) {
	stats := item.WeaponStats{Name: "rusty sword", BaseDamageMax: 6, Enchantment: -1}
	self := monster{
		Object:  overlay.Object{ID: "goblin:1", X: 6, Y: 6, Kind: "goblin", Dead: true},
		Wielded: &stats,
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

	var gotSelf monster
	if err := json.Unmarshal(cr.Self, &gotSelf); err != nil {
		t.Fatalf("unmarshal self: %v", err)
	}
	if gotSelf.Wielded != nil {
		t.Errorf("expected Wielded cleared in returned Self, got %+v", gotSelf.Wielded)
	}
}

func TestCorpsePreservesNonInventoryAttributesInSelf(t *testing.T) {
	self := monster{
		Object:     overlay.Object{ID: "goblin:9", X: 3, Y: 5, Glyph: 'g', Color: overlay.Color{G: 200}, Layer: 1, Kind: "goblin", Weight: 50, Dead: true},
		Aware:      true,
		HP:         -1,
		MaxHP:      9,
		Attributes: attributes.Attributes{Strength: 5, Intelligence: 3, Dexterity: 6, Stamina: 2},
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

	var gotSelf monster
	if err := json.Unmarshal(cr.Self, &gotSelf); err != nil {
		t.Fatalf("unmarshal self: %v", err)
	}
	if gotSelf.ID != self.ID || gotSelf.X != self.X || gotSelf.Y != self.Y || gotSelf.Kind != self.Kind {
		t.Errorf("expected identity/position preserved, got %+v", gotSelf)
	}
	if gotSelf.HP != self.HP || gotSelf.MaxHP != self.MaxHP {
		t.Errorf("expected HP/MaxHP preserved, got HP=%d MaxHP=%d", gotSelf.HP, gotSelf.MaxHP)
	}
	if gotSelf.Aware != self.Aware {
		t.Errorf("expected Aware preserved, got %v", gotSelf.Aware)
	}
	if gotSelf.Attributes != self.Attributes {
		t.Errorf("expected Attributes preserved, got %+v, want %+v", gotSelf.Attributes, self.Attributes)
	}
	if gotSelf.Weight != self.Weight {
		t.Errorf("expected Weight preserved, got %d, want %d", gotSelf.Weight, self.Weight)
	}
	if !gotSelf.Dead {
		t.Error("expected Dead to remain true")
	}
}

// threeRoomFleeMap: three disconnected rooms, no corridors — enough for
// pickFleeRoom's own selection logic (which only needs aug.Map.Rooms),
// without needing actual routes between them.
func threeRoomFleeMap() mapdata.Map {
	roomA := mapdata.Room{ID: 0, X: 5, Y: 5, W: 5, H: 5}
	roomB := mapdata.Room{ID: 1, X: 20, Y: 5, W: 5, H: 5}
	roomC := mapdata.Room{ID: 2, X: 35, Y: 5, W: 5, H: 5}
	return mapdata.Map{Width: 50, Height: 20, Rooms: []mapdata.Room{roomA, roomB, roomC}}
}

func TestFleeThresholdIsAtLeastOneAcrossGoblinsHPRange(t *testing.T) {
	for maxHP := 5; maxHP <= 10; maxHP++ {
		if got := fleeThreshold(maxHP); got != 1 {
			t.Errorf("maxHP=%d: expected threshold 1, got %d", maxHP, got)
		}
	}
}

func TestDefendSetsFleeingWhenHPDropsToThreshold(t *testing.T) {
	self, err := json.Marshal(monster{
		Object: overlay.Object{ID: "goblin:1", X: 6, Y: 6},
		HP:     2,
		MaxHP:  10,
	})
	if err != nil {
		t.Fatalf("marshal self: %v", err)
	}
	attack, err := json.Marshal(action.Result{Action: "attack", Weapon: "hands", DamageMin: 1, DamageMax: 1})
	if err != nil {
		t.Fatalf("marshal attack: %v", err)
	}

	out, err := Defend(self, attack)
	if err != nil {
		t.Fatalf("Defend: %v", err)
	}
	got := defendResult(t, out)
	if got.Self.HP != 1 {
		t.Fatalf("test setup: expected HP 1 after this hit, got %d", got.Self.HP)
	}
	if !got.Self.Fleeing {
		t.Error("expected Fleeing set true once HP drops to the threshold")
	}
	if got.Self.FleeRoomID != -1 {
		t.Errorf("expected FleeRoomID -1 (not yet chosen), got %d", got.Self.FleeRoomID)
	}
}

func TestDefendDoesNotSetFleeingWhenHPStaysAboveThreshold(t *testing.T) {
	self, err := json.Marshal(monster{
		Object: overlay.Object{ID: "goblin:1", X: 6, Y: 6},
		HP:     10,
		MaxHP:  10,
	})
	if err != nil {
		t.Fatalf("marshal self: %v", err)
	}
	attack, err := json.Marshal(action.Result{Action: "attack", Weapon: "hands", DamageMin: 1, DamageMax: 1})
	if err != nil {
		t.Fatalf("marshal attack: %v", err)
	}

	out, err := Defend(self, attack)
	if err != nil {
		t.Fatalf("Defend: %v", err)
	}
	got := defendResult(t, out)
	if got.Self.Fleeing {
		t.Error("expected Fleeing to remain false while HP stays above the threshold")
	}
}

func TestDefendDoesNotResetFleeRoomIDWhenAlreadyFleeing(t *testing.T) {
	self, err := json.Marshal(monster{
		Object:     overlay.Object{ID: "goblin:1", X: 6, Y: 6},
		HP:         2,
		MaxHP:      10,
		Fleeing:    true,
		FleeRoomID: 1,
	})
	if err != nil {
		t.Fatalf("marshal self: %v", err)
	}
	attack, err := json.Marshal(action.Result{Action: "attack", Weapon: "hands", DamageMin: 1, DamageMax: 1})
	if err != nil {
		t.Fatalf("marshal attack: %v", err)
	}

	out, err := Defend(self, attack)
	if err != nil {
		t.Fatalf("Defend: %v", err)
	}
	got := defendResult(t, out)
	if !got.Self.Fleeing {
		t.Error("expected still fleeing")
	}
	if got.Self.FleeRoomID != 1 {
		t.Errorf("expected FleeRoomID to remain 1 (destination committed), got %d", got.Self.FleeRoomID)
	}
}

func TestDefendDoesNotSetFleeingOnKillingBlow(t *testing.T) {
	self, err := json.Marshal(monster{
		Object: overlay.Object{ID: "goblin:1", X: 6, Y: 6},
		HP:     1,
		MaxHP:  10,
	})
	if err != nil {
		t.Fatalf("marshal self: %v", err)
	}
	attack, err := json.Marshal(action.Result{Action: "attack", Weapon: "hands", DamageMin: 5, DamageMax: 5})
	if err != nil {
		t.Fatalf("marshal attack: %v", err)
	}

	out, err := Defend(self, attack)
	if err != nil {
		t.Fatalf("Defend: %v", err)
	}
	got := defendResult(t, out)
	if !got.Self.Dead {
		t.Fatal("test setup: expected the goblin dead")
	}
	if got.Self.Fleeing {
		t.Error("expected Fleeing not set on a killing blow")
	}
}

// TestPickFleeRoomAndCellChoosesRoomOtherThanPlayersRoom exercises
// pickFleeRoomAndCell directly (white-box, same package) rather than
// through Action — threeRoomFleeMap has no corridors between its rooms,
// so a full Action/fleeStep call would immediately find no route and
// give up before this test could observe what got picked; the picking
// logic itself doesn't need connectivity, only aug.Map.Rooms.
func TestPickFleeRoomAndCellChoosesRoomOtherThanPlayersRoom(t *testing.T) {
	m := threeRoomFleeMap()
	playerX, playerY := 7, 7 // inside room 0

	seenB, seenC := false, false
	for i := 0; i < 200; i++ {
		room, cellX, cellY, ok := pickFleeRoomAndCell(m, playerX, playerY)
		if !ok {
			t.Fatalf("iteration %d: expected ok=true", i)
		}
		switch room.ID {
		case 0:
			t.Fatalf("iteration %d: picked the Player's own room", i)
		case 1:
			seenB = true
		case 2:
			seenC = true
		default:
			t.Fatalf("iteration %d: picked an unexpected room %d", i, room.ID)
		}
		if cellX < room.X || cellX >= room.X+room.W || cellY < room.Y || cellY >= room.Y+room.H {
			t.Fatalf("iteration %d: cell (%d,%d) outside the picked room %+v", i, cellX, cellY, room)
		}
	}
	if !seenB || !seenC {
		t.Errorf("expected both non-Player rooms to be picked at least once across 200 iterations, seenB=%v seenC=%v", seenB, seenC)
	}
}

func TestActionFleeIgnoresPlayerVisibility(t *testing.T) {
	m := testMap()
	self := monster{
		Object:     overlay.Object{ID: "goblin:1", X: 6, Y: 6},
		HP:         1,
		MaxHP:      10,
		Aware:      true,
		Fleeing:    true,
		FleeRoomID: 1,
		FleeX:      32,
		FleeY:      7,
	}
	p := overlay.Object{ID: "player:1", X: 6, Y: 7, Invisible: true}

	out, err := Action(selfJSONFrom(t, self), augmentedJSON(t, m, p))
	if err != nil {
		t.Fatalf("Action: %v", err)
	}
	got := actionResult(t, out)
	if got.Self.X == 6 && got.Self.Y == 6 {
		t.Error("expected the fleeing goblin to move despite the Player being invisible")
	}
	if !got.Self.Fleeing {
		t.Error("expected still fleeing (hasn't reached room 1 yet)")
	}
}

func TestActionFleeContinuesAcrossMultipleTurnsUntilArrival(t *testing.T) {
	m := testMap()
	self := monster{
		Object:     overlay.Object{ID: "goblin:1", X: 6, Y: 6},
		HP:         1,
		MaxHP:      10,
		Fleeing:    true,
		FleeRoomID: 1,
		FleeX:      32,
		FleeY:      7,
	}
	p := overlay.Object{ID: "player:1", X: 6, Y: 7}
	selfJ := selfJSONFrom(t, self)

	var last monster
	reachedSafety := false
	for turn := 0; turn < 40; turn++ {
		out, err := Action(selfJ, augmentedJSON(t, m, p))
		if err != nil {
			t.Fatalf("turn %d: Action: %v", turn, err)
		}
		got := actionResult(t, out)
		last = got.Self
		selfJ = selfJSONFrom(t, got.Self)
		if !got.Self.Fleeing {
			reachedSafety = true
			break
		}
	}
	if !reachedSafety {
		t.Fatalf("expected the goblin to stop fleeing within 40 turns, ended at (%d,%d) fleeing=%v", last.X, last.Y, last.Fleeing)
	}
	if last.X != 32 || last.Y != 7 {
		t.Errorf("expected the goblin to have arrived exactly at its chosen cell (32,7), got (%d,%d)", last.X, last.Y)
	}
	if last.Aware {
		t.Error("expected Aware reset to false after reaching safety")
	}
}

func TestActionFleeStopsAndResetsAwareOnReachingDestination(t *testing.T) {
	m := testMap()
	// Already standing exactly on its own chosen flee cell.
	self := monster{
		Object:     overlay.Object{ID: "goblin:1", X: 32, Y: 7},
		HP:         1,
		MaxHP:      10,
		Aware:      true,
		Fleeing:    true,
		FleeRoomID: 1,
		FleeX:      32,
		FleeY:      7,
	}
	p := overlay.Object{ID: "player:1", X: 6, Y: 6}

	out, err := Action(selfJSONFrom(t, self), augmentedJSON(t, m, p))
	if err != nil {
		t.Fatalf("Action: %v", err)
	}
	got := actionResult(t, out)
	if got.Self.Fleeing {
		t.Error("expected fleeing to end upon already being in the destination room")
	}
	if got.Self.Aware {
		t.Error("expected Aware reset to false")
	}
}

func TestActionFleeEndsWhenHPAboveThreshold(t *testing.T) {
	m := testMap()
	self := monster{
		Object:     overlay.Object{ID: "goblin:1", X: 6, Y: 6},
		HP:         10,
		MaxHP:      10,
		Aware:      true,
		Fleeing:    true,
		FleeRoomID: 1,
	}
	p := overlay.Object{ID: "player:1", X: 20, Y: 9} // in the corridor, not sharing a room

	out, err := Action(selfJSONFrom(t, self), augmentedJSON(t, m, p))
	if err != nil {
		t.Fatalf("Action: %v", err)
	}
	got := actionResult(t, out)
	if got.Self.Fleeing {
		t.Error("expected fleeing to end once HP is above the threshold")
	}
	if got.Self.Aware {
		t.Error("expected Aware reset to false")
	}
}

// TestActionFleeLandsOnVariousCellsNotJustRoomCenter runs many full flee
// episodes (auto-picking a destination, then walking multi-turn until
// arrival, exactly as real play would) and confirms the final landing
// position varies across runs and isn't always the room's exact center —
// the specific behavior requested: the goblin commits to a random
// interior cell, not just the middle of the room.
func TestFleeStepSlidesOnOilyDestination(t *testing.T) {
	m := testMap()
	self := monster{
		Object: overlay.Object{ID: "goblin:1", X: 6, Y: 6},
		HP:     5, MaxHP: 10,
		Fleeing: true, FleeRoomID: 0, FleeX: 12, FleeY: 6,
	}
	// The first flee step from (6,6) toward (12,6) is (7,6), well inside
	// the room's open interior (5-14,5-14) — every one of the 4 slide
	// candidates is legal and unobstructed.
	aug := world.AugmentedMap{Map: m, OilyTiles: []mapdata.Point{{X: 7, Y: 6}}}

	seen := map[mapdata.Point]bool{}
	for i := 0; i < 400; i++ {
		out, err := fleeStep(self, aug)
		if err != nil {
			t.Fatalf("fleeStep: %v", err)
		}
		got := actionResult(t, out)
		pos := mapdata.Point{X: got.Self.X, Y: got.Self.Y}
		switch pos {
		case mapdata.Point{X: 7, Y: 6}, mapdata.Point{X: 8, Y: 6}, mapdata.Point{X: 7, Y: 5}, mapdata.Point{X: 7, Y: 7}:
		default:
			t.Fatalf("unexpected resulting position %+v", pos)
		}
		seen[pos] = true
	}
	if len(seen) < 2 {
		t.Errorf("expected the oily destination to sometimes divert the fleeing goblin elsewhere across 400 attempts, only ever saw %+v", seen)
	}
}

func TestActionFleeLandsOnVariousCellsNotJustRoomCenter(t *testing.T) {
	m := testMap()
	p := overlay.Object{ID: "player:1", X: 6, Y: 6} // inside room 0

	room1, ok := m.RoomByID(1)
	if !ok {
		t.Fatal("test setup: expected room 1 to exist")
	}
	centerX, centerY := room1.CenterX(), room1.CenterY()

	landings := map[[2]int]bool{}
	for i := 0; i < 100; i++ {
		self := monster{
			Object:     overlay.Object{ID: "goblin:1", X: 6, Y: 7},
			HP:         1,
			MaxHP:      10,
			Fleeing:    true,
			FleeRoomID: -1,
		}
		selfJ := selfJSONFrom(t, self)
		var last monster
		arrived := false
		for turn := 0; turn < 40; turn++ {
			out, err := Action(selfJ, augmentedJSON(t, m, p))
			if err != nil {
				t.Fatalf("iteration %d turn %d: Action: %v", i, turn, err)
			}
			got := actionResult(t, out)
			last = got.Self
			selfJ = selfJSONFrom(t, got.Self)
			if !got.Self.Fleeing {
				arrived = true
				break
			}
		}
		if !arrived {
			t.Fatalf("iteration %d: expected the goblin to stop fleeing within 40 turns, ended at (%d,%d)", i, last.X, last.Y)
		}
		if r, ok := m.RoomAt(last.X, last.Y); !ok || r.ID != 1 {
			t.Fatalf("iteration %d: expected the goblin to land in room 1, got (%d,%d)", i, last.X, last.Y)
		}
		landings[[2]int{last.X, last.Y}] = true
	}

	if len(landings) < 2 {
		t.Errorf("expected landing positions to vary across repeated flee episodes, got only %v", landings)
	}
	sawNonCenter := false
	for pos := range landings {
		if pos[0] != centerX || pos[1] != centerY {
			sawNonCenter = true
		}
	}
	if !sawNonCenter {
		t.Error("expected at least one flee episode to land somewhere other than the room's exact center")
	}
}

func TestActionFleeFallsBackToNormalWhenNoOtherRoomExists(t *testing.T) {
	room := mapdata.Room{ID: 0, X: 5, Y: 5, W: 10, H: 10}
	m := mapdata.Map{Width: 20, Height: 20, Rooms: []mapdata.Room{room}}
	self := monster{
		Object:     overlay.Object{ID: "goblin:1", X: 6, Y: 6},
		HP:         1,
		MaxHP:      10,
		Fleeing:    true,
		FleeRoomID: -1,
	}
	p := overlay.Object{ID: "player:1", X: 10, Y: 10}

	out, err := Action(selfJSONFrom(t, self), augmentedJSON(t, m, p))
	if err != nil {
		t.Fatalf("Action: %v", err)
	}
	got := actionResult(t, out)
	if got.Self.Fleeing {
		t.Error("expected fleeing to be abandoned when no other room exists to flee to")
	}
}

func TestActionFleeAttacksIfPathBlockedByPlayer(t *testing.T) {
	m := testMap()
	self := monster{
		Object:     overlay.Object{ID: "goblin:1", X: 6, Y: 6},
		HP:         1,
		MaxHP:      10,
		Fleeing:    true,
		FleeRoomID: 1,
		FleeX:      32,
		FleeY:      7,
	}
	nx, ny, ok := m.NextStep(self.X, self.Y, self.FleeX, self.FleeY)
	if !ok {
		t.Fatal("test setup: expected a route from the goblin toward its flee cell")
	}
	p := overlay.Object{ID: "player:1", X: nx, Y: ny}

	out, err := Action(selfJSONFrom(t, self), augmentedJSON(t, m, p))
	if err != nil {
		t.Fatalf("Action: %v", err)
	}
	got := actionResult(t, out)
	if got.Result.Action != "attack" {
		t.Errorf("expected an attack result when the flee path is blocked by the Player, got %+v", got.Result)
	}
	if got.Self.X != 6 || got.Self.Y != 6 {
		t.Errorf("expected the goblin to stay put rather than move onto the Player's tile, got (%d,%d)", got.Self.X, got.Self.Y)
	}
}

func TestActionDoesNotBecomeAwareOfSneakingPlayerOnThreshold(t *testing.T) {
	m := testMap()
	p := overlay.Object{ID: "player:1", X: 15, Y: 9, Sneaking: true} // room A's door threshold

	out, err := Action(selfJSON(t, "goblin:1", 6, 6, false), augmentedJSON(t, m, p))
	if err != nil {
		t.Fatalf("Action: %v", err)
	}
	got := actionResult(t, out)
	if got.Self.Aware {
		t.Error("expected the goblin to remain unaware of a sneaking Player standing only on the threshold")
	}
	if got.Self.X != 6 || got.Self.Y != 6 {
		t.Errorf("expected the goblin to stay put, got (%d,%d)", got.Self.X, got.Self.Y)
	}
}

func TestActionBecomesAwareOfNonSneakingPlayerOnThreshold(t *testing.T) {
	m := testMap()
	p := overlay.Object{ID: "player:1", X: 15, Y: 9} // Sneaking left false

	out, err := Action(selfJSON(t, "goblin:1", 6, 6, false), augmentedJSON(t, m, p))
	if err != nil {
		t.Fatalf("Action: %v", err)
	}
	got := actionResult(t, out)
	if !got.Self.Aware {
		t.Error("expected the goblin to become aware of a non-sneaking Player standing on the threshold — regression pin for the original doorway-detection fix")
	}
}

func TestActionBecomesAwareOfSneakingPlayerFullyInsideRoom(t *testing.T) {
	m := testMap()
	p := overlay.Object{ID: "player:1", X: 7, Y: 7, Sneaking: true} // well inside room A, not a threshold

	out, err := Action(selfJSON(t, "goblin:1", 6, 6, false), augmentedJSON(t, m, p))
	if err != nil {
		t.Fatalf("Action: %v", err)
	}
	got := actionResult(t, out)
	if !got.Self.Aware {
		t.Error("expected sneaking to only protect the exact threshold tile — stepping fully into the room should still be detected")
	}
}

func TestActionKeepsChasingAlreadyAwareSneakingPlayerOnThreshold(t *testing.T) {
	m := testMap()
	self := selfJSON(t, "goblin:1", 6, 6, true) // already aware
	p := overlay.Object{ID: "player:1", X: 15, Y: 9, Sneaking: true}

	out, err := Action(self, augmentedJSON(t, m, p))
	if err != nil {
		t.Fatalf("Action: %v", err)
	}
	got := actionResult(t, out)
	if !got.Self.Aware {
		t.Error("expected the goblin to remain aware")
	}
	if got.Self.X == 6 && got.Self.Y == 6 {
		t.Error("expected an already-aware goblin to keep closing in on a sneaking Player standing on a threshold")
	}
}
