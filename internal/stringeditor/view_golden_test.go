package stringeditor

import (
	"flag"
	"os"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

var updateGolden = flag.Bool("update", false, "regenerate golden files")

// checkGolden compares got against testdata/<name>.golden, regenerating the
// file when -update is passed.
//
// These are the audit's screen captures: one archived rendering per terminal
// size and interaction state, so a layout change shows up as a reviewable diff
// rather than having to be spotted by eye.
func checkGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name+".golden")
	if *updateGolden {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v (run with -update to regenerate)", path, err)
	}
	if got != string(want) {
		t.Errorf("%s: output differs from golden file %s", name, path)
	}
}

// TestViewGolden archives the rendered screen for each audited size and
// interaction state. Color is forced on so the captures include the styling.
func TestViewGolden(t *testing.T) {
	old := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(old) })

	states := []struct {
		name  string
		apply func(t *testing.T, m Model) Model
	}{
		{"navigate", func(t *testing.T, m Model) Model { return m }},
		{"edit", func(t *testing.T, m Model) Model {
			return key2(t, m, tea.KeyMsg{Type: tea.KeyF1})
		}},
		{"edit_error", func(t *testing.T, m Model) Model {
			m = key2(t, m, tea.KeyMsg{Type: tea.KeyF1})
			m.textInput.SetValue(`\q`)
			return key2(t, m, tea.KeyMsg{Type: tea.KeyEnter})
		}},
		{"search", func(t *testing.T, m Model) Model {
			return key2(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
		}},
		{"dialog", func(t *testing.T, m Model) Model {
			m.mode = modeAbortConfirm
			return m
		}},
		{"last_page", func(t *testing.T, m Model) Model {
			return key2(t, m, tea.KeyMsg{Type: tea.KeyEnd})
		}},
	}

	for _, size := range []struct{ w, h int }{{80, 25}, {120, 45}} {
		for _, st := range states {
			name := st.name + "_" + sizeName(size.w, size.h)
			t.Run(name, func(t *testing.T) {
				m := newShippedModel(t)
				m = resize(t, m, size.w, size.h)
				// Park the cursor on the multiline entry that motivated #234.
				m.cursor = indexOfKey(t, m, "pageOnlineNodesHeader")
				m.page = m.cursor / m.pageSize
				checkGolden(t, name, st.apply(t, m).View())
			})
		}
	}
}
