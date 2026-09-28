package usereditor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// paste returns the KeyMsg a terminal paste of s delivers.
func paste(s string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s), Paste: true} }

// openKeys moves to the WFC Keys action and opens the key manager.
func openKeys(t *testing.T, m Model) Model {
	t.Helper()
	m = focusField(t, m, "WFC Keys")
	m = press(t, m, key(tea.KeyEnter))
	if m.mode != modeKeyList {
		t.Fatalf("Enter on WFC Keys: mode = %v", m.mode)
	}
	return m
}

// A key pasted into the add dialog is registered, selected in the list, and
// saved with the user.
func TestKeyManagerAddsAndSavesAKey(t *testing.T) {
	m, path := editBob(t)
	m = openKeys(t, m)
	if !strings.Contains(stripANSIGolden(m.View()), "(no keys registered)") {
		t.Error("empty key list is not explained")
	}
	m = press(t, m, char('d'))
	if m.dirty {
		t.Error("D with no keys changed something")
	}

	m = press(t, m, char('a'))
	if m.mode != modeKeyAdd {
		t.Fatalf("A: mode = %v", m.mode)
	}
	if !strings.Contains(stripANSIGolden(m.View()), "Paste OpenSSH Public Key") {
		t.Error("add dialog is not drawn")
	}
	m = press(t, m, paste(makeTestKey(t, "bobkey")), key(tea.KeyEnter))
	if m.mode != modeKeyList || m.keySelected != 0 {
		t.Fatalf("after add: mode=%v selected=%d", m.mode, m.keySelected)
	}
	if !strings.Contains(stripANSIGolden(m.View()), "bobkey") {
		t.Error("added key is not listed")
	}

	m = press(t, m, key(tea.KeyEscape))
	if m.mode != modeEdit {
		t.Fatalf("Escape from key list: mode = %v", m.mode)
	}
	if !strings.Contains(stripANSIGolden(m.View()), "(1)") {
		t.Error("edit screen does not count the new key")
	}
	m = press(t, m, key(tea.KeyEscape), key(tea.KeyEnter))

	got, _ := reloadUser(t, path, 2)
	if len(got.PublicKeys) != 1 || !strings.Contains(got.PublicKeys[0], "bobkey") {
		t.Errorf("saved keys = %v", got.PublicKeys)
	}
}

// A malformed key is refused with the reason shown in the dialog, which stays
// open; Escape then clears the error and returns to the list.
func TestKeyManagerRejectsAMalformedKey(t *testing.T) {
	m, _ := editBob(t)
	m = openKeys(t, m)
	m = press(t, m, char('a'), paste("ssh-rsa not-a-key"), key(tea.KeyEnter))
	if m.mode != modeKeyAdd || m.keyDialogErr == "" {
		t.Fatalf("bad key: mode=%v err=%q", m.mode, m.keyDialogErr)
	}
	errPrefix := m.keyDialogErr
	if len(errPrefix) > 20 {
		errPrefix = errPrefix[:20]
	}
	if !strings.Contains(stripANSIGolden(m.View()), errPrefix) {
		t.Error("the rejection reason is not shown")
	}
	if len(m.users[m.editIndex].PublicKeys) != 0 || m.dirty {
		t.Error("a malformed key was stored")
	}
	m = press(t, m, key(tea.KeyEscape))
	if m.mode != modeKeyList || m.keyDialogErr != "" {
		t.Errorf("Escape: mode=%v err=%q", m.mode, m.keyDialogErr)
	}
}

// Enter on an empty add dialog is a cancel, not an error.
func TestKeyManagerEmptyAddCancels(t *testing.T) {
	m, _ := editBob(t)
	m = openKeys(t, m)
	m = press(t, m, char('A'), key(tea.KeyEnter))
	if m.mode != modeKeyList || m.keyDialogErr != "" || m.dirty {
		t.Errorf("mode=%v err=%q dirty=%v", m.mode, m.keyDialogErr, m.dirty)
	}
}

