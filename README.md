# micro/map

A roguelike, split into a server that owns the game and a client that draws it.

```
go.work              two modules, no shared third one
server/              the game: HTTP API + all logic          (module micromap/server)
client/              display and input                        (module micromap/client)
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

## Testing

```sh
cd server && go test -short ./...    # ~7s   — the inner loop
cd server && go test ./...           # ~22s  — adds four seed sweeps
cd client && go test ./...           # ~2.5s
./scripts/smoke.sh                   # both halves over a real socket
```

`go test ./...` does **not** work from this directory: the workspace root holds
no module of its own. Use a module path (`go test micromap/server/...`) or `cd`
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

Both rules are enforced rather than trusted. `server/go.mod` has no
dependencies at all (every fyne and x/image dependency is the client's), every
server package sits under `internal/`, and a test in
`client/internal/httpclient` fails if any client package acquires a
`micromap/server` dependency.

## Cheats

`-reveal-hidden` and `-show-full-map` are `mapd` flags, not request fields,
because they exist to reveal what the game is otherwise hiding. A client has no
way to ask for either. The only thing a client may choose is the seed, and only
so a bug can be reproduced — it reproduces the opening state, not a whole
playthrough, because combat rolls come from the global RNG by long-standing
convention.
