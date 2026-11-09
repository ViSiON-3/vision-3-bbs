package user

import (
	"os"
	"runtime"
	"testing"
)

// A save that fails leaves the account in memory as it is on disk, whether
// or not the handle changed, so the next save that succeeds still writes
// it. Before this, a failed save with the handle unchanged dropped the
// account from the map.
func TestUpdateUserByIDFailedSaveKeepsAccount(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs a directory the test cannot write to")
	}
	dir := t.TempDir()
	um, err := NewUserManager(dir)
	if err != nil {
		t.Fatal(err)
	}
	u, err := um.AddUser("password", "Reader", "Real Reader", "Loc")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

	edited := *u
	edited.GroupLocation = "Elsewhere"
	if err := um.UpdateUserByID(&edited); err == nil {
		t.Fatal("UpdateUserByID succeeded on a read-only directory")
	}
	got, ok := um.GetUserByID(u.ID)
	if !ok || got.GroupLocation != "Loc" {
		t.Fatalf("after a failed save: ok=%v user=%+v, want the account as on disk", ok, got)
	}

	// Writable again, the account is in the next save.
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := um.UpdateUserByID(&edited); err != nil {
		t.Fatalf("save after the directory came back: %v", err)
	}
	reloaded, err := NewUserManager(dir)
	if err != nil {
		t.Fatal(err)
	}
	if r, ok := reloaded.GetUserByID(u.ID); !ok || r.GroupLocation != "Elsewhere" {
		t.Errorf("on disk after the second save: ok=%v user=%+v", ok, r)
	}
}
