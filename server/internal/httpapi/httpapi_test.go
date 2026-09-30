package httpapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"sort"
	"strings"
	"testing"

	"micro/server/internal/game"
	"micro/server/internal/mapgen"
)

// testConfig is a small, fast level with enough rooms that fog of war has
// something to hide, and both dark rooms and hidden doors so the secrecy
// tests below have subjects.
func testConfig() mapgen.Config {
	cfg := mapgen.DefaultConfig()
	cfg.Width, cfg.Height = 40, 24
	cfg.NumRooms = 6
	cfg.Seed = 1234
	return cfg
}

// newTestServer builds a server whose games use testConfig, with cheats off
// unless a test asks otherwise.
func newTestServer(t *testing.T, reveal, full bool) *httptest.Server {
	t.Helper()
	h := New(func(seed *int64) (*game.Game, error) {
		c := testConfig()
		if seed != nil {
			c.Seed = *seed
		}
		return game.NewGame(c, reveal, full)
	}, nil)
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv
}

// do issues a request and decodes the JSON response into dst (which may be
// nil), returning the status code.
func do(t *testing.T, srv *httptest.Server, method, path string, body any, dst any) int {
	t.Helper()

	var r *bytes.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal request: %v", err)
		}
		r = bytes.NewReader(data)
	} else {
		r = bytes.NewReader(nil)
	}

	req, err := http.NewRequest(method, srv.URL+path, r)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()

	if dst != nil {
		if err := json.NewDecoder(resp.Body).Decode(dst); err != nil {
			t.Fatalf("%s %s: decode response: %v", method, path, err)
		}
	}
	return resp.StatusCode
}

// startGame creates a game and returns its id and opening frame.
func startGame(t *testing.T, srv *httptest.Server) (string, game.Frame) {
	t.Helper()
	var out NewGameResponse
	if code := do(t, srv, "POST", "/v1/games", NewGameRequest{}, &out); code != http.StatusCreated {
		t.Fatalf("POST /v1/games: status %d, want 201", code)
	}
	if out.GameID == "" {
		t.Fatal("POST /v1/games returned no game_id")
	}
	return out.GameID, out.Frame
}

func TestHealthz(t *testing.T) {
	srv := newTestServer(t, false, false)

	var out map[string]string
	if code := do(t, srv, "GET", "/healthz", nil, &out); code != http.StatusOK {
		t.Errorf("status %d, want 200", code)
	}
	if out["status"] != "ok" {
		t.Errorf("status = %q, want %q", out["status"], "ok")
	}
}

func TestNewGameReturnsAnOpeningFrame(t *testing.T) {
	srv := newTestServer(t, false, false)

	_, f := startGame(t, srv)

	if f.Turn != 0 {
		t.Errorf("Turn = %d, want 0 on a fresh game", f.Turn)
	}
	if f.Level != 1 {
		t.Errorf("Level = %d, want 1", f.Level)
	}
	if len(f.Rows) != f.Height || f.Height == 0 {
		t.Errorf("got %d rows, want Height=%d and non-zero", len(f.Rows), f.Height)
	}
	if len(f.HUD) == 0 {
		t.Error("expected a HUD on the opening frame")
	}
}

// TestNewGameAcceptsASeedForReproducibility checks the one thing a client is
// allowed to choose actually takes effect: two games on the same seed must
// open identically, and a different seed must differ.
func TestNewGameAcceptsASeedForReproducibility(t *testing.T) {
	srv := newTestServer(t, false, false)

	frameForSeed := func(seed int64) game.Frame {
		var out NewGameResponse
		if code := do(t, srv, "POST", "/v1/games", NewGameRequest{Seed: &seed}, &out); code != http.StatusCreated {
			t.Fatalf("POST /v1/games seed=%d: status %d", seed, code)
		}
		return out.Frame
	}

	a, b := frameForSeed(99), frameForSeed(99)
	if strings.Join(a.Rows, "\n") != strings.Join(b.Rows, "\n") {
		t.Error("the same seed produced different opening frames")
	}
	if c := frameForSeed(12345); strings.Join(a.Rows, "\n") == strings.Join(c.Rows, "\n") {
		t.Error("different seeds produced identical opening frames")
	}
}

// TestNewGameIgnoresUnknownRequestFields is the guard against cheating by
// configuration. Dungeon geometry is server policy, so a request that tries
// to set it must be refused outright rather than silently ignored — silently
// ignoring it would leave a caller believing they had set something.
func TestNewGameRejectsDungeonSettings(t *testing.T) {
	srv := newTestServer(t, false, false)

	body := bytes.NewReader([]byte(`{"seed":1,"hidden_door_percent":0,"num_rooms":40}`))
	req, err := http.NewRequest("POST", srv.URL+"/v1/games", body)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status %d, want 400 — dungeon settings must not be settable by a client", resp.StatusCode)
	}
}

