package menu

import (
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/user"
	"golang.org/x/crypto/bcrypt"
)

// Keystrokes for driving the admin user editor (ADMINLISTUSERS).
const (
	ueBS        = "\x7f"   // backspace
	ueEsc       = "\x1b"   // bare Esc when followed by a non-sequence byte
	ueArrowDown = "\x1b[B" // cursor down
	ueArrowUp   = "\x1b[A" // cursor up
)

// ueClear returns n backspaces, enough to empty a pre-filled field of n runes.
func ueClear(n int) string { return strings.Repeat(ueBS, n) }

// TestUserEditor_EditsTextFieldsAndSaves pins the field editors end to end:
// handle, real name, group, note, flags and level typed over the pre-filled
// values are all saved to disk by [S], and each field gets an audit entry
// with its old and new value.
func TestUserEditor_EditsTextFieldsAndSaves(t *testing.T) {
	env := newMenuEnv(t)
	input := "j" +
		"a" + ueClear(6) + "  Carla  \r" +
		"b" + ueClear(11) + "Carla Caller\r" +
		"c" + "Pittsburgh\r" +
		"d" + "watch this one\r" +
		"e" + "AB\r" +
		"f" + ueClear(2) + "50\r" +
		"s" + "q"
	r := env.runCmd("ADMINLISTUSERS", env.sysop, "", input)
	if r.err != nil {
		t.Fatalf("err = %v", r.err)
	}
	if !r.has("User Editor", "Field marked for update.", "Changes saved for Carla.") {
		t.Errorf("output:\n%s", r.text())
	}
	u, ok := env.diskUser(2)
	if !ok {
		t.Fatal("caller missing")
	}
	if u.Handle != "Carla" || u.RealName != "Carla Caller" || u.GroupLocation != "Pittsburgh" ||
		u.PrivateNote != "watch this one" || u.Flags != "AB" || u.AccessLevel != 50 {
		t.Errorf("saved record = %+v", u)
	}
	logs := modAdminLog(t, env)
	want := map[string][2]string{
		"handle":   {"Caller", "Carla"},
		"realname": {"Carl Caller", "Carla Caller"},
		"grouploc": {"", "Pittsburgh"},
		"note":     {"", "watch this one"},
		"flags":    {"", "AB"},
		"level":    {"10", "50"},
	}
	for f, ov := range want {
		l, ok := modLogField(logs, 2, f)
		if !ok || l.OldValue != ov[0] || l.NewValue != ov[1] {
			t.Errorf("%s audit = %+v (found %v), want %q -> %q", f, l, ok, ov[0], ov[1])
		}
	}
}

// TestUserEditor_UnchangedAndCancelledEditsStageNothing pins that retyping
// the same value reports "No change.", Esc abandons an edit, and in both
// cases quitting leaves the record and audit log untouched.
func TestUserEditor_UnchangedAndCancelledEditsStageNothing(t *testing.T) {
	env := newMenuEnv(t)
	input := "j" +
		"b\r" + // real name, accepted unchanged
		"a\r" + // handle, accepted unchanged
		"f\r" + // level, accepted unchanged
		"d" + "scribble" + ueEsc + // note, cancelled mid-edit
		"q"
	r := env.runCmd("ADMINLISTUSERS", env.sysop, "", input)
	if r.err != nil {
		t.Fatalf("err = %v", r.err)
	}
	if !r.has("No change.") {
		t.Errorf("output:\n%s", r.text())
	}
	if r.has("Unsaved changes!") {
		t.Errorf("quit was blocked by a staged change:\n%s", r.text())
	}
	if u, _ := env.diskUser(2); u.PrivateNote != "" || u.RealName != "Carl Caller" {
		t.Errorf("record changed: %+v", u)
	}
	if logs := modAdminLog(t, env); len(logs) != 0 {
		t.Errorf("admin log written: %+v", logs)
	}
}

