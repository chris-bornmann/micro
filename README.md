# micro

A roguelike, split into a server that owns the game and a client that draws it.

```
go.work              two modules, no shared third one
server/              the game: HTTP API + all logic          (module micro/server)
  internal/rules/    the rules engine: entities, traits, combat, turns
  internal/content/  every monster and item, as YAML
client/              display and input                        (module micro/client)
  web/               the browser client (TypeScript, Vite)    see client/web/README.md
PROTOCOL.md          the contract between them
api.http             play the game with no client at all
scripts/smoke.sh     end-to-end check: real server, real client, real socket
```

## Running it

In VS Code, open **this** folder (not `server/` or `client/`) and press F5 →
**Server + terminal client**, or **Server + windowed client**. `go.work` is what
lets gopls treat both modules as one project, so go-to-definition and rename
work across the boundary in one window.

By hand:

```sh
cd server && go run ./cmd/mapd                    # :8080
cd client && go run ./cmd/hackterm                # terminal
cd client && go run ./cmd/hackgui                 # window
```

Either client waits a few seconds for the server, so the order does not matter.

The web client runs in a browser and calls the server from another origin:

```sh
cd server && go run ./cmd/mapd -cors-origin http://localhost:5173
cd client/web && npm install && npm run dev       # http://localhost:5173
```

## Testing

```sh
cd server && go test -short ./...    # ~7s   — the inner loop
cd server && go test ./...           # ~22s  — adds four seed sweeps
cd client && go test ./...           # ~2.5s
./scripts/smoke.sh                   # both halves over a real socket
```

`go test ./...` does **not** work from this directory: the workspace root holds
no module of its own. Use a module path (`go test micro/server/...`) or `cd`
into one. Every VS Code task sets an explicit `cwd` for this reason.

## The two rules

**The server sends a rasterized character grid, never a level.** The client
cannot tell a wall from a monster and has no way to find out. That is not
minimalism, it is the security model: a client cannot reveal what it was never
sent, and it is never sent the seed, the room graph, unexplored geometry,
hidden-door flags, or any entity state. `PROTOCOL.md` has the details and the
reasoning.

**The two sides share no code.** No shared module, no import edge, no generated
types. Each declares its own structs for the payloads it handles, and
`PROTOCOL.md` is the only thing that says they must match — with a golden test
in `server/internal/httpapi` pinning the frame's key set so drift fails a build
rather than a playthrough.

Both rules are enforced rather than trusted. `server/go.mod` has exactly one
dependency, the YAML parser its content is written in (every fyne and x/image
dependency is the client's), every
server package sits under `internal/`, and a test in
`client/internal/httpclient` fails if any client package acquires a
`micro/server` dependency.

## Monsters and items are data

Every monster, weapon, piece of armor and item is a template in
`server/internal/content/data/*.yaml`. What a template does beyond the basics
comes from its **traits**: small, reusable behaviors written once in Go and
configured per template. The mummy's weakness to fire is one line:

```yaml
traits:
  - {trait: vulnerable, type: fire, multiplier: 2}
```

When anything is decided — how much damage a blow does, whether a sword can
be let go of, whether a goblin notices you — the rules engine
(`server/internal/rules`) asks every trait involved: the attacker's, the
weapon's, the defender's, and whatever either of them is wearing. Adding a
monster or an item that uses existing traits needs no Go at all. A new *kind*
of behavior is a new trait, AI or action, registered in `rules/traits`,
`rules/ai` or `rules/actions`.

Content is checked when the server starts. An unknown field or trait, a
parameter of the wrong type or a reference to a template that does not exist
stops it with the file and line. `server/internal/content/data/README.md` has
the full vocabulary.

## Cheats

`-reveal-hidden`, `-show-full-map` and `-cheat-mode` are `mapd` flags, not
request fields, because they exist to reveal what the game is otherwise hiding
or to bend its rules. `-reveal-hidden` draws hidden doors and invisible
monsters in magenta; `-show-full-map` lifts fog of war and nothing else;
`-cheat-mode` starts the player with 1000 hit points. A client has no way to
ask for any of them. The only thing a client may choose is the seed, and only
so a bug can be reproduced — it reproduces the opening state, not a whole
playthrough, because combat rolls come from the global RNG by long-standing
convention.
