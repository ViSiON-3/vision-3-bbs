package menu

import (
	"os"
	"strings"
	"testing"
)

// filesManageRoles grants files.manage in GENERAL only (general_files) and
// in both stock file areas (all_files).
const filesManageRoles = `{"roles": [
	{"name": "general_files", "permissions": [{"permission": "files.manage", "areas": ["GENERAL"]}]},
	{"name": "all_files", "permissions": [{"permission": "files.manage", "areas": ["GENERAL", "UPLOADS"]}]},
	{"name": "upload_files", "permissions": [{"permission": "files.manage", "areas": ["UPLOADS"]}]}
]}`

// setCallerRoles sets env's caller's roles, on the stored record (which the
// permission checks read) and on the session copy, keeping the session copy's other settings.
func setCallerRoles(t *testing.T, env *menuEnv, roles ...string) {
	t.Helper()
	stored, _ := env.um.GetUserByID(env.caller.ID)
	stored.Roles = roles
	if err := env.um.UpdateUserByID(stored); err != nil {
		t.Fatal(err)
	}
	env.caller.Roles = roles
}

// A role granting files.manage in GENERAL gives the file list's management
// keys there: kill deletes the file.
func TestFilesManageRoleKillsInItsArea(t *testing.T) {
	useRoles(t, filesManageRoles)
	env := lightbarListEnv(t)
	setCallerRoles(t, env, "general_files")
	id, p := addReviewUpload(t, env, 1, "DOOMED.ZIP", 4)

	r := env.runCmd("LISTFILES", env.caller, "", "kYq")
	if !r.has("Delete DOOMED.ZIP from disk?") {
		t.Fatalf("no delete prompt for the file manager:\n%s", r.text())
	}
	if reloadedFileRecord(t, env, id) != nil {
		t.Error("record survived the delete")
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Errorf("file still on disk: %v", err)
	}
}

// A grant for another area gives nothing here.
func TestFilesManageRoleForAnotherAreaGivesNothing(t *testing.T) {
	useRoles(t, filesManageRoles)
	env := lightbarListEnv(t)
	setCallerRoles(t, env, "upload_files")
	id, _ := addReviewUpload(t, env, 1, "KEEP.ZIP", 4)

	r := env.runCmd("LISTFILES", env.caller, "", "kYq")
	if r.has("Delete KEEP.ZIP") || reloadedFileRecord(t, env, id) == nil {
		t.Errorf("a manager of UPLOADS deleted from GENERAL:\n%s", r.text())
	}
}

// A move needs files.manage in the area it goes to as well.
func TestFilesManageMoveNeedsBothAreas(t *testing.T) {
	useRoles(t, filesManageRoles)
	env := lightbarListEnv(t)
	setCallerRoles(t, env, "general_files")
	id, _ := addReviewUpload(t, env, 1, "MOVER.ZIP", 4)

	r := env.runCmd("LISTFILES", env.caller, "", "m2\rq")
	if !r.has("You don't manage the files in Upload Queue.") || r.has("Move MOVER.ZIP to") {
		t.Errorf("move into an unmanaged area wasn't refused up front:\n%s", r.text())
	}
	if got := reloadedFileRecord(t, env, id).AreaID; got != 1 {
		t.Fatalf("area = %d, want 1", got)
	}

	setCallerRoles(t, env, "all_files")
	env.runCmd("LISTFILES", env.caller, "", "m2\rYq")
	if got := reloadedFileRecord(t, env, id).AreaID; got != 2 {
		t.Errorf("area = %d after a move between managed areas, want 2", got)
	}
}

// A grant taken away while the caller is at the prompt stops the change.
func TestFilesManageRevokedAtPrompt(t *testing.T) {
	useRoles(t, filesManageRoles)
	env := lightbarListEnv(t)
	setCallerRoles(t, env, "general_files")
	id, _ := addReviewUpload(t, env, 1, "DOOMED.ZIP", 4)
	env.whenOutput("Delete DOOMED.ZIP from disk?", func() { useRoles(t, `{"roles": []}`) })

	r := env.runCmd("LISTFILES", env.caller, "", "kYq")
	if !r.has("You can no longer manage files in this area. Nothing was changed.") {
		t.Errorf("no refusal:\n%s", r.text())
	}
	if reloadedFileRecord(t, env, id) == nil {
		t.Error("the file was deleted after the grant was taken away")
	}
}

// The review queue lists only the areas the caller manages.
func TestFilesManageReviewQueueListsManagedAreas(t *testing.T) {
	useRoles(t, filesManageRoles)
	env := newMenuEnv(t)
	setCallerRoles(t, env, "general_files")
	addReviewUpload(t, env, 1, "MINE.ZIP", 4)
	addReviewUpload(t, env, 2, "THEIRS.ZIP", 4)

	r := env.runCmd("EDITFILERECORD", env.caller, "", "Y\rQ\r")
	if !r.has("MINE.ZIP") || r.has("THEIRS.ZIP") {
		t.Errorf("review queue for a GENERAL manager:\n%s", r.text())
	}
}

// A file manager may edit any description in their area, not just their
// own uploads; elsewhere only their own.
func TestFilesManageEditsAnyDescriptionInArea(t *testing.T) {
	useRoles(t, filesManageRoles)
	env := newMenuEnv(t)
	setCallerRoles(t, env, "general_files")
	ids := addUploads(t, env, map[string]string{"SYSOPS.ZIP": "Sysop"})
	env.caller.CurrentFileAreaID = 1

	r := env.runCmd("EDITUPLOAD", env.caller, "", "sysops.zip\rbetter words\r")
	if !r.has("Description saved.") || fileDesc(t, env, ids["SYSOPS.ZIP"]) != "better words" {
		t.Errorf("file manager couldn't edit the description:\n%s", r.text())
	}

	setCallerRoles(t, env, "upload_files")
	r = env.runCmd("EDITUPLOAD", env.caller, "", "sysops.zip\rworse words\r")
	if !r.has("That file wasn't uploaded by you.") || strings.Contains(fileDesc(t, env, ids["SYSOPS.ZIP"]), "worse") {
		t.Errorf("a manager of another area edited it:\n%s", r.text())
	}
}
