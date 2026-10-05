package content

// appearances.yaml: the pools of looks unidentified items are dealt from
// (see rules.Identity). Unlike every other file here it holds pools, not
// templates:
//
//	ring:
//	  format: "{} ring"          # "{}" becomes the look's name
//	  color: [200, 200, 200]     # for any look without its own
//	  looks:
//	    - {name: jade, color: [0, 168, 107]}
//	    - agate                  # just a name: the pool's color

import (
	"errors"
	"fmt"
	"strings"

	"go.yaml.in/yaml/v3"

	"micro/server/internal/overlay"
	"micro/server/internal/rules"
)

// appearancesFile is the one content file that holds pools.
const appearancesFile = "appearances.yaml"

type poolYAML struct {
	Format string     `yaml:"format"`
	Color  []int      `yaml:"color"`
	Looks  []lookYAML `yaml:"looks"`
	line   int
}

func (p *poolYAML) UnmarshalYAML(n *yaml.Node) error {
	type plain poolYAML
	if err := strictDecode(n, (*plain)(p)); err != nil {
		return err
	}
	p.line = n.Line
	return nil
}

// lookYAML is one look: a bare name, or {name: ..., color: [r, g, b]}.
type lookYAML struct {
	Name  string `yaml:"name"`
	Color []int  `yaml:"color"`
	line  int
}

func (l *lookYAML) UnmarshalYAML(n *yaml.Node) error {
	l.line = n.Line
	if n.Kind == yaml.ScalarNode {
		return n.Decode(&l.Name)
	}
	type plain lookYAML
	return strictDecode(n, (*plain)(l))
}

// parsePools reads and checks the pools in f.
func parsePools(f yamlFile) (map[string]*rules.Pool, error) {
	var raw map[string]*poolYAML
	if err := yaml.Unmarshal(f.data, &raw); err != nil {
		return nil, fmt.Errorf("%s: %w", f.name, err)
	}
	pools := map[string]*rules.Pool{}
	var errs []error
	for name, y := range raw {
		bad := func(line int, format string, args ...any) {
			errs = append(errs, fmt.Errorf("%s:%d: pool %s: %s", f.name, line, name, fmt.Sprintf(format, args...)))
		}
		if y == nil {
			bad(0, "empty pool")
			continue
		}
		if !strings.Contains(y.Format, "{}") {
			bad(y.line, `format must contain "{}" for the look's name, got %q`, y.Format)
		}
		if len(y.Looks) == 0 {
			bad(y.line, "needs looks")
		}
		var poolColor *overlay.Color
		if y.Color != nil {
			c, err := colorOf(y.Color)
			if err != nil {
				bad(y.line, "%v", err)
			}
			poolColor = &c
		}
		p := &rules.Pool{Name: name, Format: y.Format}
		seen := map[string]bool{}
		for _, l := range y.Looks {
			switch {
			case l.Name == "":
				bad(l.line, "a look needs a name")
				continue
			case seen[l.Name]:
				bad(l.line, "look %q is listed twice", l.Name)
				continue
			}
			seen[l.Name] = true
			look := rules.PoolLook{Name: l.Name}
			switch {
			case l.Color != nil:
				c, err := colorOf(l.Color)
				if err != nil {
					bad(l.line, "%s: %v", l.Name, err)
				}
				look.Color = c
			case poolColor != nil:
				look.Color = *poolColor
			default:
				bad(l.line, "look %q needs a color, or the pool one", l.Name)
			}
			p.Looks = append(p.Looks, look)
		}
		pools[name] = p
	}
	return pools, errors.Join(errs...)
}

// colorOf checks and converts an [r, g, b] list.
func colorOf(v []int) (overlay.Color, error) {
	if len(v) != 3 {
		return overlay.Color{}, fmt.Errorf("color must be [r, g, b], got %v", v)
	}
	for _, x := range v {
		if x < 0 || x > 255 {
			return overlay.Color{}, fmt.Errorf("color components must be 0-255, got %v", v)
		}
	}
	return overlay.Color{R: uint8(v[0]), G: uint8(v[1]), B: uint8(v[2])}, nil
}
