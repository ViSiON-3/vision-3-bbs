package user

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeUsersFile(t *testing.T, path string, users ...*User) {
	t.Helper()
	data, err := json.MarshalIndent(users, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func readUsersFile(t *testing.T, path string) map[string]*User {
	t.Helper()
	m, err := readUsersFromDisk(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	return m
}

// mgrWithUser builds a manager whose in-memory map and on-disk file agree,
// with the mtime recorded as loadUsers would.
func mgrWithUser(t *testing.T, u *User) (*UserMgr, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "users.json")
	writeUsersFile(t, path, u)

	cp := *u
	cp.persisted = true
	um := NewUserMgrForTest(&cp)
	um.path = path
	um.fileState = fingerprintOf(path)
	return um, path
}

// The reported bug: a sysop demotes a user in ./ue while the BBS is running,
// and the next save reverts it.
func TestExternalEditSurvivesSave(t *testing.T) {
	um, path := mgrWithUser(t, &User{
		ID: 1, Handle: "Felonius", AccessLevel: 255, Validated: true,
		GroupLocation: "ViSiON/3", TimesCalled: 11,
	})

	// ./ue writes the file directly while the BBS holds its own copy.
	writeUsersFile(t, path, &User{
		ID: 1, Handle: "Felonius", AccessLevel: 100, Validated: false,
		GroupLocation: "EDITED-BY-UE", TimesCalled: 11,
	})

	// The BBS then saves, as any login would.
	if err := um.SaveUsers(); err != nil {
		t.Fatalf("SaveUsers: %v", err)
	}

	got := readUsersFile(t, path)["felonius"]
	if got.AccessLevel != 100 {
		t.Errorf("AccessLevel = %d, want 100 — the sysop's edit was overwritten", got.AccessLevel)
	}
	if got.Validated {
		t.Error("Validated = true, want false — the sysop's edit was overwritten")
	}
	if got.GroupLocation != "EDITED-BY-UE" {
		t.Errorf("GroupLocation = %q, want the edited value", got.GroupLocation)
	}
}

// Session-owned state must not be rolled back by the merge.
func TestSessionStateSurvivesExternalEdit(t *testing.T) {
	um, path := mgrWithUser(t, &User{ID: 1, Handle: "Felonius", AccessLevel: 255})

	// The session records login activity in memory.
	loginAt := time.Date(2026, 9, 5, 10, 0, 0, 0, time.UTC)
	prev := time.Date(2026, 9, 4, 10, 0, 0, 0, time.UTC)
	um.mu.Lock()
	u := um.users["felonius"]
	u.LastLogin = loginAt
	u.PreviousLogin = prev
	u.SeenNewsIDs = []int{1, 2, 5}
	u.CurrentMessageAreaTag = "GENERAL"
	um.mu.Unlock()

	// Meanwhile the sysop demotes them on disk.
	writeUsersFile(t, path, &User{ID: 1, Handle: "Felonius", AccessLevel: 20})

	if err := um.SaveUsers(); err != nil {
		t.Fatalf("SaveUsers: %v", err)
	}

	got := readUsersFile(t, path)["felonius"]
	if got.AccessLevel != 20 {
		t.Errorf("AccessLevel = %d, want 20 (sysop field from disk)", got.AccessLevel)
	}
	if !got.LastLogin.Equal(loginAt) {
		t.Errorf("LastLogin = %v, want %v (session field from memory)", got.LastLogin, loginAt)
	}
	if !got.PreviousLogin.Equal(prev) {
		t.Errorf("PreviousLogin = %v, want %v", got.PreviousLogin, prev)
	}
	if len(got.SeenNewsIDs) != 3 {
		t.Errorf("SeenNewsIDs = %v, want the session's [1 2 5]", got.SeenNewsIDs)
	}
	if got.CurrentMessageAreaTag != "GENERAL" {
		t.Errorf("CurrentMessageAreaTag = %q, want GENERAL", got.CurrentMessageAreaTag)
	}
}

// A user added in ./ue while the BBS runs must not be erased by the next save.
func TestUserAddedExternallyIsAdopted(t *testing.T) {
	um, path := mgrWithUser(t, &User{ID: 1, Handle: "Felonius", AccessLevel: 255})

	writeUsersFile(t, path,
		&User{ID: 1, Handle: "Felonius", AccessLevel: 255},
		&User{ID: 2, Handle: "Newbie", AccessLevel: 10},
	)

	if err := um.SaveUsers(); err != nil {
		t.Fatalf("SaveUsers: %v", err)
	}

	got := readUsersFile(t, path)
	if _, ok := got["newbie"]; !ok {
		t.Error("externally added user was erased by the save")
	}
	if len(got) != 2 {
		t.Errorf("expected 2 users on disk, got %d", len(got))
	}
}

// A user deleted in ./ue must stay deleted rather than being resurrected.
func TestUserDeletedExternallyIsNotResurrected(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "users.json")
	writeUsersFile(t, path,
		&User{ID: 1, Handle: "Felonius", AccessLevel: 255},
		&User{ID: 2, Handle: "Doomed", AccessLevel: 10},
	)
	um := NewUserMgrForTest(
		&User{ID: 1, Handle: "Felonius", AccessLevel: 255, persisted: true},
		&User{ID: 2, Handle: "Doomed", AccessLevel: 10, persisted: true},
	)
	um.path = path
	um.fileState = fingerprintOf(path)

	writeUsersFile(t, path, &User{ID: 1, Handle: "Felonius", AccessLevel: 255})

	if err := um.SaveUsers(); err != nil {
		t.Fatalf("SaveUsers: %v", err)
	}

	if _, ok := readUsersFile(t, path)["doomed"]; ok {
		t.Error("externally deleted user was resurrected by the save")
	}
}

// With no external edit, a save must not re-read or alter anything.
func TestNoExternalEditLeavesMemoryAuthoritative(t *testing.T) {
	um, path := mgrWithUser(t, &User{ID: 1, Handle: "Felonius", AccessLevel: 255})

	um.mu.Lock()
	um.users["felonius"].AccessLevel = 200 // a legitimate in-session change
	um.mu.Unlock()

	if um.externallyModified() {
		t.Fatal("file reported as externally modified when nothing touched it")
	}
	if err := um.SaveUsers(); err != nil {
		t.Fatalf("SaveUsers: %v", err)
	}
	if got := readUsersFile(t, path)["felonius"].AccessLevel; got != 200 {
		t.Errorf("AccessLevel = %d, want 200 — in-session change should persist", got)
	}
}

// Our own write must not look like somebody else's edit on the next save.
func TestSaveUpdatesRecordedMtime(t *testing.T) {
	um, _ := mgrWithUser(t, &User{ID: 1, Handle: "Felonius", AccessLevel: 255})

	if err := um.SaveUsers(); err != nil {
		t.Fatalf("SaveUsers: %v", err)
	}
	if um.externallyModified() {
		t.Error("manager thinks its own write was an external edit")
	}
}

// A corrupt file must not cost the session its state.
func TestMergeSkippedWhenFileUnreadable(t *testing.T) {
	um, path := mgrWithUser(t, &User{ID: 1, Handle: "Felonius", AccessLevel: 255})

	if err := os.WriteFile(path, []byte("{not json"), 0600); err != nil {
		t.Fatalf("write: %v", err)
	}

	if err := um.SaveUsers(); err != nil {
		t.Fatalf("SaveUsers: %v", err)
	}
	got := readUsersFile(t, path)
	if _, ok := got["felonius"]; !ok {
		t.Error("session state lost when the on-disk file was corrupt")
	}
}

// The case the first version of this fix missed, caught only by running a
// real login: the save path merges the external edit correctly, and then a
// later UpdateUser writes back a copy the session took *before* the merge.
// Because the manager itself made the most recent write, no external change is
// detected the second time and the sysop's edit is reverted.
func TestStaleSessionCopyCannotRevertExternalEdit(t *testing.T) {
	um, path := mgrWithUser(t, &User{
		ID: 1, Handle: "Felonius", AccessLevel: 255, Validated: true,
		GroupLocation: "ViSiON/3",
	})

	// A session takes its copy, as Authenticate hands out.
	sessionCopy, ok := um.GetUser("Felonius")
	if !ok {
		t.Fatal("GetUser failed")
	}

	// The sysop demotes them in ./ue.
	writeUsersFile(t, path, &User{
		ID: 1, Handle: "Felonius", AccessLevel: 100, Validated: false,
		GroupLocation: "EDITED-BY-UE",
	})

	// First save folds the edit in (this is what Authenticate's save does).
	if err := um.SaveUsers(); err != nil {
		t.Fatalf("first SaveUsers: %v", err)
	}
	if got := readUsersFile(t, path)["felonius"].AccessLevel; got != 100 {
		t.Fatalf("merge did not apply: AccessLevel = %d", got)
	}

	// Now the session writes its pre-edit copy back, mutating only its own
	// state — exactly what the login sequence does.
	sessionCopy.LastLogin = time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	sessionCopy.SeenNewsIDs = []int{1, 2}
	if err := um.UpdateUser(sessionCopy); err != nil {
		t.Fatalf("UpdateUser: %v", err)
	}

	got := readUsersFile(t, path)["felonius"]
	if got.AccessLevel != 100 {
		t.Errorf("AccessLevel = %d, want 100 — a stale session copy reverted the sysop", got.AccessLevel)
	}
	if got.Validated {
		t.Error("Validated reverted to true by a stale session copy")
	}
	if got.GroupLocation != "EDITED-BY-UE" {
		t.Errorf("GroupLocation = %q, want the sysop's value", got.GroupLocation)
	}
	// The session's own state must still have been written.
	if len(got.SeenNewsIDs) != 2 {
		t.Errorf("SeenNewsIDs = %v, want the session's [1 2]", got.SeenNewsIDs)
	}
	if got.LastLogin.IsZero() {
		t.Error("session's LastLogin was not persisted")
	}
}

// With no external edit in play, a session copy must still be able to change
// sysop-owned fields — new user validation promotes AccessLevel, and the user
// config menu changes terminal preferences.
func TestSessionCanStillChangeSysopFieldsNormally(t *testing.T) {
	um, path := mgrWithUser(t, &User{ID: 1, Handle: "Newbie", AccessLevel: 10, Validated: false})

	u, ok := um.GetUser("Newbie")
	if !ok {
		t.Fatal("GetUser failed")
	}
	u.AccessLevel = 30 // NEWUSERVAL promotion
	u.Validated = true
	u.ScreenWidth = 132 // user config menu

	if err := um.UpdateUser(u); err != nil {
		t.Fatalf("UpdateUser: %v", err)
	}

	got := readUsersFile(t, path)["newbie"]
	if got.AccessLevel != 30 || !got.Validated || got.ScreenWidth != 132 {
		t.Errorf("legitimate session changes were discarded: level=%d validated=%v width=%d",
			got.AccessLevel, got.Validated, got.ScreenWidth)
	}
}

// Review finding: TimesCalled and FilePoints are advanced continuously by the
// BBS, so taking them from disk would discard a session's activity on any
// external edit, however unrelated to those fields.
func TestCountersStaySessionOwned(t *testing.T) {
	um, path := mgrWithUser(t, &User{
		ID: 1, Handle: "Felonius", AccessLevel: 255, TimesCalled: 11, FilePoints: 5,
	})

	// The session records a login and a transfer.
	um.mu.Lock()
	um.users["felonius"].TimesCalled = 12
	um.users["felonius"].FilePoints = 9
	um.mu.Unlock()

	// The sysop edits an unrelated field, saving the older counts alongside it.
	writeUsersFile(t, path, &User{
		ID: 1, Handle: "Felonius", AccessLevel: 100, TimesCalled: 11, FilePoints: 5,
	})

	if err := um.SaveUsers(); err != nil {
		t.Fatalf("SaveUsers: %v", err)
	}

	got := readUsersFile(t, path)["felonius"]
	if got.AccessLevel != 100 {
		t.Errorf("AccessLevel = %d, want 100 (sysop field from disk)", got.AccessLevel)
	}
	if got.TimesCalled != 12 {
		t.Errorf("TimesCalled = %d, want 12 — the session's login was discarded", got.TimesCalled)
	}
	if got.FilePoints != 9 {
		t.Errorf("FilePoints = %d, want 9 — the session's transfer was discarded", got.FilePoints)
	}
}

// Review finding: mtime is not reliable change identity. A filesystem with
// coarse resolution, or an editor writing within the same tick as our own
// write, would report an unchanged file and the merge would be skipped.
func TestChangeDetectedWhenMtimeIsUnchanged(t *testing.T) {
	um, path := mgrWithUser(t, &User{ID: 1, Handle: "Felonius", AccessLevel: 255})

	before, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}

	writeUsersFile(t, path, &User{ID: 1, Handle: "Felonius", AccessLevel: 100})
	// Force the mtime back to exactly what it was, simulating a same-tick write.
	if err := os.Chtimes(path, before.ModTime(), before.ModTime()); err != nil {
		t.Fatalf("chtimes: %v", err)
	}
	if after, _ := os.Stat(path); !after.ModTime().Equal(before.ModTime()) {
		t.Fatal("could not pin the mtime; the test would not prove anything")
	}

	if !um.externallyModified() {
		t.Fatal("change went undetected with an unchanged mtime")
	}
	if err := um.SaveUsers(); err != nil {
		t.Fatalf("SaveUsers: %v", err)
	}
	if got := readUsersFile(t, path)["felonius"].AccessLevel; got != 100 {
		t.Errorf("AccessLevel = %d, want 100 — the edit was clobbered", got)
	}
}

