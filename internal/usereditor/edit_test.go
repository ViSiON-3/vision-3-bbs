package usereditor

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"golang.org/x/crypto/bcrypt"

	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// editBob returns an editor over the sysop and Bob, sitting on Bob's edit
// screen, with the users.json path.
func editBob(t *testing.T) (Model, string) {
	t.Helper()
	m, path := editorOver(t,
		&user.User{ID: 1, Handle: "Sysop", AccessLevel: 255},
		&user.User{ID: 2, Handle: "Bob", AccessLevel: 5, RealName: "Bob Smith"},
	)
	m = press(t, m, tea.WindowSizeMsg{Width: 80, Height: 25}, key(tea.KeyDown), key(tea.KeyEnter))
	if m.mode != modeEdit || m.users[m.editIndex].ID != 2 {
		t.Fatalf("setup: mode %v editing ID %d", m.mode, m.users[m.editIndex].ID)
	}
	return m, path
}

// replaceField opens the focused field, erases its value and types s, leaving
// the input open.
func replaceField(t *testing.T, m Model, s string) Model {
	t.Helper()
	m = press(t, m, key(tea.KeyEnter))
	if m.mode != modeEditField {
		t.Fatalf("Enter did not open %q, mode = %v", m.label(), m.mode)
	}
	for range m.textInput.Value() {
		m = press(t, m, key(tea.KeyBackspace))
	}
	return typeRunes(t, m, s)
}

// Editing a text field and leaving with "save to disk? Yes" writes it: the
// value survives a reload through the BBS's user manager.
func TestEditedFieldIsSavedOnLeave(t *testing.T) {
	m, path := editBob(t)
	m = focusField(t, m, "Group/Location")
	m = replaceField(t, m, "Mars")
	m = press(t, m, key(tea.KeyEnter))
	if m.mode != modeEdit {
		t.Fatalf("Enter did not confirm, mode = %v message %q", m.mode, m.message)
	}
	if !strings.Contains(stripANSIGolden(m.View()), "Mars") {
		t.Error("edited value is not shown on the edit screen")
	}

	m = press(t, m, key(tea.KeyEscape))
	if m.mode != modeSaveOnLeave {
		t.Fatalf("Escape after an edit: mode = %v, want modeSaveOnLeave", m.mode)
	}
	if !strings.Contains(stripANSIGolden(m.View()), "Save changes to disk?") {
		t.Error("save prompt is not drawn over the edit screen")
	}
	m = press(t, m, key(tea.KeyEnter)) // default is Yes
	if m.mode != modeList || m.dirty {
		t.Fatalf("after saving: mode=%v dirty=%v message=%q", m.mode, m.dirty, m.message)
	}

	got, _ := reloadUser(t, path, 2)
	if got.GroupLocation != "Mars" {
		t.Errorf("group on disk = %q, want Mars", got.GroupLocation)
	}
}

// Answering No to "save to disk?" returns to the list without writing, but
// keeps the edit in memory (still dirty) for a later save.
func TestSaveOnLeaveNoKeepsEditInMemoryOnly(t *testing.T) {
	m, path := editBob(t)
	m = focusField(t, m, "Access Flags")
	m = replaceField(t, m, "abc")
	m = press(t, m, key(tea.KeyTab), key(tea.KeyEscape), char('n'))

	if m.mode != modeList || !m.dirty {
		t.Fatalf("mode=%v dirty=%v, want list and still dirty", m.mode, m.dirty)
	}
	if m.users[1].Flags != "ABC" {
		t.Errorf("flags in memory = %q, want upper-cased ABC", m.users[1].Flags)
	}
	got, _ := reloadUser(t, path, 2)
	if got.Flags != "" {
		t.Errorf("declined save reached disk: flags = %q", got.Flags)
	}
}

// Escape from the save prompt goes back to the edit screen, not the list.
func TestSaveOnLeaveEscapeReturnsToEdit(t *testing.T) {
	m, _ := editBob(t)
	m = focusField(t, m, "Private Note")
	m = replaceField(t, m, "hi")
	m = press(t, m, key(tea.KeyEnter), key(tea.KeyEscape), key(tea.KeyEscape))
	if m.mode != modeEdit {
		t.Errorf("mode = %v, want modeEdit", m.mode)
	}
}

// Leaving the edit screen without having changed anything goes straight back
// to the list, with no prompt.
func TestEscapeWithoutEditsReturnsToList(t *testing.T) {
	m, _ := editBob(t)
	m = press(t, m, key(tea.KeyEscape))
	if m.mode != modeList {
		t.Errorf("mode = %v, want modeList", m.mode)
	}
}

