package apiauth

import (
	"crypto/subtle"
	"net/http"
	"strings"
)

// Middleware returns an http middleware that enforces a Bearer token on every
// request, except:
//   - CORS preflight (OPTIONS) — browsers do not send Authorization on preflight
//   - paths in exemptPaths (exact match) — the playground HTML page is exempt
//     so it can be opened in a browser; it then collects the passphrase and
//     authenticates subsequent JS calls.
//
// For /api/events requests that cannot send the Authorization header (notably
// the browser EventSource API), the token may be supplied via the "token" query
// parameter. Other endpoints intentionally reject query-string tokens because
// URLs are more likely to end up in logs and shell history.
func Middleware(expectedToken string, exemptPaths map[string]bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodOptions || exemptPaths[r.URL.Path] {
				next.ServeHTTP(w, r)
				return
			}

			provided := extractToken(r)
			if provided == "" {
				writeUnauthorized(w, "missing bearer token")
				return
			}
			// subtle.ConstantTimeCompare returns 0 immediately on length
			// mismatch, which technically leaks length. Acceptable here:
			// expectedToken always has fixed length 43 (32-byte PBKDF2
			// output, base64url-encoded without padding), so a wrong-length
			// input only tells the attacker what they could have computed
			// from the public algorithm anyway.
			if subtle.ConstantTimeCompare([]byte(provided), []byte(expectedToken)) != 1 {
				writeUnauthorized(w, "invalid bearer token")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func extractToken(r *http.Request) string {
	if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimPrefix(auth, "Bearer ")
	}
	if r.URL.Path != "/api/events" {
		return ""
	}
	return r.URL.Query().Get("token")
}

func writeUnauthorized(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("WWW-Authenticate", `Bearer realm="lazyagent"`)
	w.WriteHeader(http.StatusUnauthorized)
	_, _ = w.Write([]byte(`{"error":"` + msg + `"}`))
}
