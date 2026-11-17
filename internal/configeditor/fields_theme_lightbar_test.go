package configeditor

import (
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/ViSiON-3/vision-3-bbs/internal/menuset"
)

// lightbarPage opens Theme → Lightbar Colors on an 80-column screen.
func lightbarPage(t *testing.T, root string) Model {
	t.Helper()
	m, err := New(filepath.Join(root, "configs"))
	if err != nil {
		t.Fatal(err)
	}
	nm, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 25})
	return press(t, nm.(Model), "C", "3")
}

// fitsHelp reports whether help shows whole in the two help lines.
func fitsHelp(help string) bool {
	l1, l2 := wrapHelpTwoLines(help, 71)
	return strings.Join(strings.Fields(l1+" "+l2), " ") == help
}

// With a *HI.BAR present, the Lightbar Colors page names the screens that
// don't follow Selected (#1091); with none, or only inherit markers, it
// reads as before.
func TestLightbarPageNamesScreenOverrides(t *testing.T) {
	_, root := newThemeModel(t)
	writeBar(t, root, "menus/v3/bar/MSGCONFHI.BAR", shippedHi)
	writeBar(t, root, "menus.d/v3/bar/MSGCONFHI.BAR", menuset.BarInheritFile)
	m := lightbarPage(t, root)
	if len(m.sysFields) != 9 || !strings.HasPrefix(m.sysFields[0].Help, "The highlighted row or command on every lightbar;") {
		t.Fatalf("no override: %d fields, Selected help %q", len(m.sysFields), m.sysFields[0].Help)
	}

	writeBar(t, root, "menus/v3/bar/FILELISTHI.BAR", shippedHi)
	writeBar(t, root, "menus.d/v3/bar/MSGAREAHI.BAR", sysopHi)
	m = lightbarPage(t, root)
	if len(m.sysFields) != 10 {
		t.Fatalf("%d fields, want the roles and the note", len(m.sysFields))
	}
	note := m.sysFields[9]
	if note.Label != "Overridden by" || note.Type != ftDisplay || note.Get() != "2 screens: File List, Message Areas" {
		t.Errorf("note %q = %q", note.Label, note.Get())
	}
	if !strings.Contains(note.Help, "Screen Overrides") || strings.Contains(note.Help, "Door") {
		t.Errorf("note help %q", note.Help)
	}
	if !strings.Contains(m.sysFields[0].Help, "without a screen override") {
		t.Errorf("Selected help %q", m.sysFields[0].Help)
	}
	wantScreen(t, m, "Overridden by", "2 screens: File List, Message Areas")
	for i, line := range strings.Split(m.View(), "\n") {
		if w := lipgloss.Width(line); w > 80 {
			t.Errorf("line %d is %d wide", i, w)
		}
	}
}

// A screen chosen on Screen Overrides to inherit, not yet saved, no longer
// counts: once saved it follows Selected.
func TestLightbarPageLeavesOutPendingInherits(t *testing.T) {
	_, root := newThemeModel(t)
	writeBar(t, root, "menus/v3/bar/FILELISTHI.BAR", shippedHi)
	writeBar(t, root, "menus/v3/bar/MSGAREAHI.BAR", shippedHi)
	m := press(t, lightbarPage(t, root), "esc", "4", "enter", "esc", "3")
	if len(m.sysFields) != 10 || m.sysFields[9].Get() != "1 screen: Message Areas" {
		t.Errorf("fields %d, note %q", len(m.sysFields), m.sysFields[len(m.sysFields)-1].Get())
	}
	m = press(t, m, "esc", "4", "down", "enter", "esc", "3")
	if len(m.sysFields) != 9 {
		t.Errorf("both pending inherit: %d fields, want no note", len(m.sysFields))
	}
}

// With every screen overridden the names don't fit the line, so it gives
// the count and the help names them; the door menu's regular colour is
// mentioned where DOORMENUHI.BAR is one of them. Both fit 80 columns.
func TestLightbarPageNoteForEveryScreen(t *testing.T) {
	var bars []screenBar
	for _, sb := range menuset.ScreenBars {
		bars = append(bars, screenBar{ScreenBar: sb, Highlight: 31})
	}
	note := screenOverridesNote(bars, 11)
	if note.Get() != "5 screens, see Screen Overrides" {
		t.Errorf("note = %q", note.Get())
	}
	for _, sb := range menuset.ScreenBars {
		if !strings.Contains(note.Help, sb.Screen) {
			t.Errorf("help %q doesn't name %s", note.Help, sb.Screen)
		}
	}
	if !strings.Contains(note.Help, "Door Menu's also sets its regular colour") || !fitsHelp(note.Help) {
		t.Errorf("help %q: want the door note, whole in two lines", note.Help)
	}
	if one := screenOverridesNote(bars[4:], 11); one.Get() != "1 screen: Door Menu" || !fitsHelp(one.Help) {
		t.Errorf("door only: %q, help %q", one.Get(), one.Help)
	}

	_, root := newThemeModel(t)
	for _, sb := range menuset.ScreenBars {
		writeBar(t, root, "menus/v3/bar/"+sb.Name+".BAR", shippedHi)
	}
	m := lightbarPage(t, root)
	for i, line := range strings.Split(m.View(), "\n") {
		if w := lipgloss.Width(line); w > 80 {
			t.Errorf("line %d is %d wide", i, w)
		}
	}
	wantScreen(t, m, "5 screens, see Screen Overrides")
}
