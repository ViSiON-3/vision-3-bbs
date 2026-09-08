package menueditor

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mattn/go-runewidth"
)

// screenCells returns the width in terminal cells of s, ignoring ANSI escapes.
func screenCells(s string) int {
	inEsc := false
	n := 0
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
		n += runewidth.RuneWidth(r)
	}
	return n
}

func sizeLabel(w, h int) string {
	return strings.ReplaceAll(strings.TrimSpace(itoa(w)+"x"+itoa(h)), " ", "")
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

// enterMode drives real keystrokes from the menu list into the target mode.
//
// The modes must be reached by keystroke rather than by assigning m.mode: the
// screens that host a text input size their rows from m.textInput.Width, which
// only the real transition sets. Assigning the mode leaves the input at its
// constructor width and reports failures that are artifacts of the test.
func enterMode(t *testing.T, m Model, mode editorMode) Model {
	t.Helper()
	switch mode {
	case modeMenuList:
	case modeMenuEdit:
		m = press(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	case modeMenuEditField:
		m = press(t, m, tea.KeyMsg{Type: tea.KeyEnter})
		m = press(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	case modeAddMenu:
		m = press(t, m, tea.KeyMsg{Type: tea.KeyF5})
	case modeDeleteMenuConfirm:
		m = press(t, m, tea.KeyMsg{Type: tea.KeyF2})
	case modeCommandList:
		m = press(t, m, tea.KeyMsg{Type: tea.KeyF10})
	case modeCommandEdit:
		m = press(t, m, tea.KeyMsg{Type: tea.KeyF10})
		m = press(t, m, tea.KeyMsg{Type: tea.KeyF5})
	case modeCommandEditField:
		m = press(t, m, tea.KeyMsg{Type: tea.KeyF10})
		m = press(t, m, tea.KeyMsg{Type: tea.KeyF5})
		m = press(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	case modeDeleteCmdConfirm:
		m = press(t, m, tea.KeyMsg{Type: tea.KeyF10})
		m = press(t, m, tea.KeyMsg{Type: tea.KeyF5})
		m = press(t, m, tea.KeyMsg{Type: tea.KeyEscape})
		m = press(t, m, tea.KeyMsg{Type: tea.KeyF2})
	default:
		m.mode = mode
	}
	if m.mode != mode {
		t.Fatalf("keystrokes landed in mode %v, want %v", m.mode, mode)
	}
	return m
}

// TestViewGeometry checks that every screen the menu editor can draw fills
// exactly the terminal it was told it has, matching the guarantee ./config and
// ./strings already carry.
//
// Three screens failed this before the audit: the menu edit and command edit
// screens each emitted one row too many, pushing content off the bottom and
// scrolling the terminal, and the command list emitted one row too few on any
// terminal taller than 25.
func TestViewGeometry(t *testing.T) {
	sizes := []struct{ w, h int }{
		{80, 25}, {100, 30}, {120, 45}, {160, 60}, {60, 15}, {200, 100},
	}
	modes := []struct {
		name string
		mode editorMode
	}{
		{"menu_list", modeMenuList},
		{"menu_edit", modeMenuEdit},
		{"menu_edit_field", modeMenuEditField},
		{"add_menu", modeAddMenu},
		{"delete_menu_confirm", modeDeleteMenuConfirm},
		{"command_list", modeCommandList},
		{"command_edit", modeCommandEdit},
		{"command_edit_field", modeCommandEditField},
		{"delete_cmd_confirm", modeDeleteCmdConfirm},
		{"exit_confirm", modeExitConfirm},
		{"help", modeHelp},
	}

	for _, size := range sizes {
		for _, mc := range modes {
			t.Run(mc.name+"_"+sizeLabel(size.w, size.h), func(t *testing.T) {
				m := newTestEditor(t)
				m = asModel(t, mustUpdate(m.Update(tea.WindowSizeMsg{Width: size.w, Height: size.h})))
				m = enterMode(t, m, mc.mode)

				out := m.View()
				lines := strings.Split(out, "\n")
				if len(lines) != m.height {
					t.Fatalf("rendered %d rows, want %d", len(lines), m.height)
				}
				for i, line := range lines {
					if got := screenCells(line); got != m.width {
						t.Errorf("row %d is %d cells wide, want %d", i, got, m.width)
					}
				}
			})
		}
	}
}

func mustUpdate(m tea.Model, _ tea.Cmd) tea.Model { return m }
