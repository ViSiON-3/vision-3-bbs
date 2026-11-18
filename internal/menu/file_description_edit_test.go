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
