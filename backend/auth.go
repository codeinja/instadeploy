package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/lib/pq"
)

type ctxKey string

const (
	userIDKey  ctxKey = "user_id"
	agentIDKey ctxKey = "agent_id"
)

// Users sign in through Better Auth (in the Next.js frontend), which stores
// sessions in the shared "session" table. The browser's session cookie is
// "<token>.<signature>"; we only need the token to look the session up.
// API clients can send the same token as "Authorization: Bearer <token>".
var sessionCookies = []string{"__Secure-better-auth.session_token", "better-auth.session_token"}

// sessionTokens returns every session token the request carries. A browser
// can hold more than one session cookie (a stale one from an old install,
// or both the __Secure- and plain variants); any valid one is accepted.
func sessionTokens(r *http.Request) (tokens []string, fromCookie bool) {
	if t := bearerToken(r); t != "" {
		t, _, _ = strings.Cut(t, ".")
		return []string{t}, false
	}
	for _, c := range r.Cookies() {
		if !slices.Contains(sessionCookies, c.Name) {
			continue
		}
		v, _ := url.QueryUnescape(c.Value)
		if v, _, _ = strings.Cut(v, "."); v != "" {
			tokens = append(tokens, v)
			fromCookie = true
		}
	}
	return tokens, fromCookie
}

// crossOrigin reports a browser request made from another site. Requests
// through the dashboard's /api proxy carry no Origin header; browsers send
// one when a page calls this API directly. Cookies are sent to every port
// of a host, so without this check another site could reach this API with
// the user's cookie.
func (s *Server) crossOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" || r.Method == http.MethodGet || r.Method == http.MethodHead {
		return false
	}
	return origin != s.cfg.DashboardOrigin
}

// Agent tokens are random 256-bit values. Because they're high-entropy, a
// plain SHA-256 is enough to store them safely (no need for bcrypt).
func newAgentToken() (token, hash string) {
	b := make([]byte, 32)
	rand.Read(b)
	token = "idt_" + hex.EncodeToString(b)
	return token, hashToken(token)
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func bearerToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if t, ok := strings.CutPrefix(h, "Bearer "); ok {
		return strings.TrimSpace(t)
	}
	return ""
}

func (s *Server) requireUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tokens, fromCookie := sessionTokens(r)
		if fromCookie && s.crossOrigin(r) {
			writeError(w, http.StatusForbidden, "cross-site request blocked")
			return
		}
		var userID string
		err := s.db.QueryRowContext(r.Context(),
			`SELECT "userId" FROM "session" WHERE "token" = ANY($1) AND "expiresAt" > now() LIMIT 1`, pq.Array(tokens)).Scan(&userID)
		if len(tokens) == 0 || err != nil {
			writeError(w, http.StatusUnauthorized, "not logged in")
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), userIDKey, userID)))
	})
}

func (s *Server) requireAgent(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := bearerToken(r)
		var agentID string
		err := s.db.QueryRowContext(r.Context(),
			`UPDATE agents SET last_seen = now(), status = 'ONLINE' WHERE token_hash = $1 AND revoked_at IS NULL RETURNING id`,
			hashToken(token)).Scan(&agentID)
		if token == "" || err != nil {
			writeError(w, http.StatusUnauthorized, "invalid agent token")
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), agentIDKey, agentID)))
	})
}

func userID(r *http.Request) string  { return r.Context().Value(userIDKey).(string) }
func agentID(r *http.Request) string { return r.Context().Value(agentIDKey).(string) }

func randomHex(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return fmt.Sprintf("%x", b)
}