// TestUserEditor_LevelInputValidation pins that a non-numeric level is
// rejected and that User #1 cannot be lowered below the sysop level from the
// field editor.
func TestUserEditor_LevelInputValidation(t *testing.T) {
	env := newMenuEnv(t)
	input := "f" + ueClear(3) + "10\r" + // sysop row: lower User #1
		"j" + "f" + ueClear(2) + "xy\r" + // caller row: not a number
		"q"
	r := env.runCmd("ADMINLISTUSERS", env.sysop, "", input)
	if !r.has("Cannot lower User #1 below SysOp level!", "Invalid number.") {
		t.Errorf("output:\n%s", r.text())
	}
	if u, _ := env.diskUser(1); u.AccessLevel != 255 {
		t.Errorf("sysop level = %d", u.AccessLevel)
	}
	if u, _ := env.diskUser(2); u.AccessLevel != 10 {
		t.Errorf("caller level = %d", u.AccessLevel)
	}
}

// TestUserEditor_PasswordChangeIsMaskedAndHashed pins that [P] echoes only
// asterisks, stores a bcrypt hash of the new password, and never writes the
// plaintext to the audit log.
func TestUserEditor_PasswordChangeIsMaskedAndHashed(t *testing.T) {
	env := newMenuEnv(t)
	r := env.runCmd("ADMINLISTUSERS", env.sysop, "", "jp"+"hunter22"+ueBS+"2\rsq")
	if r.err != nil {
		t.Fatalf("err = %v", r.err)
	}
	if strings.Contains(r.raw, "hunter") {
		t.Error("password echoed in clear")
	}
	if !r.has("********", "Password marked for update.") {
		t.Errorf("output:\n%s", r.text())
	}
	u, _ := env.diskUser(2)
	if err := bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte("hunter22")); err != nil {
		t.Errorf("stored hash does not match new password: %v", err)
	}
	l, ok := modLogField(modAdminLog(t, env), 2, "password")
	if !ok || l.OldValue != "********" || l.NewValue != "********" {
		t.Errorf("password audit = %+v (found %v)", l, ok)
	}
}

// TestUserEditor_EmptyPasswordCancels pins that accepting a blank password
// stages nothing.
func TestUserEditor_EmptyPasswordCancels(t *testing.T) {
	env := newMenuEnv(t)
	r := env.runCmd("ADMINLISTUSERS", env.sysop, "", "jp\rq")
	if !r.has("Password change cancelled.") || r.has("Unsaved changes!") {
		t.Errorf("output:\n%s", r.text())
	}
}

// TestUserEditor_UnsavedChangesGuardQuitAndDiscard pins that with an edit
// staged, [Q] and Esc warn instead of leaving and navigation is frozen (so a
// save cannot land on another user), and [X] discards the edit.
func TestUserEditor_UnsavedChangesGuardQuitAndDiscard(t *testing.T) {
	env := newMenuEnv(t)
	input := "j" + "e" + "Z\r" + "q" + ueEsc + "k" + ueArrowUp + "x" + "k" + "q"
	r := env.runCmd("ADMINLISTUSERS", env.sysop, "", input)
	if r.err != nil {
		t.Fatalf("err = %v", r.err)
	}
	if !r.has("Unsaved changes! Press [S] to save or [X] to abort.", "Changes discarded.") {
		t.Errorf("output:\n%s", r.text())
	}
	if r.next == "LOGOFF" {
		t.Error("editor ran out of input instead of quitting after discard")
	}
	if u, _ := env.diskUser(2); u.Flags != "" {
		t.Errorf("discarded flags saved: %q", u.Flags)
	}
}

