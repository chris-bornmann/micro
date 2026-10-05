package rules

import (
	"fmt"
	"slices"
	"strings"

	"micro/server/internal/throw"
)

// InventoryCapacity is the most items a creature may carry in its
// inventory, not counting what it has equipped.
const InventoryCapacity = 10

// AttemptKind is an action a Vetoer may refuse.
type AttemptKind int

const (
	AttemptPickUp AttemptKind = iota
	AttemptEquip
	// AttemptUnequip is taking an equipped item off, including by
	// equipping something else in its slot.
	AttemptUnequip
	AttemptDrop
	AttemptThrow
)

// Attempt is an action about to happen to Item.
type Attempt struct {
	Kind  AttemptKind
	Actor *Entity
	Item  *Entity
	// Name is what the player calls Item — its look, if it is unidentified
	// — for a refusal to name it by.
	Name string
}

// veto asks the item and the actor (with its gear) whether a may go ahead,
// returning the first refusal.
func (c *Ctx) veto(a *Attempt) string {
	a.Name = c.Name(a.Item)
	ps := []participant{{a.Item, a.Actor, RoleSubject}}
	ps = append(ps, ownParticipants(a.Actor)...)
	for _, ht := range c.Reg.holders(ps) {
		if v, ok := ht.t.(Vetoer); ok {
			if msg := v.Veto(a, ht.h); msg != "" {
				return msg
			}
		}
	}
	return ""
}

// Result is what an item action did. Refused means it did not happen and
// the turn should not be spent; Msg says why, or what happened, to the
// player, and They says it of a monster that did it.
type Result struct {
	Msg     string
	They    string
	Refused bool
}

func refused(msg string) Result { return Result{Msg: msg, Refused: true} }

// PickUp moves item from the floor into actor's inventory. The caller
// removes it from the floor when the result is not Refused and the
// inventory was not full.
func (c *Ctx) PickUp(actor, item *Entity) (Result, bool) {
	if msg := c.veto(&Attempt{Kind: AttemptPickUp, Actor: actor, Item: item}); msg != "" {
		return refused(msg), false
	}
	if len(actor.Inventory) >= InventoryCapacity {
		// Not a refusal: bumping into a full pack is ordinary play and
		// costs the turn, as it always has.
		return Result{Msg: "Your inventory is full."}, false
	}
	actor.Inventory = append(actor.Inventory, item)
	return Result{Msg: fmt.Sprintf("You pick up the %s.", c.Name(item))}, true
}

// Equip moves item from actor's inventory into a slot its template allows
// (see EquipSlotsFor): the first that is free, or else the first whose
// occupant will come off. Whatever is in its way comes off and goes back
// into the inventory: the slot's occupant, anything blocking the slot
// (wielded iron gloves cover the hands), and anything in a slot the item
// itself blocks (Template.Blocks). Anything cursed refuses, and so does
// the equip. A lit torch that has to make way is put out.
func (c *Ctx) Equip(actor, item *Entity) Result {
	if item.T.Slot == "" {
		return refused(fmt.Sprintf("You can't equip the %s.", c.Name(item)))
	}
	slot, refusal := Slot(""), ""
	for _, s := range EquipSlotsFor(item.T.Slot) {
		if len(inTheWay(actor, item, s)) == 0 {
			slot = s
			break
		}
	}
	if slot == "" {
		for _, s := range EquipSlotsFor(item.T.Slot) {
			msg := c.cannotClear(actor, item, s)
			if msg == "" {
				slot = s
				break
			}
			if refusal == "" {
				refusal = msg
			}
		}
	}
	if slot == "" {
		return refused(refusal)
	}
	if msg := c.veto(&Attempt{Kind: AttemptEquip, Actor: actor, Item: item}); msg != "" {
		return refused(msg)
	}
	var notes []string
	for _, prev := range inTheWay(actor, item, slot) {
		if note := putAway(actor, prev); note != "" {
			notes = append(notes, note)
		}
	}
	actor.removeFromInventory(item)
	actor.equipInto(slot, item)
	msg := fmt.Sprintf("You put on the %s.", c.Name(item))
	switch slot {
	case SlotWeapon:
		msg = fmt.Sprintf("You wield the %s.", c.Name(item))
	case SlotOffHand:
		msg = fmt.Sprintf("You hold the %s.", c.Name(item))
	case SlotRightHand, SlotLeftHand:
		msg = fmt.Sprintf("You put the %s on your %s.", c.Name(item), HandName(slot))
	}
	return Result{Msg: strings.Join(append(notes, msg), " ")}
}

// inTheWay is everything that would have to come off for item to go into
// slot on actor: the slot's occupant, whatever blocks the slot, and
// whatever sits in a slot item blocks — each once.
func inTheWay(actor, item *Entity, slot Slot) []*Entity {
	var out []*Entity
	add := func(e *Entity) {
		if e != nil && e != item && !slices.Contains(out, e) {
			out = append(out, e)
		}
	}
	add(actor.Equipped[slot])
	add(actor.blockerOf(slot))
	for _, b := range item.T.Blocks {
		add(actor.Equipped[b])
	}
	return out
}

