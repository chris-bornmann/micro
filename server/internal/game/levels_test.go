package game

import (
	"strconv"
	"strings"
	"testing"

	"micro/server/internal/rules"
)

func countKind(ls *levelState, id string) int {
	n := 0
	for _, m := range ls.Monsters {
		if m.T.ID == id {
			n++
		}
	}
	return n
}

func TestPopulateLevelSpawnsExpectedItemsAndStairs(t *testing.T) {
	ls, err := populateLevel(testEngine(t), testLevelConfig(), 1, map[string]bool{})
	if err != nil {
		t.Fatalf("populateLevel: %v", err)
	}

	got := map[string]int{}
	for _, it := range ls.FloorItems {
		if it.T.Category != rules.CategoryTrap { // traps vary by level: see their own tests
			got[it.T.ID]++
		}
	}
	want := map[string]int{
		"healing_potion": 1, "invisibility_potion": 1, "fine_dagger": 1, "iron_gloves": 1, "torch": 1,
		"teleport_scroll": 1, "awareness_scroll": 1, "summon_scroll": 1, "knowledge_scroll": 1, "ring_of_true_sight": 1, "ring_of_regeneration": 1, "amulet_of_wasting": 1,
		"leather_armor": 1, "chain_mail": 1, "cursed_mail": 1, "helm": 1, "cursed_helm": 1, "gauntlets": 1, "small_shield": 1, "cloak_of_protection": 1, "shoes_of_sneaking": 1, "flask_of_oil": 1, "stairs_up": 1, "stairs_down": 1,
	}
	if len(got) != len(want) {
		t.Errorf("floor items %v, want %v", got, want)
	}
	for id, n := range want {
		if got[id] != n {
			t.Errorf("%d %s on the floor, want %d", got[id], id, n)
		}
	}
	for _, it := range ls.FloorItems {
		if _, ok := ls.Map.RoomAt(it.X, it.Y); !ok {
			t.Errorf("%s spawned outside every room at (%d,%d)", it.Name, it.X, it.Y)
		}
	}
}

// TestPopulateLevelIsReproducible: the same seed builds the same level,
// down to where everything lies and what it rolled.
func TestPopulateLevelIsReproducible(t *testing.T) {
	a, _ := populateLevel(testEngine(t), testLevelConfig(), 1, map[string]bool{})
	b, _ := populateLevel(testEngine(t), testLevelConfig(), 1, map[string]bool{})
	describe := func(ls *levelState) string {
		var parts []string
		for _, e := range append(append([]*rules.Entity{}, ls.Monsters...), ls.FloorItems...) {
			parts = append(parts, strings.Join([]string{e.T.ID, strconv.Itoa(e.X), strconv.Itoa(e.Y), strconv.Itoa(e.HP)}, ","))
			for _, u := range e.Uses {
				for _, k := range []string{"amount", "turns"} {
					parts = append(parts, strconv.Itoa(u.Params.Int(k)))
				}
			}
		}
		return strings.Join(parts, " ")
	}
	if describe(a) != describe(b) {
		t.Errorf("same seed, different levels:\n%s\n%s", describe(a), describe(b))
	}
}

// TestPopulateLevelArmsTheFirstTwoGoblins: equipment fills slots in
// order, so the first goblin on a level gets the rusty sword, the second
// the poisoned dagger, and the rest fight bare-handed.
func TestPopulateLevelArmsTheFirstTwoGoblins(t *testing.T) {
	want := []string{"rusty sword", "poisoned dagger"}
	for seed := int64(1); seed <= 20; seed++ {
		cfg := testLevelConfig()
		cfg.Seed = seed
		ls, _ := populateLevel(testEngine(t), cfg, 1, map[string]bool{})
		n := 0
		for _, m := range ls.Monsters {
			if m.T.ID != "goblin" {
				continue
			}
			got := ""
			if w := m.Weapon(); w != nil {
				got = w.Name
			}
			exp := ""
			if n < len(want) {
				exp = want[n]
			}
			if got != exp {
				t.Errorf("seed %d: goblin #%d wields %q, want %q", seed, n+1, got, exp)
			}
			n++
		}
	}
}

func TestPopulateLevelNeverSpawnsMummyBeforeItsEarliestLevel(t *testing.T) {
	spawnedOnce := map[string]bool{}
	for level := 1; level < 3; level++ {
		cfg := testLevelConfig()
		cfg.Seed += int64(level) // vary the map/rng across levels, like a real run would
		ls, err := populateLevel(testEngine(t), cfg, level, spawnedOnce)
		if err != nil {
			t.Fatalf("populateLevel(level %d): %v", level, err)
		}
		if countKind(ls, "mummy") > 0 {
			t.Errorf("expected no mummy before its earliest level, found one on level %d", level)
		}
	}
}

