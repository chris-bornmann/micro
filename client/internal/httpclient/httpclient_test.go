package httpclient

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// These tests run against a hand-written stub server, not the real one. That is
// the point: this package's job is to speak the protocol as documented, and a
// stub is the only way to check it against the document rather than against
// whatever the server happens to do. The real server and the real client are
// exercised together in cmd/hackterm's own test.

func stub(t *testing.T, h http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return New(srv.URL)
}

// sampleFrame is a frame exactly as PROTOCOL.md describes one.
const sampleFrame = `{
  "turn": 3, "title": "Hack — level 1, seed 1234", "level": 1,
  "width": 4, "height": 2,
  "rows": ["#..#", " @g "],
  "colors": [[0,1,1,0],[2,3,4,2]],
  "palette": ["#828282","#e8dfa4","#000000","#ffffff","#00c800"],
  "hud": ["HP: 18/18"],
  "game_over": false
}`

func TestNewGameReadsTheGameIDAndFrame(t *testing.T) {
	c := stub(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/v1/games" {
			t.Errorf("got %s %s, want POST /v1/games", r.Method, r.URL.Path)
		}
		fmt.Fprintf(w, `{"game_id":"g7","frame":%s}`, sampleFrame)
	})

	f, err := c.NewGame(nil)
	if err != nil {
		t.Fatalf("NewGame: %v", err)
	}
	if c.GameID() != "g7" {
		t.Errorf("GameID = %q, want g7", c.GameID())
	}
	if f.Turn != 3 || f.Title == "" || f.Width != 4 || f.Height != 2 {
		t.Errorf("frame decoded wrong: %+v", f)
	}
	if len(f.HUD) != 1 || f.HUD[0] != "HP: 18/18" {
		t.Errorf("HUD = %v", f.HUD)
	}
}

// TestNewGameSendsOnlyASeed guards the contract from this side too: the client
// must not invent request fields, because the server rejects unknown ones.
func TestNewGameSendsOnlyASeed(t *testing.T) {
	var got map[string]any
	c := stub(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&got)
		fmt.Fprintf(w, `{"game_id":"g1","frame":%s}`, sampleFrame)
	})

	seed := int64(1234)
	if _, err := c.NewGame(&seed); err != nil {
		t.Fatalf("NewGame: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("request body = %v, want just a seed", got)
	}
	if got["seed"] != float64(1234) {
		t.Errorf("seed = %v, want 1234", got["seed"])
	}
}

// TestNewGameOmitsAZeroSeed keeps "let the server choose" distinct from
// "reproduce seed 0", which is what the flag default means.
func TestNewGameOmitsAZeroSeed(t *testing.T) {
	var got map[string]any
	c := stub(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&got)
		fmt.Fprintf(w, `{"game_id":"g1","frame":%s}`, sampleFrame)
	})

	zero := int64(0)
	if _, err := c.NewGame(&zero); err != nil {
		t.Fatalf("NewGame: %v", err)
	}
	if _, ok := got["seed"]; ok {
		t.Errorf("a zero seed should be omitted, got %v", got)
	}
}

