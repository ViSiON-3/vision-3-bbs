package stringeditor

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
)

// Styles matching the Pascal original's color scheme:
//
//	NormalColor  = 8  (Dark Gray)
//	BoldColor    = 15 (White)
//	BarColor     = 95 (White on Magenta → we use White on Blue for status)
//	InputColor   = 31 (White on Blue)
//	ChoiceColor  = 15 (White)
//	DataColor    = 9  (Light Blue)
var (
	// Status bar styles matching Pascal's SetColor() attributes:
	//   SetColor(16) = bg=blue, fg=black  (fill + separators)
	//   SetColor(25) = bg=blue, fg=light blue (labels)
	//   SetColor(31) = bg=blue, fg=white (values)
	//   SetColor(27) = bg=blue, fg=light cyan (page label)
	statusBarFillStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("0")).
				Background(lipgloss.Color("4"))

	statusBarLabelStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("12")).
				Background(lipgloss.Color("4"))

	statusBarValueStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("15")).
				Background(lipgloss.Color("4")).
				Bold(true)

	statusBarPageLabelStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("14")).
				Background(lipgloss.Color("4"))

	// Item label in bracket: dark gray
	bracketStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("8"))

	// Normal item label
	labelNormalStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("7"))

	// Highlighted item: white on magenta bar (Pascal BarColor=95 → bg=5, fg=15)
	labelHighlightStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("15")).
				Background(lipgloss.Color("5")).
				Bold(true)

	bracketHighlightStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("14")).
				Background(lipgloss.Color("5"))

	// Description bar (row 24): magenta text, centered
	descriptionStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("13"))

	// Dialog box styles
	dialogBorderStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("15")).
				Background(lipgloss.Color("4"))

	dialogTextStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("14")).
			Background(lipgloss.Color("4"))

	dialogButtonStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("7")).
				Background(lipgloss.Color("4"))

	dialogButtonActiveStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("15")).
				Background(lipgloss.Color("5")).
				Bold(true)

	// Search bar
	searchLabelStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("11")).
				Bold(true)

	// Message flash
	messageStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("14"))

	// Edit mode indicator
	editingStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("11")).
			Background(lipgloss.Color("4")).
			Bold(true)

	// Rejected input (malformed escape sequence)
	errorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("15")).
			Background(lipgloss.Color("1")).
			Bold(true)
)

// View implements tea.Model.
func (m Model) View() string {
	var b strings.Builder

	// === Row 1: Status Bar ===
	b.WriteString(m.renderStatusBar())
	b.WriteByte('\n')

	// === Row 2: Separator / Column Headers ===
	b.WriteString(m.renderColumnHeader())
	b.WriteByte('\n')

	// === Item list: as many rows as the terminal height allows ===
	pageStart := m.page * m.pageSize
	pageEnd := pageStart + m.pageSize
	if pageEnd > len(m.entries) {
		pageEnd = len(m.entries)
	}

	for row := 0; row < m.pageSize; row++ {
		idx := pageStart + row
		if idx < pageEnd {
			b.WriteString(m.renderItem(idx))
		} else {
			// Empty row filler
			b.WriteString(strings.Repeat(" ", m.width))
		}
		b.WriteByte('\n')
	}

	// === Row 23: Message / Mode indicator ===
	b.WriteString(m.renderMessageBar())
	b.WriteByte('\n')

	// === Row 24: Description Bar ===
	b.WriteString(m.renderDescriptionBar())
	b.WriteByte('\n')

	// === Row 25: Help Bar ===
	b.WriteString(m.renderHelpBar())

	// === Overlay: Confirm Dialog ===
	switch m.mode {
	case modeAbortConfirm:
		return m.overlayDialog(b.String(), "Abort Without Saving?")
	case modeRevertConfirm:
		return m.overlayDialog(b.String(), "Revert to Last Saved?")
	case modeDefaultConfirm:
		return m.overlayDialog(b.String(), "Restore ViSiON/3 Default?")
	}

	return b.String()
}

// renderStatusBar creates the top status bar matching the Pascal original.
// Pascal gotopage procedure row 1 layout:
//
//	SetColor(16); ClrEOL;  (fill entire row blue)
//	SetColor(25); Write(' Current Topic Number:');
//	SetColor(31); Write(' '+strr(top));
//	SetColor(16); Write('│');
//	SetColor(25); Write(' ViSiON/2 BBS String Configuration');
//	SetColor(16); Write(' │');
//	SetColor(27); Write(' Current Page:');
//	SetColor(31); Write(' '+strr(page));
func (m Model) renderStatusBar() string {
	// First item on current page (1-based, matching Pascal's top variable)
	topItem := m.page*m.pageSize + 1
	pageNum := m.page + 1

	// Build segments exactly matching Pascal's SetColor/Write sequence
	seg1 := statusBarLabelStyle.Render(" Current Topic Number:")
	seg2 := statusBarValueStyle.Render(fmt.Sprintf(" %d", topItem))
	sep1 := statusBarFillStyle.Render(" │")
	seg3 := statusBarLabelStyle.Render(" ViSiON/3 BBS String Configuration")
	sep2 := statusBarFillStyle.Render(" │")
	seg4 := statusBarPageLabelStyle.Render(" Current Page:")
	seg5 := statusBarValueStyle.Render(fmt.Sprintf(" %d", pageNum))

	content := seg1 + seg2 + sep1 + seg3 + sep2 + seg4 + seg5

	// Measure the styled text directly rather than a parallel plain copy, which
	// drifts out of sync. A narrow terminal or a three-digit topic number can
	// push the bar past the last column, so clip it instead of overflowing.
	visLen := visualLen(content)
	if visLen > m.width {
		return truncateVisual(content, m.width)
	}

	return content + statusBarFillStyle.Render(strings.Repeat(" ", m.width-visLen))
}

