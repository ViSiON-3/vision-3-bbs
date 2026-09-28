package menu

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"

	"github.com/ViSiON-3/vision-3-bbs/internal/file"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
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

// lightbarListEnv returns an env whose sysop and caller list General Files
// in the lightbar file lister.
func lightbarListEnv(t *testing.T) *menuEnv {
	t.Helper()
	env := newMenuEnv(t)
	for _, u := range []*user.User{env.caller, env.sysop} {
		u.FileListingMode = "lightbar"
		u.CurrentFileAreaID = 1
		u.CurrentFileAreaTag = "GENERAL"
	}
	return env
}

// TestFileLightbarSysopEditsDescription pins the sysop "e" command: the new
// description is saved to the file record; a blank entry keeps the old one.
func TestFileLightbarSysopEditsDescription(t *testing.T) {
	env := lightbarListEnv(t)
	id, _ := addReviewUpload(t, env, 1, "EDIT.ZIP", 4)

	env.runCmd("LISTFILES", env.sysop, "", "e\rq")
	if got := reloadedFileRecord(t, env, id).Description; got != "original desc" {
		t.Errorf("blank edit changed description to %q", got)
	}
	r := env.runCmd("LISTFILES", env.sysop, "", "eFresh words\rq")
	if !r.has("New description:") {
		t.Errorf("no description prompt:\n%s", r.text())
	}
	if got := reloadedFileRecord(t, env, id).Description; got != "Fresh words" {
		t.Errorf("description = %q, want Fresh words", got)
	}
}

// TestFileLightbarSysopKeysIgnoredForCaller pins that e/k/m/r do nothing for
// a caller below sysop level: the record and its file are untouched.
func TestFileLightbarSysopKeysIgnoredForCaller(t *testing.T) {
	env := lightbarListEnv(t)
	id, p := addReviewUpload(t, env, 1, "KEEP.ZIP", 4)

	r := env.runCmd("LISTFILES", env.caller, "", "eX\rkYm2\rYrNEW.ZIP\rq")
	for _, p := range []string{"New description:", "Delete KEEP.ZIP", "Move to area", "New filename:"} {
		if r.has(p) {
			t.Errorf("sysop prompt %q shown to caller", p)
		}
	}
	if !r.has("KEEP.ZIP") {
		t.Errorf("caller did not see the listing:\n%s", r.text())
	}
	rec := reloadedFileRecord(t, env, id)
	if rec == nil || rec.Filename != "KEEP.ZIP" || rec.AreaID != 1 || rec.Description != "original desc" {
		t.Errorf("record changed: %+v", rec)
	}
	if _, err := os.Stat(p); err != nil {
		t.Errorf("file gone: %v", err)
	}
}

// TestFileLightbarSysopKillsFile pins "k": No keeps the file, Yes deletes the
// record and its bytes and drops the file from the sysop's marks.
func TestFileLightbarSysopKillsFile(t *testing.T) {
	env := lightbarListEnv(t)
	id, p := addReviewUpload(t, env, 1, "DOOMED.ZIP", 4)
	env.sysop.TaggedFileIDs = []uuid.UUID{id}

	env.runCmd("LISTFILES", env.sysop, "", "kNq")
	if reloadedFileRecord(t, env, id) == nil {
		t.Fatal("No at the prompt deleted the file")
	}
	r := env.runCmd("LISTFILES", env.sysop, "", "kYq")
	if !r.has("Delete DOOMED.ZIP from disk?") {
		t.Errorf("no delete prompt:\n%s", r.text())
	}
	if reloadedFileRecord(t, env, id) != nil {
		t.Error("record survived delete")
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Errorf("file still on disk: %v", err)
	}
	if len(env.sysop.TaggedFileIDs) != 0 {
		t.Errorf("tags = %v, want the deleted file dropped", env.sysop.TaggedFileIDs)
	}
	if r := env.runCmd("LISTFILES", env.sysop, "", "k"); r.next != "LOGOFF" {
		t.Errorf("disconnect at delete prompt: next = %q, want LOGOFF", r.next)
	}
}

// TestFileLightbarSysopMovesFile pins "m": an unknown area is refused, a
// blank answer or No leaves the file, and Yes moves it to the tagged area.
func TestFileLightbarSysopMovesFile(t *testing.T) {
	env := lightbarListEnv(t)
	id, _ := addReviewUpload(t, env, 1, "MOVER.ZIP", 4)

	r := env.runCmd("LISTFILES", env.sysop, "", "mNOWHERE\rm\rmuploads\rNq")
	if !r.has("Area not found.", "Move MOVER.ZIP to Upload Queue?") {
		t.Errorf("move prompts missing:\n%s", r.text())
	}
	if got := reloadedFileRecord(t, env, id).AreaID; got != 1 {
		t.Fatalf("area = %d after refusals, want 1", got)
	}
	env.runCmd("LISTFILES", env.sysop, "", "m2\rYq")
	if got := reloadedFileRecord(t, env, id).AreaID; got != 2 {
		t.Errorf("area = %d, want 2", got)
	}
	if r := env.runCmd("LISTFILES", env.sysop, "", "m2\r"); r.next != "LOGOFF" {
		t.Errorf("disconnect at move confirm: next = %q, want LOGOFF", r.next)
	}
}

// TestFileLightbarSysopRenamesFile pins "r": reserved names and a name taken
// by another record are refused, a file already on disk under the new name
// is not clobbered, and a clean name renames both the record and the file.
func TestFileLightbarSysopRenamesFile(t *testing.T) {
	env := lightbarListEnv(t)
	id, p := addReviewUpload(t, env, 1, "AAA.ZIP", 4)
	addReviewUpload(t, env, 1, "TAKEN.ZIP", 4)
	stray := filepath.Join(filepath.Dir(p), "STRAY.ZIP")
	if err := os.WriteFile(stray, []byte("untracked"), 0o644); err != nil {
		t.Fatal(err)
	}

	r := env.runCmd("LISTFILES", env.sysop, "", "r..\rrtaken.zip\rrSTRAY.ZIP\rr\rq")
	if !r.has("Invalid filename.", "Filename already exists in this area.", "A file with that name already exists.") {
		t.Errorf("rename refusals missing:\n%s", r.text())
	}
	if got := reloadedFileRecord(t, env, id).Filename; got != "AAA.ZIP" {
		t.Fatalf("filename = %q after refusals", got)
	}
	if b, _ := os.ReadFile(stray); string(b) != "untracked" {
		t.Error("untracked file clobbered")
	}

	env.runCmd("LISTFILES", env.sysop, "", "rBBB.ZIP\rq")
	if got := reloadedFileRecord(t, env, id).Filename; got != "BBB.ZIP" {
		t.Errorf("filename = %q, want BBB.ZIP", got)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(p), "BBB.ZIP")); err != nil {
		t.Errorf("renamed file not on disk: %v", err)
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Errorf("old name still on disk: %v", err)
	}
}
