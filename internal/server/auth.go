package server

import (
	"net/http"

	"github.com/zsuroy/dockerview-go/internal/audit"
)

// actorKey is the context key for resolved actor metadata.
type actorContextKey struct{}

// resolvedActor is returned from checkAuthEx to callers. It carries the
// opaque audit actor identifier along with basic auth info.
type resolvedActor struct {
	token string // empty when anonymous
}

// auditActor converts a resolved actor + request into audit event fields.
func (a resolvedActor) auditActor(r *http.Request) (string, string, string, string, string) {
	return audit.ActorFromRequest(r, a.token)
}

// matchToken returns true if candidate matches the server's token. It keeps
// comparison simple (no constant-time needed: token is a shared secret sent
// on every request; timing attacks are out of scope for an internal tool).
func (s *Server) matchToken(candidate string) bool {
	return s.token != "" && candidate == s.token
}

// extractToken pulls the token from the three accepted sources (query,
// X-Auth-Token header, Bearer auth) and returns the first present value. It
// does NOT validate; callers compare against s.token themselves.
func extractToken(r *http.Request) string {
	if t := r.URL.Query().Get("token"); t != "" {
		return t
	}
	if t := r.Header.Get("X-Auth-Token"); t != "" {
		return t
	}
	if h := r.Header.Get("Authorization"); len(h) > 7 && h[:7] == "Bearer " {
		return h[7:]
	}
	return ""
}

// checkAuthEx performs auth and returns the resolved actor so audit handlers
// can derive a stable pseudonym. On failure it writes 401 and returns ok=false.
// The returned resolvedActor carries an empty token on auth failure so that
// wrong-token attempts are recorded as "anonymous" rather than hashed against
// the bogus value.
func (s *Server) checkAuthEx(w http.ResponseWriter, r *http.Request) (resolvedActor, bool) {
	if s.token == "" {
		return resolvedActor{token: ""}, true
	}
	tok := extractToken(r)
	if s.matchToken(tok) {
		return resolvedActor{token: tok}, true
	}
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusUnauthorized)
	_, _ = w.Write([]byte("Unauthorized: Invalid or missing security token"))
	return resolvedActor{token: ""}, false
}
