package menu

import (
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/ViSiON-3/vision-3-bbs/internal/file"
)

// diz is a three-line FILE_ID.DIZ description.
const diz = "COOL GAME v1.0\nA game about things\n(c) 1994 Somebody"

// setDesc sets file id's description.
func setDesc(t *testing.T, env *menuEnv, id uuid.UUID, desc string) {
	t.Helper()
	if err := env.e.FileMgr.UpdateFileRecord(id, func(r *file.FileRecord) { r.Description = desc }); err != nil {
		t.Fatal(err)
	}
}

// Ctrl-Z saves in the full-screen editor; Ctrl-A then Y aborts.
const (
	editorSave  = "\x1a"
	editorAbort = "\x01Y"
)

func TestMultiLineDescription(t *testing.T) {
	for desc, want := range map[string]bool{
		"one line": false, "one line\n": false, "": false,
		"two\nlines": true, diz: true,
	} {
		if got := multiLineDescription(desc); got != want {
			t.Errorf("multiLineDescription(%q) = %v", desc, got)
		}
	}
	if got := descriptionLineCap(diz); got != 10 {
		t.Errorf("cap for 3 lines = %d, want 10", got)
	}
	if got := descriptionLineCap(strings.Repeat("x\n", 11) + "x"); got != 12 {
		t.Errorf("cap for 12 lines = %d, want 12", got)
	}
}

// A multi-line description is edited in place in the full-screen editor:
// the caller fixes one line and the others stay (#1021).
func TestEditUploadMultiLineInPlace(t *testing.T) {
	env := newMenuEnv(t)
	ids := addUploads(t, env, map[string]string{"GAME.ZIP": "Caller"})
	setDesc(t, env, ids["GAME.ZIP"], diz)
	env.caller.CurrentFileAreaID = 1

	r := env.runCmd("EDITUPLOAD", env.caller, "", "GAME.ZIP\r and more"+editorSave)
	if !r.has("Full Screen Message Editor", "Description saved.") || r.has("New description:") {
		t.Errorf("output:\n%s", r.text())
	}
	if got, want := fileDesc(t, env, ids["GAME.ZIP"]), diz+" and more"; got != want {
		t.Errorf("description = %q, want %q", got, want)
	}
}

// Aborting the editor, or saving it unchanged, keeps the description. (The
// editor itself won't save an empty text.)
func TestEditUploadMultiLineKeeps(t *testing.T) {
	env := newMenuEnv(t)
	ids := addUploads(t, env, map[string]string{"GAME.ZIP": "Caller"})
	env.caller.CurrentFileAreaID = 1
	for name, keys := range map[string]string{
		"abort":     editorAbort,
		"unchanged": editorSave,
	} {
		setDesc(t, env, ids["GAME.ZIP"], diz)
		r := env.runCmd("EDITUPLOAD", env.caller, "", "GAME.ZIP\r"+keys)
		if !r.has("Description unchanged.") {
			t.Errorf("%s: output:\n%s", name, r.text())
		}
		if got := fileDesc(t, env, ids["GAME.ZIP"]); got != diz {
			t.Errorf("%s: description = %q", name, got)
		}
	}
}

// A description changed while the caller edits it in place isn't written
// over: the caller's edit started from the old one.
func TestEditUploadMultiLineChangedMeanwhile(t *testing.T) {
	env := newMenuEnv(t)
	ids := addUploads(t, env, map[string]string{"GAME.ZIP": "Caller"})
	setDesc(t, env, ids["GAME.ZIP"], diz)
	env.caller.CurrentFileAreaID = 1
	env.whenOutput("Full Screen Message Editor", func() { setDesc(t, env, ids["GAME.ZIP"], "the sysop's words\nline two") })

	r := env.runCmd("EDITUPLOAD", env.caller, "", "GAME.ZIP\r!"+editorSave)
	if !r.has("That file was changed elsewhere. Nothing was saved.") {
		t.Errorf("output:\n%s", r.text())
	}
	if got := fileDesc(t, env, ids["GAME.ZIP"]); got != "the sysop's words\nline two" {
		t.Errorf("description = %q", got)
	}
}

