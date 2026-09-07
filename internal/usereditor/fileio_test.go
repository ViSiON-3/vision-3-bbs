package usereditor

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

func TestSaveLoadUsers_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "users.json")

	in := []*user.User{
		{ID: 3, Handle: "Charlie", AccessLevel: 10},
		{ID: 1, Handle: "Alice", AccessLevel: 255},
		{ID: 2, Handle: "Bob", AccessLevel: 20},
	}

	if _, err := SaveUsers(path, in); err != nil {
		t.Fatalf("SaveUsers: %v", err)
	}

	out, _, err := LoadUsers(path)
	if err != nil {
		t.Fatalf("LoadUsers: %v", err)
	}
	if len(out) != 3 {
		t.Fatalf("loaded %d users, want 3", len(out))
	}
	// Both save and load sort by ID.
	for i, wantID := range []int{1, 2, 3} {
		if out[i].ID != wantID {
			t.Errorf("user[%d].ID = %d, want %d (should be ID-sorted)", i, out[i].ID, wantID)
		}
	}
	if out[0].Handle != "Alice" {
		t.Errorf("user[0].Handle = %q, want Alice", out[0].Handle)
	}
}

func TestLoadUsers_MissingFile(t *testing.T) {
	_, _, err := LoadUsers(filepath.Join(t.TempDir(), "nope.json"))
	if err == nil {
		t.Fatal("expected error loading a missing file")
	}
}

func TestCheckFileChanged(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "users.json")
	fp, err := SaveUsers(path, []*user.User{{ID: 1, Handle: "A"}})
	if err != nil {
		t.Fatalf("SaveUsers: %v", err)
	}

	if CheckFileChanged(path, fp) {
		t.Error("file should be unchanged immediately after save")
	}
	// A fingerprint from different content must read as changed.
	if !CheckFileChanged(path, "0-deadbeef") {
		t.Error("expected changed=true for a stale stored fingerprint")
	}
	// Missing file is treated as unchanged: there is nothing to overwrite.
	if CheckFileChanged(filepath.Join(dir, "gone.json"), fp) {
		t.Error("missing file should report unchanged")
	}
}

// The editor compared modification times before, which cannot distinguish two
// writes inside one filesystem tick. Content comparison can, and a BBS write
// landing in the same tick as our load is exactly the case that used to be
// missed and silently overwritten.
func TestCheckFileChangedDetectsASameTickWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "users.json")

	fp, err := SaveUsers(path, []*user.User{{ID: 1, Handle: "A", AccessLevel: 10}})
	if err != nil {
		t.Fatalf("SaveUsers: %v", err)
	}
	// Stand in for the BBS writing immediately afterwards, forcing the same
	// mtime so only the content differs.
	if _, err := SaveUsers(path, []*user.User{{ID: 1, Handle: "A", AccessLevel: 255}}); err != nil {
		t.Fatalf("second SaveUsers: %v", err)
	}
	stamp := time.Now()
	if err := os.Chtimes(path, stamp, stamp); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}

	if !CheckFileChanged(path, fp) {
		t.Error("a same-tick external write went undetected; the next save would overwrite it")
	}
}

// SaveUsersChecked is the guarded write: it refuses when the file moved, and
// force is what the "overwrite?" prompt turns into.
func TestSaveUsersCheckedRefusesAndForces(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "users.json")

	loadedFP, err := SaveUsers(path, []*user.User{{ID: 1, Handle: "A", AccessLevel: 10}})
	if err != nil {
		t.Fatalf("SaveUsers: %v", err)
	}
	// Someone else writes after we loaded.
	if _, err := SaveUsers(path, []*user.User{{ID: 1, Handle: "A", AccessLevel: 99}}); err != nil {
		t.Fatalf("external SaveUsers: %v", err)
	}

	mine := []*user.User{{ID: 1, Handle: "A", AccessLevel: 20}}
	if _, err := SaveUsersChecked(path, mine, loadedFP, false); !errors.Is(err, ErrFileChanged) {
		t.Fatalf("guarded save error = %v, want ErrFileChanged", err)
	}
	// The refusal must not have written anything.
	after, _, err := LoadUsers(path)
	if err != nil {
		t.Fatalf("LoadUsers: %v", err)
	}
	if after[0].AccessLevel != 99 {
		t.Errorf("a refused save still wrote: level = %d, want 99", after[0].AccessLevel)
	}

	// Forcing goes through and reports a fingerprint matching what landed.
	newFP, err := SaveUsersChecked(path, mine, loadedFP, true)
	if err != nil {
		t.Fatalf("forced save: %v", err)
	}
	if CheckFileChanged(path, newFP) {
		t.Error("fingerprint returned by a forced save does not match the file it wrote")
	}
	forced, _, err := LoadUsers(path)
	if err != nil {
		t.Fatalf("LoadUsers after force: %v", err)
	}
	if forced[0].AccessLevel != 20 {
		t.Errorf("forced save did not take: level = %d, want 20", forced[0].AccessLevel)
	}
}

