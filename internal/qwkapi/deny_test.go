package qwkapi

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

func denyingServer(t *testing.T, reason string, g LoginGuard) http.Handler {
	t.Helper()
	deps := testDeps(t, newAccountStore(testAccount()), &fakeSvc{})
	deps.DenyLogin = func(*user.User) string { return reason }
	deps.Guard = g
	return newTestServer(t, deps)
}

// A correct password for an account the terminal would refuse gets no token.
func TestAPI_LoginDeniedByAccountChecks(t *testing.T) {
	for _, reason := range []string{DenyIntroPending, DenyLogonLevel} {
		t.Run(reason, func(t *testing.T) {
			g := newFakeGuard()
			h := denyingServer(t, reason, g)
			rr := loginFrom(h, "203.0.113.9", "felonius", "pw")
			if rr.Code != http.StatusForbidden {
				t.Fatalf("status = %d, want 403", rr.Code)
			}
			var body map[string]string
			_ = json.Unmarshal(rr.Body.Bytes(), &body)
			if body["error"] != reason || body["message"] == "" {
				t.Errorf("body = %v, want error %q with a message", body, reason)
			}
			if _, hasToken := body["token"]; hasToken {
				t.Error("denied login returned a token")
			}
			// Not a failed guess, so not counted; not a session, so not cleared.
			if g.failed["203.0.113.9"] != 0 || g.cleared["203.0.113.9"] != 0 {
				t.Errorf("guard calls: failed=%d cleared=%d, want 0/0", g.failed["203.0.113.9"], g.cleared["203.0.113.9"])
			}
		})
	}
}

func TestNewServerRequiresDenyLogin(t *testing.T) {
	_, err := NewServer(Deps{
		ConfigDir:      t.TempDir(),
		Service:        &fakeSvc{},
		TwitMatcherFor: func(*user.User) *user.TwitMatcher { return nil },
	})
	if err == nil {
		t.Fatal("NewServer accepted a nil DenyLogin")
	}
}
