# Game content

Every player, monster and item is a **template** in one of these YAML files.
The file a template lives in does not matter; its key is its ID, and IDs are
unique across all files. The server checks everything here when it starts
and refuses to run on a mistake, naming the file and line. The one
exception is `appearances.yaml`, which holds the looks of unidentified
items rather than templates (see "Unidentified items").

A template's fields set its basics. Its **traits** give it behavior: each
trait names a behavior written in Go (`server/internal/rules/traits`) and
configures it. When the game decides anything, it asks every trait involved.
In an attack that means the attacker, the attacker's gear, the weapon, the
defender and the defender's gear, so a trait never needs to know who else
is in the fight.

## Adding a monster

```yaml
giant_rat:
  category: monster
  name: Giant rat
  glyph: r
  color: [150, 120, 90]
  weight: 10                 # the player weighs 100
  level: 2                   # its power, and the center of its spawn curve
  hp: [3, 6]                 # rolled when it spawns
  attributes: [1, 4]         # Strength etc., each rolled from this range
  attack: {name: bite, damage: {piercing: [1, 3]}}
  ai: {behavior: chase_and_flee, flee_at_percent: 30}
  spawn: {model: common, density: 4, sigma_up: 3, sigma_down: 1, max_above: 1}
  traits:
    - {trait: resist, type: fire}
```

That is all it takes: it spawns, hunts, fights, flees and leaves a corpse.
`server/internal/content/extend_test.go` proves this for exactly this rat.

## Adding an item

```yaml
flaming_sword:
  category: weapon
  name: flaming sword
  glyph: ")"
  color: [255, 120, 0]
  weight: 5
  slot: weapon
  damage: {slashing: [1, 4], fire: 2}
  traits:
    - {trait: enchant, amount: 1}
  spawn: {model: fixed, count: 1}
```

## Fields

| Field | For | Meaning |
|---|---|---|
| `category` | all | `player`, `monster`, `weapon`, `armor`, `potion`, `scroll`, `ring`, `amulet`, `torch`, `trap`, `flask`, `stairs` or `corpse`. Only `player` and `monster` change what the engine does. |
| `name` | all | Display name. |
| `kind` | all | The tag `bonus_vs` matches against. Defaults to the ID. |
| `glyph`, `color` | all | One character, and `[r, g, b]`. |
| `layer` | all | Draw order when things share a tile. Defaults: player 2, monsters 1, items 0. |
| `weight` | all | Affects how far and how hard it can be thrown. |
| `level`, `hp`, `attributes`, `attack` | creatures | Power level; HP (a number, or a range to roll); attributes; natural attack used with nothing wielded. `attributes` is one number or range for all four, or each set apart: `{strength: 18, intelligence: [6, 20], dexterity: 15, stamina: [6, 20]}` (all four required). |
| `ai` | monsters | `{behavior: name, ...parameters}`. |
| `equipment` | monsters | Starting items: `- {item: rusty_sword, limit_per_level: 1}`. The limit caps how many of this monster on one level get the item; 0 means all of them. Items fill slots in the order listed, and one whose slot is already taken is skipped — so a sword limited to 1 then a dagger limited to 2 arms the first goblin with the sword and the second with the dagger. `chance` (percent, default 100) makes an item occasional: `{item: helm, chance: 30}`. |
| `leaves_corpse` | monsters | Default `true`. |
| `inventory` | creatures | Items carried, unequipped: `- {item: healing_potion, chance: 50}` (`chance` defaults to 100). |
| `attitude` | creatures | How it starts out toward the player: `hostile` (default), `neutral` or `afraid`. |
| `unique` | monsters | A unique character: see "Unique characters". |
| `returns` | unique monsters | `{chance: 40}`: the chance a character who left a level turns up on each new, deeper one. |
| `brain` | monsters | The options of the `utility` AI: see "Unique characters". |
| `slot` | items | Where it is equipped: `weapon` (the main hand), `off_hand` (a shield), `head`, `body`, `hands`, `feet`, `neck`, `cloak`, or `ring` — either hand's fingers, at most two rings at once. A ring goes on a free hand, or else swaps out one that will come off. See "Hands". |
| `blocks` | items | Other slots it takes up while equipped: iron gloves `blocks: [hands]`, so no gauntlets while they're wielded (and the other way round); a two-handed weapon would have `blocks: [off_hand]`. |
| `unidentified` | items | `{pool: ring, chance: 100}`: the item may be seen only by a look from that pool (`appearances.yaml`) until its type is identified. `chance` (default 100) is the percent of games it starts unknown. Without it, always known. See "Unidentified items". |
| `damage` | weapons | `{type: range, ...}`. The first part is the weapon's main damage, the one enchantments and curses shift. Types: `slashing`, `piercing`, `bludgeoning`, `fire` (see Damage types). |
| `on_use` | items | Actions run when the item is used (the `A` key). The item is used up unless an action keeps it. |
| `use_verb` | items | How using it reads: `drink`, `read`. Defaults to `use`. |
| `stairs` | stairs | `up` or `down`. |
| `pronoun` | all | `he`, `she`, `it` (default) or `they`, for messages. |
| `messages` | all | How melee attacks are described — see below. |
| `spawn` | all but the player | How many appear per level. `{model: fixed, count: N}` places N everywhere. `common` and `single_shot` follow a curve around `level` (see `server/internal/spawn`). `{model: placed}` never spawns at random: only where a drawn level puts it (see "Special levels"). |

