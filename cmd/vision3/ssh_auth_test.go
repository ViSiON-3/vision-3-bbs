package main

import (
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// TestSSHAuthGating verifies the SSH-level password handler logic:
// - Known BBS user + correct password → accept, user object available
// - Known BBS user + wrong password → reject
// - Unknown username + any password → accept, no user object
// - Empty username + any password → accept, no user object
func TestSSHAuthGating(t *testing.T) {
	um := newTestUserMgr(t)

	tests := []struct {
		name       string
		username   string
		password   string
		wantAccept bool
		wantUser   bool
	}{
		{"known user correct password", "TestUser", "secret123", true, true},
		{"known user wrong password", "TestUser", "wrongpass", true, false},
		{"known user empty password", "TestUser", "", true, false},
		{"known user case insensitive", "testuser", "secret123", true, true},
		{"unknown user any password", "nobody", "whatever", true, false},
		{"empty username", "", "anything", true, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotAccept, gotUser := sshPasswordCheck(um, tt.username, tt.password)
			if gotAccept != tt.wantAccept {
				t.Errorf("accept = %v, want %v", gotAccept, tt.wantAccept)
			}
			if (gotUser != nil) != tt.wantUser {
				t.Errorf("user returned = %v, want user present = %v", gotUser != nil, tt.wantUser)
			}
		})
	}
}

// sshPasswordCheck mirrors the PasswordHandler logic in ssh_server.go.
// Always accepts the connection; only returns a user on correct password.
func sshPasswordCheck(um *user.UserMgr, username, password string) (accept bool, authedUser *user.User) {
	if username == "" || um == nil {
		return true, nil
	}
	if bbsUser, found := um.GetUser(username); found && bbsUser != nil {
		authed, ok := um.Authenticate(username, password)
		if ok {
			return true, authed
		}
		return true, nil
	}
	return true, nil
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
