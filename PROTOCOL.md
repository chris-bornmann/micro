# Protocol

The server owns the game. The client draws a grid of characters and sends
keypresses. This file is the contract between them.

**The two sides share no code.** There is no shared module, no import edge, and
no generated types. Each declares its own structs for the payloads it handles,
and this document is the only thing that says they must match. A golden test in
`server/internal/httpapi` pins the frame's key set so drift fails a build
rather than a playthrough.

## Why a character grid

The server sends the map already rasterized — rows of characters plus colors —
rather than rooms and corridors. The client cannot interpret what it draws: it
cannot tell a wall from a monster, and has no way to find out.

That is the security model. A client cannot reveal what it was never sent, and
it is never sent the level:

| Not on the wire | Why it matters |
|---|---|
| The seed | `mapgen.Generate` is deterministic in it. A client holding the seed reconstructs the whole dungeon — every unexplored room, every hidden door — no matter how carefully the map it was sent was filtered. |
| Room and corridor identity | Room ids alone tell a player how many rooms they have not found. |
| Unexplored geometry | Fog of war is applied before rasterizing, so unseen tiles are blank characters. |
| Hidden-door flags | An undiscovered hidden door is rasterized as the same wall character, in the same color, as the wall beside it. |
| Entity state | `overlay.Object` has 17 fields, 11 of them private (`Kind`, `Level`, `Oiliness`, `Burning`, `Invisible`, …). None survive rasterization. |

The cost is that the palette lives server-side. Every frame carries its palette
with it, so a client can restyle by substituting colors; what it cannot do is
learn what a color *means*.

## Frame

Everything needed to draw one screen.

```json
{
  "turn": 12,
  "title": "Hack — level 1, seed 1234",
  "level": 1,
  "width": 40,
  "height": 20,
  "rows":    ["   ####   ", "   #..@#  "],
  "colors":  [[0,0,0,1,1,1], [0,0,0,1,2,3]],
  "palette": ["#000000", "#616161", "#ada78c", "#ffffff"],
  "hud": ["Level: 1", "HP: 18/18", "Strength: 16"],
  "game_over": false
}
```

| Field | Notes |
|---|---|
| `turn` | Turns actually consumed. Does **not** advance when a command opens a picker, cancels one, or is refused after death. A client can use it to tell "nothing happened" from "a turn passed" without parsing messages, and to discard an out-of-order response. |
| `title` | Window caption, composed server-side. Carries the seed as inert text for bug reports — which is exactly why no structured field does. |
| `level` | Current depth, 1-indexed. |
| `width`, `height` | Grid dimensions. `rows` has `height` entries; each has `width` **runes**. |
| `rows` | The glyph layer, one string per row. Plain text, so a frame is legible straight out of `curl` — you can read the dungeon in the response. Decode with runes, not bytes: today every glyph is ASCII (`#` `.` `+` `h` `@` `g` `m` `%` `!` `)` `?` `/` `[`), but a multi-byte glyph must occupy one cell rather than shifting the row. |
| `colors` | A palette index per cell, as integers. `height` arrays of `width` entries. |
| `palette` | The `#rrggbb` values `colors` indexes into, deduplicated in first-appearance order. Index 0 is the blank cell. Around two dozen entries in practice. |
| `hud` | The always-visible status strip, **already formatted** as `"Label: Value"`. Omitted when empty. |
| `game_over` | Always present, unlike `hud`. A client reading a boolean should not have to treat absent and false as the same case. |

A space (or any cell whose glyph is a space) means nothing is there; draw
nothing.

## Endpoints

| Method | Path | Body → Response |
|---|---|---|
| `GET` | `/healthz` | → `{"status":"ok"}` |
| `GET` | `/v1/config` | → `{"sign_in"?: {"domain","client_id"}}` |
| `GET` | `/v1/me` | → `{"current_game_id"?}` |
| `POST` | `/v1/games` | `{"seed":1234}` (optional) → `201 {"game_id","frame"}` |
| `GET` | `/v1/games/{id}/frame` | → `{"frame"}` |
| `POST` | `/v1/games/{id}/command` | `{"key":"Down"}` → `{"messages"?,"frame"?,"prompt"?}` |
| `POST` | `/v1/games/{id}/commands` | `{"keys":[...]}` → `{"messages"?,"frame","prompt"?}` |
| `GET` | `/v1/games/{id}/views/{name}` | → `{"name","title","lines"}` |

`POST /v1/games/{id}/commands` replays a script in one round trip. It exists so
a bug report can be four lines of JSON; the clients do not use it.

A `game_id` is opaque: 128 random bits, hex-encoded. Without accounts nothing
else stands between a game and anyone who can reach the server, so it must be
unguessable; clients must not parse it or assume its form.

### Keys

Exactly the names the game has always used, which are Fyne's key names:

```
Up  Down  Left  Right  .  ,  <  >  A  E  D  T  X  Escape
```

`X` abandons the quest. It asks first, as a `character` prompt; `y` or `Y`
ends the game as death does, and any other answer keeps it going. Neither
costs a turn.

plus `A`–`J` while a prompt is open.

An **unrecognized key is not an error**. It falls through to the game and
costs a turn, exactly as pressing an unbound key always has. Validating
here would mean maintaining a whitelist that can silently drift out of step
with the command switch.