A number may be written as a range, `[min, max]`. A range is rolled once,
when the entity is created: every healing potion heals a fixed amount, but
not the same amount as every other potion. This works for parameters too.

## Hands

Every creature has two hands. The main hand (`weapon`) holds whatever it
fights with; the off hand (`off_hand`) holds a shield, or a lit torch
carried alongside a weapon. Rings are on the fingers and don't count.

A torch can be wielded — a club, 1-2 bludgeoning — or lit. Lit, it lights
the way from either hand; wielded and lit, its blows add 1-2 fire and set
anything oily or `flammable` burning. Lighting a torch puts it in the
hand it is in, if wielded; else the off hand; else the main hand. With a
weapon *and* a shield, there is no free hand, and it can't be lit. When
something else takes a lit torch's hand, the torch is put out and goes
back into the pack.

So: a weapon and a lit torch; or a shield and a wielded lit torch; or a
weapon and a shield, in the dark.

## Special levels

Most levels are made by the usual generator: rooms joined by corridors.
`levels/*.yaml` adds special ones, each settled once per game:

```yaml
throne_room:
  depth: 10              # or depths: [first, last] — one is picked per game
  chance: 100            # percent of games that have it (default 100)
  map: |2                # drawn by hand ("|2" keeps the leading spaces)
    ...
  legend: {K: king, w: royal_guard}
  populate: false        # what it also gets at random, by depth (see below)

maze:
  depths: [4, 8]
  chance: 60
  generator: maze        # made afresh each game
  braid: 10              # percent of dead ends opened into loops
```

No two special levels share a depth; one whose range is taken goes to the
next free depth in it, or is left out.

**Drawing a level.** Every character is one tile:

| Character | Is |
|---|---|
| `.` / `:` | floor, lit / dark (any `:` darkens its room) |
| `#` | wall |
| `+` / `S` | door / hidden door |
| `,` | corridor |
| (space) | solid rock |
| `<` `>` | the stairs (unless the legend says otherwise) |
| anything else | what the legend places there, standing on floor |

A room is a rectangle of floor ringed by walls and doors; walls *inside*
it split it up (pillars, a maze), and sight there follows lines of sight.
A corridor runs from one door to another without branching; a door in a
wall two rooms share joins them directly. Mistakes stop the server with
the line and column.

**Depth.** `depth: 10` puts the level at exactly depth 10 in every game
(with `chance` 100, the default); `depths: [4, 8]` lets each game pick.

**What else is on it.** A drawn level starts with only what its legend
places. `populate` adds random spawns, chosen by the level's depth
exactly as on an ordinary level: `true` for everything, or a list of
`monsters`, `items` and `traps` — `populate: [monsters]` for a hand-made
layout with the usual monsters for its depth, its items all placed by
hand. A maze is populated unless it says `populate: false`. Stairs a
drawn level doesn't place go anywhere, as usual.

A monster that should appear nowhere else spawns with
`{model: placed}` (the King, the royal guards); a unique one placed by
hand counts as having appeared.

**Mazes** are one big dark room walled into passages, the way down as far
from the way in as it gets. They are populated at random like any level.
In a maze you see only what your light reaches (a torch: 3 tiles), and a
monster notices you only within 4 tiles with no wall between.

## Unidentified items

Most items can be seen only by how they look — "an agate ring", "a bubbly
potion", "a notched dagger" — until their type is identified. An item opts
in with `unidentified: {pool: ..., chance: ...}`; a torch, without it, is
always a torch.

