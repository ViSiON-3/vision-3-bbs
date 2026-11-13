package user

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/atomicfile"
)

// Purging an account retires its handle: it can't be registered, or taken
// by a rename, again; and its old mail isn't the new account's to read.
func TestPurgeRetiresHandle(t *testing.T) {
	dir := t.TempDir()
	um, err := NewUserManager(dir)
	if err != nil {
		t.Fatal(err)
	}
	gone, err := um.AddUser("password1", "Bob", "Bob Gone", "")
	if err != nil {
		t.Fatal(err)
	}
	other, err := um.AddUser("password1", "Ann", "Ann Here", "")
	if err != nil {
		t.Fatal(err)
	}
	stored, _ := um.GetUserByID(gone.ID)
	old := time.Now().AddDate(0, 0, -30)
	stored.DeletedUser, stored.DeletedAt = true, &old
	if err := um.UpdateUserByID(stored); err != nil {
		t.Fatal(err)
	}
	if res, err := um.PurgeDeletedUsers(1); err != nil || len(res) != 1 {
		t.Fatalf("purge: %v %v", res, err)
	}

	if _, err := um.AddUser("password2", "BOB", "Someone Else", ""); !errors.Is(err, ErrHandleRetired) {
		t.Errorf("registering a retired handle: %v", err)
	}
	if !um.HandleRetired("bob") {
		t.Error("HandleRetired(bob) = false")
	}
	ann, _ := um.GetUserByID(other.ID)
	ann.Handle = "Bob"
	if err := um.UpdateUserByID(ann); !errors.Is(err, ErrHandleRetired) {
		t.Errorf("renaming to a retired handle: %v", err)
	}

	// Releasing it is deleting its line.
	if err := os.WriteFile(filepath.Join(dir, RetiredHandlesFile), []byte("# none\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := um.AddUser("password2", "Bob", "Someone Else", ""); err != nil {
		t.Errorf("after release: %v", err)
	}
}

// A retired list that can't be read refuses new accounts, and a purge that
// can't retire the handles purges nothing.
func TestRetiredListUnreadableFailsClosed(t *testing.T) {
	dir := t.TempDir()
	um, err := NewUserManager(dir)
	if err != nil {
		t.Fatal(err)
	}
	gone, _ := um.AddUser("password1", "Bob", "Bob Gone", "")
	stored, _ := um.GetUserByID(gone.ID)
	old := time.Now().AddDate(0, 0, -30)
	stored.DeletedUser, stored.DeletedAt = true, &old
	if err := um.UpdateUserByID(stored); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, RetiredHandlesFile), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := um.AddUser("password1", "Carol", "Carol New", ""); err == nil {
		t.Error("an account was created with an unreadable retired list")
	}
	if _, err := um.PurgeDeletedUsers(1); err == nil {
		t.Error("purged without retiring the handle")
	}
	if _, ok := um.GetUserByID(gone.ID); !ok {
		t.Error("the account went though its handle wasn't retired")
	}
}

// A rename retires the old handle; a case-only change doesn't.
func TestRenameRetiresOldHandle(t *testing.T) {
	dir := t.TempDir()
	um, err := NewUserManager(dir)
	if err != nil {
		t.Fatal(err)
	}
	bob, _ := um.AddUser("password1", "Bob", "Bob Smith", "")
	u, _ := um.GetUserByID(bob.ID)
	u.Handle = "BOB"
	if err := um.UpdateUserByID(u); err != nil {
		t.Fatal(err)
	}
	if um.HandleRetired("bob") {
		t.Fatal("a case-only change retired the handle")
	}
	u.Handle = "Robert"
	if err := um.UpdateUserByID(u); err != nil {
		t.Fatal(err)
	}
	if _, err := um.AddUser("password2", "Bob", "Someone Else", ""); !errors.Is(err, ErrHandleRetired) {
		t.Errorf("registering a renamed account's old handle: %v", err)
	}
}