// Review finding: rebuilding the map purely from disk drops accounts that
// exist only in memory, losing a registration that is mid-save.
func TestInFlightRegistrationSurvivesMerge(t *testing.T) {
	um, path := mgrWithUser(t, &User{ID: 1, Handle: "Felonius", AccessLevel: 255})

	// A registration in progress: in the map, not yet on disk.
	um.mu.Lock()
	um.users["newbie"] = &User{ID: 2, Handle: "Newbie", AccessLevel: 10}
	um.mu.Unlock()

	// Meanwhile the sysop edits someone else.
	writeUsersFile(t, path, &User{ID: 1, Handle: "Felonius", AccessLevel: 100})

	if err := um.SaveUsers(); err != nil {
		t.Fatalf("SaveUsers: %v", err)
	}

	got := readUsersFile(t, path)
	if _, ok := got["newbie"]; !ok {
		t.Error("registration in flight was dropped by the merge")
	}
	if got["felonius"].AccessLevel != 100 {
		t.Error("sysop edit was lost")
	}
}

// Review finding: adopted records must advance the ID counter, or the next
// registration reuses an ID that is already taken.
func TestAdoptedUsersAdvanceNextUserID(t *testing.T) {
	um, path := mgrWithUser(t, &User{ID: 1, Handle: "Felonius", AccessLevel: 255})
	um.mu.Lock()
	um.nextUserID = 2
	um.mu.Unlock()

	// The editor adds two accounts while we were running.
	writeUsersFile(t, path,
		&User{ID: 1, Handle: "Felonius", AccessLevel: 255},
		&User{ID: 2, Handle: "Alpha", AccessLevel: 10},
		&User{ID: 7, Handle: "Beta", AccessLevel: 10},
	)

	if err := um.SaveUsers(); err != nil {
		t.Fatalf("SaveUsers: %v", err)
	}

	um.mu.RLock()
	next := um.nextUserID
	um.mu.RUnlock()
	if next <= 7 {
		t.Errorf("nextUserID = %d, want > 7 — a new registration would reuse an adopted ID", next)
	}
}

