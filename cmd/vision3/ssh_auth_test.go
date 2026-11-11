package main

import (
	"context"
	"net"
	"sync"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/user"
	"github.com/gliderlabs/ssh"
)

// TestSSHAuthGating checks SSH pre-authentication (the connection itself is
// always accepted; see sshPasswordHandler):
// - Known BBS user + correct password → the user, for logging straight in
// - Known BBS user + wrong or empty password → none (LOGIN menu)
// - Unknown or empty username → none (LOGIN menu)
func TestSSHAuthGating(t *testing.T) {
	um := newTestUserMgr(t)

	tests := []struct {
		name     string
		username string
		password string
		wantUser bool
	}{
		{"known user correct password", "TestUser", "secret123", true},
		{"known user wrong password", "TestUser", "wrongpass", false},
		{"known user empty password", "TestUser", "", false},
		{"known user case insensitive", "testuser", "secret123", true},
		{"unknown user any password", "nobody", "whatever", false},
		{"empty username", "", "anything", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sshPreAuthenticate(um, nil, tt.username, tt.password, "192.0.2.1")
			if (got != nil) != tt.wantUser {
				t.Errorf("user returned = %v, want user present = %v", got != nil, tt.wantUser)
			}
		})
	}
}

// TestSSHAuthLockout: SSH pre-authentication keeps the LOGIN menu's per-IP
// lockout. Wrong passwords count toward it, a locked-out IP gets no
// pre-authentication even with the right password, and a match clears the
// count. Empty passwords and unknown users don't count.
func TestSSHAuthLockout(t *testing.T) {
	um := newTestUserMgr(t)
	ct := NewConnectionTracker(0, 0, 3, 15, "", "")
	const ip = "192.0.2.7"

	// A match clears earlier failures.
	sshPreAuthenticate(um, ct, "TestUser", "wrong1", ip)
	if u := sshPreAuthenticate(um, ct, "TestUser", "secret123", ip); u == nil {
		t.Fatal("correct password after one failure should pre-authenticate")
	}
	if _, _, left := ct.IsIPLockedOut(ip); left != 3 {
		t.Errorf("after a match, attempts left = %d, want 3", left)
	}

	// Empty passwords and unknown users aren't counted.
	for i := 0; i < 5; i++ {
		sshPreAuthenticate(um, ct, "TestUser", "", ip)
		sshPreAuthenticate(um, ct, "nobody", "guess", ip)
	}
	if locked, _, _ := ct.IsIPLockedOut(ip); locked {
		t.Fatal("empty passwords or unknown users locked the IP out")
	}

	// Three wrong passwords lock the IP out.
	for i := 0; i < 3; i++ {
		sshPreAuthenticate(um, ct, "TestUser", "guess", ip)
	}
	if locked, _, _ := ct.IsIPLockedOut(ip); !locked {
		t.Fatal("three wrong SSH passwords should lock the IP out")
	}
	if u := sshPreAuthenticate(um, ct, "TestUser", "secret123", ip); u != nil {
		t.Error("a locked-out IP was pre-authenticated with the right password")
	}

	// Another IP is unaffected.
	if u := sshPreAuthenticate(um, ct, "TestUser", "secret123", "192.0.2.8"); u == nil {
		t.Error("another IP should still pre-authenticate")
	}
}

// newTestUserMgr creates a UserMgr with a single test user for auth tests.
func newTestUserMgr(t *testing.T) *user.UserMgr {
	t.Helper()
	tmpDir := t.TempDir()
	um, err := user.NewUserManager(tmpDir)
	if err != nil {
		t.Fatalf("NewUserManager: %v", err)
	}
	_, err = um.AddUser("secret123", "TestUser", "Test User", "TestGroup")
	if err != nil {
		t.Fatalf("AddUser: %v", err)
	}
	return um
}

// TestSSHAuthLockoutDuringCheck is the race from review: another login from
// the same IP locks it out while this one's correct password is being
// checked. The lockout must stay, and this login must not pre-authenticate.
func TestSSHAuthLockoutDuringCheck(t *testing.T) {
	um := newTestUserMgr(t)
	ct := NewConnectionTracker(0, 0, 3, 15, "", "")
	const ip = "192.0.2.9"

	prev := sshPasswordChecked
	t.Cleanup(func() { sshPasswordChecked = prev })
	sshPasswordChecked = func(string) {
		for i := 0; i < 3; i++ {
			ct.RecordFailedLoginAttempt(ip)
		}
	}

	if u := sshPreAuthenticate(um, ct, "TestUser", "secret123", ip); u != nil {
		t.Error("pre-authenticated although the IP was locked out during the check")
	}
	if locked, _, _ := ct.IsIPLockedOut(ip); !locked {
		t.Error("the lockout started during the check was cleared")
	}
	if u, _ := um.GetUser("TestUser"); u.TimesCalled != 0 || !u.LastLogin.IsZero() {
		t.Errorf("a call was recorded for a login that was turned away: TimesCalled %d, LastLogin %v", u.TimesCalled, u.LastLogin)
	}
}

