package user

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/filelock"
)

// mgrAt builds a manager backed by a users.json at the given path, already
// written out so the fingerprint starts in step with the file.
func mgrAt(t *testing.T, path string, u *User) *UserMgr {
	t.Helper()
	um := NewUserMgrForTest(u)
	um.path = path
	if err := um.SaveUsers(); err != nil {
		t.Fatalf("seed SaveUsers: %v", err)
	}
	return um
}

// A save must wait for whoever holds the lock rather than charging through the
// read-merge-write while ./ue is midway through its own.
func TestSaveWaitsForTheCrossProcessLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "users.json")
	um := mgrAt(t, path, &User{ID: 1, Handle: "Alice", AccessLevel: 10})

	lock, err := filelock.Acquire(path, time.Second)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}

	released := make(chan struct{})
	go func() {
		time.Sleep(120 * time.Millisecond)
		lock.Release()
		close(released)
	}()

	start := time.Now()
	if err := um.SaveUsers(); err != nil {
		t.Fatalf("SaveUsers: %v", err)
	}
	<-released
	if waited := time.Since(start); waited < 100*time.Millisecond {
		t.Errorf("save completed in %s without waiting for the lock holder", waited)
	}
}

// The whole point of the lock: an edit written by another process after our
// fingerprint check but before our write must not be lost. Holding the lock
// while making that edit forces the save to see it.
func TestSaveDoesNotClobberAnEditMadeUnderTheLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "users.json")
	um := mgrAt(t, path, &User{ID: 1, Handle: "Alice", AccessLevel: 10})

	lock, err := filelock.Acquire(path, time.Second)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	// Stand in for ./ue: promote Alice on disk while holding the lock.
	writeUsersJSON(t, path, []*User{{ID: 1, Handle: "Alice", AccessLevel: 255}})

	done := make(chan error, 1)
	go func() { done <- um.SaveUsers() }()

	time.Sleep(80 * time.Millisecond) // let the save block on the lock
	lock.Release()

	if err := <-done; err != nil {
		t.Fatalf("SaveUsers: %v", err)
	}

	onDisk := readUsersJSON(t, path)
	if len(onDisk) != 1 || onDisk[0].AccessLevel != 255 {
		t.Errorf("the external promotion was overwritten: %+v", onDisk)
	}
}

// A crash or a concurrent reader must never see a half-written users.json.
// os.WriteFile truncates and then writes, so a reader landing in that window
// sees an empty or partial file; a rename cannot be observed partway through.
// Reading continuously across many saves is what tells the two apart.
func TestSaveIsNeverObservedPartiallyWritten(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "users.json")
	um := mgrAt(t, path, &User{ID: 1, Handle: "Alice", AccessLevel: 10})

	stop := make(chan struct{})
	bad := make(chan string, 1)
	go func() {
		for {
			select {
			case <-stop:
				return
			default:
			}
			data, err := os.ReadFile(path)
			if err != nil {
				continue // the file is only ever replaced, never absent for long
			}
			var out []*User
			if err := json.Unmarshal(data, &out); err != nil {
				select {
				case bad <- "reader saw a partially written file: " + err.Error():
				default:
				}
				return
			}
			if len(out) == 0 {
				select {
				case bad <- "reader saw an empty user list mid-save":
				default:
				}
				return
			}
		}
	}()

	for i := range 200 {
		um.mu.Lock()
		um.users["alice"].AccessLevel = i % 256
		um.mu.Unlock()
		if err := um.SaveUsers(); err != nil {
			close(stop)
			t.Fatalf("SaveUsers: %v", err)
		}
	}
	close(stop)

	select {
	case msg := <-bad:
		t.Error(msg)
	default:
	}

	// No temp files left behind; the lock sidecar is expected to stay.
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		switch e.Name() {
		case "users.json", "users.json.lock":
		default:
			t.Errorf("leftover file after save: %s", e.Name())
		}
	}
}

func writeUsersJSON(t *testing.T, path string, users []*User) {
	t.Helper()
	data, err := json.MarshalIndent(users, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func readUsersJSON(t *testing.T, path string) []*User {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var out []*User
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return out
}