func TestCloneUser_IsDeepCopy(t *testing.T) {
	now := time.Now()
	orig := &user.User{
		ID:                    7,
		Handle:                "Orig",
		LastReadMessageIDs:    map[int]string{1: "uuid-a"},
		TaggedFileIDs:         []uuid.UUID{uuid.New()},
		TaggedMessageAreaTags: []string{"GENERAL"},
		DeletedAt:             &now,
	}

	clone := CloneUser(orig)
	if clone == orig {
		t.Fatal("clone must be a distinct pointer")
	}

	// Mutating the clone's reference-type fields must not touch the original.
	clone.LastReadMessageIDs[1] = "mutated"
	clone.LastReadMessageIDs[2] = "added"
	clone.TaggedFileIDs[0] = uuid.New()
	clone.TaggedMessageAreaTags[0] = "CHANGED"
	*clone.DeletedAt = now.Add(time.Hour)

	if orig.LastReadMessageIDs[1] != "uuid-a" {
		t.Errorf("original map mutated: %q", orig.LastReadMessageIDs[1])
	}
	if _, ok := orig.LastReadMessageIDs[2]; ok {
		t.Error("original map gained an entry from clone mutation")
	}
	if orig.TaggedMessageAreaTags[0] != "GENERAL" {
		t.Errorf("original slice mutated: %q", orig.TaggedMessageAreaTags[0])
	}
	if !orig.DeletedAt.Equal(now) {
		t.Error("original DeletedAt mutated through the clone pointer")
	}
}

func TestCloneUser_Nil(t *testing.T) {
	if CloneUser(nil) != nil {
		t.Error("CloneUser(nil) should return nil")
	}
}

func TestSaveUsers_AtomicNoTempLeftBehind(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "users.json")
	if _, err := SaveUsers(path, []*user.User{{ID: 1, Handle: "A"}}); err != nil {
		t.Fatalf("SaveUsers: %v", err)
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		// The lock sidecar is meant to stay. Deleting it after unlocking races
		// with another process taking the same path, so it is left in place;
		// see filelock.Release.
		if e.Name() == "users.json.lock" {
			continue
		}
		if filepath.Ext(e.Name()) == ".tmp" || filepath.Base(e.Name()) != "users.json" {
			t.Errorf("unexpected leftover file after atomic save: %s", e.Name())
		}
	}
}

// A users.json that exists but cannot be read is not the same as one that is
// gone. Its contents are unknown and a save would replace them, so it has to
// raise the prompt rather than be assumed unchanged and quietly overwritten.
func TestCheckFileChangedTreatsUnreadableAsChanged(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root; permission bits do not prevent reads")
	}
	if runtime.GOOS == "windows" {
		t.Skip("os.Chmod cannot make a file unreadable on Windows")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "users.json")
	fp, err := SaveUsers(path, []*user.User{{ID: 1, Handle: "A"}})
	if err != nil {
		t.Fatalf("SaveUsers: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o600) })
	if err := os.Chmod(path, 0o000); err != nil {
		t.Fatalf("chmod: %v", err)
	}

	if !CheckFileChanged(path, fp) {
		t.Error("an unreadable users.json reported unchanged; the next save would overwrite it blind")
	}
}

// And a guarded save over one refuses rather than writing.
func TestSaveUsersCheckedRefusesOverAnUnreadableFile(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root; permission bits do not prevent reads")
	}
	if runtime.GOOS == "windows" {
		t.Skip("os.Chmod cannot make a file unreadable on Windows")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "users.json")
	fp, err := SaveUsers(path, []*user.User{{ID: 1, Handle: "A"}})
	if err != nil {
		t.Fatalf("SaveUsers: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o600) })
	if err := os.Chmod(path, 0o000); err != nil {
		t.Fatalf("chmod: %v", err)
	}

	if _, err := SaveUsersChecked(path, []*user.User{{ID: 1, Handle: "B"}}, fp, false); !errors.Is(err, ErrFileChanged) {
		t.Errorf("guarded save error = %v, want ErrFileChanged", err)
	}
}
