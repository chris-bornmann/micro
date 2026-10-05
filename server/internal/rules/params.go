package rules

import (
	"fmt"
	"math/rand"
	"sort"
	"strings"
)

// ParamKind is the type a behavior declares for one of its parameters.
type ParamKind int

const (
	// ParamInt is an integer. In a template it may also be written as a
	// [min, max] range, which is rolled once when an entity is created —
	// so every healing potion heals a fixed amount, but not the same
	// fixed amount as every other one.
	ParamInt ParamKind = iota
	ParamFloat
	ParamString
	ParamBool
	// ParamDamageType is a string that must name a DamageType.
	ParamDamageType
	// ParamTrait is a string that must name a registered trait behavior.
	ParamTrait
	// ParamDice is a number or a [min, max] range that is kept as a range
	// and rolled each time it is used (see Params.Roll) — a dart's damage —
	// unlike ParamInt's, which is rolled once when the entity is made.
	ParamDice
)

func (k ParamKind) String() string {
	switch k {
	case ParamInt:
		return "integer or [min, max]"
	case ParamFloat:
		return "number"
	case ParamString:
		return "string"
	case ParamBool:
		return "boolean"
	case ParamDamageType:
		return "damage type"
	case ParamTrait:
		return "trait name"
	case ParamDice:
		return "integer or [min, max]"
	}
	return "unknown"
}

// ParamDef declares one parameter a behavior accepts. A parameter that is
// not Required and is omitted takes Default (which may be nil, meaning the
// behavior checks Has itself). Min and Max, when set, bound a ParamInt —
// both ends of it, if it is written as a range.
type ParamDef struct {
	Name     string
	Kind     ParamKind
	Required bool
	Default  any
	Min, Max *int
}

// Bound returns a pointer to n, for ParamDef.Min and Max.
func Bound(n int) *int { return &n }

// Dice is a ParamDice value: a range rolled each time it is used.
type Dice Range

// Params holds a behavior's parameter values. After validation each value
// is an int, a Range (an int still to be rolled), a float64, a string or a
// bool, according to its ParamDef.
type Params map[string]any

// Has reports whether name is set.
func (p Params) Has(name string) bool { _, ok := p[name]; return ok }

// Int returns name as an int. A Range that was never rolled returns its
// minimum, which only happens for a spec read directly rather than
// through an instance.
func (p Params) Int(name string) int {
	switch v := p[name].(type) {
	case int:
		return v
	case Range:
		return v.Min
	case float64:
		return int(v)
	}
	return 0
}

// Roll rolls the ParamDice parameter name from the global source: it is
// turn-time randomness.
func (p Params) Roll(name string) int {
	d, ok := p[name].(Dice)
	if !ok {
		return p.Int(name)
	}
	return rollRange(Range(d), nil)
}

// Float returns name as a float64.
func (p Params) Float(name string) float64 {
	switch v := p[name].(type) {
	case float64:
		return v
	case int:
		return float64(v)
	}
	return 0
}

// String returns name as a string.
func (p Params) String(name string) string { s, _ := p[name].(string); return s }

// Bool returns name as a bool.
func (p Params) Bool(name string) bool { b, _ := p[name].(bool); return b }

// roll returns a copy of p with every Range replaced by a value rolled
// from rng. rng may be nil, in which case the global source is used.
func (p Params) roll(rng *rand.Rand) Params {
	out := make(Params, len(p))
	for _, k := range sortedKeys(p) {
		v := p[k]
		if r, ok := v.(Range); ok {
			v = rollRange(r, rng)
		}
		out[k] = v
	}
	return out
}

func rollRange(r Range, rng *rand.Rand) int {
	if r.Max <= r.Min {
		return r.Min
	}
	if rng == nil {
		return r.Min + rand.Intn(r.Max-r.Min+1)
	}
	return r.Min + rng.Intn(r.Max-r.Min+1)
}

