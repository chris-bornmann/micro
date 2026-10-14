package auth

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// pool is a stand-in for a Cognito user pool: it publishes a key set at the
// path Cognito does, and signs tokens with whichever of its keys a test
// asks for.
type pool struct {
	srv     *httptest.Server
	keys    map[string]*rsa.PrivateKey // the published ones, by kid
	fetches atomic.Int32
}

const testClient = "test-client"

func newPool(t *testing.T, kids ...string) *pool {
	t.Helper()
	p := &pool{keys: map[string]*rsa.PrivateKey{}}
	for _, kid := range kids {
		p.keys[kid] = newKey(t)
	}
	p.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/.well-known/jwks.json" {
			http.NotFound(w, r)
			return
		}
		p.fetches.Add(1)
		var set struct {
			Keys []map[string]string `json:"keys"`
		}
		for kid, k := range p.keys {
			set.Keys = append(set.Keys, map[string]string{
				"kid": kid, "kty": "RSA", "alg": "RS256", "use": "sig",
				"n": b64(k.N.Bytes()),
				"e": b64(big.NewInt(int64(k.E)).Bytes()),
			})
		}
		_ = json.NewEncoder(w).Encode(set)
	}))
	t.Cleanup(p.srv.Close)
	return p
}

// checker is a Cognito that trusts this pool, on a clock the test sets.
func (p *pool) checker(now time.Time) *Cognito {
	c := newCognito(p.srv.URL, testClient)
	c.now = func() time.Time { return now }
	return c
}

// validClaims are the claims of a good access token for user "user-1",
// expiring an hour after now.
func (p *pool) validClaims(now time.Time) map[string]any {
	return map[string]any{
		"iss":       p.srv.URL,
		"sub":       "user-1",
		"token_use": "access",
		"client_id": testClient,
		"exp":       now.Add(time.Hour).Unix(),
	}
}

func newKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	k, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func b64(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

// sign makes a token with header {alg, kid}, signed by key with RS256 (key
// may sign under any alg; the header is what a forger controls).
func sign(t *testing.T, key *rsa.PrivateKey, alg, kid string, claims map[string]any) string {
	t.Helper()
	h, _ := json.Marshal(map[string]string{"alg": alg, "kid": kid, "typ": "JWT"})
	c, _ := json.Marshal(claims)
	signed := b64(h) + "." + b64(c)
	digest := sha256.Sum256([]byte(signed))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	return signed + "." + b64(sig)
}

// authenticate runs Authenticate on a request carrying the given
// Authorization header ("" for none).
func authenticate(c *Cognito, authorization string) (string, error) {
	r := httptest.NewRequest("GET", "/v1/me", nil)
	if authorization != "" {
		r.Header.Set("Authorization", authorization)
	}
	return c.Authenticate(r)
}

func TestAValidAccessTokenNamesItsUser(t *testing.T) {
	now := time.Now()
	p := newPool(t, "k1")
	token := sign(t, p.keys["k1"], "RS256", "k1", p.validClaims(now))

	user, err := authenticate(p.checker(now), "Bearer "+token)
	if err != nil || user != "user-1" {
		t.Fatalf("Authenticate = %q, %v; want user-1", user, err)
	}
}

func TestATokenIsRefusedUnlessEveryCheckPasses(t *testing.T) {
	now := time.Now()
	p := newPool(t, "k1")
	good := p.keys["k1"]

	with := func(key string, value any) map[string]any {
		cl := p.validClaims(now)
		if value == nil {
			delete(cl, key)
		} else {
			cl[key] = value
		}
		return cl
	}
	cases := map[string]string{
		"no header":          "",
		"not a bearer token": "Basic dXNlcjpwYXNz",
		"an empty token":     "Bearer ",
		"two parts":          "Bearer abc.def",
		"garbage":            "Bearer a.b.c",
		"alg none":           "Bearer " + sign(t, good, "none", "k1", p.validClaims(now)),
		"alg HS256":          "Bearer " + sign(t, good, "HS256", "k1", p.validClaims(now)),
		"another key":        "Bearer " + sign(t, newKey(t), "RS256", "k1", p.validClaims(now)),
		"an unknown key id":  "Bearer " + sign(t, good, "RS256", "k9", p.validClaims(now)),
		"another issuer":     "Bearer " + sign(t, good, "RS256", "k1", with("iss", "https://evil.example")),
		"an ID token":        "Bearer " + sign(t, good, "RS256", "k1", with("token_use", "id")),
		"another app client": "Bearer " + sign(t, good, "RS256", "k1", with("client_id", "other-client")),
		"expired":            "Bearer " + sign(t, good, "RS256", "k1", with("exp", now.Add(-time.Minute).Unix())),
		"no expiry":          "Bearer " + sign(t, good, "RS256", "k1", with("exp", nil)),
		"no user":            "Bearer " + sign(t, good, "RS256", "k1", with("sub", nil)),
		"claims edited after signing": "Bearer " + func() string {
			parts := strings.Split(sign(t, good, "RS256", "k1", p.validClaims(now)), ".")
			c, _ := json.Marshal(with("sub", "someone-else"))
			return parts[0] + "." + b64(c) + "." + parts[2]
		}(),
	}
	for name, authorization := range cases {
		if user, err := authenticate(p.checker(now), authorization); err == nil {
			t.Errorf("%s: accepted as %q", name, user)
		}
	}
}

func TestATokenJustPastExpiryIsAllowedForClockSkew(t *testing.T) {
	now := time.Now()
	p := newPool(t, "k1")
	cl := p.validClaims(now)
	cl["exp"] = now.Add(-clockSkew / 2).Unix()

	if _, err := authenticate(p.checker(now), "Bearer "+sign(t, p.keys["k1"], "RS256", "k1", cl)); err != nil {
		t.Errorf("a token %v past expiry was refused: %v", clockSkew/2, err)
	}
}

func TestANewKeyIsFetchedWhenCognitoRotates(t *testing.T) {
	now := time.Now()
	p := newPool(t, "k1")
	c := p.checker(now)
	if _, err := authenticate(c, "Bearer "+sign(t, p.keys["k1"], "RS256", "k1", p.validClaims(now))); err != nil {
		t.Fatal(err)
	}

	// Cognito starts signing with k2, which the checker has never seen.
	p.keys["k2"] = newKey(t)
	later := now.Add(2 * minRefetch)
	c.now = func() time.Time { return later }
	if _, err := authenticate(c, "Bearer "+sign(t, p.keys["k2"], "RS256", "k2", p.validClaims(later))); err != nil {
		t.Fatalf("a token signed with the new key was refused: %v", err)
	}
	if got := p.fetches.Load(); got != 2 {
		t.Errorf("fetched the key set %d times, want 2", got)
	}
}

func TestUnknownKeysCannotMakeTheServerFetchOnEveryRequest(t *testing.T) {
	now := time.Now()
	p := newPool(t, "k1")
	c := p.checker(now)

	for i := range 20 {
		token := sign(t, p.keys["k1"], "RS256", "forged-"+string(rune('a'+i)), p.validClaims(now))
		if _, err := authenticate(c, "Bearer "+token); err == nil {
			t.Fatal("a token with an unknown key ID was accepted")
		}
	}
	if got := p.fetches.Load(); got != 1 {
		t.Errorf("fetched the key set %d times within %v, want 1", got, minRefetch)
	}
}

func TestNewCognitoReadsTheRegionFromThePoolID(t *testing.T) {
	c, err := NewCognito("eu-west-2_AbCdEf123", "client")
	if err != nil {
		t.Fatal(err)
	}
	if want := "https://cognito-idp.eu-west-2.amazonaws.com/eu-west-2_AbCdEf123"; c.issuer != want {
		t.Errorf("issuer = %q, want %q", c.issuer, want)
	}
	for _, bad := range []string{"", "AbCdEf123", "_AbCdEf123"} {
		if _, err := NewCognito(bad, "client"); err == nil {
			t.Errorf("pool ID %q was accepted", bad)
		}
	}
	if _, err := NewCognito("eu-west-2_AbCdEf123", ""); err == nil {
		t.Error("an empty client ID was accepted")
	}
}
