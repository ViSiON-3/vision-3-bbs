package usereditor

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// View implements tea.Model.
func (m Model) View() string {
	switch {
	case m.mode == modeEdit || m.mode == modeEditField || m.mode == modePasswordEntry || m.mode == modeSaveOnLeave:
		return m.viewEditScreen()
	case m.confirmFromEdit && (m.mode == modeDeleteConfirm || m.mode == modeUndeleteConfirm || m.mode == modePurgeConfirm || m.mode == modeValidate):
		return m.viewEditScreen()
	case m.mode == modeInfoAlert && m.alertReturn == modeEdit:
		return m.viewEditScreen()
	default:
		return m.viewListScreen()
	}
}

// viewListScreen renders the main user list browser.
// Faithfully recreates UE.PAS v1.3 Init_Pick_Screen + Display_Group.
func (m Model) viewListScreen() string {
	var b strings.Builder

	// === Row 1: Title bar ===
	// UE.PAS: Color(8,15) Center_Write('╌╌ ViSiON/2 User Editor v1.3...')
	title := centerText("-- ViSiON/3 User Editor v1.0 --", m.width)
	b.WriteString(titleBarStyle.Render(title))
	b.WriteByte('\n')

	// Background fill line (reused throughout)
	bgLine := bgFillStyle.Render(strings.Repeat("░", m.width))

	// Vertical centering: distribute extra rows above and below box.
	// Fixed content: 1 title + box(19) + message(1) + help(1) = 22 rows
	extraV := max(0, m.height-22)
	topPad := max(1, extraV/2)
	bottomPad := max(1, extraV-topPad)

	for i := 0; i < topPad; i++ {
		b.WriteString(bgLine)
		b.WriteByte('\n')
	}

	// === Top border of list box ===
	// UE.PAS: GrowBox(10,5,70,22) with Mixed_Border
	boxW := 60 // columns 10-70
	padL := max(0, (m.width-boxW-2)/2)
	padR := max(0, m.width-padL-boxW-2)

	topBorder := bgFillStyle.Render(strings.Repeat("░", padL)) +
		listBorderStyle.Render("╒"+strings.Repeat("═", boxW)+"╕") +
		bgFillStyle.Render(strings.Repeat("░", max(0, padR)))
	b.WriteString(topBorder)
	b.WriteByte('\n')

	// === Row 6: Header text inside box ===
	// UE.PAS: Color(1,14) Center_Write('╌╌ Bash (CR) to Edit...')
	headerText := "-- Press Enter to Edit Highlighted User --"
	headerLine := centerInBox(headerText, boxW, listHeaderStyle, listBorderStyle, padL, padR)
	b.WriteString(bgFillStyle.Render(strings.Repeat("░", padL)) + headerLine + bgFillStyle.Render(strings.Repeat("░", max(0, padR))))
	b.WriteByte('\n')

	// === Row 7: Empty row inside box ===
	emptyBoxLine := bgFillStyle.Render(strings.Repeat("░", padL)) +
		listBorderStyle.Render("│") +
		listItemStyle.Render(strings.Repeat(" ", boxW)) +
		listBorderStyle.Render("│") +
		bgFillStyle.Render(strings.Repeat("░", max(0, padR)))
	b.WriteString(emptyBoxLine)
	b.WriteByte('\n')

	// === Row 8: Column title row ===
	// UE.PAS: Color(9,15) Tab(NameStr + Title[ListType], 58)
	colHeader := m.renderColumnTitle(boxW)
	colLine := bgFillStyle.Render(strings.Repeat("░", padL)) +
		listBorderStyle.Render("│") +
		columnTitleStyle.Render(padRight(colHeader, boxW)) +
		listBorderStyle.Render("│") +
		bgFillStyle.Render(strings.Repeat("░", max(0, padR)))
	b.WriteString(colLine)
	b.WriteByte('\n')

	// === Blank separator row between column header and list ===
	b.WriteString(emptyBoxLine)
	b.WriteByte('\n')

	// === User list (13 rows, traditional scrolling lightbar) ===
	// Build display list: user indices with a separator (-1) before deleted users.
	displayRows := m.buildDisplayRows()
	totalDisplay := len(displayRows)
	startIdx := m.scrollOffset
	for row := 0; row < listVisible; row++ {
		dIdx := startIdx + row

		var rowContent string
		if dIdx < 0 || dIdx >= totalDisplay {
			rowContent = listItemStyle.Render(strings.Repeat(" ", boxW))
		} else if displayRows[dIdx] == -1 {
			// Separator row for deleted users
			sepText := "--- DELETED USERS ---"
			rowContent = separatorStyle.Render(centerText(sepText, boxW))
		} else {
			idx := displayRows[dIdx]
			isHighlight := idx == m.cursor
			rowContent = m.renderUserRow(idx, isHighlight, boxW)
		}

		line := bgFillStyle.Render(strings.Repeat("░", padL)) +
			listBorderStyle.Render("│") +
			rowContent +
			listBorderStyle.Render("│") +
			bgFillStyle.Render(strings.Repeat("░", max(0, padR)))
		b.WriteString(line)
		b.WriteByte('\n')
	}

	// === Row 22: Bottom border ===
	botBorder := bgFillStyle.Render(strings.Repeat("░", padL)) +
		listBorderStyle.Render("╘"+strings.Repeat("═", boxW)+"╛") +
		bgFillStyle.Render(strings.Repeat("░", max(0, padR)))
	b.WriteString(botBorder)
	b.WriteByte('\n')

	// === Row 23: Message or background ===
	if m.message != "" {
		msgLine := bgFillStyle.Render(strings.Repeat("░", padL)) +
			flashMessageStyle.Render(" "+padRight(m.message, boxW+1)) +
			bgFillStyle.Render(strings.Repeat("░", max(0, padR)))
		b.WriteString(msgLine)
	} else if m.mode == modeSearch {
		searchLine := bgFillStyle.Render(strings.Repeat("░", padL)) +
			flashMessageStyle.Render(" Search: ") +
			m.searchInput.View() +
			bgFillStyle.Render(strings.Repeat("░", max(0, padR)))
		b.WriteString(searchLine)
	} else {
		b.WriteString(bgLine)
	}
	b.WriteByte('\n')

	// Bottom fill rows (vertically centers content)
	for i := 0; i < bottomPad; i++ {
		b.WriteString(bgLine)
		b.WriteByte('\n')
	}

	// === Bottom help bar ===
	// UE.PAS: Color(8,15) Center_Write('Press Alt-H for Pop-Up Help Screen.')
	helpText := centerText("Press Alt-H for Pop-Up Help Screen.", m.width)
	b.WriteString(helpBarStyle.Render(helpText))

	// === Overlay dialogs ===
	result := b.String()
	switch m.mode {
	case modeDeleteConfirm:
		handle := ""
		if m.cursor >= 0 && m.cursor < len(m.users) {
			handle = m.users[m.cursor].Handle
		}
		result = m.overlayDeleteDialog(result, handle)
	case modePurgeConfirm:
		handle := ""
		if m.cursor >= 0 && m.cursor < len(m.users) {
			handle = m.users[m.cursor].Handle
		}
		result = m.overlayPurgeDialog(result, handle)
	case modeUndeleteConfirm:
		handle := ""
		if m.cursor >= 0 && m.cursor < len(m.users) {
			handle = m.users[m.cursor].Handle
		}
		result = m.overlayConfirmDialog(result, "-- Undelete User --",
			fmt.Sprintf("Undelete %s? ", handle))
	case modeMassPurge:
		result = m.overlayConfirmDialog(result, "-- Purge All Deleted Users --",
			fmt.Sprintf("Permanently purge %d deleted user(s)? ", m.deletedCount()))
	case modeMassDelete:
		result = m.overlayConfirmDialog(result, "-- Super Duper User Nuker --",
			fmt.Sprintf("Delete All Tagged (%d) Users? ", m.taggedCount()))
	case modeValidate:
		handle := ""
		if m.cursor >= 0 && m.cursor < len(m.users) {
			handle = m.users[m.cursor].Handle
		}
		result = m.overlayConfirmDialog(result, "-- Automatic User Quick Validation --",
			fmt.Sprintf("Set %s to Default? ", handle))
	case modeMassValidate:
		result = m.overlayConfirmDialog(result, "-- Super Duper User Validation --",
			fmt.Sprintf("Set All Tagged (%d) Users to Defaults? ", m.taggedCount()))
	case modeExitConfirm:
		result = m.overlayConfirmDialog(result, "-- Unsaved Changes --",
			"Save changes before exit? ")
	case modeExitClean:
		result = m.overlayConfirmDialog(result, "-- Exit --",
			"Exit user editor? ")
	case modeFileChanged:
		result = m.overlayConfirmDialog(result, "-- File Modified Externally --",
			"Overwrite with your changes? ")
	case modeHelp:
		result = m.overlayHelpScreen(result)
	case modeInfoAlert:
		result = m.overlayInfoAlert(result)
	}

	return result
}