- **Per type, per game.** When a game starts, each such type rolls its
  `chance` once: it starts that game unknown, or known. Every copy of a
  type looks the same, and identifying one identifies them all.
- **Shuffled every game.** Each pool's looks are dealt out afresh (from
  the game's seed), so a jade ring is a different ring in the next game.
  Two types in one pool never share a look.
- **Looks are drawn in their own color**, not the type's, so the color
  gives nothing away either.
- **Your own kit is known.** Whatever the player starts with is
  identified from the start.
- **Messages must not give it away.** An item that can be unidentified
  may not name itself in its `messages`; write `{weapon}`, which becomes
  whatever the player calls it — "Your notched dagger slips between the
  goblin's ribs". The server refuses to start otherwise.

`appearances.yaml` holds the pools:

```yaml
ring:
  format: "{} ring"            # "{}" becomes the look's name
  color: [200, 200, 200]       # for any look without its own
  looks:
    - {name: jade, color: [0, 168, 107]}
    - agate                    # the pool's color
```

A pool needs at least as many looks as item types use it. A type is
identified by reading a scroll of knowledge on it (the `identify` action);
`rules.Identity.Identify` is the hook for any other way.

## Testing with a hand-crafted player

`player.yaml` is the built-in player. To test a scenario, write a player of
your own — any HP, attributes, `equipment`, `inventory` and `traits` — and
start the server with it in place of the built-in one:

```sh
go run ./cmd/mapd -player players/example.yaml
```

`server/players/example.yaml` shows every option. The file is checked as
strictly as this directory; a mistake, or a path that doesn't exist, stops
the server at startup. It can use anything defined here by ID and define new
things of its own, but not redefine anything here. Without `-player`, the
built-in player is used.

A trait in any template may also have `duration: n`: it lasts n turns, then
wears off, where without it, it is there for good — a player who starts
`{trait: poisoned, damage: 2, duration: 5}`, say.

## Unique characters

A unique character is a monster with `unique: true`:
- **At most one in a run.** It spawns with model `single_shot`.
- **An upper-case glyph.** The loader enforces both ways: a unique
  monster must have one, and an ordinary monster must not.
- **It can leave a level alive** (the Thief escaping), carrying
  everything it has and remembering how you treated it. It may come back
  on a deeper level (`returns`).

These characters think with the **`utility` AI**: `ai: {behavior: utility}`
plus a `brain`, which is a list of options. Each turn, every option says
how much it wants to act, multiplied by its `weight` (default 1). The most
eager one acts; ties go to the option listed first. Any monster can use
it, not only unique ones.

| Option | Params | Wants to act when… | Base score |
|---|---|---|---|
| `drink_to_heal` | `below_hp_percent` (40) | hurt, and carrying something whose use heals | 1.5 |
| `read_to_escape` | `below_hp_percent` (40) | hurt, and carrying something whose use teleports | 1.4 |
| `flee` | `below_hp_percent` (0), `when_afraid` | hurt, or (with `when_afraid`) afraid: runs for a room the player isn't in, or backs away if it can't get out. Running because it is hurt makes it afraid | 1.3 |
| `attack` | — | hostile, and next to the player | 1.0 |
| `approach` | `until_adjacent` | hostile: toward where the player was last seen. Otherwise, with `until_adjacent`, sidling up beside them | 0.5 |
| `wander` | — | always | 0.1 |
| `wait` | — | always | 0.01 |
| `retaliate` | `fight_chance` (50), `afraid_fight_chance` (0) | the player attacked it this turn, and it isn't already hostile: fights back (hostile) with `fight_chance`, or `afraid_fight_chance` if it was afraid; otherwise it is afraid and flees. Never fights when hurt enough for its own `flee` to run | 3.0 |
| `steal` | `chance` (50) | neutral, and next to a player with something in their pack: takes one item, silently, then turns afraid | 1.0 |
| `escape` | — | afraid, and out of the player's sight: leaves the level | 2.0 |

Options find items by what they do, not by name, so a new healing
potion works without changes. Seeing works like the player's own view:
sharing a room, with a doorway counting, except to notice a sneaker.
There is no geometric line of sight.

A new option is Go code: implement `rules.Option` (`Score` and `Act`) and
register it in `rules/ai`.

## Traps

A trap is a feature on a floor cell, placed when its level is made, like
the stairs. Its template lives in `traps.yaml`, with `category: trap`, and
carries three traits:

