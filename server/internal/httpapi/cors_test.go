package httpapi

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"
)

func corsServer() http.Handler {
	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	return WithCORS(ok, []string{"http://localhost:5173", "https://play.example"})
}

func TestCORSAllowsAListedOrigin(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/v1/games/x/frame", nil)
	r.Header.Set("Origin", "https://play.example")
	w := httptest.NewRecorder()
	corsServer().ServeHTTP(w, r)
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "https://play.example" {
		t.Errorf("Allow-Origin = %q", got)
	}
	if w.Header().Get("Vary") != "Origin" || w.Code != http.StatusOK {
		t.Errorf("Vary %q, code %d", w.Header().Get("Vary"), w.Code)
	}
}

func TestCORSAnswersThePreflight(t *testing.T) {
	r := httptest.NewRequest(http.MethodOptions, "/v1/games", nil)
	r.Header.Set("Origin", "http://localhost:5173")
	r.Header.Set("Access-Control-Request-Method", "POST")
	r.Header.Set("Access-Control-Request-Headers", "content-type")
	w := httptest.NewRecorder()
	corsServer().ServeHTTP(w, r)
	if w.Code != http.StatusNoContent || w.Header().Get("Access-Control-Allow-Headers") != "Content-Type" ||
		w.Header().Get("Access-Control-Allow-Methods") != "GET, POST, OPTIONS" {
		t.Errorf("code %d, headers %v", w.Code, w.Header())
	}
}

func TestCORSRefusesAnUnlistedOrigin(t *testing.T) {
	for _, method := range []string{http.MethodGet, http.MethodOptions} {
		r := httptest.NewRequest(method, "/v1/games", nil)
		r.Header.Set("Origin", "https://evil.example")
		r.Header.Set("Access-Control-Request-Method", "POST")
		w := httptest.NewRecorder()
		corsServer().ServeHTTP(w, r)
		if w.Header().Get("Access-Control-Allow-Origin") != "" {
			t.Errorf("%s: an unlisted origin got CORS headers", method)
		}
		if method == http.MethodOptions && w.Code != http.StatusForbidden {
			t.Errorf("preflight from an unlisted origin: %d", w.Code)
		}
	}
}

func TestCORSIsOffByDefault(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Origin", "http://localhost:5173")
	w := httptest.NewRecorder()
	WithCORS(inner, nil).ServeHTTP(w, r)
	if len(w.Header()) != 0 {
		t.Errorf("no origins should mean no CORS at all, got %v", w.Header())
	}
}

func TestGameIDsAreRandomAndOpaque(t *testing.T) {
	re := regexp.MustCompile(`^[0-9a-f]{32}$`)
	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		id, err := newGameID()
		if err != nil || !re.MatchString(id) || seen[id] {
			t.Fatalf("id %q, err %v, repeated %v", id, err, seen[id])
		}
		seen[id] = true
	}
}