func TestFrameDecodesToAPaintableGrid(t *testing.T) {
	c := stub(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"game_id":"g1","frame":%s}`, sampleFrame)
	})

	f, err := c.NewGame(nil)
	if err != nil {
		t.Fatalf("NewGame: %v", err)
	}
	g, err := f.Decode()
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}

	if g.Width != 4 || g.Height != 2 {
		t.Fatalf("got %dx%d, want 4x2", g.Width, g.Height)
	}
	if got := g.At(0, 0); got.Ch != '#' || got.R != 0x82 {
		t.Errorf("(0,0) = %v, want '#' in 828282", got)
	}
	if got := g.At(1, 1); got.Ch != '@' || got.R != 255 || got.G != 255 || got.B != 255 {
		t.Errorf("(1,1) = %v, want '@' in white", got)
	}
	if got := g.At(2, 1); got.Ch != 'g' || got.G != 200 {
		t.Errorf("(2,1) = %v, want 'g' in 00c800", got)
	}
}

func TestCommandTargetsTheCurrentGame(t *testing.T) {
	var path, key string
	c := stub(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/games" {
			fmt.Fprintf(w, `{"game_id":"g9","frame":%s}`, sampleFrame)
			return
		}
		path = r.URL.Path
		var body map[string]string
		json.NewDecoder(r.Body).Decode(&body)
		key = body["key"]
		fmt.Fprintf(w, `{"messages":["You wait."],"frame":%s}`, sampleFrame)
	})

	if _, err := c.NewGame(nil); err != nil {
		t.Fatalf("NewGame: %v", err)
	}
	resp, err := c.Command("Down")
	if err != nil {
		t.Fatalf("Command: %v", err)
	}

	if path != "/v1/games/g9/command" {
		t.Errorf("path = %q", path)
	}
	if key != "Down" {
		t.Errorf("key = %q", key)
	}
	if len(resp.Messages) != 1 || resp.Messages[0] != "You wait." {
		t.Errorf("messages = %v", resp.Messages)
	}
	if resp.Frame == nil {
		t.Error("expected a frame")
	}
	if resp.Prompt != nil {
		t.Error("expected no prompt")
	}
}

// TestCommandReadsAPromptAndNoFrame is the shape the client branches on: a
// prompt means no turn passed, so there is nothing to redraw.
func TestCommandReadsAPromptAndNoFrame(t *testing.T) {
	c := stub(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/games" {
			fmt.Fprintf(w, `{"game_id":"g1","frame":%s}`, sampleFrame)
			return
		}
		io := `{"prompt":{"kind":"choose","title":"Choose one","lines":["A) torch","B) potion"]}}`
		fmt.Fprint(w, io)
	})

	if _, err := c.NewGame(nil); err != nil {
		t.Fatalf("NewGame: %v", err)
	}
	resp, err := c.Command("D")
	if err != nil {
		t.Fatalf("Command: %v", err)
	}

	if resp.Frame != nil {
		t.Error("a prompt response should carry no frame")
	}
	if resp.Prompt == nil {
		t.Fatal("expected a prompt")
	}
	if resp.Prompt.Kind != PromptChoose {
		t.Errorf("Kind = %q", resp.Prompt.Kind)
	}
	if len(resp.Prompt.Lines) != 2 {
		t.Errorf("Lines = %v", resp.Prompt.Lines)
	}
}

func TestViewFetchesByName(t *testing.T) {
	var path string
	c := stub(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/games" {
			fmt.Fprintf(w, `{"game_id":"g1","frame":%s}`, sampleFrame)
			return
		}
		path = r.URL.Path
		fmt.Fprint(w, `{"name":"inventory","title":"Inventory","lines":["A) torch"]}`)
	})

	if _, err := c.NewGame(nil); err != nil {
		t.Fatalf("NewGame: %v", err)
	}
	v, err := c.View("inventory")
	if err != nil {
		t.Fatalf("View: %v", err)
	}

	if path != "/v1/games/g1/views/inventory" {
		t.Errorf("path = %q", path)
	}
	if v.Title != "Inventory" || len(v.Lines) != 1 {
		t.Errorf("view = %+v", v)
	}
}

// TestErrorsCarryTheServersMessage matters for a terminal client, where the
// server's sentence is all the user sees.
func TestErrorsCarryTheServersMessage(t *testing.T) {
	c := stub(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, `{"error":"unknown view \"spellbook\" (have: help, inventory, look, status)"}`)
	})

	_, err := c.View("spellbook")
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "spellbook") || !strings.Contains(err.Error(), "inventory") {
		t.Errorf("error lost the server's message: %v", err)
	}
	if !strings.Contains(err.Error(), "404") {
		t.Errorf("error lost the status: %v", err)
	}
}

func TestErrorsSurviveANonJSONBody(t *testing.T) {
	c := stub(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, "plain text explosion")
	})

	_, err := c.Frame()
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "plain text explosion") {
		t.Errorf("error = %v", err)
	}
}

func TestResumeContinuesTheGameInProgress(t *testing.T) {
	c := stub(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "GET /v1/me":
			fmt.Fprint(w, `{"current_game_id":"g3"}`)
		case "GET /v1/games/g3/frame":
			fmt.Fprintf(w, `{"frame":%s}`, sampleFrame)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusTeapot)
		}
	})

	f, resumed, err := c.Resume(nil)
	if err != nil || !resumed || c.GameID() != "g3" || f.Turn != 3 {
		t.Errorf("Resume = turn %d, %v, %v; game %q", f.Turn, resumed, err, c.GameID())
	}
}

func TestResumeStartsAGameWhenThereIsNoneInProgress(t *testing.T) {
	c := stub(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "GET /v1/me":
			fmt.Fprint(w, `{}`)
		case "POST /v1/games":
			fmt.Fprintf(w, `{"game_id":"g7","frame":%s}`, sampleFrame)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
	})

	if _, resumed, err := c.Resume(nil); err != nil || resumed || c.GameID() != "g7" {
		t.Errorf("Resume = %v, %v; game %q", resumed, err, c.GameID())
	}
}

// TestResumeTakesTheGameNamedByA409 covers a game started from another
// client between the two questions Resume asks.
func TestResumeTakesTheGameNamedByA409(t *testing.T) {
	c := stub(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "GET /v1/me":
			fmt.Fprint(w, `{}`)
		case "POST /v1/games":
			w.WriteHeader(http.StatusConflict)
			fmt.Fprint(w, `{"error":"you already have a game in progress","current_game_id":"g5"}`)
		case "GET /v1/games/g5/frame":
			fmt.Fprintf(w, `{"frame":%s}`, sampleFrame)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
	})

	if _, resumed, err := c.Resume(nil); err != nil || !resumed || c.GameID() != "g5" {
		t.Errorf("Resume = %v, %v; game %q", resumed, err, c.GameID())
	}
}

func TestEveryRequestCarriesTheToken(t *testing.T) {
	c := stub(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer tok" {
			t.Errorf("%s %s: Authorization = %q", r.Method, r.URL.Path, got)
		}
		fmt.Fprintf(w, `{"game_id":"g1","frame":%s}`, sampleFrame)
	})
	c.UseToken(func() (string, error) { return "tok", nil })

	if _, err := c.NewGame(nil); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Frame(); err != nil {
		t.Fatal(err)
	}
}

func TestConfigSaysWhereToSignIn(t *testing.T) {
	c := stub(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"sign_in":{"domain":"auth.example.com","client_id":"abc"}}`)
	})
	cfg, err := c.Config()
	if err != nil || cfg.SignIn == nil || *cfg.SignIn != (SignIn{Domain: "auth.example.com", ClientID: "abc"}) {
		t.Errorf("Config = %+v, %v", cfg.SignIn, err)
	}
}

