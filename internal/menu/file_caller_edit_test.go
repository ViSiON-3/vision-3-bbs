package menu

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/ViSiON-3/vision-3-bbs/internal/file"
)

// addUploads adds files to area 1, each uploaded by the handle given with it,
// and returns their IDs by filename.
func addUploads(t *testing.T, env *menuEnv, byName map[string]string) map[string]uuid.UUID {
	t.Helper()
	ids := map[string]uuid.UUID{}
	for name, by := range byName {
		ids[name] = uuid.New()
		if err := env.e.FileMgr.AddFileRecord(file.FileRecord{ID: ids[name], AreaID: 1, Filename: name, Size: 2048,
			UploadedBy: by, Description: "old words"}); err != nil {
			t.Fatalf("AddFileRecord: %v", err)
		}
	}
	return ids
}

// fileDesc is the description of file id as it is now.
func fileDesc(t *testing.T, env *menuEnv, id uuid.UUID) string {
	t.Helper()
	rec, err := env.e.FileMgr.GetFileRecordByID(id)
	if err != nil {
		t.Fatalf("GetFileRecordByID: %v", err)
	}
	return rec.Description
}

// A caller changes the description of a file they uploaded, named in any
// case; the uploader's handle matches in any case too. It is cut to 60
// characters, as at upload.
func TestEditUploadOwnFile(t *testing.T) {
	env := newMenuEnv(t)
	ids := addUploads(t, env, map[string]string{"MINE.ZIP": "caller"})
	env.caller.CurrentFileAreaID = 1

	r := env.runCmd("EDITUPLOAD", env.caller, "", "mine.zip\rfresh words\r")
	if !r.has("old words", "Description saved.") {
		t.Errorf("output:\n%s", r.text())
	}
	if got := fileDesc(t, env, ids["MINE.ZIP"]); got != "fresh words" {
		t.Errorf("description = %q, want fresh words", got)
	}

	env.runCmd("EDITUPLOAD", env.caller, "", "MINE.ZIP\r"+strings.Repeat("x", 70)+"\r")
	if got := fileDesc(t, env, ids["MINE.ZIP"]); got != strings.Repeat("x", 60) {
		t.Errorf("description = %q, want 60 x's", got)
	}
}

// Another caller's upload is refused and left alone; a co-sysop may edit
// any file's description.
func TestEditUploadOthersFile(t *testing.T) {
	env := newMenuEnv(t)
	ids := addUploads(t, env, map[string]string{"THEIRS.ZIP": "Uploader", "NOBODY.ZIP": ""})
	env.caller.CurrentFileAreaID = 1
	env.sysop.CurrentFileAreaID = 1

	for _, name := range []string{"THEIRS.ZIP", "NOBODY.ZIP"} {
		r := env.runCmd("EDITUPLOAD", env.caller, "", name+"\rmine now\r")
		if !r.has("That file wasn't uploaded by you.") || r.has("New description") {
			t.Errorf("%s: output:\n%s", name, r.text())
		}
		if got := fileDesc(t, env, ids[name]); got != "old words" {
			t.Errorf("%s: description = %q, want it unchanged", name, got)
		}
	}

	env.runCmd("EDITUPLOAD", env.sysop, "", "THEIRS.ZIP\rsysop words\r")
	if got := fileDesc(t, env, ids["THEIRS.ZIP"]); got != "sysop words" {
		t.Errorf("co-sysop's edit: description = %q", got)
	}
}

// An empty line keeps the old description.
func TestEditUploadEmptyKeeps(t *testing.T) {
	env := newMenuEnv(t)
	ids := addUploads(t, env, map[string]string{"MINE.ZIP": "Caller"})
	env.caller.CurrentFileAreaID = 1

	r := env.runCmd("EDITUPLOAD", env.caller, "", "MINE.ZIP\r  \r")
	if !r.has("Description unchanged.") {
		t.Errorf("output:\n%s", r.text())
	}
	if got := fileDesc(t, env, ids["MINE.ZIP"]); got != "old words" {
		t.Errorf("description = %q, want it unchanged", got)
	}
}

// If the sysop gives the file to someone else while the caller types, the
// uploader is checked again as it's saved, and nothing is saved.
func TestEditUploadReassignedWhileTyping(t *testing.T) {
	env := newMenuEnv(t)
	ids := addUploads(t, env, map[string]string{"MINE.ZIP": "Caller"})
	env.caller.CurrentFileAreaID = 1
	env.whenOutput("New description:", func() {
		if err := env.e.FileMgr.UpdateFileRecord(ids["MINE.ZIP"], func(r *file.FileRecord) { r.UploadedBy = "Someone" }); err != nil {
			t.Error(err)
		}
	})

	r := env.runCmd("EDITUPLOAD", env.caller, "", "MINE.ZIP\rnew words\r")
	if !r.has("That file was changed elsewhere. Nothing was saved.") {
		t.Errorf("output:\n%s", r.text())
	}
	if got := fileDesc(t, env, ids["MINE.ZIP"]); got != "old words" {
		t.Errorf("description = %q, want it unchanged", got)
	}
}

