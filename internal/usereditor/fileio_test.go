package usereditor

import (
	"os"
	"path/filepath"
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
	mtime, err := SaveUsers(path, []*user.User{{ID: 1, Handle: "A"}})
	if err != nil {
		t.Fatalf("SaveUsers: %v", err)
	}

	if CheckFileChanged(path, mtime) {
		t.Error("file should be unchanged immediately after save")
	}
	// A clearly different stored mtime must read as changed.
	if !CheckFileChanged(path, mtime.Add(-time.Hour)) {
		t.Error("expected changed=true for a stale stored mtime")
	}
	// Missing file is treated as unchanged (can't stat).
	if CheckFileChanged(filepath.Join(dir, "gone.json"), mtime) {
		t.Error("missing file should report unchanged")
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
		if filepath.Ext(e.Name()) == ".tmp" || filepath.Base(e.Name()) != "users.json" {
			t.Errorf("unexpected leftover file after atomic save: %s", e.Name())
		}
	}
}
