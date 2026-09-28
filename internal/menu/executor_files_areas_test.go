package menu

import (
	"strings"
	"testing"
)

// TestSelectFileAreaLightbarNavigatesWithoutJoining drives the default
// (lightbar) SELECTFILEAREA through its navigation keys — arrows, paging,
// Home/End, a digit, and Left/Right across conferences — and quits. Only the
// areas the caller may list are offered, and nothing is joined.
func TestSelectFileAreaLightbarNavigatesWithoutJoining(t *testing.T) {
	env := newMenuEnv(t)
	env.sysop.CurrentFileConferenceID = 1

	keys := "\x1b[B\x1b[A\x1b[6~\x1b[5~\x1b[H\x1b[F1\x1b[C\x1b[Dq"
	r := env.runCmd("SELECTFILEAREA", env.sysop, "", keys)
	if r.err != nil || r.user != env.sysop {
		t.Fatalf("result = (%v, %v), want the sysop and nil", r.user, r.err)
	}
	if env.sysop.CurrentFileAreaID != 0 || env.sysop.CurrentFileConferenceID != 1 {
		t.Errorf("navigation changed the selection: area=%d conf=%d",
			env.sysop.CurrentFileAreaID, env.sysop.CurrentFileConferenceID)
	}
	if !r.has("General Files", "Upload Queue", "No File Areas Available") {
		t.Errorf("want both Local areas, then FelonyNet's empty list:\n%s", r.text())
	}

	env.caller.CurrentFileConferenceID = 1
	r = env.runCmd("SELECTFILEAREA", env.caller, "", "\x1b")
	if !r.has("General Files") || r.has("Upload Queue") {
		t.Errorf("caller should be offered only General Files:\n%s", r.text())
	}
}

// TestSelectFileAreaLightbarJoinsSelectedArea pins that Enter joins the
// highlighted file area and saves it.
func TestSelectFileAreaLightbarJoinsSelectedArea(t *testing.T) {
	env := newMenuEnv(t)
	env.sysop.CurrentFileConferenceID = 1

	r := env.runCmd("SELECTFILEAREA", env.sysop, "", "\x1b[B\r")
	if r.err != nil {
		t.Fatalf("err = %v", r.err)
	}
	saved := env.mustDiskUser(env.sysop.ID)
	if saved.CurrentFileAreaTag != "UPLOADS" || saved.CurrentFileAreaID != 2 || saved.CurrentFileConferenceID != 1 {
		t.Errorf("saved file area = %d/%q conf %d, want 2/UPLOADS conf 1",
			saved.CurrentFileAreaID, saved.CurrentFileAreaTag, saved.CurrentFileConferenceID)
	}
}

// TestSelectFileAreaClassicPromptCommands pins the text-mode selector a
// "classic" file-listing user gets: blank reprompts, ? relists, Q quits, and
// none of them change the area.
func TestSelectFileAreaClassicPromptCommands(t *testing.T) {
	env := newMenuEnv(t)
	env.caller.FileListingMode = "classic"

	r := env.runCmd("SELECTFILEAREA", env.caller, "", "\r?\rq\r")
	if r.err != nil || r.user != env.caller {
		t.Fatalf("result = (%v, %v), want the caller and nil", r.user, r.err)
	}
	if n := strings.Count(r.text(), "General Files"); n != 2 {
		t.Errorf("General Files listed %d times, want 2 (initial and ?):\n%s", n, r.text())
	}
	if env.caller.CurrentFileAreaID != 0 {
		t.Errorf("area changed to %d without a selection", env.caller.CurrentFileAreaID)
	}

	r = env.runCmd("SELECTFILEAREA", env.caller, "", "")
	if r.next != "LOGOFF" {
		t.Errorf("disconnect at the prompt: next = %q, want LOGOFF", r.next)
	}
}

// TestSelectFileAreaClassicRefusesThenSelects pins the text-mode selector's
// ACS check and its save: the caller asking for area 2 (Upload Queue, s250)
// by ID is refused, then naming GENERAL by tag joins it and persists.
func TestSelectFileAreaClassicRefusesThenSelects(t *testing.T) {
	env := newMenuEnv(t)
	env.caller.FileListingMode = "classic"

	r := env.runCmd("SELECTFILEAREA", env.caller, "", "2\rgeneral\r")
	if r.err != nil {
		t.Fatalf("err = %v", r.err)
	}
	if !r.has("Access denied to file area 'UPLOADS'!") {
		t.Errorf("Upload Queue should be refused for the caller:\n%s", r.text())
	}
	if !r.has("Current file area set to: General Files") {
		t.Errorf("missing selection confirmation:\n%s", r.text())
	}
	saved := env.mustDiskUser(env.caller.ID)
	if saved.CurrentFileAreaTag != "GENERAL" || saved.CurrentFileConferenceID != 1 {
		t.Errorf("saved file area = %q conf %d, want GENERAL conf 1", saved.CurrentFileAreaTag, saved.CurrentFileConferenceID)
	}
}
