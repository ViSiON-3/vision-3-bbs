package main

import (
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/user"
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
