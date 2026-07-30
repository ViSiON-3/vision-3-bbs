package menu

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"

	"github.com/ViSiON-3/vision-3-bbs/internal/file"
)

// newTestFileManagerTwoAreas builds a real *file.FileManager with two areas
// (ID 1 "UTILS" and ID 2 "GAMES") for resolveTargetArea to look up.
func newTestFileManagerTwoAreas(t *testing.T) *file.FileManager {
	t.Helper()
	dataDir, cfgDir := t.TempDir(), t.TempDir()
	areasJSON := `[
		{"id":1,"tag":"UTILS","name":"Utilities","path":"utils","acs_list":""},
		{"id":2,"tag":"GAMES","name":"Games","path":"games","acs_list":""}
	]`
	if err := os.WriteFile(filepath.Join(cfgDir, "file_areas.json"), []byte(areasJSON), 0644); err != nil {
		t.Fatalf("write areas: %v", err)
	}
	fm, err := file.NewFileManager(dataDir, cfgDir)
	if err != nil {
		t.Fatalf("NewFileManager: %v", err)
	}
	return fm
}

func TestResolveTargetArea_ByID(t *testing.T) {
	fm := newTestFileManagerTwoAreas(t)

	id, area := resolveTargetArea(fm, "2")

	if area == nil {
		t.Fatal("expected area to be found by ID")
	}
	if id != 2 || area.Tag != "GAMES" {
		t.Errorf("resolveTargetArea(\"2\") = (%d, %+v), want (2, GAMES)", id, area)
	}
}

func TestResolveTargetArea_ByTag(t *testing.T) {
	fm := newTestFileManagerTwoAreas(t)

	id, area := resolveTargetArea(fm, "utils")

	if area == nil {
		t.Fatal("expected area to be found by tag")
	}
	if id != 1 || area.Tag != "UTILS" {
		t.Errorf("resolveTargetArea(\"utils\") = (%d, %+v), want (1, UTILS)", id, area)
	}
}

func TestResolveTargetArea_NotFound(t *testing.T) {
	fm := newTestFileManagerTwoAreas(t)

	_, area := resolveTargetArea(fm, "NOPE")
	if area != nil {
		t.Errorf("resolveTargetArea(\"NOPE\") = %+v, want nil", area)
	}

	_, area = resolveTargetArea(fm, "99")
	if area != nil {
		t.Errorf("resolveTargetArea(\"99\") = %+v, want nil", area)
	}
}

func TestValidateNewName(t *testing.T) {
	id1, id2 := uuid.New(), uuid.New()
	existing := []file.FileRecord{
		{ID: id1, Filename: "existing.zip"},
	}

	tests := []struct {
		name       string
		input      string
		currentID  uuid.UUID
		wantClean  string
		wantErrMsg bool
	}{
		{"valid new name", "renamed.zip", id2, "renamed.zip", false},
		{"trims whitespace and path", "  ../../etc/renamed.zip  ", id2, "renamed.zip", false},
		{"rejects dot", ".", id2, ".", true},
		{"rejects dotdot", "..", id2, "..", true},
		{"rejects duplicate of another file", "existing.zip", id2, "existing.zip", true},
		{"allows renaming a file to its own current name", "existing.zip", id1, "existing.zip", false},
		{"case-insensitive duplicate check", "EXISTING.ZIP", id2, "EXISTING.ZIP", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cleaned, errMsg := validateNewName(tt.input, existing, tt.currentID)
			if cleaned != tt.wantClean {
				t.Errorf("cleaned = %q, want %q", cleaned, tt.wantClean)
			}
			if (errMsg != "") != tt.wantErrMsg {
				t.Errorf("errMsg = %q, want non-empty=%v", errMsg, tt.wantErrMsg)
			}
		})
	}
}

func TestSafeRenameOnDisk_Success(t *testing.T) {
	dir := t.TempDir()
	oldPath := filepath.Join(dir, "old.txt")
	newPath := filepath.Join(dir, "new.txt")
	if err := os.WriteFile(oldPath, []byte("hi"), 0644); err != nil {
		t.Fatalf("write oldPath: %v", err)
	}

	conflict, err := safeRenameOnDisk(oldPath, newPath)

	if conflict != renameOK || err != nil {
		t.Fatalf("safeRenameOnDisk = (%v, %v), want (renameOK, nil)", conflict, err)
	}
	if _, statErr := os.Stat(newPath); statErr != nil {
		t.Errorf("expected file at newPath after rename: %v", statErr)
	}
}

func TestSafeRenameOnDisk_TargetExists(t *testing.T) {
	dir := t.TempDir()
	oldPath := filepath.Join(dir, "old.txt")
	newPath := filepath.Join(dir, "new.txt")
	if err := os.WriteFile(oldPath, []byte("hi"), 0644); err != nil {
		t.Fatalf("write oldPath: %v", err)
	}
	if err := os.WriteFile(newPath, []byte("already here"), 0644); err != nil {
		t.Fatalf("write newPath: %v", err)
	}

	conflict, err := safeRenameOnDisk(oldPath, newPath)

	if conflict != renameTargetExists {
		t.Errorf("conflict = %v, want renameTargetExists", conflict)
	}
	if err != nil {
		t.Errorf("err = %v, want nil (renameTargetExists isn't itself an error)", err)
	}
	// Original file must be untouched.
	if _, statErr := os.Stat(oldPath); statErr != nil {
		t.Errorf("oldPath should still exist after a refused rename: %v", statErr)
	}
}

func TestSafeRenameOnDisk_SameFileCaseOnlyRenameSucceeds(t *testing.T) {
	dir := t.TempDir()
	oldPath := filepath.Join(dir, "same.txt")
	if err := os.WriteFile(oldPath, []byte("hi"), 0644); err != nil {
		t.Fatalf("write oldPath: %v", err)
	}
	// os.Stat(oldPath) via a different-cased path resolves to the same file
	// on a case-insensitive filesystem, exercising the os.SameFile branch.
	newPath := filepath.Join(dir, "SAME.txt")

	conflict, err := safeRenameOnDisk(oldPath, newPath)

	// On a case-sensitive filesystem SAME.txt won't exist yet, so this is a
	// plain successful rename; on a case-insensitive one it's the SameFile
	// path. Either way it must not be reported as a conflict.
	if conflict == renameTargetExists {
		t.Errorf("conflict = %v, want not renameTargetExists", conflict)
	}
	if conflict != renameOK {
		t.Errorf("conflict = %v, err = %v, want renameOK", conflict, err)
	}
}

func TestToggleTaggedID(t *testing.T) {
	a, b, c := uuid.New(), uuid.New(), uuid.New()

	// Adding to an empty list.
	got := toggleTaggedID(nil, a)
	if len(got) != 1 || got[0] != a {
		t.Errorf("toggle onto empty list = %v, want [%v]", got, a)
	}

	// Adding a second, distinct ID.
	got = toggleTaggedID([]uuid.UUID{a}, b)
	if len(got) != 2 || got[0] != a || got[1] != b {
		t.Errorf("toggle add = %v, want [%v %v]", got, a, b)
	}

	// Removing an already-tagged ID preserves the order of the rest.
	got = toggleTaggedID([]uuid.UUID{a, b, c}, b)
	if len(got) != 2 || got[0] != a || got[1] != c {
		t.Errorf("toggle remove = %v, want [%v %v]", got, a, c)
	}
}
