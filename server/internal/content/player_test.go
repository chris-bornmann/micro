package content

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"micro/server/internal/attributes"
	"micro/server/internal/rules"
	"micro/server/internal/rules/builtin"
)

// withPlayer is base with its attributes line replaced, and extra lines
// appended to the player.
func withPlayer(attrs, extra string) string {
	return strings.Replace(base, "  attributes: [6, 20]\n", "  attributes: "+attrs+"\n", 1) + extra
}

func TestAttributesMayBeSetOneByOne(t *testing.T) {
	fsys := map[string]string{"player.yaml": withPlayer("{strength: 18, intelligence: [6, 20], dexterity: 15, stamina: [3, 4]}", "")}
	cat := mustLoad(t, fsys)
	got := cat.Get("player").Attributes
	want := attributes.Ranges{
		Strength:     attributes.Range{Min: 18, Max: 18},
		Intelligence: attributes.Range{Min: 6, Max: 20},
		Dexterity:    attributes.Range{Min: 15, Max: 15},
		Stamina:      attributes.Range{Min: 3, Max: 4},
	}
	if got != want {
		t.Errorf("attributes %+v, want %+v", got, want)
	}
}

func TestAttributeMistakes(t *testing.T) {
	for attrs, want := range map[string]string{
		"{strength: 18, intelligence: 6, dexterity: 15}":                      "stamina is missing",
		"{strength: 18, intelligence: 6, dexterity: 15, stamina: 1, luck: 3}": `unknown attribute "luck"`,
		"{strength: [9, 2], intelligence: 6, dexterity: 15, stamina: 1}":      "line 9: range [9, 2] has max below min",
	} {
		err := load(t, map[string]string{"player.yaml": withPlayer(attrs, "")})
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: want an error mentioning %q, got %v", attrs, want, err)
		}
	}
}

func TestATraitMayLastAFewTurns(t *testing.T) {
	cat := mustLoad(t, map[string]string{"player.yaml": withPlayer("[6, 20]",
		"  traits:\n    - {trait: poisoned, damage: 2, duration: 5}\n    - {trait: sneaking}\n")})
	tr := cat.Get("player").Traits
	if len(tr) != 2 || tr[0].Duration != 5 || tr[0].Params.Int("damage") != 2 || tr[1].Duration != 0 {
		t.Errorf("traits %+v", tr)
	}
	for _, bad := range []string{"0", "-3", "soon"} {
		err := load(t, map[string]string{"player.yaml": withPlayer("[6, 20]", "  traits: [{trait: sneaking, duration: "+bad+"}]\n")})
		if err == nil || !strings.Contains(err.Error(), "duration must be") {
			t.Errorf("duration %s: %v", bad, err)
		}
	}
}

// TestTheExamplePlayerLoads: players/example.yaml, in place of the
// built-in player, with the rest of the real content. What it says is up
// to whoever is testing with it; only that it loads is checked.
func TestTheExamplePlayerLoads(t *testing.T) {
	cat, err := LoadWithPlayer(builtin.Registry(), filepath.Join("..", "..", "players", "example.yaml"))
	if err != nil {
		t.Fatalf("players/example.yaml: %v", err)
	}
	if p := cat.ByCategory(rules.CategoryPlayer); len(p) != 1 {
		t.Errorf("want one player, got %d", len(p))
	}
}

// TestAPlayerFileCannotRedefineTheGame: a hand-crafted player may add
// things, not replace what the built-in content defines.
func TestAPlayerFileCannotRedefineTheGame(t *testing.T) {
	file := filepath.Join(t.TempDir(), "p.yaml")
	body := base + "\ngoblin: {category: corpse, name: goblin, glyph: x, color: [1, 1, 1], weight: 1}\n"
	if err := os.WriteFile(file, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := LoadWithPlayer(builtin.Registry(), file)
	if err == nil || !strings.Contains(err.Error(), "goblin") || !strings.Contains(err.Error(), "already defined") {
		t.Errorf("want a duplicate goblin error, got %v", err)
	}
	if _, err := LoadWithPlayer(builtin.Registry(), filepath.Join(t.TempDir(), "missing.yaml")); err == nil {
		t.Error("a missing player file is an error")
	}
}

func mustLoad(t *testing.T, files map[string]string) *rules.Catalog {
	t.Helper()
	var all []yamlFile
	for name, body := range files {
		all = append(all, yamlFile{name: name, data: []byte(body), level: strings.HasPrefix(name, levelsDir+"/")})
	}
	cat, err := loadFiles(all, builtin.Registry())
	if err != nil {
		t.Fatal(err)
	}
	return cat
}

// TestRegenerationCannotRunBackwards: a negative multiplier is a mistake —
// wearing something away is what wasting is for.
func TestRegenerationCannotRunBackwards(t *testing.T) {
	err := load(t, map[string]string{"player.yaml": withPlayer("[6, 20]", "  traits: [{trait: regeneration, multiplier: -1}]\n")})
	if err == nil || !strings.Contains(err.Error(), "below the minimum 0") {
		t.Errorf("want a minimum error, got %v", err)
	}
}
