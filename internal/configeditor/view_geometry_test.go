package configeditor

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

// TestViewGeometry checks that every screen the config editor can draw fills
// exactly the terminal it was told it has: the audit treats ./config as a
// reference to inspect, not as known-good.
func TestViewGeometry(t *testing.T) {
	sizes := []struct{ w, h int }{
		{80, 25}, {100, 30}, {120, 45}, {160, 60}, {60, 15}, {200, 100},
	}
	modes := []struct {
		name string
		mode editorMode
	}{
		{"top_menu", modeTopMenu},
		{"sys_config_menu", modeSysConfigMenu},
		{"exit_confirm", modeExitConfirm},
		{"save_confirm", modeSaveConfirm},
		{"quit_confirm", modeQuitConfirm},
		{"help", modeHelp},
	}

	for _, size := range sizes {
		for _, mc := range modes {
			t.Run(mc.name+"_"+sizeLabel(size.w, size.h), func(t *testing.T) {
				m := newGeometryModel(t)
				updated, _ := m.Update(tea.WindowSizeMsg{Width: size.w, Height: size.h})
				m = updated.(Model)
				m.mode = mc.mode

				out := m.View()
				lines := strings.Split(out, "\n")
				if len(lines) != m.height {
					t.Errorf("view has %d rows, want %d", len(lines), m.height)
				}
				for i, line := range lines {
					if w := screenCells(line); w != m.width {
						t.Errorf("row %d: width %d, want exactly %d", i, w, m.width)
					}
				}
			})
		}
	}
}

// TestViewGeometryRecordScreens extends the geometry check to the record list
// and the field editors, the screens closest in shape to the string editor's
// list. These carry real config data, so they are driven through New rather
// than a hand-built model.
func TestViewGeometryRecordScreens(t *testing.T) {
	sizes := []struct{ w, h int }{
		{80, 25}, {100, 30}, {120, 45}, {160, 60}, {60, 15}, {200, 100},
	}
	screens := []struct {
		name  string
		setup func(Model) Model
	}{
		{"record_list_doors", func(m Model) Model { m.recordType = "door"; m.mode = modeRecordList; return m }},
		{"record_list_protocols", func(m Model) Model { m.recordType = "protocol"; m.mode = modeRecordList; return m }},
		{"record_list_events", func(m Model) Model { m.recordType = "event"; m.mode = modeRecordList; return m }},
		{"record_reorder", func(m Model) Model { m.recordType = "door"; m.mode = modeRecordReorder; return m }},
		{"record_edit", func(m Model) Model { m.recordType = "door"; m.mode = modeRecordEdit; return m }},
		{"record_field", func(m Model) Model { m.recordType = "door"; m.mode = modeRecordField; return m }},
		{"delete_confirm", func(m Model) Model { m.recordType = "door"; m.mode = modeDeleteConfirm; return m }},
		{"sys_config_edit", func(m Model) Model { m.mode = modeSysConfigEdit; return m }},
		{"sys_config_field", func(m Model) Model { m.mode = modeSysConfigField; return m }},
		{"category_menu", func(m Model) Model { m.mode = modeCategoryMenu; return m }},
	}

	for _, size := range sizes {
		for _, sc := range screens {
			t.Run(sc.name+"_"+sizeLabel(size.w, size.h), func(t *testing.T) {
				base, err := New("testdata")
				if err != nil {
					t.Skipf("New(testdata): %v", err)
				}
				updated, _ := base.Update(tea.WindowSizeMsg{Width: size.w, Height: size.h})
				m := sc.setup(updated.(Model))

				out := m.View()
				lines := strings.Split(out, "\n")
				if len(lines) != m.height {
					t.Errorf("view has %d rows, want %d", len(lines), m.height)
				}
				for i, line := range lines {
					if w := screenCells(line); w != m.width {
						t.Errorf("row %d: width %d, want exactly %d", i, w, m.width)
					}
				}
			})
		}
	}
}

func sizeLabel(w, h int) string {
	return itoaCells(w) + "x" + itoaCells(h)
}

func itoaCells(n int) string {
	if n == 0 {
		return "0"
	}
	var d []byte
	for n > 0 {
		d = append([]byte{byte('0' + n%10)}, d...)
		n /= 10
	}
	return string(d)
}

// newGeometryModel builds a minimal Model sufficient to render the menu and
// dialog screens.
func newGeometryModel(t *testing.T) Model {
	t.Helper()
	art := pickBackdropArt()
	return Model{
		topItems:     []topMenuItem{{"1", "System Setup"}, {"Q", "Quit Program"}},
		sysMenuItems: systemConfigMenuItems(),
		sysMenuTitle: "System Setup",
		width:        minWidth,
		height:       minHeight,
		mode:         modeTopMenu,
		backdropArt:  art,
		backdrop:     loadBackdropFrom(art, minWidth, minHeight),
	}
}
