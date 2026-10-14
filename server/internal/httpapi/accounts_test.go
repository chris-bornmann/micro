package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"micro/server/internal/game"
)

// headerAuth is a test Authenticator: the user is whatever the X-Test-User
// header says, and a request without it is refused. The real one checks a
// signed token (see internal/auth); what the server does with the answer is
// the same.
type headerAuth struct{}

func (headerAuth) Authenticate(r *http.Request) (string, error) {
	if u := r.Header.Get("X-Test-User"); u != "" {
		return u, nil
	}
	return "", errors.New("no X-Test-User header")
}

// newAccountsServer is newTestServer with accounts turned on.
func newAccountsServer(t *testing.T) *httptest.Server {
	t.Helper()
	h := New(func(seed *int64) (*game.Game, error) {
		return game.NewGame(testConfig(), false, false, false)
	}, Options{Auth: headerAuth{}, SignIn: &SignIn{Domain: "auth.example.com", ClientID: "test-client"}})
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv
}

// doAs is do, sent as user ("" sends no user at all). It returns the
// response so a test can look at its headers.
func doAs(t *testing.T, srv *httptest.Server, user, method, path string, body, dst any) *http.Response {
	t.Helper()
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	req, err := http.NewRequest(method, srv.URL+path, bytes.NewReader(data))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if user != "" {
		req.Header.Set("X-Test-User", user)
	}
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
	return resp
}

// startGameAs starts a game as user, failing the test unless it is created.
func startGameAs(t *testing.T, srv *httptest.Server, user string) string {
	t.Helper()
	var ng NewGameResponse
	if resp := doAs(t, srv, user, "POST", "/v1/games", nil, &ng); resp.StatusCode != http.StatusCreated {
		t.Fatalf("%s: new game = %d, want 201", user, resp.StatusCode)
	}
	return ng.GameID
}

func TestAUserHasNoGameInProgressUntilTheyStartOne(t *testing.T) {
	srv := newAccountsServer(t)

	var me MeResponse
	if resp := doAs(t, srv, "alice", "GET", "/v1/me", nil, &me); resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /v1/me = %d, want 200", resp.StatusCode)
	}
	if me.CurrentGameID != "" {
		t.Errorf("a new user has game %q in progress", me.CurrentGameID)
	}

	id := startGameAs(t, srv, "alice")
	doAs(t, srv, "alice", "GET", "/v1/me", nil, &me)
	if me.CurrentGameID != id {
		t.Errorf("after starting a game, current = %q, want %q", me.CurrentGameID, id)
	}
}

func TestAUserHasOneGameInProgressAtATime(t *testing.T) {
	srv := newAccountsServer(t)
	id := startGameAs(t, srv, "alice")

	var refused GameInProgressResponse
	resp := doAs(t, srv, "alice", "POST", "/v1/games", nil, &refused)
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("a second game = %d, want 409", resp.StatusCode)
	}
	if refused.CurrentGameID != id {
		t.Errorf("the 409 names game %q, want the one in progress, %q", refused.CurrentGameID, id)
	}

	// The limit is per user, not per server.
	startGameAs(t, srv, "bob")
}

func TestAnotherUsersGameIsNotFound(t *testing.T) {
	srv := newAccountsServer(t)
	id := startGameAs(t, srv, "alice")

	for _, req := range []struct{ method, path string }{
		{"GET", "/v1/games/" + id + "/frame"},
		{"POST", "/v1/games/" + id + "/command"},
		{"POST", "/v1/games/" + id + "/commands"},
		{"GET", "/v1/games/" + id + "/views/inventory"},
	} {
		body := map[string]any{"key": ".", "keys": []string{"."}}
		if req.method == "GET" {
			body = nil
		}
		if resp := doAs(t, srv, "bob", req.method, req.path, body, nil); resp.StatusCode != http.StatusNotFound {
			t.Errorf("bob: %s %s = %d, want 404", req.method, req.path, resp.StatusCode)
		}
	}
	if resp := doAs(t, srv, "alice", "GET", "/v1/games/"+id+"/frame", nil, nil); resp.StatusCode != http.StatusOK {
		t.Errorf("alice's own frame = %d, want 200", resp.StatusCode)
	}
}

