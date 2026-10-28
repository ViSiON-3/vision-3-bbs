package menu

import (
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/terminalio"
	"golang.org/x/term"
)

// drawMessageListScreen renders the complete message list display. str
// supplies the configurable labels; nil draws the shipped ones.
func drawMessageListScreen(terminal *term.Terminal, state *MessageListState, areaName string, confName string, outputMode ansi.OutputMode, str *config.StringsConfig) error {
	str = listStrings(str)
	// Reset attributes directly (bypass WriteProcessedBytes to avoid UTF-8 decode issues)
	if _, err := terminal.Write([]byte("\x1b[0m")); err != nil {
		return err
	}

	// Hide cursor for cleaner display
	if err := terminalio.WriteProcessedBytes(terminal, []byte("\x1b[?25l"), outputMode); err != nil {
		return err
	}

	// Clear screen
	clearSeq := ansi.ClearScreen()
	if err := terminalio.WriteProcessedBytes(terminal, []byte(clearSeq), outputMode); err != nil {
		return err
	}

	// Move to home position
	if err := terminalio.WriteProcessedBytes(terminal, []byte("\x1b[H"), outputMode); err != nil {
		return err
	}

	// Draw header with CP437 box characters (bright cyan borders)
	// Top border: ┌─...─┐ (total width: 79 chars = 1 + 77 + 1)
	header := fmt.Sprintf("|11┌%s┐|07\r\n", strings.Repeat("─", 77))
	if err := terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(header)), outputMode); err != nil {
		return err
	}

	// Title line with conference and area name (bright magenta text)
	name := areaName
	if confName != "" && confName != "Local" {
		name = fmt.Sprintf("%s > %s", confName, areaName)
	}
	title := fmt.Sprintf("%s - Message List", name)
	if state.Threaded {
		title = fmt.Sprintf(str.MsgListThreadedTitle, name)
	}
	title = truncateString(title, 75)
	// Runes, not bytes: truncateString clamps to 75 runes, so a multi-byte
	// title measured in bytes overflows the 77-column frame and hands
	// strings.Repeat a negative count.
	titleLen := utf8.RuneCountInString(title)
	padding := (77 - titleLen) / 2
	titleLine := fmt.Sprintf("|11│|13%s%s%s|11│|07\r\n",
		strings.Repeat(" ", padding),
		title,
		strings.Repeat(" ", 77-padding-titleLen))
	if err := terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(titleLine)), outputMode); err != nil {
		return err
	}

	// Separator: ├─...─┤ (total width: 79 chars = 1 + 77 + 1)
	separator := fmt.Sprintf("|11├%s┤|07\r\n", strings.Repeat("─", 77))
	if err := terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(separator)), outputMode); err != nil {
		return err
	}

	// Column headers, built from the same widths as the rows so the two cannot
	// drift apart. The status column has no label.
	columnHeaders := fmt.Sprintf("|11│|15%s%s %s %s %s %s |11│|07\r\n",
		strings.Repeat(" ", listStatusWidth),
		ansi.PadLeft("N#", listNumWidth),
		listCell("Subject", listSubjectWidth, outputMode),
		listCell("From", listFromWidth, outputMode),
		listCell("To", listToWidth, outputMode),
		listCell("Date", listDateWidth, outputMode))
	if state.Threaded {
		columnHeaders = fmt.Sprintf("|11│|15%s%s %s %s %s |11│|07\r\n",
			strings.Repeat(" ", listStatusWidth),
			ansi.PadLeft("N#", listNumWidth),
			listCell(str.MsgListThreadColumn, listThreadWidth, outputMode),
			listCell("From", listFromWidth, outputMode),
			listCell("Date", listDateWidth, outputMode))
	}
	if err := terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(columnHeaders)), outputMode); err != nil {
		return err
	}

	// Separator
	if err := terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(separator)), outputMode); err != nil {
		return err
	}

	// Draw messages for current page
	start, end := calculatePagination(len(state.Entries), state.ItemsPerPage, state.CurrentPage)
	for i := start; i < end; i++ {
		isHighlighted := (i - start) == state.SelectedIndex
		if err := drawMessageListLine(terminal, state.Entries[i], isHighlighted, outputMode, str); err != nil {
			return err
		}
	}

	// Fill remaining lines with empty rows if needed
	linesShown := end - start
	for i := linesShown; i < state.ItemsPerPage; i++ {
		emptyLine := fmt.Sprintf("|11│|07%s|11│|07\r\n", strings.Repeat(" ", 77))
		if err := terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(emptyLine)), outputMode); err != nil {
			return err
		}
	}

	// Bottom separator
	if err := terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(separator)), outputMode); err != nil {
		return err
	}

	// Pagination info (centered, bright cyan text)
	pageLine := pageInfoLine(state) + "\r\n"
	if err := terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(pageLine)), outputMode); err != nil {
		return err
	}

	// Separator
	if err := terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(separator)), outputMode); err != nil {
		return err
	}

	// Help/command line (centered, bright magenta text, with arrow characters)
	upArrow, downArrow := "\x18", "\x19" // CP437 arrows
	if outputMode == ansi.OutputModeUTF8 {
		upArrow, downArrow = "\u2191", "\u2193" // Unicode arrows
	}
	helpText := fmt.Sprintf(str.MsgListHelpFlat, upArrow, downArrow)
	if state.Threaded {
		helpText = fmt.Sprintf(str.MsgListHelpThreaded, upArrow, downArrow)
	}
	// Centred in runes, not bytes: the UTF-8 arrows are 3 bytes each but
	// occupy one column, so a byte length pads 4 columns short and pulls the
	// right border inward.
	helpLine := fmt.Sprintf("|11│|13%s|11│|07\r\n", ansi.Center(truncateString(helpText, 77), 77))
	if err := terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(helpLine)), outputMode); err != nil {
		return err
	}

	// Bottom border: └─...─┘ (total width: 79 chars = 1 + 77 + 1)
	// NOTE: No \r\n at end to prevent scrolling when cursor reaches bottom of terminal
	footer := fmt.Sprintf("|11└%s┘|07", strings.Repeat("─", 77))
	if err := terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(footer)), outputMode); err != nil {
		return err
	}

	return nil
}

