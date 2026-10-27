package qwkapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

type tokenEntry struct {
	user      *user.User
	expiresAt time.Time
}

type tokenStore struct {
	mu  sync.Mutex
	ttl time.Duration
	m   map[string]tokenEntry
}

func newTokenStore(ttl time.Duration) *tokenStore {
	return &tokenStore{ttl: ttl, m: make(map[string]tokenEntry)}
}

// Issue creates a random bearer token bound to u, returning it and its expiry.
// It returns an error (and no token) if secure randomness is unavailable.
func (ts *tokenStore) Issue(u *user.User) (string, time.Time, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		// Fail closed: never issue a token from an incomplete random buffer.
		return "", time.Time{}, err
	}
	tok := hex.EncodeToString(raw)
	exp := time.Now().Add(ts.ttl)
	ts.mu.Lock()
	ts.m[tok] = tokenEntry{user: u, expiresAt: exp}
	ts.mu.Unlock()
	return tok, exp, nil
}

// Resolve returns the user for a live token; ok is false if unknown or expired.
func (ts *tokenStore) Resolve(tok string) (*user.User, bool) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	e, ok := ts.m[tok]
	if !ok {
		return nil, false
	}
	if time.Now().After(e.expiresAt) {
		delete(ts.m, tok)
		return nil, false
	}
	return e.user, true
}

// Revoke deletes tok, ending that session; unknown tokens are ignored.
func (ts *tokenStore) Revoke(tok string) {
	ts.mu.Lock()
	delete(ts.m, tok)
	ts.mu.Unlock()
}

func (ts *tokenStore) sweep() {
	now := time.Now()
	ts.mu.Lock()
	for k, e := range ts.m {
		if now.After(e.expiresAt) {
			delete(ts.m, k)
		}
	}
	ts.mu.Unlock()
}

// size reports the number of stored tokens (test helper).
func (ts *tokenStore) size() int {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	return len(ts.m)
}

// expireForTest backdates a token (same-package test helper).
func (ts *tokenStore) expireForTest(tok string) {
	ts.mu.Lock()
	if e, ok := ts.m[tok]; ok {
		e.expiresAt = time.Now().Add(-time.Minute)
		ts.m[tok] = e
	}
	ts.mu.Unlock()
}

type ctxKey int

const userCtxKey ctxKey = 0

func userFromContext(ctx context.Context) *user.User {
	u, _ := ctx.Value(userCtxKey).(*user.User)
	return u
}

// bearerToken resolves the request's bearer token, writing the 401 itself and
// returning ok=false when it is missing, unknown or expired.
func (ts *tokenStore) bearerToken(w http.ResponseWriter, r *http.Request) (tok string, u *user.User, ok bool) {
	auth := r.Header.Get("Authorization")
	tok = strings.TrimPrefix(auth, "Bearer ")
	if tok == auth || tok == "" {
		slog.Info("qwk api auth", "remote", clientIP(r), "path", r.URL.Path, "outcome", "noToken")
		writeError(w, http.StatusUnauthorized, "unauthorized", "missing bearer token")
		return "", nil, false
	}
	u, ok = ts.Resolve(tok)
	if !ok {
		// Routine after a restart (tokens are in-memory), but also what a
		// forged or replayed token looks like, so it is always recorded.
		slog.Info("qwk api auth", "remote", clientIP(r), "path", r.URL.Path, "outcome", "badToken")
		writeError(w, http.StatusUnauthorized, "unauthorized", "invalid or expired token")
		return "", nil, false
	}
	return tok, u, true
}

// requireSession wraps next, admitting only requests with a live token whose
// account may still log in. The account is re-read on every request, so a
// ban, demotion, deletion or password change takes effect on the client's
// next request rather than when its token expires, and the token is revoked
// then. next receives the account as it is now, not as it was at login, so
// write ACS, tagged areas and the signature follow current settings too.
func (s *Server) requireSession(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tok, snap, ok := s.tokens.bearerToken(w, r)
		if !ok {
			return
		}
		cur, found := s.deps.Users.GetUserByID(snap.ID)
		var reason string
		switch {
		case !found || cur.DeletedUser:
			reason = "accountGone"
		case cur.PasswordHash != snap.PasswordHash:
			reason = "passwordChanged"
		}
		if reason != "" {
			s.tokens.Revoke(tok)
			slog.Info("qwk api auth", "handle", snap.Handle, "remote", clientIP(r), "path", r.URL.Path, "outcome", "revoked", "reason", reason)
			writeError(w, http.StatusUnauthorized, "unauthorized", "session no longer valid; log in again")
			return
		}
		if deny := s.deps.DenyLogin(cur); deny != "" {
			s.tokens.Revoke(tok)
			slog.Info("qwk api auth", "handle", cur.Handle, "remote", clientIP(r), "path", r.URL.Path, "outcome", "revoked", "reason", deny)
			writeDenied(w, deny)
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), userCtxKey, cur)))
	}
}
