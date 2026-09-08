package usereditor

import (
	"fmt"
	"github.com/ViSiON-3/vision-3-bbs/internal/uitext"
	"strings"
	"unicode/utf8"

	"github.com/ViSiON-3/vision-3-bbs/internal/tuiart"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// viewEditScreen renders the per-user field editor.
// Faithfully recreates UE.PAS v1.3 Edit_User screen.
func (m Model) viewEditScreen() string {
	if m.editIndex < 0 || m.editIndex >= len(m.users) {
		return "No user selected"
	}
	u := m.users[m.editIndex]

	// Fixed rows: title(1) + box(border, 19 field rows, border = 21) + help(1).
	const editRowFirst, editRowLast = 4, 22
	boxRows := (editRowLast - editRowFirst + 1) + 2
	topPad, bottomPad := tuiart.Split(m.height, boxRows+2)

	sc := tuiart.NewScreen(m.width, m.backdrop)

	// === Row 1: Title bar ===
	// UE.PAS: Color(8,15) Center_Write('╌╌ ViSiON/3 Quick & Easy User Editor v1.0 ╌╌')
	sc.Line(editTitleStyle.Render(centerText("-- ViSiON/3 Quick & Easy User Editor v1.0 --", m.width)))
	sc.BgRows(topPad)

	// === Edit box ===
	// UE.PAS: GrowBOX(2,3,78,23) Color(1,9)
	boxW := 76 // columns 2-78
	padL := max(0, (m.width-boxW-2)/2)
	padR := max(0, m.width-padL-boxW-2)

	// === Top border ===
	sc.Line(sc.Pad(padL, padR, editBorderStyle.Render("╒"+strings.Repeat("═", boxW)+"╕")))

	// === Field area ===
	for row := editRowFirst; row <= editRowLast; row++ {
		sc.Line(sc.Pad(padL, padR, editBorderStyle.Render("│")+
			m.renderEditRow(row, u, boxW)+editBorderStyle.Render("│")))
	}

	// === Bottom border ===
	sc.Line(sc.Pad(padL, padR, editBorderStyle.Render("╘"+strings.Repeat("═", boxW)+"╛")))

	sc.BgRows(bottomPad)

	// === Bottom help bar ===
	// UE.PAS: 'F2 - Delete  F5 - Set Defaults  F10 - Aborts  ESC - Save Changes'
	f2Label := "Delete"
	if u.DeletedUser {
		f2Label = "Undelete"
	}
	var helpItems string
	if u.DeletedUser {
		helpItems = fmt.Sprintf("F2 - %s  F4 - Purge  F5 - Set Defaults  F10 - Aborts  ESC - Save", f2Label)
	} else {
		helpItems = fmt.Sprintf("F2 - %s  F5 - Set Defaults  F10 - Aborts  ESC - Save Changes", f2Label)
	}
	sc.Last(helpBarStyle.Render(centerText(helpItems, m.width)))

	// Overlay for password entry and WFC key-manager dialogs
	result := sc.String()
	switch m.mode {
	case modePasswordEntry:
		result = m.overlayPasswordDialog(result)
	case modeKeyList:
		result = m.overlayKeyListDialog(result)
	case modeKeyAdd:
		result = m.overlayKeyAddDialog(result)
	case modeDeleteConfirm:
		result = m.overlayDeleteDialog(result, u.Handle)
	case modePurgeConfirm:
		result = m.overlayPurgeDialog(result, u.Handle)
	case modeUndeleteConfirm:
		result = m.overlayConfirmDialog(result, "-- Undelete User --",
			fmt.Sprintf("Undelete %s? ", u.Handle))
	case modeValidate:
		result = m.overlayConfirmDialog(result, "-- Auto Validate --",
			fmt.Sprintf("Set %s to Defaults? ", u.Handle))
	case modeSaveOnLeave:
		result = m.overlayConfirmDialog(result, "-- Unsaved Changes --",
			"Save changes to disk? ")
	case modeInfoAlert:
		result = m.overlayInfoAlert(result)
	}

	return result
}

// renderEditRow renders a single row inside the edit box area.
func (m Model) renderEditRow(row int, u *userType, boxW int) string {
	var leftField, rightField string
	var leftRawW, rightRawW int

	// Column budgets, declared once and also handed to renderField so an
	// active input is bounded by the space it actually has. Emitting more than
	// the budget only skips the padding below; the row overruns regardless.
	leftW := 42 // Left column width (41 content + 1 gap before right column)
	rightW := boxW - leftW

	// Find fields that belong to this row
	for i, f := range m.fields {
		if f.Row != row {
			continue
		}

		switch f.Col {
		case leftCol:
			leftField, leftRawW = m.renderField(i, f, u, leftW)
		case rightCol:
			rightField, rightRawW = m.renderField(i, f, u, rightW)
		}
	}

	// Separator row between editable and read-only fields
	if row == 17 {
		sepText := "-- Read Only --"
		sepPad := (boxW - len(sepText)) / 2
		return separatorStyle.Render(strings.Repeat("─", sepPad)) +
			separatorStyle.Render(sepText) +
			separatorStyle.Render(strings.Repeat("─", max(0, boxW-sepPad-len(sepText))))
	}

	// Special row: User Number display (row 22 in the box)
	if row == 22 {
		infoText := fmt.Sprintf("User Number: %d of %d", m.editIndex+1, len(m.users))
		infoRendered := editInfoLabelStyle.Render("User Number: ") +
			editInfoValueStyle.Render(fmt.Sprintf("%d", m.editIndex+1)) +
			editInfoLabelStyle.Render(" of ") +
			editInfoValueStyle.Render(fmt.Sprintf("%d", len(m.users)))
		rawLen := len(infoText)
		leftPad := (boxW - rawLen) / 2
		return fieldDisplayStyle.Render(strings.Repeat(" ", leftPad)) + infoRendered +
			fieldDisplayStyle.Render(strings.Repeat(" ", max(0, boxW-leftPad-rawLen)))
	}

	if leftField == "" && rightField == "" {
		return fieldDisplayStyle.Render(strings.Repeat(" ", boxW))
	}

	// Build the row using pre-computed raw widths (not ANSI measurement).
	var result string
	if leftField != "" {
		result = leftField
		if leftRawW < leftW {
			result += fieldDisplayStyle.Render(strings.Repeat(" ", leftW-leftRawW))
		}
	} else {
		result = fieldDisplayStyle.Render(strings.Repeat(" ", leftW))
	}

	if rightField != "" {
		result += rightField
		if rightRawW < rightW {
			result += fieldDisplayStyle.Render(strings.Repeat(" ", rightW-rightRawW))
		}
	} else {
		result += fieldDisplayStyle.Render(strings.Repeat(" ", rightW))
	}

	return result
}

// renderField renders a single field (label + value).
// Returns the styled string and the raw (unstyled) visible character width.
// budget is the column width the field must fit inside.
func (m Model) renderField(fieldIdx int, f fieldDef, u *userType, budget int) (string, int) {
	isActive := m.editField == fieldIdx

	// Pad labels to consistent widths so colons align vertically.
	// Left column: longest is "Group/Location" at 14 chars.
	// Right column: longest is "Screen Height" at 13 chars.
	labelText := f.Label
	switch f.Col {
	case 3:
		labelText = padRight(labelText, 14)
	case 50:
		labelText = padRight(labelText, 13)
	}
	label := labelText + " : "
	// Count runes, not bytes: every other width helper here is rune-based
	// (padRight, centerText). Labels are ASCII today, so the two agree, but a
	// mixed basis is how geometry drift gets reintroduced.
	labelLen := utf8.RuneCountInString(label)

	var value string
	if f.Get != nil {
		value = f.Get(u)
	}

	// Raw width is always label + field width (value is padded/clamped to f.Width)
	rawW := labelLen + f.Width

	// If actively editing this field, bound the widget's output to the column
	// budget and report what it actually occupies, rather than deriving either
	// from f.Width. It appends a cursor cell after the text, so a value that
	// fills the field renders Width+1 cells. No ./ue field is wide enough to
	// overrun its column today, but ./menuedit's were, and measuring alone was
	// not enough there: when the view exceeds the space, the padding falls to
	// zero and the oversized view still overruns.
	if isActive && m.mode == modeEditField {
		view, w := tuiart.FitInput(m.textInput.View(), max(0, budget-labelLen))
		return fieldLabelStyle.Render(label) + view, labelLen + w
	}

	// Display the value
	displayValue := padRight(value, f.Width)

	if isActive && m.mode == modeEdit {
		// Highlighted field (ready to edit)
		fillStr := strings.Repeat(string(fieldFillChar), max(0, f.Width-len(value)))
		return fieldLabelStyle.Render(label) + fieldEditStyle.Render(value+fillStr), rawW
	}

	switch f.Type {
	case ftDisplay:
		return fieldLabelStyle.Render(label) + editInfoValueStyle.Render(displayValue), rawW
	case ftAction:
		return fieldLabelStyle.Render(label) + editInfoValueStyle.Render(displayValue), rawW
	default:
		return fieldLabelStyle.Render(label) + fieldDisplayStyle.Render(displayValue), rawW
	}
}

// overlayPasswordDialog renders a password entry dialog over the background.
func (m Model) overlayPasswordDialog(background string) string {
	lines := strings.Split(background, "\n")
	dialogW := 54
	dialogH := 5
	startRow := (m.height - dialogH) / 2
	startCol := (m.width - dialogW) / 2

	border := dialogBorderStyle.Render("╔" + strings.Repeat("═", dialogW-2) + "╗")
	borderBot := dialogBorderStyle.Render("╚" + strings.Repeat("═", dialogW-2) + "╝")
	side := dialogBorderStyle.Render("║")

	titleText := "Enter New Password"
	titlePad := (dialogW - 2 - len(titleText)) / 2
	titleLine := side +
		dialogTextStyle.Render(strings.Repeat(" ", titlePad)+titleText+strings.Repeat(" ", dialogW-2-titlePad-len(titleText))) +
		side

	emptyLine := side +
		dialogTextStyle.Render(strings.Repeat(" ", dialogW-2)) +
		side

	// textinput.View() width varies (cursor, placeholder styling, etc).
	// Measure actual visible width to compute correct right padding.
	tiView := m.textInput.View()
	inputVisLen := 1 + uitext.ApproximateVisibleLen(tiView) // 1 for left space
	innerW := dialogW - 2
	rightPad := max(0, innerW-inputVisLen)
	inputLine := side +
		dialogTextStyle.Render(" ") +
		tiView +
		dialogTextStyle.Render(strings.Repeat(" ", rightPad)) +
		side

	dialogLines := []string{border, titleLine, emptyLine, inputLine, borderBot}

	// Overlay dialog on background, preserving content on both sides
	endCol := startCol + dialogW
	for i, dl := range dialogLines {
		row := startRow + i
		if row >= 0 && row < len(lines) {
			left := padToCol(lines[row], startCol)
			right := skipToCol(lines[row], endCol)
			lines[row] = left + dl + right
		}
	}

	return strings.Join(lines, "\n")
}

// padToCol truncates or pads a line to reach a specific column.
func padToCol(line string, col int) string {
	vis := uitext.ApproximateVisibleLen(line)
	if vis >= col {
		return uitext.TruncateToVisual(line, col)
	}
	return line + strings.Repeat(" ", col-vis)
}

// skipToCol returns everything in a string from visible column n onward,
// replaying the last active ANSI escape sequence so styling is preserved.
func skipToCol(s string, n int) string {
	var lastESC strings.Builder // tracks the most recent ANSI sequence
	var curESC strings.Builder  // builds current escape sequence
	inEsc := false
	count := 0
	for i, r := range s {
		if r == '\x1b' {
			inEsc = true
			curESC.Reset()
			curESC.WriteRune(r)
			continue
		}
		if inEsc {
			curESC.WriteRune(r)
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				inEsc = false
				lastESC.Reset()
				lastESC.WriteString(curESC.String())
			}
			continue
		}
		if count == n {
			// Prepend the last ANSI sequence to restore styling context
			return lastESC.String() + s[i:]
		}
		count++
	}
	return ""
}

// userType is a type alias for user.User (keeps method signatures clean).
type userType = user.User

// Ensure the user package is imported for the type alias.
var _ *user.User
