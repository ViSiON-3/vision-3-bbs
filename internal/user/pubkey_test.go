package user

import (
	"testing"

	"golang.org/x/crypto/ssh"
)

func TestFindByAuthorizedKey(t *testing.T) {
	// A deterministic test key (ed25519 authorized_keys line).
	const line = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIGb9ECWmEzf6FdyLqHU2eXvUjEjzaQjqQVZsGZ3GqGZ7 sysop@test"
	pub, _, _, _, err := ssh.ParseAuthorizedKey([]byte(line))
	if err != nil {
		t.Fatalf("parse test key: %v", err)
	}

	// This test is package user, so it may set the unexported map directly.
	// UserMgr.users is map[string]*User keyed by handle (confirmed in manager.go).
	m := &UserMgr{users: map[string]*User{
		"nobody": {ID: 1, Handle: "nobody", AccessLevel: 10},
		"sysop":  {ID: 2, Handle: "sysop", AccessLevel: 255, PublicKeys: []string{line}},
	}}

	got, ok := m.FindByAuthorizedKey(pub.Marshal())
	if !ok || got.Handle != "sysop" {
		t.Fatalf("expected sysop, got %v %+v", ok, got)
	}

	// Unknown key → not found.
	const other = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA other@x"
	opub, _, _, _, _ := ssh.ParseAuthorizedKey([]byte(other))
	if _, ok := m.FindByAuthorizedKey(opub.Marshal()); ok {
		t.Fatal("unknown key should not match")
	}
}
