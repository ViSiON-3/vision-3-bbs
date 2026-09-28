package menu

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/ViSiON-3/vision-3-bbs/internal/file"
)

// addReviewUpload adds an unreviewed upload named name to areaID of env's
// real file manager and writes its bytes to disk, so rename/move/delete have
// a file to act on. It returns the record's ID and on-disk path.
func addReviewUpload(t *testing.T, env *menuEnv, areaID int, name string, size int) (uuid.UUID, string) {
	t.Helper()
	id := uuid.New()
	if err := env.e.FileMgr.AddFileRecord(file.FileRecord{
		ID: id, AreaID: areaID, Filename: name, Description: "original desc",
		Size: int64(size), UploadedAt: time.Date(2026, 3, 4, 5, 6, 0, 0, time.UTC), UploadedBy: "Caller",
	}); err != nil {
		t.Fatalf("AddFileRecord: %v", err)
	}
	p, err := env.e.FileMgr.GetFilePath(id)
	if err != nil {
		t.Fatalf("GetFilePath: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, make([]byte, size), 0o644); err != nil {
		t.Fatal(err)
	}
	return id, p
}

// reloadedFileRecord re-reads file metadata from disk through a fresh
// FileManager and returns the record, or nil if it no longer exists.
func reloadedFileRecord(t *testing.T, env *menuEnv, id uuid.UUID) *file.FileRecord {
	t.Helper()
	fm, err := file.NewFileManager(env.dataDir(), env.cfgDir())
	if err != nil {
		t.Fatalf("reload file manager: %v", err)
	}
	rec, err := fm.GetFileRecordByID(id)
	if err != nil {
		return nil
	}
	return rec
}

// TestEditFileRecordRequiresCoSysop pins that EDITFILERECORD refuses a caller
// below the CoSysOp level without prompting.
func TestEditFileRecordRequiresCoSysop(t *testing.T) {
	env := newMenuEnv(t)
	addReviewUpload(t, env, 1, "SECRET.ZIP", 10)

	r := env.runCmd("EDITFILERECORD", env.caller, "", "Y\r")
	if !r.has("CoSysOp access required.") {
		t.Errorf("want the access refusal:\n%s", r.text())
	}
	if r.has("SECRET.ZIP") {
		t.Errorf("refused caller was shown the review queue:\n%s", r.text())
	}
}

// TestEditFileRecordChangeDescription pins the C action: the new description
// and the "reviewed" mark are both saved to the area's metadata.
func TestEditFileRecordChangeDescription(t *testing.T) {
	env := newMenuEnv(t)
	id, _ := addReviewUpload(t, env, 1, "GAME.ZIP", 2048)

	r := env.runCmd("EDITFILERECORD", env.sysop, "", "Y\rC\rA shiny new description\rY\r")
	if r.err != nil {
		t.Fatalf("err = %v", r.err)
	}
	if !r.has("GAME.ZIP", "2k", "Caller", "03/04/2026 05:06", "General Files", "original desc") {
		t.Errorf("review screen missing the record's details:\n%s", r.text())
	}
	rec := reloadedFileRecord(t, env, id)
	if rec == nil {
		t.Fatal("record vanished")
	}
	if rec.Description != "A shiny new description" || !rec.Reviewed {
		t.Errorf("saved record = desc %q reviewed %v, want new desc and reviewed", rec.Description, rec.Reviewed)
	}
}

// TestEditFileRecordBlankDescriptionKeepsOld pins that an empty C answer
// leaves the description alone, and declining the mark leaves it unreviewed.
func TestEditFileRecordBlankDescriptionKeepsOld(t *testing.T) {
	env := newMenuEnv(t)
	id, _ := addReviewUpload(t, env, 1, "KEEP.ZIP", 10)

	env.runCmd("EDITFILERECORD", env.sysop, "", "Y\rC\r\rN\r")
	rec := reloadedFileRecord(t, env, id)
	if rec == nil || rec.Description != "original desc" || rec.Reviewed {
		t.Errorf("record = %+v, want original desc and unreviewed", rec)
	}
}

// TestEditFileRecordRename pins the R action on the current area only (scan
// all = N): the file is renamed on disk and in the record.
func TestEditFileRecordRename(t *testing.T) {
	env := newMenuEnv(t)
	env.sysop.CurrentFileAreaID = 1
	id, oldPath := addReviewUpload(t, env, 1, "OLDNAME.ZIP", 10)

	env.runCmd("EDITFILERECORD", env.sysop, "", "N\rR\rNEWNAME.ZIP\rY\r")
	rec := reloadedFileRecord(t, env, id)
	if rec == nil || rec.Filename != "NEWNAME.ZIP" || !rec.Reviewed {
		t.Fatalf("record = %+v, want NEWNAME.ZIP and reviewed", rec)
	}
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Errorf("old file still on disk (err=%v)", err)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(oldPath), "NEWNAME.ZIP")); err != nil {
		t.Errorf("renamed file missing: %v", err)
	}
}

