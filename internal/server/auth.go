package server

import (
	"crypto/subtle"
	"net/http"
	"strings"
)

// auth gates /api and /stream behind the shared access token when one is
// configured. Static surfaces stay open so the WebView can load the shell
// before any credential exists, and healthz stays plain for scripts.
// Enforcement itself (refusing to run tokenless) lives in main; the token
// arrives via the PICOUCH_ACCESS_TOKEN env var or the config file.
func (a *app) auth(next http.Handler) http.Handler {
	if a.accessToken == "" {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/") && !strings.HasPrefix(r.URL.Path, "/stream/") {
			next.ServeHTTP(w, r)
			return
		}
		if a.tokenOK(r) {
			next.ServeHTTP(w, r)
			return
		}
		a.logger.Warn("denied request", "path", r.URL.Path)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	})
}

// tokenOK compares the presented credential (Bearer header, then the
// picouch_token cookie) against the configured token. An empty token
// never matches: callers that must stay gated with no token configured
// (exec) get false, callers that pass through when unconfigured (auth)
// check for empty before calling.
func (a *app) tokenOK(r *http.Request) bool {
	if a.accessToken == "" {
		return false
	}
	presented := ""
	if v := r.Header.Get("Authorization"); strings.HasPrefix(v, "Bearer ") {
		presented = strings.TrimPrefix(v, "Bearer ")
	}
	if presented == "" {
		if c, err := r.Cookie("picouch_token"); err == nil {
			presented = c.Value
		}
	}
	return subtle.ConstantTimeCompare([]byte(presented), []byte(a.accessToken)) == 1
}
