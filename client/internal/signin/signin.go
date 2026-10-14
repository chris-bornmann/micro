// Package signin signs a player in to a server that wants accounts, for the
// clients that are not in a browser.
//
// It uses the same sign-in as the web client: Cognito's hosted page, by the
// OAuth 2.0 authorization code flow with PKCE (RFC 7636). The page opens in
// the player's browser, and when they have signed in Cognito sends the
// browser back to a small server this package runs on localhost for the
// moment it takes. The game never sees a password.
//
// The refresh token is kept in a file only the player can read, so signing
// in is needed once a month (the app client's refresh token lifetime, see
// deploy/cognito.tf), not every time the game starts.
package signin

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"time"

	"micro/client/internal/httpclient"
)

// CallbackAddr is where the browser is sent back to after signing in. It
// must match one of the app client's callback URLs exactly, port and all,
// which is why it is fixed (see deploy/cognito.tf).
const CallbackAddr = "localhost:47615"

// ErrSignInNeeded means there is no token to send: the player has to sign
// in, or sign in again.
var ErrSignInNeeded = errors.New("sign-in needed")

// refreshMargin is how close to expiry an access token is refreshed rather
// than sent, so it cannot expire on the way.
const refreshMargin = time.Minute

// Session is one player's sign-in to one server. Not safe for concurrent
// use, like the httpclient.Client it supplies tokens to.
type Session struct {
	cfg httpclient.SignIn
	// file is where the refresh token is kept; "" keeps it only in memory.
	file string
	// base is the hosted page's address, and listen where the callback
	// server listens; both are replaceable in tests.
	base   string
	listen string
	http   *http.Client
	now    func() time.Time

	access    string
	expiresAt time.Time
	refresh   string
}

// stored is the file's contents. The refresh token is only good with the
// pool and app client that issued it, so those are kept beside it and a
// file from another server is ignored.
type stored struct {
	Domain       string `json:"domain"`
	ClientID     string `json:"client_id"`
	RefreshToken string `json:"refresh_token"`
}

// New returns a session for signing in with cfg, keeping its refresh token
// in file. A token already there is picked up.
func New(cfg httpclient.SignIn, file string) *Session {
	s := &Session{
		cfg:    cfg,
		file:   file,
		base:   "https://" + cfg.Domain,
		listen: CallbackAddr,
		http:   &http.Client{Timeout: 15 * time.Second},
		now:    time.Now,
	}
	if data, err := os.ReadFile(file); err == nil {
		var st stored
		if json.Unmarshal(data, &st) == nil && st.Domain == cfg.Domain && st.ClientID == cfg.ClientID {
			s.refresh = st.RefreshToken
		}
	}
	return s
}

// DefaultFile is where the refresh token is kept unless a client says
// otherwise: in the player's own configuration directory.
func DefaultFile() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "micro", "sign-in.json"), nil
}

// Token returns the access token to send, refreshing it first if it is
// about to expire. ErrSignInNeeded means there is none to be had without
// the player signing in.
func (s *Session) Token() (string, error) {
	if s.access != "" && s.now().Before(s.expiresAt.Add(-refreshMargin)) {
		return s.access, nil
	}
	if s.refresh == "" {
		return "", ErrSignInNeeded
	}
	err := s.requestTokens(url.Values{"grant_type": {"refresh_token"}, "refresh_token": {s.refresh}})
	var refused *refusedError
	if errors.As(err, &refused) {
		// The refresh token has expired, or the player was removed: only
		// signing in again will do.
		s.forget()
		return "", ErrSignInNeeded
	}
	if err != nil {
		return "", err
	}
	return s.access, nil
}

