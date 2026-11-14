package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/ViSiON-3/vision-3-bbs/internal/file"
	"github.com/ViSiON-3/vision-3-bbs/internal/menu"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

type sponsorFinding struct {
	name, message string
	status        doctorSeverity
}

func writeOwnersFile(t *testing.T, root, rel, body string) {
	t.Helper()
	path := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestCheckSponsors(t *testing.T) {
	root := t.TempDir()
	writeOwnersFile(t, root, "configs/message_areas.json", `[
		{"id": 1, "tag": "OK", "sponsor": "Alice", "sponsor_id": 2},
		{"id": 2, "tag": "UNBOUND", "sponsor": "Alice"},
		{"id": 3, "tag": "NOBODY", "sponsor": "Ghost", "sponsor_id": -1},
		{"id": 4, "tag": "GONE", "sponsor": "Bob", "sponsor_id": 9},
		{"id": 5, "tag": "RENAMED", "sponsor": "Carol", "sponsor_id": 3}
	]`)
	writeOwnersFile(t, root, "data/users/users.json", `[
		{"id": 2, "handle": "Alice"},
		{"id": 3, "handle": "Caroline"}
	]`)
	var got []sponsorFinding
	checkSponsors(filepath.Join(root, "configs"), root, func(name string, status doctorSeverity, message, _ string) {
		got = append(got, sponsorFinding{name, message, status})
	})
	var msgs []string
	for _, c := range got {
		if c.status != doctorWarn || doctorSection(c.name) != "NETWORK & MESSAGING" {
			t.Errorf("check %+v", c)
		}
		msgs = append(msgs, c.message)
	}
	joined := strings.Join(msgs, "\n")
	for _, want := range []string{
		`area NOBODY: sponsor "Ghost" matched no account`,
		`area GONE: sponsor "Bob" was account #9, which no longer exists`,
		`area RENAMED: sponsor is account #3, now "Caroline", but the area names "Carol"`,
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("checks lack %q:\n%s", want, joined)
		}
	}
	if len(got) != 3 {
		t.Errorf("%d findings, want 3:\n%s", len(got), joined)
	}
}

func TestCheckUserIDMark(t *testing.T) {
	for _, tc := range []struct {
		mark string
		want doctorSeverity
	}{
		{"", doctorInfo},
		{"lots", doctorFail},
		{"3", doctorWarn},
		{"9", doctorOK},
	} {
		root := t.TempDir()
		writeOwnersFile(t, root, "data/users/users.json", `[{"id": 1}, {"id": 4}]`)
		if tc.mark != "" {
			writeOwnersFile(t, root, "data/users/next_user_id", tc.mark+"\n")
		}
		var got []sponsorFinding
		checkUserIDMark(root, func(name string, status doctorSeverity, message, _ string) {
			got = append(got, sponsorFinding{name, message, status})
		})
		if len(got) != 1 || got[0].status != tc.want {
			t.Errorf("mark %q: %+v, want %s", tc.mark, got, tc.want)
		}
	}
}

func TestCheckHandleCollisions(t *testing.T) {
	root := t.TempDir()
	writeOwnersFile(t, root, "data/users/users.json", `[{"id": 1, "handle": "Sam"}, {"id": 2, "handle": "ſam"}, {"id": 3, "handle": "Ann"}]`)
	var got []sponsorFinding
	checkHandleCollisions(root, func(name string, status doctorSeverity, message, _ string) {
		got = append(got, sponsorFinding{name, message, status})
	})
	if len(got) != 1 || got[0].status != doctorWarn || !strings.Contains(got[0].message, `"Sam" (#1) and "ſam" (#2)`) {
		t.Errorf("findings = %+v", got)
	}
}

// Binding refuses a retired handle (#1053): a name from before IDs that a
// purged or renamed account had meant that account, not whoever holds the
// handle now. accountNameFor gives a bound account's current handle.
func TestOwnerLookupsAndRetiredHandles(t *testing.T) {
	dir := t.TempDir()
	um, err := user.NewUserManager(dir)
	if err != nil {
		t.Fatal(err)
	}
	bob, err := um.AddUser("password1", "Bob", "Bob Smith", "")
	if err != nil {
		t.Fatal(err)
	}
	idFor, nameFor := accountIDFor(um), accountNameFor(um)
	if got := idFor("bob"); got != bob.ID {
		t.Fatalf("idFor(bob) = %d, want %d", got, bob.ID)
	}
	if err := user.RetireHandles(filepath.Join(dir, "users.json"), "Bob"); err != nil {
		t.Fatal(err)
	}
	if got := idFor("bob"); got != 0 {
		t.Errorf("idFor(retired bob) = %d, want 0", got)
	}
	if name, ok := nameFor(bob.ID); !ok || name != "Bob" {
		t.Errorf("nameFor = %q, %v", name, ok)
	}
	if _, ok := nameFor(999); ok {
		t.Error("nameFor(unknown) found an account")
	}
}