// Review finding: a legacy record keyed off "username" must have Handle
// promoted on adoption. Saving clears LegacyUsername, so without it the record
// is written back with no handle at all and skipped by the next load.
func TestAdoptedLegacyRecordKeepsItsHandle(t *testing.T) {
	um, path := mgrWithUser(t, &User{ID: 1, Handle: "Felonius", AccessLevel: 255})

	// An account written in the old shape: username set, handle absent.
	writeUsersFile(t, path,
		&User{ID: 1, Handle: "Felonius", AccessLevel: 255},
		&User{ID: 2, LegacyUsername: "OldTimer", AccessLevel: 10},
	)

	if err := um.SaveUsers(); err != nil {
		t.Fatalf("SaveUsers: %v", err)
	}

	got := readUsersFile(t, path)["oldtimer"]
	if got == nil {
		t.Fatal("legacy record was not adopted")
	}
	if got.Handle != "OldTimer" {
		t.Errorf("Handle = %q, want OldTimer — the record would be skipped on the next load", got.Handle)
	}
}

// paired returns a user with the named devices, all valid for its password.
func paired(ids ...string) *User {
	u := &User{ID: 7, Handle: "Pat", PasswordHash: "hash"}
	for _, id := range ids {
		u.APIDevices = append(u.APIDevices, APIDevice{ID: id, Name: id, PasswordStamp: PasswordStamp(u.PasswordHash)})
	}
	return u
}

