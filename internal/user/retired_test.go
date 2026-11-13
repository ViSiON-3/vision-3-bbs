package user

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
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