// Retired handles match as private mail matches handles (EqualFold), so a
// handle that only folds to a retired one is refused too; and a handle
// starting with '#' or '\' survives the list's comment syntax.
func TestRetiredHandlesFoldAndEscape(t *testing.T) {
	path := filepath.Join(t.TempDir(), "users.json")
	if err := RetireHandles(path, "Sam", "#hash", `\slash`); err != nil {
		t.Fatal(err)
	}
	for _, h := range []string{"sam", "SAM", "ſam", "#HASH", `\slash`} {
		if retired, err := HandleRetired(path, h); err != nil || !retired {
			t.Errorf("HandleRetired(%q) = %v, %v; want true", h, retired, err)
		}
	}
	if retired, _ := HandleRetired(path, "hash"); retired {
		t.Error("an unrelated handle reads as retired")
	}
	// Retiring a fold-equal handle again adds nothing.
	if err := RetireHandles(path, "ſAM"); err != nil {
		t.Fatal(err)
	}
	if list, _ := readRetired(path); len(list) != 3 {
		t.Errorf("list = %q, want 3 entries", list)
	}
}

// A change mail sees as the same handle isn't retired, so the account can
// change back; a handle with a line break or other control character is
// refused outright, so the list stays one handle per line.
func TestFoldEqualRenameAndControlHandles(t *testing.T) {
	dir := t.TempDir()
	um, err := NewUserManager(dir)
	if err != nil {
		t.Fatal(err)
	}
	sam, _ := um.AddUser("password1", "Sam", "Sam Smith", "")
	u, _ := um.GetUserByID(sam.ID)
	u.Handle = "ſam"
	if err := um.UpdateUserByID(u); err != nil {
		t.Fatal(err)
	}
	if um.HandleRetired("Sam") {
		t.Fatal("a fold-equal change retired the handle")
	}
	u.Handle = "Sam"
	if err := um.UpdateUserByID(u); err != nil {
		t.Errorf("changing back: %v", err)
	}

	if _, err := um.AddUser("password1", "Two\nLines", "Two Lines", ""); !errors.Is(err, ErrHandleControl) {
		t.Errorf("AddUser with a line break: %v", err)
	}
	if err := ValidateHandleEdit("Sam", "Tab\there"); !errors.Is(err, ErrHandleControl) {
		t.Errorf("ValidateHandleEdit with a tab: %v", err)
	}
}

// A handle with a line break, from an account made before those were
// refused, is retired as one entry, not two.
func TestRetireLegacyControlHandle(t *testing.T) {
	path := filepath.Join(t.TempDir(), "users.json")
	if err := RetireHandles(path, "A\nB", `"quoted`); err != nil {
		t.Fatal(err)
	}
	for h, want := range map[string]bool{"A\nB": true, `"quoted`: true, "A": false, "B": false} {
		if got, err := HandleRetired(path, h); err != nil || got != want {
			t.Errorf("HandleRetired(%q) = %v, %v; want %v", h, got, err, want)
		}
	}
}

// A retry that finds the entry already written still syncs the directory,
// so a purge never goes ahead on a rename whose sync failed.
func TestRetryStillSyncsDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "users.json")
	if err := RetireHandles(path, "Bob"); err != nil {
		t.Fatal(err)
	}
	if err := raiseNextUserID(path, 5); err != nil {
		t.Fatal(err)
	}
	failing := errors.New("sync failed")
	syncDir = func(string) error { return failing }
	t.Cleanup(func() { syncDir = atomicfile.SyncDir })
	if err := RetireHandles(path, "bob"); !errors.Is(err, failing) {
		t.Errorf("RetireHandles retry: %v, want the sync error", err)
	}
	if err := raiseNextUserID(path, 3); !errors.Is(err, failing) {
		t.Errorf("raiseNextUserID retry: %v, want the sync error", err)
	}
}
