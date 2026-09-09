package menueditor

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/ViSiON-3/vision-3-bbs/internal/tuiart"
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
		// filled input and not only the empty one. Two overruns hid behind
		// empty inputs during this audit.
		typed string
	}{
		{"menu_list", modeMenuList, ""},
		{"menu_edit", modeMenuEdit, ""},
		{"menu_edit_field", modeMenuEditField, "NEWTITLE"},
		{"command_list", modeCommandList, ""},
		{"command_edit", modeCommandEdit, ""},
		{"add_menu", modeAddMenu, "NEWMENU"},
		{"delete_menu_confirm", modeDeleteMenuConfirm, ""},
		{"help", modeHelp, ""},
	}

	for _, size := range []struct{ w, h int }{{80, 25}, {120, 45}} {
		for _, st := range states {
			name := st.name + "_" + sizeLabel(size.w, size.h)
			t.Run(name, func(t *testing.T) {
				m := newTestEditor(t)
				m = asModel(t, mustUpdate(m.Update(tea.WindowSizeMsg{Width: size.w, Height: size.h})))
				m = pinBackdrop(m, m.width, m.height)
				m = enterMode(t, m, st.mode)
				if st.typed != "" {
					m = typeText(t, m, st.typed)
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
var mixedBorderPairs = []string{
	"┌╔", "╗┐", "└╚", "╝┘", "│║", "║│", "─═", "═─",
}

// TestNoMixedBorderPairs checks every archived capture for that artifact.
//
// The help overlay had it: at 50 columns the dialog was centred within a column
// of the 52-column menu list box, so the list's side borders showed through.
// The geometry tests all passed — every row was exactly the right width, with
// the wrong characters in it.
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
			plain := stripANSI(string(b))
			for _, pair := range mixedBorderPairs {
				if strings.Contains(plain, pair) {
					t.Errorf("capture contains %q: a dialog is leaving a sliver of the panel behind it", pair)
				}
			}
		})
	}
}

// stripANSI removes SGR escape sequences so the box characters can be compared
// as plain text.
func stripANSI(s string) string {
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
