// Package auth checks that a request comes from someone signed in with
// Amazon Cognito.
//
// A client signs in on Cognito's own hosted page and gets back an access
// token: a JWT that Cognito signed with one of its user pool's RSA keys. The
// client sends it on every request as "Authorization: Bearer <token>", and
// Cognito here checks it with nothing but the standard library: the
// signature against the pool's published keys, then the claims that say
// the token is current and was issued to this game.
//
// The server never talks to Cognito about a user. The only call it makes is
// to fetch the pool's public keys, once at the first request and again only
// when Cognito starts signing with a key the server has not seen.
package auth

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"
)

// clockSkew is how far past its expiry a token is still accepted, to allow
// for the server's clock and Cognito's disagreeing slightly.
const clockSkew = 30 * time.Second

// minRefetch is the least time between two fetches of the key set. A token
// naming an unknown key triggers a fetch, and without this a stream of
// forged tokens could make the server fetch on every request.
const minRefetch = time.Minute

// Cognito checks access tokens issued by one user pool to one app client.
// It is safe for concurrent use.
type Cognito struct {
	issuer   string
	clientID string
	jwksURL  string
	client   *http.Client
	// now is the clock, replaceable in tests.
	now func() time.Time

	mu        sync.Mutex
	keys      map[string]*rsa.PublicKey // by key ID ("kid")
	fetchedAt time.Time
}

// NewCognito returns a checker for tokens from the user pool poolID (such
// as "us-east-1_AbCdEf123", which names its region) issued to the app
// client clientID.
func NewCognito(poolID, clientID string) (*Cognito, error) {
	region, _, ok := strings.Cut(poolID, "_")
	if !ok || region == "" {
		return nil, fmt.Errorf("user pool ID %q should look like <region>_<id>", poolID)
	}
	if clientID == "" {
		return nil, errors.New("an app client ID is required")
	}
	issuer := fmt.Sprintf("https://cognito-idp.%s.amazonaws.com/%s", region, poolID)
	return newCognito(issuer, clientID), nil
}

// newCognito is NewCognito for any issuer, so tests can stand up their own.
// Cognito publishes a pool's keys at a fixed path under its issuer URL.
func newCognito(issuer, clientID string) *Cognito {
	return &Cognito{
		issuer:   issuer,
		clientID: clientID,
		jwksURL:  issuer + "/.well-known/jwks.json",
		client:   &http.Client{Timeout: 5 * time.Second},
		now:      time.Now,
	}
}

// Authenticate returns the user r was sent by: the token's "sub" claim,
// Cognito's permanent ID for the user. Unlike a username or an email
// address, it never changes.
func (c *Cognito) Authenticate(r *http.Request) (string, error) {
	token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok || token == "" {
		return "", errors.New("no bearer token")
	}
	claims, err := c.verify(r.Context(), token)
	if err != nil {
		return "", err
	}
	return claims.Sub, nil
}

// header is the part of a JWT's header this checks.
type header struct {
	Alg string `json:"alg"`
	Kid string `json:"kid"`
}

// claims is the part of a Cognito access token's payload this checks.
type claims struct {
	Iss      string `json:"iss"`
	Sub      string `json:"sub"`
	TokenUse string `json:"token_use"`
	ClientID string `json:"client_id"`
	Exp      int64  `json:"exp"`
}