func TestPopulateLevelMarksSpawnedOnceWhenAMummyAppears(t *testing.T) {
	spawnedOnce := map[string]bool{}
	ls, err := populateLevel(testEngine(t), testLevelConfig(), 8, spawnedOnce)
	if err != nil {
		t.Fatalf("populateLevel: %v", err)
	}
	if countKind(ls, "mummy") != 1 {
		t.Fatal("expected exactly one guaranteed mummy at its force-by depth")
	}
	if !spawnedOnce["mummy"] {
		t.Error("expected spawnedOnce[\"mummy\"] to be set once a mummy actually spawned")
	}
}

func TestPopulateLevelNeverSpawnsAnotherMummyOnceSpawnedOnce(t *testing.T) {
	ls, err := populateLevel(testEngine(t), testLevelConfig(), 8, map[string]bool{"mummy": true})
	if err != nil {
		t.Fatalf("populateLevel: %v", err)
	}
	if countKind(ls, "mummy") > 0 {
		t.Error("expected no further mummy once one has already spawned this run")
	}
}

func TestPopulateLevelGoblinCountVariesAcrossLevels(t *testing.T) {
	if testing.Short() {
		t.Skip("seed sweep")
	}
	counts := map[int]bool{}
	for seed := int64(1); seed <= 40; seed++ {
		cfg := testLevelConfig()
		cfg.Seed = seed
		ls, err := populateLevel(testEngine(t), cfg, 1, map[string]bool{})
		if err != nil {
			t.Fatalf("populateLevel: %v", err)
		}
		counts[countKind(ls, "goblin")] = true
	}
	if len(counts) < 2 {
		t.Errorf("expected goblin count to vary across seeds, always got %+v", counts)
	}
}

// TestPhantomsAreRareButCanComeInNumbers: at its own level a phantom
// usually appears alone or not at all, now and then with company.
func TestPhantomsAreRareButCanComeInNumbers(t *testing.T) {
	if testing.Short() {
		t.Skip("seed sweep")
	}
	total, crowded, empty := 0, 0, 0
	for seed := int64(1); seed <= 40; seed++ {
		cfg := testLevelConfig()
		cfg.Seed = seed
		ls, err := populateLevel(testEngine(t), cfg, 3, map[string]bool{})
		if err != nil {
			t.Fatalf("populateLevel: %v", err)
		}
		n := countKind(ls, "phantom")
		total += n
		if n > 1 {
			crowded++
		}
		if n == 0 {
			empty++
		}
	}
	if total == 0 || crowded == 0 || empty == 0 {
		t.Errorf("over 40 level-3 levels: %d phantoms, %d levels with several, %d with none", total, crowded, empty)
	}
	if avg := float64(total) / 40; avg >= 2 {
		t.Errorf("phantoms should be rare: %.1f per level", avg)
	}
}

func TestPopulateLevelOmitsStairsDownOnTheLastLevel(t *testing.T) {
	ls, err := populateLevel(testEngine(t), testLevelConfig(), maxLevels, map[string]bool{})
	if err != nil {
		t.Fatalf("populateLevel: %v", err)
	}
	if stairsAmong(ls.FloorItems, "up") == nil {
		t.Error("expected stairs up even on the last level")
	}
	if stairsAmong(ls.FloorItems, "down") != nil {
		t.Error("expected no stairs down on the last level")
	}
}

func TestApplyStairsTransitionDescendsGeneratesAndStoresLevel2(t *testing.T) {
	state := newLevel1TestState(t)

	if msg := mustKey(t, state, ">"); !strings.Contains(msg, "descend to level 2") {
		t.Errorf("expected a 'descend to level 2' message, got %q", msg)
	}
	if state.CurrentLevel != 2 || state.Levels[1] == nil || state.levelState != state.Levels[1] {
		t.Fatalf("expected to be on a stored level 2, CurrentLevel %d", state.CurrentLevel)
	}
	up := stairsAmong(state.FloorItems, "up")
	if up == nil || up.X != state.Player.X || up.Y != state.Player.Y {
		t.Error("expected the Player to land exactly on level 2's up-stairs")
	}
}

func TestApplyStairsTransitionAscendRestoresTheExactStoredLevel(t *testing.T) {
	state := newLevel1TestState(t)
	level1 := state.levelState

	mustKey(t, state, ">")
	if msg := mustKey(t, state, "<"); !strings.Contains(msg, "ascend to level 1") {
		t.Errorf("expected an 'ascend to level 1' message, got %q", msg)
	}
	if state.CurrentLevel != 1 || state.levelState != level1 {
		t.Error("expected ascending back to level 1 to restore the exact same stored levelState")
	}
}