// cannotClear is why slot cannot be cleared for item — the first thing in
// the way that refuses to come off — or "" if it can.
func (c *Ctx) cannotClear(actor, item *Entity, slot Slot) string {
	for _, e := range inTheWay(actor, item, slot) {
		if msg := c.veto(&Attempt{Kind: AttemptUnequip, Actor: actor, Item: e}); msg != "" {
			return msg
		}
	}
	return ""
}

// putAway takes equipped item off actor and back into its inventory. A lit
// torch is put out first — it is an ordinary torch again — and the note
// says so.
func putAway(actor, item *Entity) (note string) {
	if s, ok := actor.SlotOf(item); ok {
		delete(actor.Equipped, s)
	}
	if lit := item.FindTrait(lightSourceTrait); lit != nil {
		item.RemoveTrait(lit)
		note = "You put out the " + item.Name + "."
	}
	actor.Inventory = append(actor.Inventory, item)
	return note
}

// Release is what a Releaser is told: how an item left its holder's hands
// and where it came to rest. A Releaser may set Destroyed, so the item
// never reaches the floor, and add Notes to what the player is told.
type Release struct {
	How       ReleaseHow
	Actor     *Entity
	Item      *Entity
	X, Y      int
	Destroyed bool
	Notes     []string
}

// ReleaseHow is how an item left its holder.
type ReleaseHow int

const (
	ReleaseDropped ReleaseHow = iota
	// ReleaseThrownHit: thrown, and struck a creature.
	ReleaseThrownHit
	// ReleaseThrownWall: thrown, and stopped by a wall.
	ReleaseThrownWall
	// ReleaseThrownFloor: thrown, and landed on open floor.
	ReleaseThrownFloor
)

// Thrown reports whether r is one of the thrown outcomes.
func (r ReleaseHow) Thrown() bool { return r != ReleaseDropped }

func (c *Ctx) release(r *Release) {
	for _, ht := range c.Reg.holders([]participant{{r.Item, r.Actor, RoleProjectile}}) {
		if rl, ok := ht.t.(Releaser); ok {
			rl.OnRelease(c, r, ht.h)
		}
	}
}

// Drop takes item off actor, wherever it is carried, and returns it placed
// at actor's feet for the caller to put on the floor — or nil if a
// Releaser destroyed it on the way down.
func (c *Ctx) Drop(actor, item *Entity) (Result, *Entity) {
	if msg := c.veto(&Attempt{Kind: AttemptDrop, Actor: actor, Item: item}); msg != "" {
		return refused(msg), nil
	}
	slot, equipped := actor.SlotOf(item)
	if !actor.release(item) {
		return refused(fmt.Sprintf("You aren't carrying the %s.", c.Name(item))), nil
	}
	r := &Release{How: ReleaseDropped, Actor: actor, Item: item, X: actor.X, Y: actor.Y}
	c.release(r)

	msg := fmt.Sprintf("You drop the %s.", c.Name(item))
	switch {
	case r.Destroyed:
	case equipped && slot == SlotWeapon:
		msg += " You are now fighting bare-handed."
	case equipped && slot != SlotOffHand:
		msg = fmt.Sprintf("You take off the %s and drop it.", c.Name(item))
	}
	if len(r.Notes) > 0 {
		msg += " " + strings.Join(r.Notes, " ")
	}
	if r.Destroyed {
		return Result{Msg: msg}, nil
	}
	item.X, item.Y = actor.X, actor.Y
	return Result{Msg: msg}, item
}

// Use is what an Action is told: who is using what, with the action's
// rolled parameters. An action sets Msg, saying what happened to the
// player, and They, saying it of a monster — the user may be either. It
// sets Keep if the item should not be used up.
type Use struct {
	Actor, Item *Entity
	Params      Params
	// Name is what the player calls Item: its look, if it is unidentified.
	Name string
	// Answer is the player's reply to the action's question, for an
	// action that asks one (see Asker).
	Answer Answer
	Msg    string
	They   string
	Keep   bool
	// Refused, set by an action, means the use does not happen after all:
	// the item is kept, no turn passes, and Msg says why.
	Refused bool
}

// Says phrases a use both ways: "You drink the healing potion and <you>"
// and "The thief drinks the healing potion and <they>". An empty they
// leaves the monster's sentence at "The thief drinks the healing potion."
func (u *Use) Says(you, they string) {
	verb := u.Item.T.UseVerb
	if verb == "" {
		verb = "use"
	}
	u.Msg = fmt.Sprintf("You %s the %s and %s", verb, u.Name, you)
	u.They = fmt.Sprintf("%s %ss %s", capitalize(the(u.Actor)), verb, A(u.Name))
	if they == "" {
		u.They += "."
	} else {
		u.They += " and " + they
	}
}

// Usable reports whether item does anything when used.
func Usable(item *Entity) bool { return len(item.Uses) > 0 }

// Use runs every on-use action item has, then removes item from actor
// unless an action kept it.
func (c *Ctx) Use(actor, item *Entity) Result { return c.UseWith(actor, item, Answer{}) }