// Message list column widths. They sum, with the single-space separators
// between them, to listInteriorWidth.
//
// Every cell is built to its exact width by listCell rather than with a %-Ns
// verb. Two reasons, both of which had already gone wrong here: fmt's width is
// a *minimum*, so "%3d" quietly grew to four columns at message 1000 and walked
// the right border out of the frame; and its padding counts bytes, so a
// multi-byte name padded short and pulled the border the other way.
const (
	listStatusWidth   = 1
	listNumWidth      = 5 // up to 99999; see listNumCell for what happens beyond
	listSubjectWidth  = 31
	listFromWidth     = 16
	listToWidth       = 11
	listDateWidth     = 8 // 01/02/06
	listInteriorWidth = 77

	// Four single-space separators between the six columns, plus one trailing
	// space so the date does not touch the right border.
	listSeparators = 5

	// Threaded mode replaces Subject and To with one Thread column (the tree
	// prefix and the subject or excerpt), so its rows have five columns
	// (status, N#, Thread, From, Date) with three separators between them
	// and the trailing space.
	listThreadWidth      = 43
	listThreadSeparators = 4
)

// listCell renders s as exactly width columns, truncating with an ellipsis and
// padding with spaces.
//
// Order matters. toCP437Safe emits one byte per rune, and CP437's high bytes
// are not valid UTF-8, so anything that decodes runes must run before the
// conversion: truncating afterwards turns every accented character into U+FFFD
// while leaving the column count correct, which is corruption a width check
// cannot see. Truncate and measure in Unicode, convert, then pad with ASCII
// spaces that are the same byte either way.
func listCell(s string, width int, outputMode ansi.OutputMode) string {
	s = ansi.TruncateRunes(s, width, "...")
	n := utf8.RuneCountInString(s)
	if outputMode == ansi.OutputModeCP437 {
		s = toCP437Safe(s)
	}
	if n < width {
		s += strings.Repeat(" ", width-n)
	}
	return s
}

