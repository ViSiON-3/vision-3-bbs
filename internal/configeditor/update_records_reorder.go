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

	target := m.recordCursor
	switch msg.Type {
	case tea.KeyUp:
		target = m.recordCursor - 1
	case tea.KeyDown:
		target = m.recordCursor + 1
	case tea.KeyHome:
		target = lo
	case tea.KeyEnd:
		target = hi
	case tea.KeyPgUp:
		target = m.recordCursor - listVisible
	case tea.KeyPgDown:
		target = m.recordCursor + listVisible
	case tea.KeyEnter:
		// Commit. The slice is already in its final order from the live moves;
		// renumber positions once. Only if the item actually ended up somewhere
		// new — pressing P then Enter without moving (or after moving back) must
		// not renumber or dirty the config.
		if m.recordCursor != m.reorderSourceIdx {
			m.renumberReorderedPositions()
			m.dirty = true
		}
		m.reorderSourceIdx = -1
		m.mode = modeRecordList
		m.clampRecordScroll()
		return m, nil
	case tea.KeyEscape:
		// Cancel: carry the item back to where it started, restoring the
		// original order. Positions were never touched, so nothing else to undo.
		m.moveRecordSlice(m.recordCursor, m.reorderSourceIdx)
		m.recordCursor = m.reorderSourceIdx
		m.reorderSourceIdx = -1
		m.mode = modeRecordList
		m.clampRecordScroll()
		return m, nil
	default:
		return m, nil
	}

	// Move the item to the clamped target so it travels with the cursor and is
	// visibly reordered on screen (the row is painted green at recordCursor).
	if target < lo {
		target = lo
	}
	if target > hi {
		target = hi
	}
	if target != m.recordCursor {
		m.moveRecordSlice(m.recordCursor, target)
		m.recordCursor = target
	}
	m.clampRecordScroll()
	return m, nil
}
