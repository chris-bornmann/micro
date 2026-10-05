package content

// levels/*.yaml: special levels (see rules.LevelSpec). Each file holds one
// or more, by name:
//
//	throne_room:
//	  depth: 10               # or depths: [first, last] — one is picked
//	  chance: 100             # percent of games that have it (default 100)
//	  map: |                  # a hand-drawn level (see levels.Parse)
//	    ...
//	  legend: {K: king}       # what the drawing's other characters place
//
//	  populate: [monsters]    # also random monsters by depth (true: all
//	                          # of monsters, items, traps; default none)
//
//	maze:
//	  depths: [4, 8]
//	  generator: maze         # or a generated one
//	  braid: 10               # (populate defaults to true here)

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	"go.yaml.in/yaml/v3"

	"micro/server/internal/levels"
	"micro/server/internal/rules"
)

// levelsDir is where special levels live, under the content directory.
const levelsDir = "levels"

type levelYAML struct {
	Depth     *int              `yaml:"depth"`
	Depths    *rangeYAML        `yaml:"depths"`
	Chance    *int              `yaml:"chance"`
	Map       string            `yaml:"map"`
	Legend    map[string]string `yaml:"legend"`
	Generator string            `yaml:"generator"`
	Braid     *int              `yaml:"braid"`
	Populate  *populateYAML     `yaml:"populate"`
	line      int
}

// populateYAML is true (everything), false (nothing), or a list of
// monsters, items and traps.
type populateYAML struct {
	p    rules.Populate
	line int
}

func (y *populateYAML) UnmarshalYAML(n *yaml.Node) error {
	y.line = n.Line
	var all bool
	if n.Kind == yaml.ScalarNode && n.Decode(&all) == nil {
		if all {
			y.p = rules.PopulateAll
		}
		return nil
	}
	var kinds []string
	if err := n.Decode(&kinds); err != nil {
		return fmt.Errorf("line %d: populate is true, false, or a list of monsters, items and traps", n.Line)
	}
	for _, k := range kinds {
		switch k {
		case "monsters":
			y.p.Monsters = true
		case "items":
			y.p.Items = true
		case "traps":
			y.p.Traps = true
		default:
			return fmt.Errorf("line %d: populate: unknown %q (known: monsters, items, traps)", n.Line, k)
		}
	}
	return nil
}

func (l *levelYAML) UnmarshalYAML(n *yaml.Node) error {
	type plain levelYAML
	if err := strictDecode(n, (*plain)(l)); err != nil {
		return err
	}
	l.line = n.Line
	return nil
}

// loadLevels reads and checks the special levels in files against cat,
// sorted by name.
func loadLevels(files []yamlFile, cat *rules.Catalog) ([]*rules.LevelSpec, error) {
	var specs []*rules.LevelSpec
	var errs []error
	seen := map[string]string{}
	for _, f := range files {
		var raw map[string]*levelYAML
		if err := yaml.Unmarshal(f.data, &raw); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", f.name, err))
			continue
		}
		for name, y := range raw {
			bad := func(format string, args ...any) {
				line := 0
				if y != nil {
					line = y.line
				}
				errs = append(errs, fmt.Errorf("%s:%d: level %s: %s", f.name, line, name, fmt.Sprintf(format, args...)))
			}
			if prev, dup := seen[name]; dup {
				bad("already defined in %s", prev)
				continue
			}
			seen[name] = f.name
			if y == nil {
				bad("empty level")
				continue
			}
			if spec, ok := buildLevel(name, y, cat, bad); ok {
				specs = append(specs, spec)
			}
		}
	}
	sort.Slice(specs, func(i, j int) bool { return specs[i].Name < specs[j].Name })
	return specs, errors.Join(errs...)
}

// buildLevel checks one level and builds its spec.
func buildLevel(name string, y *levelYAML, cat *rules.Catalog, bad func(string, ...any)) (*rules.LevelSpec, bool) {
	spec := &rules.LevelSpec{Name: name, Map: y.Map, Generator: y.Generator, Chance: 100, Braid: 10, Legend: map[rune]string{}}
	ok := true
	fail := func(format string, args ...any) { bad(format, args...); ok = false }

	switch {
	case (y.Depth == nil) == (y.Depths == nil):
		fail("give depth: n, or depths: [first, last]")
	case y.Depth != nil:
		spec.Depths = rules.Range{Min: *y.Depth, Max: *y.Depth}
	default:
		spec.Depths = y.Depths.r
	}
	if spec.Depths.Min < 1 || spec.Depths.Max > rules.DeepestLevel {
		fail("depths must lie within 1-%d", rules.DeepestLevel)
	}
	if y.Chance != nil {
		spec.Chance = *y.Chance
		if spec.Chance < 0 || spec.Chance > 100 {
			fail("chance must be 0-100, got %d", spec.Chance)
		}
	}
	if y.Braid != nil {
		spec.Braid = *y.Braid
	}
	// A drawn level has only what it places, unless it asks for more; a
	// generated one is populated like any level, unless it asks for less.
	switch {
	case y.Populate != nil:
		spec.Populate = y.Populate.p
	case y.Generator != "":
		spec.Populate = rules.PopulateAll
	}

	switch {
	case (y.Map == "") == (y.Generator == ""):
		fail("give a map: (drawn by hand) or a generator:, not both")
	case y.Generator != "" && y.Generator != "maze":
		fail("unknown generator %q (known: maze)", y.Generator)
	case y.Generator != "" && len(y.Legend) > 0:
		fail("a legend is for a drawn map")
	}
	for k, id := range y.Legend {
		r, size := utf8.DecodeRuneInString(k)
		t := cat.Get(id)
		switch {
		case size != len(k) || k == "":
			fail("legend: %q must be one character", k)
		case strings.ContainsRune(".:#+S, ", r):
			fail("legend: %q already means something in a drawing", k)
		case t == nil:
			fail("legend: %s: no template named %q", k, id)
		case t.Category == rules.CategoryPlayer || t.Category == rules.CategoryCorpse:
			fail("legend: %s: a %s can't be placed", k, t.Category)
		default:
			spec.Legend[r] = id
		}
	}
	if ok && spec.Map != "" {
		if _, err := levels.Parse(spec.Map, spec.Legend); err != nil {
			fail("map: %v", err)
		}
	}
	return spec, ok
}
