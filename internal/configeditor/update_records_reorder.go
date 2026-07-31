package configeditor

import (
	tea "github.com/charmbracelet/bubbletea"
)

// Record Reorder mode: moving a record up or down within its list, for the
// record types whose on-disk order is meaningful.

// recordTypeSupportsReorder returns true if the current record type supports P-key reordering.
func (m Model) recordTypeSupportsReorder() bool {
	switch m.recordType {
	case "msgarea", "filearea", "conference", "login", "protocol", "archiver":
		return true
	}
	return false
}

func (m Model) updateRecordReorder(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	listVisible := m.recordListVisible()
	lo := m.reorderMinIdx
	hi := m.reorderMaxIdx

	switch msg.Type {
	case tea.KeyUp:
		if m.recordCursor > lo {
			m.recordCursor--
		}
	case tea.KeyDown:
		if m.recordCursor < hi {
			m.recordCursor++
		}
	case tea.KeyHome:
		m.recordCursor = lo
	case tea.KeyEnd:
		m.recordCursor = hi
	case tea.KeyPgUp:
		m.recordCursor -= listVisible
		if m.recordCursor < lo {
			m.recordCursor = lo
		}
	case tea.KeyPgDown:
		m.recordCursor += listVisible
		if m.recordCursor > hi {
			m.recordCursor = hi
		}
	case tea.KeyEnter:
		m.reorderRecord()
		m.dirty = true
		m.reorderSourceIdx = -1
		m.mode = modeRecordList
		m.clampRecordScroll()
		return m, nil
	case tea.KeyEscape:
		m.recordCursor = m.reorderSourceIdx
		m.reorderSourceIdx = -1
		m.mode = modeRecordList
		m.clampRecordScroll()
		return m, nil
	}
	m.clampRecordScroll()
	return m, nil
}
