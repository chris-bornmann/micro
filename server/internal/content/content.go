// Package content loads the game's templates — the player, every monster
// and every item — from YAML, and checks them.
//
// The files live in data/ and are compiled into the server. Loading is
// strict: an unknown field, an unknown trait, action or AI, a parameter a
// behavior does not accept or of the wrong type, and a reference to a
// template that does not exist are all errors, reported with the file and
// line, so a mistake in content stops the server at startup instead of
// quietly changing the game. See data/README.md for how to write content.
package content

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"reflect"
	"slices"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"go.yaml.in/yaml/v3"

	"micro/server/internal/attributes"
	"micro/server/internal/overlay"
	"micro/server/internal/rules"
)

//go:embed data/*.yaml data/levels/*.yaml
var data embed.FS

// Load reads the built-in content and checks it against reg.
func Load(reg *rules.Registry) (*rules.Catalog, error) {
	files, err := builtIn()
	if err != nil {
		return nil, err
	}
	return loadFiles(files, reg)
}

// LoadWithPlayer is Load with the player read from the YAML file at
// playerPath instead of the built-in player.yaml — a hand-crafted player
// to test with. The file may define more than the player (an item only it
// carries, say), but nothing the built-in content already defines.
func LoadWithPlayer(reg *rules.Registry, playerPath string) (*rules.Catalog, error) {
	files, err := builtIn()
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(playerPath)
	if err != nil {
		return nil, err
	}
	kept := files[:0]
	for _, f := range files {
		if f.name != "player.yaml" {
			kept = append(kept, f)
		}
	}
	return loadFiles(append(kept, yamlFile{name: playerPath, data: b}), reg)
}

// LoadFS reads every *.yaml file at the top of fsys and checks it against
// reg. Every error found is reported, not just the first.
func LoadFS(fsys fs.FS, reg *rules.Registry) (*rules.Catalog, error) {
	files, err := readAll(fsys)
	if err != nil {
		return nil, err
	}
	return loadFiles(files, reg)
}

// yamlFile is one content file: its name, for error messages, and what it
// says.
type yamlFile struct {
	name string
	data []byte
	// level is a special level's file (levels/), not templates.
	level bool
}

// builtIn is the content compiled into the server.
func builtIn() ([]yamlFile, error) {
	sub, err := fs.Sub(data, "data")
	if err != nil {
		return nil, err
	}
	return readAll(sub)
}

// readAll reads every *.yaml file at the top of fsys, in name order.
func readAll(fsys fs.FS) ([]yamlFile, error) {
	var files []yamlFile
	for _, pattern := range []string{"*.yaml", levelsDir + "/*.yaml"} {
		names, err := fs.Glob(fsys, pattern)
		if err != nil {
			return nil, err
		}
		sort.Strings(names)
		for _, name := range names {
			b, err := fs.ReadFile(fsys, name)
			if err != nil {
				return nil, err
			}
			files = append(files, yamlFile{name: name, data: b, level: pattern != "*.yaml"})
		}
	}
	return files, nil
}

// loadFiles checks files against reg and builds the catalog.
func loadFiles(files []yamlFile, reg *rules.Registry) (*rules.Catalog, error) {
	raw := map[string]*templateYAML{}
	pools := map[string]*rules.Pool{}
	var levelFiles []yamlFile
	var errs []error
	for _, f := range files {
		name := f.name
		if f.level {
			levelFiles = append(levelFiles, f)
			continue
		}
		if path.Base(name) == appearancesFile {
			p, err := parsePools(f)
			if err != nil {
				errs = append(errs, err)
			}
			pools = p
			continue
		}
		var file map[string]*templateYAML
		if err := yaml.Unmarshal(f.data, &file); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", name, err))
			continue
		}
		for id, t := range file {
			if t == nil {
				errs = append(errs, fmt.Errorf("%s: %s: empty template", name, id))
				continue
			}
			t.file = path.Base(name)
			if prev, dup := raw[id]; dup {
				errs = append(errs, fmt.Errorf("%s:%d: %s: already defined at %s:%d", t.file, t.line, id, prev.file, prev.line))
				continue
			}
			raw[id] = t
		}
	}
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}

	b := &builder{reg: reg, raw: raw, built: map[string]*rules.Template{}, pools: pools}
	var ids []string
	for id := range raw {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		b.build(id)
	}
	b.checkCatalog()
	if len(b.errs) > 0 {
		return nil, errors.Join(b.errs...)
	}
	var templates []*rules.Template
	for _, id := range ids {
		templates = append(templates, b.built[id])
	}
	cat := rules.NewCatalog(templates)
	cat.Pools = pools
	levels, err := loadLevels(levelFiles, cat)
	if err != nil {
		return nil, err
	}
	cat.Levels = levels
	return cat, nil
}