func TestNewGameToleratesAnEmptyBody(t *testing.T) {
	srv := newTestServer(t, false, false)

	req, err := http.NewRequest("POST", srv.URL+"/v1/games", bytes.NewReader(nil))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		t.Errorf("status %d, want 201 — a game with no options need not send a body", resp.StatusCode)
	}
}

func TestFrameDoesNotAdvanceTheTurn(t *testing.T) {
	srv := newTestServer(t, false, false)
	id, _ := startGame(t, srv)

	var a, b FrameResponse
	do(t, srv, "GET", "/v1/games/"+id+"/frame", nil, &a)
	do(t, srv, "GET", "/v1/games/"+id+"/frame", nil, &b)

	if a.Frame.Turn != 0 || b.Frame.Turn != 0 {
		t.Errorf("turns %d then %d; looking at the board must cost nothing", a.Frame.Turn, b.Frame.Turn)
	}
}

func TestCommandAdvancesTheTurnAndReturnsAFrame(t *testing.T) {
	srv := newTestServer(t, false, false)
	id, _ := startGame(t, srv)

	var resp game.CommandResponse
	if code := do(t, srv, "POST", "/v1/games/"+id+"/command", CommandRequest{Key: "."}, &resp); code != http.StatusOK {
		t.Fatalf("status %d, want 200", code)
	}

	if resp.Frame == nil {
		t.Fatal("a resolved turn must carry a frame")
	}
	if resp.Prompt != nil {
		t.Error("waiting a turn should not open a prompt")
	}
	if resp.Frame.Turn != 1 {
		t.Errorf("Turn = %d, want 1 after one command", resp.Frame.Turn)
	}
}

func TestCommandRequiresAKey(t *testing.T) {
	srv := newTestServer(t, false, false)
	id, _ := startGame(t, srv)

	if code := do(t, srv, "POST", "/v1/games/"+id+"/command", CommandRequest{}, nil); code != http.StatusBadRequest {
		t.Errorf("status %d, want 400 for an empty key", code)
	}
}

// TestCommandAcceptsAnUnknownKey pins the deliberate choice not to validate
// the key vocabulary: an unbound key falls through to the player service and
// costs a turn, exactly as it did before the split. The alternative is a
// whitelist that can drift out of step with the command switch.
func TestCommandAcceptsAnUnknownKey(t *testing.T) {
	srv := newTestServer(t, false, false)
	id, _ := startGame(t, srv)

	var resp game.CommandResponse
	if code := do(t, srv, "POST", "/v1/games/"+id+"/command", CommandRequest{Key: "banana"}, &resp); code != http.StatusOK {
		t.Fatalf("status %d, want 200 — an unbound key is a wasted turn, not an error", code)
	}
	if resp.Frame == nil || resp.Frame.Turn != 1 {
		t.Error("an unbound key should consume a turn, as it always has")
	}
}

func TestCommandOnAnUnknownGameIs404(t *testing.T) {
	srv := newTestServer(t, false, false)

	if code := do(t, srv, "POST", "/v1/games/nope/command", CommandRequest{Key: "."}, nil); code != http.StatusNotFound {
		t.Errorf("status %d, want 404", code)
	}
}

func TestMalformedBodyIs400(t *testing.T) {
	srv := newTestServer(t, false, false)
	id, _ := startGame(t, srv)

	req, err := http.NewRequest("POST", srv.URL+"/v1/games/"+id+"/command",
		bytes.NewReader([]byte(`{"key": `)))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status %d, want 400", resp.StatusCode)
	}
}

func TestViewsCostNoTurn(t *testing.T) {
	srv := newTestServer(t, false, false)
	id, _ := startGame(t, srv)

	for _, name := range []string{"inventory", "look", "status", "help"} {
		var v game.View
		if code := do(t, srv, "GET", "/v1/games/"+id+"/views/"+name, nil, &v); code != http.StatusOK {
			t.Errorf("GET views/%s: status %d, want 200", name, code)
			continue
		}
		if v.Name != name {
			t.Errorf("views/%s: Name = %q", name, v.Name)
		}
		if v.Title == "" {
			t.Errorf("views/%s: no title; captions are the server's to own", name)
		}
		if len(v.Lines) == 0 {
			t.Errorf("views/%s: no lines", name)
		}
	}

	var f FrameResponse
	do(t, srv, "GET", "/v1/games/"+id+"/frame", nil, &f)
	if f.Frame.Turn != 0 {
		t.Errorf("Turn = %d after four views; none of them is an action", f.Frame.Turn)
	}
}

