package usereditor

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mattn/go-runewidth"

	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// Double-width characters in a handle or location must not push a row past the
// terminal, on the list screen or the edit screen.
func TestWideTextKeepsRowsInBounds(t *testing.T) {
	wide := strings.Repeat("界", 40)
	m, _ := editorOver(t,
		&user.User{ID: 1, Handle: "Sysop", AccessLevel: 255},
		&user.User{ID: 2, Handle: wide, RealName: wide, GroupLocation: wide, PrivateNote: wide},
	)
	check := func(name string, m Model) {
		t.Helper()
		for i, line := range strings.Split(stripANSIGolden(m.View()), "\n") {
			if w := runewidth.StringWidth(line); w > 80 {
				t.Errorf("%s row %d is %d cells wide: %q", name, i, w, line)
			}
		}
	}
	m = press(t, m, tea.WindowSizeMsg{Width: 80, Height: 25}, key(tea.KeyDown))
	check("list", m)
	m = press(t, m, key(tea.KeyEnter))
	check("edit", m)
	m = press(t, m, key(tea.KeyDown))
	check("edit, field highlighted", m)
}
