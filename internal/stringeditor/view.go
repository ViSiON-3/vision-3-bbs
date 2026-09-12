package stringeditor

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"

	"github.com/ViSiON-3/vision-3-bbs/internal/stringformat"
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

	// A fallback preview is what the BBS prints, not what the file holds, so
	// it is drawn dimmed to read as informational rather than as a value.
	fallbackStyle = tuiart.Color(dosBlack, dosDarkGray)

	// State marker column, between the label and the value.
	markerStyle = tuiart.Color(dosBlack, dosLightCyan)

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
	helpText        = "Enter Edit  |  F1 Prefill  |  F3 Revert  |  F4 Default  |  F10 Save  |  / Search  |  ^R Reserved  |  Esc Quit"
	helpTextCompact = "Enter Edit  F1 Prefill  F3 Revert  F4 Default  F10 Save  / Search  ^R  Esc Quit"
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

// panelWidth returns the width of the DOS list panel for this terminal.
func (m Model) panelWidth() int { return panelWidthFor(m.width) }

// panelWidthFor fills the terminal apart from a one-column border of
// background on each side, and never drops below the 80 columns the DOS layout
// needs.
func panelWidthFor(width int) int {
	w := width - 2*artMargin
	if w < minWidth {
		w = minWidth
	}
	if w > width {
		w = width
	}
	return w
}

// valueWidth returns the terminal cells available for a value preview, which is
// everything in the panel to the right of the label column.
func (m Model) valueWidth() int { return valueWidthFor(m.width) }

// renderStatusBar creates the panel's status bar, following the Pascal
// original's SetColor/Write sequence:
//
//	SetColor(16); ClrEOL;  (fill entire row blue)
//	SetColor(25); Write(' Current Topic Number:');
//	SetColor(31); Write(' '+strr(top));
//	SetColor(16); Write(' │');
//	SetColor(27); Write(' Current Page:');
//	SetColor(31); Write(' '+strr(page));
//
// The original also wrote the program name here. That now lives in the shared
// title bar one row above, and repeating it pushed the page number off the
// right edge of an 80-column terminal, so this bar carries only the position.
func (m Model) renderStatusBar(panelW int) string {
	// First item on current page (1-based, matching Pascal's top variable)
	topItem := 0
	if start := m.page * m.pageSize; start < len(m.entries) {
		topItem = m.entries[start].Number
	}
	pageNum := m.page + 1

	content := statusBarLabelStyle.Render(" Current Topic Number:") +
		statusBarValueStyle.Render(fmt.Sprintf(" %d", topItem)) +
		statusBarFillStyle.Render(" │") +
		statusBarPageLabelStyle.Render(" Current Page:") +
		statusBarValueStyle.Render(fmt.Sprintf(" %d", pageNum)) +
		statusBarPageLabelStyle.Render(" of") +
		statusBarValueStyle.Render(fmt.Sprintf(" %d", m.numPages))

	// Measure the styled text directly rather than a parallel plain copy, which
	// drifts out of sync.
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
		strings.Repeat(" ", markerWidth) +
		"Value"
	if pad := panelW - cellWidth(header); pad > 0 {
		header += strings.Repeat(" ", pad)
	}
	return bracketStyle.Render(header)
}

// renderItem renders a single list item: number, label, a one-character state
// marker, and the value preview.
func (m Model) renderItem(idx, panelW int) string {
	entry := m.entries[idx]
	isSelected := idx == m.cursor
	value, isFallback := m.previewValue(entry.Key)
	state := m.stateOf(entry.Key)
	valueWidth := m.valueWidth()

	// The item number is the entry's stable position in the catalog, so it
	// does not shift when reserved entries are filtered out of the view.
	numStr := fmt.Sprintf("%*d", numWidth, entry.Number)

	// The selection bar covers the number and the bracketed label and stops at
	// the closing bracket. The state marker and the value beyond it keep the
	// panel's own background, so the bar reads as the label's highlight rather
	// than as a stripe running into the value column.
	numStyle, labelStyle := bracketStyle, labelNormalStyle
	if isSelected {
		numStyle, labelStyle = bracketHighlightStyle, labelHighlightStyle
	}

	line := numStyle.Render(numStr) +
		numStyle.Render("[") +
		labelStyle.Render(padOrTrunc(entry.Label, labelWidth)) +
		numStyle.Render("]") +
		markerStyle.Render(state.marker())

	if isSelected && m.mode == modeEdit {
		// Clip the input to the value column so a long escaped value can never
		// push the row past the panel edge.
		input := m.textInput.View()
		if visualLen(input) > valueWidth {
			input = truncateVisual(input, valueWidth)
		}
		line += input
	} else if isFallback {
		line += renderDimString(value, valueWidth)
	} else {
		line += RenderColorString(value, valueWidth)
	}

	// Pad the value column out to the panel edge so the row is opaque against
	// the background behind it.
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
		switch {
		case m.message != "":
			text = " " + m.message
		default:
			// With no flash message, use the row to name the selected entry's
			// state, so a blank value column is never ambiguous.
			state := m.currentState()
			if state == stateDefault || state == stateCustom {
				return panelStyle.Render(strings.Repeat(" ", panelW))
			}
			text = " " + state.marker() + "  " + state.label()
			style = fallbackStyle
		}
	}

	if cellWidth(text) > panelW {
		text = truncateVisual(text, panelW)
	}
	return style.Render(text) + panelStyle.Render(strings.Repeat(" ", panelW-cellWidth(text)))
}

// signatureFor describes the arguments a formatted key must consume, or "" for
// a string that is printed verbatim.
func (m Model) signatureFor(key string) string {
	if !stringformat.IsFormatted(key) {
		return ""
	}
	def, ok := m.defaultFor(key)
	if !ok {
		return ""
	}
	return stringformat.Signature(key, def)
}

// currentState classifies the entry under the cursor.
func (m Model) currentState() valueState {
	if m.cursor < 0 || m.cursor >= len(m.entries) {
		return stateDefault
	}
	return m.stateOf(m.entries[m.cursor].Key)
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
		entry := m.entries[m.cursor]
		desc = entry.Description
		if desc == "" {
			desc = entry.Key
		}
		// Name the arguments a formatted string must keep, so the sysop can
		// see the contract while editing rather than after breaking it.
		if sig := m.signatureFor(entry.Key); sig != "" {
			desc += "  [" + sig + "]"
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
