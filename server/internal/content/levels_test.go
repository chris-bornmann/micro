package content

import (
	"strings"
	"testing"

	"micro/server/internal/rules"
	"micro/server/internal/rules/builtin"
)

func TestTheRealLevelsLoad(t *testing.T) {
	cat, err := Load(builtin.Registry())
	if err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for _, l := range cat.Levels {
		names = append(names, l.Name)
	}
	if strings.Join(names, ",") != "maze,throne_room" {
		t.Errorf("levels %v", names)
	}
}

func TestLevelMistakes(t *testing.T) {
	drawn := "#####\n    #<.!#\n    #####\n"
	for name, tc := range map[string]struct{ level, want string }{
		"no depth":      {"x: {generator: maze}", "give depth: n, or depths"},
		"too deep":      {"x: {depth: 99, generator: maze}", "depths must lie within"},
		"both":          {"x: {depth: 2, generator: maze, map: \"#\"}", "not both"},
		"unknown gen":   {"x: {depth: 2, generator: cave}", `unknown generator "cave"`},
		"bad legend id": {"x:\n  depth: 2\n  map: |\n    " + drawn + "  legend: {\"!\": elixir}\n", `no template named "elixir"`},
		"bad drawing":   {"x:\n  depth: 2\n  map: |\n    " + drawn, `'!' is not in the legend`},
		"long key":      {"x:\n  depth: 2\n  map: |\n    " + drawn + "  legend: {ab: torch}\n", "must be one character"},
		"unknown field": {"x: {depth: 2, generator: maze, level: 3}", `unknown field "level"`},
	} {
		err := load(t, map[string]string{"player.yaml": base + "\ntorch: {category: torch, name: torch, glyph: /, color: [1, 1, 1], weight: 1}\n", "levels/x.yaml": tc.level})
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: want an error mentioning %q, got %v", name, tc.want, err)
		}
	}
}

func TestPopulateSaysWhatIsAlsoRandom(t *testing.T) {
	drawn := "x:\n  depth: 2\n  map: |\n    ###\n    #<#\n    ###\n"
	for extra, want := range map[string]rules.Populate{
		"":                                {},
		"  populate: true\n":              rules.PopulateAll,
		"  populate: false\n":             {},
		"  populate: [monsters, traps]\n": {Monsters: true, Traps: true},
	} {
		cat := mustLoad(t, map[string]string{"player.yaml": base, "levels/x.yaml": drawn + extra})
		if got := cat.Levels[0].Populate; got != want {
			t.Errorf("%q: %+v, want %+v", extra, got, want)
		}
	}
	cat := mustLoad(t, map[string]string{"player.yaml": base, "levels/m.yaml": "m: {depth: 2, generator: maze}\n"})
	if cat.Levels[0].Populate != rules.PopulateAll {
		t.Error("a maze is populated unless it says otherwise")
	}
	err := load(t, map[string]string{"player.yaml": base, "levels/x.yaml": drawn + "  populate: [gold]\n"})
	if err == nil || !strings.Contains(err.Error(), `unknown "gold"`) {
		t.Errorf("want an unknown-kind error, got %v", err)
	}
}