// renderColumnHeader creates a subtle column header line. Its columns line up
// with renderItem: the name sits over the labels and "Value" over labelCol.
func (m Model) renderColumnHeader() string {
	const nameHeading = "  # Name"
	header := nameHeading +
		strings.Repeat(" ", labelCol-len(nameHeading)) +
		"Value"
	if pad := m.width - cellWidth(header); pad > 0 {
		header += strings.Repeat(" ", pad)
	}
	return bracketStyle.Render(header)
}

// valueWidth returns the terminal cells available for a value preview, which
// is everything to the right of the label column.
func (m Model) valueWidth() int {
	w := m.width - labelCol
	if w < 10 {
		w = 10
	}
	return w
}

// renderItem renders a single list item.
func (m Model) renderItem(idx int) string {
	entry := m.entries[idx]
	isSelected := idx == m.cursor
	itemNum := idx + 1
	value := m.getValue(entry.Key)
	valueWidth := m.valueWidth()

	// Format item number, right-aligned in numWidth columns
	numStr := fmt.Sprintf("%*d", numWidth, itemNum)

	var line string
	if isSelected {
		if m.mode == modeEdit {
			// Show text input in the value area
			numPart := bracketHighlightStyle.Render(numStr)
			bracket1 := bracketHighlightStyle.Render("[")
			label := labelHighlightStyle.Render(padOrTrunc(entry.Label, labelWidth))
			bracket2 := bracketHighlightStyle.Render("]")
			// Clip the input to the value column so a long escaped value can
			// never push the row past the last terminal cell.
			input := m.textInput.View()
			if visualLen(input) > valueWidth {
				input = truncateVisual(input, valueWidth)
			}
			line = numPart + bracket1 + label + bracket2 + input
		} else {
			// Highlighted item
			numPart := bracketHighlightStyle.Render(numStr)
			bracket1 := bracketHighlightStyle.Render("[")
			label := labelHighlightStyle.Render(padOrTrunc(entry.Label, labelWidth))
			bracket2 := bracketHighlightStyle.Render("]")
			renderedVal := RenderColorString(value, valueWidth)
			line = numPart + bracket1 + label + bracket2 + renderedVal
		}
	} else {
		// Normal item
		numPart := bracketStyle.Render(numStr)
		bracket1 := bracketStyle.Render("[")
		label := labelNormalStyle.Render(padOrTrunc(entry.Label, labelWidth))
		bracket2 := bracketStyle.Render("]")
		renderedVal := RenderColorString(value, valueWidth)
		line = numPart + bracket1 + label + bracket2 + renderedVal
	}

	return line
}

// renderMessageBar renders the message/mode indicator line. Every branch is
// clipped and padded to exactly the terminal width so a long key, a long escape
// error, or a narrow terminal cannot wrap the row.
func (m Model) renderMessageBar() string {
	var text string
	style := messageStyle

	switch m.mode {
	case modeEdit:
		if m.editErr != "" {
			text, style = " "+m.editErr, errorStyle
		} else {
			text = fmt.Sprintf(` Editing: %s  \r \n \t = control chars  Enter=Save  Esc=Cancel`, m.editKey)
			style = editingStyle
		}
	case modeSearch:
		return padRow(searchLabelStyle.Render(" Search: ")+m.searchInput.View(), m.width)
	default:
		if m.message == "" {
			return strings.Repeat(" ", m.width)
		}
		text = " " + m.message
	}

	if cellWidth(text) > m.width {
		text = truncateVisual(text, m.width)
	}
	return style.Render(text) + strings.Repeat(" ", m.width-cellWidth(text))
}

// padRow clips or pads an already-styled row to exactly width cells.
func padRow(rendered string, width int) string {
	vis := visualLen(rendered)
	if vis > width {
		return truncateVisual(rendered, width)
	}
	return rendered + strings.Repeat(" ", width-vis)
}

// renderDescriptionBar renders the description for the current item (row 24).
// In the Pascal original, this is centered magenta text.
func (m Model) renderDescriptionBar() string {
	if m.cursor >= 0 && m.cursor < len(m.entries) {
		desc := m.entries[m.cursor].Description
		if desc == "" {
			desc = m.entries[m.cursor].Key
		}
		// Center the description text
		pad := (m.width - cellWidth(desc)) / 2
		if pad < 0 {
			pad = 0
		}
		return padRow(strings.Repeat(" ", pad)+descriptionStyle.Render(desc), m.width)
	}
	return strings.Repeat(" ", m.width)
}