func deviceIDs(devices []APIDevice) []string {
	ids := []string{}
	for _, d := range devices {
		ids = append(ids, d.ID)
	}
	return ids
}

// An unpairing made in ./ue must survive the BBS's next save, and must not
// disturb the last-use time the BBS holds for the devices that remain.
func TestExternalUnpairSurvivesSave(t *testing.T) {
	um, path := mgrWithUser(t, paired("a", "b"))
	used := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
	if err := um.TouchAPIDevice(7, "b", used); err != nil {
		t.Fatal(err)
	}
	writeUsersFile(t, path, paired("b"))

	if err := um.UpdateUser(paired()); err != nil {
		t.Fatal(err)
	}
	got, _ := um.GetUserByID(7)
	if ids := deviceIDs(got.APIDevices); len(ids) != 1 || ids[0] != "b" {
		t.Fatalf("devices in memory = %v, want [b]", ids)
	}
	if !got.APIDevices[0].LastUsed.Equal(used) {
		t.Errorf("LastUsed = %v, want the BBS's %v", got.APIDevices[0].LastUsed, used)
	}
	if ids := deviceIDs(readUsersFile(t, path)["pat"].APIDevices); len(ids) != 1 || ids[0] != "b" {
		t.Errorf("devices on disk = %v, want [b]", ids)
	}
}

