package qwkapi

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/message"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// fakeGuard records the calls the API makes into the BBS login guard.
type fakeGuard struct {
	blocked     map[string]bool
	lockedUntil map[string]time.Time
	failed      map[string]int
	cleared     map[string]int
}

func newFakeGuard() *fakeGuard {
	return &fakeGuard{
		blocked:     map[string]bool{},
		lockedUntil: map[string]time.Time{},
		failed:      map[string]int{},
		cleared:     map[string]int{},
	}
}

func (g *fakeGuard) IsBlocked(ip string) bool { return g.blocked[ip] }
func (g *fakeGuard) IsIPLockedOut(ip string) (bool, time.Time, int) {
	until, ok := g.lockedUntil[ip]
	return ok && time.Now().Before(until), until, 0
}
func (g *fakeGuard) RecordFailedLoginAttempt(ip string) bool { g.failed[ip]++; return false }
func (g *fakeGuard) ClearFailedLoginAttempts(ip string)      { g.cleared[ip]++ }

func guardedServer(t *testing.T, g LoginGuard) http.Handler {
	t.Helper()
	s, err := NewServer(Deps{
		Config:    config.QWKAPIConfig{},
		ConfigDir: t.TempDir(),
		Users:     fakeAuth{ok: true},
		Service:   &fakeSvc{},
		AuthorizeFor: func(u *user.User) func(*message.MessageArea) bool {
			return func(*message.MessageArea) bool { return true }
		},
		TwitMatcherFor: func(*user.User) *user.TwitMatcher { return nil },
		DenyLogin:      admitAll,
		Guard:          g,
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	return s.Handler()
}

// loginFrom posts a login from ip and returns the recorded response.
func loginFrom(h http.Handler, ip, handle, password string) *httptest.ResponseRecorder {
	body := fmt.Sprintf(`{"handle":%q,"password":%q}`, handle, password)
	r := clientReq("POST", "/api/qwk/login", []byte(body))
	r.RemoteAddr = ip + ":40000"
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, r)
	return rr
}

func TestAPI_BlockedIPRejectedOnEveryEndpoint(t *testing.T) {
	g := newFakeGuard()
	g.blocked["203.0.113.9"] = true
	h := guardedServer(t, g)
	for _, path := range []string{"/api/qwk/login", "/api/qwk/packet", "/api/qwk/reply"} {
		r := clientReq("POST", path, []byte(`{"handle":"felonius","password":"pw"}`))
		r.RemoteAddr = "203.0.113.9:40000"
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, r)
		if rr.Code != http.StatusForbidden {
			t.Errorf("%s: status = %d, want 403", path, rr.Code)
		}
	}
	if rr := loginFrom(h, "198.51.100.1", "felonius", "pw"); rr.Code != http.StatusOK {
		t.Errorf("unblocked IP: status = %d, want 200", rr.Code)
	}
}

func TestAPI_LockedIPRefusedEvenWithGoodPassword(t *testing.T) {
	g := newFakeGuard()
	g.lockedUntil["203.0.113.9"] = time.Now().Add(10 * time.Minute)
	h := guardedServer(t, g)
	rr := loginFrom(h, "203.0.113.9", "felonius", "pw")
	if rr.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", rr.Code)
	}
	secs, err := strconv.Atoi(rr.Header().Get("Retry-After"))
	if err != nil || secs < 590 || secs > 600 {
		t.Errorf("Retry-After = %q, want ~600", rr.Header().Get("Retry-After"))
	}
}

func TestAPI_LoginReportsToGuard(t *testing.T) {
	g := newFakeGuard()
	h := guardedServer(t, g)
	loginFrom(h, "203.0.113.9", "felonius", "wrong")
	if g.failed["203.0.113.9"] != 1 {
		t.Errorf("failed attempts recorded = %d, want 1", g.failed["203.0.113.9"])
	}
	if rr := loginFrom(h, "203.0.113.9", "felonius", "pw"); rr.Code != http.StatusOK {
		t.Fatalf("good login: status = %d", rr.Code)
	}
	if g.cleared["203.0.113.9"] != 1 {
		t.Errorf("clears = %d, want 1", g.cleared["203.0.113.9"])
	}
}

// Guesses spread across many addresses dodge the per-IP lockout and rate
// limit, so the handle itself must lock.
func TestAPI_HandleLocksAcrossIPs(t *testing.T) {
	h := guardedServer(t, newFakeGuard())
	for i := 0; i < handleMaxFailures; i++ {
		ip := fmt.Sprintf("198.51.100.%d", i+1)
		if rr := loginFrom(h, ip, "Felonius", "wrong"); rr.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d: status = %d, want 401", i+1, rr.Code)
		}
	}
	// Correct password, fresh IP, different case: still locked.
	rr := loginFrom(h, "192.0.2.77", "felonius", "pw")
	if rr.Code != http.StatusTooManyRequests {
		t.Fatalf("after lockout: status = %d, want 429", rr.Code)
	}
	if rr.Header().Get("Retry-After") == "" {
		t.Error("missing Retry-After")
	}
	// Other handles are unaffected.
	if rr := loginFrom(h, "192.0.2.77", "someoneelse", "x"); rr.Code != http.StatusUnauthorized {
		t.Errorf("other handle: status = %d, want 401", rr.Code)
	}
}

func TestAPI_NilGuardStillLocksHandles(t *testing.T) {
	h := guardedServer(t, nil)
	for i := 0; i < handleMaxFailures; i++ {
		loginFrom(h, fmt.Sprintf("198.51.100.%d", i+1), "felonius", "wrong")
	}
	if rr := loginFrom(h, "192.0.2.77", "felonius", "pw"); rr.Code != http.StatusTooManyRequests {
		t.Errorf("status = %d, want 429", rr.Code)
	}
}

func TestHandleLockouts_SuccessClearsCount(t *testing.T) {
	hl := newHandleLockouts(3, time.Minute)
	hl.fail("felonius")
	hl.fail("felonius")
	hl.clear("FELONIUS")
	if hl.fail("felonius") {
		t.Fatal("locked after clear + 1 failure")
	}
	if _, locked := hl.lockedUntil("felonius"); locked {
		t.Fatal("unexpectedly locked")
	}
}

func TestHandleLockouts_ExpiryAndSweep(t *testing.T) {
	hl := newHandleLockouts(2, 20*time.Millisecond)
	hl.fail("felonius")
	if !hl.fail("felonius") {
		t.Fatal("second failure should lock")
	}
	if _, locked := hl.lockedUntil("felonius"); !locked {
		t.Fatal("not locked")
	}
	time.Sleep(30 * time.Millisecond)
	if _, locked := hl.lockedUntil("felonius"); locked {
		t.Fatal("lock did not expire")
	}
	// A failure after expiry starts a fresh count.
	if hl.fail("felonius") {
		t.Fatal("first failure after expiry should not lock")
	}
	time.Sleep(30 * time.Millisecond)
	hl.sweep()
	if hl.size() != 0 {
		t.Errorf("size after sweep = %d, want 0", hl.size())
	}
}
