// Package httpapi serves the game over HTTP.
//
// It is a thin facade over game.Game and holds no game logic itself. Its only
// real responsibilities are routing, turning errors into status codes, and
// keeping a registry of live games.
//
// Scope, stated plainly: games live in memory and are never persisted. A
// restart loses every game in progress. That is the right trade for now;
// the URLs carry a game id from the start so that growing out of it does not
// mean changing the shape of the API.
//
// The server runs in one of two ways:
//
//   - Anonymous (no Authenticator), on a developer's machine: anyone who
//     can reach it may start any number of games, and a game's unguessable
//     ID is all that guards it. Games are never evicted.
//   - With accounts (an Authenticator): every request names its user. Each
//     game belongs to the user who started it, and is invisible to anyone
//     else. A user has at most one game in progress, and starting the next
//     one discards the last, so memory holds at most one game per user.
//
// Nothing a client sends can widen what it is allowed to see. Dungeon
// geometry comes from the server's own flags, not from a request, and the
// reveal-hidden and show-full-map cheats are not reachable over HTTP at all.
// The only thing a client may choose is the seed, and only so a bug can be
// reproduced.
package httpapi

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"sort"
	"strings"
	"sync"

	"micro/server/internal/game"
)

// NewGameFunc creates a game, optionally with a caller-supplied seed. The
// server's own dungeon settings are closed over by whoever provides this, so
// that a request cannot influence them.
type NewGameFunc func(seed *int64) (*game.Game, error)

// Options are a Server's optional settings. The zero value is an anonymous
// server that logs nothing.
type Options struct {
	// Auth says who sent each request. Nil makes every request anonymous
	// (see the package comment).
	Auth Authenticator
	// SignIn tells clients where to sign in, and is served at
	// GET /v1/config. Set it whenever Auth is set.
	SignIn *SignIn
	// Web, if set, is the built web client, served at "/" so that the game
	// and its API share one address and the browser needs no CORS.
	Web fs.FS
	// Logger, if set, logs every request that fails.
	Logger *log.Logger
}

// SignIn is what a client needs to sign a user in with Cognito's hosted
// page. None of it is secret: the app client is a public one, made for
// code that runs on the player's own device.
type SignIn struct {
	// Domain is the hosted sign-in page's domain, such as
	// "micro-hack.auth.us-east-1.amazoncognito.com".
	Domain string `json:"domain"`
	// ClientID is the user pool's app client for the game.
	ClientID string `json:"client_id"`
}

// Server routes requests to live games.
type Server struct {
	mux    *http.ServeMux
	new    NewGameFunc
	auth   Authenticator
	signIn *SignIn
	logger *log.Logger

	mu    sync.RWMutex
	games map[string]*entry
	// latest is each user's most recent game, by ID. It is the game in
	// progress while that game is not over. Anonymous requests have no
	// entry here.
	latest map[string]string
}

// entry is a live game and the user it belongs to ("" when anonymous).
type entry struct {
	game  *game.Game
	owner string
}

