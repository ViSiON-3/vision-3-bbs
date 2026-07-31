package configeditor

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// Record Field Editing mode: opening a field, handling keys while it is open,
// and writing the entered value back to the record. Also the field-row window
// (maxFieldRows) and the scrolling that keeps the edited field visible.

func (m Model) startRecordFieldEdit() (Model, tea.Cmd) {
	f := m.recordFields[m.editField]
	if f.Type == ftDisplay {
		return m, nil
	}

	if f.Type == ftLookup && f.LookupItems != nil {
		m.pickerItems = f.LookupItems()
		m.pickerCursor = 0
		m.pickerScroll = 0
		m.pickerReturnMode = modeRecordEdit
		// Pre-select current value by matching display text
		cur := f.Get()
		for i, item := range m.pickerItems {
			if item.Value == cur || item.Display == cur {
				m.pickerCursor = i
				break
			}
		}
		// Also try matching by value embedded in display (e.g. "Name (ID: 1)")
		if m.pickerCursor == 0 && len(m.pickerItems) > 0 {
			for i, item := range m.pickerItems {
				if strings.Contains(cur, "(ID: "+item.Value+")") {
					m.pickerCursor = i
					break
				}
			}
		}
		m.clampPickerScroll()
		m.mode = modeLookupPicker
		return m, nil
	}

	val := f.Get()
	m.mode = modeRecordField
	m.textInput.SetValue(val)
	m.textInput.CharLimit = f.Width
	m.textInput.Width = f.Width
	m.textInput.EchoMode = textinput.EchoNormal
	m.textInput.Placeholder = ""
	m.textInput.CursorEnd()
	m.textInput.Focus()

	return m, textinput.Blink
}

func (m Model) updateRecordField(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	f := m.recordFields[m.editField]

	switch msg.Type {
	case tea.KeyEnter, tea.KeyTab, tea.KeyDown:
		if err := m.applyRecordFieldValue(f); err != nil {
			m.message = fmt.Sprintf("Invalid: %v", err)
			return m, nil
		}
		m.textInput.Blur()
		m.mode = modeRecordEdit
		if !m.stayOnField {
			m.editField = m.nextRecordEditableField(1)
		}
		m.stayOnField = false
		m.clampFieldScroll(m.recordFields)
		return m, nil

	case tea.KeyUp:
		if err := m.applyRecordFieldValue(f); err != nil {
			m.message = fmt.Sprintf("Invalid: %v", err)
			return m, nil
		}
		m.textInput.Blur()
		m.mode = modeRecordEdit
		if !m.stayOnField {
			m.editField = m.nextRecordEditableField(-1)
		}
		m.stayOnField = false
		m.clampFieldScroll(m.recordFields)
		return m, nil

	case tea.KeyEscape:
		m.textInput.Blur()
		m.mode = modeRecordEdit
		return m, nil

	default:
		if f.Type == ftYesNo {
			if len(msg.Runes) == 1 {
				ch := msg.Runes[0]
				switch ch {
				case 'y', 'Y':
					m.textInput.SetValue("Y")
				case 'n', 'N':
					m.textInput.SetValue("N")
				}
				if err := m.applyRecordFieldValue(f); err == nil {
					m.textInput.Blur()
					m.mode = modeRecordEdit
					m.editField = m.nextRecordEditableField(1)
					m.clampFieldScroll(m.recordFields)
				}
				return m, nil
			}
			return m, nil
		}

		if f.Type == ftInteger {
			if len(msg.Runes) == 1 {
				ch := msg.Runes[0]
				if (ch < '0' || ch > '9') && ch != '-' {
					return m, nil
				}
			}
		}

		var cmd tea.Cmd
		m.textInput, cmd = m.textInput.Update(msg)
		return m, cmd
	}
}

func (m *Model) applyRecordFieldValue(f fieldDef) error {
	val := m.textInput.Value()

	switch f.Type {
	case ftInteger:
		n, err := strconv.Atoi(val)
		if err != nil {
			return fmt.Errorf("not a number")
		}
		if n < f.Min || n > f.Max {
			return fmt.Errorf("must be %d-%d", f.Min, f.Max)
		}
	case ftYesNo:
		upper := strings.ToUpper(val)
		if upper != "Y" && upper != "N" {
			return fmt.Errorf("must be Y or N")
		}
		val = upper
	}

	if f.Set != nil {
		if err := f.Set(val); err != nil {
			return err
		}
		m.dirty = true
		m.message = ""
		if f.AfterSet != nil {
			f.AfterSet(m, val)
		}
	}

	// Rebuild field list in case a toggle (e.g. Is DOS) changed visible fields.
	m.recordFields = m.buildRecordFields()
	if m.editField >= len(m.recordFields) {
		m.editField = len(m.recordFields) - 1
	}

	return nil
}

// toggleYesNo flips a Y/N field value in place.
func (m *Model) toggleYesNo(f fieldDef) {
	if f.Get != nil && f.Set != nil {
		var val string
		if f.Get() == "Y" {
			val = "N"
		} else {
			val = "Y"
		}
		_ = f.Set(val) // Y/N field setters never fail
		m.dirty = true
		m.message = ""
		if f.AfterSet != nil {
			f.AfterSet(m, val)
		}
		// Rebuild fields in case toggle changed visible fields (e.g. Is DOS)
		m.recordFields = m.buildRecordFields()
		if m.editField >= len(m.recordFields) {
			m.editField = len(m.recordFields) - 1
		}
	}
}

// maxFieldRows is the maximum number of field rows visible in the edit box.
const maxFieldRows = 12

// clampFieldScroll adjusts fieldScroll so the active field row is visible.
func (m *Model) clampFieldScroll(fields []fieldDef) {
	if len(fields) == 0 {
		m.fieldScroll = 0
		return
	}
	// Get the row of the current field
	activeRow := fields[m.editField].Row

	if activeRow < m.fieldScroll+1 {
		m.fieldScroll = activeRow - 1
	}
	if activeRow > m.fieldScroll+maxFieldRows {
		m.fieldScroll = activeRow - maxFieldRows
	}
	// Keep 1 row of context above the cursor when at the top edge
	if m.fieldScroll > 0 && activeRow == m.fieldScroll+1 {
		m.fieldScroll--
	}
	if m.fieldScroll < 0 {
		m.fieldScroll = 0
	}
}