// SignIn has the player sign in on the hosted page. show is given the
// page's address, to open in a browser or to print; SignIn returns once the
// browser comes back, or ctx ends.
func (s *Session) SignIn(ctx context.Context, show func(pageURL string)) error {
	ln, err := net.Listen("tcp", s.listen)
	if err != nil {
		return fmt.Errorf("listen for the sign-in page's answer: %w", err)
	}
	defer ln.Close()
	redirect := "http://localhost:" + strconv.Itoa(ln.Addr().(*net.TCPAddr).Port) + "/callback"

	verifier, state := randomString(), randomString()
	challenge := sha256.Sum256([]byte(verifier))
	q := url.Values{
		"response_type":         {"code"},
		"client_id":             {s.cfg.ClientID},
		"redirect_uri":          {redirect},
		"scope":                 {"openid email"},
		"code_challenge_method": {"S256"},
		"code_challenge":        {base64.RawURLEncoding.EncodeToString(challenge[:])},
		"state":                 {state},
	}

	type answer struct {
		code string
		err  error
	}
	answers := make(chan answer, 1)
	srv := &http.Server{
		ReadHeaderTimeout: 10 * time.Second,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Anything else on this port (a browser's favicon request, say)
			// is not the answer.
			if r.URL.Path != "/callback" {
				http.NotFound(w, r)
				return
			}
			got := r.URL.Query()
			var a answer
			switch {
			case got.Get("state") != state:
				// Not the answer to this request: someone else's link.
				http.Error(w, "This is not the sign-in the game is waiting for.", http.StatusBadRequest)
				return
			case got.Get("error") != "":
				a.err = fmt.Errorf("sign-in refused: %s %s", got.Get("error"), got.Get("error_description"))
			case got.Get("code") == "":
				a.err = errors.New("sign-in page sent no code")
			default:
				a.code = got.Get("code")
			}
			if a.err != nil {
				http.Error(w, a.err.Error(), http.StatusBadRequest)
			} else {
				io.WriteString(w, "Signed in. You can close this tab and go back to the game.\n")
			}
			select {
			case answers <- a:
			default: // already answered
			}
		}),
	}
	go srv.Serve(ln)
	defer srv.Close()

	show(s.base + "/oauth2/authorize?" + q.Encode())

	var a answer
	select {
	case a = <-answers:
	case <-ctx.Done():
		return fmt.Errorf("waiting to be signed in: %w", ctx.Err())
	}
	if a.err != nil {
		return a.err
	}
	return s.requestTokens(url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {a.code},
		"redirect_uri":  {redirect},
		"code_verifier": {verifier},
	})
}

// refusedError is the token endpoint saying no, as opposed to not being
// reached at all.
type refusedError struct{ status string }

func (e *refusedError) Error() string { return "sign-in refused: " + e.status }

// requestTokens asks the token endpoint for tokens with form, and keeps
// what it answers.
func (s *Session) requestTokens(form url.Values) error {
	form.Set("client_id", s.cfg.ClientID)
	resp, err := s.http.PostForm(s.base+"/oauth2/token", form)
	if err != nil {
		return fmt.Errorf("sign in: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return &refusedError{status: resp.Status}
	}
	var body struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return fmt.Errorf("sign in: decode tokens: %w", err)
	}
	if body.AccessToken == "" {
		return errors.New("sign in: the answer has no access token")
	}
	s.access = body.AccessToken
	s.expiresAt = s.now().Add(time.Duration(body.ExpiresIn) * time.Second)
	// A refresh does not come with a new refresh token; the old one stays
	// good until it expires.
	if body.RefreshToken != "" && body.RefreshToken != s.refresh {
		s.refresh = body.RefreshToken
		return s.save()
	}
	return nil
}

// save writes the refresh token to the file, readable by the player alone:
// it is as good as their password for a month.
func (s *Session) save() error {
	if s.file == "" {
		return nil
	}
	data, err := json.Marshal(stored{Domain: s.cfg.Domain, ClientID: s.cfg.ClientID, RefreshToken: s.refresh})
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.file), 0o700); err != nil {
		return fmt.Errorf("keep sign-in: %w", err)
	}
	if err := os.WriteFile(s.file, data, 0o600); err != nil {
		return fmt.Errorf("keep sign-in: %w", err)
	}
	return nil
}

// forget drops every token, in memory and on disk.
func (s *Session) forget() {
	s.access, s.refresh = "", ""
	if s.file != "" {
		os.Remove(s.file)
	}
}

// Setup signs c in if its server wants that, asking the player to sign in
// on the hosted page when no kept sign-in will do, and telling them what is
// going on through log. Against a server without accounts it does nothing.
func Setup(ctx context.Context, c *httpclient.Client, log io.Writer) error {
	cfg, err := c.Config()
	if err != nil {
		return err
	}
	if cfg.SignIn == nil {
		return nil
	}
	file, err := DefaultFile()
	if err != nil {
		return err
	}
	s := New(*cfg.SignIn, file)
	if _, err := s.Token(); errors.Is(err, ErrSignInNeeded) {
		err = s.SignIn(ctx, func(pageURL string) {
			fmt.Fprintln(log, "Sign in to play. Opening your browser at:")
			fmt.Fprintln(log, " ", pageURL)
			// If no browser opens, the address above still works.
			_ = openBrowser(pageURL)
		})
		if err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	c.UseToken(func() (string, error) {
		token, err := s.Token()
		if errors.Is(err, ErrSignInNeeded) {
			return "", errors.New("your sign-in has expired: restart the game to sign in again")
		}
		return token, err
	})
	return nil
}

// openBrowser opens pageURL in the player's browser, the way each system
// does that.
func openBrowser(pageURL string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", pageURL)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", pageURL)
	default:
		cmd = exec.Command("xdg-open", pageURL)
	}
	return cmd.Start()
}

// randomString is 32 random bytes, base64url-encoded: a PKCE verifier, or a
// state.
func randomString() string {
	b := make([]byte, 32)
	rand.Read(b) // never fails; see its documentation
	return base64.RawURLEncoding.EncodeToString(b)
}