// TestEditFileRecordRenameRejectsPaths pins that a rename naming another
// directory is refused and changes nothing on disk or in the record.
func TestEditFileRecordRenameRejectsPaths(t *testing.T) {
	env := newMenuEnv(t)
	id, oldPath := addReviewUpload(t, env, 1, "STAY.ZIP", 10)

	r := env.runCmd("EDITFILERECORD", env.sysop, "", "Y\rR\r../escape.zip\rN\r")
	if !r.has("Invalid filename.") {
		t.Errorf("want the invalid-filename notice:\n%s", r.text())
	}
	if rec := reloadedFileRecord(t, env, id); rec == nil || rec.Filename != "STAY.ZIP" {
		t.Errorf("record = %+v, want STAY.ZIP unchanged", rec)
	}
	if _, err := os.Stat(oldPath); err != nil {
		t.Errorf("original file moved: %v", err)
	}
}

// TestEditFileRecordDelete pins the D action: declining keeps the file, and
// confirming removes both the record and the file on disk.
func TestEditFileRecordDelete(t *testing.T) {
	env := newMenuEnv(t)
	id, p := addReviewUpload(t, env, 1, "DOOMED.ZIP", 10)

	env.runCmd("EDITFILERECORD", env.sysop, "", "Y\rD\rN\r")
	if reloadedFileRecord(t, env, id) == nil {
		t.Fatal("declined delete removed the record")
	}

	env.runCmd("EDITFILERECORD", env.sysop, "", "Y\rD\rY\r")
	if rec := reloadedFileRecord(t, env, id); rec != nil {
		t.Errorf("record still present after confirmed delete: %+v", rec)
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Errorf("file still on disk after delete (err=%v)", err)
	}
}

// TestEditFileRecordMove pins the M action: the target list omits the
// current area, and confirming moves record and file to the Upload Queue.
func TestEditFileRecordMove(t *testing.T) {
	env := newMenuEnv(t)
	id, oldPath := addReviewUpload(t, env, 1, "MOVER.ZIP", 10)

	r := env.runCmd("EDITFILERECORD", env.sysop, "", "Y\rM\r2\rY\rY\r")
	if !r.has("Available areas:", "2 - Upload Queue", "Move to Upload Queue?") {
		t.Errorf("move prompts missing:\n%s", r.text())
	}
	if r.has("1 - General Files") {
		t.Errorf("move list offers the file's own area:\n%s", r.text())
	}
	rec := reloadedFileRecord(t, env, id)
	if rec == nil || rec.AreaID != 2 || !rec.Reviewed {
		t.Fatalf("record = %+v, want area 2 and reviewed", rec)
	}
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Errorf("file still in the source area (err=%v)", err)
	}
}

