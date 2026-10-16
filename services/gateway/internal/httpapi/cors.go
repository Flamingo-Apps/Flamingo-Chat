package httpapi

import (
	"net/http"
	"path"
	"strings"
)

// CORS wraps next with the browser-facing CORS headers every endpoint
// needs - nothing here was reachable from a browser before this (every
// prior manual/e2e check used curl or a Go client, neither of which
// enforce CORS), so this had simply never been exercised.
//
// originPatterns uses the exact same patterns and matching
// (lower-cased path.Match against the request's Host, following
// coder/websocket's own authenticateOrigin) as ws.Serve's
// OriginPatterns (docs/ARCHITECTURE.md §4.4's WebSocket origin check) -
// one trust boundary, not two independently-configured ones.
func CORS(originPatterns []string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" && originAllowed(originPatterns, origin) {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func originAllowed(patterns []string, origin string) bool {
	host := origin
	if idx := strings.Index(origin, "://"); idx != -1 {
		host = origin[idx+3:]
	}
	host = strings.ToLower(host)
	for _, pattern := range patterns {
		if matched, err := path.Match(strings.ToLower(pattern), host); err == nil && matched {
			return true
		}
	}
	return false
}
