package menu

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
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

var fixedCfgTestTime = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

// --- runCfgToggle ---

func TestRunCfgToggle_FlipsAndPersists(t *testing.T) {
	um, u := newUserConfigTestUser(t)
	if u.HotKeys {
		t.Fatal("expected HotKeys to default false")
	}

	e := &MenuExecutor{}
	ts := newTestSession("")
	terminal := newTestTerminal(ts)

	getter := func(u *user.User) bool { return u.HotKeys }
	setter := func(u *user.User, v bool) { u.HotKeys = v }

	returned, action, err := runCfgToggle(e, ts, terminal, um, u, 1, fixedCfgTestTime, "",
		ansi.OutputModeUTF8, "Hot Keys", getter, setter)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if action != "" {
		t.Errorf("action = %q, want empty", action)
	}
	if returned == nil {
		t.Fatal("returned user is nil")
	}
	if !returned.HotKeys {
		t.Error("returned user's HotKeys not flipped to true")
	}

	persisted, ok := um.GetUser("Tester")
	if !ok {
		t.Fatal("user not found after toggle")
	}
	if !persisted.HotKeys {
		t.Error("HotKeys not persisted as true")
	}

	// Toggling again flips it back.
	returned2, _, err := runCfgToggle(e, ts, terminal, um, returned, 1, fixedCfgTestTime, "",
		ansi.OutputModeUTF8, "Hot Keys", getter, setter)
	if err != nil {
		t.Fatalf("unexpected error on second toggle: %v", err)
	}
	if returned2.HotKeys {
		t.Error("expected HotKeys to flip back to false")
	}
	persisted2, ok := um.GetUser("Tester")
	if !ok {
		t.Fatal("user not found after second toggle")
	}
	if persisted2.HotKeys {
		t.Error("HotKeys not persisted as false after second toggle")
	}
}

func TestRunCfgToggle_NilUserReturnsNilWithoutPanic(t *testing.T) {
	um, _ := newUserConfigTestUser(t)
	e := &MenuExecutor{}
	ts := newTestSession("")
	terminal := newTestTerminal(ts)

	getter := func(u *user.User) bool { return u.HotKeys }
	setter := func(u *user.User, v bool) { u.HotKeys = v }

	returned, action, err := runCfgToggle(e, ts, terminal, um, nil, 1, fixedCfgTestTime, "",
		ansi.OutputModeUTF8, "Hot Keys", getter, setter)
	if returned != nil {
		t.Errorf("returned = %v, want nil", returned)
	}
	if action != "" {
		t.Errorf("action = %q, want empty", action)
	}
	if err != nil {
		t.Errorf("err = %v, want nil", err)
	}
}