// renderHelpBar renders the bottom help bar.
func (m Model) renderHelpBar() string {
	help := " Enter Edit  F1 Prefill  F3 Revert  F4 Default  F10 Save  Esc Quit  / Search"
	if cellWidth(help) > m.width {
		help = truncateVisual(help, m.width)
	}
	padded := help + strings.Repeat(" ", m.width-cellWidth(help))
	style := lipgloss.NewStyle().
		Foreground(lipgloss.Color("11")).
		Background(lipgloss.Color("4")).
		Bold(true)
	return style.Render(padded)
}

// overlayDialog renders a confirmation dialog centered over the content.
func (m Model) overlayDialog(background string, title string) string {
	lines := strings.Split(background, "\n")

	// Dialog dimensions
	dialogW := 30
	dialogH := 5

	// Calculate center position
	startRow := (m.height - dialogH) / 2
	startCol := (m.width - dialogW) / 2
	if startRow < 0 {
		startRow = 0
	}
	if startCol < 0 {
		startCol = 0
	}

	// Build dialog lines
	border := dialogBorderStyle.Render("╔" + strings.Repeat("═", dialogW-2) + "╗")
	borderBot := dialogBorderStyle.Render("╚" + strings.Repeat("═", dialogW-2) + "╝")
	borderSide := dialogBorderStyle.Render("║")

	titlePad := (dialogW - 2 - len(title)) / 2
	titleLine := borderSide +
		dialogTextStyle.Render(strings.Repeat(" ", titlePad)+title+strings.Repeat(" ", dialogW-2-titlePad-len(title))) +
		borderSide

	emptyLine := borderSide +
		dialogTextStyle.Render(strings.Repeat(" ", dialogW-2)) +
		borderSide

	// Buttons
	var yesBtn, noBtn string
	if m.confirmYes {
		yesBtn = dialogButtonActiveStyle.Render(" Yes ")
		noBtn = dialogButtonStyle.Render(" No ")
	} else {
		yesBtn = dialogButtonStyle.Render(" Yes ")
		noBtn = dialogButtonActiveStyle.Render(" No ")
	}
	btnPad := (dialogW - 2 - 11) / 2 // 5+2+4 = 11 visible chars
	buttonLine := borderSide +
		dialogTextStyle.Render(strings.Repeat(" ", btnPad)) +
		yesBtn + dialogTextStyle.Render("  ") + noBtn +
		dialogTextStyle.Render(strings.Repeat(" ", max(0, dialogW-2-btnPad-11))) +
		borderSide

	dialogLines := []string{border, titleLine, emptyLine, buttonLine, borderBot}

	// Overlay dialog on background, padding each line to full terminal width
	for i, dl := range dialogLines {
		row := startRow + i
		if row >= 0 && row < len(lines) {
			line := lines[row]
			// Pad or truncate left side to startCol so all rows align
			left := padToCol(line, startCol)
			right := skipVisual(line, startCol+dialogW)
			composed := left + dl + right
			// Pad to full terminal width so no gap appears on wide terminals
			if vis := visualLen(composed); vis < m.width {
				composed += strings.Repeat(" ", m.width-vis)
			}
			lines[row] = composed
		}
	}

	return strings.Join(lines, "\n")
}

// padOrTrunc pads or truncates a string to exactly width characters.
func padOrTrunc(s string, width int) string {
	runes := []rune(s)
	if len(runes) >= width {
		return string(runes[:width])
	}
	return s + strings.Repeat(" ", width-len(runes))
}

// visualLen returns the width in terminal cells of s, ignoring ANSI escape
// sequences.
func visualLen(s string) int {
	inEsc := false
	count := 0
	for _, r := range s {
		if r == '\x1b' {
			inEsc = true
			continue
		}
		if inEsc {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				inEsc = false
			}
			continue
		}
		count += runewidth.RuneWidth(r)
	}
	return count
}

// padToCol truncates or pads a line to reach exactly n visible columns.
func padToCol(s string, n int) string {
	vis := visualLen(s)
	if vis >= n {
		return truncateVisual(s, n)
	}
	return s + strings.Repeat(" ", n-vis)
}

// truncateVisual returns the first n visible characters (preserving ANSI codes).
func truncateVisual(s string, n int) string {
	var b strings.Builder
	inEsc := false
	count := 0
	for _, r := range s {
		if count >= n && !inEsc {
			break
		}
		b.WriteRune(r)
		if r == '\x1b' {
			inEsc = true
			continue
		}
		if inEsc {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				inEsc = false
			}
			continue
		}
		count += runewidth.RuneWidth(r)
	}
	return b.String()
}

// skipVisual skips the first n visible characters and returns the rest,
// replaying the last active ANSI escape sequence so styling is preserved.
func skipVisual(s string, n int) string {
	var lastESC strings.Builder
	var curESC strings.Builder
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
		if count >= n {
			return lastESC.String() + s[i:]
		}
		count += runewidth.RuneWidth(r)
	}
	return ""
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