func TestUnknownViewIs404AndListsTheRealOnes(t *testing.T) {
	srv := newTestServer(t, false, false)
	id, _ := startGame(t, srv)

	var e ErrorResponse
	if code := do(t, srv, "GET", "/v1/games/"+id+"/views/spellbook", nil, &e); code != http.StatusNotFound {
		t.Errorf("status %d, want 404", code)
	}
	for _, name := range game.ViewNames() {
		if !strings.Contains(e.Error, name) {
			t.Errorf("error %q does not mention the available view %q", e.Error, name)
		}
	}
}

// TestCommandsReplaysAScript covers the endpoint that makes a bug report four
// lines of JSON.
func TestCommandsReplaysAScript(t *testing.T) {
	srv := newTestServer(t, false, false)
	id, _ := startGame(t, srv)

	var out CommandsResponse
	if code := do(t, srv, "POST", "/v1/games/"+id+"/commands",
		CommandsRequest{Keys: []string{".", ".", "."}}, &out); code != http.StatusOK {
		t.Fatalf("status %d, want 200", code)
	}

	if out.Frame == nil {
		t.Fatal("expected a final frame")
	}
	if out.Frame.Turn != 3 {
		t.Errorf("Turn = %d, want 3 after three keys", out.Frame.Turn)
	}
}

func TestCommandsRequiresKeys(t *testing.T) {
	srv := newTestServer(t, false, false)
	id, _ := startGame(t, srv)

	if code := do(t, srv, "POST", "/v1/games/"+id+"/commands", CommandsRequest{}, nil); code != http.StatusBadRequest {
		t.Errorf("status %d, want 400", code)
	}
}

// TestCommandsAlwaysReturnsAFrameEvenWhenItEndsOnAPrompt matters because a
// script that stops mid-picker otherwise has no frame to report, and the
// caller still wants to see the board.
func TestCommandsAlwaysReturnsAFrame(t *testing.T) {
	srv := newTestServer(t, false, false)
	id, _ := startGame(t, srv)

	// "D" opens the drop picker whenever anything is carried, and the Player
	// starts with items, so this ends on a prompt.
	var out CommandsResponse
	if code := do(t, srv, "POST", "/v1/games/"+id+"/commands",
		CommandsRequest{Keys: []string{"D"}}, &out); code != http.StatusOK {
		t.Fatalf("status %d, want 200", code)
	}
	if out.Frame == nil {
		t.Error("expected a frame even though the script ended on a prompt")
	}
}

func TestGamesAreIndependent(t *testing.T) {
	srv := newTestServer(t, false, false)
	a, _ := startGame(t, srv)
	b, _ := startGame(t, srv)

	if a == b {
		t.Fatal("two games got the same id")
	}

	var resp game.CommandResponse
	do(t, srv, "POST", "/v1/games/"+a+"/command", CommandRequest{Key: "."}, &resp)

	var bf FrameResponse
	do(t, srv, "GET", "/v1/games/"+b+"/frame", nil, &bf)
	if bf.Frame.Turn != 0 {
		t.Errorf("game %s advanced to turn %d because %s did", b, bf.Frame.Turn, a)
	}
}

// ---- the contract itself ----

// wireFrameKeys is the frame's JSON shape, spelled out independently of the
// Go struct. It is the executable half of PROTOCOL.md.
//
// The client declares its own view of this shape and shares no code with the
// server, so nothing but a test can notice the two drifting apart. Adding a
// field here without updating PROTOCOL.md and the client is exactly the
// mistake this catches.
var wireFrameKeys = []string{
	"colors", "game_over", "height", "hud", "level",
	"palette", "rows", "title", "turn", "width",
}

func TestFrameWireShapeIsTheDocumentedOne(t *testing.T) {
	srv := newTestServer(t, false, false)
	id, _ := startGame(t, srv)

	var envelope struct {
		Frame map[string]any `json:"frame"`
	}
	do(t, srv, "GET", "/v1/games/"+id+"/frame", nil, &envelope)

	got := make([]string, 0, len(envelope.Frame))
	for k := range envelope.Frame {
		got = append(got, k)
	}
	sort.Strings(got)

	want := append([]string(nil), wireFrameKeys...)
	sort.Strings(want)

	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("frame wire shape drifted.\n got: %v\nwant: %v\nUpdate PROTOCOL.md and the client too.", got, want)
	}
}