// EDITUPLOAD's other exits: no caller, no area, a blank filename, an unknown
// file and a disconnect.
func TestEditUploadEdgeCases(t *testing.T) {
	env := newMenuEnv(t)
	if r := env.runCmd("EDITUPLOAD", nil, "", "X\r"); r.raw != "" {
		t.Errorf("no user printed:\n%s", r.text())
	}
	if r := env.runCmd("EDITUPLOAD", env.caller, "", "X\r"); !strings.Contains(r.text(), stripPipes(env.e.Strings().FileNoAreaSelected)) {
		t.Errorf("no area:\n%s", r.text())
	}
	env.caller.CurrentFileAreaID = 1
	if r := env.runCmd("EDITUPLOAD", env.caller, "", "\r"); r.has("New description") {
		t.Errorf("blank filename went on:\n%s", r.text())
	}
	if r := env.runCmd("EDITUPLOAD", env.caller, "", "MISSING.ZIP\r"); !r.has("MISSING.ZIP") || r.has("New description") {
		t.Errorf("unknown file:\n%s", r.text())
	}
	if r := env.runCmd("EDITUPLOAD", env.caller, "", ""); r.next != "LOGOFF" {
		t.Errorf("disconnect: next = %q", r.next)
	}
}

// tagFiles sets u's batch to ids and saves it.
func tagFiles(t *testing.T, env *menuEnv, ids ...uuid.UUID) {
	t.Helper()
	env.caller.TaggedFileIDs = ids
	if err := env.um.UpdateUser(env.caller); err != nil {
		t.Fatal(err)
	}
}

// REMOVEFROMBATCH lists the batch, numbered, and takes off the file given by
// number, or by filename in any case; the rest stay, in order, and the batch
// is saved.
func TestRemoveFromBatch(t *testing.T) {
	env := newMenuEnv(t)
	ids := addDownloadRecords(t, env, "A.ZIP", "B.ZIP", "C.ZIP")

	tagFiles(t, env, ids...)
	r := env.runCmd("REMOVEFROMBATCH", env.caller, "", "2\r")
	if !r.has(" 1. A.ZIP", " 2. B.ZIP", " 3. C.ZIP", "B.ZIP is off your batch: 2 left.") {
		t.Errorf("output:\n%s", r.text())
	}
	if got := env.mustDiskUser(env.caller.ID).TaggedFileIDs; len(got) != 2 || got[0] != ids[0] || got[1] != ids[2] {
		t.Errorf("saved batch = %v, want A and C", got)
	}

	tagFiles(t, env, ids...)
	env.runCmd("REMOVEFROMBATCH", env.caller, "", "c.zip\r")
	if got := env.mustDiskUser(env.caller.ID).TaggedFileIDs; len(got) != 2 || got[0] != ids[0] || got[1] != ids[1] {
		t.Errorf("by name: saved batch = %v, want A and B", got)
	}
}

// A number or name that isn't in the batch changes nothing; a blank line
// leaves; an empty batch says so; a file gone from the board is listed and
// can still be taken off by number.
func TestRemoveFromBatchEdgeCases(t *testing.T) {
	env := newMenuEnv(t)
	ids := addDownloadRecords(t, env, "A.ZIP", "B.ZIP")

	if r := env.runCmd("REMOVEFROMBATCH", env.caller, "", "1\r"); !strings.Contains(r.text(), stripPipes(env.e.Strings().BatchQueueEmpty)) {
		t.Errorf("empty batch:\n%s", r.text())
	}

	tagFiles(t, env, ids...)
	for _, in := range []string{"3\r", "0\r", "Z.ZIP\r", "\r"} {
		r := env.runCmd("REMOVEFROMBATCH", env.caller, "", in)
		if got := env.mustDiskUser(env.caller.ID).TaggedFileIDs; len(got) != 2 {
			t.Errorf("%q: saved batch = %v, want both still there", in, got)
		}
		if in != "\r" && !r.has("That file isn't in your batch.") {
			t.Errorf("%q: output:\n%s", in, r.text())
		}
	}

	gone := uuid.New()
	tagFiles(t, env, ids[0], gone)
	r := env.runCmd("REMOVEFROMBATCH", env.caller, "", "2\r")
	if !r.has(" 2. (no longer on the board)") {
		t.Errorf("missing file not listed:\n%s", r.text())
	}
	if got := env.mustDiskUser(env.caller.ID).TaggedFileIDs; len(got) != 1 || got[0] != ids[0] {
		t.Errorf("saved batch = %v, want just A", got)
	}
	if r := env.runCmd("REMOVEFROMBATCH", env.caller, "", ""); r.next != "LOGOFF" {
		t.Errorf("disconnect: next = %q", r.next)
	}
}

// The stock file menu binds R to REMOVEFROMBATCH, and E to EDITFILERECORD
// for sysops then EDITUPLOAD for everyone, so a caller's E edits their
// upload and a sysop's stays the review queue.
func TestStockFileMenuBindsCallerFileCommands(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "menus", "v3", "cfg", "FILEM.CFG"))
	if err != nil {
		t.Fatal(err)
	}
	var items []struct{ KEYS, CMD, ACS string }
	if err := json.Unmarshal(data, &items); err != nil {
		t.Fatal(err)
	}
	var e, r []string
	for _, it := range items {
		switch it.KEYS {
		case "E":
			e = append(e, it.CMD+"/"+it.ACS)
		case "R":
			r = append(r, it.CMD)
		}
	}
	if strings.Join(e, ",") != "RUN:EDITFILERECORD/SYSOP,RUN:EDITUPLOAD/" {
		t.Errorf("E runs %v, want the review queue for SYSOP then EDITUPLOAD", e)
	}
	if strings.Join(r, ",") != "RUN:REMOVEFROMBATCH" {
		t.Errorf("R runs %v, want RUN:REMOVEFROMBATCH", r)
	}
}