### What a client may not choose

`POST /v1/games` accepts **only** a seed, and unknown fields are rejected with
`400`. Dungeon geometry — size, room count, dark-room and hidden-door
percentages — is server policy, set from `mapd`'s flags. A client that could
send `hidden_door_percent: 0` is a client that can cheat by configuration.

The two cheat switches, `-reveal-hidden` and `-show-full-map`, are `mapd` flags
and are not reachable over HTTP at all. They exist to reveal what the game is
otherwise hiding.

A seed is accepted only so a bug can be reproduced. It reproduces the **opening
state** — geometry, spawn positions, starting attributes. It does not reproduce
a whole playthrough: combat rolls and other turn-time randomness come from the
global RNG by long-standing convention (see `randomUnoccupiedTile`), so two runs
on one seed diverge as soon as anything is fought.

## Command response

Exactly one of `frame` and `prompt` is present.

A **`prompt`** means the keypress opened an interaction instead of spending a
turn. Nothing moved, so there is no new frame — the one already on screen is
still correct.

```json
{ "kind": "choose",    "title": "Choose one", "lines": ["A) short sword (wielded)", "B) torch"] }
{ "kind": "direction", "title": "Throw",      "lines": ["Throw the flask of oil — which direction?"] }
{ "kind": "character", "title": "Scroll of summon monster", "lines": ["Summon which monster? Type its letter."] }
```

`lines` arrive already formatted, so a client never learns what an item, a
category or a pending action is. It shows the lines and sends back whatever key
was pressed: a matching letter, direction or character resolves the prompt, and
**anything else cancels it**. Either way the next response says what happened,
so a client need model no prompt state beyond "is one open" and its `kind`.

| `kind` | Answer |
|---|---|
| `choose` | One of the letters `lines` is keyed by. Case doesn't matter. |
| `direction` | `Up`, `Down`, `Left` or `Right`. |
| `character` | Any single character, sent **exactly as typed**: case matters (`t` and `T` are different monsters). `Escape`, or any other multi-character key name, cancels. |

A client may otherwise ignore `kind` and just show `lines` — except that for a
`character` prompt it must send the typed character itself, not a key name
(Fyne's `"T"` for both t and T, say).

A **`frame`** means the turn resolved. `messages` carries that turn's text in
order — an attack outcome, an item confirmation, an effect wearing off, each
monster's move. Join them however you like; the desktop client joins with
`" | "`.

## Views

`GET /v1/games/{id}/views/{name}` where `name` is one of `inventory`, `look`,
`status`, `help`. An unknown name is `404` and lists the real ones.

Views cost no turn and mutate nothing — looking at your own inventory is not an
action. `lines` are preformatted and `title` is the caption to show them under,
so a second client cannot title a view differently from the first.

## Accounts

`mapd -auth cognito` makes every `/v1/` request but `/v1/config` carry a
signed-in user's Amazon Cognito access token, as `Authorization: Bearer
<token>`. Without it, or with a token that does not check out, the answer is
`401` with `WWW-Authenticate: Bearer realm="micro"`. `/healthz` and the web
client's files need no token.

A client learns whether to sign in, and where, from `GET /v1/config`. Its
`sign_in` names the domain of Cognito's hosted sign-in page and the app client
to sign in to, both public; it is absent when the server has no accounts.
Clients sign in there by the authorization code flow with PKCE, and send the
access token. The callback URLs the app client accepts are fixed (see
`deploy/cognito.tf`): the site's root for the web client, and
`http://localhost:47615/callback` for the others.

With accounts:

- **A game belongs to the user who started it.** Another user's game is `404`,
  as though it did not exist.
- **A user has one game in progress at a time.** `GET /v1/me` names it in
  `current_game_id` (absent when there is none). Starting another is `409`,
  naming the one in progress:

  ```json
  {"error":"you already have a game in progress","current_game_id":"9f2c..."}
  ```

  A client continues that game rather than reporting an error.
- **A finished game** (death, or the quest abandoned) stays visible until the
  user starts the next one, and is then let go.

Without accounts (`-auth none`, the default) there are no users: anyone may
start any number of games, and `/v1/me` always answers `{}`.

## Calling from a browser (CORS)

`mapd -cors-origin http://localhost:5173,https://play.example` lets web pages
from exactly those origins call the API. A matching `Origin` gets
`Access-Control-Allow-Origin` (never `*`, never credentials) and the preflight a
JSON `POST` (or the `Authorization` header) triggers is answered `204`; any other origin is refused. Without the
flag there are no CORS headers at all, and only same-origin pages and
non-browser clients can call the API.

## Errors

Non-2xx responses carry `{"error":"..."}`. `400` for a malformed or
over-specified request, `401` for a request that needs a signed-in user and
has none, `404` for an unknown game or view (or another user's game), `409`
for a second game in progress (see Accounts), `500` for a genuine server
fault.

## Scope

Games live in memory, are never persisted, and are never evicted. A restart
loses every game in progress; a long-running server accumulates them. That is
the right trade for one person playing a single-player game on their own
machine, and the wrong one for anything else. The URLs carry a game id from the
start so that outgrowing this does not mean changing the shape of the API.
