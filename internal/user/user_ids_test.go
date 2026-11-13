package user

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// Purging the newest account and starting again doesn't give its ID to the
// next sign-up.
func TestPurgedUserIDIsNotReused(t *testing.T) {
	dir := t.TempDir()
	um, err := NewUserManager(dir)
	if err != nil {
		t.Fatal(err)
	}
	gone, err := um.AddUser("password1", "Gone", "Gone Away", "")
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

	again, err := NewUserManager(dir)
	if err != nil {
		t.Fatal(err)
	}
	// The purge retired the handle; the sysop releases it, so the new
	// account can take it and the ID is what tells them apart.
	if err := os.Remove(filepath.Join(dir, RetiredHandlesFile)); err != nil {
		t.Fatal(err)
	}
	fresh, err := again.AddUser("password2", "Gone", "Someone Else", "")
	if err != nil {
		t.Fatal(err)
	}
	if fresh.ID == gone.ID {
		t.Fatalf("new account got the purged account's ID %d", gone.ID)
	}
	data, _ := os.ReadFile(filepath.Join(dir, NextUserIDFile))
	if strings.TrimSpace(string(data)) != "4" {
		t.Errorf("next_user_id = %q, want 4", data)
	}
}

// The mark only goes up, and a tool without a UserMgr can raise it.
func TestRecordUserIDsOnlyRaises(t *testing.T) {
	path := filepath.Join(t.TempDir(), "users.json")
	if err := RecordUserIDs(path, []*User{{ID: 7}, {ID: 3}}); err != nil {
		t.Fatal(err)
	}
	if err := RecordUserIDs(path, []*User{{ID: 2}}); err != nil {
		t.Fatal(err)
	}
	if got, _ := readNextUserID(path); got != 8 {
		t.Errorf("mark = %d, want 8", got)
	}
}

// Raises racing from many writers leave the highest mark.
func TestRaiseNextUserIDConcurrent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "users.json")
	var wg sync.WaitGroup
	for n := 1; n <= 40; n++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			if err := raiseNextUserID(path, n); err != nil {
				t.Error(err)
			}
		}(n)
	}
	wg.Wait()
	if got, _ := readNextUserID(path); got != 40 {
		t.Errorf("mark = %d, want 40", got)
	}
}

// A mark that can't be trusted stops new accounts and purges rather than
// guessing from users.json, which could hand out a retired ID.
func TestBadMarkRefusesNewAccountsAndPurges(t *testing.T) {
	for name, bad := range map[string]func(path string) error{
		"not a number": func(p string) error { return os.WriteFile(p, []byte("lots\n"), 0o600) },
		"unreadable":   func(p string) error { return os.Mkdir(p, 0o700) },
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			um, err := NewUserManager(dir)
			if err != nil {
				t.Fatal(err)
			}
			gone, _ := um.AddUser("password1", "Gone", "Gone Away", "")
			stored, _ := um.GetUserByID(gone.ID)
			old := time.Now().AddDate(0, 0, -30)
			stored.DeletedUser, stored.DeletedAt = true, &old
			if err := um.UpdateUserByID(stored); err != nil {
				t.Fatal(err)
			}
			mark := filepath.Join(dir, NextUserIDFile)
			if err := os.Remove(mark); err != nil {
				t.Fatal(err)
			}
			if err := bad(mark); err != nil {
				t.Fatal(err)
			}
			if _, err := um.PurgeDeletedUsers(1); err == nil {
				t.Error("purged with a bad mark")
			}
			if _, ok := um.GetUserByID(gone.ID); !ok {
				t.Error("the account went though the purge failed")
			}

			again, err := NewUserManager(dir)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := again.AddUser("password2", "Newcomer", "New Person", ""); !errors.Is(err, ErrUserIDMark) {
				t.Errorf("AddUser with a bad mark: %v", err)
			}
		})
	}
}

// The mark is past the new account's ID before the account is saved.
func TestAddUserStoresTheMarkFirst(t *testing.T) {
	dir := t.TempDir()
	um, err := NewUserManager(dir)
	if err != nil {
		t.Fatal(err)
	}
	u, err := um.AddUser("password1", "Someone", "Some One", "")
	if err != nil {
		t.Fatal(err)
	}
	if got, err := readNextUserID(filepath.Join(dir, "users.json")); err != nil || got <= u.ID {
		t.Errorf("mark = %d (%v), want past %d", got, err, u.ID)
	}
}
