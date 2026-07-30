package menu

import (
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
