package stringeditor

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"

	"github.com/ViSiON-3/vision-3-bbs/internal/tuiart"
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
	// Panel styles follow the Pascal original's SetColor() attributes, now
	// expressed in the shared DOS palette so ./strings and ./config render the
	// same colors. Every panel style names an explicit background: the panel
	// sits on top of the backdrop art and must be opaque.
	//
	//   SetColor(16) = bg=blue, fg=black       (fill + separators)
	//   SetColor(25) = bg=blue, fg=light blue  (labels)
	//   SetColor(31) = bg=blue, fg=white       (values)
	//   SetColor(27) = bg=blue, fg=light cyan  (page label)
	statusBarFillStyle      = tuiart.Color(dosBlue, dosBlack)
	statusBarLabelStyle     = tuiart.Color(dosBlue, dosLightBlue)
	statusBarValueStyle     = tuiart.Color(dosBlue, dosWhite).Bold(true)
	statusBarPageLabelStyle = tuiart.Color(dosBlue, dosLightCyan)

	// Item number and brackets: dark gray on the panel's black ground
	bracketStyle = tuiart.Color(dosBlack, dosDarkGray)

	// Normal item label
	labelNormalStyle = tuiart.Color(dosBlack, dosLightGray)

	// Highlighted item: white on the magenta bar (Pascal BarColor=95)
	labelHighlightStyle   = tuiart.Color(dosMagenta, dosWhite).Bold(true)
	bracketHighlightStyle = tuiart.Color(dosMagenta, dosLightCyan)

	// Panel background for value text and blank filler
	panelStyle = tuiart.Color(dosBlack, dosLightGray)

	// Description caption, drawn over the backdrop below the panel
	descriptionStyle = tuiart.Color(dosBlack, dosLightMagenta)

	// Dialog box styles
	dialogBorderStyle       = tuiart.Color(dosBlue, dosWhite)
	dialogTextStyle         = tuiart.Color(dosBlue, dosLightCyan)
	dialogButtonStyle       = tuiart.Color(dosBlue, dosLightGray)
	dialogButtonActiveStyle = tuiart.Color(dosMagenta, dosWhite).Bold(true)

	// Search bar
	searchLabelStyle = tuiart.Color(dosBlack, dosYellow).Bold(true)

	// Message flash
	messageStyle = tuiart.Color(dosBlack, dosLightCyan)

	// Edit mode indicator
	editingStyle = tuiart.Color(dosBlue, dosYellow).Bold(true)

	// Rejected input (malformed escape sequence)
	errorStyle = tuiart.Color(dosRed, dosWhite).Bold(true)
)

// headerTitle is the persistent title bar text, matching ./config's format.
const headerTitle = "-- ViSiON/3 String Configuration v1.0 --"

// helpText is the keyboard reference drawn on the last row; helpTextCompact is
// the same set of keys tightened to fit an 80-column terminal, so no shortcut
// is ever truncated away at the minimum size.
const (
	helpText        = "Enter Edit  |  F1 Prefill  |  F3 Revert  |  F4 Default  |  F10 Save  |  / Search  |  Esc Quit"
	helpTextCompact = "Enter Edit  F1 Prefill  F3 Revert  F4 Default  F10 Save  / Search  Esc Quit"
)

// helpBarText picks the widest help variant that fits the terminal.
func helpBarText(width int) string {
	if cellWidth(helpText) <= width {
		return helpText
	}
	return helpTextCompact
}