// TestFrameOverTheWireCarriesNoSecrets is the frame_test.go assertions again,
// but through a real HTTP round trip — so it covers the serialization path a
// client actually sees rather than the struct.
func TestFrameOverTheWireCarriesNoSecrets(t *testing.T) {
	srv := newTestServer(t, false, false)
	id, _ := startGame(t, srv)

	resp, err := srv.Client().Get(srv.URL + "/v1/games/" + id + "/frame")
	if err != nil {
		t.Fatalf("GET frame: %v", err)
	}
	defer resp.Body.Close()

	var envelope struct {
		Frame map[string]any `json:"frame"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		t.Fatalf("decode: %v", err)
	}

	for key := range envelope.Frame {
		lower := strings.ToLower(key)
		for _, secret := range []string{"seed", "room", "corridor", "door", "object", "monster", "item"} {
			if strings.Contains(lower, secret) {
				t.Errorf("frame exposes %q over the wire", key)
			}
		}
	}
}

// TestFramesConformToTheProtocolOverTheWire checks the real serialization path
// against the documented shape, on the opening frame and on a mid-game one in
// case anything about them differs.
func TestFramesConformToTheProtocolOverTheWire(t *testing.T) {
	srv := newTestServer(t, false, false)
	id, opening := startGame(t, srv)

	assertFrameConformsToProtocol(t, opening)

	var resp game.CommandResponse
	do(t, srv, "POST", "/v1/games/"+id+"/command", CommandRequest{Key: "."}, &resp)
	if resp.Frame == nil {
		t.Fatal("no frame after a command")
	}
	assertFrameConformsToProtocol(t, *resp.Frame)
}

// assertFrameConformsToProtocol checks the structural invariants PROTOCOL.md
// states about a frame's grid, without importing any client. The server and
// every client share no code, so each side is checked against the document
// rather than against the other; separating the modules is what caught these
// tests calling the client's decoder to do this job.
func assertFrameConformsToProtocol(t *testing.T, f game.Frame) {
	t.Helper()

	hexColor := regexp.MustCompile(`^#[0-9a-f]{6}$`)

	if len(f.Rows) != f.Height {
		t.Errorf("got %d rows, want Height=%d", len(f.Rows), f.Height)
	}
	if len(f.Colors) != f.Height {
		t.Errorf("got %d color rows, want Height=%d", len(f.Colors), f.Height)
	}
	for i, c := range f.Palette {
		if !hexColor.MatchString(c) {
			t.Errorf("palette[%d] = %q, want #rrggbb", i, c)
		}
	}
	for y := 0; y < len(f.Rows) && y < len(f.Colors); y++ {
		if n := len([]rune(f.Rows[y])); n != f.Width {
			t.Errorf("row %d has %d runes, want Width=%d", y, n, f.Width)
		}
		if n := len(f.Colors[y]); n != f.Width {
			t.Errorf("color row %d has %d entries, want Width=%d", y, n, f.Width)
		}
		for x, idx := range f.Colors[y] {
			if idx < 0 || idx >= len(f.Palette) {
				t.Errorf("cell (%d,%d): palette index %d outside a palette of %d",
					x, y, idx, len(f.Palette))
			}
		}
	}
}

// TestCheatsAreNotReachableOverHTTP is the point of making them flags. There
// must be no request that turns them on.
func TestCheatsAreNotReachableOverHTTP(t *testing.T) {
	honest := newTestServer(t, false, false)
	cheating := newTestServer(t, false, true)

	drawn := func(f game.Frame) int {
		n := 0
		for _, row := range f.Rows {
			for _, ch := range row {
				if ch != ' ' {
					n++
				}
			}
		}
		return n
	}

	_, h := startGame(t, honest)
	_, c := startGame(t, cheating)

	// The control: the cheat really does reveal more, so the comparison below
	// is meaningful.
	if drawn(c) <= drawn(h) {
		t.Fatalf("show-full-map drew %d cells and fog of war drew %d; expected more", drawn(c), drawn(h))
	}

	// Now try to obtain the cheating view from the honest server by asking
	// for it in every way the API allows.
	for _, body := range []string{
		`{"show_full_map":true}`,
		`{"full":true}`,
		`{"reveal_hidden":true}`,
		`{"seed":1234,"show_full_map":true}`,
	} {
		req, err := http.NewRequest("POST", honest.URL+"/v1/games", strings.NewReader(body))
		if err != nil {
			t.Fatalf("new request: %v", err)
		}
		resp, err := honest.Client().Do(req)
		if err != nil {
			t.Fatalf("POST %s: %v", body, err)
		}
		func() {
			defer resp.Body.Close()
			if resp.StatusCode == http.StatusCreated {
				var out NewGameResponse
				if err := json.NewDecoder(resp.Body).Decode(&out); err == nil {
					if drawn(out.Frame) >= drawn(c) {
						t.Errorf("body %s obtained the cheating view", body)
					}
				}
				return
			}
			if resp.StatusCode != http.StatusBadRequest {
				t.Errorf("body %s: status %d, want 400 or a fogged 201", body, resp.StatusCode)
			}
		}()
	}
}

func TestPromptCarriesPreformattedLinesAndNoFrame(t *testing.T) {
	srv := newTestServer(t, false, false)
	id, _ := startGame(t, srv)

	// "D" always opens the drop picker when anything is carried.
	var resp game.CommandResponse
	if code := do(t, srv, "POST", "/v1/games/"+id+"/command", CommandRequest{Key: "D"}, &resp); code != http.StatusOK {
		t.Fatalf("status %d", code)
	}

	if resp.Prompt == nil {
		t.Skip("the Player started with nothing to drop on this seed")
	}
	if resp.Frame != nil {
		t.Error("a prompt means no turn passed, so there should be no new frame")
	}
	if resp.Prompt.Title == "" {
		t.Error("prompt has no title")
	}
	if len(resp.Prompt.Lines) == 0 {
		t.Fatal("prompt has no lines")
	}
	// Preformatted means the client never has to know what an item is.
	if !strings.Contains(resp.Prompt.Lines[0], ")") {
		t.Errorf("prompt line %q is not preformatted as \"A) name\"", resp.Prompt.Lines[0])
	}
	if resp.Prompt.Kind != game.PromptChoose {
		t.Errorf("Kind = %q, want %q", resp.Prompt.Kind, game.PromptChoose)
	}

	// The turn counter must not have moved.
	var f FrameResponse
	do(t, srv, "GET", "/v1/games/"+id+"/frame", nil, &f)
	if f.Frame.Turn != 0 {
		t.Errorf("Turn = %d; opening a picker is not a turn", f.Frame.Turn)
	}
}

// TestConcurrentCommandsDoNotCorruptTheGame exercises the per-game mutex.
// Without it, overlapping keypresses race on GameState, and descending a
// staircase can call mapgen.Generate from two goroutines at once. Run with
// -race for this to be worth much.
//
// The assertion cannot simply be "24 commands, 24 turns". Waiting 24 turns
// beside a dozen monsters may well kill the Player, and once dead every
// further keypress is refused without spending a turn. Whether that happens
// is not reproducible either, because combat rolls come from the global RNG
// by the convention randomUnoccupiedTile documents. So the invariant is
// stated exactly: every command spent a turn except those refused after
// death, and a refusal is identifiable by its message.
func TestConcurrentCommandsDoNotCorruptTheGame(t *testing.T) {
	srv := newTestServer(t, false, false)
	id, _ := startGame(t, srv)

	const n = 24
	type result struct {
		gameOver bool
		err      error
	}
	done := make(chan result, n)
	for i := 0; i < n; i++ {
		go func() {
			var resp game.CommandResponse
			body, _ := json.Marshal(CommandRequest{Key: "."})
			r, err := srv.Client().Post(srv.URL+"/v1/games/"+id+"/command",
				"application/json", bytes.NewReader(body))
			if err != nil {
				done <- result{err: err}
				return
			}
			defer r.Body.Close()
			if r.StatusCode != http.StatusOK {
				done <- result{err: fmt.Errorf("status %d", r.StatusCode)}
				return
			}
			if err := json.NewDecoder(r.Body).Decode(&resp); err != nil {
				done <- result{err: err}
				return
			}
			// handleKeyMessages short-circuits on a dead Player with exactly
			// this message and never reaches finishTurn, so this is the one
			// response that costs no turn.
			refused := len(resp.Messages) == 1 && resp.Messages[0] == "Game Over"
			done <- result{gameOver: refused}
		}()
	}

	refusals := 0
	for i := 0; i < n; i++ {
		r := <-done
		if r.err != nil {
			t.Fatalf("concurrent command: %v", r.err)
		}
		if r.gameOver {
			refusals++
		}
	}

	var f FrameResponse
	do(t, srv, "GET", "/v1/games/"+id+"/frame", nil, &f)
	if want := n - refusals; f.Frame.Turn != want {
		t.Errorf("Turn = %d after %d concurrent commands with %d refused post-mortem, want %d",
			f.Frame.Turn, n, refusals, want)
	}

	// Whatever happened, the game is still coherent and still well-formed.
	assertFrameConformsToProtocol(t, f.Frame)
}
