// Package httpclient talks to the game server.
//
// Every type here is declared locally and deliberately. None is imported from
// the server, and nothing in this package (or anything a client builds on it)
// imports a single line of game code — that is the property the whole split
// exists to produce, and it is checked by a test that greps the import graph.
//
// The structs below therefore mirror PROTOCOL.md rather than any Go type. They
// are what the client needs of the contract, which is less than the server
// sends: there are no fields here for anything a client cannot act on.
package httpclient

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"micro/client/internal/screen"
)

// Frame is one screen's worth of state. See PROTOCOL.md.
//
// A client cannot interpret any of this beyond drawing it: rows are characters
// it has no key to, and the palette gives colors without saying what they
// mean. Decode turns it into something paintable.
type Frame struct {
	Turn     int      `json:"turn"`
	Title    string   `json:"title"`
	Level    int      `json:"level"`
	Width    int      `json:"width"`
	Height   int      `json:"height"`
	Rows     []string `json:"rows"`
	Colors   [][]int  `json:"colors"`
	Palette  []string `json:"palette"`
	HUD      []string `json:"hud"`
	GameOver bool     `json:"game_over"`
}

// Decode turns the frame's wire form into a paintable grid.
func (f Frame) Decode() (screen.Grid, error) {
	return screen.Decode(f.Width, f.Height, f.Rows, f.Colors, f.Palette)
}

// PromptKind is a hint about how to present a Prompt. A client may ignore it
// and simply show Lines.
type PromptKind string

const (
	PromptChoose    PromptKind = "choose"
	PromptDirection PromptKind = "direction"
	// PromptCharacter wants one character, sent exactly as typed.
	PromptCharacter PromptKind = "character"
)

// Prompt is a pending interaction whose lines are already formatted. The
// client shows them and sends back whatever key was pressed; a matching letter
// or direction resolves it and anything else cancels it, so there is no prompt
// state to model beyond "is one open".
type Prompt struct {
	Kind  PromptKind `json:"kind"`
	Title string     `json:"title"`
	Lines []string   `json:"lines"`
}

// CommandResponse is the result of one keypress. Exactly one of Frame and
// Prompt is set: a Prompt means no turn was spent, so the frame already on
// screen is still correct.
type CommandResponse struct {
	Messages []string `json:"messages"`
	Frame    *Frame   `json:"frame"`
	Prompt   *Prompt  `json:"prompt"`
}

// View is a read-only query's answer: preformatted lines and the caption to
// show them under. Costs no turn.
type View struct {
	Name  string   `json:"name"`
	Title string   `json:"title"`
	Lines []string `json:"lines"`
}

// Config is how the server is set up: SignIn is nil unless it wants players
// signed in.
type Config struct {
	SignIn *SignIn `json:"sign_in"`
}

// SignIn says where to sign in: Cognito's hosted page, and the app client
// to sign in to. None of it is secret.
type SignIn struct {
	Domain   string `json:"domain"`
	ClientID string `json:"client_id"`
}

// Error is a response the server refused, with the message it gave.
type Error struct {
	Status  string // e.g. "409 Conflict"
	Code    int
	Message string
	// CurrentGameID is set on a 409 for starting a second game: the game
	// the user already has in progress.
	CurrentGameID string
}

func (e *Error) Error() string { return e.Status + ": " + e.Message }

// Client is a connection to one game on one server. Not safe for concurrent
// use; a client draws and types on one thread.
type Client struct {
	base   string
	gameID string
	http   *http.Client
	token  func() (string, error)
}

// New returns a Client for the server at base (e.g. "http://localhost:8080").
func New(base string) *Client {
	return &Client{
		base: strings.TrimRight(base, "/"),
		http: &http.Client{Timeout: 15 * time.Second},
	}
}

// UseToken has every request carry the access token token returns, for a
// server that wants players signed in.
func (c *Client) UseToken(token func() (string, error)) { c.token = token }

// GameID is the id of the game NewGame created, for logs and bug reports.
func (c *Client) GameID() string { return c.gameID }