// View implements tea.Model.
//
// The screen is a global header bar, the DOS list panel centered over the
// backdrop art, the message and description rows, and the help bar on the last
// line. The panel keeps the Pascal original's flat three-column layout; only
// the surrounding chrome is shared with ./config.
func (m Model) View() string {
	var b strings.Builder

	panelW := m.panelWidth()
	padL := max(0, (m.width-panelW)/2)
	padR := max(0, m.width-padL-panelW)
	row := 0

	line := func(s string) {
		b.WriteString(s)
		b.WriteByte('\n')
		row++
	}
	// panelLine draws one panel row with backdrop art filling both margins.
	panelLine := func(content string) {
		line(m.backdrop.Segment(row, 0, padL) + content +
			m.backdrop.Segment(row, m.width-padR, padR))
	}
	bgLines := func(n int) {
		for i := 0; i < n; i++ {
			line(m.backdrop.Line(row))
		}
	}

	// Vertical centering: the panel grows with the terminal until the page size
	// caps out, and the leftover rows become backdrop above and below it.
	extraV := max(0, m.height-chromeRows-m.pageSize)
	topPad := extraV / 2
	bottomPad := extraV - topPad

	line(tuiart.HeaderBarStyle.Render(tuiart.CenterText(headerTitle, m.width)))
	bgLines(topPad)

	panelLine(m.renderStatusBar(panelW))
	panelLine(m.renderColumnHeader(panelW))

	pageStart := m.page * m.pageSize
	pageEnd := min(pageStart+m.pageSize, len(m.entries))
	for i := 0; i < m.pageSize; i++ {
		idx := pageStart + i
		if idx < pageEnd {
			panelLine(m.renderItem(idx, panelW))
		} else {
			panelLine(panelStyle.Render(strings.Repeat(" ", panelW)))
		}
	}

	panelLine(m.renderMessageBar(panelW))
	line(m.renderDescriptionBar(row))
	bgLines(bottomPad)
	b.WriteString(tuiart.HelpBarStyle.Render(tuiart.CenterText(helpBarText(m.width), m.width)))

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

// panelWidth returns the width of the DOS list panel. It is 80 columns on a
// minimum terminal, widens on a larger one while keeping artMargin columns of
// backdrop on each side, and stops at maxPanelWidth.
func (m Model) panelWidth() int {
	w := m.width - 2*artMargin
	if w > maxPanelWidth {
		w = maxPanelWidth
	}
	if w < minWidth {
		w = minWidth
	}
	if w > m.width {
		w = m.width
	}
	return w
}

// valueWidth returns the terminal cells available for a value preview, which is
// everything in the panel to the right of the label column.
func (m Model) valueWidth() int {
	return max(10, m.panelWidth()-labelCol)
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
func (m Model) renderStatusBar(panelW int) string {
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
	if visLen > panelW {
		return truncateVisual(content, panelW)
	}

	return content + statusBarFillStyle.Render(strings.Repeat(" ", panelW-visLen))
}

// renderColumnHeader creates a subtle column header line. Its columns line up
// with renderItem: the name sits over the labels and "Value" over labelCol.
func (m Model) renderColumnHeader(panelW int) string {
	const nameHeading = "  # Name"
	header := nameHeading +
		strings.Repeat(" ", labelCol-len(nameHeading)) +
		"Value"
	if pad := panelW - cellWidth(header); pad > 0 {
		header += strings.Repeat(" ", pad)
	}
	return bracketStyle.Render(header)
}

// renderItem renders a single list item.
func (m Model) renderItem(idx, panelW int) string {
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

	// Pad the value column out to the panel edge so the row is opaque against
	// the backdrop art behind it.
	if pad := panelW - visualLen(line); pad > 0 {
		line += panelStyle.Render(strings.Repeat(" ", pad))
	}
	return line
}

// renderMessageBar renders the message/mode indicator line. Every branch is
// clipped and padded to exactly the panel width so a long key, a long escape
// error, or a narrow terminal cannot wrap the row.
func (m Model) renderMessageBar(panelW int) string {
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
		return padRowStyled(searchLabelStyle.Render(" Search: ")+m.searchInput.View(),
			panelW, panelStyle)
	default:
		if m.message == "" {
			return panelStyle.Render(strings.Repeat(" ", panelW))
		}
		text = " " + m.message
	}

	if cellWidth(text) > panelW {
		text = truncateVisual(text, panelW)
	}
	return style.Render(text) + panelStyle.Render(strings.Repeat(" ", panelW-cellWidth(text)))
}

// padRowStyled clips or pads an already-styled row to exactly width cells,
// filling any shortfall with the given background style.
func padRowStyled(rendered string, width int, fill lipgloss.Style) string {
	vis := visualLen(rendered)
	if vis > width {
		return truncateVisual(rendered, width)
	}
	return rendered + fill.Render(strings.Repeat(" ", width-vis))
}

// renderDescriptionBar renders the caption for the current item, centered over
// the backdrop art on the given screen row.
func (m Model) renderDescriptionBar(row int) string {
	desc := ""
	if m.cursor >= 0 && m.cursor < len(m.entries) {
		desc = m.entries[m.cursor].Description
		if desc == "" {
			desc = m.entries[m.cursor].Key
		}
	}
	if desc == "" {
		return m.backdrop.Line(row)
	}
	// Pad with a space either side so the caption does not butt up against the
	// shaded background.
	desc = " " + desc + " "
	if cellWidth(desc) > m.width {
		desc = truncateVisual(desc, m.width)
	}
	left := (m.width - cellWidth(desc)) / 2
	right := m.width - left - cellWidth(desc)
	return m.backdrop.Segment(row, 0, left) +
		descriptionStyle.Render(desc) +
		m.backdrop.Segment(row, m.width-right, right)
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