// --- UpdateUser failure rollback ---
//
// These two tests force a genuine UpdateUser save failure (permission denied
// on users.json) to characterize the rollback fix: on save failure, the
// in-memory currentUser must revert to its pre-mutation value, matching what
// is genuinely still on disk.
//
// breakUserStore makes users.json read-only so a subsequent UpdateUser call
// fails with a real write error, and schedules cleanup to restore write
// access so t.TempDir() can remove it afterward. Chmod-ing the *directory*
// does not work for this: an existing file can still be truncated and
// rewritten by its owner as long as the directory permits opening it --
// os.WriteFile only needs write access to the file itself, not create/rename
// access to its parent directory. Verified empirically before writing these
// tests (see the report).
func breakUserStore(t *testing.T, dir string) {
	t.Helper()
	usersFile := filepath.Join(dir, "users.json")
	if _, err := os.Stat(usersFile); err != nil {
		t.Fatalf("stat users.json before breaking it: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(usersFile, 0o600) })
	if err := os.Chmod(usersFile, 0o400); err != nil {
		t.Fatalf("chmod users.json: %v", err)
	}
}

// reloadPersistedUser restores write access to users.json and opens a fresh
// *user.UserMgr over the same data directory, so the returned user reflects
// what genuinely made it to disk. A second GetUser on the ORIGINAL manager
// would not prove this: UserMgr.UpdateUser writes its in-memory map entry
// before attempting the save and does not roll that back on error (unlike
// UpdateUserByID, which does), so the original manager's cache would show the
// failed-to-persist value. Reloading from disk sidesteps that unrelated,
// pre-existing gap in internal/user and tests only what this fix controls.
func reloadPersistedUser(t *testing.T, dir, handle string) *user.User {
	t.Helper()
	if err := os.Chmod(filepath.Join(dir, "users.json"), 0o600); err != nil {
		t.Fatalf("restore users.json permissions: %v", err)
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

func TestRunCfgToggle_UpdateFailureRollsBackAndPreservesStore(t *testing.T) {
	dir := t.TempDir()
	um, err := user.NewUserManager(dir)
	if err != nil {
		t.Fatalf("NewUserManager: %v", err)
	}
	u, err := um.AddUser("password", "Tester", "Real Name", "Loc")
	if err != nil {
		t.Fatalf("AddUser: %v", err)
	}
	if u.HotKeys {
		t.Fatal("expected HotKeys to default false")
	}
	breakUserStore(t, dir)

	e := &MenuExecutor{}
	ts := newTestSession("")
	terminal := newTestTerminal(ts)
	getter := func(u *user.User) bool { return u.HotKeys }
	setter := func(u *user.User, v bool) { u.HotKeys = v }

	returned, action, err := runCfgToggle(e, ts, terminal, um, u, 1, fixedCfgTestTime, "",
		ansi.OutputModeUTF8, "Hot Keys", getter, setter)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if action != "" {
		t.Errorf("action = %q, want empty", action)
	}
	if returned.HotKeys {
		t.Error("HotKeys should have rolled back to false in memory after the save failed")
	}

	persisted := reloadPersistedUser(t, dir, "Tester")
	if persisted.HotKeys {
		t.Error("store should still hold HotKeys=false; the failed write must not have reached disk")
	}
}

func TestRunCfgScreenWidth_UpdateFailureRollsBackAndPreservesStore(t *testing.T) {
	dir := t.TempDir()
	um, err := user.NewUserManager(dir)
	if err != nil {
		t.Fatalf("NewUserManager: %v", err)
	}
	u, err := um.AddUser("password", "Tester", "Real Name", "Loc")
	if err != nil {
		t.Fatalf("AddUser: %v", err)
	}
	originalWidth := u.ScreenWidth
	breakUserStore(t, dir)

	e := &MenuExecutor{}
	ts := newTestSession("100\r")
	terminal := newTestTerminal(ts)
	c := &cmdCtx{
		e: e, s: ts, terminal: terminal, userManager: um, currentUser: u,
		nodeNumber: 1, outputMode: ansi.OutputModeUTF8,
	}

	returned, action, err := runCfgScreenWidth(c, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if action != "" {
		t.Errorf("action = %q, want empty", action)
	}
	if returned.ScreenWidth != originalWidth {
		t.Errorf("ScreenWidth = %d, want rolled back to %d in memory after the save failed", returned.ScreenWidth, originalWidth)
	}

	persisted := reloadPersistedUser(t, dir, "Tester")
	if persisted.ScreenWidth != originalWidth {
		t.Errorf("store ScreenWidth = %d, want unchanged %d; the failed write must not have reached disk", persisted.ScreenWidth, originalWidth)
	}
}

// --- runCfgStringInput ---

func TestRunCfgStringInput_StoresTypedValue(t *testing.T) {
	um, u := newUserConfigTestUser(t)
	e := &MenuExecutor{}
	ts := newTestSession("Robbie Whiting\r")
	terminal := newTestTerminal(ts)

	getter := func(u *user.User) string { return u.RealName }
	setter := func(u *user.User, v string) { u.RealName = v }

	returned, _, err := runCfgStringInput(e, ts, terminal, um, u, 1, ansi.OutputModeUTF8,
		"Real Name", 40, getter, setter)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if returned.RealName != "Robbie Whiting" {
		t.Errorf("RealName = %q, want %q", returned.RealName, "Robbie Whiting")
	}

	persisted, ok := um.GetUser("Tester")
	if !ok {
		t.Fatal("user not found after string input")
	}
	if persisted.RealName != "Robbie Whiting" {
		t.Errorf("persisted RealName = %q, want %q", persisted.RealName, "Robbie Whiting")
	}
}

func TestRunCfgStringInput_EmptyInputLeavesValueUnchanged(t *testing.T) {
	um, u := newUserConfigTestUser(t)
	u.RealName = "Original Name"
	if err := um.UpdateUser(u); err != nil {
		t.Fatalf("seed UpdateUser: %v", err)
	}

	e := &MenuExecutor{}
	// Just Enter with no other input: readLineFromSessionIH returns "".
	ts := newTestSession("\r")
	terminal := newTestTerminal(ts)

	getter := func(u *user.User) string { return u.RealName }
	setter := func(u *user.User, v string) { u.RealName = v }

	returned, _, err := runCfgStringInput(e, ts, terminal, um, u, 1, ansi.OutputModeUTF8,
		"Real Name", 40, getter, setter)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if returned.RealName != "Original Name" {
		t.Errorf("RealName = %q, want unchanged %q", returned.RealName, "Original Name")
	}

	persisted, ok := um.GetUser("Tester")
	if !ok {
		t.Fatal("user not found")
	}
	if persisted.RealName != "Original Name" {
		t.Errorf("persisted RealName = %q, want unchanged %q", persisted.RealName, "Original Name")
	}
}

func TestRunCfgStringInput_TrimsSurroundingWhitespace(t *testing.T) {
	um, u := newUserConfigTestUser(t)
	e := &MenuExecutor{}
	ts := newTestSession("  spaced text  \r")
	terminal := newTestTerminal(ts)

	getter := func(u *user.User) string { return u.PrivateNote }
	setter := func(u *user.User, v string) { u.PrivateNote = v }

	returned, _, err := runCfgStringInput(e, ts, terminal, um, u, 1, ansi.OutputModeUTF8,
		"User Note", 35, getter, setter)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if returned.PrivateNote != "spaced text" {
		t.Errorf("PrivateNote = %q, want %q", returned.PrivateNote, "spaced text")
	}
}

func TestRunCfgStringInput_TruncatesToMaxLen(t *testing.T) {
	um, u := newUserConfigTestUser(t)
	e := &MenuExecutor{}
	ts := newTestSession("abcdefgh\r")
	terminal := newTestTerminal(ts)

	getter := func(u *user.User) string { return u.PrivateNote }
	setter := func(u *user.User, v string) { u.PrivateNote = v }

	returned, _, err := runCfgStringInput(e, ts, terminal, um, u, 1, ansi.OutputModeUTF8,
		"User Note", 5, getter, setter)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if returned.PrivateNote != "abcde" {
		t.Errorf("PrivateNote = %q, want %q (truncated to maxLen 5)", returned.PrivateNote, "abcde")
	}
}

// --- fileListModeDisplay ---

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
