package configeditor

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

// openYesNo opens Theme -> Prompt Colors on Yes/No Highlight.
func openYesNo(t *testing.T) Model {
	t.Helper()
	m, _ := newThemeModel(t)
	m = press(t, m, "C", "1")
	if m.sysFields[m.editField].Label != "Yes/No Highlight" {
		t.Fatalf("field %q", m.sysFields[m.editField].Label)
	}
	return m
}

// Enter opens the picker on the field's colour; the arrows choose a
// foreground and a background, Enter sets the attribute, and the field
// is marked changed.
func TestYesNoColorPicked(t *testing.T) {
	m := openYesNo(t)
	m.configs.Theme.YesNoHighlightColor = 31 // white on blue
	m = press(t, m, "enter")
	if m.mode != modeColorAttr || m.attr.pick.FG != 15 {
		t.Fatalf("mode %v, fg %d", m.mode, m.attr.pick.FG)
	}
	wantScreen(t, m, "Yes/No Highlight", "Foreground:", "Background:", "Preview:", "= 31")
	// Foreground 15 -> 14 (yellow); background row, blue -> the next one.
	m = press(t, m, "left", "down", "right", "enter")
	got := m.configs.Theme.YesNoHighlightColor
	if got&0x0F != 14 || got>>4 == 1 || m.mode != modeSysConfigEdit || !m.dirty {
		t.Errorf("value %d, mode %v, dirty %v", got, m.mode, m.dirty)
	}
}

// Esc leaves the value as it was; T types the number, held to 0-255.
func TestYesNoColorEscAndTyped(t *testing.T) {
	m := openYesNo(t)
	m.configs.Theme.YesNoHighlightColor = 112
	m = press(t, m, "enter", "right", "esc")
	if m.configs.Theme.YesNoHighlightColor != 112 || m.dirty {
		t.Errorf("esc changed it: %d dirty %v", m.configs.Theme.YesNoHighlightColor, m.dirty)
	}
	m = press(t, m, "enter", "t")
	if m.mode != modeSysConfigField {
		t.Fatalf("T did not open the number: mode %v", m.mode)
	}
	m = replaceText(t, m, "300")
	m = press(t, m, "enter")
	if m.configs.Theme.YesNoHighlightColor != 112 {
		t.Errorf("an out-of-range number was taken: %d", m.configs.Theme.YesNoHighlightColor)
	}
	m = press(t, m, "esc")
	m = press(t, m, "enter", "t")
	m = replaceText(t, m, "79")
	m = press(t, m, "enter")
	if m.configs.Theme.YesNoHighlightColor != 79 {
		t.Errorf("typed 79, got %d", m.configs.Theme.YesNoHighlightColor)
	}
}

// The dialog fits an 80-column screen.
func TestYesNoColorFits80(t *testing.T) {
	m := press(t, openYesNo(t), "enter")
	m.width, m.height = 80, 25
	for i, line := range strings.Split(m.View(), "\n") {
		if w := lipgloss.Width(line); w > 80 {
			t.Errorf("line %d is %d wide", i, w)
		}
	}
}