// verify checks token's signature and claims, returning the claims only if
// every check passes.
func (c *Cognito) verify(ctx context.Context, token string) (*claims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, errors.New("malformed token: want three parts")
	}

	var h header
	if err := decodePart(parts[0], &h); err != nil {
		return nil, fmt.Errorf("malformed token header: %w", err)
	}
	// Accept exactly the algorithm Cognito uses. Taking the header's word
	// for it is the classic JWT hole: "none", or an HMAC keyed with the
	// public key, would let anyone sign their own token.
	if h.Alg != "RS256" {
		return nil, fmt.Errorf("token algorithm %q is not RS256", h.Alg)
	}
	key, err := c.key(ctx, h.Kid)
	if err != nil {
		return nil, err
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return nil, fmt.Errorf("malformed token signature: %w", err)
	}
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if err := rsa.VerifyPKCS1v15(key, crypto.SHA256, digest[:], sig); err != nil {
		return nil, errors.New("token signature does not verify")
	}

	// Only now that Cognito is known to have written the claims are they
	// worth reading.
	var cl claims
	if err := decodePart(parts[1], &cl); err != nil {
		return nil, fmt.Errorf("malformed token claims: %w", err)
	}
	switch {
	case cl.Iss != c.issuer:
		return nil, fmt.Errorf("token issued by %q, not this user pool", cl.Iss)
	case cl.TokenUse != "access":
		// An ID token is signed by the same keys, but it says who someone
		// is, not that they may use this API.
		return nil, fmt.Errorf("token is an %q token, not an access token", cl.TokenUse)
	case cl.ClientID != c.clientID:
		return nil, fmt.Errorf("token issued to app client %q, not this one", cl.ClientID)
	case c.now().After(time.Unix(cl.Exp, 0).Add(clockSkew)):
		return nil, errors.New("token has expired")
	case cl.Sub == "":
		return nil, errors.New("token names no user")
	}
	return &cl, nil
}

// decodePart decodes one base64url-encoded JSON part of a token into dst.
func decodePart(part string, dst any) error {
	b, err := base64.RawURLEncoding.DecodeString(part)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, dst)
}

// key returns the pool's public key kid, fetching the key set if it has
// not been fetched yet, or if kid is new and the last fetch was long
// enough ago. Cognito rotates keys rarely; a new kid usually means a
// rotation the server has not seen.
func (c *Cognito) key(ctx context.Context, kid string) (*rsa.PublicKey, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if k, ok := c.keys[kid]; ok {
		return k, nil
	}
	if c.keys != nil && c.now().Sub(c.fetchedAt) < minRefetch {
		return nil, fmt.Errorf("token signed with unknown key %q", kid)
	}
	keys, err := c.fetchKeys(ctx)
	if err != nil {
		return nil, err
	}
	c.keys, c.fetchedAt = keys, c.now()
	if k, ok := c.keys[kid]; ok {
		return k, nil
	}
	return nil, fmt.Errorf("token signed with unknown key %q", kid)
}

// jwks is a JSON Web Key Set, as Cognito publishes it. Only the fields an
// RSA public key needs are read.
type jwks struct {
	Keys []struct {
		Kid string `json:"kid"`
		Kty string `json:"kty"`
		N   string `json:"n"`
		E   string `json:"e"`
	} `json:"keys"`
}

// fetchKeys downloads the pool's key set. Caller holds mu.
func (c *Cognito) fetchKeys(ctx context.Context) (map[string]*rsa.PublicKey, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.jwksURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch user pool keys: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch user pool keys: %s", resp.Status)
	}
	var set jwks
	if err := json.NewDecoder(resp.Body).Decode(&set); err != nil {
		return nil, fmt.Errorf("decode user pool keys: %w", err)
	}

	keys := map[string]*rsa.PublicKey{}
	for _, k := range set.Keys {
		if k.Kty != "RSA" {
			continue
		}
		n, err := base64.RawURLEncoding.DecodeString(k.N)
		if err != nil {
			return nil, fmt.Errorf("key %q: modulus: %w", k.Kid, err)
		}
		e, err := base64.RawURLEncoding.DecodeString(k.E)
		if err != nil {
			return nil, fmt.Errorf("key %q: exponent: %w", k.Kid, err)
		}
		// The exponent is a big-endian integer, in practice 65537.
		exp := new(big.Int).SetBytes(e)
		if !exp.IsInt64() || exp.Int64() > 1<<31-1 || len(bytes.TrimLeft(n, "\x00")) == 0 {
			return nil, fmt.Errorf("key %q is not a usable RSA key", k.Kid)
		}
		keys[k.Kid] = &rsa.PublicKey{N: new(big.Int).SetBytes(n), E: int(exp.Int64())}
	}
	return keys, nil
}