// TestUserEditor_BanUnbanAndDeleteToggles pins the [0] and [9] toggles: [0]
// on a live user bans (level 0, unvalidated), [0] on a banned user restores
// the regular level and validation, [9] soft-deletes, and all three refuse
// User #1.
func TestUserEditor_BanUnbanAndDeleteToggles(t *testing.T) {
	env := newMenuEnv(t)
	env.seedUsers(&user.User{ID: 3, Handle: "Exile", AccessLevel: 0})

	input := "0" + "9" + // refused on User #1
		"j0s" + // ban Caller
		"j0s" + // unban Exile
		"k9s" + // delete Caller
		"q"
	r := env.runCmd("ADMINLISTUSERS", env.sysop, "", input)
	if r.err != nil {
		t.Fatalf("err = %v", r.err)
	}
	if !r.has("Cannot ban User #1!", "Cannot delete User #1!") {
		t.Errorf("output:\n%s", r.text())
	}
	c, _ := env.diskUser(2)
	if c.AccessLevel != 0 || c.Validated || !c.DeletedUser {
		t.Errorf("caller = level %d validated %v deleted %v, want 0/false/true", c.AccessLevel, c.Validated, c.DeletedUser)
	}
	x, _ := env.diskUser(3)
	if want := env.e.GetServerConfig().RegularUserLevel; x.AccessLevel != want || !x.Validated {
		t.Errorf("exile = level %d validated %v, want %d/true", x.AccessLevel, x.Validated, want)
	}
	if s, _ := env.diskUser(1); s.AccessLevel != 255 || !s.Validated || s.DeletedUser {
		t.Errorf("sysop changed: %+v", s)
	}
}

// TestUserEditor_ToggleValidatedAndProtectUserOne pins [G]: it refuses to
// unvalidate User #1 and unvalidates another user when saved.
func TestUserEditor_ToggleValidatedAndProtectUserOne(t *testing.T) {
	env := newMenuEnv(t)
	r := env.runCmd("ADMINLISTUSERS", env.sysop, "", "g"+ueArrowDown+"gsq")
	if !r.has("Cannot unvalidate User #1!", "Unvalidated status marked for update.") {
		t.Errorf("output:\n%s", r.text())
	}
	if u, _ := env.diskUser(2); u.Validated {
		t.Error("caller still validated")
	}
}

// TestUserEditor_SaveKeyMovesWhenNothingStaged pins that [S] with nothing
// staged steps down the list (the lightbar's WASD binding) rather than saving.
func TestUserEditor_SaveKeyMovesWhenNothingStaged(t *testing.T) {
	env := newMenuEnv(t)
	r := env.runCmd("ADMINLISTUSERS", env.sysop, "", "s"+"e"+"Q\r"+"s"+"w"+"q")
	if r.err != nil {
		t.Fatalf("err = %v", r.err)
	}
	if u, _ := env.diskUser(2); u.Flags != "Q" {
		t.Errorf("caller flags = %q, want Q (S should have moved onto Caller)", u.Flags)
	}
	if u, _ := env.diskUser(1); u.Flags != "" {
		t.Errorf("sysop flags = %q", u.Flags)
	}
}

// TestUserEditor_BlankHandleIsNotSaved pins that clearing the handle and
// saving is refused and leaves the handle on disk.
func TestUserEditor_BlankHandleIsNotSaved(t *testing.T) {
	env := newMenuEnv(t)
	r := env.runCmd("ADMINLISTUSERS", env.sysop, "", "ja"+ueClear(6)+"\rsxq")
	if !r.has("Handle cannot be blank.") {
		t.Errorf("output:\n%s", r.text())
	}
	if u, _ := env.diskUser(2); u.Handle != "Caller" {
		t.Errorf("handle = %q", u.Handle)
	}
}

// TestUserEditor_DisconnectLogsOff pins that running out of input while the
// editor waits for a key, or inside a field edit, reports a logoff and saves
// nothing.
func TestUserEditor_DisconnectLogsOff(t *testing.T) {
	env := newMenuEnv(t)
	for _, in := range []string{"j", "jb" + "partial"} {
		r := env.runCmd("ADMINLISTUSERS", env.sysop, "", in)
		if r.next != "LOGOFF" {
			t.Errorf("input %q: next = %q, want LOGOFF", in, r.next)
		}
	}
	if u, _ := env.diskUser(2); u.RealName != "Carl Caller" {
		t.Errorf("real name = %q", u.RealName)
	}
}