// Integer fields refuse non-digits as they are typed and reject out-of-range
// values on confirm, staying open with an explanation; Escape then cancels
// without changing the user.
func TestIntegerFieldValidation(t *testing.T) {
	m, _ := editBob(t)
	m = focusField(t, m, "Access Level")
	m = replaceField(t, m, "4x2")
	if got := m.textInput.Value(); got != "42" {
		t.Errorf("typed 4x2, input holds %q; non-digits must be dropped", got)
	}

	m = replaceField(t, press(t, m, key(tea.KeyEscape)), "999")
	m = press(t, m, key(tea.KeyEnter))
	if m.mode != modeEditField || m.message != "Invalid: must be 0-255" {
		t.Fatalf("out-of-range: mode=%v message=%q", m.mode, m.message)
	}
	m = press(t, m, key(tea.KeyDown))
	if m.mode != modeEditField {
		t.Error("Down confirmed an out-of-range value")
	}

	m = replaceField(t, press(t, m, key(tea.KeyEscape)), "-")
	m = press(t, m, key(tea.KeyEnter))
	if m.message != "Invalid: not a number" {
		t.Errorf("lone minus: message = %q", m.message)
	}

	m = press(t, m, key(tea.KeyEscape))
	if m.mode != modeEdit || m.users[m.editIndex].AccessLevel != 5 || m.dirty {
		t.Errorf("cancel: mode=%v level=%d dirty=%v, want edit, 5, clean",
			m.mode, m.users[m.editIndex].AccessLevel, m.dirty)
	}
}

// A valid integer confirmed with Up applies and moves up within the column.
func TestIntegerFieldAppliesOnUp(t *testing.T) {
	m, _ := editBob(t)
	m = focusField(t, m, "Time Limit")
	m = replaceField(t, m, "90")
	m = press(t, m, key(tea.KeyUp))
	if m.users[m.editIndex].TimeLimit != 90 {
		t.Errorf("time limit = %d, want 90", m.users[m.editIndex].TimeLimit)
	}
	if m.label() != "Custom Prompt" {
		t.Errorf("Up landed on %q, want Custom Prompt", m.label())
	}
}

// Yes/No fields take a single Y or N and confirm immediately, advancing to the
// next field; other keys are ignored while the field is open.
func TestYesNoFieldAutoConfirms(t *testing.T) {
	m, _ := editBob(t)
	m = focusField(t, m, "Validated")
	m = press(t, m, key(tea.KeyEnter), key(tea.KeyBackspace))
	if m.mode != modeEditField {
		t.Fatalf("a non-rune key closed the Y/N field, mode = %v", m.mode)
	}
	m = press(t, m, char('y'))
	if m.mode != modeEdit || !m.users[m.editIndex].Validated {
		t.Fatalf("y: mode=%v validated=%v", m.mode, m.users[m.editIndex].Validated)
	}
	if m.label() != "Hot Keys" {
		t.Errorf("after y the cursor is on %q, want Hot Keys", m.label())
	}

	m = focusField(t, m, "Validated")
	m = press(t, m, key(tea.KeyEnter), char('N'))
	if m.users[m.editIndex].Validated {
		t.Error("N did not clear Validated")
	}
}

// Ctrl+Home and Ctrl+End jump to the first and last editable fields.
func TestCtrlHomeEndJump(t *testing.T) {
	m, _ := editBob(t)
	m = press(t, m, key(tea.KeyCtrlEnd))
	if m.label() != "Output Mode" {
		t.Errorf("Ctrl+End = %q, want Output Mode", m.label())
	}
	m = press(t, m, key(tea.KeyCtrlHome))
	if m.label() != "Handle" {
		t.Errorf("Ctrl+Home = %q, want Handle", m.label())
	}
}

// Page Down and Page Up step between users on the edit screen, wrapping at
// both ends and resetting the field cursor.
func TestPageKeysStepBetweenUsers(t *testing.T) {
	m, _ := editBob(t)
	m = press(t, m, key(tea.KeyDown))
	m = press(t, m, key(tea.KeyPgDown))
	if m.users[m.editIndex].ID != 1 || m.editField != 0 {
		t.Errorf("PgDn from the last user: ID %d field %d, want wrap to 1 at field 0",
			m.users[m.editIndex].ID, m.editField)
	}
	m = press(t, m, key(tea.KeyPgUp))
	if m.users[m.editIndex].ID != 2 {
		t.Errorf("PgUp from the first user: ID %d, want wrap to 2", m.users[m.editIndex].ID)
	}
	if !strings.Contains(stripANSIGolden(m.View()), "Bob Smith") {
		t.Error("edit screen does not show the paged-to user")
	}
}

