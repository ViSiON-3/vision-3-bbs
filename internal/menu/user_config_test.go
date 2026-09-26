package menu

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// newUserConfigTestUser builds a real *user.UserMgr over t.TempDir() with a
// single user, mirroring the real-manager pattern used elsewhere (see
// internal/menu/file_lightbar_test.go, internal/menu/message_list_test.go).
func newUserConfigTestUser(t *testing.T) (*user.UserMgr, *user.User) {
	t.Helper()
	um, err := user.NewUserManager(t.TempDir())
	if err != nil {
		t.Fatalf("NewUserManager: %v", err)
	}
	u, err := um.AddUser("password", "Tester", "Real Name", "Loc")
	if err != nil {
		t.Fatalf("AddUser: %v", err)
	}
	return um, u
}

// breakUserStore makes saving users.json fail, by removing write permission
// from the directory rather than from the file.
//
// The file itself is no longer enough. The BBS writes users.json atomically:
// a fresh temp file in the same directory, renamed into place. Rename replaces
// the directory entry and does not need write permission on the file it
// replaces, so a read-only users.json is happily overwritten. Taking away
// write permission on the directory blocks creating the temp file, which is
// where an atomic write actually fails.
func breakUserStore(t *testing.T, dir string) {
	t.Helper()
	// Windows does not honour a POSIX mode here, so the directory stays
	// writable and the save under test would succeed. Skipping is honest;
	// silently passing a test that never exercised the failure path is not.
	if runtime.GOOS == "windows" {
		t.Skip("cannot make a directory unwritable with os.Chmod on Windows")
	}
	if _, err := os.Stat(filepath.Join(dir, "users.json")); err != nil {
		t.Fatalf("stat users.json before breaking it: %v", err)
	}
	// Restore write permission whatever happens, or t.TempDir cannot clean up.
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatalf("chmod data dir: %v", err)
	}
}

// reloadPersistedUser restores write access to the data directory and opens a fresh
// *user.UserMgr over the same data directory, so the returned user reflects
// what genuinely made it to disk. A second GetUser on the ORIGINAL manager
// would not prove this: UserMgr.UpdateUser writes its in-memory map entry
// before attempting the save and does not roll that back on error (unlike
// UpdateUserByID, which does), so the original manager's cache would show the
// failed-to-persist value. Reloading from disk sidesteps that unrelated,
// pre-existing gap in internal/user and tests only what the caller controls.
func reloadPersistedUser(t *testing.T, dir, handle string) *user.User {
	t.Helper()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatalf("restore data dir permissions: %v", err)
	}
	reloaded, err := user.NewUserManager(dir)
	if err != nil {
		t.Fatalf("reload NewUserManager: %v", err)
	}
	persisted, ok := reloaded.GetUser(handle)
	if !ok {
		t.Fatalf("user %q not found after reload", handle)
	}
	return persisted
}

// A failed save must leave the session's copy of the user matching what is
// still on disk, and tell the caller.
func TestKonfigSaveFailureRollsBack(t *testing.T) {
	dir := t.TempDir()
	um, err := user.NewUserManager(dir)
	if err != nil {
		t.Fatalf("NewUserManager: %v", err)
	}
	u, err := um.AddUser("password", "Tester", "Real Name", "Loc")
	if err != nil {
		t.Fatalf("AddUser: %v", err)
	}
	breakUserStore(t, dir)

	// Hot Keys (a switch) and Screen Width (a typed value).
	screen, got, _ := runKonfig(t, um, u, "da"+keyClear+"132\rq", "")
	if got.HotKeys || got.ScreenWidth == 132 {
		t.Errorf("in memory: HotKeys=%v ScreenWidth=%d, want both rolled back", got.HotKeys, got.ScreenWidth)
	}
	if !strings.Contains(screen.Row(konfigEditRow), "Couldn't save Screen Width") {
		t.Errorf("status row = %q", screen.Row(konfigEditRow))
	}
	persisted := reloadPersistedUser(t, dir, "Tester")
	if persisted.HotKeys || persisted.ScreenWidth == 132 {
		t.Error("a failed save reached disk")
	}
}

func TestFileListModeDisplay(t *testing.T) {
	tests := []struct {
		name string
		mode string
		want string
	}{
		{"lowercase classic", "classic", "Classic"},
		{"mixed case classic", "Classic", "Classic"},
		{"uppercase classic", "CLASSIC", "Classic"},
		{"lightbar", "lightbar", "Lightbar"},
		{"unknown value", "grid", "Lightbar"},
		{"empty string", "", "Lightbar"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := fileListModeDisplay(tt.mode); got != tt.want {
				t.Errorf("fileListModeDisplay(%q) = %q, want %q", tt.mode, got, tt.want)
			}
		})
	}
}
