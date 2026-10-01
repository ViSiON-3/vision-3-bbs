package user

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

func TestWFCReadOnlyOmittedWhenOff(t *testing.T) {
	data, err := json.Marshal(&User{Handle: "boss"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "wfcReadOnly") {
		t.Fatalf("an unset flag reached users.json: %s", data)
	}
	var u User
	if err := json.Unmarshal([]byte(`{"handle":"boss","wfcReadOnly":true}`), &u); err != nil || !u.WFCReadOnly {
		t.Fatalf("wfcReadOnly not read back: %+v %v", u, err)
	}
}

// ./ue sets the flag while the BBS runs; the next BBS save must keep it.
func TestExternalWFCReadOnlyEditSurvivesSave(t *testing.T) {
	um, path := mgrWithUser(t, &User{ID: 1, Handle: "Boss", AccessLevel: 255})
	writeUsersFile(t, path, &User{ID: 1, Handle: "Boss", AccessLevel: 255, WFCReadOnly: true})
	if err := um.SaveUsers(); err != nil {
		t.Fatal(err)
	}
	if !readUsersFile(t, path)["boss"].WFCReadOnly {
		t.Fatal("the sysop's read-only setting was overwritten")
	}
	if u, _ := um.GetUser("boss"); !u.WFCReadOnly {
		t.Fatal("the running BBS did not take the read-only setting")
	}
}

// SyncFromDisk folds a ./ue edit into the running manager without a save.
func TestSyncFromDiskTakesExternalEdit(t *testing.T) {
	um, path := mgrWithUser(t, &User{ID: 1, Handle: "Boss", AccessLevel: 255})
	writeUsersFile(t, path, &User{ID: 1, Handle: "Boss", AccessLevel: 10, WFCReadOnly: true})
	sessionRefresh.mu.Lock()
	sessionRefresh.lastSync = time.Time{}
	sessionRefresh.mu.Unlock()
	um.SyncFromDisk()
	u, _ := um.GetUser("boss")
	if u.AccessLevel != 10 || !u.WFCReadOnly {
		t.Fatalf("edit not taken: level %d readOnly %v", u.AccessLevel, u.WFCReadOnly)
	}
}

// A manager with no users.json, as tests build, has nothing to sync and
// must not touch the working directory.
func TestSyncFromDiskWithoutAFile(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	sessionRefresh.mu.Lock()
	sessionRefresh.lastSync = time.Time{}
	sessionRefresh.mu.Unlock()
	um := NewUserMgrForTest(&User{Handle: "boss", AccessLevel: 255})
	um.SyncFromDisk()
	if u, ok := um.GetUser("boss"); !ok || u.AccessLevel != 255 {
		t.Fatal("in-memory user lost")
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("sync created %v", entries)
	}
	sessionRefresh.mu.Lock()
	synced := !sessionRefresh.lastSync.IsZero()
	sessionRefresh.mu.Unlock()
	if synced {
		t.Fatal("a manager with no file used up the sync interval")
	}
}