// templateYAML is one template as written.
type templateYAML struct {
	Category     string            `yaml:"category"`
	Name         string            `yaml:"name"`
	Kind         string            `yaml:"kind"`
	Glyph        string            `yaml:"glyph"`
	Color        []int             `yaml:"color"`
	Layer        *int              `yaml:"layer"`
	Weight       int               `yaml:"weight"`
	Level        int               `yaml:"level"`
	HP           *rangeYAML        `yaml:"hp"`
	Attributes   *attributesYAML   `yaml:"attributes"`
	Attack       *attackYAML       `yaml:"attack"`
	Damage       damageYAML        `yaml:"damage"`
	Slot         string            `yaml:"slot"`
	Blocks       []string          `yaml:"blocks"`
	UseVerb      string            `yaml:"use_verb"`
	Stairs       string            `yaml:"stairs"`
	Traits       []specYAML        `yaml:"traits"`
	OnUse        []specYAML        `yaml:"on_use"`
	AI           *specYAML         `yaml:"ai"`
	Brain        []specYAML        `yaml:"brain"`
	Spawn        *spawnYAML        `yaml:"spawn"`
	Equipment    []equipYAML       `yaml:"equipment"`
	Inventory    []carryYAML       `yaml:"inventory"`
	LeavesCorpse *bool             `yaml:"leaves_corpse"`
	Pronoun      string            `yaml:"pronoun"`
	Attitude     string            `yaml:"attitude"`
	Unique       bool              `yaml:"unique"`
	Returns      *returnsYAML      `yaml:"returns"`
	Messages     *messagesYAML     `yaml:"messages"`
	Unidentified *unidentifiedYAML `yaml:"unidentified"`

	file string
	line int
}

// UnmarshalYAML decodes a template strictly, remembering its line.
func (t *templateYAML) UnmarshalYAML(n *yaml.Node) error {
	type plain templateYAML
	if err := strictDecode(n, (*plain)(t)); err != nil {
		return err
	}
	t.line = n.Line
	return nil
}

type attackYAML struct {
	Name   string     `yaml:"name"`
	Damage damageYAML `yaml:"damage"`
}

func (a *attackYAML) UnmarshalYAML(n *yaml.Node) error {
	type plain attackYAML
	return strictDecode(n, (*plain)(a))
}

// messagesYAML is a template's attack descriptions.
type messagesYAML struct {
	Hit     []string `yaml:"hit"`
	Miss    []string `yaml:"miss"`
	YouHit  []string `yaml:"you_hit"`
	YouMiss []string `yaml:"you_miss"`
	line    int
}

func (m *messagesYAML) UnmarshalYAML(n *yaml.Node) error {
	type plain messagesYAML
	if err := strictDecode(n, (*plain)(m)); err != nil {
		return err
	}
	m.line = n.Line
	return nil
}

type spawnYAML struct {
	Model       string     `yaml:"model"`
	Count       *rangeYAML `yaml:"count"`
	Levels      *rangeYAML `yaml:"levels"`
	Chance      int        `yaml:"chance"`
	AlwaysOn    []int      `yaml:"always_on"`
	Density     float64    `yaml:"density"`
	SigmaUp     float64    `yaml:"sigma_up"`
	SigmaDown   float64    `yaml:"sigma_down"`
	MaxAbove    float64    `yaml:"max_above"`
	Earliest    int        `yaml:"earliest"`
	ForceBy     int        `yaml:"force_by"`
	ChanceScale float64    `yaml:"chance_scale"`
}

func (s *spawnYAML) UnmarshalYAML(n *yaml.Node) error {
	type plain spawnYAML
	return strictDecode(n, (*plain)(s))
}

// returnsYAML is how a unique character comes back after leaving a level.
type returnsYAML struct {
	Chance int `yaml:"chance"`
}

func (r *returnsYAML) UnmarshalYAML(n *yaml.Node) error {
	type plain returnsYAML
	return strictDecode(n, (*plain)(r))
}

// carryYAML is an item a creature starts with in its pack.
type carryYAML struct {
	Item   string `yaml:"item"`
	Chance *int   `yaml:"chance"`
	line   int
}

func (c *carryYAML) UnmarshalYAML(n *yaml.Node) error {
	type plain carryYAML
	if err := strictDecode(n, (*plain)(c)); err != nil {
		return err
	}
	c.line = n.Line
	return nil
}

type equipYAML struct {
	Item          string `yaml:"item"`
	LimitPerLevel int    `yaml:"limit_per_level"`
	Chance        *int   `yaml:"chance"`
	line          int
}

func (e *equipYAML) UnmarshalYAML(n *yaml.Node) error {
	type plain equipYAML
	if err := strictDecode(n, (*plain)(e)); err != nil {
		return err
	}
	e.line = n.Line
	return nil
}