// TestWaitForServerReturnsWhenItComesUp covers the compound-launch race the
// editor creates by starting server and client simultaneously.
func TestWaitForServerReturnsWhenItComesUp(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"status":"ok"}`)
	}))
	defer srv.Close()

	c := New(srv.URL)
	if err := c.WaitForServer(2 * time.Second); err != nil {
		t.Errorf("WaitForServer on a live server: %v", err)
	}
}

func TestWaitForServerGivesUpOnADeadPort(t *testing.T) {
	// A server that is closed immediately leaves a port nothing listens on.
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := srv.URL
	srv.Close()

	c := New(url)
	start := time.Now()
	err := c.WaitForServer(600 * time.Millisecond)
	if err == nil {
		t.Fatal("expected an error for a port nothing is listening on")
	}
	if elapsed := time.Since(start); elapsed < 400*time.Millisecond {
		t.Errorf("gave up after %v; it should have retried until the timeout", elapsed)
	}
}

// TestTheClientImportsNoServerCode is the structural guarantee of the whole
// split: the client shares no code with the server, and that is a property of
// the import graph rather than of anything a reader can see in one file.
//
// Since the two are separate modules and every server package sits under
// internal/, the compiler already refuses this — the assertion here is a
// second line of defence, and a check that no one has "solved" a future
// problem by adding a require directive. It forbids the whole
// micro/server prefix rather than a list of package names, so it keeps
// working for packages that do not exist yet.
func TestTheClientImportsNoServerCode(t *testing.T) {
	clientPackages := []string{
		"micro/client/internal/httpclient",
		"micro/client/internal/screen",
		"micro/client/internal/ansi",
		"micro/client/internal/render",
		"micro/client/cmd/hackgui",
		"micro/client/cmd/hackterm",
	}

	for _, pkg := range clientPackages {
		out, err := exec.Command("go", "list", "-deps", pkg).Output()
		if err != nil {
			t.Fatalf("go list -deps %s: %v", pkg, err)
		}
		for _, line := range strings.Split(string(out), "\n") {
			dep := strings.TrimSpace(line)
			if strings.HasPrefix(dep, "micro/server") {
				t.Errorf("%s depends on %s; the client must share no server code", pkg, dep)
			}
		}
	}
}

// TestTheClientModuleRequiresNoServerModule checks the same thing one level up,
// where it would be introduced: a require directive in go.mod.
func TestTheClientModuleRequiresNoServerModule(t *testing.T) {
	data, err := os.ReadFile("../../go.mod")
	if err != nil {
		t.Fatalf("read go.mod: %v", err)
	}
	if strings.Contains(string(data), "micro/server") {
		t.Error("client/go.mod mentions micro/server; the two modules must stay independent")
	}
}
