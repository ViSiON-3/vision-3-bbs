package menu

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// sponsorTemplateSet builds a throwaway menu set holding just the named
// template files, each containing its own name so a test can tell them apart.
func sponsorTemplateSet(t *testing.T, names ...string) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "templates")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("<"+name+">"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// TestSponsorFileConfGuards pins the cases CHANGEFILECONF refuses before
// drawing the picker, each with its own configured message.
func TestSponsorFileConfGuards(t *testing.T) {
	t.Run("not logged in", func(t *testing.T) {
		env := newMenuEnv(t)
		r := env.runCmd("CHANGEFILECONF", nil, "", "\r")
		if r.user != nil || !r.has("You must be logged in to change conferences.") {
			t.Errorf("got user %v, want nil and the login notice:\n%s", r.user, r.text())
		}
	})

	t.Run("no conference manager", func(t *testing.T) {
		env := newMenuEnv(t)
		env.e.ConferenceMgr = nil
		r := env.runCmd("CHANGEFILECONF", env.sysop, "", "\r")
		if r.user != env.sysop || !r.has("No conferences configured.") {
			t.Errorf("want the sysop back and the no-conferences notice:\n%s", r.text())
		}
	})

	t.Run("templates missing", func(t *testing.T) {
		env := newMenuEnv(t)
		env.e.MenuSetPath = t.TempDir()
		r := env.runCmd("CHANGEFILECONF", env.sysop, "", "\r")
		if r.user != env.sysop || !r.has("Error loading conference templates.") {
			t.Errorf("want the sysop back and the template error:\n%s", r.text())
		}
		if r.has("No conferences configured.") {
			t.Errorf("missing templates reported as missing conferences:\n%s", r.text())
		}
	})

	// Both shipped conferences need s10, so a level-5 user is offered none.
	t.Run("no conference the user may join", func(t *testing.T) {
		env := newMenuEnv(t)
		low := &user.User{ID: 3, Handle: "Lowly", AccessLevel: 5, Validated: true, TimeLimit: 60}
		r := env.runCmd("CHANGEFILECONF", low, "", "\r")
		if r.user != low || !r.has("No conferences configured.") || r.has("Local Areas") {
			t.Errorf("want the no-conferences notice and no list:\n%s", r.text())
		}
		if low.CurrentFileConferenceID != 0 {
			t.Errorf("joined conference %d without access", low.CurrentFileConferenceID)
		}
	})
}

// TestSponsorFileConfJoinAppliesToBothMenus pins that picking a conference
// in CHANGEFILECONF joins it for files and messages alike, moves each side to
// its first accessible area there, and saves the result.
func TestSponsorFileConfJoinAppliesToBothMenus(t *testing.T) {
	env := newMenuEnv(t)
	u := env.sysop
	u.CurrentFileConferenceID, u.CurrentFileConferenceTag = 1, "LOCAL"
	u.CurrentMsgConferenceID, u.CurrentMsgConferenceTag = 1, "LOCAL"
	u.CurrentFileAreaID, u.CurrentFileAreaTag = 2, "UPLOADS"
	u.CurrentMessageAreaID, u.CurrentMessageAreaTag = 2, "PRIVMAIL"

	// Down to FelonyNet, which ships with no areas on either side: the old
	// areas are cleared rather than left pointing into Local.
	r := env.sub(t).runCmd("CHANGEFILECONF", u, "", "\x1b[B\r")
	if r.err != nil || r.user != u || r.next != "" {
		t.Fatalf("got (%v, %q, %v), want the sysop back", r.user, r.next, r.err)
	}
	if !r.has("Current Conference: Local Areas", "[ FelonyNet ] Conference Joined!") {
		t.Errorf("want the current conference in the header and the join notice:\n%s", r.text())
	}
	got := snapshotConfSelection(env.mustDiskUser(u.ID))
	want := confSelection{msgConfID: 2, fileConfID: 2, msgConfTag: "FELONYNET", fileConfTag: "FELONYNET"}
	if got != want {
		t.Errorf("saved selection after joining FelonyNet:\n got %+v\nwant %+v", got, want)
	}

	// Back up to Local: each side lands on that conference's first area.
	r = env.sub(t).runCmd("CHANGEFILECONF", u, "", "\x1b[A\r")
	if !r.has("Current Conference: FelonyNet", "[ Local Areas ] Conference Joined!") {
		t.Errorf("want FelonyNet as the current conference and the join notice:\n%s", r.text())
	}
	got = snapshotConfSelection(env.mustDiskUser(u.ID))
	want = confSelection{
		msgConfID: 1, msgConfTag: "LOCAL", msgAreaID: 1, msgAreaTag: "GENERAL",
		fileConfID: 1, fileConfTag: "LOCAL", fileAreaID: 1, fileAreaTag: "GENERAL",
	}
	if got != want {
		t.Errorf("saved selection after rejoining Local:\n got %+v\nwant %+v", got, want)
	}
}

// TestSponsorFileConfJoinRespectsAreaAccess pins that the join lands only on
// areas the user may use: in Local a level-10 caller can list General Files
// and read General Discussion, the first area on each side.
func TestSponsorFileConfJoinRespectsAreaAccess(t *testing.T) {
	env := newMenuEnv(t)

	// The picker opens on the first conference for a caller who has none.
	r := env.runCmd("CHANGEFILECONF", env.caller, "", "\r")
	if !r.has("Current Conference: None", "[ Local Areas ] Conference Joined!") {
		t.Errorf("want no current conference and the join notice:\n%s", r.text())
	}
	saved := env.mustDiskUser(env.caller.ID)
	if saved.CurrentFileAreaTag != "GENERAL" || saved.CurrentMessageAreaTag != "GENERAL" {
		t.Errorf("saved areas = file %q msg %q, want GENERAL for both",
			saved.CurrentFileAreaTag, saved.CurrentMessageAreaTag)
	}
}

// TestSponsorFileConfQuitChangesNothing pins that leaving the picker without
// choosing keeps the selection, in memory and on disk.
func TestSponsorFileConfQuitChangesNothing(t *testing.T) {
	env := newMenuEnv(t)
	u := env.sysop
	u.CurrentFileConferenceID, u.CurrentFileAreaID = 1, 2
	before := snapshotConfSelection(u)

	r := env.runCmd("CHANGEFILECONF", u, "", "\x1b[Bq")
	if r.err != nil || r.has("Conference Joined!") {
		t.Errorf("quit should join nothing (err %v):\n%s", r.err, r.text())
	}
	if got := snapshotConfSelection(u); got != before {
		t.Errorf("selection changed by quitting:\n got %+v\nwant %+v", got, before)
	}
	if saved := env.mustDiskUser(u.ID); saved.CurrentFileConferenceID != 0 {
		t.Errorf("quit saved file conference %d", saved.CurrentFileConferenceID)
	}
}

// TestSponsorFileConfSaveFailureRevertsSelection pins that a join which
// cannot be saved is reported and undone, so the session does not show a
// conference that would vanish on the next call.
func TestSponsorFileConfSaveFailureRevertsSelection(t *testing.T) {
	env := newMenuEnv(t)
	u := env.sysop
	u.CurrentFileConferenceID, u.CurrentFileConferenceTag = 1, "LOCAL"
	u.CurrentMsgConferenceID, u.CurrentMsgConferenceTag = 1, "LOCAL"
	u.CurrentFileAreaID, u.CurrentFileAreaTag = 2, "UPLOADS"
	u.CurrentMessageAreaID, u.CurrentMessageAreaTag = 2, "PRIVMAIL"
	before := snapshotConfSelection(u)
	sponsorReadOnlyDir(t, env.dataDir())

	r := env.runCmd("CHANGEFILECONF", u, "", "\x1b[B\r")
	if r.err != nil || r.user != u {
		t.Fatalf("got (%v, %v), want the sysop and nil", r.user, r.err)
	}
	if !r.has("Could not save the conference change.") || r.has("Conference Joined!") {
		t.Errorf("want the save error and no join notice:\n%s", r.text())
	}
	if got := snapshotConfSelection(u); got != before {
		t.Errorf("selection not reverted:\n got %+v\nwant %+v", got, before)
	}
}

// TestSponsorFileConfTemplateFallback pins which templates the file
// conference picker uses: FILECONF.* when a set ships both, the MSGCONF.*
// pair otherwise, and none when neither pair is complete.
func TestSponsorFileConfTemplateFallback(t *testing.T) {
	tests := []struct {
		name             string
		files            []string
		wantTop, wantMid string
	}{
		{"file templates preferred", []string{"FILECONF.TOP", "FILECONF.MID", "MSGCONF.TOP", "MSGCONF.MID"}, "<FILECONF.TOP>", "<FILECONF.MID>"},
		{"message templates as fallback", []string{"MSGCONF.TOP", "MSGCONF.MID"}, "<MSGCONF.TOP>", "<MSGCONF.MID>"},
		{"half a file pair falls back", []string{"FILECONF.TOP", "MSGCONF.TOP", "MSGCONF.MID"}, "<MSGCONF.TOP>", "<MSGCONF.MID>"},
		{"half of each pair is nothing", []string{"FILECONF.TOP", "MSGCONF.MID"}, "", ""},
		{"empty set", nil, "", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := &MenuExecutor{MenuSetPath: sponsorTemplateSet(t, tc.files...)}
			top, mid := loadFileConfTemplates(e)
			if string(top) != tc.wantTop || string(mid) != tc.wantMid {
				t.Errorf("templates = (%q, %q), want (%q, %q)", top, mid, tc.wantTop, tc.wantMid)
			}
			if tc.wantTop == "" && (top != nil || mid != nil) {
				t.Errorf("want nil templates, got (%q, %q)", top, mid)
			}
		})
	}
}

// TestSponsorFileConfUsesOwnTemplates pins that a menu set's FILECONF
// templates are what the picker actually draws with.
func TestSponsorFileConfUsesOwnTemplates(t *testing.T) {
	env := newMenuEnv(t)
	env.e.MenuSetPath = sponsorTemplateSet(t, "FILECONF.TOP", "FILECONF.MID")

	r := env.runCmd("CHANGEFILECONF", env.sysop, "", "q")
	if r.err != nil || !r.has("<FILECONF.TOP>", "<FILECONF.MID>") {
		t.Errorf("want the set's own templates drawn (err %v):\n%s", r.err, r.text())
	}
}