// An edited description keeps at most 10 lines, or the description's own
// count when it has more, and the caller is told when lines were dropped.
func TestEditUploadMultiLineCap(t *testing.T) {
	env := newMenuEnv(t)
	ids := addUploads(t, env, map[string]string{"GAME.ZIP": "Caller"})
	env.caller.CurrentFileAreaID = 1

	setDesc(t, env, ids["GAME.ZIP"], diz)
	r := env.runCmd("EDITUPLOAD", env.caller, "", "GAME.ZIP\r"+strings.Repeat("\rmore", 9)+editorSave)
	if !r.has("Only its first 10 lines were kept.") {
		t.Errorf("output:\n%s", r.text())
	}
	if got := strings.Count(fileDesc(t, env, ids["GAME.ZIP"]), "\n") + 1; got != 10 {
		t.Errorf("%d lines kept, want 10", got)
	}

	long := strings.TrimSuffix(strings.Repeat("line\n", 12), "\n")
	setDesc(t, env, ids["GAME.ZIP"], long)
	env.runCmd("EDITUPLOAD", env.caller, "", "GAME.ZIP\r!"+editorSave)
	if got := fileDesc(t, env, ids["GAME.ZIP"]); got != long+"!" {
		t.Errorf("a 12-line description lost lines: %q", got)
	}
}

// The sysop's review queue edits a multi-line description the same way.
func TestEditFileRecordMultiLineDescription(t *testing.T) {
	env := newMenuEnv(t)
	id, _ := addReviewUpload(t, env, 1, "GAME.ZIP", 10)
	setDesc(t, env, id, diz)

	r := env.runCmd("EDITFILERECORD", env.sysop, "", "Y\rC\r and more"+editorSave+"Y\r")
	if r.err != nil {
		t.Fatalf("err = %v", r.err)
	}
	rec := reloadedFileRecord(t, env, id)
	if rec == nil || rec.Description != diz+" and more" || !rec.Reviewed {
		t.Errorf("record = %+v, want the edited DIZ, reviewed", rec)
	}
}

// A DOS FILE_ID.DIZ is stored with CRLF, or with bare CRs, which the file
// list also breaks lines at. Either is edited in place, opens in the editor
// with plain line breaks, and saved unchanged is not a change (#1021).
func TestEditUploadLineEndings(t *testing.T) {
	env := newMenuEnv(t)
	ids := addUploads(t, env, map[string]string{"GAME.ZIP": "Caller"})
	env.caller.CurrentFileAreaID = 1
	for _, stored := range []string{
		"COOL GAME\r\nA game\r\n(c) 1994\r\n",
		"COOL GAME\rA game\r(c) 1994",
		"COOL GAME   \nA game\t\n(c) 1994\n\n",
	} {
		setDesc(t, env, ids["GAME.ZIP"], stored)
		r := env.runCmd("EDITUPLOAD", env.caller, "", "GAME.ZIP\r"+editorSave)
		if !r.has("Full Screen Message Editor", "Description unchanged.") || r.has("New description:") {
			t.Errorf("%q unchanged: output:\n%s", stored, r.text())
		}
		if got := fileDesc(t, env, ids["GAME.ZIP"]); got != stored {
			t.Errorf("%q was rewritten as %q", stored, got)
		}
		env.runCmd("EDITUPLOAD", env.caller, "", "GAME.ZIP\r!"+editorSave)
		if got := fileDesc(t, env, ids["GAME.ZIP"]); got != "COOL GAME\nA game\n(c) 1994!" {
			t.Errorf("%q edited: %q", stored, got)
		}
	}
}

