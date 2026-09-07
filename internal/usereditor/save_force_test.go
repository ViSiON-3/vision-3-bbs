package usereditor

import (
	"path/filepath"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// editorOver builds a Model over a users.json seeded with the given records.
func editorOver(t *testing.T, users ...*user.User) (Model, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "users.json")
	if _, err := SaveUsers(path, users); err != nil {
		t.Fatalf("seed SaveUsers: %v", err)
	}
	m, err := New(path)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return m, path
}

// An unguarded save writes, and leaves the model in step with the file so an
// immediate second save is not mistaken for a conflict.
func TestSaveAllToDiskWritesAndResyncs(t *testing.T) {
	m, path := editorOver(t, &user.User{ID: 1, Handle: "Alice", AccessLevel: 10})

	m.users[0].AccessLevel = 42
	m.dirty = true
	m.saveAllToDisk(false)

	if m.dirty {
		t.Errorf("still dirty after a successful save; message = %q", m.message)
	}
	if m.mode == modeFileChanged {
		t.Fatalf("a save with no external write raised a conflict: %q", m.message)
	}
	got, _, err := LoadUsers(path)
	if err != nil {
		t.Fatalf("LoadUsers: %v", err)
	}
	if got[0].AccessLevel != 42 {
		t.Errorf("level on disk = %d, want 42", got[0].AccessLevel)
	}
}

// When the BBS writes underneath the editor, an ordinary save must stop and
// ask rather than overwrite.
func TestSaveAllToDiskRaisesTheConflictPrompt(t *testing.T) {
	m, path := editorOver(t, &user.User{ID: 1, Handle: "Alice", AccessLevel: 10})

	// The BBS saves while the sysop is editing.
	if _, err := SaveUsers(path, []*user.User{{ID: 1, Handle: "Alice", AccessLevel: 99}}); err != nil {
		t.Fatalf("external SaveUsers: %v", err)
	}

	m.users[0].AccessLevel = 42
	m.dirty = true
	m.saveAllToDisk(false)

	if m.mode != modeFileChanged {
		t.Errorf("mode = %v, want modeFileChanged so the sysop is asked", m.mode)
	}
	got, _, err := LoadUsers(path)
	if err != nil {
		t.Fatalf("LoadUsers: %v", err)
	}
	if got[0].AccessLevel != 99 {
		t.Errorf("the refused save still wrote: level = %d, want 99", got[0].AccessLevel)
	}
}

// The regression this pins: confirming "overwrite?" used to call the same
// guarded save, which re-ran the check that raised the prompt, failed it again
// and returned without writing. The sysop was sent back to the list with their
// edits silently dropped, and no second warning.
func TestForcedSaveAfterAConflictActuallyWrites(t *testing.T) {
	m, path := editorOver(t, &user.User{ID: 1, Handle: "Alice", AccessLevel: 10})

	if _, err := SaveUsers(path, []*user.User{{ID: 1, Handle: "Alice", AccessLevel: 99}}); err != nil {
		t.Fatalf("external SaveUsers: %v", err)
	}

	m.users[0].AccessLevel = 42
	m.dirty = true
	m.saveAllToDisk(false) // raises the prompt
	if m.mode != modeFileChanged {
		t.Fatalf("expected the conflict prompt first, got mode %v", m.mode)
	}

	m.saveAllToDisk(true) // the sysop confirms "overwrite"
	m.mode = modeList     // executeConfirm does this; saveAllToDisk does not

	if m.dirty {
		t.Errorf("still dirty after confirming the overwrite; message = %q", m.message)
	}
	got, _, err := LoadUsers(path)
	if err != nil {
		t.Fatalf("LoadUsers: %v", err)
	}
	if got[0].AccessLevel != 42 {
		t.Errorf("confirmed overwrite did not reach disk: level = %d, want 42", got[0].AccessLevel)
	}

	// And the model must be back in step with the file it just wrote, or the
	// very next save would re-raise a conflict against its own write.
	m.users[0].AccessLevel = 7
	m.dirty = true
	m.saveAllToDisk(false)
	if m.mode == modeFileChanged {
		t.Fatal("a save straight after a forced one re-raised the conflict; the fingerprint was not resynced")
	}
	after, _, err := LoadUsers(path)
	if err != nil {
		t.Fatalf("LoadUsers: %v", err)
	}
	if after[0].AccessLevel != 7 {
		t.Errorf("the save after the forced one did not land: level = %d, want 7", after[0].AccessLevel)
	}
}

// Driving the confirm handler itself, which is where the defect lived: it
// called the guarded save, so answering "yes" to "overwrite?" re-ran the check
// that raised the prompt, failed it again, and returned to the list having
// written nothing and warned no second time.
func TestConfirmingTheOverwritePromptWrites(t *testing.T) {
	m, path := editorOver(t, &user.User{ID: 1, Handle: "Alice", AccessLevel: 10})

	// The BBS writes while the sysop is editing.
	if _, err := SaveUsers(path, []*user.User{{ID: 1, Handle: "Alice", AccessLevel: 99}}); err != nil {
		t.Fatalf("external SaveUsers: %v", err)
	}

	m.users[0].AccessLevel = 42
	m.dirty = true
	m.saveAllToDisk(false)
	if m.mode != modeFileChanged {
		t.Fatalf("expected the overwrite prompt, got mode %v", m.mode)
	}

	// The sysop selects "yes" on the prompt.
	updated, _ := m.executeConfirm()
	got, ok := updated.(Model)
	if !ok {
		t.Fatalf("executeConfirm returned %T, want Model", updated)
	}

	onDisk, _, err := LoadUsers(path)
	if err != nil {
		t.Fatalf("LoadUsers: %v", err)
	}
	if onDisk[0].AccessLevel != 42 {
		t.Errorf("confirming the overwrite wrote nothing: level on disk = %d, want 42", onDisk[0].AccessLevel)
	}
	if got.dirty {
		t.Error("still dirty after confirming the overwrite, so the edits were never saved")
	}
	if got.mode != modeList {
		t.Errorf("mode = %v after confirming, want modeList", got.mode)
	}
}