// A file-area reload binds records that arrived without an uploader ID while
// the BBS ran, instead of leaving them for the next start (#1053).
func TestFileAreasReloadBindsUploaders(t *testing.T) {
	tmp := t.TempDir()
	configDir, dataDir := filepath.Join(tmp, "configs"), filepath.Join(tmp, "data")
	writeOwnersFile(t, tmp, "configs/file_areas.json", `[{"id":1,"tag":"ONE","name":"One","path":"one"}]`)
	fileMgr, err := file.NewFileManager(dataDir, configDir)
	if err != nil {
		t.Fatal(err)
	}
	um, err := user.NewUserManager(filepath.Join(tmp, "users"))
	if err != nil {
		t.Fatal(err)
	}
	ann, _ := um.AddUser("password1", "Ann", "Ann Here", "")
	id := uuid.New()
	if err := fileMgr.AddFileRecord(file.FileRecord{ID: id, AreaID: 1, Filename: "A.ZIP", UploadedBy: "Ann"}); err != nil {
		t.Fatal(err)
	}
	cw := &ConfigWatcher{rootConfigPath: configDir, menuExecutor: &menu.MenuExecutor{FileMgr: fileMgr}, userMgr: um}
	if err := cw.applyFileAreas(); err != nil {
		t.Fatal(err)
	}
	if rec, _ := fileMgr.GetFileRecordByID(id); rec == nil || rec.UploadedByID != ann.ID {
		t.Errorf("record = %+v, want uploader #%d", rec, ann.ID)
	}
}

// With the retired list unreadable, binding stops rather than record every
// old owner as nobody for good: the records stay unbound for a later run.
func TestBindingWaitsForAReadableRetiredList(t *testing.T) {
	tmp := t.TempDir()
	configDir, dataDir := filepath.Join(tmp, "configs"), filepath.Join(tmp, "data")
	writeOwnersFile(t, tmp, "configs/file_areas.json", `[{"id":1,"tag":"ONE","name":"One","path":"one"}]`)
	fileMgr, err := file.NewFileManager(dataDir, configDir)
	if err != nil {
		t.Fatal(err)
	}
	usersDir := filepath.Join(tmp, "users")
	um, err := user.NewUserManager(usersDir)
	if err != nil {
		t.Fatal(err)
	}
	ann, _ := um.AddUser("password1", "Ann", "Ann Here", "")
	id := uuid.New()
	if err := fileMgr.AddFileRecord(file.FileRecord{ID: id, AreaID: 1, Filename: "A.ZIP", UploadedBy: "Ann"}); err != nil {
		t.Fatal(err)
	}
	retired := filepath.Join(usersDir, user.RetiredHandlesFile)
	if err := os.Mkdir(retired, 0o700); err != nil { // unreadable as a file
		t.Fatal(err)
	}
	if err := bindUploaders(um, fileMgr); err == nil {
		t.Error("bindUploaders reported success with the list unreadable")
	}
	// The file-area reload reports it too, so the watcher retries.
	cw := &ConfigWatcher{rootConfigPath: configDir, menuExecutor: &menu.MenuExecutor{FileMgr: fileMgr}, userMgr: um}
	if err := cw.applyFileAreas(); err == nil {
		t.Error("applyFileAreas reported success with the list unreadable")
	}
	if rec, _ := fileMgr.GetFileRecordByID(id); rec.UploadedByID != 0 {
		t.Fatalf("bound to %d with the list unreadable, want left unbound", rec.UploadedByID)
	}
	if err := os.Remove(retired); err != nil {
		t.Fatal(err)
	}
	if err := bindUploaders(um, fileMgr); err != nil {
		t.Fatal(err)
	}
	if rec, _ := fileMgr.GetFileRecordByID(id); rec.UploadedByID != ann.ID {
		t.Errorf("after the list was fixed, uploader = %d, want #%d", rec.UploadedByID, ann.ID)
	}
}
