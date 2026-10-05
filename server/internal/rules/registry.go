package rules

import (
	"fmt"
	"sort"
)

// Trait is a trait behavior: Go code a template refers to by name. Params
// declares what a template may configure; everything else a behavior does
// is through the optional hook interfaces below, of which it implements any
// subset.
type Trait interface {
	Params() []ParamDef
}

// Modifier contributes to a number being computed. It may only add to
// q.Add or multiply q.Mul, never overwrite them, so that the order traits
// are visited in cannot change the result.
type Modifier interface {
	Modify(q *Query, h Holder)
}

// Vetoer may refuse an attempted action. A non-empty return is the
// refusal, shown to the player as-is.
type Vetoer interface {
	Veto(a *Attempt, h Holder) string
}

// HitHandler reacts to a hit that has just landed and done its damage.
type HitHandler interface {
	OnHit(c *Ctx, hit *Hit, h Holder)
}

// Ticker is called once per turn for an instance on a creature or on
// something it has equipped. A Ticker owns its instance's countdown: the
// engine only counts down Turns for behaviors that are not Tickers.
type Ticker interface {
	Tick(c *Ctx, h Holder)
}

// Expirer is told when the engine's own countdown removes an instance, so
// it can say so.
type Expirer interface {
	Expired(c *Ctx, h Holder)
}

// Releaser reacts to the item carrying it leaving its holder's hands:
// dropped, or thrown and come to rest. It may destroy the item.
type Releaser interface {
	OnRelease(c *Ctx, r *Release, h Holder)
}

// Enterer reacts to a creature stepping onto the cell where its carrier — a
// feature on the floor, such as a trap — lies.
type Enterer interface {
	OnEnter(c *Ctx, e *Entering, h Holder)
}

// Leaver may keep a creature from moving off its cell — stuck in a pit. It
// is asked only about an actual move: a creature next to an enemy fights
// it, stuck or not.
type Leaver interface {
	OnLeave(c *Ctx, l *Leaving, h Holder)
}

// Appearer changes how an entity looks to others (see Look).
type Appearer interface {
	Appear(l *Look, h Holder)
}

// Describer contributes a line to an entity's status view. ok is false
// when there is nothing to show right now.
type Describer interface {
	Describe(h Holder) (label, value string, ok bool)
}

// Action is an on-use behavior: what happens when an item is used.
type Action interface {
	Params() []ParamDef
	Use(c *Ctx, u *Use)
}

// Asker is an Action that needs one more thing from the player before it
// can act: a single typed character — which monster to summon — or one
// of the items they carry — which to identify. Ask is the question; the
// reply arrives in Use.Answer.
type Asker interface {
	Ask(p Params) Question
}

// Question is what an Asker asks, and what kind of answer it wants.
type Question struct {
	Kind QuestionKind
	Text string
}

// QuestionKind is what kind of answer a Question wants.
type QuestionKind int

const (
	// AskCharacter wants one typed character, case and all.
	AskCharacter QuestionKind = iota
	// AskItem wants one of the items the player carries, other than the
	// one being used.
	AskItem
)

// Answer is the reply to a Question: the character typed, or the item
// chosen.
type Answer struct {
	Char string
	Item *Entity
}

// AI decides a monster's move each turn. Act returns the tile it wants to
// step onto; the engine turns that into a move, a slide or an attack
// exactly as it does for the player. ok false means it stays put.
type AI interface {
	Params() []ParamDef
	Act(c *Ctx, self *Entity, p Params) (x, y int, ok bool)
}

// Option is one thing a creature with the utility AI might do this turn.
// Each turn every option on its menu (Template.Brain) is scored and the
// best is carried out.
type Option interface {
	Params() []ParamDef
	// Score is how much self wants to do this now; 0 means not at all.
	Score(c *Ctx, self *Entity, p Params) float64
	// Act does it. Some options act directly (drink a potion); others
	// name a tile to step onto, which the engine turns into a move — or an
	// attack, if the player stands there — exactly as for any monster.
	Act(c *Ctx, self *Entity, p Params) (x, y int, step bool)
}

// Hurter is an AI that reacts to losing HP, from any source.
type Hurter interface {
	Hurt(self *Entity, p Params)
}

// Registry holds every behavior a template may name.
type Registry struct {
	traits  map[string]Trait
	actions map[string]Action
	ais     map[string]AI
	options map[string]Option
}

// NewRegistry returns an empty Registry.
func NewRegistry() *Registry {
	return &Registry{traits: map[string]Trait{}, actions: map[string]Action{}, ais: map[string]AI{}, options: map[string]Option{}}
}

// AddTrait registers a trait behavior. Registering a name twice is a
// programming error.
func (r *Registry) AddTrait(name string, t Trait) {
	if _, dup := r.traits[name]; dup {
		panic(fmt.Sprintf("rules: trait %q registered twice", name))
	}
	r.traits[name] = t
}

// AddAction registers an on-use action.
func (r *Registry) AddAction(name string, a Action) {
	if _, dup := r.actions[name]; dup {
		panic(fmt.Sprintf("rules: action %q registered twice", name))
	}
	r.actions[name] = a
}

// AddAI registers an AI behavior.
func (r *Registry) AddAI(name string, a AI) {
	if _, dup := r.ais[name]; dup {
		panic(fmt.Sprintf("rules: AI %q registered twice", name))
	}
	r.ais[name] = a
}

// AddOption registers a utility AI option.
func (r *Registry) AddOption(name string, o Option) {
	if _, dup := r.options[name]; dup {
		panic(fmt.Sprintf("rules: option %q registered twice", name))
	}
	r.options[name] = o
}

// Option returns the utility AI option registered under name.
func (r *Registry) Option(name string) (Option, bool) { o, ok := r.options[name]; return o, ok }

// OptionNames lists every registered option, sorted.
func (r *Registry) OptionNames() []string { return sortedNames(r.options) }

// Trait returns the trait behavior registered under name.
func (r *Registry) Trait(name string) (Trait, bool) { t, ok := r.traits[name]; return t, ok }

// Action returns the action registered under name.
func (r *Registry) Action(name string) (Action, bool) { a, ok := r.actions[name]; return a, ok }

// AI returns the AI behavior registered under name.
func (r *Registry) AI(name string) (AI, bool) { a, ok := r.ais[name]; return a, ok }

// TraitNames lists every registered trait, sorted, for error messages.
func (r *Registry) TraitNames() []string { return sortedNames(r.traits) }

// ActionNames lists every registered action, sorted.
func (r *Registry) ActionNames() []string { return sortedNames(r.actions) }

// AINames lists every registered AI, sorted.
func (r *Registry) AINames() []string { return sortedNames(r.ais) }

func sortedNames[T any](m map[string]T) []string {
	names := make([]string, 0, len(m))
	for n := range m {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}