// strictDecode decodes mapping node n into v, rejecting any key v has no
// yaml tag for. (A Node's own Decode does not honor the decoder's
// KnownFields option, so custom unmarshalers have to check for
// themselves.)
func strictDecode(n *yaml.Node, v any) error {
	if n.Kind != yaml.MappingNode {
		return fmt.Errorf("line %d: expected a mapping", n.Line)
	}
	known := map[string]bool{}
	rt := reflect.TypeOf(v).Elem()
	for i := 0; i < rt.NumField(); i++ {
		if tag := strings.Split(rt.Field(i).Tag.Get("yaml"), ",")[0]; tag != "" {
			known[tag] = true
		}
	}
	for i := 0; i < len(n.Content); i += 2 {
		k := n.Content[i]
		if !known[k.Value] {
			var names []string
			for name := range known {
				names = append(names, name)
			}
			sort.Strings(names)
			return fmt.Errorf("line %d: unknown field %q (known: %s)", k.Line, k.Value, strings.Join(names, ", "))
		}
	}
	return n.Decode(v)
}

// unidentifiedYAML is how an item can go unrecognized: its pool of looks,
// and the percent chance (default 100) a game starts with it unknown.
type unidentifiedYAML struct {
	Pool   string `yaml:"pool"`
	Chance *int   `yaml:"chance"`
	line   int
}

func (u *unidentifiedYAML) UnmarshalYAML(n *yaml.Node) error {
	type plain unidentifiedYAML
	if err := strictDecode(n, (*plain)(u)); err != nil {
		return err
	}
	u.line = n.Line
	return nil
}

// rangeYAML is a number or a [min, max] pair.
type rangeYAML struct {
	r    rules.Range
	line int
}

func (r *rangeYAML) UnmarshalYAML(n *yaml.Node) error {
	r.line = n.Line
	switch n.Kind {
	case yaml.ScalarNode:
		var v int
		if err := n.Decode(&v); err != nil {
			return fmt.Errorf("line %d: want an integer or [min, max]", n.Line)
		}
		r.r = rules.Range{Min: v, Max: v}
		return nil
	case yaml.SequenceNode:
		var v []any
		if err := n.Decode(&v); err != nil {
			return err
		}
		rg, err := rules.RangeFrom(v)
		if err != nil {
			return fmt.Errorf("line %d: %w", n.Line, err)
		}
		r.r = rg
		return nil
	}
	return fmt.Errorf("line %d: want an integer or [min, max]", n.Line)
}

// attributesYAML is a creature's attribute ranges: one number or [min, max]
// for all four, or a mapping that sets each separately — every one of the
// four, each a number or [min, max].
type attributesYAML struct {
	r attributes.Ranges
}

func (a *attributesYAML) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind != yaml.MappingNode {
		var all rangeYAML
		if err := n.Decode(&all); err != nil {
			return err
		}
		a.r = attributes.Same(attributes.Range{Min: all.r.Min, Max: all.r.Max})
		return nil
	}
	var each map[string]rangeYAML
	if err := n.Decode(&each); err != nil {
		return err
	}
	stats := []struct {
		name string
		into *attributes.Range
	}{
		{"strength", &a.r.Strength},
		{"intelligence", &a.r.Intelligence},
		{"dexterity", &a.r.Dexterity},
		{"stamina", &a.r.Stamina},
	}
	for _, s := range stats {
		v, ok := each[s.name]
		if !ok {
			return fmt.Errorf("line %d: attributes needs all four of strength, intelligence, dexterity and stamina; %s is missing", n.Line, s.name)
		}
		*s.into = attributes.Range{Min: v.r.Min, Max: v.r.Max}
		delete(each, s.name)
	}
	for name, v := range each {
		return fmt.Errorf("line %d: unknown attribute %q (known: strength, intelligence, dexterity, stamina)", v.line, name)
	}
	return nil
}

// damageYAML is a mapping from damage type to range, in the order written:
// the first part is the weapon's main damage.
type damageYAML struct {
	parts []rules.DamagePart
	line  int
}

func (d *damageYAML) UnmarshalYAML(n *yaml.Node) error {
	d.line = n.Line
	if n.Kind != yaml.MappingNode {
		return fmt.Errorf("line %d: damage is a mapping of type to [min, max], e.g. {slashing: [1, 6]}", n.Line)
	}
	for i := 0; i < len(n.Content); i += 2 {
		k, v := n.Content[i], n.Content[i+1]
		typ := rules.DamageType(k.Value)
		if !rules.ValidDealtType(typ) {
			var names []string
			for _, d := range rules.DealtTypes {
				names = append(names, string(d))
			}
			return fmt.Errorf("line %d: unknown damage type %q — an attack deals %s", k.Line, k.Value, strings.Join(names, ", "))
		}
		var r rangeYAML
		if err := r.UnmarshalYAML(v); err != nil {
			return err
		}
		d.parts = append(d.parts, rules.DamagePart{Type: typ, Range: r.r})
	}
	return nil
}

