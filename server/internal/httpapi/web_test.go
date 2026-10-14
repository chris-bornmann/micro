package httpapi

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"

	"micro/server/internal/game"
)

func TestTheWebClientIsServedBesideTheAPI(t *testing.T) {
	web := fstest.MapFS{
		"index.html":    {Data: []byte("<title>micro</title>")},
		"assets/app.js": {Data: []byte("console.log(1)")},
	}
	h := New(func(seed *int64) (*game.Game, error) {
		return game.NewGame(testConfig(), false, false, false)
	}, Options{Web: web})
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)

	for path, want := range map[string]string{"/": "<title>micro</title>", "/assets/app.js": "console.log(1)"} {
		resp, err := srv.Client().Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK || string(body) != want {
			t.Errorf("GET %s = %d %q, want 200 %q", path, resp.StatusCode, body, want)
		}
	}

	// The API still answers its own routes.
	if code := do(t, srv, "GET", "/healthz", nil, nil); code != http.StatusOK {
		t.Errorf("GET /healthz = %d, want 200", code)
	}
	if code := do(t, srv, "POST", "/v1/games", nil, nil); code != http.StatusCreated {
		t.Errorf("POST /v1/games = %d, want 201", code)
	}
}
