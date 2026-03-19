package configeditor

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// updateCategoryMenu handles input for a generic category sub-menu.
func (m Model) updateCategoryMenu(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyUp:
		if m.catMenuCursor > 0 {
			m.catMenuCursor--
		}
	case tea.KeyDown:
		if m.catMenuCursor < len(m.catMenuItems)-1 {
			m.catMenuCursor++
		}
	case tea.KeyHome:
		m.catMenuCursor = 0
	case tea.KeyEnd:
		m.catMenuCursor = len(m.catMenuItems) - 1
	case tea.KeyEnter:
		return m.selectCategoryMenuItem()
	case tea.KeyEscape:
		m.mode = modeTopMenu
		return m, nil
	default:
		key := strings.ToUpper(msg.String())
		if key == "Q" {
			m.mode = modeTopMenu
			return m, nil
		}
		if len(key) == 1 && key[0] >= '1' && key[0] <= '9' {
			idx := int(key[0] - '1')
			if idx < len(m.catMenuItems) {
				m.catMenuCursor = idx
				return m.selectCategoryMenuItem()
			}
		}
	}
	return m, nil
}

// selectCategoryMenuItem handles selection of a category sub-menu item.
func (m Model) selectCategoryMenuItem() (Model, tea.Cmd) {
	if m.catMenuCursor < 0 || m.catMenuCursor >= len(m.catMenuItems) {
		return m, nil
	}
	item := m.catMenuItems[m.catMenuCursor]

	// If the item specifies a special mode, transition to it.
	if item.Mode != 0 {
		m.returnMode = modeCategoryMenu
		m.mode = item.Mode
		return m, nil
	}

	// Otherwise open the record list for this record type.
	if item.RecordType != "" {
		m.recordType = item.RecordType
		m.recordCursor = 0
		m.recordScroll = 0
		m.returnMode = modeCategoryMenu
		m.mode = modeRecordList
	}
	return m, nil
}
