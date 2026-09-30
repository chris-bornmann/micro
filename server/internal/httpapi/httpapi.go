// Package httpapi serves the game over HTTP.
//
// It is a thin facade over game.Game and holds no game logic itself. Its only
// real responsibilities are routing, turning errors into status codes, and
// keeping a registry of live games.
//
// Scope, stated plainly: games live in memory, are never persisted, and are
// never evicted. A restart loses every game in progress, and a long-running
// server accumulates them. That is the right trade for a single-player game
// played by one person on their own machine, which is what this is; it would
// not be for anything else. The URLs carry a game id from the start so that
// growing out of this does not mean changing the shape of the API.
//
// Nothing a client sends can widen what it is allowed to see. Dungeon
// geometry comes from the server's own flags, not from a request, and the
// reveal-hidden and show-full-map cheats are not reachable over HTTP at all.
// The only thing a client may choose is the seed, and only so a bug can be
// reproduced.
package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
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

// Server routes requests to live games.
type Server struct {
	mux    *http.ServeMux
	new    NewGameFunc
	logger *log.Logger

	mu    sync.RWMutex
	games map[string]*game.Game
	next  int
}

// New builds a Server. logger may be nil, in which case request errors are
// not logged.
func New(newGame NewGameFunc, logger *log.Logger) *Server {
	s := &Server{
		mux:    http.NewServeMux(),
		new:    newGame,
		logger: logger,
		games:  map[string]*game.Game{},
	}

	s.mux.HandleFunc("GET /healthz", s.handleHealth)
	s.mux.HandleFunc("POST /v1/games", s.handleNewGame)
	s.mux.HandleFunc("GET /v1/games/{id}/frame", s.handleFrame)
	s.mux.HandleFunc("POST /v1/games/{id}/command", s.handleCommand)
	s.mux.HandleFunc("POST /v1/games/{id}/commands", s.handleCommands)
	s.mux.HandleFunc("GET /v1/games/{id}/views/{name}", s.handleView)

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

func (s *Server) handleNewGame(w http.ResponseWriter, r *http.Request) {
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

	s.mu.Lock()
	s.next++
	id := fmt.Sprintf("g%d", s.next)
	s.games[id] = g
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
	// the player service and costs a turn, exactly as it did before the
	// split — pressing an unbound key has always wasted a turn. Validating
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

// lookup resolves {id} to a live game, answering 404 itself if there is none.
func (s *Server) lookup(w http.ResponseWriter, r *http.Request) (*game.Game, bool) {
	id := r.PathValue("id")

	s.mu.RLock()
	g, ok := s.games[id]
	s.mu.RUnlock()

	if !ok {
		s.fail(w, r, http.StatusNotFound, fmt.Errorf("no game %q", id))
		return nil, false
	}
	return g, true
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
