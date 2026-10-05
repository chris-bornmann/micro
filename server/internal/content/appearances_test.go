package content

import (
	"strings"
	"testing"

	"micro/server/internal/rules/builtin"
)

const pools = `
ring:
  format: "{} ring"
  color: [1, 2, 3]
  looks: [jade, {name: agate, color: [9, 9, 9]}]
`

// ring is an item that can go unidentified, with extra appended to it.
func ring(extra string) string {
	return `
ring_x:
  category: ring
  name: ring of x
  glyph: "="
  color: [1, 1, 1]
  weight: 1
  slot: ring
  unidentified: {pool: ring, chance: 70}
` + extra
}

func TestTheRealAppearancesLoad(t *testing.T) {
	cat, err := Load(builtin.Registry())
	if err != nil {
		t.Fatal(err)
	}
	if p := cat.Pools["ring"]; p == nil || len(p.Looks) < 2 || p.Format != "{} ring" {
		t.Fatalf("ring pool %+v", p)
	}
	if u := cat.Get("ring_of_true_sight").Unidentified; u == nil || u.Pool != "ring" || u.Chance != 100 {
		t.Errorf("ring of true sight: %+v", u)
	}
	if cat.Get("torch").Unidentified != nil {
		t.Error("a torch is always known")
	}
}

func TestAPoolLoads(t *testing.T) {
	cat := mustLoad(t, map[string]string{"player.yaml": base, "appearances.yaml": pools, "items.yaml": ring("")})
	p := cat.Pools["ring"]
	if len(p.Looks) != 2 || p.Looks[0].Color.R != 1 || p.Looks[1].Color.R != 9 {
		t.Errorf("looks %+v: jade takes the pool's color, agate its own", p.Looks)
	}
	if u := cat.Get("ring_x").Unidentified; u.Chance != 70 {
		t.Errorf("chance %d", u.Chance)
	}
}

func TestUnidentifiedMistakes(t *testing.T) {
	monster := strings.Replace(base, "  weight: 100\n", "  weight: 100\n  unidentified: {pool: ring}\n", 1)
	for name, tc := range map[string]struct {
		files map[string]string
		want  string
	}{
		"unknown pool": {map[string]string{"p.yaml": base, "appearances.yaml": pools, "i.yaml": strings.Replace(ring(""), "pool: ring", "pool: gem", 1)}, `unknown pool "gem"`},
		"pool too small": {map[string]string{"p.yaml": base, "appearances.yaml": pools,
			"i.yaml": ring("") + strings.ReplaceAll(strings.ReplaceAll(ring(""), "ring_x", "ring_y"), "of x", "of y") + strings.ReplaceAll(strings.ReplaceAll(ring(""), "ring_x", "ring_z"), "of x", "of z")},
			"pool ring has 2 looks for 3 item types"},
		"not an item": {map[string]string{"p.yaml": monster, "appearances.yaml": pools}, "only items can be unidentified"},
		"bad chance":  {map[string]string{"p.yaml": base, "appearances.yaml": pools, "i.yaml": strings.Replace(ring(""), "chance: 70", "chance: 101", 1)}, "chance must be 0-100"},
		"gives itself away": {map[string]string{"p.yaml": base, "appearances.yaml": pools,
			"i.yaml": ring("  messages:\n    you_hit: [\"Your ring of x glows!\"]\n")}, "gives away what it is"},
		"no format":   {map[string]string{"p.yaml": base, "appearances.yaml": "ring: {format: ring, color: [1, 1, 1], looks: [a]}\n"}, `format must contain "{}"`},
		"no color":    {map[string]string{"p.yaml": base, "appearances.yaml": "ring: {format: \"{} ring\", looks: [a]}\n"}, `look "a" needs a color`},
		"twice":       {map[string]string{"p.yaml": base, "appearances.yaml": "ring: {format: \"{} ring\", color: [1, 1, 1], looks: [a, a]}\n"}, `look "a" is listed twice`},
		"unknown key": {map[string]string{"p.yaml": base, "appearances.yaml": "ring: {format: \"{} ring\", colour: [1, 1, 1], looks: [a]}\n"}, `unknown field "colour"`},
	} {
		err := load(t, tc.files)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: want an error mentioning %q, got %v", name, tc.want, err)
		}
	}
}