// renderColumnTitle returns the column header text based on listType.
// Column positions match renderUserRow: tag(1) + num(3) + space(1) + handle(30) + data cols.
func (m Model) renderColumnTitle(width int) string {
	nameStr := " " + padRight("#", 3) + " " + padRight("Handle", 30)
	var cols string
	switch m.listType {
	case 1:
		cols = padRight("Level", 8) + padRight("Calls", 11)
	case 2:
		cols = padRight("Group/Location", 19)
	case 3:
		cols = padRight("Posts", 8) + padRight("Valid", 11)
	case 4:
		cols = padRight("Last Date", 11) + padRight("Online", 8)
	}
	full := nameStr + cols
	if len(full) > width {
		full = full[:width]
	}
	return full
}

// renderUserRow renders a single user row in the list.
func (m Model) renderUserRow(idx int, isHighlight bool, boxW int) string {
	u := m.users[idx]
	tagged := m.tagged[idx]

	// Tag marker
	var tagChar string
	if tagged {
		tagChar = "*"
	} else {
		tagChar = " "
	}

	// User number (3 chars)
	numStr := fmt.Sprintf("%3d", idx+1)

	// Handle (30 chars)
	handle := padRight(u.Handle, 30)
	if u.Handle == "" {
		handle = padRight("[ Open User Record ]", 30)
	}

	// Data columns based on listType
	var dataCols string
	switch m.listType {
	case 1:
		dataCols = padRight(fmt.Sprintf("%d", u.AccessLevel), 8) +
			padRight(fmt.Sprintf("%d", u.TimesCalled), 11)
	case 2:
		dataCols = padRight(u.GroupLocation, 19)
	case 3:
		dataCols = padRight(fmt.Sprintf("%d", u.MessagesPosted), 8) +
			padRight(boolToYN(u.Validated), 11)
	case 4:
		dataCols = padRight(formatDate(u.LastLogin), 11) +
			padRight(formatTimeOnly(u.LastLogin), 8)
	}

	// Build the full row content
	content := tagChar + numStr + " " + handle + dataCols
	// Ensure it fills the box width
	if len(content) < boxW {
		content += strings.Repeat(" ", boxW-len(content))
	} else if len(content) > boxW {
		content = content[:boxW]
	}

	if isHighlight {
		// UE.PAS: Color(0,9) for tag, Color(0,14) for text
		tagPart := highlightTagStyle.Render(string(content[0]))
		textPart := highlightTextStyle.Render(content[1:])
		return tagPart + textPart
	}

	// Normal row
	if tagged {
		tagPart := taggedStyle.Render(string(content[0]))
		textPart := listItemStyle.Render(content[1:])
		return tagPart + textPart
	}
	return listItemStyle.Render(content)
}

