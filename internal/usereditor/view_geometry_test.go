package usereditor

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mattn/go-runewidth"

	"github.com/ViSiON-3/vision-3-bbs/internal/user"
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

// enterMode drives real keystrokes from the user list into the target mode.
//
// The field-edit mode must be reached by keystroke rather than by assigning
// m.mode: its row is sized from what m.textInput renders, which only the real
// transition configures. Assigning the mode leaves the input at its constructor
// width and reports a failure that is an artifact of the test.
func enterMode(t *testing.T, m Model, mode editorMode) Model {
	t.Helper()
	press := func(k tea.KeyType) {
		t.Helper()
		updated, _ := m.Update(tea.KeyMsg{Type: k})
		m = updated.(Model)
	}
	switch mode {
	case modeList:
	case modeEdit:
		press(tea.KeyEnter)
	case modeEditField:
		press(tea.KeyEnter)
		press(tea.KeyEnter)
	case modePasswordEntry:
		press(tea.KeyEnter)
		for i := 0; i < len(m.fields); i++ {
			if m.fields[m.editField].Label == "Password" {
				break
			}
			m.editField = m.verticalField(1)
		}
		press(tea.KeyEnter)
	case modeKeyList:
		press(tea.KeyEnter)
		for i := 0; i < len(m.fields); i++ {
			if m.fields[m.editField].Label == "WFC Keys" {
				break
			}
			m.editField = m.verticalField(1)
		}
		press(tea.KeyEnter)
	case modeSearch:
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
		m = updated.(Model)
	default:
		m.mode = mode
		m.alertTitle, m.alertMessage = "-- Alert --", "message"
	}
	if m.mode != mode {
		t.Fatalf("keystrokes landed in mode %v, want %v", m.mode, mode)
	}
	return m
}

// TestViewGeometry checks that every screen the user editor can draw fills
// exactly the terminal it was told it has, matching the guarantee ./config and
// ./strings already carry.
//
// ./ue passed this before the audit and must keep passing after adopting the
// backdrop: sourcing the background from art rather than a flat fill changes
// how every margin column is produced.
func TestViewGeometry(t *testing.T) {
	sizes := []struct{ w, h int }{
		{80, 25}, {100, 30}, {120, 45}, {160, 60}, {60, 15}, {200, 100},
	}
	modes := []struct {
		name string
		mode editorMode
	}{
		{"list", modeList},
		{"edit", modeEdit},
		{"edit_field", modeEditField},
		{"search", modeSearch},
		{"password", modePasswordEntry},
		{"key_list", modeKeyList},
		{"delete_confirm", modeDeleteConfirm},
		{"undelete_confirm", modeUndeleteConfirm},
		{"purge_confirm", modePurgeConfirm},
		{"mass_delete", modeMassDelete},
		{"validate", modeValidate},
		{"mass_validate", modeMassValidate},
		{"help", modeHelp},
		{"file_changed", modeFileChanged},
		{"exit_confirm", modeExitConfirm},
		{"exit_clean", modeExitClean},
		{"save_confirm", modeSaveConfirm},
		{"save_on_leave", modeSaveOnLeave},
		{"info_alert", modeInfoAlert},
	}

	for _, size := range sizes {
		for _, mc := range modes {
			t.Run(mc.name+"_"+fmt.Sprintf("%dx%d", size.w, size.h), func(t *testing.T) {
				m, _ := editorOver(t,
					&user.User{ID: 1, Handle: "Alice", AccessLevel: 10},
					&user.User{ID: 2, Handle: "Bob", AccessLevel: 20})
				updated, _ := m.Update(tea.WindowSizeMsg{Width: size.w, Height: size.h})
				m = updated.(Model)
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
