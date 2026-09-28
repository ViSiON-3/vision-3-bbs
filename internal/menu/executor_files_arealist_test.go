package menu

import (
	"testing"

	"github.com/google/uuid"

	"github.com/ViSiON-3/vision-3-bbs/internal/file"
)

// TestListFileAreasHidesUnlistableAreas pins LISTFILEAR's ACS filter: the
// level-10 caller sees General Files (s10) but not the Upload Queue (s250),
// and the sysop sees both under the Local conference header.
func TestListFileAreasHidesUnlistableAreas(t *testing.T) {
	env := newMenuEnv(t)

	r := env.runCmd("LISTFILEAR", env.caller, "", "\r")
	if r.err != nil || r.next != "" {
		t.Fatalf("caller LISTFILEAR = (%q, %v), want (\"\", nil)", r.next, r.err)
	}
	if !r.has("GENERAL", "General Files") {
		t.Errorf("caller list missing General Files:\n%s", r.text())
	}
	if r.has("Upload Queue") {
		t.Errorf("caller list shows the s250 Upload Queue:\n%s", r.text())
	}

	r = env.runCmd("LISTFILEAR", env.sysop, "", "\r")
	if !r.has("General Files", "Upload Queue", "Local Areas (LOCAL)") {
		t.Errorf("sysop list should show both areas and the conference header:\n%s", r.text())
	}
}

// TestListFileAreasShowsFileCounts pins the ^NF column: a record added to
// General Files is counted in the listing.
func TestListFileAreasShowsFileCounts(t *testing.T) {
	env := newMenuEnv(t)
	for _, name := range []string{"ONE.ZIP", "TWO.ZIP", "THREE.ZIP"} {
		if err := env.e.FileMgr.AddFileRecord(file.FileRecord{ID: uuid.New(), AreaID: 1, Filename: name, UploadedBy: "Sysop"}); err != nil {
			t.Fatalf("AddFileRecord: %v", err)
		}
	}

	r := env.runCmd("LISTFILEAR", env.caller, "", "\r")
	if !r.has("General Files 3") {
		t.Errorf("want General Files listed with 3 files:\n%s", r.text())
	}
}

// TestListFileAreasNoAccessibleAreas pins the empty-list notice when every
// area's list ACS refuses the user.
func TestListFileAreasNoAccessibleAreas(t *testing.T) {
	env := newMenuEnv(t)
	env.caller.AccessLevel = 1

	r := env.runCmd("LISTFILEAR", env.caller, "", "\r")
	if !r.has("No accessible file areas found.") {
		t.Errorf("want the empty-list notice:\n%s", r.text())
	}
	if r.has("General Files") {
		t.Errorf("level-1 caller should see no areas:\n%s", r.text())
	}
}

// TestListFileAreasDisconnectAtPause pins that input ending at LISTFILEAR's
// pause logs the caller off.
func TestListFileAreasDisconnectAtPause(t *testing.T) {
	env := newMenuEnv(t)
	r := env.runCmd("LISTFILEAR", env.sysop, "", "")
	if r.next != "LOGOFF" {
		t.Errorf("next = %q, want LOGOFF", r.next)
	}
}