// Up and Down move the selection within the list's bounds, and D deletes the
// selected key, keeping the selection on a remaining key.
func TestKeyManagerSelectAndDelete(t *testing.T) {
	m, _ := editBob(t)
	u := m.users[m.editIndex]
	for _, c := range []string{"one", "two", "three"} {
		if _, err := u.AddPublicKey(makeTestKey(t, c)); err != nil {
			t.Fatal(err)
		}
	}
	m = openKeys(t, m)
	m = press(t, m, key(tea.KeyUp))
	if m.keySelected != 0 {
		t.Errorf("Up at the top = %d", m.keySelected)
	}
	m = press(t, m, key(tea.KeyDown), key(tea.KeyDown), key(tea.KeyDown))
	if m.keySelected != 2 {
		t.Errorf("Down past the end = %d, want 2", m.keySelected)
	}
	m = press(t, m, char('D'))
	if m.keySelected != 1 {
		t.Errorf("after deleting the last key selection = %d, want 1", m.keySelected)
	}
	keys, _ := u.ListPublicKeys()
	if len(keys) != 2 || keys[0].Comment != "one" || keys[1].Comment != "two" {
		t.Errorf("remaining keys = %+v, want one, two", keys)
	}
	if !m.dirty {
		t.Error("delete did not mark the model dirty")
	}
}

// The InfoForms display field reports which of the five forms the user has on
// file, from the responses directory under the data dir.
func TestInfoFormsStatusFromDisk(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "users.json")
	if _, err := SaveUsers(path, []*user.User{{ID: 1, Handle: "Sysop"}}); err != nil {
		t.Fatal(err)
	}
	responses := filepath.Join(dir, "infoforms", "responses")
	if err := os.MkdirAll(responses, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(responses, "1_2.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := New(path, dir)
	if err != nil {
		t.Fatal(err)
	}
	m = press(t, m, key(tea.KeyEnter))
	if want := "1[ ] 2[x] 3[ ]"; !strings.Contains(stripANSIGolden(m.View()), want) {
		t.Errorf("edit screen lacks %q", want)
	}
	if got := infoformStatus("", 1); got != "N/A" {
		t.Errorf("infoformStatus with no data dir = %q, want N/A", got)
	}
}

// The Auto Purge field counts down the configured retention from the delete
// date: blank for a live user, "Eligible now" once it has passed, and "Never"
// when retention is disabled.
func TestAutoPurgeCountdown(t *testing.T) {
	root := t.TempDir()
	dataDir := filepath.Join(root, "data")
	cfgDir := filepath.Join(root, "configs")
	for _, d := range []string{filepath.Join(dataDir, "users"), cfgDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeRetention := func(days string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(cfgDir, "config.json"),
			[]byte(`{"deletedUserRetentionDays": `+days+`}`), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	long := time.Now().AddDate(0, 0, -100)
	recent := time.Now().Add(-time.Hour)
	path := filepath.Join(dataDir, "users", "users.json")
	if _, err := SaveUsers(path, []*user.User{
		{ID: 1, Handle: "Sysop"},
		{ID: 2, Handle: "Old", DeletedUser: true, DeletedAt: &long},
		{ID: 3, Handle: "New", DeletedUser: true, DeletedAt: &recent},
	}); err != nil {
		t.Fatal(err)
	}

	purge := func(m Model, id int) string {
		for _, f := range m.fields {
			if f.Label == "Auto Purge" {
				for _, u := range m.users {
					if u.ID == id {
						return f.Get(u)
					}
				}
			}
		}
		t.Fatalf("no Auto Purge field or user %d", id)
		return ""
	}

	writeRetention("30")
	m, err := New(path, dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if got := purge(m, 1); got != "" {
		t.Errorf("live user: %q, want blank", got)
	}
	if got := purge(m, 2); got != "Eligible now" {
		t.Errorf("deleted 100 days ago: %q, want Eligible now", got)
	}
	if got := purge(m, 3); got != "30 days" {
		t.Errorf("deleted an hour ago: %q, want 30 days", got)
	}

	writeRetention("1")
	m, _ = New(path, dataDir)
	if got := purge(m, 3); got != "1 day" {
		t.Errorf("1-day retention: %q, want 1 day", got)
	}

	writeRetention("-1")
	m, _ = New(path, dataDir)
	if got := purge(m, 2); got != "Never" {
		t.Errorf("retention disabled: %q, want Never", got)
	}
}

// New fails cleanly on a users file it cannot parse, rather than opening an
// editor over nothing that would then save an empty list.
func TestNewRejectsACorruptFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "users.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := New(path); err == nil {
		t.Error("New accepted a corrupt users.json")
	}
}