// specYAML is a behavior reference with parameters: {trait: name, ...},
// {action: name, ...} or {behavior: name, ...}. Which key names the
// behavior depends on where it appears; the rest are parameters.
type specYAML struct {
	fields map[string]any
	line   int
}

func (s *specYAML) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind != yaml.MappingNode {
		return fmt.Errorf("line %d: expected a mapping like {trait: name, ...}", n.Line)
	}
	s.line = n.Line
	return n.Decode(&s.fields)
}

// split separates the behavior name (under key) from the parameters.
func (s specYAML) split(key string) (string, map[string]any, error) {
	params := map[string]any{}
	var name string
	for k, v := range s.fields {
		if k == key {
			n, ok := v.(string)
			if !ok {
				return "", nil, fmt.Errorf("%q must be a name, got %v", key, v)
			}
			name = n
			continue
		}
		params[k] = v
	}
	if name == "" {
		return "", nil, fmt.Errorf("missing %q naming the behavior", key)
	}
	return name, params, nil
}

// builder turns templateYAML into rules.Template, collecting errors.
type builder struct {
	reg   *rules.Registry
	raw   map[string]*templateYAML
	built map[string]*rules.Template
	pools map[string]*rules.Pool
	errs  []error
}

func (b *builder) errorf(t *templateYAML, id string, line int, format string, args ...any) {
	if line == 0 {
		line = t.line
	}
	b.errs = append(b.errs, fmt.Errorf("%s:%d: %s: %s", t.file, line, id, fmt.Sprintf(format, args...)))
}