// listNumCell right-aligns a message number in listNumWidth columns.
//
// A number too large for the column keeps its leading digits rather than
// widening the row: the frame holding is worth more than the last digit of a
// six-figure message number, and the number is only a label here — the reader
// shows it in full.
func listNumCell(n int) string {
	return ansi.PadLeft(ansi.TruncateRunes(strconv.Itoa(n), listNumWidth, ""), listNumWidth)
}

// listDateCell formats a message date for the list. A zero time renders blank
// rather than as year 1, which is what a message with no usable date carries.
// The format is ASCII, so it needs no output-mode handling.
func listDateCell(t time.Time) string {
	if t.IsZero() {
		return strings.Repeat(" ", listDateWidth)
	}
	return ansi.PadRight(ansi.TruncateRunes(t.Format("01/02/06"), listDateWidth, ""), listDateWidth)
}

// messageListRow builds every column of one message row except the status
// character, which the caller writes separately because it carries pipe codes
// in the unhighlighted case. The result is listInteriorWidth - listStatusWidth
// columns wide.
func messageListRow(entry MessageListEntry, outputMode ansi.OutputMode) string {
	// Trailing space so the date does not butt against the right border, the
	// way the status column keeps the number off the left one.
	return listNumCell(entry.MsgNum) + " " +
		listCell(entry.Subject, listSubjectWidth, outputMode) + " " +
		listCell(entry.From, listFromWidth, outputMode) + " " +
		listCell(entry.To, listToWidth, outputMode) + " " +
		listDateCell(entry.Date) + " "
}

// threadedListRow builds a threaded row after the status character: number,
// the Thread cell (tree prefix, then the subject or excerpt), author and
// date. plain leaves out the colour codes, for the highlighted row, which is
// drawn in reverse video as one colour. The result is listInteriorWidth -
// listStatusWidth columns wide.
//
//	5 │ ├─That was it. Font sticks now, but the …  PhosphorDotMatr… 10/20/26
//	  │ └─#4 deleted
func threadedListRow(entry MessageListEntry, plain bool, outputMode ansi.OutputMode, str *config.StringsConfig) string {
	str = listStrings(str)
	colour := func(code string) string {
		if plain {
			return ""
		}
		return code
	}
	prefix := treePrefix(threadRow{Depth: entry.Depth, Prefix: entry.Prefix})
	textWidth := max(listThreadWidth-utf8.RuneCountInString(prefix), 0)
	if outputMode == ansi.OutputModeCP437 {
		prefix = toCP437Safe(prefix)
	}
	if entry.Placeholder {
		label := fmt.Sprintf(str.MsgListDeletedPlaceholder, entry.MsgNum)
		return strings.Repeat(" ", listNumWidth) + " " +
			colour("|03") + prefix + colour("|08") + listCell(label, textWidth, outputMode) + " " +
			strings.Repeat(" ", listFromWidth) + " " + strings.Repeat(" ", listDateWidth) + " "
	}
	text, textColour := entry.Excerpt, "|07"
	if entry.OwnSubject {
		text, textColour = entry.Subject, "|15"
	}
	return listNumCell(entry.MsgNum) + " " +
		colour("|03") + prefix + colour(textColour) + listCell(text, textWidth, outputMode) + " " +
		colour("|15") + listCell(entry.From, listFromWidth, outputMode) + " " +
		listDateCell(entry.Date) + " "
}

// drawMessageListLine renders a single message line with optional
// highlighting. str supplies the placeholder label; nil draws the shipped one.
func drawMessageListLine(terminal *term.Terminal, entry MessageListEntry, isHighlighted bool, outputMode ansi.OutputMode, str *config.StringsConfig) error {
	// Format status character (aware of highlight state)
	statusStr := formatStatusChar(entry, isHighlighted)
	var row string
	switch {
	case entry.Placeholder:
		statusStr = " "
		row = threadedListRow(entry, false, outputMode, str)
	case entry.Threaded:
		row = threadedListRow(entry, isHighlighted, outputMode, str)
	default:
		row = messageListRow(entry, outputMode)
	}

	var line string
	if isHighlighted && !entry.Placeholder {
		// Use ANSI reverse video for highlighting (black on white)
		line = fmt.Sprintf("|11│\x1b[7m%s%s\x1b[27m|11│|07\r\n", statusStr, row)
	} else {
		// Normal display (bright white text on black)
		line = fmt.Sprintf("|11│|15%s%s|11│|07\r\n", statusStr, row)
	}

	return terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(line)), outputMode)
}