// WaitForServer blocks until the server answers, or until timeout elapses.
//
// This exists because the editor's compound launch configuration starts the
// server and the client simultaneously, so the client's first request
// routinely loses the race. Retrying here is the fix; trying to sequence the
// two in the launch configuration instead is fragile and solves it only for
// that one way of starting them.
//
// Only connection failures are retried. An HTTP error means the server is up
// and objecting, which is not something waiting fixes.
func (c *Client) WaitForServer(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var lastErr error
	for {
		resp, err := c.http.Get(c.base + "/healthz")
		if err == nil {
			resp.Body.Close()
			return nil
		}
		lastErr = err
		if !isDialFailure(err) || time.Now().After(deadline) {
			return fmt.Errorf("waiting for %s: %w", c.base, lastErr)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// isDialFailure reports whether err is "nothing is listening yet" rather than
// a server that answered.
//
// A dial to a closed port arrives as *url.Error wrapping *net.OpError wrapping
// syscall.ECONNREFUSED, and errors.As unwraps to the middle of that. Matching
// *net.OpError rather than *url.Error matters: url.Error wraps essentially
// every transport failure including ones waiting cannot fix, and an HTTP error
// response is not an error here at all — http.Client returns those as a
// response, so they never reach this function.
func isDialFailure(err error) bool {
	var opErr *net.OpError
	return errors.As(err, &opErr)
}

// NewGame starts a game. seed may be nil to let the server choose; it is
// honoured only so a bug can be reproduced, and reproduces the opening state
// rather than a whole playthrough (see PROTOCOL.md).
func (c *Client) NewGame(seed *int64) (Frame, error) {
	body := map[string]any{}
	if seed != nil && *seed != 0 {
		body["seed"] = *seed
	}

	var out struct {
		GameID string `json:"game_id"`
		Frame  Frame  `json:"frame"`
	}
	if err := c.do("POST", "/v1/games", body, &out); err != nil {
		return Frame{}, err
	}
	c.gameID = out.GameID
	return out.Frame, nil
}

// Config asks how the server is set up. It needs no sign-in.
func (c *Client) Config() (Config, error) {
	var out Config
	err := c.do("GET", "/v1/config", nil, &out)
	return out, err
}

// Resume picks up the signed-in user's game in progress if they have one,
// and otherwise starts a new game as NewGame does. It reports whether it
// resumed.
//
// A server without accounts knows of no game in progress, so there this is
// NewGame. With accounts, a user has one game at a time, and asking for
// another is refused with the one they have; Resume takes that one too, in
// case it was started (from another client) after the first question.
func (c *Client) Resume(seed *int64) (f Frame, resumed bool, err error) {
	var me struct {
		CurrentGameID string `json:"current_game_id"`
	}
	if err := c.do("GET", "/v1/me", nil, &me); err != nil {
		return Frame{}, false, err
	}
	if me.CurrentGameID == "" {
		f, err = c.NewGame(seed)
		var refused *Error
		if !errors.As(err, &refused) || refused.CurrentGameID == "" {
			return f, false, err
		}
		me.CurrentGameID = refused.CurrentGameID
	}
	c.gameID = me.CurrentGameID
	f, err = c.Frame()
	return f, true, err
}

// Frame fetches the current screen without advancing anything.
func (c *Client) Frame() (Frame, error) {
	var out struct {
		Frame Frame `json:"frame"`
	}
	if err := c.do("GET", c.path("/frame"), nil, &out); err != nil {
		return Frame{}, err
	}
	return out.Frame, nil
}

// Command sends one keypress. Key names are exactly the ones in PROTOCOL.md.
func (c *Client) Command(key string) (CommandResponse, error) {
	var out CommandResponse
	if err := c.do("POST", c.path("/command"), map[string]string{"key": key}, &out); err != nil {
		return CommandResponse{}, err
	}
	return out, nil
}

// View fetches a read-only view by name: inventory, look, status or help. The
// client does not decide which exist, nor what they are called on screen.
func (c *Client) View(name string) (View, error) {
	var out View
	if err := c.do("GET", c.path("/views/"+url.PathEscape(name)), nil, &out); err != nil {
		return View{}, err
	}
	return out, nil
}

// path builds a URL under the current game, and is the one place that notices
// NewGame was never called.
func (c *Client) path(suffix string) string {
	if c.gameID == "" {
		return "/v1/games//" + strings.TrimPrefix(suffix, "/") // yields a clear 404
	}
	return "/v1/games/" + url.PathEscape(c.gameID) + suffix
}

// do issues one request and decodes its JSON response, turning a non-2xx into
// the error the server put in the body.
func (c *Client) do(method, path string, body any, dst any) error {
	var r io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal %s %s: %w", method, path, err)
		}
		r = bytes.NewReader(data)
	}

	req, err := http.NewRequest(method, c.base+path, r)
	if err != nil {
		return fmt.Errorf("%s %s: %w", method, path, err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.token != nil {
		token, err := c.token()
		if err != nil {
			return fmt.Errorf("%s %s: %w", method, path, err)
		}
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("%s %s: %w", method, path, serverError(resp))
	}
	if dst == nil {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(dst); err != nil {
		return fmt.Errorf("%s %s: decode response: %w", method, path, err)
	}
	return nil
}

// serverError reads an error response: the message out of its body, falling
// back to whatever raw text is there.
func serverError(resp *http.Response) *Error {
	e := &Error{Status: resp.Status, Code: resp.StatusCode, Message: "(no body)"}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
	if err != nil || len(data) == 0 {
		return e
	}
	var body struct {
		Error         string `json:"error"`
		CurrentGameID string `json:"current_game_id"`
	}
	if err := json.Unmarshal(data, &body); err == nil && body.Error != "" {
		e.Message, e.CurrentGameID = body.Error, body.CurrentGameID
	} else {
		e.Message = strings.TrimSpace(string(data))
	}
	return e
}