func (b *builder) build(id string) {
	y := b.raw[id]
	t := &rules.Template{
		ID:       id,
		Name:     y.Name,
		Category: rules.Category(y.Category),
		Kind:     y.Kind,
		Weight:   y.Weight,
		Level:    y.Level,
		Slot:     rules.Slot(y.Slot),
		UseVerb:  y.UseVerb,
		Stairs:   y.Stairs,
		Damage:   y.Damage.parts,
	}
	b.built[id] = t
	bad := func(format string, args ...any) { b.errorf(y, id, 0, format, args...) }

	valid := false
	for _, c := range rules.Categories {
		valid = valid || c == t.Category
	}
	if !valid {
		bad("unknown category %q (known: %s)", y.Category, joinCategories())
		return
	}
	creature := t.Category.IsCreature()

	if y.Name == "" {
		bad("name is required")
	}
	if utf8.RuneCountInString(y.Glyph) != 1 {
		bad("glyph must be exactly one character, got %q", y.Glyph)
	} else {
		t.Glyph, _ = utf8.DecodeRuneInString(y.Glyph)
	}
	if len(y.Color) != 3 {
		bad("color must be [r, g, b], got %v", y.Color)
	} else {
		for _, v := range y.Color {
			if v < 0 || v > 255 {
				bad("color components must be 0-255, got %v", y.Color)
				break
			}
		}
		t.Color = overlay.Color{R: uint8(y.Color[0]), G: uint8(y.Color[1]), B: uint8(y.Color[2])}
	}
	if y.Weight < 0 {
		bad("weight must not be negative")
	}
	switch {
	case y.Layer != nil:
		t.Layer = *y.Layer
	case t.Category == rules.CategoryPlayer:
		t.Layer = 2
	case creature:
		t.Layer = 1
	case t.Category == rules.CategoryTrap:
		t.Layer = -1 // under anything lying on it
	}

	if creature {
		if y.HP == nil || y.HP.r.Min <= 0 {
			bad("a creature needs hp: [min, max] above 0")
		} else {
			t.HP = y.HP.r
		}
		if y.Attributes == nil {
			bad("a creature needs attributes: [min, max], or {strength: ..., intelligence: ..., dexterity: ..., stamina: ...}")
		} else {
			t.Attributes = y.Attributes.r
		}
		if y.Attack == nil || len(y.Attack.Damage.parts) == 0 || y.Attack.Name == "" {
			bad("a creature needs attack: {name: ..., damage: {bludgeoning: [min, max]}}")
		} else {
			t.Attack = rules.NaturalAttack{Name: y.Attack.Name, Damage: y.Attack.Damage.parts}
		}
		if len(y.Damage.parts) > 0 || y.Slot != "" || len(y.OnUse) > 0 || y.Stairs != "" || y.UseVerb != "" {
			bad("damage, slot, on_use, use_verb and stairs are for items, not creatures")
		}
	} else {
		if y.HP != nil || y.Attributes != nil || y.Attack != nil || y.AI != nil || len(y.Equipment) > 0 || len(y.Inventory) > 0 || y.LeavesCorpse != nil || y.Level != 0 {
			bad("hp, attributes, attack, ai, equipment, inventory, level and leaves_corpse are for creatures, not items")
		}
	}

	if t.Slot != "" {
		ok := false
		for _, s := range rules.EquipSlots {
			ok = ok || s == t.Slot
		}
		if !ok {
			bad("unknown slot %q (known: %s)", y.Slot, joinSlots())
		}
	}
	for _, s := range y.Blocks {
		switch {
		case t.Slot == "":
			bad("blocks is for an item that is equipped: it has no slot")
		case !slices.Contains(rules.EquipSlots, rules.Slot(s)) || rules.Slot(s) == rules.SlotRing:
			bad("blocks: unknown slot %q (known: %s)", s, joinSlots())
		case rules.Slot(s) == t.Slot:
			bad("blocks: %q is its own slot", s)
		default:
			t.Blocks = append(t.Blocks, rules.Slot(s))
		}
	}
	if t.Slot == rules.SlotWeapon && len(t.Damage) == 0 {
		bad("an item in the weapon slot needs damage: {slashing: [min, max]}")
	}
	if len(t.Damage) > 0 && t.Slot != rules.SlotWeapon {
		bad("damage is only used by items in the weapon slot")
	}
	if y.UseVerb != "" && len(y.OnUse) == 0 {
		bad("use_verb without on_use")
	}
	if y.Stairs != "" && y.Stairs != "up" && y.Stairs != "down" {
		bad("stairs must be up or down, got %q", y.Stairs)
	}

	for i, s := range y.Traits {
		name, params, err := s.split("trait")
		if err != nil {
			b.errorf(y, id, s.line, "traits[%d]: %v", i, err)
			continue
		}
		beh, ok := b.reg.Trait(name)
		if !ok {
			b.errorf(y, id, s.line, "traits[%d]: unknown trait %q (known: %s)", i, name, strings.Join(b.reg.TraitNames(), ", "))
			continue
		}
		// duration belongs to every trait, not to any one behavior: how
		// many turns it lasts, rather than for good.
		duration := 0
		if d, ok := params["duration"]; ok {
			n, isInt := d.(int)
			if !isInt || n < 1 {
				b.errorf(y, id, s.line, "traits[%d] (%s): duration must be a whole number of turns, 1 or more; got %v", i, name, d)
				continue
			}
			duration = n
			delete(params, "duration")
		}
		p, err := rules.CheckParams(params, beh.Params(), b.reg)
		if err != nil {
			b.errorf(y, id, s.line, "traits[%d] (%s): %v", i, name, err)
			continue
		}
		t.Traits = append(t.Traits, rules.TraitSpec{Trait: name, Params: p, Duration: duration})
	}

	for i, s := range y.OnUse {
		name, params, err := s.split("action")
		if err != nil {
			b.errorf(y, id, s.line, "on_use[%d]: %v", i, err)
			continue
		}
		act, ok := b.reg.Action(name)
		if !ok {
			b.errorf(y, id, s.line, "on_use[%d]: unknown action %q (known: %s)", i, name, strings.Join(b.reg.ActionNames(), ", "))
			continue
		}
		p, err := rules.CheckParams(params, act.Params(), b.reg)
		if err != nil {
			b.errorf(y, id, s.line, "on_use[%d] (%s): %v", i, name, err)
			continue
		}
		b.checkGrantable(y, id, s.line, act.Params(), p)
		t.OnUse = append(t.OnUse, rules.ActionSpec{Action: name, Params: p})
	}

	if y.AI != nil {
		name, params, err := y.AI.split("behavior")
		if err != nil {
			b.errorf(y, id, y.AI.line, "ai: %v", err)
		} else if beh, ok := b.reg.AI(name); !ok {
			b.errorf(y, id, y.AI.line, "ai: unknown behavior %q (known: %s)", name, strings.Join(b.reg.AINames(), ", "))
		} else if p, err := rules.CheckParams(params, beh.Params(), b.reg); err != nil {
			b.errorf(y, id, y.AI.line, "ai (%s): %v", name, err)
		} else {
			t.AI = rules.AISpec{Behavior: name, Params: p}
		}
	}
	b.buildBrain(y, id, t)
	if t.Category == rules.CategoryMonster && y.AI == nil {
		bad("a monster needs ai: {behavior: ...}")
	}
	if t.Category == rules.CategoryPlayer && y.AI != nil {
		bad("the player is driven by the keyboard, not an ai")
	}

	if t.Category == rules.CategoryMonster {
		t.LeavesCorpse = y.LeavesCorpse == nil || *y.LeavesCorpse
	}

	t.Attitude = rules.Attitude(y.Attitude)
	if y.Attitude != "" && (!creature || !slices.Contains(rules.Attitudes, t.Attitude)) {
		bad("attitude is for creatures, and one of hostile, neutral, afraid; got %q", y.Attitude)
	}

	t.Pronoun = y.Pronoun
	if y.Pronoun != "" && !slices.Contains(rules.Pronouns, y.Pronoun) {
		bad("pronoun must be one of %s, got %q", strings.Join(rules.Pronouns, ", "), y.Pronoun)
	}
	b.buildMessages(y, id, t)
	b.buildUnidentified(y, id, t)
	b.buildUnique(y, id, t)

	b.buildSpawn(y, id, t)
}

