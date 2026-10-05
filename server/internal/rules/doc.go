// Package rules is the game's rules engine: the one place that decides what
// happens when entities act on each other.
//
// Every player, monster and item is an *Entity built from a *Template, and
// templates are data (see package content). What an entity does beyond the
// core rules comes from its traits: each TraitInstance names a behavior in a
// Registry, and a behavior is Go code that hooks into specific decision
// points — a number being computed (Modifier), an action that may be refused
// (Vetoer), a hit landing (HitHandler), a turn passing (Ticker), a thrown or
// dropped item coming to rest (Releaser), and how an entity appears to
// others (Appearer).
//
// The engine's job is to gather every trait that has a say in a decision
// before making it. An attack consults the attacker, the attacker's gear,
// the weapon or projectile, the defender and the defender's gear, each
// tagged with the Role it plays (see gather). Modifiers only ever add to or
// multiply a Query's accumulators, and addition and multiplication commute,
// so the result does not depend on the order traits are visited in.
//
// What stays in Go and out of traits is the core: turn order, movement and
// sliding, the Strength bonus, death and corpses, inventory capacity and
// throwing distance. Those live in this package with their reasons.
//
// Built-in behaviors live in subpackages (rules/traits, rules/ai,
// rules/actions) and are added to a Registry by their Register functions;
// that split is the extension point for new code.
package rules
