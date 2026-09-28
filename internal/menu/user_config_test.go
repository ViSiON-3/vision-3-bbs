package menu

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"

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

// setCallerPassword gives env's caller a stored password hash.
func setCallerPassword(t *testing.T, env *menuEnv, pw string) {
	t.Helper()
	h, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	env.caller.PasswordHash = string(h)
	if err := env.um.UpdateUser(env.caller); err != nil {
		t.Fatal(err)
	}
}

// TestCfgPasswordChangesAfterVerifying pins CFG_PASSWORD: a wrong current
// password changes nothing; the right one, then a matching new pair, saves
// a hash that verifies against the new password and not the old.
func TestCfgPasswordChangesAfterVerifying(t *testing.T) {
	env := newMenuEnv(t)
	setCallerPassword(t, env, "oldpass")

	r := env.runCmd("CFG_PASSWORD", env.caller, "", "nope\r")
	if !strings.Contains(r.text(), stripPipes(env.e.Strings().CfgIncorrectPw)) {
		t.Errorf("no incorrect-password notice:\n%s", r.text())
	}
	if bcrypt.CompareHashAndPassword([]byte(env.mustDiskUser(2).PasswordHash), []byte("oldpass")) != nil {
		t.Fatal("wrong current password changed the hash")
	}

	// A too-short and a mismatched attempt are re-prompted before success.
	r = env.runCmd("CFG_PASSWORD", env.caller, "", "oldpass\rab\rnewpass1\rnewpass2\rnewpass1\rnewpass1\r")
	if r.has("newpass1") {
		t.Error("password echoed")
	}
	saved := env.mustDiskUser(2).PasswordHash
	if bcrypt.CompareHashAndPassword([]byte(saved), []byte("newpass1")) != nil {
		t.Fatalf("new password does not verify\n%s", r.text())
	}
	if bcrypt.CompareHashAndPassword([]byte(saved), []byte("oldpass")) == nil {
		t.Fatal("old password still verifies")
	}
	if !strings.Contains(r.text(), stripPipes(env.e.Strings().CfgPasswordChanged)) {
		t.Errorf("no changed notice:\n%s", r.text())
	}
}

// TestCfgPasswordDisconnects pins that dropping carrier at either prompt
// logs off without changing the password, and no user is a no-op.
func TestCfgPasswordDisconnects(t *testing.T) {
	env := newMenuEnv(t)
	setCallerPassword(t, env, "oldpass")
	for _, in := range []string{"", "oldpass\r", "oldpass\rnewpass1\r"} {
		if r := env.runCmd("CFG_PASSWORD", env.caller, "", in); r.next != "LOGOFF" {
			t.Errorf("input %q: next = %q, want LOGOFF", in, r.next)
		}
	}
	if bcrypt.CompareHashAndPassword([]byte(env.mustDiskUser(2).PasswordHash), []byte("oldpass")) != nil {
		t.Error("password changed by an interrupted run")
	}
	if r := env.runCmd("CFG_PASSWORD", nil, "", "x\r"); r.raw != "" {
		t.Errorf("no user printed:\n%s", r.text())
	}
}

// TestCfgAutoSigCreateAndDelete pins CFG_AUTOSIG: C opens the editor and
// saves the typed signature; D deletes it; D with none says so.
func TestCfgAutoSigCreateAndDelete(t *testing.T) {
	env := newMenuEnv(t)

	r := env.runCmd("CFG_AUTOSIG", env.caller, "", "D\rC\r-- Carl\x1aQ\r")
	if !r.has("You don't have an Auto-Signature to delete!", "Auto-Signature saved!") {
		t.Errorf("notices missing:\n%s", r.text())
	}
	if got := env.mustDiskUser(2).AutoSignature; got != "-- Carl" {
		t.Fatalf("saved sig = %q", got)
	}

	r = env.runCmd("CFG_AUTOSIG", env.caller, "", "D\r\r")
	if !r.has("Your current Auto-Signature is...", "-- Carl", "Auto-Signature has been deleted.") {
		t.Errorf("delete flow:\n%s", r.text())
	}
	if got := env.mustDiskUser(2).AutoSignature; got != "" {
		t.Errorf("sig = %q after delete", got)
	}
}

// TestCfgAutoSigTruncatesAndAbandons pins that CFG_AUTOSIG cuts a signature
// to five lines and says so, that an abandoned edit keeps the old one, and
// that a disconnect at the menu logs off.
func TestCfgAutoSigTruncatesAndAbandons(t *testing.T) {
	env := newMenuEnv(t)
	r := env.runCmd("CFG_AUTOSIG", env.caller, "", "C\ra\rb\rc\rd\re\rf\x1aQ\r")
	if !r.has("Signature truncated to 5 lines.") {
		t.Errorf("no truncation notice:\n%s", r.text())
	}
	if got := env.mustDiskUser(2).AutoSignature; got != "a\nb\nc\nd\ne" {
		t.Fatalf("saved sig = %q", got)
	}
	r = env.runCmd("CFG_AUTOSIG", env.caller, "", "C\rXX\x01YQ\r")
	if !r.has("Auto-Signature not changed.") || env.mustDiskUser(2).AutoSignature != "a\nb\nc\nd\ne" {
		t.Errorf("abandoned edit:\n%s", r.text())
	}
	if r := env.runCmd("CFG_AUTOSIG", env.caller, "", ""); r.next != "LOGOFF" {
		t.Errorf("disconnect: next = %q", r.next)
	}
}

// TestCfgFileColumnsTogglesAndSaves pins CFG_FILECOLUMNS: from the all-on
// default, toggling a column turns the rest on explicitly and that one off,
// and Q saves the set.
func TestCfgFileColumnsTogglesAndSaves(t *testing.T) {
	env := newMenuEnv(t)
	r := env.runCmd("CFG_FILECOLUMNS", env.caller, "", "L\rU\rS\rS\rQ\r")
	if !strings.Contains(r.text(), stripPipes(env.e.Strings().CfgFileColumnsSaved)) {
		t.Errorf("no saved notice:\n%s", r.text())
	}
	c := env.mustDiskUser(2).FileListColumns
	if !c.Name || !c.Size || !c.Date || c.Downloads || c.Uploader || !c.Description {
		t.Errorf("columns = %+v, want Downloads and Uploader off", c)
	}
	env.runCmd("CFG_FILECOLUMNS", env.caller, "", "N\rD\rE\r\r")
	c = env.mustDiskUser(2).FileListColumns
	if c.Name || !c.Size || c.Date || c.Description {
		t.Errorf("second pass columns = %+v", c)
	}
	if r := env.runCmd("CFG_FILECOLUMNS", env.caller, "", "N\r"); r.next != "LOGOFF" {
		t.Errorf("disconnect: next = %q", r.next)
	}
}