// Question is what using item asks first — "Summon which monster?",
// "Learn the true name of which item?" — if one of its actions asks
// anything (see Asker). The reply goes to UseWith.
func (c *Ctx) Question(item *Entity) (Question, bool) {
	for _, ai := range item.Uses {
		if act, ok := c.Reg.Action(ai.Action); ok {
			if a, ok := act.(Asker); ok {
				return a.Ask(ai.Params), true
			}
		}
	}
	return Question{}, false
}

// UseWith is Use, with answer — the reply to the item's Question.
func (c *Ctx) UseWith(actor, item *Entity, answer Answer) Result {
	if !Usable(item) {
		return refused(fmt.Sprintf("You can't use the %s.", c.Name(item)))
	}
	var msgs, theirs []string
	keep := false
	for _, ai := range item.Uses {
		act, ok := c.Reg.Action(ai.Action)
		if !ok {
			continue
		}
		u := &Use{Actor: actor, Item: item, Params: ai.Params, Name: c.Name(item), Answer: answer}
		act.Use(c, u)
		if u.Refused {
			return refused(u.Msg)
		}
		if u.Msg != "" {
			msgs = append(msgs, u.Msg)
		}
		if u.They != "" {
			theirs = append(theirs, u.They)
		}
		keep = keep || u.Keep
	}
	if !keep {
		actor.release(item)
	}
	res := Result{Msg: strings.Join(msgs, " "), They: strings.Join(theirs, " ")}
	// The player's use is reported by whoever asked for it; a monster's
	// happens during its turn, so it is told as an event.
	if !actor.IsPlayer() && res.They != "" {
		c.Say(actor, res.Msg, res.They)
	}
	return res
}

// Throwable is everything actor may throw: a lit torch it holds, if any,
// then its inventory. Other equipped items must be taken off first.
func Throwable(actor *Entity) []*Entity {
	var out []*Entity
	if l := actor.Lit(); l != nil {
		out = append(out, l)
	}
	return append(out, actor.Inventory...)
}

// ThrowResult is what a throw did.
type ThrowResult struct {
	Result
	// Target is the creature hit, if any, and Outcome what the hit did.
	Target  *Entity
	Outcome Outcome
	// Landed is the item at rest on the floor, for the caller to place;
	// nil if it was destroyed.
	Landed *Entity
	// Notes are what the item's own Releasers said about how it landed.
	Notes []string
}

// Throw throws item from actor's hands in direction (dx, dy). It flies up
// to throw.Distance cells and stops at the first wall or creature. Hitting
// a creature attacks it (see ThrownHit). Wherever it stops, its Releasers
// decide whether it survives to land there.
func (c *Ctx) Throw(actor, item *Entity, dx, dy int) ThrowResult {
	if msg := c.veto(&Attempt{Kind: AttemptThrow, Actor: actor, Item: item}); msg != "" {
		return ThrowResult{Result: refused(msg)}
	}
	actor.release(item)

	distance := throw.Distance(item.Weight, actor.Attrs.Strength)
	how := ReleaseThrownFloor
	var target *Entity
	x, y := actor.X, actor.Y
	obstacles := c.World.obstacles(actor)
flight:
	for i := 0; i < distance; i++ {
		nx, ny := x+dx, y+dy
		if !c.World.Map.CanMove(x, y, nx, ny) {
			how = ReleaseThrownWall
			break
		}
		for _, o := range obstacles {
			if o != actor && o.X == nx && o.Y == ny {
				target, how = o, ReleaseThrownHit
				x, y = nx, ny
				break flight
			}
		}
		x, y = nx, ny
	}

	res := ThrowResult{Result: Result{Msg: fmt.Sprintf("You throw the %s.", c.Name(item))}, Target: target}
	r := &Release{How: how, Actor: actor, Item: item, X: x, Y: y}
	c.release(r)
	res.Notes = r.Notes
	if target != nil {
		res.Outcome = c.ThrownHit(actor, item, target)
	}
	if !r.Destroyed {
		item.X, item.Y = x, y
		res.Landed = item
	}
	return res
}

// lightSourceTrait is the trait a burning torch carries.
const lightSourceTrait = "light_source"

// Light sets item burning for turns turns (the light_source trait), in a
// hand: where it is, if actor wields it; else the off hand, if free; else
// the main hand, if free. It reports false, lighting nothing, if actor has
// no free hand to hold it.
func Light(actor, item *Entity, turns int) bool {
	slot, wielded := actor.SlotOf(item)
	if !wielded {
		switch {
		case actor.Equipped[SlotOffHand] == nil && actor.blockerOf(SlotOffHand) == nil:
			slot = SlotOffHand
		case actor.Equipped[SlotWeapon] == nil:
			slot = SlotWeapon
		default:
			return false
		}
		actor.removeFromInventory(item)
		actor.equipInto(slot, item)
	}
	item.AddTrait(&TraitInstance{Trait: lightSourceTrait, Params: Params{}, Turns: turns})
	return true
}

// HandName is "right hand" or "left hand" for a ring slot.
func HandName(s Slot) string {
	if s == SlotLeftHand {
		return "left hand"
	}
	return "right hand"
}
