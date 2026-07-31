package configeditor

import (
	tea "github.com/charmbracelet/bubbletea"
)

// Record Edit mode: moving between a record's fields before one is opened for
// editing.

func (m Model) updateRecordEdit(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if len(m.recordFields) == 0 {
		if msg.Type == tea.KeyEscape {
			m.mode = modeRecordList
		}
		return m, nil
	}

	switch msg.Type {
	case tea.KeyTab, tea.KeyEnter:
		f := m.recordFields[m.editField]
		if f.Type == ftDisplay {
			// V3Net hub network "Areas" field opens the area manager.
			if m.recordType == "v3nethub" && f.Label == "Areas" {
				if m.recordEditIdx >= 0 && m.recordEditIdx < len(m.configs.V3Net.Hub.Networks) {
					netName := m.configs.V3Net.Hub.Networks[m.recordEditIdx].Name
					return m.enterHubAreaManager(netName)
				}
			}
			// V3Net leaf "Browse Areas" field opens the area browser.
			if m.recordType == "v3netleaf" && f.Label == "Browse Areas" {
				if m.recordEditIdx >= 0 && m.recordEditIdx < len(m.configs.V3Net.Leaves) {
					leaf := m.configs.V3Net.Leaves[m.recordEditIdx]
					return m.enterAreaBrowser(leaf.HubURL, leaf.Network, modeRecordEdit)
				}
			}
			m.editField = m.nextRecordEditableField(1)
			m.clampFieldScroll(m.recordFields)
			return m, nil
		}
		if f.Type == ftYesNo {
			m.toggleYesNo(f)
			return m, nil
		}
		return m.startRecordFieldEdit()

	case tea.KeySpace:
		f := m.recordFields[m.editField]
		if f.Type == ftYesNo {
			m.toggleYesNo(f)
		}
		return m, nil

	case tea.KeyDown:
		m.editField = m.nextRecordEditableField(1)
		m.clampFieldScroll(m.recordFields)

	case tea.KeyUp:
		m.editField = m.nextRecordEditableField(-1)
		m.clampFieldScroll(m.recordFields)

	case tea.KeyEscape:
		// V3Net hub/leaf edits prompt to save before leaving.
		if m.recordType == "v3nethub" || m.recordType == "v3netleaf" {
			return m.promptNavSave(modeRecordList)
		}
		m.mode = modeRecordList
		return m, nil

	case tea.KeyPgDown:
		total := m.recordCount()
		if m.recordEditIdx >= 0 && total > 0 && m.recordEditIdx < total-1 {
			m.recordEditIdx++
			m.recordFields = m.buildRecordFields()
			m.editField = 0
			m.fieldScroll = 0
		}
		return m, nil

	case tea.KeyPgUp:
		if m.recordEditIdx > 0 {
			m.recordEditIdx--
			m.recordFields = m.buildRecordFields()
			m.editField = 0
			m.fieldScroll = 0
		}
		return m, nil
	}
	return m, nil
}

func (m Model) nextRecordEditableField(dir int) int {
	n := len(m.recordFields)
	if n == 0 {
		return 0
	}
	idx := m.editField + dir
	if idx > n-1 {
		idx = 0
	} else if idx < 0 {
		idx = n - 1
	}
	return idx
}