// drawMessageLineAtRow draws a single message line at a specific screen row (optimized refresh)
func drawMessageLineAtRow(terminal *term.Terminal, entry MessageListEntry, row int, isHighlighted bool, outputMode ansi.OutputMode, str *config.StringsConfig) error {
	// Position cursor at the specified row
	positionCmd := fmt.Sprintf("\x1b[%d;1H", row)
	if err := terminalio.WriteProcessedBytes(terminal, []byte(positionCmd), outputMode); err != nil {
		return err
	}

	// Draw the line
	return drawMessageListLine(terminal, entry, isHighlighted, outputMode, str)
}

// pageInfoLine is the centred "Page p of n [a-b of m messages]" row, inside
// its borders and without a line ending. It counts messages, not rows: a
// deleted placeholder is neither numbered nor counted.
func pageInfoLine(state *MessageListState) string {
	start, end := calculatePagination(len(state.Entries), state.ItemsPerPage, state.CurrentPage)
	first := state.selectableBefore(start) + 1
	last := state.selectableBefore(end)
	if last < first {
		first = last
	}
	pageInfo := fmt.Sprintf("Page %d of %d [%d-%d of %d messages]",
		state.CurrentPage, state.totalPages(), first, last, state.selectableCount())
	pageInfo = truncateString(pageInfo, 77)
	return fmt.Sprintf("|11│|11%s|11│|07", ansi.Center(pageInfo, 77))
}

// drawPageInfoAtRow draws the page info line at a specific row
func drawPageInfoAtRow(terminal *term.Terminal, state *MessageListState, row int, outputMode ansi.OutputMode) error {
	pageLine := pageInfoLine(state)

	// Position cursor and draw
	positionCmd := fmt.Sprintf("\x1b[%d;1H", row)
	if err := terminalio.WriteProcessedBytes(terminal, []byte(positionCmd), outputMode); err != nil {
		return err
	}
	return terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(pageLine)), outputMode)
}

// refreshPageContent redraws message lines and page info for page changes (optimized, no screen clear)
func refreshPageContent(terminal *term.Terminal, state *MessageListState, outputMode ansi.OutputMode, str *config.StringsConfig) error {
	// Calculate pagination
	start, _ := calculatePagination(len(state.Entries), state.ItemsPerPage, state.CurrentPage)

	// Redraw all message lines for current page
	// Message lines start at row 6 (1: top border, 2: title, 3: sep, 4: headers, 5: sep, 6+: messages)
	startRow := 6
	for i := 0; i < state.ItemsPerPage; i++ {
		row := startRow + i
		actualIndex := start + i

		if actualIndex < len(state.Entries) {
			// Draw message line
			isHighlighted := i == state.SelectedIndex
			if err := drawMessageLineAtRow(terminal, state.Entries[actualIndex], row, isHighlighted, outputMode, str); err != nil {
				return err
			}
		} else {
			// Draw empty line for remaining slots
			positionCmd := fmt.Sprintf("\x1b[%d;1H", row)
			if err := terminalio.WriteProcessedBytes(terminal, []byte(positionCmd), outputMode); err != nil {
				return err
			}
			emptyLine := fmt.Sprintf("|11│|07%s|11│|07", strings.Repeat(" ", 77))
			if err := terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(emptyLine)), outputMode); err != nil {
				return err
			}
		}
	}

	// Update page info line
	// Page info is at: startRow + itemsPerPage (messages end) + 1 (separator) = startRow + itemsPerPage + 1
	pageInfoRow := startRow + state.ItemsPerPage + 1
	return drawPageInfoAtRow(terminal, state, pageInfoRow, outputMode)
}
