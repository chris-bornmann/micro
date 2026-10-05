# micro

A roguelike in Go. The **server** (`server/`, module `micro/server`) owns every
rule of the game. The **clients** (`client/`, module `micro/client`) only draw
frames and send keys:
- **hackterm:** a terminal client;
- **hackgui:** a Fyne desktop client;
- **web** (`client/web`): TypeScript, Vite and Preact.

`PROTOCOL.md` is the contract between the two sides. They share no code, and
that's on purpose. `go.work` exists only so gopls sees both modules.

## Before calling anything done

Run all of these from the repo root unless noted, and say plainly if any
fail:

```sh
(cd server && go vet ./... && go test ./...)
(cd client && go vet ./... && go test ./...)
go list -f '{{.Dir}}' micro/server/... micro/client/... | xargs gofmt -l   # must print nothing
./scripts/smoke.sh                                                       # ends "==> PASS"
(cd client/web && npm run typecheck && npm run lint && npx prettier --check src && npm test)  # if web changed
```

- **Whole-suite runs** can take longer than two minutes. Don't pipe a test
  run into `head` (it cuts the output short), and never leave a command
  waiting on stdin.
- **Flaky tests:** when a test might be flaky (randomness), run it with
  `-count=30`.

## The user's server: hands off

The user keeps their own `mapd` on **port 8080**, often with flags like
`-seed 4 -cheat-mode -show-full-map -reveal-hidden`.
- **Never** kill, restart or rebind it.
- **For a live check,** build to the scratchpad and run on **:8090**, e.g.
  `go build -o $SCRATCH/mapd ./cmd/mapd && $SCRATCH/mapd -addr :8090 ...`.
  Then stop only that process: `pkill -f "$SCRATCH/mapd -addr :8090"`.
- **When you finish,** tell the user to restart their server to pick up
  changes. If you notice it isn't running, say so.

Useful mapd flags:
- `-seed N`
- `-cheat-mode` (1000 HP)
- `-show-full-map`
- `-reveal-hidden`
- `-player path.yaml` (a hand-crafted player, e.g. `server/players/example.yaml`)

## Git

**Commit or push only when asked.** End commit messages with the attribution
line the session provides.

## How the server is built

- **`internal/rules`: the rules engine.**
  - Entities have **traits**. Behavior is Go code registered in
    `rules/traits`, `rules/actions` and `rules/ai`, then configured from
    YAML.
  - Decisions are **queries** folded over every participant: attacker,
    attacker's gear, weapon or projectile, defender, defender's gear.
    Modifiers only add to `Add` or multiply `Mul`, so trait order never
    matters.
  - Keep new mechanics as traits wherever possible. That way they work
    wherever the trait sits: on a creature, on armor, on a ring.
- **`internal/content`: all content is data,** in `content/data/*.yaml`,
  `appearances.yaml` (looks for unidentified items) and `levels/*.yaml`
  (special levels).
  - Loading is **strict**: unknown fields, traits or IDs are errors with the
    file and line.
  - `content/data/README.md` documents every field, trait, action and AI
    option. **Update it whenever you add or change one.**
- **`internal/game`: turns, levels, views, frames and the HTTP-facing
  `Game`.** Clients get pre-formatted text and frames; they never learn
  what an item or monster *is*.
- **The map model is rooms, corridors and doors,** plus walls inside a room
  (mazes). Special levels (hand-drawn, or maze) are built into that same
  model by `internal/levels`. Don't add a second kind of map.

## Rules that are easy to break

- **Item names:** anything that shows a player an item's name or color goes
  through the game's `Identity` (`c.Name(e)`, `Identity.Name/Color`), never
  `e.Name`, because items may be unidentified.
  - Weapon messages say `{weapon}`, not the item's name.
  - The loader refuses an identifiable item whose messages name it.
- **Seeds:** generation is seeded. A given `-seed` must keep producing the
  same levels; `TestOrdinaryLevelsAreAsTheyWere` pins them.
  - Only draw from an RNG when it changes the outcome. For example, roll a
    chance only when it's below 100.
  - If a change *should* alter levels, say so, and update the pins
    deliberately.
- **Turn timing:** `World.Turn` is the turn being played. An attack's
  lingering effect (poison, fire) starts on `c.NextTurn()`.
- **Errors propagate.** Don't swallow a level-generation error, for
  example.

## Tests

- **Name tests as sentences** that state the rule: `TestAShieldIsHeldInTheOffHand`.
- **Fixtures:**
  - `rules/rulestest` provides `Engine(t)`, a fresh engine where every blow
    hits; `New` (attributes zeroed); `Room`; and `TwoRooms`.
  - The game tests use `testPlayer`, `testMonster`, `testItem`,
    `newTestGameState` (nil `Identity`, so everything is known) and
    `mustKey`.
- **The game tests' engine is shared.** Never mutate its catalog in a test;
  change a fresh `rulestest.Engine(t)` instead.
- **Tests own their fixtures.** Don't assert on files the user edits for
  play, such as `server/players/example.yaml`. Use `testdata/`.
- **Randomness:**
  - pin the to-hit roll with `HitRoll`;
  - check ranges with a spread of many trials;
  - reset any built-up state between trials (e.g. `Healing`).
- **Check a new test is meaningful:** break the fix on purpose, watch the
  test fail, then restore it. Check the edit hit the right spot.

## Style

- **Match the surrounding code:** comments explain *why*, in full
  sentences, at the same density as nearby code.
- **No made-up helpers:** use the standard library (`strconv`, `slices`,
  `strings`).
- **gofmt** everything.
- **Web client:**
  - TypeScript strict;
  - state (`src/state`) is kept separate from presentation (`src/ui`,
    `src/render`), with the layering enforced by ESLint import rules;
  - Prettier formatting.

## Working with the user

- **Bigger features:** plan first (plan mode), ask about real design choices,
  and keep scope to what was agreed.
- **Reporting:** say what changed, how it was verified, and what wasn't
  verified, plainly. Mention anything the user edited that affected the
  work, such as content files changed on disk.
