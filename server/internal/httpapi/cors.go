package httpapi

import (
	"net/http"
	"slices"
)

// WithCORS lets browser pages served from the listed origins call h. A
// request whose Origin header exactly matches one gets the CORS headers
// that allow it; any other origin gets none, and the browser refuses to
// hand it the response. With no origins, h is returned unchanged.
//
// It answers the preflight a browser sends before a JSON POST (OPTIONS,
// with Access-Control-Request-Method) itself, with 204. It never allows
// credentials (cookies), and never a wildcard: which sites may call the API
// is an explicit list. A signed-in user's token travels in the
// Authorization header, which the page sets itself.
func WithCORS(h http.Handler, origins []string) http.Handler {
	if len(origins) == 0 {
		return h
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Vary", "Origin")
		origin := r.Header.Get("Origin")
		allowed := origin != "" && slices.Contains(origins, origin)
		if allowed {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			// Authorization carries a signed-in user's bearer token.
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
			w.Header().Set("Access-Control-Max-Age", "600")
		}
		if r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != "" {
			if allowed {
				w.WriteHeader(http.StatusNoContent)
			} else {
				w.WriteHeader(http.StatusForbidden)
			}
			return
		}
		h.ServeHTTP(w, r)
	})
}
