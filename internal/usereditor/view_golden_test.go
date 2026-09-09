package usereditor

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/ViSiON-3/vision-3-bbs/internal/tuiart"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
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

// pinBackdrop replaces the randomly chosen startup art with the first embedded
// screen, so a capture records the layout rather than which of the three arts
// the run happened to draw. tuiart.Arts is sorted by filename, so index 0 is
// stable across runs and platforms.
func pinBackdrop(m Model, w, h int) Model {
	arts := tuiart.Arts()
	if len(arts) == 0 {
		return m
	}
	m.backdropArt = arts[0]
	m.backdrop = loadBackdropFrom(m.backdropArt, w, h)
	return m
}

// goldenUsers are fixed records with no wall-clock fields set, so the captures
// do not change with the date they were taken.
func goldenUsers() []*user.User {
	return []*user.User{
		{ID: 1, Handle: "Alice", RealName: "Alice A", AccessLevel: 100, GroupLocation: "Sysop"},
		{ID: 2, Handle: "Bob", RealName: "Bob B", AccessLevel: 20, GroupLocation: "Somewhere"},
		{ID: 3, Handle: "Carol", RealName: "Carol C", AccessLevel: 10},
	}
}

// TestViewGolden archives the rendered screen for each audited size and
// interaction state. Color is forced on so the captures include the styling
// that the tuiart palette adoption changed.
func TestViewGolden(t *testing.T) {
	old := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(old) })

	states := []struct {
		name string
		mode editorMode
		// typed is entered after reaching the mode, so the captures cover a
		// filled input and not only the empty one.
		typed string
	}{
		{"list", modeList, ""},
		{"edit", modeEdit, ""},
		{"edit_field", modeEditField, "NewHandle"},
		{"search", modeSearch, "car"},
		{"password", modePasswordEntry, "hunter2"},
		{"key_list", modeKeyList, ""},
		{"delete_confirm", modeDeleteConfirm, ""},
		{"help", modeHelp, ""},
	}

	for _, size := range []struct{ w, h int }{{80, 25}, {120, 45}} {
		for _, st := range states {
			name := st.name + "_" + fmt.Sprintf("%dx%d", size.w, size.h)
			t.Run(name, func(t *testing.T) {
				m, _ := editorOver(t, goldenUsers()...)
				updated, _ := m.Update(tea.WindowSizeMsg{Width: size.w, Height: size.h})
				m = updated.(Model)
				m = pinBackdrop(m, m.width, m.height)
				m = enterMode(t, m, st.mode)
				for _, r := range st.typed {
					u, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
					m = u.(Model)
				}
				checkGolden(t, name, m.View())
			})
		}
	}
}
