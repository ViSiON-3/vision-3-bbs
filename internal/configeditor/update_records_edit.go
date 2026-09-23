package configeditor

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
)

// Record Edit mode: moving between a record's fields before one is opened for
// editing.

// currentDoor returns the door record being edited, or false if the editor is
// not on one.
func (m Model) currentDoor() (config.DoorConfig, bool) {
	if m.recordType != "door" {
		return config.DoorConfig{}, false
	}
	keys := m.doorKeys()
	if m.recordEditIdx < 0 || m.recordEditIdx >= len(keys) {
		return config.DoorConfig{}, false
	}
	return m.configs.Doors[keys[m.recordEditIdx]], true
}

// doorExitWarning is the message shown when a sysop leaves a door record that
// will not run as configured, or "" when there is nothing to say.
//
// It warns rather than refusing. Blocking the exit would strand a sysop in a
// half-made record, and blocking the save -- which is where this check started
// -- let one mistyped door stop unrelated FTN and V3Net work from being
// written at all. Telling them here, while they are looking at the record and
// one keystroke from the offending field, is where the warning is worth the
// most and costs the least.
func doorExitWarning(d config.DoorConfig, ok bool) string {
	if !ok {
		return ""
	}
	if err := d.ValidateRLogin(); err != nil {
		// Leaving a record does not write anything -- the change sits in
		// memory until the sysop saves -- so the wording must not suggest it
		// has already reached disk.
		return fmt.Sprintf("WARNING: %v. It can still be saved, but the door will fail when a caller opens it.", err)
	}
	return ""
}

// warnIfLeavingUnrunnableDoor posts the warning for the door record being left,
// if it is one and it will not run. Every way out of a record calls it: Escape
// returns to the list, and PageUp/PageDown move to another record, so keying it
// to one of them would let a sysop page straight past the problem.
//
// A record that is fine clears any warning left by a previous one, so the
// message never outlives the record it describes.
func (m *Model) warnIfLeavingUnrunnableDoor() {
	if m.recordType != "door" {
		return
	}
	if warning := doorExitWarning(m.currentDoor()); warning != "" {
		m.message = warning
	} else if strings.HasPrefix(m.message, "WARNING: door ") {
		m.message = ""
	}
}

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
		m.warnIfLeavingUnrunnableDoor()
		m.mode = modeRecordList
		return m, nil

	case tea.KeyPgDown:
		total := m.recordCount()
		if m.recordEditIdx >= 0 && total > 0 && m.recordEditIdx < total-1 {
			m.warnIfLeavingUnrunnableDoor()
			m.recordEditIdx++
			m.recordFields = m.buildRecordFields()
			m.editField = 0
			m.fieldScroll = 0
		}
		return m, nil

	case tea.KeyPgUp:
		if m.recordEditIdx > 0 {
			m.warnIfLeavingUnrunnableDoor()
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