// buildMessages checks a template's attack descriptions. A monster only
// ever attacks the player, so it has no you_ lists; the player only ever
// attacks monsters, so it has only you_ lists. An item can be wielded by
// either.
// buildUnidentified checks an item's unidentified block: only items that
// are carried can go unrecognized, from a pool that exists — and an item
// that can must not give itself away in its own messages, which name it
// with {weapon} instead.
func (b *builder) buildUnidentified(y *templateYAML, id string, t *rules.Template) {
	u := y.Unidentified
	if u == nil {
		return
	}
	bad := func(format string, args ...any) { b.errorf(y, id, u.line, format, args...) }
	chance := 100
	if u.Chance != nil {
		chance = *u.Chance
	}
	switch t.Category {
	case rules.CategoryPlayer, rules.CategoryMonster, rules.CategoryStairs, rules.CategoryTrap, rules.CategoryCorpse:
		bad("only items can be unidentified, not a %s", t.Category)
		return
	}
	if b.pools[u.Pool] == nil {
		var names []string
		for n := range b.pools {
			names = append(names, n)
		}
		sort.Strings(names)
		bad("unidentified: unknown pool %q (known: %s)", u.Pool, strings.Join(names, ", "))
		return
	}
	if chance < 0 || chance > 100 {
		bad("unidentified: chance must be 0-100, got %d", chance)
		return
	}
	m := t.Messages
	for _, line := range slices.Concat(m.Hit, m.Miss, m.YouHit, m.YouMiss) {
		if strings.Contains(strings.ToLower(line), strings.ToLower(t.Name)) {
			bad("message %q gives away what it is — it can go unidentified, so name it {weapon}", line)
		}
	}
	t.Unidentified = &rules.Unidentified{Pool: u.Pool, Chance: chance}
}

func (b *builder) buildMessages(y *templateYAML, id string, t *rules.Template) {
	m := y.Messages
	if m == nil {
		return
	}
	lists := []struct {
		name  string
		lines []string
		you   bool
	}{{"hit", m.Hit, false}, {"miss", m.Miss, false}, {"you_hit", m.YouHit, true}, {"you_miss", m.YouMiss, true}}
	for _, l := range lists {
		if len(l.lines) == 0 {
			continue
		}
		switch {
		case l.you && t.Category == rules.CategoryMonster:
			b.errorf(y, id, m.line, "messages.%s: a monster never attacks as the player; use hit and miss", l.name)
		case !l.you && t.Category == rules.CategoryPlayer:
			b.errorf(y, id, m.line, "messages.%s: the player's own attacks are described by you_hit and you_miss", l.name)
		}
		for i, line := range l.lines {
			if err := rules.CheckMessage(line); err != nil {
				b.errorf(y, id, m.line, "messages.%s[%d]: %v", l.name, i, err)
			}
		}
	}
	t.Messages = rules.AttackMessages{Hit: m.Hit, Miss: m.Miss, YouHit: m.YouHit, YouMiss: m.YouMiss}
}

// buildUnique checks a unique character, and the glyph convention that
// sets them apart: an upper-case letter for a unique monster, never for
// an ordinary one.
func (b *builder) buildUnique(y *templateYAML, id string, t *rules.Template) {
	bad := func(format string, args ...any) { b.errorf(y, id, 0, format, args...) }
	t.Unique = y.Unique
	upper := unicode.IsUpper(t.Glyph)
	switch {
	case y.Unique && t.Category != rules.CategoryMonster:
		bad("only a monster can be unique")
	case y.Unique && !upper:
		bad("a unique monster's glyph is an upper-case letter, got %q", string(t.Glyph))
	case !y.Unique && t.Category == rules.CategoryMonster && upper:
		bad("an upper-case glyph marks a unique monster; add unique: true or use lower case")
	case y.Unique && y.Spawn != nil && y.Spawn.Model != string(rules.SpawnSingleShot) && y.Spawn.Model != string(rules.SpawnPlaced):
		bad("a unique monster spawns with model single_shot or placed: at most once a run")
	}
	if y.Returns != nil {
		if !y.Unique {
			bad("returns is for unique monsters")
		}
		if y.Returns.Chance < 0 || y.Returns.Chance > 100 {
			bad("returns chance must be 0-100, got %d", y.Returns.Chance)
		}
		t.ReturnChance = y.Returns.Chance
	}
}

