package usereditor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mattn/go-runewidth"

	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

func hasField(m Model, label string) bool {
	for _, f := range m.fields {
		if f.Label == label {
			return true
		}
	}
	return false
}

func pairedBob() *user.User {
	bob := &user.User{ID: 2, Handle: "Bob", AccessLevel: 5, PasswordHash: "hash"}
	stamp := user.PasswordStamp(bob.PasswordHash)
	bob.APIDevices = []user.APIDevice{
		{ID: "a", Name: "Old Phone", PasswordStamp: stamp, CreatedAt: time.Date(2026, 1, 2, 12, 0, 0, 0, time.UTC)},
		{ID: "b", Name: "New Phone", PasswordStamp: stamp, CreatedAt: time.Date(2026, 3, 4, 12, 0, 0, 0, time.UTC)},
	}
	return bob
}

// A board that has never enabled the QWK API must not see the field.
func TestDeviceFieldHiddenWithoutQWKAPI(t *testing.T) {
	m, _ := editBob(t)
	if hasField(m, qwkDevicesField) {
		t.Error("QWK Devices shown on a board without the API or any devices")
	}
}

func TestDeviceFieldFollowsConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "users.json")
	if _, err := SaveUsers(path, []*user.User{{ID: 1, Handle: "Sysop", AccessLevel: 255}}); err != nil {
		t.Fatal(err)
	}
	cfg := filepath.Join(dir, "configs")
	if err := os.Mkdir(cfg, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfg, "config.json"), []byte(`{"qwkAPI":{"enabled":true}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	m, err := NewWithConfig(path, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !hasField(m, qwkDevicesField) {
		t.Error("QWK Devices hidden with the API enabled")
	}
}

// Unpairing marks the user changed, reaches the file on save, and leaves the
// other device alone. Leftover devices also keep the field reachable with the
// API off.
func TestDeviceDialogUnpairsAndSaves(t *testing.T) {
	m, path := editorOver(t, &user.User{ID: 1, Handle: "Sysop", AccessLevel: 255}, pairedBob())
	m = press(t, m, tea.WindowSizeMsg{Width: 80, Height: 25}, key(tea.KeyDown), key(tea.KeyEnter))
	m = focusField(t, m, qwkDevicesField)
	m = press(t, m, key(tea.KeyEnter))
	if m.mode != modeDeviceList {
		t.Fatalf("mode = %v", m.mode)
	}
	if screen := stripANSIGolden(m.View()); !strings.Contains(screen, "Old Phone") || !strings.Contains(screen, "New Phone") {
		t.Errorf("devices not listed:\n%s", screen)
	}
	m = press(t, m, char('d'), key(tea.KeyEscape))
	if !m.dirty {
		t.Error("unpairing did not mark the user changed")
	}
	m.saveCurrentUser()
	m.saveAllToDisk(false)
	users, _, err := LoadUsers(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := users[1].APIDevices; len(got) != 1 || got[0].ID != "b" {
		t.Errorf("devices on disk = %+v, want only b", got)
	}
}

// F10 restores from the load-time snapshot, so that copy must not share the
// device slice with the one being edited.
func TestCloneUserCopiesDevices(t *testing.T) {
	u := pairedBob()
	c := CloneUser(u)
	u.APIDevices[0].Name = "changed"
	if c.APIDevices[0].Name == "changed" {
		t.Error("clone shares its device slice")
	}
}

// A device or handle using double-width characters must not widen the dialog.
func TestDeviceDialogFitsWideNames(t *testing.T) {
	bob := pairedBob()
	bob.Handle = strings.Repeat("界", 40)
	bob.APIDevices[0].Name = strings.Repeat("界", 44)
	m, _ := editorOver(t, &user.User{ID: 1, Handle: "Sysop", AccessLevel: 255}, bob)
	m = press(t, m, tea.WindowSizeMsg{Width: 80, Height: 25}, key(tea.KeyDown), key(tea.KeyEnter))
	m = focusField(t, m, qwkDevicesField)
	m = press(t, m, key(tea.KeyEnter))
	for i, line := range strings.Split(stripANSIGolden(m.View()), "\n") {
		if !strings.Contains(line, "║") {
			continue // the edit screen behind the dialog
		}
		if w := runewidth.StringWidth(line); w > 80 {
			t.Errorf("row %d is %d cells wide: %q", i, w, line)
		}
	}
}
