package configeditor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// namedKeys maps the readable key names accepted by press to their key types.
var namedKeys = map[string]tea.KeyType{
	"enter":     tea.KeyEnter,
	"esc":       tea.KeyEscape,
	"up":        tea.KeyUp,
	"down":      tea.KeyDown,
	"left":      tea.KeyLeft,
	"right":     tea.KeyRight,
	"home":      tea.KeyHome,
	"end":       tea.KeyEnd,
	"pgup":      tea.KeyPgUp,
	"pgdown":    tea.KeyPgDown,
	"tab":       tea.KeyTab,
	"shift+tab": tea.KeyShiftTab,
	"space":     tea.KeySpace,
	"backspace": tea.KeyBackspace,
	"insert":    tea.KeyInsert,
	"delete":    tea.KeyDelete,
}

// keyMsg converts a key name into a tea.KeyMsg. Names listed in namedKeys map
// to their special key; anything else is sent as a rune key press, so "y" or
// "S" behave like a typed letter.
func keyMsg(k string) tea.KeyMsg {
	if kt, ok := namedKeys[k]; ok {
		if kt == tea.KeySpace {
			return tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}}
		}
		return tea.KeyMsg{Type: kt}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
}

// press drives m through Update with each key in order (see keyMsg for the
// accepted names) and returns the resulting Model. Returned commands are
// discarded; tests that need a command call Update directly.
func press(t *testing.T, m Model, keys ...string) Model {
	t.Helper()
	for _, k := range keys {
		m = hit(t, m, keyMsg(k))
	}
	return m
}

// typeText sends s to m one rune at a time, as a user typing into the active
// text input would.
func typeText(t *testing.T, m Model, s string) Model {
	t.Helper()
	for _, r := range s {
		m = hit(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	return m
}

// replaceText clears the active text input and types s into it.
func replaceText(t *testing.T, m Model, s string) Model {
	t.Helper()
	m.textInput.SetValue("")
	return typeText(t, m, s)
}

// newDiskModel builds an editor Model over <tmp>/configs, so saves, the
// binkd.conf sync (<tmp>/data) and JAM paths all land inside t.TempDir().
// The data dir is created up front for key files. It returns the model and
// the configs directory for reloading.
func newDiskModel(t *testing.T) (Model, string) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "configs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "..", "data"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	m, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	// Keep any V3Net key file inside the temp tree rather than ./data.
	m.configs.V3Net.KeystorePath = filepath.Join(dir, "..", "data", "v3net.key")
	return m, dir
}

// reloadConfigs reads every config file back from dir, failing the test on
// any load error.
func reloadConfigs(t *testing.T, dir string) allConfigs {
	t.Helper()
	ac, err := loadAllConfigs(dir)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	return ac
}

// screen renders m and strips ANSI styling for substring assertions.
func screen(m Model) string {
	return stripStyles(m.View())
}

// wantScreen fails the test unless the rendered screen contains every want.
func wantScreen(t *testing.T, m Model, wants ...string) {
	t.Helper()
	s := screen(m)
	for _, w := range wants {
		if !strings.Contains(s, w) {
			t.Errorf("screen missing %q", w)
		}
	}
}

// first returns the model from an Update result, dropping the command.
func first(m tea.Model, _ tea.Cmd) tea.Model {
	return m
}
