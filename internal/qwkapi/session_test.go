package qwkapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/message"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// accountStore is an Authenticator over live accounts the test can change
// between requests, as a sysop or the terminal would. Every account's password
// is "pw".
type accountStore struct {
	mu    sync.Mutex
	users []*user.User
}

func newAccountStore(users ...*user.User) *accountStore { return &accountStore{users: users} }

func (a *accountStore) Authenticate(handle, password string) (*user.User, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, u := range a.users {
		if strings.EqualFold(u.Handle, handle) && !u.DeletedUser && password == "pw" {
			c := *u
			return &c, true
		}
	}
	return nil, false
}

func (a *accountStore) GetUserByID(id int) (*user.User, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, u := range a.users {
		if u.ID == id {
			c := *u
			return &c, true
		}
	}
	return nil, false
}

// change applies fn to the stored account under the store's lock.
func (a *accountStore) change(id int, fn func(*user.User)) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, u := range a.users {
		if u.ID == id {
			fn(u)
		}
	}
}

// sessionServer serves one account, ID 7 "felonius" at level 20, under a
// DenyLogin that refuses anything below level 10.
func sessionServer(t *testing.T) (http.Handler, *accountStore, *twitPacketService) {
	t.Helper()
	accounts := newAccountStore(&user.User{ID: 7, Handle: "felonius", AccessLevel: 20, PasswordHash: "hash-1"})
	svc := &twitPacketService{}
	s, err := NewServer(Deps{
		Config:    config.QWKAPIConfig{},
		ConfigDir: t.TempDir(),
		Users:     accounts,
		Service:   svc,
		AuthorizeFor: func(u *user.User) func(*message.MessageArea) bool {
			return func(*message.MessageArea) bool { return true }
		},
		TwitMatcherFor: func(*user.User) *user.TwitMatcher { return nil },
		DenyLogin: func(u *user.User) string {
			if u.AccessLevel < 10 {
				return DenyLogonLevel
			}
			return ""
		},
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	return s.Handler(), accounts, svc
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