- **`hidden`:** undiscovered until sprung, so the cell looks like floor.
  The ring of true sight shows hidden traps, and `-reveal-hidden` draws
  them in magenta.
- **`fixed`:** never picked up, and remembered on the map once seen.
- **Exactly one effect trait:**

| Trait | Params | What it does |
|---|---|---|
| `pit` | `min` (default 1), `per_level` (default 1) | Bludgeoning damage from `min` up to depth × `per_level`, so deeper pits hurt more; then the player is stuck. Each try to move off the cell climbs out with a 50% chance, and otherwise wastes the turn. A monster next to them is fought as usual. |
| `dart` | `hit_chance`, `damage` (rolled each time), `poison_damage`, `poison_turns` | Hits with `hit_chance` − Dexterity percent (kept within 5–95), for piercing damage, and poisons if `poison_damage` is set. |
| `trap_door` | — | Drops the player to a random free spot on the level below. Does nothing on the last level. |
| `teleport_trap` | — | Moves the player to a random free spot on the same level. |

Every effect trait also takes **`avoid_known`** (default 75): once the
player has found a trap, it is the percent chance they step around it
instead of setting it off. A hidden trap always springs, and springing it
reveals it as `^`. Traps never affect monsters.

Traps spawn by their own model:

```yaml
spawn: {model: per_level, levels: [3, 24], chance: 25, count: [1, 2], always_on: [5]}
```

On each level from `levels`' first to last, the trap appears with `chance`
percent, `count` times (a number or a range). Every level in `always_on`
gets at least one. Traps never share a cell with the stairs, a monster or
each other, and never with the player's start.

**Writing a new trap** — or an altar, or anything else that lives on a
cell — means writing a trait that implements `rules.Enterer` (a creature
stepped onto its cell). Something that holds a creature in place, like
`in_pit`, implements `rules.Leaver`. See `rules/traits/traps.go`.

## Damage types

Every attack deals one or more of **slashing**, **piercing**, **bludgeoning**,
**fire** and **poison**. A sword slashes, a dagger pierces, fists and clubs bludgeon. A
monster's `resist` and `vulnerable` traits decide how much of each it takes,
so damage depends on what hits what. The slime:

```yaml
traits:
  - {trait: resist, type: slashing, multiplier: 0.25}    # blades pass through
  - {trait: resist, type: piercing, multiplier: 0.5}
  - {trait: vulnerable, type: bludgeoning, multiplier: 1.5}
  - {trait: vulnerable, type: fire, multiplier: 3}
```

A rusty sword does it 0–1 damage, a fine dagger 1–3, your fists 2–3, and
burning oil 6–12 a turn. Multipliers from several traits (the monster's own,
its armor's) multiply together.

**`physical`** is shorthand in `resist` and `vulnerable` for all three
physical types at once — `{trait: resist, type: physical}` on plate armor.
An attack in a template can't deal `physical`; it has to say which kind.
Only a thrown item deals plain physical damage, so only a resistance to
physical damage as a whole applies to it.

## Hitting and missing

A melee blow lands with a chance of **50%, plus the attacker's Dexterity,
minus the defender's**, adjusted by traits (`accuracy`, `evasion`, an unseen
`invisible` defender), and kept **between 20% and 90%**. A miss does nothing
at all. Thrown items always hit. The numbers are `hitBase`, `hitFloor` and
`hitCeiling` in `server/internal/rules/combat.go`.

## Attack messages

A template's `messages` describe melee attacks: on a creature, its own
attack; on an item, attacks made with it. One line is picked at random each
time.

```yaml
messages:
  hit:        # it attacks you, and hits
    - "{The attacker} strikes you with {his} rusty sword for {damage} hp of damage!"
    - "You are sliced by {the attacker's} rusty sword for {damage} hp of damage!"
  miss:       # it attacks you, and misses
    - "{The attacker} swings {his} rusty sword at you, but misses!"
  you_hit:    # you attack with it, and hit
    - "Your rusty sword bites into {the defender} for {damage} hp of damage."
  you_miss:   # you attack with it, and miss
    - "Your rusty sword swings wide of {the defender}."
```

A monster has only `hit` and `miss`, since it only ever attacks you; the
player has only `you_hit` and `you_miss`; an item may have all four. The
wielded weapon's lines win over the attacker's own, and with neither the
game uses its plain lines ("Goblin takes 3 damage (4 HP left)"). A killing
blow adds "The goblin dies!" or "You die!".

Placeholders — capitalize the first letter for the start of a sentence:

