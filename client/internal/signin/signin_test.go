package signin

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"micro/client/internal/httpclient"
)

// cognito is a stand-in for the hosted page's token endpoint. It hands out
// tokens for the code "the-code", provided the verifier matches the
// challenge the browser brought to the sign-in page, and for refresh tokens
// it has issued.
type cognito struct {
	srv *httptest.Server

	mu        sync.Mutex
	challenge string          // the last one the sign-in page was shown
	refreshes map[string]bool // refresh tokens still good
	issued    int
}

func newCognito(t *testing.T) *cognito {
	t.Helper()
	c := &cognito{refreshes: map[string]bool{}}
	c.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/oauth2/token" || r.ParseForm() != nil {
			http.NotFound(w, r)
			return
		}
		c.mu.Lock()
		defer c.mu.Unlock()
		f := r.PostForm
		if f.Get("client_id") != "the-client" {
			http.Error(w, `{"error":"invalid_client"}`, http.StatusBadRequest)
			return
		}
		answer := map[string]any{"expires_in": 3600}
		switch f.Get("grant_type") {
		case "authorization_code":
			sum := sha256.Sum256([]byte(f.Get("code_verifier")))
			if f.Get("code") != "the-code" || base64.RawURLEncoding.EncodeToString(sum[:]) != c.challenge {
				http.Error(w, `{"error":"invalid_grant"}`, http.StatusBadRequest)
				return
			}
			answer["refresh_token"] = "refresh-1"
			c.refreshes["refresh-1"] = true
		case "refresh_token":
			if !c.refreshes[f.Get("refresh_token")] {
				http.Error(w, `{"error":"invalid_grant"}`, http.StatusBadRequest)
				return
			}
		}
		c.issued++
		answer["access_token"] = "access-" + string(rune('0'+c.issued))
		json.NewEncoder(w).Encode(answer)
	}))
	t.Cleanup(c.srv.Close)
	return c
}

var testConfig = httpclient.SignIn{Domain: "auth.example.com", ClientID: "the-client"}

// session is a Session that talks to fake, listens on any free port, and
// keeps its refresh token in a file of its own.
func (c *cognito) session(t *testing.T, file string) *Session {
	s := New(testConfig, file)
	s.base, s.listen = c.srv.URL, "localhost:0"
	return s
}

// browser plays the player's browser and the hosted page: shown the
// sign-in page's address, it signs in and is sent back with code.
func (c *cognito) browser(t *testing.T, code string) func(string) {
	return func(pageURL string) {
		u, err := url.Parse(pageURL)
		if err != nil {
			t.Error(err)
			return
		}
		q := u.Query()
		c.mu.Lock()
		c.challenge = q.Get("code_challenge")
		c.mu.Unlock()
		if q.Get("code_challenge_method") != "S256" || q.Get("client_id") != "the-client" {
			t.Errorf("sign-in page address = %s", pageURL)
		}
		back := q.Get("redirect_uri") + "?" + url.Values{"code": {code}, "state": {q.Get("state")}}.Encode()
		go func() {
			if resp, err := http.Get(back); err == nil {
				resp.Body.Close()
			}
		}()
	}
}

func signInFile(t *testing.T) string { return filepath.Join(t.TempDir(), "micro", "sign-in.json") }

func TestSigningInGetsATokenAndKeepsTheRefreshToken(t *testing.T) {
	c, file := newCognito(t), signInFile(t)
	s := c.session(t, file)
	if _, err := s.Token(); !errors.Is(err, ErrSignInNeeded) {
		t.Fatalf("before signing in, Token = %v; want ErrSignInNeeded", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.SignIn(ctx, c.browser(t, "the-code")); err != nil {
		t.Fatalf("SignIn: %v", err)
	}
	if token, err := s.Token(); err != nil || token != "access-1" {
		t.Errorf("Token = %q, %v", token, err)
	}

	info, err := os.Stat(file)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("the refresh token's file is %v; only its owner should read it", perm)
	}

	// The next time the game starts, the kept refresh token signs in.
	if token, err := c.session(t, file).Token(); err != nil || token != "access-2" {
		t.Errorf("a new session's Token = %q, %v", token, err)
	}
}

func TestAWrongCodeSignsNoOneIn(t *testing.T) {
	c := newCognito(t)
	s := c.session(t, signInFile(t))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.SignIn(ctx, c.browser(t, "a-guess")); err == nil {
		t.Fatal("signed in with a code Cognito did not issue")
	}
}

func TestAnAnswerToSomeoneElsesRequestIsIgnored(t *testing.T) {
	c := newCognito(t)
	s := c.session(t, signInFile(t))
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	err := s.SignIn(ctx, func(pageURL string) {
		u, _ := url.Parse(pageURL)
		back := u.Query().Get("redirect_uri") + "?code=the-code&state=forged"
		go func() {
			resp, err := http.Get(back)
			if err != nil {
				t.Error(err)
				return
			}
			resp.Body.Close()
			if resp.StatusCode != http.StatusBadRequest {
				t.Errorf("a forged answer got %s", resp.Status)
			}
		}()
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("SignIn = %v; want it still waiting for the real answer", err)
	}
}

func TestATokenIsRefreshedBeforeItExpires(t *testing.T) {
	c := newCognito(t)
	s := c.session(t, signInFile(t))
	now := time.Now()
	s.now = func() time.Time { return now }
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.SignIn(ctx, c.browser(t, "the-code")); err != nil {
		t.Fatal(err)
	}

	now = now.Add(time.Hour - refreshMargin/2)
	if token, err := s.Token(); err != nil || token != "access-2" {
		t.Errorf("Token near expiry = %q, %v; want a fresh one", token, err)
	}
	if token, _ := s.Token(); token != "access-2" {
		t.Errorf("a fresh token was refreshed again: %q", token)
	}
}

func TestARefusedRefreshTokenIsForgotten(t *testing.T) {
	c, file := newCognito(t), signInFile(t)
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(stored{Domain: testConfig.Domain, ClientID: testConfig.ClientID, RefreshToken: "revoked"})
	if err := os.WriteFile(file, data, 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := c.session(t, file).Token(); !errors.Is(err, ErrSignInNeeded) {
		t.Errorf("Token = %v; want ErrSignInNeeded", err)
	}
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Errorf("the refused refresh token is still kept: %v", err)
	}
}

func TestAnotherServersRefreshTokenIsNotUsed(t *testing.T) {
	c, file := newCognito(t), signInFile(t)
	os.MkdirAll(filepath.Dir(file), 0o700)
	data, _ := json.Marshal(stored{Domain: "elsewhere.example.com", ClientID: testConfig.ClientID, RefreshToken: "refresh-1"})
	os.WriteFile(file, data, 0o600)
	c.refreshes["refresh-1"] = true

	if _, err := c.session(t, file).Token(); !errors.Is(err, ErrSignInNeeded) {
		t.Errorf("Token = %v; want ErrSignInNeeded", err)
	}
}