// New builds a Server that starts games with newGame.
func New(newGame NewGameFunc, opts Options) *Server {
	s := &Server{
		mux:    http.NewServeMux(),
		new:    newGame,
		auth:   opts.Auth,
		signIn: opts.SignIn,
		logger: opts.Logger,
		games:  map[string]*entry{},
		latest: map[string]string{},
	}

	s.mux.HandleFunc("GET /healthz", s.handleHealth)
	s.mux.HandleFunc("GET /v1/config", s.handleConfig)
	s.mux.HandleFunc("GET /v1/me", s.handleMe)
	s.mux.HandleFunc("POST /v1/games", s.handleNewGame)
	s.mux.HandleFunc("GET /v1/games/{id}/frame", s.handleFrame)
	s.mux.HandleFunc("POST /v1/games/{id}/command", s.handleCommand)
	s.mux.HandleFunc("POST /v1/games/{id}/commands", s.handleCommands)
	s.mux.HandleFunc("GET /v1/games/{id}/views/{name}", s.handleView)
	if opts.Web != nil {
		// "GET /" matches only what no route above does, so the web
		// client's files can never shadow the API.
		s.mux.Handle("GET /", http.FileServerFS(opts.Web))
	}

	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

// --- requests and responses ---

// NewGameRequest carries only a seed, deliberately. Mirroring the nine
// fields of mapgen.Config here would let a client ask for a dungeon with no
// hidden doors and no dark rooms, which is cheating by configuration.
type NewGameRequest struct {
	Seed *int64 `json:"seed,omitempty"`
}

type NewGameResponse struct {
	GameID string     `json:"game_id"`
	Frame  game.Frame `json:"frame"`
}

// GameInProgressResponse is the 409 a user gets for starting a game while
// they still have one going. It names that game, so a client can offer to
// continue it instead.
type GameInProgressResponse struct {
	Error         string `json:"error"`
	CurrentGameID string `json:"current_game_id"`
}

// ConfigResponse tells a client how this server is set up before it has
// signed in. SignIn is absent on an anonymous server, where there is no
// signing in to do.
type ConfigResponse struct {
	SignIn *SignIn `json:"sign_in,omitempty"`
}

// MeResponse is what the server knows about the signed-in user: only their
// game in progress, if any.
type MeResponse struct {
	CurrentGameID string `json:"current_game_id,omitempty"`
}

// CommandRequest is one keypress, named exactly as the game has always named
// them: "Up" "Down" "Left" "Right" "." "," "<" ">" "A" "E" "D" "T" "Escape",
// plus "A".."J" while a prompt is open.
type CommandRequest struct {
	Key string `json:"key"`
}

// CommandsRequest replays a whole key script in one round trip. It exists for
// testing and bug reports — a reproduction becomes a line of JSON — and the
// clients do not use it.
type CommandsRequest struct {
	Keys []string `json:"keys"`
}

// CommandsResponse reports every message the script produced, in order, and
// the frame it ended on. A prompt left open at the end is reported too, since
// a script can legitimately stop mid-picker.
type CommandsResponse struct {
	Messages []string     `json:"messages,omitempty"`
	Frame    *game.Frame  `json:"frame,omitempty"`
	Prompt   *game.Prompt `json:"prompt,omitempty"`
}

type FrameResponse struct {
	Frame game.Frame `json:"frame"`
}

type ErrorResponse struct {
	Error string `json:"error"`
}

// --- handlers ---

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, ConfigResponse{SignIn: s.signIn})
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	user, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	s.mu.RLock()
	id := s.inProgress(user)
	s.mu.RUnlock()
	writeJSON(w, http.StatusOK, MeResponse{CurrentGameID: id})
}

func (s *Server) handleNewGame(w http.ResponseWriter, r *http.Request) {
	user, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	// Check before doing the work of generating a level. The check is
	// repeated when the game is stored, in case another request for the
	// same user got there first.
	s.mu.RLock()
	current := s.inProgress(user)
	s.mu.RUnlock()
	if current != "" {
		s.refuseSecondGame(w, r, current)
		return
	}

	var req NewGameRequest
	if err := decodeBody(r, &req); err != nil {
		s.fail(w, r, http.StatusBadRequest, err)
		return
	}

	g, err := s.new(req.Seed)
	if err != nil {
		// A rejected Config is the caller's fault only insofar as they chose
		// a seed; mapgen.Generate validates its own input, so surface it as a
		// bad request rather than a server fault.
		s.fail(w, r, http.StatusBadRequest, err)
		return
	}

	f, err := g.Frame()
	if err != nil {
		s.fail(w, r, http.StatusInternalServerError, err)
		return
	}

	id, err := newGameID()
	if err != nil {
		s.fail(w, r, http.StatusInternalServerError, err)
		return
	}
	s.mu.Lock()
	if current := s.inProgress(user); current != "" {
		s.mu.Unlock()
		s.refuseSecondGame(w, r, current)
		return
	}
	if user != "" {
		// The user's last game is over, so nothing can be done with it
		// any more: let it go rather than keep every game a user ever
		// played.
		delete(s.games, s.latest[user])
		s.latest[user] = id
	}
	s.games[id] = &entry{game: g, owner: user}
	s.mu.Unlock()

	writeJSON(w, http.StatusCreated, NewGameResponse{GameID: id, Frame: f})
}