| Placeholder | Becomes |
|---|---|
| `{the attacker}`, `{the defender}` | "the goblin", or "you" |
| `{the attacker's}`, `{the defender's}` | "the goblin's", or "your" |
| `{his}`, `{he}` | the attacker's pronoun: his/her/its/their, he/she/it/they |
| `{damage}` | the damage done |
| `{hp_left}` | the defender's HP afterward |
| `{weapon}` | the weapon's name, or the attack's own (`fangs`) |

Quote every line: YAML reads an unquoted `{` as the start of a mapping.

## Healing

Every creature, the player and monsters alike, heals slowly while alive and
hurt. Healing builds up in points, 600 to the HP, so HP come back one at a
time:

```
natural = min(Strength + Stamina + 5, 60)    points a turn — attributes alone
rate    = (natural + Add) × Mul              traits: never below 0
```

| Strength + Stamina | Points a turn | 1 HP every |
|---|---|---|
| 0 | 5 | 120 turns |
| 8 (a typical goblin) | 13 | 46 |
| 25 (a typical player) | 30 | 20 |
| 40 | 45 | 13 |
| 55 or more | 60 (the cap) | 10 |

The cap binds attributes only: magic (a ring of regeneration, ×2) can heal
faster. Nothing is random, and nothing builds up at full health. The
status view shows the rate ("Healing: 1 HP / 20 turns").

Traits take part wherever they are — on the creature, or on anything it
wears or wields: `regeneration` speeds healing up, slows it or stops it;
`poisoned` stops it while it lasts; `wasting` wears HP away. The numbers
are in `internal/rules/regen.go`.

## Traits

| Trait | Parameters | What it does |
|---|---|---|
| `enchant` | `amount` | Shifts a weapon's main damage by `amount`. |
| `bonus_vs` | `kind`, `amount` | Same, but only against defenders of that `kind`. |
| `cursed` | `damage` (default 0) | Shifts a weapon's main damage, **and** once equipped the item cannot be unwielded, taken off, swapped, dropped or thrown — a weapon, armor or ring alike. |
| `vampiric_attack` | `drain_percent` (0–100), `message` | Each hit heals the attacker by that share of the damage done (rounded, never above maximum HP). Counts on the attacker itself, its weapon, or anything it wears; a thrown item drains nothing. `message` replaces the "Vampire drains 2 HP." line (`{amount}` is the HP drained); `""` says nothing, for when the attack's own messages already do. |
| `accuracy` | `amount` | Adds to the chance of the bearer's blows landing — on the attacker, its weapon or its gear. |
| `evasion` | `amount` | Takes that much off the chance of blows landing on the bearer or whoever wears it — a shield; negative, cursed armor makes its wearer easier to hit. |
| `armor` | `amount` | Takes that much off every physical part (slashing, piercing, bludgeoning) of a hit on the bearer or whoever wears it, down to nothing ("the blow glances off"); fire and poison get through. It stacks across pieces and comes off before a resistance. Negative, it adds damage (cursed armor). |
| `vulnerable` | `type`, `multiplier` (default 2) | Multiplies damage of that type taken by whoever carries or wears it. `physical` means slashing, piercing and bludgeoning together. |
| `resist` | `type`, `multiplier` (default 0.5) | The same, defaulting to half. 0 is immunity. |
| `stat` | `stat`, `amount` | Adds to a named stat of whoever carries or wears it. |
| `invisible` | `visible_below` (default 0), `periodic_visible` (default 0), `evasion` (default 20) | Nobody can see its bearer — monsters can't see an invisible player, and the player can't see an invisible monster. It shows while its HP is below `visible_below` (0: never while alive), and on `periodic_visible` percent of turns, rolled once a turn. While unseen, blows at it are `evasion` points less likely to land. A corpse is always visible. `-reveal-hidden` draws invisible monsters in magenta. A potion grants it for a time with both at 0. |
| `monster_vision` | — | Its bearer senses every live monster on the level, wherever it is — through fog of war, and even invisible ones — though no more of the map than they have explored. Worn, it lasts; granted for a time (the scroll of awareness), it fades with a message. |
| `see_invisible` | — | Its bearer sees what is hidden: hidden doors as ordinary doors, invisible monsters as themselves (fog of war still applies). An invisible defender is no harder for them to hit, and a monster with it can see an invisible player. |
| `sneaking` | — | Its bearer is not noticed from a doorway. |
| `fixed` | — | Cannot be picked up; remembered on the map once seen. |
| `fragile` | `hit`, `wall`, `floor` messages | Breaks when thrown, saying how. |
| `spills_oil` | — | Thrown onto open floor, leaves it oily; anyone stepping there may slide. |
| `poisons` | `damage`, `turns` (≥ 1) | A blow that lands poisons the defender: `damage` a turn for `turns` turns, less the victim's Stamina ÷ 5 (rounded up). If that or their resistance to poison leaves nothing, it doesn't take. Counts on the weapon, or on the attacker itself (a venomous bite); a thrown item poisons nothing. Poisoned again, the worse of the two wins. |
| `poisoned` | `damage` | Being poisoned: `damage` poison damage each turn until it wears off, with no natural healing meanwhile. It can kill. Usually given by `poisons`; written into a template with no time limit, it never ends. |
| `regeneration` | `multiplier` (1, ≥ 0), `amount` (0) | Changes how fast its bearer heals (see Healing): the rate times `multiplier`, plus `amount` points a turn. `0` never heals (the undead, the vampire), `3` the slime, `2` a ring of regeneration. On a creature or anything it wears or wields. |
| `wasting` | `every` (≥ 1), `amount` (1), `type`, `message` | Wears its bearer away: `amount` damage of `type` every `every` turns, said with `message`. Damage like any other: resistances count, and it can kill. On a worn item, it is the wearer who wastes (the amulet of wasting). |
| `adds_oil` | — | Whoever it hits gets one more stack of oil. |
| `ignites` | — | Whoever it hits catches fire, if they are oily or flammable. |
| `flammable` | `turns` | Burns without oil: any fire that touches it — a lit torch, struck with or thrown — sets it burning for `turns`. The mummy. |