// The editor holds 100 lines. A longer imported description saved unchanged
// is left whole; edited, it keeps 100 lines and the caller is told.
func TestEditUploadPastEditorLines(t *testing.T) {
	env := newMenuEnv(t)
	ids := addUploads(t, env, map[string]string{"GAME.ZIP": "Caller"})
	env.caller.CurrentFileAreaID = 1
	long := strings.TrimSuffix(strings.Repeat("line\n", 120), "\n")
	setDesc(t, env, ids["GAME.ZIP"], long)

	r := env.runCmd("EDITUPLOAD", env.caller, "", "GAME.ZIP\r"+editorSave)
	if !r.has("Description unchanged.") || fileDesc(t, env, ids["GAME.ZIP"]) != long {
		t.Errorf("unchanged save: output:\n%s", r.text())
	}
	r = env.runCmd("EDITUPLOAD", env.caller, "", "GAME.ZIP\r!"+editorSave)
	if !r.has("Only its first 100 lines were kept.") {
		t.Errorf("output:\n%s", r.text())
	}
	if got := fileDesc(t, env, ids["GAME.ZIP"]); strings.Count(got, "\n")+1 != 100 || !strings.HasSuffix(got, "line!") {
		t.Errorf("saved %d lines, ending %q", strings.Count(got, "\n")+1, got[len(got)-10:])
	}
}

// A one-line description changed while the caller types a new one isn't
// written over, the same as a multi-line one.
func TestEditUploadOneLineChangedMeanwhile(t *testing.T) {
	env := newMenuEnv(t)
	ids := addUploads(t, env, map[string]string{"GAME.ZIP": "Caller"})
	env.caller.CurrentFileAreaID = 1
	env.whenOutput("New description:", func() { setDesc(t, env, ids["GAME.ZIP"], "the sysop's words") })

	r := env.runCmd("EDITUPLOAD", env.caller, "", "GAME.ZIP\rmine\r")
	if !r.has("That file was changed elsewhere. Nothing was saved.") {
		t.Errorf("output:\n%s", r.text())
	}
	if got := fileDesc(t, env, ids["GAME.ZIP"]); got != "the sysop's words" {
		t.Errorf("description = %q", got)
	}
}

// In the review queue, an aborted edit, or one saved unchanged, isn't
// followed by the review mark: nothing was saved to approve.
func TestEditFileRecordMultiLineAbortNoReview(t *testing.T) {
	env := newMenuEnv(t)
	id, _ := addReviewUpload(t, env, 1, "GAME.ZIP", 10)
	for name, keys := range map[string]string{"abort": editorAbort, "unchanged": editorSave} {
		setDesc(t, env, id, diz)
		r := env.runCmd("EDITFILERECORD", env.sysop, "", "Y\rC\r"+keys+"Y\r")
		if r.has("Mark as reviewed?") {
			t.Errorf("%s: review mark offered:\n%s", name, r.text())
		}
		if rec := reloadedFileRecord(t, env, id); rec == nil || rec.Description != diz || rec.Reviewed {
			t.Errorf("%s: record = %+v", name, rec)
		}
	}
}

// The review queue's one-line description is cut to 60 characters and
// cleaned of control characters, as EDITUPLOAD's is, and isn't written over
// a change made meanwhile.
func TestEditFileRecordOneLineAsUpload(t *testing.T) {
	env := newMenuEnv(t)
	id, _ := addReviewUpload(t, env, 1, "GAME.ZIP", 10)
	env.runCmd("EDITFILERECORD", env.sysop, "", "Y\rC\r"+strings.Repeat("x", 70)+"\rN\r")
	if got := reloadedFileRecord(t, env, id).Description; got != strings.Repeat("x", maxUploadDescription) {
		t.Errorf("description = %q, want 60 x", got)
	}

	setDesc(t, env, id, "original desc")
	env.whenOutput("New description:", func() { setDesc(t, env, id, "the uploader's words") })
	r := env.runCmd("EDITFILERECORD", env.sysop, "", "Y\rC\rmine\rY\r")
	if !r.has("That description was changed elsewhere. Nothing was saved.") || r.has("Mark as reviewed?") {
		t.Errorf("output:\n%s", r.text())
	}
	if rec := reloadedFileRecord(t, env, id); rec.Description != "the uploader's words" || rec.Reviewed {
		t.Errorf("record = %+v", rec)
	}
}