func (s *Server) handleFrame(w http.ResponseWriter, r *http.Request) {
	g, ok := s.lookup(w, r)
	if !ok {
		return
	}
	f, err := g.Frame()
	if err != nil {
		s.fail(w, r, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, FrameResponse{Frame: f})
}

func (s *Server) handleCommand(w http.ResponseWriter, r *http.Request) {
	g, ok := s.lookup(w, r)
	if !ok {
		return
	}

	var req CommandRequest
	if err := decodeBody(r, &req); err != nil {
		s.fail(w, r, http.StatusBadRequest, err)
		return
	}
	if req.Key == "" {
		s.fail(w, r, http.StatusBadRequest, errors.New("key is required"))
		return
	}

	// An unrecognized key is deliberately not rejected. It falls through to
	// the game and costs a turn — pressing an unbound key has always
	// wasted a turn. Validating
	// here would mean maintaining a key whitelist alongside the command
	// switch, which is a second thing that can silently drift out of step
	// with it.
	resp, err := g.Command(req.Key)
	if err != nil {
		s.fail(w, r, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleCommands(w http.ResponseWriter, r *http.Request) {
	g, ok := s.lookup(w, r)
	if !ok {
		return
	}

	var req CommandsRequest
	if err := decodeBody(r, &req); err != nil {
		s.fail(w, r, http.StatusBadRequest, err)
		return
	}
	if len(req.Keys) == 0 {
		s.fail(w, r, http.StatusBadRequest, errors.New("keys is required"))
		return
	}

	var out CommandsResponse
	for i, key := range req.Keys {
		if key == "" {
			s.fail(w, r, http.StatusBadRequest, fmt.Errorf("keys[%d] is empty", i))
			return
		}
		resp, err := g.Command(key)
		if err != nil {
			s.fail(w, r, http.StatusInternalServerError, fmt.Errorf("keys[%d] (%q): %w", i, key, err))
			return
		}
		out.Messages = append(out.Messages, resp.Messages...)
		// Keep only the last of each: a script's caller wants where it ended
		// up, not every intermediate frame.
		out.Frame, out.Prompt = resp.Frame, resp.Prompt
	}

	// If the script ended on a prompt there is no final frame, so fetch the
	// one still on screen — a caller replaying a script wants to see the
	// board regardless of what the last key happened to be.
	if out.Frame == nil {
		f, err := g.Frame()
		if err != nil {
			s.fail(w, r, http.StatusInternalServerError, err)
			return
		}
		out.Frame = &f
	}

	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleView(w http.ResponseWriter, r *http.Request) {
	g, ok := s.lookup(w, r)
	if !ok {
		return
	}

	name := r.PathValue("name")
	v, err := g.View(name)
	if err != nil {
		names := game.ViewNames()
		sort.Strings(names)
		s.fail(w, r, http.StatusNotFound,
			fmt.Errorf("unknown view %q (have: %s)", name, strings.Join(names, ", ")))
		return
	}
	writeJSON(w, http.StatusOK, v)
}

// --- helpers ---

// lookup resolves {id} to a live game belonging to whoever sent r,
// answering 401 or 404 itself if there is none. Someone else's game gets
// the same 404 as a game that does not exist, so an ID that leaks reveals
// nothing.
func (s *Server) lookup(w http.ResponseWriter, r *http.Request) (*game.Game, bool) {
	user, ok := s.authenticate(w, r)
	if !ok {
		return nil, false
	}
	id := r.PathValue("id")

	s.mu.RLock()
	e, ok := s.games[id]
	s.mu.RUnlock()

	if !ok || e.owner != user {
		s.fail(w, r, http.StatusNotFound, fmt.Errorf("no game %q", id))
		return nil, false
	}
	return e.game, true
}

// inProgress returns the ID of user's game in progress, or "" if they have
// none. Anonymous users never have one: they are not limited to one game.
// Caller holds mu.
func (s *Server) inProgress(user string) string {
	if user == "" {
		return ""
	}
	id := s.latest[user]
	if e, ok := s.games[id]; !ok || e.game.Over() {
		return ""
	}
	return id
}

// refuseSecondGame answers a request to start a game while the user has
// one in progress.
func (s *Server) refuseSecondGame(w http.ResponseWriter, r *http.Request, current string) {
	if s.logger != nil {
		s.logger.Printf("%s %s: 409: game %s is in progress", r.Method, r.URL.Path, current)
	}
	writeJSON(w, http.StatusConflict, GameInProgressResponse{
		Error:         "you already have a game in progress",
		CurrentGameID: current,
	})
}

// decodeBody reads a JSON body, tolerating an empty one so that a request
// with no options (POST /v1/games with no seed) need not send "{}".
func decodeBody(r *http.Request, dst any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		if errors.Is(err, io.EOF) {
			return nil // empty body: leave dst at its zero value
		}
		return fmt.Errorf("malformed JSON body: %w", err)
	}
	return nil
}

func (s *Server) fail(w http.ResponseWriter, r *http.Request, code int, err error) {
	if s.logger != nil {
		s.logger.Printf("%s %s: %d: %v", r.Method, r.URL.Path, code, err)
	}
	writeJSON(w, code, ErrorResponse{Error: err.Error()})
}

func writeJSON(w http.ResponseWriter, code int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		// The status line is already written, so there is nowhere left to
		// report this. Dropping it is the only option.
		return
	}
}

// newGameID returns 128 random bits, hex-encoded. On a server without
// accounts a game ID is the only thing standing between one player's game
// and anyone else who can reach the server, so it must not be guessable, as
// a counter would be.
func newGameID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("generate game id: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}