// buildBrain checks a utility AI creature's menu of options. Every option
// takes a weight (default 1) besides its own parameters.
func (b *builder) buildBrain(y *templateYAML, id string, t *rules.Template) {
	utility := t.AI.Behavior == "utility"
	switch {
	case utility && len(y.Brain) == 0:
		b.errorf(y, id, 0, "ai behavior utility needs a brain: a list of options")
		return
	case !utility && len(y.Brain) > 0:
		b.errorf(y, id, 0, "brain is only for ai behavior utility")
		return
	}
	for i, s := range y.Brain {
		name, params, err := s.split("option")
		if err != nil {
			b.errorf(y, id, s.line, "brain[%d]: %v", i, err)
			continue
		}
		opt, ok := b.reg.Option(name)
		if !ok {
			b.errorf(y, id, s.line, "brain[%d]: unknown option %q (known: %s)", i, name, strings.Join(b.reg.OptionNames(), ", "))
			continue
		}
		defs := append([]rules.ParamDef{{Name: "weight", Kind: rules.ParamFloat, Default: 1.0}}, opt.Params()...)
		p, err := rules.CheckParams(params, defs, b.reg)
		if err != nil {
			b.errorf(y, id, s.line, "brain[%d] (%s): %v", i, name, err)
			continue
		}
		weight := p.Float("weight")
		delete(p, "weight")
		t.Brain = append(t.Brain, rules.OptionSpec{Option: name, Weight: weight, Params: p})
	}
}

// checkGrantable makes sure every trait an action grants at run time can
// be created without parameters, since there is nowhere to write them.
func (b *builder) checkGrantable(y *templateYAML, id string, line int, defs []rules.ParamDef, p rules.Params) {
	for _, d := range defs {
		if d.Kind != rules.ParamTrait {
			continue
		}
		name := p.String(d.Name)
		beh, _ := b.reg.Trait(name)
		for _, pd := range beh.Params() {
			if pd.Required {
				b.errorf(y, id, line, "trait %q needs parameter %q, so it cannot be granted", name, pd.Name)
			}
		}
	}
}

func (b *builder) buildSpawn(y *templateYAML, id string, t *rules.Template) {
	bad := func(format string, args ...any) { b.errorf(y, id, 0, format, args...) }
	if y.Spawn == nil {
		switch t.Category {
		case rules.CategoryMonster:
			bad("a monster needs spawn: {model: common|single_shot, ...}")
		case rules.CategoryTrap:
			bad("a trap needs spawn: {model: per_level, ...}")
		}
		return
	}
	if t.Category == rules.CategoryPlayer {
		bad("the player is not spawned by level")
		return
	}
	s := y.Spawn
	t.Spawn = rules.SpawnSpec{
		Model: rules.SpawnModel(s.Model), Density: s.Density,
		SigmaUp: s.SigmaUp, SigmaDown: s.SigmaDown, MaxAbove: s.MaxAbove,
		Earliest: s.Earliest, ForceBy: s.ForceBy, ChanceScale: s.ChanceScale,
		Chance: s.Chance, AlwaysOn: s.AlwaysOn,
	}
	if s.Levels != nil {
		t.Spawn.Levels = s.Levels.r
	}
	curve := s.SigmaUp > 0 && s.SigmaDown > 0 && s.MaxAbove > 0
	switch t.Spawn.Model {
	case rules.SpawnFixed:
		if s.Count == nil || s.Count.r.Min <= 0 || s.Count.r.Min != s.Count.r.Max {
			bad("spawn model fixed needs a single count above 0")
		} else {
			t.Spawn.Count = s.Count.r.Min
		}
	case rules.SpawnCommon:
		if s.Density <= 0 || !curve {
			bad("spawn model common needs density, sigma_up, sigma_down and max_above above 0")
		}
	case rules.SpawnSingleShot:
		if !curve || s.ChanceScale <= 0 || s.Earliest <= 0 || s.ForceBy < s.Earliest {
			bad("spawn model single_shot needs earliest, force_by (not below earliest), chance_scale, sigma_up, sigma_down and max_above")
		}
	case rules.SpawnPerLevel:
		b.checkPerLevel(bad, s, t)
	case rules.SpawnPlaced:
		if t.Category != rules.CategoryMonster {
			bad("only monsters spawn with model placed")
		}
	default:
		bad("unknown spawn model %q (known: common, single_shot, fixed, per_level, placed)", s.Model)
	}
	isTrap := t.Category == rules.CategoryTrap
	switch {
	case isTrap && t.Spawn.Model != rules.SpawnPerLevel:
		bad("traps spawn with model per_level")
	case !isTrap && t.Spawn.Model == rules.SpawnPerLevel:
		bad("only traps spawn with model per_level")
	case !isTrap && !t.Category.IsCreature() && t.Spawn.Model != rules.SpawnFixed:
		bad("items spawn with model fixed")
	case t.Category.IsCreature() && t.Spawn.Model != rules.SpawnFixed && t.Spawn.Model != rules.SpawnPlaced && t.Level <= 0:
		bad("a monster spawned by level curve needs level above 0")
	}
}

