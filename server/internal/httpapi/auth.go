package httpapi

import (
	"errors"
	"net/http"
)

// Authenticator says who sent a request.
//
// The server never sees a password. Signing in happens elsewhere (Cognito,
// in production), and a request carries proof of it, such as a bearer
// token, which an Authenticator checks. See internal/auth for the real one.
type Authenticator interface {
	// Authenticate returns the user r comes from, as a stable ID that never
	// changes for that person. An error means the request carried no
	// acceptable proof; the server answers 401 and the client should sign
	// in again.
	Authenticate(r *http.Request) (user string, err error)
}

// errUnauthorized is the message every 401 carries. It says no more than
// that, whatever the reason: a client's response is the same (refresh its
// token or sign in again), and the reason is logged.
var errUnauthorized = errors.New("sign in required")

// authenticate returns who sent r, answering 401 itself if the request
// cannot be accepted. With no Authenticator, every request is anonymous
// (user ""), which is how the server runs on a developer's machine.
func (s *Server) authenticate(w http.ResponseWriter, r *http.Request) (string, bool) {
	if s.auth == nil {
		return "", true
	}
	user, err := s.auth.Authenticate(r)
	if err != nil {
		if s.logger != nil {
			s.logger.Printf("%s %s: 401: %v", r.Method, r.URL.Path, err)
		}
		// RFC 6750: tell the client which scheme it should be using.
		w.Header().Set("WWW-Authenticate", `Bearer realm="micro"`)
		writeJSON(w, http.StatusUnauthorized, ErrorResponse{Error: errUnauthorized.Error()})
		return "", false
	}
	return user, true
}
