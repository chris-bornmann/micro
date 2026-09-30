#!/usr/bin/env bash
# End-to-end smoke test: build both halves, start the real server, play a
# scripted game through the real client, and check the output.
#
# This is the one check that exercises the client and the server together over
# a real socket. It matters because the two sides share no code: their unit
# tests verify each against PROTOCOL.md separately, and nothing but this
# notices if they have drifted apart in a way the document does not cover.
#
# Usage: scripts/smoke.sh [port]
set -euo pipefail

PORT="${1:-8199}"
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"; [[ -n "${PID:-}" ]] && kill "$PID" 2>/dev/null || true' EXIT

# Resolve the two modules. Works before the split (one module at api/) and
# after it (server/ and client/).
if [[ -d "$ROOT/server" && -d "$ROOT/client" ]]; then
    SERVER_DIR="$ROOT/server"; CLIENT_DIR="$ROOT/client"
else
    SERVER_DIR="$ROOT/api"; CLIENT_DIR="$ROOT/api"
fi

echo "==> building"
(cd "$SERVER_DIR" && go build -o "$TMP/mapd" ./cmd/mapd)
(cd "$CLIENT_DIR" && go build -o "$TMP/hackterm" ./cmd/hackterm)

echo "==> starting server on :$PORT"
"$TMP/mapd" -addr "127.0.0.1:$PORT" -seed 1234 > "$TMP/server.log" 2>&1 &
PID=$!

for _ in $(seq 1 50); do
    curl -sf "http://127.0.0.1:$PORT/healthz" > /dev/null 2>&1 && break
    sleep 0.1
done
if ! curl -sf "http://127.0.0.1:$PORT/healthz" > /dev/null 2>&1; then
    echo "FAIL: server never came up" >&2
    cat "$TMP/server.log" >&2
    exit 1
fi

echo "==> playing a scripted game"
printf 'left\nleft\nwait\nget\ni\nlook\nstatus\nhelp\nquit\n' \
    | "$TMP/hackterm" -url "http://127.0.0.1:$PORT" -seed 1234 > "$TMP/play.log" 2>&1

fail=0
check() {
    if grep -q "$1" "$TMP/play.log"; then
        echo "  ok: $2"
    else
        echo "  FAIL: $2 (expected to find /$1/)" >&2
        fail=1
    fi
}

check 'seed 1234'   'the title names the seed, for bug reports'
check 'turn 0'      'the opening frame is turn 0'
check 'turn 1'      'a command advances the turn'
check 'HP:'         'the HUD arrives preformatted'
check 'Inventory'   'the inventory view, captioned by the server'
check 'Here'        'the look view'
check 'Status'      'the status view'
check 'Arrow keys'  'the help view'

if grep -qi 'error:' "$TMP/play.log"; then
    echo "  FAIL: the session reported an error" >&2
    grep -i 'error:' "$TMP/play.log" >&2
    fail=1
else
    echo "  ok: no errors during play"
fi

# Nothing that would let a client cheat may appear anywhere in a session.
for secret in '"seed"' 'Rooms' 'Corridors' 'CorridorIDs' 'Hidden'; do
    if grep -q "$secret" "$TMP/play.log"; then
        echo "  FAIL: a session leaked $secret" >&2
        fail=1
    fi
done
[[ $fail -eq 0 ]] && echo "  ok: no level structure leaked to the client"

if [[ $fail -ne 0 ]]; then
    echo; echo "session output:"; cat "$TMP/play.log"
    exit 1
fi

echo "==> PASS"