// checkPerLevel checks a per_level spawn block: which levels, how likely,
// how many, and which levels must have one.
func (b *builder) checkPerLevel(bad func(string, ...any), s *spawnYAML, t *rules.Template) {
	if s.Levels == nil || s.Levels.r.Min < 1 || s.Levels.r.Max > rules.DeepestLevel {
		bad("spawn model per_level needs levels: [first, last] within 1-%d", rules.DeepestLevel)
	}
	if s.Chance < 0 || s.Chance > 100 {
		bad("spawn chance must be 0-100, got %d", s.Chance)
	}
	if s.Count == nil || s.Count.r.Min < 1 {
		bad("spawn model per_level needs count: n or [min, max], at least 1")
	} else {
		t.Spawn.Number = s.Count.r
	}
	for _, l := range s.AlwaysOn {
		if s.Levels != nil && (l < s.Levels.r.Min || l > s.Levels.r.Max) {
			bad("always_on level %d is outside levels [%d, %d]", l, s.Levels.r.Min, s.Levels.r.Max)
		}
	}
}

// checkCatalog checks what can only be checked once every template is
// built: references between templates, and templates the engine needs.
func (b *builder) checkCatalog() {
	var ids []string
	for id := range b.raw {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	// Every type using a pool needs a look of its own.
	using := map[string]int{}
	for _, t := range b.built {
		if t.Unidentified != nil {
			using[t.Unidentified.Pool]++
		}
	}
	for name, n := range using {
		if looks := len(b.pools[name].Looks); n > looks {
			b.errs = append(b.errs, fmt.Errorf("%s: pool %s has %d looks for %d item types: each type needs a look of its own", appearancesFile, name, looks, n))
		}
	}

	players := 0
	needCorpse := false
	for _, id := range ids {
		y, t := b.raw[id], b.built[id]
		if t.Category == rules.CategoryPlayer {
			players++
		}
		needCorpse = needCorpse || t.LeavesCorpse
		for i, e := range y.Equipment {
			item := b.built[e.Item]
			switch {
			case b.raw[e.Item] == nil:
				b.errorf(y, id, e.line, "equipment[%d]: no template named %q", i, e.Item)
			case item.Slot == "":
				b.errorf(y, id, e.line, "equipment[%d]: %q has no slot to be equipped in", i, e.Item)
			case e.Chance != nil && (*e.Chance < 0 || *e.Chance > 100):
				b.errorf(y, id, e.line, "equipment[%d]: chance must be 0-100, got %d", i, *e.Chance)
			default:
				chance := 100
				if e.Chance != nil {
					chance = *e.Chance
				}
				t.Equipment = append(t.Equipment, rules.EquipSpec{Item: item, LimitPerLevel: e.LimitPerLevel, Chance: chance})
			}
		}
		for i, cy := range y.Inventory {
			chance := 100
			if cy.Chance != nil {
				chance = *cy.Chance
			}
			switch {
			case b.raw[cy.Item] == nil:
				b.errorf(y, id, cy.line, "inventory[%d]: no template named %q", i, cy.Item)
			case b.built[cy.Item].Category.IsCreature() || b.built[cy.Item].Category == rules.CategoryTrap:
				b.errorf(y, id, cy.line, "inventory[%d]: %q is not an item", i, cy.Item)
			case chance < 0 || chance > 100:
				b.errorf(y, id, cy.line, "inventory[%d]: chance must be 0-100, got %d", i, chance)
			default:
				t.Inventory = append(t.Inventory, rules.CarrySpec{Item: b.built[cy.Item], Chance: chance})
			}
		}
	}
	if players != 1 {
		b.errs = append(b.errs, fmt.Errorf("content: want exactly one template with category player, found %d", players))
	}
	if needCorpse {
		if c := b.built["corpse"]; c == nil || c.Category != rules.CategoryCorpse {
			b.errs = append(b.errs, fmt.Errorf("content: monsters leave corpses, so a template named corpse with category corpse is required"))
		}
	}
}

func joinCategories() string {
	s := make([]string, len(rules.Categories))
	for i, c := range rules.Categories {
		s[i] = string(c)
	}
	return strings.Join(s, ", ")
}

func joinSlots() string {
	s := make([]string, len(rules.EquipSlots))
	for i, c := range rules.EquipSlots {
		s[i] = string(c)
	}
	return strings.Join(s, ", ")
}
