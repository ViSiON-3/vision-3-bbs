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

// TestViewGeometryWithFilledInputs re-runs the width check with each text input
// carrying a value that fills, and overflows, its field.
//
// TestViewGeometry drives real keystrokes but types nothing, so every input it
// renders is empty. That blind spot hid two real overruns in ./menuedit, where
// bubbles/textinput's trailing cursor cell pushed a filled field past its box.
// No ./ue field is wide enough to do the same today; this pins that it stays
// true as fields are added or widened.
func TestViewGeometryWithFilledInputs(t *testing.T) {
	sizes := []struct{ w, h int }{{80, 25}, {120, 45}}
	cases := []struct {
		name  string
		mode  editorMode
		typed string
	}{
		{"edit_field", modeEditField, strings.Repeat("X", 80)},
		{"password", modePasswordEntry, strings.Repeat("P", 80)},
		{"search", modeSearch, strings.Repeat("S", 40)},
	}

	for _, size := range sizes {
		for _, mc := range cases {
			t.Run(mc.name+"_"+fmt.Sprintf("%dx%d", size.w, size.h), func(t *testing.T) {
				m, _ := editorOver(t,
					&user.User{ID: 1, Handle: "Alice", AccessLevel: 10},
					&user.User{ID: 2, Handle: "Bob", AccessLevel: 20})
				updated, _ := m.Update(tea.WindowSizeMsg{Width: size.w, Height: size.h})
				m = updated.(Model)
				m = enterMode(t, m, mc.mode)
				for _, r := range mc.typed {
					u, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
					m = u.(Model)
				}

				lines := strings.Split(m.View(), "\n")
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

// The edit screen's columns are 42 and boxW-42 cells. A field whose label plus
// value exceeds its column overruns the row, because renderEditRow can only
// skip the padding, not claw back the content. Pin that every field still fits.
func TestEveryFieldFitsItsColumn(t *testing.T) {
	m, _ := editorOver(t, &user.User{ID: 1, Handle: "Alice"})
	const boxW, leftW = 76, 42
	for _, f := range m.fields {
		budget := leftW
		if f.Col == rightCol {
			budget = boxW - leftW
		}
		labelW := 14
		if f.Col == rightCol {
			labelW = 13
		}
		need := labelW + len(" : ") + f.Width
		if need > budget {
			t.Errorf("field %q needs %d cells but its column allows %d", f.Label, need, budget)
		}
	}
}