// AdmitLogin clears an IP's failed attempts unless it is locked out, which
// it leaves in place.
func TestConnectionTrackerAdmitLogin(t *testing.T) {
	ct := NewConnectionTracker(0, 0, 3, 15, "", "")
	if !ct.AdmitLogin("192.0.2.1") {
		t.Error("an IP with no failures wasn't admitted")
	}
	ct.RecordFailedLoginAttempt("192.0.2.1")
	if !ct.AdmitLogin("192.0.2.1") {
		t.Error("an IP under the limit wasn't admitted")
	}
	if _, _, left := ct.IsIPLockedOut("192.0.2.1"); left != 3 {
		t.Errorf("attempts left after admitting = %d, want 3", left)
	}
	for i := 0; i < 3; i++ {
		ct.RecordFailedLoginAttempt("192.0.2.2")
	}
	if ct.AdmitLogin("192.0.2.2") {
		t.Error("a locked-out IP was admitted")
	}
	if locked, _, _ := ct.IsIPLockedOut("192.0.2.2"); !locked {
		t.Error("AdmitLogin cleared a lockout")
	}
}

// fakeSSHContext is just enough of an ssh.Context for sshPasswordHandler.
type fakeSSHContext struct {
	context.Context
	sync.Mutex
	user   string
	addr   net.Addr
	values map[any]any
}

func (c *fakeSSHContext) User() string                  { return c.user }
func (c *fakeSSHContext) SessionID() string             { return "test" }
func (c *fakeSSHContext) ClientVersion() string         { return "SSH-2.0-test" }
func (c *fakeSSHContext) ServerVersion() string         { return "SSH-2.0-Vision3" }
func (c *fakeSSHContext) RemoteAddr() net.Addr          { return c.addr }
func (c *fakeSSHContext) LocalAddr() net.Addr           { return c.addr }
func (c *fakeSSHContext) Permissions() *ssh.Permissions { return &ssh.Permissions{} }
func (c *fakeSSHContext) SetValue(key, value any)       { c.values[key] = value }

// sshPasswordHandler always accepts the connection, so a caller who doesn't
// pre-authenticate (wrong or empty password, or a locked-out IP) still
// reaches the LOGIN menu; only a match stashes the user.
func TestSSHPasswordHandlerFallsBackToLogin(t *testing.T) {
	prevUM, prevCT := userMgr, connectionTracker
	t.Cleanup(func() { userMgr, connectionTracker = prevUM, prevCT })
	userMgr = newTestUserMgr(t)
	connectionTracker = NewConnectionTracker(0, 0, 3, 15, "", "")
	for i := 0; i < 3; i++ {
		connectionTracker.RecordFailedLoginAttempt("192.0.2.66")
	}

	for _, tt := range []struct {
		name, ip, password string
		wantUser           bool
	}{
		{"correct password", "192.0.2.10", "secret123", true},
		{"wrong password", "192.0.2.11", "wrong", false},
		{"empty password", "192.0.2.12", "", false},
		{"locked-out IP", "192.0.2.66", "secret123", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx := &fakeSSHContext{
				Context: context.Background(),
				user:    "TestUser",
				addr:    &net.TCPAddr{IP: net.ParseIP(tt.ip), Port: 50000},
				values:  map[any]any{},
			}
			if !sshPasswordHandler(ctx, tt.password) {
				t.Fatal("handler refused the connection; the caller should reach LOGIN")
			}
			_, got := ctx.values[sshAuthUserKey{}]
			if got != tt.wantUser {
				t.Errorf("user stashed = %v, want %v", got, tt.wantUser)
			}
		})
	}
}

// TestSSHAuthConcurrentGuesses: SSH connections from one IP that try
// passwords at the same moment get no more guesses than maxFailedLogins.
// Without the per-IP serialization they would all pass the lockout check
// before any failure was recorded. A correct password among the late ones
// isn't admitted once the IP is locked out.
func TestSSHAuthConcurrentGuesses(t *testing.T) {
	um := newTestUserMgr(t)
	ct := NewConnectionTracker(0, 0, 3, 15, "", "")
	const ip = "192.0.2.77"

	var start, done sync.WaitGroup
	start.Add(1)
	admitted := make(chan bool, 11)
	for i := 0; i < 10; i++ {
		done.Add(1)
		go func() {
			defer done.Done()
			start.Wait()
			admitted <- sshPreAuthenticate(um, ct, "TestUser", "guess", ip) != nil
		}()
	}
	start.Done()
	done.Wait()

	ct.mu.Lock()
	attempts := ct.failedLogins[ip].Attempts
	ct.mu.Unlock()
	if attempts != 3 {
		t.Errorf("failed attempts recorded = %d, want 3: more guesses got past the lockout check than maxFailedLogins", attempts)
	}
	if u := sshPreAuthenticate(um, ct, "TestUser", "secret123", ip); u != nil {
		t.Error("the right password was admitted from a locked-out IP")
	}
	close(admitted)
	for a := range admitted {
		if a {
			t.Error("a wrong password was admitted")
		}
	}
}

// ipLocks drops an address's lock once nobody holds or waits for it.
func TestIPLocksForget(t *testing.T) {
	var l ipLocks
	unlock := l.lock("192.0.2.1")
	unlock()
	l.mu.Lock()
	n := len(l.locks)
	l.mu.Unlock()
	if n != 0 {
		t.Errorf("%d lock(s) left after unlocking, want 0", n)
	}
}
