package user

import (
	"testing"

	"golang.org/x/crypto/ssh"
)

// A soft-deleted account must not be able to authenticate by SSH key.
// FindByAuthorizedKey is the lookup behind key-based login, and the caller
// (cmd/vision3) only rechecks access level, not DeletedUser — so if this
// returns a deleted user, a removed account keeps working.
func TestFindByAuthorizedKeySkipsDeletedUsers(t *testing.T) {
	// A syntactically valid authorized_keys line.
	const keyLine = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIJx0eUyJ1cVfhVJZ2m8mZmXaJXVMRxHi/8oRSPCbf5oM tester"
	pub, _, _, _, err := ssh.ParseAuthorizedKey([]byte(keyLine))
	if err != nil {
		t.Fatalf("ParseAuthorizedKey: %v", err)
	}
	marshaled := pub.Marshal()

	um := NewUserMgrForTest(&User{
		ID:          1,
		Handle:      "Ghost",
		PublicKeys:  []string{keyLine},
		DeletedUser: true,
	})

	if got, ok := um.FindByAuthorizedKey(marshaled); ok {
		t.Errorf("deleted user %q was returned for key lookup; a removed account can still log in", got.Handle)
	}
}

// An active account with the same shape must still be found, so the guard
// cannot be satisfied by rejecting everything.
func TestFindByAuthorizedKeyFindsActiveUsers(t *testing.T) {
	const keyLine = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIJx0eUyJ1cVfhVJZ2m8mZmXaJXVMRxHi/8oRSPCbf5oM tester"
	pub, _, _, _, err := ssh.ParseAuthorizedKey([]byte(keyLine))
	if err != nil {
		t.Fatalf("ParseAuthorizedKey: %v", err)
	}

	um := NewUserMgrForTest(&User{ID: 1, Handle: "Active", PublicKeys: []string{keyLine}})

	got, ok := um.FindByAuthorizedKey(pub.Marshal())
	if !ok {
		t.Fatal("active user not found by key")
	}
	if got.Handle != "Active" {
		t.Errorf("found %q, want Active", got.Handle)
	}
}
