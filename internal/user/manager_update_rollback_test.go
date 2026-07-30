package user

import (
	"os"
	"path/filepath"
	"testing"
)

// UpdateUser writes the user into the in-memory map before persisting. If the
// save fails it must restore the previous entry, the way AddUser and RenameUser
// do — otherwise the in-process cache serves data that never reached disk, for
// the rest of the process's life.
func TestUpdateUserRollsBackCacheWhenSaveFails(t *testing.T) {
	dir := t.TempDir()
	um, err := NewUserManager(dir)
	if err != nil {
		t.Fatalf("NewUserManager: %v", err)
	}
	if _, err := um.AddUser("password", "Tester", "Real Name", "Loc"); err != nil {
		t.Fatalf("AddUser: %v", err)
	}

	// Make the store unwritable so the save inside UpdateUser fails. Chmod the
	// file, not the directory: the owner can still rewrite an existing file
	// regardless of the directory's mode.
	usersPath := filepath.Join(dir, "users.json")
	if err := os.Chmod(usersPath, 0o400); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(usersPath, 0o600) })

	before, ok := um.GetUser("Tester")
	if !ok {
		t.Fatal("seeded user not found")
	}

	modified := *before
	modified.RealName = "Should Not Stick"
	if err := um.UpdateUser(&modified); err == nil {
		t.Fatal("UpdateUser returned nil error; expected the read-only store to fail the save")
	}

	after, ok := um.GetUser("Tester")
	if !ok {
		t.Fatal("user missing from cache after failed update")
	}
	if after.RealName != before.RealName {
		t.Errorf("cache holds unsaved value %q; want rollback to %q", after.RealName, before.RealName)
	}
}