// F10 leaves the edit screen for the list without the save prompt.
func TestF10LeavesWithoutPrompt(t *testing.T) {
	m, _ := editBob(t)
	m = press(t, m, key(tea.KeyF10))
	if m.mode != modeList {
		t.Errorf("mode = %v, want modeList", m.mode)
	}
}

// From the edit screen, F2 deletes (with the purge offer), and a second F2
// undeletes; both return to the edit screen on the same user.
func TestDeleteAndUndeleteFromEditScreen(t *testing.T) {
	m, _ := editBob(t)
	m = press(t, m, key(tea.KeyF2))
	if m.mode != modeDeleteConfirm {
		t.Fatalf("F2: mode = %v", m.mode)
	}
	if !strings.Contains(stripANSIGolden(m.View()), "Delete Bob?") {
		t.Error("delete prompt is not drawn over the edit screen")
	}
	m = press(t, m, char('y'), char('n'))
	if m.mode != modeEdit {
		t.Fatalf("declining purge from edit: mode = %v, want modeEdit", m.mode)
	}
	if u := m.users[m.editIndex]; u.ID != 2 || !u.DeletedUser {
		t.Fatalf("editing ID %d deleted=%v, want Bob deleted", u.ID, u.DeletedUser)
	}
	if !strings.Contains(stripANSIGolden(m.View()), "F2 - Undelete  F4 - Purge") {
		t.Error("help bar does not switch to the deleted-user actions")
	}

	m = press(t, m, key(tea.KeyF2))
	if !strings.Contains(stripANSIGolden(m.View()), "Undelete Bob?") {
		t.Error("undelete prompt is not drawn over the edit screen")
	}
	m = press(t, m, char('y'))
	if m.mode != modeEdit || m.users[m.editIndex].DeletedUser {
		t.Errorf("undelete from edit: mode=%v deleted=%v", m.mode, m.users[m.editIndex].DeletedUser)
	}
}

// F4 from the edit screen purges a deleted user and stays on the edit screen.
func TestPurgeFromEditScreen(t *testing.T) {
	m, _ := editBob(t)
	m = press(t, m, key(tea.KeyF2), char('y'), char('n'))
	m = press(t, m, key(tea.KeyF4))
	if !strings.Contains(stripANSIGolden(m.View()), "Bob") {
		t.Error("purge prompt does not name the user")
	}
	m = press(t, m, char('y'))
	if m.mode != modeEdit || len(m.users) != 1 {
		t.Errorf("mode=%v users=%d, want edit screen with Bob gone", m.mode, len(m.users))
	}
}

// User 1 is protected from the edit screen too, and a live user cannot be
// purged; the alert is drawn over the edit screen and returns to it.
func TestEditScreenRefusals(t *testing.T) {
	m, _ := editBob(t)
	m = press(t, m, key(tea.KeyF4))
	if m.mode != modeInfoAlert || !strings.Contains(stripANSIGolden(m.View()), "must be deleted") {
		t.Fatalf("F4 on live Bob: mode %v", m.mode)
	}
	m = press(t, m, key(tea.KeyEnter), key(tea.KeyPgUp))
	if m.users[m.editIndex].ID != 1 {
		t.Fatal("setup: not on user 1")
	}
	for _, k := range []tea.KeyType{tea.KeyF2, tea.KeyF4} {
		m = press(t, m, key(k))
		if m.mode != modeInfoAlert || !strings.Contains(m.alertMessage, "User 1") {
			t.Errorf("%v on user 1: mode=%v alert=%q", k, m.mode, m.alertMessage)
		}
		m = press(t, m, char(' '))
		if m.mode != modeEdit {
			t.Errorf("alert returned to %v, want modeEdit", m.mode)
		}
	}
}

// F5 from the edit screen validates the user being edited and stays there;
// "No" leaves the user unchanged.
func TestValidateFromEditScreen(t *testing.T) {
	m, _ := editBob(t)
	m = press(t, m, key(tea.KeyF5))
	if !strings.Contains(stripANSIGolden(m.View()), "Set Bob to Defaults?") {
		t.Error("validate prompt is not drawn over the edit screen")
	}
	m = press(t, m, char('n'))
	if m.mode != modeEdit || m.users[m.editIndex].Validated {
		t.Fatalf("No: mode=%v validated=%v", m.mode, m.users[m.editIndex].Validated)
	}
	m = press(t, m, key(tea.KeyF5), char('y'))
	if m.mode != modeEdit || !m.users[m.editIndex].Validated || m.users[m.editIndex].AccessLevel != 10 {
		t.Errorf("Yes: mode=%v user=%+v", m.mode, m.users[m.editIndex])
	}
}