func TestApplyStairsTransitionRevisitingReturnsTheSameStoredLevel(t *testing.T) {
	state := newLevel1TestState(t)
	mustKey(t, state, ">")
	level2 := state.levelState
	mustKey(t, state, "<")
	mustKey(t, state, ">")
	if state.levelState != level2 {
		t.Error("expected revisiting level 2 to return the exact same stored levelState, not regenerate it")
	}
}

func TestApplyStairsTransitionUpFromLevel1EndsTheGame(t *testing.T) {
	state := newLevel1TestState(t)
	if err := positionPlayerAtStairs(state, "up"); err != nil {
		t.Fatalf("positionPlayerAtStairs: %v", err)
	}

	if msg := mustKey(t, state, "<"); !strings.Contains(msg, "leave the dungeon") {
		t.Errorf("expected a 'leave the dungeon' message, got %q", msg)
	}
	if !state.Player.Dead {
		t.Error("expected leaving the dungeon to end the game")
	}
	if again := mustKey(t, state, "."); again != "Game Over" {
		t.Errorf("expected subsequent keys to return \"Game Over\", got %q", again)
	}
}

// TestMonstersNeverStartOnEachOtherOrTheStairs, on crowded early levels
// and deeper ones alike.
func TestMonstersNeverStartOnEachOtherOrTheStairs(t *testing.T) {
	if testing.Short() {
		t.Skip("seed sweep")
	}
	for seed := int64(1); seed <= 20; seed++ {
		for level := 1; level <= 10; level++ {
			cfg := testLevelConfig()
			cfg.Seed = seed
			ls, err := populateLevel(testEngine(t), cfg, level, map[string]bool{})
			if err != nil {
				t.Fatalf("populateLevel: %v", err)
			}
			taken := map[[2]int]string{}
			for _, s := range []string{"up", "down"} {
				if st := stairsAmong(ls.FloorItems, s); st != nil {
					taken[[2]int{st.X, st.Y}] = "stairs " + s
				}
			}
			for _, m := range ls.Monsters {
				k := [2]int{m.X, m.Y}
				if other, ok := taken[k]; ok {
					t.Fatalf("seed %d level %d: %s at %v shares it with %s", seed, level, m.ID, k, other)
				}
				taken[k] = m.ID
			}
		}
	}
}

func TestThePlayerNeverStartsOnAMonster(t *testing.T) {
	for seed := int64(1); seed <= 40; seed++ {
		cfg := testLevelConfig()
		cfg.Seed = seed
		g, err := NewGame(cfg, false, false, false)
		if err != nil {
			t.Fatalf("NewGame: %v", err)
		}
		if m := g.state.monsterAt(g.state.Player.X, g.state.Player.Y); m != nil {
			t.Errorf("seed %d: the Player starts on %s", seed, m.ID)
		}
	}
}

// TestAMonsterOnTheStairsStepsAsideForTheArrivingPlayer: back on a level
// already visited, a monster may be standing on the stairs.
func TestAMonsterOnTheStairsStepsAsideForTheArrivingPlayer(t *testing.T) {
	state := newLevel1TestState(t)
	mustKey(t, state, ">")
	mustKey(t, state, "<") // back on level 1, standing on its down-stairs
	down := stairsAmong(state.FloorItems, "down")

	g := testMonster(t, "goblin", down.X, down.Y, 10)
	state.Monsters = append(state.Monsters, g)
	state.Levels[0] = state.levelState
	if err := positionPlayerAtStairs(state, "down"); err != nil {
		t.Fatal(err)
	}
	if g.X == down.X && g.Y == down.Y {
		t.Fatal("the goblin should have stepped off the stairs")
	}
	if d := abs(g.X-down.X) + abs(g.Y-down.Y); d > 2 {
		t.Errorf("stepped %d tiles away; it should take the nearest free tile", d)
	}
	if other := state.monsterAt(g.X, g.Y); other != g {
		t.Errorf("landed on %v", other)
	}
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

func TestSlimesTurnUpOnTheFirstFewLevels(t *testing.T) {
	if testing.Short() {
		t.Skip("seed sweep")
	}
	for level := 1; level <= 3; level++ {
		found := 0
		for seed := int64(1); seed <= 20; seed++ {
			cfg := testLevelConfig()
			cfg.Seed = seed
			ls, err := populateLevel(testEngine(t), cfg, level, map[string]bool{})
			if err != nil {
				t.Fatalf("populateLevel: %v", err)
			}
			found += countKind(ls, "slime")
		}
		if found == 0 {
			t.Errorf("no slimes on level %d in 20 seeds", level)
		}
	}
}