// A phone paired as the first save after a ./ue edit is missing from the file
// that edit wrote. It must not be taken for an unpairing.
func TestPairingSurvivesPendingExternalEdit(t *testing.T) {
	um, path := mgrWithUser(t, paired("a"))
	edited := paired("a")
	edited.AccessLevel = 50
	writeUsersFile(t, path, edited)

	d := APIDevice{ID: "new", PublicKey: "k", PasswordStamp: PasswordStamp("hash")}
	if err := um.AddAPIDevice(7, d, 10); err != nil {
		t.Fatal(err)
	}
	onDisk := readUsersFile(t, path)["pat"]
	if ids := deviceIDs(onDisk.APIDevices); len(ids) != 2 {
		t.Errorf("devices on disk = %v, want [a new]", ids)
	}
	if onDisk.AccessLevel != 50 {
		t.Errorf("AccessLevel = %d, the ./ue edit was lost", onDisk.AccessLevel)
	}
	if um.unsavedDevices != nil {
		t.Errorf("unsavedDevices = %v after the save", um.unsavedDevices)
	}
}

// A stale copy in ./ue cannot bring back a device the BBS has since unpaired.
func TestExternalEditCannotRestoreUnpairedDevice(t *testing.T) {
	um, path := mgrWithUser(t, paired("a", "b"))
	if _, err := um.RemoveAPIDevice(7, "a"); err != nil {
		t.Fatal(err)
	}
	writeUsersFile(t, path, paired("a", "b"))

	if err := um.UpdateUser(paired()); err != nil {
		t.Fatal(err)
	}
	if got, _ := um.GetUserByID(7); len(got.APIDevices) != 1 || got.APIDevices[0].ID != "b" {
		t.Errorf("devices = %v, want [b]", deviceIDs(got.APIDevices))
	}
}

func TestShowsAPIDevices(t *testing.T) {
	stale := paired("a")
	stale.PasswordHash = "changed"
	tests := []struct {
		name    string
		u       *User
		enabled bool
		want    bool
	}{
		{"API on, no devices", paired(), true, true},
		{"API off, no devices", paired(), false, false},
		{"API off, devices left over", paired("a"), false, true},
		{"API off, only voided devices", stale, false, false},
	}
	for _, tt := range tests {
		if got := tt.u.ShowsAPIDevices(tt.enabled); got != tt.want {
			t.Errorf("%s: got %v, want %v", tt.name, got, tt.want)
		}
	}
}
