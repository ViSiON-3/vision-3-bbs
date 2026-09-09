package usereditor

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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

// mixedBorderPairs are adjacent single-line and double-line box characters.
// A dialog drawn over a panel of almost the same width leaves a one-column
// sliver of the panel's border showing, which renders as one of these pairs and
// reads as a broken frame rather than a dialog on a panel.
//
// ./ue has no such collision today — its dialogs are much narrower than the
// list (62) and edit (78) boxes. This pins that, because ./menuedit did have
// one and every geometry test passed over it: the rows were exactly the right
// width, with the wrong characters in them.
var mixedBorderPairs = []string{
	"┌╔", "╗┐", "└╚", "╝┘", "│║", "║│", "─═", "═─",
	"╒╔", "╗╕", "╘╚", "╝╛", "║│", "│║",
}

func TestNoMixedBorderPairs(t *testing.T) {
	entries, err := os.ReadDir("testdata")
	if err != nil {
		t.Fatalf("read testdata: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("no captures to check")
	}
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".golden" {
			continue
		}
		t.Run(e.Name(), func(t *testing.T) {
			b, err := os.ReadFile(filepath.Join("testdata", e.Name()))
			if err != nil {
				t.Fatal(err)
			}
			plain := stripANSIGolden(string(b))
			for _, pair := range mixedBorderPairs {
				if strings.Contains(plain, pair) {
					t.Errorf("capture contains %q: a dialog is leaving a sliver of the panel behind it", pair)
				}
			}
		})
	}
}

// stripANSIGolden removes SGR escape sequences so the box characters can be
// compared as plain text.
func stripANSIGolden(s string) string {
	var b strings.Builder
	inEsc := false
	for _, r := range s {
		if r == '\x1b' {
			inEsc = true
			continue
		}
		if inEsc {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				inEsc = false
			}
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}
