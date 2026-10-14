# Web client

A browser client for the game: the same thing `hackgui` does, in a page. It
draws the character grid the server sends, shows the turn's messages, opens
pop-ups for prompts and views, and sends keypresses. Like every client, it
holds no game logic and never learns what a glyph means; see
[PROTOCOL.md](../../PROTOCOL.md).

TypeScript, built with Vite. Preact with `@preact/signals` for the UI, rot.js
for the map, Vitest for tests.

## Running it

The page calls `mapd` from a different origin, so `mapd` must allow it:

```sh
cd server && go run ./cmd/mapd -cors-origin http://localhost:5173
cd client/web && npm install && npm run dev      # http://localhost:5173
```

`npm run dev` reads `.env.development`, which points at `mapd` on
`http://localhost:8080`. To use another address:
`VITE_API_URL=http://localhost:8090 npm run dev`.

You can add these to the page's URL:
- **`?seed=1234`:** starts new games from that seed.
- **`?brightness=1`:** shows the server's colors unchanged. The default is
  1.6, the same lift the desktop client applies to dark colors.

| Command | What it does |
|---|---|
| `npm run dev` | Development server, reloading on save |
| `npm run build` | Typecheck, then build into `dist/` |
| `npm run preview` | Serve the built `dist/` |
| `npm test` | Unit tests (Vitest) |
| `npm run typecheck` / `npm run lint` / `npm run format` | |

### Deploying

`mapd -web-dir` serves the built client beside the API, and the container
image does that (see the top-level README), so normally there is nothing to
deploy separately.

To host the files elsewhere instead, `npm run build` writes them to `dist/`,
for any static host:
1. Build with `VITE_API_URL` set to where `mapd` will be.
2. Start `mapd` with `-cors-origin` set to where the page is served from.

**Signing in.** The client asks the server (`GET /v1/config`) whether it
wants players signed in. If it does, the page sends the player to Cognito's
sign-in page and back, and keeps the tokens for as long as the tab is open
(`src/api/signin.ts`). It comes back to the site's root, which must be one of
the app client's callback URLs (see `deploy/cognito.tf`).

## Keys

| Key | Does |
|---|---|
| Arrows | Move, or attack what's in the way |
| `.` `,` | Wait, pick up |
| `<` `>` | Stairs |
| `a` `e` `d` `t` | Use, equip, drop, throw |
| `i` `l` `s` `?` | Inventory, look, status, help (no turn) |
| A letter / an arrow | Answer an open prompt; any other key cancels it |
| Any key | Close an open view |
| `x` | Abandon the quest (asks first) |
| `n` | New game, once the game is over |

- Keys held with Ctrl, Alt or Meta are left to the browser.
- Other unbound keys are not sent to the server, where they would waste a
  turn.
- Keys are read by the character they type (`event.key`), so `<` and `?`
  work on any keyboard layout.

## How it is built

The code is in three layers, and each depends only on the one below it.
ESLint enforces this (`no-restricted-imports` in `eslint.config.js`).

```
api/     protocol.ts  — the wire types, and a runtime check of every response
         client.ts    — fetch wrapper (the Api interface)
state/   game.ts      — the store: signals for frame, dialog, log, status, …
         controller.ts— the only writer: intents in, API calls, store updates
input/   keymap.ts    — KeyboardEvent + mode → intent; pure
render/  renderer.ts  — the GridRenderer interface and the Grid type
         grid.ts      — frame → Grid; pure
         palette.ts   — brightening
         rot.ts       — RotRenderer, on rot.js
ui/      *.tsx        — components: read signals, call the controller
main.tsx              — wiring: the only file that knows every layer
```

- **State is not presentation.** The store and controller know nothing of
  the DOM, so they're tested with a fake API and no browser
  (`controller.test.ts`). Components hold no game state.
- **Checking responses at runtime.** The client's types are its own,
  matching PROTOCOL.md; the server and clients share no code. A server that
  drifts from the protocol gets a clear error in the status bar, not a
  garbled map.
- **One request at a time.** A key pressed while a command is in flight is
  dropped, so a held key can't flood the server. A frame older than the one
  on screen is ignored.
- **Surviving a reload.** The game ID is kept in `sessionStorage`. On
  load, the client resumes that game, or starts a new one if the server no
  longer has it (for example, after a restart).

### Swapping the renderer

`render/renderer.ts` is the seam. Anything implementing `GridRenderer`
(`mount`, `draw`, `resize`, `destroy`) can draw the map. It gets a whole
`Grid` each turn and may redraw only what changed; `main.tsx` decides which
one is used.

rot.js was chosen because its `Display` is a small, solid canvas character
grid. Nothing else of rot.js is used: the server already does field of view,
pathfinding and map generation. `RotRenderer` draws at device pixels for
sharp text, and redraws only cells that changed.

### Animation

Each `Cell` has an `effect` field for animation, such as making a burning
monster glow. A renderer could animate cells by effect on its own
`requestAnimationFrame` loop.

Nothing sets `effect` yet, because the protocol deliberately sends no entity
state: the client can't tell a burning goblin from a goblin. Supporting this
needs a server change: an optional per-cell effects layer in the frame,
listing only effects the player can see. That widens what crosses the
network, so it should be added deliberately.

## Not done yet

- **End-to-end browser tests** (for example Playwright). The renderer and
  components are thin and are covered by running the app.
- **Mouse or touch input.**
- **Animation:** see above.
