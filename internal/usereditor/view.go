package usereditor

import (
	"fmt"
	"github.com/ViSiON-3/vision-3-bbs/internal/uitext"
	"strings"

	"github.com/ViSiON-3/vision-3-bbs/internal/tuiart"
	"github.com/charmbracelet/lipgloss"
)

// View implements tea.Model.
func (m Model) View() string {
	switch {
	case m.mode == modeEdit || m.mode == modeEditField || m.mode == modePasswordEntry || m.mode == modeSaveOnLeave ||
		m.mode == modeKeyList || m.mode == modeKeyAdd:
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
	// Fixed rows: title(1) + box(border, header, empty, column title, empty,
	// listVisible, border = listVisible+6) + message(1) + help(1).
	topPad, bottomPad := tuiart.Split(m.height, listVisible+9)

	sc := tuiart.NewScreen(m.width, m.backdrop)

	// === Row 1: Title bar ===
	// UE.PAS: Color(8,15) Center_Write('╌╌ ViSiON/2 User Editor v1.3...')
	sc.Line(titleBarStyle.Render(centerText("-- ViSiON/3 User Editor v1.0 --", m.width)))
	sc.BgRows(topPad)

	// === List box ===
	// UE.PAS: GrowBox(10,5,70,22) with Mixed_Border
	boxW := 60 // columns 10-70
	padL := max(0, (m.width-boxW-2)/2)
	padR := max(0, m.width-padL-boxW-2)

	box := func(content string) {
		sc.Line(sc.Pad(padL, padR,
			listBorderStyle.Render("│")+content+listBorderStyle.Render("│")))
	}
	emptyBox := func() { box(listItemStyle.Render(strings.Repeat(" ", boxW))) }

	// === Top border ===
	sc.Line(sc.Pad(padL, padR, listBorderStyle.Render("╒"+strings.Repeat("═", boxW)+"╕")))

	// === Header text inside box ===
	// UE.PAS: Color(1,14) Center_Write('╌╌ Bash (CR) to Edit...')
	box(listHeaderStyle.Render(centerText("-- Press Enter to Edit Highlighted User --", boxW)))

	// === Empty row inside box ===
	emptyBox()

	// === Column title row ===
	// UE.PAS: Color(9,15) Tab(NameStr + Title[ListType], 58)
	box(columnTitleStyle.Render(padRight(m.renderColumnTitle(boxW), boxW)))

	// === Blank separator row between column header and list ===
	emptyBox()

	// === User list (listVisible rows, traditional scrolling lightbar) ===
	// Build display list: user indices with a separator (-1) before deleted users.
	displayRows := m.buildDisplayRows()
	totalDisplay := len(displayRows)
	for row := 0; row < listVisible; row++ {
		dIdx := m.scrollOffset + row
		switch {
		case dIdx < 0 || dIdx >= totalDisplay:
			emptyBox()
		case displayRows[dIdx] == -1:
			// Separator row for deleted users
			box(separatorStyle.Render(centerText("--- DELETED USERS ---", boxW)))
		default:
			idx := displayRows[dIdx]
			box(m.renderUserRow(idx, idx == m.cursor, boxW))
		}
	}

	// === Bottom border ===
	sc.Line(sc.Pad(padL, padR, listBorderStyle.Render("╘"+strings.Repeat("═", boxW)+"╛")))

	// === Message, search prompt, or background ===
	switch {
	case m.message != "":
		sc.Line(sc.Pad(padL, padR, flashMessageStyle.Render(" "+padRight(m.message, boxW+1))))
	case m.mode == modeSearch:
		// Pad against what the widget actually renders: it appends a cursor
		// cell after the text, so its width is not searchInput.Width.
		const searchLabel = " Search: "
		inputView := m.searchInput.View()
		used := len(searchLabel) + uitext.ApproximateVisibleLen(inputView)
		sc.Line(sc.Pad(padL, padR, flashMessageStyle.Render(searchLabel)+inputView+
			flashMessageStyle.Render(strings.Repeat(" ", max(0, boxW+2-used)))))
	default:
		sc.BgLine()
	}

	sc.BgRows(bottomPad)

	// === Bottom help bar ===
	// UE.PAS: Color(8,15) Center_Write('Press Alt-H for Pop-Up Help Screen.')
	sc.Last(helpBarStyle.Render(centerText("Press Alt-H for Pop-Up Help Screen.", m.width)))

	// === Overlay dialogs ===
	result := sc.String()
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
			padRight(uitext.BoolToYN(u.Validated), 11)
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
//
// This is rune-based, not byte-based: every caller today passes an ASCII
// literal, so this was unreachable in practice, but a byte-offset slice on
// multi-byte input would emit a partial UTF-8 sequence and render as garbage.
func centerText(s string, width int) string { return tuiart.CenterText(s, width) }

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
