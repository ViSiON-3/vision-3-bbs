package qwkapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// sessionServer serves testAccount (ID 7 "felonius" at level 20) under a
// DenyLogin that refuses anything below level 10.
func sessionServer(t *testing.T) (http.Handler, *accountStore, *twitPacketService) {
	t.Helper()
	accounts := newAccountStore(testAccount())
	svc := &twitPacketService{}
	deps := testDeps(t, accounts, svc)
	deps.DenyLogin = func(u *user.User) string {
		if u.AccessLevel < 10 {
			return DenyLogonLevel
		}
		return ""
	}
	return newTestServer(t, deps), accounts, svc
}

func withToken(method, path, tok string) *http.Request {
	r := clientReq(method, path, nil)
	if tok != "" {
		r.Header.Set("Authorization", "Bearer "+tok)
	}
	return r
}

func serve(h http.Handler, r *http.Request) *httptest.ResponseRecorder {
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, r)
	return rr
}

// A change that ends the account's right to a session revokes the token on
// its next use; undoing the change afterwards does not bring it back.
func TestSession_RevokedWhenAccountChanges(t *testing.T) {
	cases := []struct {
		name     string
		change   func(*user.User)
		undo     func(*user.User)
		wantCode int
	}{
		{"deleted", func(u *user.User) { u.DeletedUser = true }, func(u *user.User) { u.DeletedUser = false }, http.StatusUnauthorized},
		{"password changed", func(u *user.User) { u.PasswordHash = "hash-2" }, func(u *user.User) { u.PasswordHash = "hash-1" }, http.StatusUnauthorized},
		{"banned", func(u *user.User) { u.AccessLevel = 0 }, func(u *user.User) { u.AccessLevel = 20 }, http.StatusForbidden},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h, accounts, _ := sessionServer(t)
			tok := login(t, h)
			if rr := serve(h, withToken("GET", "/api/qwk/packet", tok)); rr.Code != http.StatusNoContent {
				t.Fatalf("before change: status = %d, want 204", rr.Code)
			}
			accounts.change(7, c.change)
			if rr := serve(h, withToken("GET", "/api/qwk/packet", tok)); rr.Code != c.wantCode {
				t.Fatalf("after change: status = %d, want %d", rr.Code, c.wantCode)
			}
			accounts.change(7, c.undo)
			if rr := serve(h, withToken("POST", "/api/qwk/reply", tok)); rr.Code != http.StatusUnauthorized {
				t.Errorf("after undo: status = %d, want 401 (token revoked)", rr.Code)
			}
		})
	}
}

func TestSession_BannedGetsDenialReason(t *testing.T) {
	h, accounts, _ := sessionServer(t)
	tok := login(t, h)
	accounts.change(7, func(u *user.User) { u.AccessLevel = 0 })
	rr := serve(h, withToken("GET", "/api/qwk/packet", tok))
	var body map[string]string
	_ = json.Unmarshal(rr.Body.Bytes(), &body)
	if body["error"] != DenyLogonLevel {
		t.Errorf("error = %q, want %q", body["error"], DenyLogonLevel)
	}
}

// Handlers see the account as saved now, not the login snapshot.
func TestSession_HandlersGetCurrentAccount(t *testing.T) {
	h, accounts, svc := sessionServer(t)
	tok := login(t, h)
	accounts.change(7, func(u *user.User) { u.TaggedMessageAreaTags = []string{"GENERAL"} })
	if rr := serve(h, withToken("GET", "/api/qwk/packet", tok)); rr.Code != http.StatusNoContent {
		t.Fatalf("status = %d", rr.Code)
	}
	if len(svc.opts.TaggedTags) != 1 || svc.opts.TaggedTags[0] != "GENERAL" {
		t.Errorf("TaggedTags = %v, want [GENERAL]", svc.opts.TaggedTags)
	}
}

func TestLogout(t *testing.T) {
	h, _, _ := sessionServer(t)
	tok := login(t, h)
	if rr := serve(h, withToken("POST", "/api/qwk/logout", tok)); rr.Code != http.StatusNoContent {
		t.Fatalf("logout: status = %d, want 204", rr.Code)
	}
	if rr := serve(h, withToken("GET", "/api/qwk/packet", tok)); rr.Code != http.StatusUnauthorized {
		t.Errorf("packet after logout: status = %d, want 401", rr.Code)
	}
	if rr := serve(h, withToken("POST", "/api/qwk/logout", tok)); rr.Code != http.StatusUnauthorized {
		t.Errorf("second logout: status = %d, want 401", rr.Code)
	}
	if rr := serve(h, withToken("POST", "/api/qwk/logout", "")); rr.Code != http.StatusUnauthorized {
		t.Errorf("logout without token: status = %d, want 401", rr.Code)
	}
	if rr := serve(h, withToken("GET", "/api/qwk/logout", tok)); rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET logout: status = %d, want 405", rr.Code)
	}
}

// A client can always discard its own session, even once the account may no
// longer log in.
func TestLogout_WorksForDeniedAccount(t *testing.T) {
	h, accounts, _ := sessionServer(t)
	tok := login(t, h)
	accounts.change(7, func(u *user.User) { u.AccessLevel = 0 })
	if rr := serve(h, withToken("POST", "/api/qwk/logout", tok)); rr.Code != http.StatusNoContent {
		t.Errorf("logout: status = %d, want 204", rr.Code)
	}
}

// External ./ue edits only reach the in-memory accounts through SyncFromDisk,
// so login and every session check must call it before reading the account.
func TestSession_SyncsExternalEditsBeforeEachRead(t *testing.T) {
	h, accounts, _ := sessionServer(t)
	before := accounts.syncCount()
	passwordLogin(t, h)
	if got := accounts.syncCount() - before; got != 1 {
		t.Fatalf("syncs for a password login = %d, want 1", got)
	}
	tok := login(t, h)
	before = accounts.syncCount()
	serve(h, withToken("GET", "/api/qwk/packet", tok))
	serve(h, withToken("POST", "/api/qwk/reply", tok))
	if got := accounts.syncCount() - before; got != 2 {
		t.Errorf("syncs for two requests = %d, want 2", got)
	}
}