// TestEditFileRecordMoveBadTargets pins that a non-numeric or unknown target
// area, or a declined confirmation, leaves the file where it is.
func TestEditFileRecordMoveBadTargets(t *testing.T) {
	env := newMenuEnv(t)
	id, _ := addReviewUpload(t, env, 1, "STUCK.ZIP", 10)

	for _, tc := range []struct{ input, notice string }{
		{"Y\rM\rabc\rN\r", "Invalid area number."},
		{"Y\rM\r99\rN\r", "Area not found."},
		{"Y\rM\r2\rN\rN\r", "Move to Upload Queue?"},
		{"Y\rM\r\rN\r", "Move to area #:"},
	} {
		r := env.runCmd("EDITFILERECORD", env.sysop, "", tc.input)
		if !r.has(tc.notice) {
			t.Errorf("input %q: want %q:\n%s", tc.input, tc.notice, r.text())
		}
		if rec := reloadedFileRecord(t, env, id); rec == nil || rec.AreaID != 1 || rec.Reviewed {
			t.Errorf("input %q: record = %+v, want still in area 1 and unreviewed", tc.input, rec)
		}
	}
}

// TestEditFileRecordNoReviewPromptAfterNoChange pins that R and M only offer
// "Mark as reviewed?" after the rename or move actually happened: a
// cancelled, invalid or failed action goes straight on without the prompt.
func TestEditFileRecordNoReviewPromptAfterNoChange(t *testing.T) {
	for _, tc := range []struct {
		name, input string
		removeFile  bool // delete the upload from disk so the rename fails
	}{
		{"rename cancelled", "Y\rR\r\r", false},
		{"rename invalid", "Y\rR\r../escape.zip\r", false},
		{"rename failed", "Y\rR\rGONE.ZIP\r", true},
		{"rename to the same name", "Y\rR\rSAME.ZIP\r", false},
		{"move cancelled", "Y\rM\r\r", false},
		{"move invalid", "Y\rM\rabc\r", false},
		{"move unknown area", "Y\rM\r99\r", false},
		{"move declined", "Y\rM\r2\rN\r", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := newMenuEnv(t)
			id, p := addReviewUpload(t, env, 1, "SAME.ZIP", 10)
			if tc.removeFile {
				if err := os.Remove(p); err != nil {
					t.Fatal(err)
				}
			}
			r := env.runCmd("EDITFILERECORD", env.sysop, "", tc.input+"Y\r")
			if r.has("Mark as reviewed?") {
				t.Errorf("review offered after an action that changed nothing:\n%s", r.text())
			}
			if rec := reloadedFileRecord(t, env, id); rec == nil || rec.Filename != "SAME.ZIP" || rec.AreaID != 1 || rec.Reviewed {
				t.Errorf("record = %+v, want unchanged and unreviewed", rec)
			}
		})
	}
}

// TestEditFileRecordSkipAndQuit pins S (skip to the next file, unchanged) and
// Q (stop reviewing): with two uploads, S then Q shows both and marks none.
func TestEditFileRecordSkipAndQuit(t *testing.T) {
	env := newMenuEnv(t)
	first, _ := addReviewUpload(t, env, 1, "FIRST.ZIP", 10)
	second, _ := addReviewUpload(t, env, 2, "SECOND.ZIP", 10)

	r := env.runCmd("EDITFILERECORD", env.sysop, "", "Y\rS\rQ\r")
	if r.err != nil || r.user != env.sysop {
		t.Fatalf("result = (%v, %v)", r.user, r.err)
	}
	if !r.has("FIRST.ZIP", "SECOND.ZIP", "Upload Queue") {
		t.Errorf("scan-all should visit both areas' uploads:\n%s", r.text())
	}
	for _, id := range []uuid.UUID{first, second} {
		if rec := reloadedFileRecord(t, env, id); rec == nil || rec.Reviewed {
			t.Errorf("record %v = %+v, want present and unreviewed", id, rec)
		}
	}
}

// TestFormatReviewSize pins the review screen's byte/k/M size units.
func TestFormatReviewSize(t *testing.T) {
	for _, tc := range []struct {
		in   int64
		want string
	}{
		{0, "0B"}, {1023, "1023B"}, {1024, "1k"}, {1024*1024 - 1, "1023k"}, {3 * 1024 * 1024, "3M"},
	} {
		if got := formatReviewSize(tc.in); got != tc.want {
			t.Errorf("formatReviewSize(%d) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