The engine also uses `oily`, `on_fire` and `light_source` itself: oil
stacks, burning, and a lit torch. They are not meant to be written in a
template.

## Actions (`on_use`)

| Action | Parameters | What it does |
|---|---|---|
| `heal` | `amount` | Restores HP, never above the maximum. |
| `grant` | `trait`, `turns`, `describe` | Gives the user a trait for a number of turns: "You drink the potion and *describe*." How long it lasts is never said; the player finds out when it wears off. |
| `teleport` | — | Moves the user to a random free tile. |
| `light` | `turns` | Lights the item as the user's light source. |
| `cure` | `trait`, `message` | Removes every instance of that trait from the user, saying `message` if there was anything to remove. The healing potion cures `poisoned`. |
| `summon` | — | Asks the reader for a monster's glyph (typed; case matters) and brings one into their room — or beside them, out of a room. An ordinary monster is a new one; a unique one is *the* one, taken from wherever it is (this level, another, or gone for now); a dead one can't come. Several monsters sharing the glyph: one at random. No monster has it: nothing comes. The scroll is used up either way, unless the reader cancels with Escape. |
| `identify` | — | Asks which of the reader's other items to learn about (a letter picker of their inventory) and reveals its true name: every item of that kind is known from then on, and so is the scroll itself. An item already known is refused and the scroll kept; Escape cancels. The scroll of knowledge. |

## AI behaviors (`ai`)

| Behavior | Parameters | What it does |
|---|---|---|
| `chase` | — | Waits until it shares a room with the player, then closes in and attacks. |
| `chase_and_flee` | `flee_at_percent` (default 10) | Like `chase`, but runs to another room when a hit leaves it at or below that share of its maximum HP. |

## When YAML is not enough

A new kind of behavior is Go code: a type registered by name, which
templates can then use like any built-in.

- **A trait** implements `rules.Trait` (its parameters, where an integer
  may carry `Min` and `Max` bounds that the loader enforces) plus whichever hooks
  it needs. `Modifier` adds to a number being computed. `Vetoer` refuses an
  action. `HitHandler` reacts to a hit. `Ticker` acts each turn. `Releaser`
  acts when its item is dropped or thrown. `Appearer` changes how its bearer
  looks. `Describer` adds a line to the status view. Register it in
  `rules/traits/traits.go`.
- **An action** implements `rules.Action`; register it in
  `rules/actions/actions.go`.
- **An AI** implements `rules.AI`; register it in `rules/ai/ai.go`.

A trait decides whether it applies from the **role** its bearer plays in
the decision: `RoleWeapon` for the weapon an attack is made with,
`RoleDefender` for whoever is being hurt, and so on (see
`server/internal/rules/query.go`). Modifiers only add to a query's `Add` or
multiply its `Mul`, never overwrite them. That way the result is the same
whatever order the traits are asked in.
