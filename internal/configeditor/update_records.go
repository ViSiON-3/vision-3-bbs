package configeditor

import (
	tea "github.com/charmbracelet/bubbletea"
)

// Record List mode: browsing the records of the selected type, and the scroll
// clamping that keeps the highlighted row on screen.

func (m Model) updateRecordList(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	total := m.recordCount()
	listVisible := m.recordListVisible()

	switch msg.Type {
	case tea.KeyUp:
		if m.recordCursor > 0 {
			m.recordCursor--
		}
	case tea.KeyDown:
		if m.recordCursor < total-1 {
			m.recordCursor++
		}
	case tea.KeyHome:
		m.recordCursor = 0
	case tea.KeyEnd:
		if total > 0 {
			m.recordCursor = total - 1
		}
	case tea.KeyPgUp:
		m.recordCursor -= listVisible
		if m.recordCursor < 0 {
			m.recordCursor = 0
		}
	case tea.KeyPgDown:
		m.recordCursor += listVisible
		if m.recordCursor >= total {
			m.recordCursor = total - 1
		}
		if m.recordCursor < 0 {
			m.recordCursor = 0
		}
	case tea.KeyEnter:
		if total > 0 {
			m.recordEditIdx = m.recordCursor
			m.recordFields = m.buildRecordFields()
			m.editField = 0
			m.fieldScroll = 0
			m.mode = modeRecordEdit
		}
		return m, nil
	case tea.KeyEscape:
		// V3Net hub/leaf lists prompt to save before leaving.
		if m.recordType == "v3nethub" || m.recordType == "v3netleaf" {
			dest := m.backMode()
			return m.promptNavSave(dest)
		}
		m.mode = m.backMode()
		return m, nil
	default:
		switch msg.String() {
		case "i", "I", "insert":
			// V3Net records launch their setup wizard instead of raw insert.
			if m.recordType == "v3netleaf" {
				return m.enterLeafWizard()
			}
			if m.recordType == "v3nethub" {
				return m.enterHubWizard()
			}
			// For ftn, the network keys before the insert: the new one is
			// whichever key appears afterwards that was not there before.
			var ftnKeysBefore map[string]bool
			if m.recordType == "ftn" {
				ftnKeysBefore = make(map[string]bool, len(m.configs.FTN.Networks))
				for k := range m.configs.FTN.Networks {
					ftnKeysBefore[k] = true
				}
			}
			m.insertRecord()
			m.dirty = true
			switch m.recordType {
			case "ftnlink":
				// The new link is appended to the first (sorted) network, not
				// necessarily at the end of the flat list; point at it directly.
				nets := m.ftnNetworkKeys()
				if len(nets) > 0 {
					m.recordCursor = len(m.configs.FTN.Networks[nets[0]].Links) - 1
				}
			case "ftn":
				// The placeholder (zz_newnet_N) is not reliably the last sorted
				// key: zz_newnet_10 sorts before zz_newnet_9, and a real network
				// named zzznet sorts after all of them. Pointing at the last row
				// then opened an existing network, and the Network Name edit
				// that followed renamed that one instead.
				m.recordCursor = m.recordCount() - 1
				for i, k := range m.ftnNetworkKeys() {
					if !ftnKeysBefore[k] {
						m.recordCursor = i
						break
					}
				}
			default:
				m.recordCursor = m.recordCount() - 1
			}
			m.clampRecordScroll()
			// An echomail network is created under a placeholder name
			// (zz_newnet_N) that is useless until renamed, and that name is the
			// binkd domain every address, area and node line then hangs off.
			// Returning to the list leaves the placeholder sitting there
			// looking like a configured network, so open the new record with
			// the cursor on the field that has to be filled in.
			if m.recordType == "ftn" && m.recordCursor >= 0 {
				m.recordEditIdx = m.recordCursor
				m.recordFields = m.buildRecordFields()
				m.editField = 0
				m.fieldScroll = 0
				m.mode = modeRecordEdit
			}
			return m, nil
		case "g", "G":
			if m.recordType == "ftn" {
				m.recordFields = m.fieldsFTNGlobal()
				m.editField = 0
				m.fieldScroll = 0
				m.recordEditIdx = -1
				m.mode = modeRecordEdit
			}
			return m, nil
		case "d", "D", "delete":
			if total > 0 {
				m.mode = modeDeleteConfirm
				m.confirmYes = false
			}
			return m, nil
		case "s", "S":
			if (m.recordType == "v3nethub" || m.recordType == "v3netleaf") && m.dirty {
				m.saveAll()
			}
			return m, nil
		case "b", "B":
			if m.recordType == "v3netleaf" {
				return m.enterRegistryBrowserForLeafList()
			}
			return m, nil
		case "n", "N":
			if m.recordType == "v3nethub" && total > 0 {
				return m.enterNodeManagement(m.configs.V3Net.Hub.Networks[m.recordCursor].Name)
			}
			return m, nil
		case "p", "P":
			if total > 0 && m.recordTypeSupportsReorder() {
				m.reorderSourceIdx = m.recordCursor
				m.reorderMinIdx = 0
				m.reorderMaxIdx = total - 1

				// For message areas, clamp to the conference of the source item.
				if m.recordType == "msgarea" && m.recordCursor < len(m.configs.MsgAreas) {
					confID := m.configs.MsgAreas[m.recordCursor].ConferenceID
					lo, hi := m.recordCursor, m.recordCursor
					for lo > 0 && m.configs.MsgAreas[lo-1].ConferenceID == confID {
						lo--
					}
					for hi < len(m.configs.MsgAreas)-1 && m.configs.MsgAreas[hi+1].ConferenceID == confID {
						hi++
					}
					m.reorderMinIdx = lo
					m.reorderMaxIdx = hi
				}

				m.mode = modeRecordReorder
			}
			return m, nil
		}
	}
	m.clampRecordScroll()
	return m, nil
}

func (m *Model) clampRecordScroll() {
	total := m.recordCount()
	visible := m.recordListVisible()
	scrollThreshold := visible * 2 / 3

	if m.recordCursor < m.recordScroll {
		m.recordScroll = m.recordCursor
	}
	if m.recordCursor >= m.recordScroll+scrollThreshold {
		m.recordScroll = m.recordCursor - scrollThreshold
	}
	maxOffset := total - visible
	if maxOffset < 0 {
		maxOffset = 0
	}
	if m.recordScroll > maxOffset {
		m.recordScroll = maxOffset
	}
	if m.recordScroll < 0 {
		m.recordScroll = 0
	}
}