func TestAbandoningAGameLetsTheUserStartAnother(t *testing.T) {
	srv := newAccountsServer(t)
	first := startGameAs(t, srv, "alice")

	var cr CommandsResponse
	doAs(t, srv, "alice", "POST", "/v1/games/"+first+"/commands", CommandsRequest{Keys: []string{"X", "y"}}, &cr)
	if cr.Frame == nil || !cr.Frame.GameOver {
		t.Fatalf("X then y should end the game, got %+v", cr)
	}

	var me MeResponse
	doAs(t, srv, "alice", "GET", "/v1/me", nil, &me)
	if me.CurrentGameID != "" {
		t.Errorf("an abandoned game is still in progress: %q", me.CurrentGameID)
	}

	second := startGameAs(t, srv, "alice")
	if second == first {
		t.Fatal("the new game reused the old game's ID")
	}
	// The finished game is let go once the next one starts.
	if resp := doAs(t, srv, "alice", "GET", "/v1/games/"+first+"/frame", nil, nil); resp.StatusCode != http.StatusNotFound {
		t.Errorf("the finished game = %d, want 404", resp.StatusCode)
	}
}

func TestARequestWithoutAcceptableCredentialsIs401(t *testing.T) {
	srv := newAccountsServer(t)
	id := startGameAs(t, srv, "alice")

	for _, req := range []struct{ method, path string }{
		{"GET", "/v1/me"},
		{"POST", "/v1/games"},
		{"GET", "/v1/games/" + id + "/frame"},
	} {
		var e ErrorResponse
		resp := doAs(t, srv, "", req.method, req.path, nil, &e)
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s %s with no user = %d, want 401", req.method, req.path, resp.StatusCode)
		}
		if got := resp.Header.Get("WWW-Authenticate"); got == "" {
			t.Errorf("%s %s: a 401 must say how to authenticate", req.method, req.path)
		}
		if e.Error != errUnauthorized.Error() {
			t.Errorf("%s %s: error = %q", req.method, req.path, e.Error)
		}
	}

	// The load balancer's health check carries no credentials.
	if resp := doAs(t, srv, "", "GET", "/healthz", nil, nil); resp.StatusCode != http.StatusOK {
		t.Errorf("GET /healthz with no user = %d, want 200", resp.StatusCode)
	}
}

func TestWithoutAccountsAnyoneMayStartAnyNumberOfGames(t *testing.T) {
	srv := newTestServer(t, false, false)
	for range 3 {
		if code := do(t, srv, "POST", "/v1/games", nil, nil); code != http.StatusCreated {
			t.Fatalf("new game = %d, want 201", code)
		}
	}
	var me MeResponse
	if code := do(t, srv, "GET", "/v1/me", nil, &me); code != http.StatusOK || me.CurrentGameID != "" {
		t.Errorf("GET /v1/me = %d %+v, want 200 and no game", code, me)
	}
}

func TestConfigTellsClientsWhereToSignIn(t *testing.T) {
	var cfg ConfigResponse
	if code := do(t, newAccountsServer(t), "GET", "/v1/config", nil, &cfg); code != http.StatusOK {
		t.Fatalf("GET /v1/config = %d, want 200", code)
	}
	if cfg.SignIn == nil || cfg.SignIn.Domain != "auth.example.com" || cfg.SignIn.ClientID != "test-client" {
		t.Errorf("sign_in = %+v", cfg.SignIn)
	}

	// An anonymous server has no signing in to do.
	cfg = ConfigResponse{}
	do(t, newTestServer(t, false, false), "GET", "/v1/config", nil, &cfg)
	if cfg.SignIn != nil {
		t.Errorf("an anonymous server sent sign_in %+v", cfg.SignIn)
	}
}
