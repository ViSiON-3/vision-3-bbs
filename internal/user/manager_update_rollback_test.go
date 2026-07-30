package user

import (
	"os"
	"path/filepath"
	"testing"
)

// UpdateUser writes the user into the in-memory map before persisting. If the
// save fails it must restore the previous entry, the way AddUser and
// UpdateUserByID do — otherwise the in-process cache serves data that never
// reached disk, for the rest of the process's life.
func TestUpdateUserRollsBackCacheWhenSaveFails(t *testing.T) {
	dir := t.TempDir()
	um, err := NewUserManager(dir)
	if err != nil {
		t.Fatalf("NewUserManager: %v", err)
	}
	if _, err := um.AddUser("password", "Tester", "Real Name", "Loc"); err != nil {
		t.Fatalf("AddUser: %v", err)
	}

	// Point the store at an existing directory: os.WriteFile can never write
	// bytes into a directory inode, so the save fails deterministically on every
	// platform and regardless of privileges. (chmod would not — the owner can
	// still rewrite a read-only file, and root ignores the mode entirely.)
	blocker := filepath.Join(dir, "blocked")
	if err := os.MkdirAll(blocker, 0o750); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	um.path = blocker

	before, ok := um.GetUser("Tester")
	if !ok {
		t.Fatal("seeded user not found")
	}

	modified := *before
	modified.RealName = "Should Not Stick"
	if err := um.UpdateUser(&modified); err == nil {
		t.Fatal("UpdateUser returned nil error; expected the unwritable store to fail the save")
	}

	after, ok := um.GetUser("Tester")
	if !ok {
		t.Fatal("user missing from cache after failed update")
	}
	if after.RealName != before.RealName {
		t.Errorf("cache holds unsaved value %q; want rollback to %q", after.RealName, before.RealName)
	}
}
