package menu

import (
	"os"
	"path/filepath"
	"testing"
)

// TestWantListCallerSubmitsRequest pins the caller's WANTLIST: a filename and
// reason are appended to data/wantlist.json under the caller's handle; a
// blank filename adds nothing.
func TestWantListCallerSubmitsRequest(t *testing.T) {
	env := newMenuEnv(t)

	env.runCmd("WANTLIST", env.caller, "", "\r")
	if _, err := os.Stat(filepath.Join(env.dataDir(), "wantlist.json")); !os.IsNotExist(err) {
		t.Fatalf("blank filename wrote the want list: %v", err)
	}

	r := env.runCmd("WANTLIST", env.caller, "", "DOOM2.ZIP\rneed it\r")
	if !r.has(stripPipes(env.e.Strings().WantListSubmitted)) {
		t.Errorf("no submitted notice:\n%s", r.text())
	}
	env.runCmd("WANTLIST", env.caller, "", "QUAKE.ZIP\r\r")

	wl, err := loadWantList(env.dataDir())
	if err != nil {
		t.Fatal(err)
	}
	got := wl.Entries
	if len(got) != 2 || got[0].ID == got[1].ID || got[0].Handle != "Caller" || got[0].Filename != "DOOM2.ZIP" || got[0].Reason != "need it" ||
		got[1].Filename != "QUAKE.ZIP" || got[0].Date == "" {
		t.Errorf("want list = %+v", got)
	}
	if r := env.runCmd("WANTLIST", nil, "", "X\r"); r.raw != "" {
		t.Errorf("no user should do nothing, got:\n%s", r.text())
	}
}

// TestWantListSysopReviewsAndDeletes pins the sysop's WANTLIST: an empty list
// says so; entries are listed; D removes one by number (bad numbers change
// nothing); C clears everything.
func TestWantListSysopReviewsAndDeletes(t *testing.T) {
	env := newMenuEnv(t)
	if r := env.runCmd("WANTLIST", env.sysop, "", "\r"); !r.has(stripPipes(env.e.Strings().WantListEmpty)) {
		t.Errorf("empty list:\n%s", r.text())
	}

	if err := saveWantList(env.dataDir(), &wantListData{NextID: 4, Entries: []WantListEntry{
		{ID: 1, Handle: "Caller", Filename: "ONE.ZIP", Reason: "r1", Date: "01/02/2026"},
		{ID: 2, Handle: "Other", Filename: "TWO.ZIP", Reason: "r2", Date: "01/03/2026"},
		{ID: 3, Handle: "Third", Filename: "THREE.ZIP", Reason: "r3", Date: "01/04/2026"},
	}}); err != nil {
		t.Fatal(err)
	}

	r := env.runCmd("WANTLIST", env.sysop, "", "Q\r")
	if !r.has("ONE.ZIP", "TWO.ZIP", "THREE.ZIP", "Other") {
		t.Errorf("entries not listed:\n%s", r.text())
	}
	env.runCmd("WANTLIST", env.sysop, "", "D\r9\r")
	env.runCmd("WANTLIST", env.sysop, "", "D\rabc\r")
	if wl, _ := loadWantList(env.dataDir()); len(wl.Entries) != 3 {
		t.Fatalf("bad delete numbers changed the list: %+v", wl.Entries)
	}
	env.runCmd("WANTLIST", env.sysop, "", "d\r2\r")
	wl, _ := loadWantList(env.dataDir())
	got := wl.Entries
	if len(got) != 2 || got[0].Filename != "ONE.ZIP" || got[1].Filename != "THREE.ZIP" {
		t.Errorf("after deleting #2: %+v", got)
	}

	r = env.runCmd("WANTLIST", env.sysop, "", "C\r")
	if !r.has(stripPipes(env.e.Strings().WantListCleared)) {
		t.Errorf("no cleared notice:\n%s", r.text())
	}
	if wl, _ := loadWantList(env.dataDir()); len(wl.Entries) != 0 || wl.NextID != 4 {
		t.Errorf("after clear: %+v, want no entries and next_id kept at 4", wl)
	}
}

// TestWantListCorruptFileReportsError pins that an unparseable want list is
// surfaced as an error to both views rather than silently overwritten.
func TestWantListCorruptFileReportsError(t *testing.T) {
	env := newMenuEnv(t)
	p := filepath.Join(env.dataDir(), "wantlist.json")
	if err := os.WriteFile(p, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if r := env.runCmd("WANTLIST", env.sysop, "", "C\r"); r.err == nil {
		t.Error("sysop view: want a parse error")
	}
	if r := env.runCmd("WANTLIST", env.caller, "", "X.ZIP\rwhy\r"); r.err == nil {
		t.Error("caller submit: want a parse error")
	}
	if b, _ := os.ReadFile(p); string(b) != "{not json" {
		t.Errorf("corrupt file overwritten: %q", b)
	}
}
