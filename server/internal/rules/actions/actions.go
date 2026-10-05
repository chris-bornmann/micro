// Package actions holds the built-in on-use actions a template's on_use:
// list names: what happens when a creature uses an item.
//
// This is where special-purpose code goes. When no combination of traits
// can express what an item does — teleporting its user, say — the item
// names an action here instead.
package actions

import (
	"fmt"
	"math/rand"
	"strings"

	"micro/server/internal/rules"
)

// Register adds every built-in action to reg.
func Register(reg *rules.Registry) {
	reg.AddAction("heal", heal{})
	reg.AddAction("grant", grant{reg: reg})
	reg.AddAction("teleport", teleport{})
	reg.AddAction("light", light{})
	reg.AddAction("cure", cure{})
	reg.AddAction("summon", summon{})
	reg.AddAction("identify", identify{})
}

// heal restores amount HP, never above the user's maximum.
type heal struct{}

func (heal) Params() []rules.ParamDef {
	return []rules.ParamDef{{Name: "amount", Kind: rules.ParamInt, Required: true}}
}

func (heal) Use(c *rules.Ctx, u *rules.Use) {
	a := u.Actor
	before := a.HP
	a.HP += u.Params.Int("amount")
	if a.HP > a.MaxHP {
		a.HP = a.MaxHP
	}
	u.Says(fmt.Sprintf("recover %d HP (%d HP now).", a.HP-before, a.HP), "looks healthier.")
}

// grant gives the user a trait for turns turns. describe completes the
// message: "You drink the invisibility potion and <describe>."
type grant struct{ reg *rules.Registry }

func (grant) Params() []rules.ParamDef {
	return []rules.ParamDef{
		{Name: "trait", Kind: rules.ParamTrait, Required: true},
		{Name: "turns", Kind: rules.ParamInt, Required: true},
		{Name: "describe", Kind: rules.ParamString, Required: true},
		// The same, said of a monster: "turns invisible". Without it a
		// monster's use is told without the effect.
		{Name: "describe_they", Kind: rules.ParamString},
	}
}

func (g grant) Use(c *rules.Ctx, u *rules.Use) {
	name := u.Params.String("trait")
	turns := u.Params.Int("turns")
	t, _ := g.reg.Trait(name)
	u.Actor.AddTrait(&rules.TraitInstance{Trait: name, Params: rules.DefaultParams(t), Turns: turns, Source: u.Item.ID})
	// How long it lasts is not said: the player finds out when it wears off.
	they := u.Params.String("describe_they")
	if they != "" {
		they += "."
	}
	u.Says(u.Params.String("describe")+".", they)
}

// teleport moves the user to a random unoccupied tile on the level.
type teleport struct{}

func (teleport) Params() []rules.ParamDef { return nil }

func (teleport) Use(c *rules.Ctx, u *rules.Use) {
	x, y, ok := c.World.RandomUnoccupiedTile()
	if !ok {
		u.Says("nothing happens.", "")
		return
	}
	u.Actor.X, u.Actor.Y = x, y
	u.Says("are teleported away!", "vanishes!")
}

// light lights the item as the user's light source, burning for turns
// turns (see the light_source trait), held in a hand: where it is if
// wielded, else the off hand, else the main hand. Only one light may burn
// at a time, and it needs a free hand; asking for a second, or with both
// hands full, keeps the item, spends no turn and says why.
type light struct{}

func (light) Params() []rules.ParamDef {
	return []rules.ParamDef{{Name: "turns", Kind: rules.ParamInt, Required: true}}
}

func (light) Use(c *rules.Ctx, u *rules.Use) {
	u.Keep = true
	if lit := u.Actor.Lit(); lit != nil {
		u.Refused = true
		u.Msg = "Your " + c.Name(lit) + " is already lit."
		return
	}
	turns := u.Params.Int("turns")
	if !rules.Light(u.Actor, u.Item, turns) {
		u.Refused = true
		u.Msg = fmt.Sprintf("You have no free hand to hold the %s.", u.Name)
		return
	}
	u.Msg = fmt.Sprintf("You light the %s. It should burn for %d turns.", u.Name, turns)
	u.They = fmt.Sprintf("%s lights %s.", u.Actor.Name, rules.A(u.Name))
}

// cure removes every instance of a trait from the user — poison, by a
// healing potion. message says so, but only if there was something to
// cure: a healthy drinker hears nothing about poison.
type cure struct{}

func (cure) Params() []rules.ParamDef {
	return []rules.ParamDef{
		{Name: "trait", Kind: rules.ParamTrait, Required: true},
		{Name: "message", Kind: rules.ParamString},
	}
}

func (cure) Use(c *rules.Ctx, u *rules.Use) {
	name, cured := u.Params.String("trait"), false
	for inst := u.Actor.FindTrait(name); inst != nil; inst = u.Actor.FindTrait(name) {
		u.Actor.RemoveTrait(inst)
		cured = true
	}
	if cured {
		u.Msg = u.Params.String("message")
	}
}

// summon asks which monster to call — by its glyph, typed — and brings one
// to the reader's room (see World.Summon): an ordinary monster is a new
// one, a unique character is the one and only, from wherever it is. With
// several monsters sharing the glyph, one of them comes at random. Nothing
// comes for a glyph no monster has.
type summon struct{}

func (summon) Params() []rules.ParamDef { return nil }

func (summon) Ask(rules.Params) rules.Question {
	return rules.Question{Kind: rules.AskCharacter, Text: "Summon which monster? Type its letter."}
}

func (summon) Use(c *rules.Ctx, u *rules.Use) {
	var matches []*rules.Template
	for _, t := range c.Cat.ByCategory(rules.CategoryMonster) {
		if string(t.Glyph) == u.Answer.Char {
			matches = append(matches, t)
		}
	}
	if len(matches) == 0 || c.World.Summon == nil {
		u.Says("nothing answers your call.", "")
		return
	}
	t := matches[rand.Intn(len(matches))]
	e, ok := c.World.Summon(t)
	if !ok {
		u.Says(fmt.Sprintf("feel that the %s can no longer answer.", t.Name), "")
		return
	}
	u.Says(summoned(e)+" appears!", "")
}

// summoned names a creature that has just been called up: "a goblin", "an
// ogre", or — there being only one — "the Thief".
func summoned(e *rules.Entity) string {
	if e.T.Unique {
		return "the " + e.Name
	}
	return rules.A(strings.ToLower(e.Name))
}

// identify asks which carried item to learn about, and reveals its true
// name: every copy of its type is known from now on. Reading it tells
// the reader what the scroll itself is, too. An item already known is
// refused, and the scroll kept.
type identify struct{}

func (identify) Params() []rules.ParamDef { return nil }

func (identify) Ask(rules.Params) rules.Question {
	return rules.Question{Kind: rules.AskItem, Text: "Learn the true name of which item?"}
}

func (identify) Use(c *rules.Ctx, u *rules.Use) {
	target, id := u.Answer.Item, c.World.Identity
	if target == nil {
		u.Says("nothing happens.", "")
		return
	}
	if id.Known(target.T.ID) {
		u.Refused = true
		u.Msg = fmt.Sprintf("You already know the %s.", target.Name)
		return
	}
	look := c.Name(target)
	id.Identify(target.T.ID)
	id.Identify(u.Item.T.ID)
	u.Says(fmt.Sprintf("learn that the %s is %s.", look, rules.A(target.Name)), "")
}