// buildDisplayRows returns a list of user indices for the list view.
// A value of -1 indicates the "--- DELETED USERS ---" separator row.
// Deleted users are always at the end (after sorting), so the separator
// appears just before the first deleted user.
func (m Model) buildDisplayRows() []int {
	var rows []int
	hasDeleted := false
	for i, u := range m.users {
		if u.DeletedUser && !hasDeleted {
			hasDeleted = true
			rows = append(rows, -1) // separator
		}
		rows = append(rows, i)
	}
	return rows
}

// firstDeletedIndex returns the index of the first deleted user, or -1 if none.
func (m Model) firstDeletedIndex() int {
	for i, u := range m.users {
		if u.DeletedUser {
			return i
		}
	}
	return -1
}

// cursorToDisplayRow converts a user index (cursor) to a display row position,
// accounting for the separator row before deleted users.
func (m Model) cursorToDisplayRow(cursor int) int {
	sep := m.firstDeletedIndex()
	if sep >= 0 && cursor >= sep {
		return cursor + 1 // +1 for separator row
	}
	return cursor
}

// centerText centers a string within a given width.
func centerText(s string, width int) string {
	if len(s) >= width {
		return s[:width]
	}
	pad := (width - len(s)) / 2
	return strings.Repeat(" ", pad) + s + strings.Repeat(" ", width-pad-len(s))
}

// centerInBox centers text inside the box area between borders.
func centerInBox(text string, boxW int, textStyle, borderStyle lipgloss.Style, padL, padR int) string {
	centered := centerText(text, boxW)
	return borderStyle.Render("│") +
		textStyle.Render(centered) +
		borderStyle.Render("│")
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