// openPassword moves to the Password action and opens its entry dialog.
func openPassword(t *testing.T, m Model) Model {
	t.Helper()
	m = focusField(t, m, "Password")
	m = press(t, m, key(tea.KeyEnter))
	if m.mode != modePasswordEntry {
		t.Fatalf("Enter on Password: mode = %v", m.mode)
	}
	return m
}

// The password dialog hashes what is typed with bcrypt, and the new hash is
// what gets saved; the typed text never is.
func TestPasswordEntrySetsAHash(t *testing.T) {
	m, path := editBob(t)
	m = openPassword(t, m)
	m = typeRunes(t, m, "hunter2")
	if strings.Contains(stripANSIGolden(m.View()), "hunter2") {
		t.Error("password is echoed on screen")
	}
	m = press(t, m, key(tea.KeyEnter))
	if m.mode != modeEdit || m.message != "Password updated" {
		t.Fatalf("mode=%v message=%q", m.mode, m.message)
	}
	m = press(t, m, key(tea.KeyEscape), key(tea.KeyEnter))

	got, _ := reloadUser(t, path, 2)
	if err := bcrypt.CompareHashAndPassword([]byte(got.PasswordHash), []byte("hunter2")); err != nil {
		t.Errorf("saved hash does not match the typed password: %v", err)
	}
}

// Cancelling or submitting an empty password leaves the hash alone.
func TestPasswordEntryCancelAndEmpty(t *testing.T) {
	m, _ := editBob(t)
	m = openPassword(t, m)
	m = typeRunes(t, m, "nope")
	m = press(t, m, key(tea.KeyEscape))
	if m.mode != modeEdit || m.users[m.editIndex].PasswordHash != "" || m.dirty {
		t.Fatalf("Escape: mode=%v dirty=%v", m.mode, m.dirty)
	}
	m = press(t, openPassword(t, m), key(tea.KeyEnter))
	if m.mode != modeEdit || m.users[m.editIndex].PasswordHash != "" || m.dirty {
		t.Errorf("empty Enter: mode=%v dirty=%v", m.mode, m.dirty)
	}
}

// bcrypt reads only 72 bytes; a password longer than that in bytes (easy with
// multi-byte runes under the 72-rune input limit) is refused, not truncated.
func TestPasswordEntryRejectsOver72Bytes(t *testing.T) {
	m, _ := editBob(t)
	m = openPassword(t, m)
	m = typeRunes(t, m, strings.Repeat("é", 40)) // 80 bytes
	m = press(t, m, key(tea.KeyEnter))
	if m.mode != modePasswordEntry || m.message != "Password too long (max 72 bytes)" {
		t.Errorf("mode=%v message=%q", m.mode, m.message)
	}
	if m.users[m.editIndex].PasswordHash != "" {
		t.Error("an over-long password was hashed")
	}
}

// Every editable text and number field round-trips: a value typed into it and
// confirmed is what the field then reads back from the user record, and what
// is written to disk.
func TestEveryFieldAcceptsAnEdit(t *testing.T) {
	values := map[string]string{
		"Handle": "Robert", "Real Name": "Rob Smith", "Access Level": "20",
		"Total Calls": "12", "Group/Location": "Moon", "Access Flags": "XY",
		"Private Note": "vip", "File Points": "300", "Custom Prompt": "$>",
		"Time Limit": "45", "Screen Width": "132", "Screen Height": "50",
		"Encoding": "utf8", "Msg Header": "3", "Output Mode": "ansi",
	}
	m, path := editBob(t)
	for label, v := range values {
		m = focusField(t, m, label)
		m = replaceField(t, m, v)
		m = press(t, m, key(tea.KeyEnter))
		if m.mode != modeEdit {
			t.Fatalf("%s=%q not accepted: %q", label, v, m.message)
		}
	}
	m = press(t, m, key(tea.KeyEscape), key(tea.KeyEnter))

	got, ok := reloadUser(t, path, 2)
	if !ok {
		t.Fatal("Bob missing after save")
	}
	for _, f := range m.fields {
		if want, ok := values[f.Label]; ok {
			if have := f.Get(got); have != want {
				t.Errorf("%s on disk = %q, want %q", f.Label, have, want)
			}
		}
	}
}