func sortedKeys(p Params) []string {
	keys := make([]string, 0, len(p))
	for k := range p {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// CheckParams converts raw parameter values (as decoded from YAML: int,
// float64, string, bool, or a two-element []any for a range) into typed
// Params according to defs, filling in defaults. It reports an unknown
// parameter, a missing required one, or a value of the wrong type. reg is
// consulted for ParamTrait values.
func CheckParams(raw map[string]any, defs []ParamDef, reg *Registry) (Params, error) {
	known := map[string]ParamDef{}
	for _, d := range defs {
		known[d.Name] = d
	}
	var names []string
	for k := range raw {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, k := range names {
		if _, ok := known[k]; !ok {
			return nil, fmt.Errorf("unknown parameter %q (accepted: %s)", k, paramNames(defs))
		}
	}

	out := Params{}
	for _, d := range defs {
		v, ok := raw[d.Name]
		if !ok {
			if d.Required {
				return nil, fmt.Errorf("missing required parameter %q", d.Name)
			}
			if d.Default != nil {
				out[d.Name] = d.Default
			}
			continue
		}
		cv, err := convertParam(v, d, reg)
		if err != nil {
			return nil, fmt.Errorf("parameter %q: %w", d.Name, err)
		}
		out[d.Name] = cv
	}
	return out, nil
}

func paramNames(defs []ParamDef) string {
	if len(defs) == 0 {
		return "none"
	}
	names := make([]string, len(defs))
	for i, d := range defs {
		names[i] = d.Name
	}
	return strings.Join(names, ", ")
}

func convertParam(v any, d ParamDef, reg *Registry) (any, error) {
	switch d.Kind {
	case ParamInt:
		switch x := v.(type) {
		case int:
			return x, checkBounds(x, d)
		case []any:
			r, err := RangeFrom(x)
			if err != nil {
				return nil, err
			}
			if err := checkBounds(r.Min, d); err != nil {
				return nil, err
			}
			return r, checkBounds(r.Max, d)
		}
	case ParamDice:
		switch x := v.(type) {
		case int:
			return Dice{x, x}, checkBounds(x, d)
		case []any:
			r, err := RangeFrom(x)
			if err != nil {
				return nil, err
			}
			if err := checkBounds(r.Min, d); err != nil {
				return nil, err
			}
			return Dice(r), checkBounds(r.Max, d)
		}
	case ParamFloat:
		var f float64
		switch x := v.(type) {
		case int:
			f = float64(x)
		case float64:
			f = x
		default:
			return nil, fmt.Errorf("want %s, got %v", d.Kind, v)
		}
		if d.Min != nil && f < float64(*d.Min) {
			return nil, fmt.Errorf("%v is below the minimum %d", f, *d.Min)
		}
		if d.Max != nil && f > float64(*d.Max) {
			return nil, fmt.Errorf("%v is above the maximum %d", f, *d.Max)
		}
		return f, nil
	case ParamString:
		if s, ok := v.(string); ok {
			return s, nil
		}
	case ParamBool:
		if b, ok := v.(bool); ok {
			return b, nil
		}
	case ParamDamageType:
		if s, ok := v.(string); ok {
			if !ValidDamageType(DamageType(s)) {
				return nil, fmt.Errorf("unknown damage type %q", s)
			}
			return s, nil
		}
	case ParamTrait:
		if s, ok := v.(string); ok {
			if _, ok := reg.Trait(s); !ok {
				return nil, fmt.Errorf("unknown trait %q", s)
			}
			return s, nil
		}
	}
	return nil, fmt.Errorf("want %s, got %v", d.Kind, v)
}

func checkBounds(n int, d ParamDef) error {
	if d.Min != nil && n < *d.Min {
		return fmt.Errorf("%d is below the minimum %d", n, *d.Min)
	}
	if d.Max != nil && n > *d.Max {
		return fmt.Errorf("%d is above the maximum %d", n, *d.Max)
	}
	return nil
}

// RangeFrom converts a decoded two-element list into a Range.
func RangeFrom(x []any) (Range, error) {
	if len(x) != 2 {
		return Range{}, fmt.Errorf("a range needs exactly two numbers, got %d", len(x))
	}
	lo, ok1 := x[0].(int)
	hi, ok2 := x[1].(int)
	if !ok1 || !ok2 {
		return Range{}, fmt.Errorf("a range needs two integers, got %v", x)
	}
	if hi < lo {
		return Range{}, fmt.Errorf("range [%d, %d] has max below min", lo, hi)
	}
	return Range{Min: lo, Max: hi}, nil
}

// ValidDamageType reports whether a trait may name t: any dealt type, or
// the physical umbrella.
func ValidDamageType(t DamageType) bool {
	return t == DamagePhysical || ValidDealtType(t)
}

// ValidDealtType reports whether an attack may deal t.
func ValidDealtType(t DamageType) bool {
	for _, d := range DealtTypes {
		if t == d {
			return true
		}
	}
	return false
}

// DefaultParams is t's parameters with every default filled in and nothing
// else — the parameters of a trait granted at run time rather than written
// in a template.
func DefaultParams(t Trait) Params {
	p := Params{}
	if t == nil {
		return p
	}
	for _, d := range t.Params() {
		if d.Default != nil {
			p[d.Name] = d.Default
		}
	}
	return p
}
