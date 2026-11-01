package editor

import (
	"strings"
	"unicode"
)

// WordWrapper handles word wrapping and paragraph formatting
type WordWrapper struct {
	buffer *MessageBuffer
}

// NewWordWrapper creates a new word wrapper
func NewWordWrapper(buffer *MessageBuffer) *WordWrapper {
	return &WordWrapper{
		buffer: buffer,
	}
}

// ReflowRange reflows soft-wrapped lines starting from startLine.
// It scans forward through consecutive soft-wrapped lines (hardNewline=false)
// and stops at the first hard newline, the end of the buffer, or a line under
// a different quote prefix. Text is collected, re-wrapped to MaxLineLength,
// and placed back into the buffer. A quoted paragraph is wrapped under its
// prefix: the prefix is taken off each line, the text rewrapped to the width
// left beside it, and the prefix put back on every line.
// cursorLine/cursorCol track the cursor through the reflow using a linear
// character offset, which is more robust than the old col-wrapPos arithmetic.
// Returns the adjusted cursor position (line, col).
func (ww *WordWrapper) ReflowRange(startLine, cursorLine, cursorCol int) (int, int) {
	lineCount := ww.buffer.GetLineCount()
	if startLine < 1 || startLine > lineCount {
		return cursorLine, cursorCol
	}

	// Find paragraph bounds: scan forward until hardNewline=true, end of
	// buffer, or a change of quote prefix, so a quote never takes in the
	// caller's own text or a quote at another depth.
	prefix := ww.buffer.QuotePrefix(startLine)
	endLine := startLine
	for endLine < lineCount && !ww.buffer.IsHardNewline(endLine) && ww.buffer.QuotePrefix(endLine+1) == prefix {
		endLine++
	}
	width := MaxLineLength - len(prefix)
	if width < minQuoteWrapWidth {
		width = minQuoteWrapWidth
	}
	endHardNewline := ww.buffer.IsHardNewline(endLine)

	// Collect text from all lines in the paragraph and compute cursor offset
	var collected strings.Builder
	cursorOffset := 0
	cursorFound := false

	for i := startLine; i <= endLine; i++ {
		// Spaces in the buffer are editable content. Trimming them here would
		// make a single Backspace delete extra characters and move the cursor.
		line := ww.buffer.GetLine(i)[len(prefix):]
		// A soft wrap stands for the space between two words, except where it
		// split a word too long for the margin.
		if i > startLine && collected.Len() > 0 && len(line) > 0 && !ww.buffer.IsWordSplit(i-1) {
			collected.WriteByte(' ')
			if !cursorFound {
				cursorOffset++
			}
		}
		if i == cursorLine && !cursorFound {
			col := cursorCol - 1 - len(prefix) // 0-based, within the text
			if col < 0 {
				col = 0 // a cursor in the prefix goes to the start of the text
			}
			if col > len(line) {
				col = len(line)
			}
			cursorOffset += col
			cursorFound = true
		} else if !cursorFound {
			cursorOffset += len(line)
		}
		collected.WriteString(line)
	}

	text := collected.String()
	if cursorOffset > len(text) {
		cursorOffset = len(text)
	}

	// Re-wrap the collected text into lines of width.
	// Track start offsets in the collected text for cursor mapping.
	var outputLines []string
	var lineStarts []int
	var splits []bool // splits[i]: output line i ends inside a word
	pos := 0

	for pos < len(text) {
		lineStarts = append(lineStarts, pos)
		remaining := text[pos:]

		if len(remaining) <= width {
			outputLines = append(outputLines, remaining)
			splits = append(splits, false)
			break
		}

		wrapPos := findWrapPosition(remaining, width)
		split := wrapPos <= 0
		if split {
			wrapPos = width
		}

		outputLines = append(outputLines, remaining[:wrapPos])
		splits = append(splits, split)
		if split {
			// No space was consumed, so there is none to restore below.
			pos += wrapPos
			continue
		}

		// The soft line break represents one separator space (restored when
		// collecting above). Preserve any additional spaces on either side.
		pos += wrapPos
		if pos < len(text) && text[pos] == ' ' {
			pos++
			if pos == len(text) {
				// The separator was the last character — typically the space
				// just typed past the margin. Open an empty continuation line
				// so the cursor moves onto it; otherwise the next letter lands
				// on this line and the following wrap glues the two words
				// together, losing the space.
				lineStarts = append(lineStarts, pos)
				outputLines = append(outputLines, "")
				splits = append(splits, false)
			}
		}
	}

	if len(outputLines) == 0 {
		outputLines = []string{""}
		lineStarts = []int{0}
		splits = []bool{false}
	}
	for i := range outputLines {
		outputLines[i] = prefix + outputLines[i]
	}

	// Place output lines back into the buffer
	originalCount := endLine - startLine + 1
	newCount := len(outputLines)

	// Set content for lines we can reuse
	commonCount := originalCount
	if newCount < commonCount {
		commonCount = newCount
	}
	for i := 0; i < commonCount; i++ {
		ww.buffer.SetLine(startLine+i, outputLines[i])
		ww.buffer.SetHardNewline(startLine+i, false)
		ww.buffer.SetWordSplit(startLine+i, splits[i])
		ww.buffer.SetQuotePrefix(startLine+i, prefix)
	}

	if newCount > originalCount {
		// Need more lines — insert extras; track actual count written
		actualNew := originalCount
		for i := originalCount; i < newCount; i++ {
			if !ww.buffer.InsertLine(startLine + i) {
				// Buffer full: append remaining text to last written line
				last := startLine + actualNew - 1
				tail := text[lineStarts[i]:] // text only; the line has the prefix
				if tail == "" {
					// Only the separator space was left to place (see the
					// empty continuation line above). Keep it on the last
					// line so the next word typed does not join this one.
					ww.buffer.SetLine(last, ww.buffer.GetLine(last)+" ")
				} else {
					curr := ww.buffer.GetLine(last)
					if ww.buffer.IsWordSplit(last) {
						// The tail carries on a word split at the margin.
						ww.buffer.SetLine(last, curr+tail)
						ww.buffer.SetWordSplit(last, false)
					} else if curr != "" {
						ww.buffer.SetLine(last, curr+" "+tail)
					} else {
						ww.buffer.SetLine(last, tail)
					}
				}
				break
			}
			ww.buffer.SetLine(startLine+i, outputLines[i])
			ww.buffer.SetHardNewline(startLine+i, false)
			ww.buffer.SetWordSplit(startLine+i, splits[i])
			ww.buffer.SetQuotePrefix(startLine+i, prefix)
			actualNew++
		}
		// Truncate tracking to what was actually written
		if actualNew < newCount {
			newCount = actualNew
			lineStarts = lineStarts[:newCount]
		}
	} else if newCount < originalCount {
		// Need fewer lines — delete extras
		for i := 0; i < originalCount-newCount; i++ {
			ww.buffer.DeleteLine(startLine + newCount)
		}
	}

	// Clamp newCount to buffer bounds
	bufLineCount := ww.buffer.GetLineCount()
	if startLine+newCount-1 > bufLineCount {
		newCount = bufLineCount - startLine + 1
	}

	// Last output line inherits the original paragraph-end hardNewline. Its
	// end is the paragraph's end, so it splits no word.
	ww.buffer.SetHardNewline(startLine+newCount-1, endHardNewline)
	ww.buffer.SetWordSplit(startLine+newCount-1, false)

	// Map cursor offset to output line/col using lineStarts.
	// Use actual buffer line lengths (not outputLines) since buffer-full
	// fallback may have appended overflow text to the last written line.
	if cursorOffset >= len(text) {
		lastLine := startLine + newCount - 1
		return lastLine, ww.buffer.GetLineLength(lastLine) + 1
	}

	// Columns below are within the text; the prefix comes before them.
	for i := 0; i < len(lineStarts) && i < newCount; i++ {
		nextStart := len(text) + 1
		if i+1 < len(lineStarts) {
			nextStart = lineStarts[i+1]
		}
		if cursorOffset >= lineStarts[i] && cursorOffset < nextStart {
			localOffset := cursorOffset - lineStarts[i]
			actualLen := ww.buffer.GetLineLength(startLine+i) - len(prefix)
			if localOffset > actualLen {
				localOffset = actualLen
			}
			return startLine + i, len(prefix) + localOffset + 1
		}
	}

	// Fallback: cursor at end of last output line
	lastLine := startLine + newCount - 1
	return lastLine, ww.buffer.GetLineLength(lastLine) + 1
}

// WrapAfterInsert handles word wrap after a character insertion.
// Fast path: only reflows if the current line exceeds MaxLineLength.
func (ww *WordWrapper) WrapAfterInsert(lineNum, cursorCol int) (int, int) {
	line := ww.buffer.GetLine(lineNum)
	if len(line) <= MaxLineLength {
		return lineNum, cursorCol
	}
	return ww.ReflowRange(lineNum, lineNum, cursorCol)
}

// minQuoteWrapWidth is the narrowest width a quoted paragraph is wrapped to
// beside its prefix, matching the quote picker's wrapQuoted.
const minQuoteWrapWidth = 10

// findWrapPosition finds the best position to wrap the line at width.
// Returns the index of the last space at or before width, or -1 if there is
// none past the first column.
func findWrapPosition(line string, width int) int {
	for i := width; i > 0; i-- {
		if i < len(line) && line[i] == ' ' {
			return i
		}
	}
	return -1
}

// ReformatParagraph reformats a paragraph starting at the specified line.
// Uses ReflowRange to redistribute words across soft-wrapped lines.
// Stops at hard newlines or blank lines.
func (ww *WordWrapper) ReformatParagraph(startLine int) int {
	if ww.buffer.IsLineEmpty(startLine) {
		return startLine
	}

	// ReflowRange handles paragraph bounds via hardNewline flags
	newLine, _ := ww.ReflowRange(startLine, startLine, 1)

	// Find the last line of the reformatted paragraph
	endLine := newLine
	lineCount := ww.buffer.GetLineCount()
	for endLine < lineCount && !ww.buffer.IsHardNewline(endLine) && !ww.buffer.IsLineEmpty(endLine+1) {
		endLine++
	}
	return endLine
}

// reflowParagraph reflows the paragraph around lineNum after an edit to it. A
// shorter line can let the words after it move up, and if its first word got
// shorter, by a deletion or a space typed into it, that word can move up onto
// the line above, so the reflow starts at the top of the paragraph. The cursor
// (lineNum, col) is carried through, and may end up on a line above lineNum.
func (ww *WordWrapper) reflowParagraph(lineNum, col int) (int, int) {
	return ww.ReflowRange(ww.paragraphReflowStart(lineNum), lineNum, col)
}

// paragraphReflowStart returns the line to start a reflow on after an edit on
// lineNum: the first line of its paragraph, walking back over soft wraps.
//
// A line just emptied, down to its quote prefix if it has one, stays where
// it is. The soft break above it stands for
// the space between words, and starting above would drop that space, so the
// next word typed would fuse onto the line above.
//
// The walk may cross a word split at the margin: the buffer's word-split flag
// tells ReflowRange to rejoin it without a space.
func (ww *WordWrapper) paragraphReflowStart(lineNum int) int {
	prefix := ww.buffer.QuotePrefix(lineNum)
	if ww.buffer.GetLineLength(lineNum) == len(prefix) {
		return lineNum
	}
	for lineNum > 1 && !ww.buffer.IsHardNewline(lineNum-1) && !ww.buffer.IsLineEmpty(lineNum-1) &&
		ww.buffer.QuotePrefix(lineNum-1) == prefix {
		lineNum--
	}
	return lineNum
}

// HandleBackspace handles backspace at the given position.
// Deletes the character before the cursor and reflows the paragraph, pulling
// words up from the lines below and onto the line above. At start-of-line,
// joins with the previous line and reflows.
// Returns new cursor position (line, col) and whether a change was made.
func (ww *WordWrapper) HandleBackspace(lineNum, col int) (int, int, bool) {
	if col > 1 {
		if ww.buffer.DeleteChar(lineNum, col-1) {
			newLine, newCol := ww.reflowParagraph(lineNum, col-1)
			return newLine, newCol, true
		}
		return lineNum, col, false
	}

	// At start of line - join with previous line. JoinLines gives the combined
	// line the second line's hard-newline flag, so a paragraph break after it
	// survives and the reflow stops there.
	if lineNum > 1 {
		prevLine := ww.buffer.GetLine(lineNum - 1)
		prevLen := len(prevLine)

		if ww.buffer.JoinLines(lineNum - 1) {
			newLine, newCol := ww.reflowParagraph(lineNum-1, prevLen+1)
			return newLine, newCol, true
		}
	}

	return lineNum, col, false
}

// HandleDelete handles delete at the given position.
// Deletes the character at the cursor and reflows, or joins with the next
// line if at end-of-line.
// Returns new cursor position (line, col) and whether a change was made.
func (ww *WordWrapper) HandleDelete(lineNum, col int) (int, int, bool) {
	lineLen := ww.buffer.GetLineLength(lineNum)

	if col <= lineLen {
		if ww.buffer.DeleteChar(lineNum, col) {
			newLine, newCol := ww.reflowParagraph(lineNum, col)
			return newLine, newCol, true
		}
		return lineNum, col, false
	}

	// At end of line - join with next line. As in HandleBackspace, the combined
	// line keeps the next line's hard-newline flag.
	if lineNum < ww.buffer.GetLineCount() {
		if ww.buffer.JoinLines(lineNum) {
			newLine, newCol := ww.reflowParagraph(lineNum, col)
			return newLine, newCol, true
		}
	}

	return lineNum, col, false
}

// DeleteWord deletes the word to the right of the cursor and reflows.
// Returns new cursor position (line, col) and whether a change was made.
func (ww *WordWrapper) DeleteWord(lineNum, col int) (int, int, bool) {
	line := ww.buffer.GetLine(lineNum)
	if col > len(line) {
		return lineNum, col, false
	}

	i := col - 1
	if i >= len(line) {
		return lineNum, col, false
	}

	// Skip initial whitespace
	for i < len(line) && unicode.IsSpace(rune(line[i])) {
		i++
	}
	// Skip word characters
	for i < len(line) && !unicode.IsSpace(rune(line[i])) {
		i++
	}

	if i > col-1 {
		newLine := line[:col-1] + line[i:]
		ww.buffer.SetLine(lineNum, newLine)
		newLn, newCol := ww.reflowParagraph(lineNum, col)
		return newLn, newCol, true
	}

	return lineNum, col, false
}

// DeleteLine deletes lineNum and closes up the paragraph around it. A line
// that ended its paragraph passes its hard break to the line above, so that
// line does not run on into the next paragraph. A line that held the middle of
// a split word leaves the parts either side of it as two words. Returns the
// cursor position for the start of the text that followed the deleted line,
// and whether a line was deleted.
func (ww *WordWrapper) DeleteLine(lineNum int) (int, int, bool) {
	wasHard := ww.buffer.IsHardNewline(lineNum)
	if !ww.buffer.DeleteLine(lineNum) {
		return lineNum, 1, false
	}
	if lineNum > 1 {
		if wasHard {
			ww.buffer.SetHardNewline(lineNum-1, true)
		}
		ww.buffer.SetWordSplit(lineNum-1, false)
	}
	if lineNum > ww.buffer.GetLineCount() {
		// The last line went; the cursor goes to the start of the new last.
		return ww.buffer.GetLineCount(), 1, true
	}
	newLine, newCol := ww.reflowParagraph(lineNum, 1)
	return newLine, newCol, true
}

// FindWordLeft finds the start of the word to the left
func (ww *WordWrapper) FindWordLeft(lineNum, col int) int {
	line := ww.buffer.GetLine(lineNum)
	if col <= 1 {
		return 1
	}

	pos := col - 2 // Start before cursor

	// Skip trailing whitespace
	for pos >= 0 && pos < len(line) && unicode.IsSpace(rune(line[pos])) {
		pos--
	}
	// Skip word characters
	for pos >= 0 && pos < len(line) && !unicode.IsSpace(rune(line[pos])) {
		pos--
	}

	return pos + 2 // Convert to 1-based and move to start of word
}

// FindWordRight finds the start of the word to the right
func (ww *WordWrapper) FindWordRight(lineNum, col int) int {
	line := ww.buffer.GetLine(lineNum)
	if col > len(line) {
		return len(line) + 1
	}

	pos := col - 1 // Current position (0-based)

	// Skip current word
	for pos < len(line) && !unicode.IsSpace(rune(line[pos])) {
		pos++
	}
	// Skip whitespace
	for pos < len(line) && unicode.IsSpace(rune(line[pos])) {
		pos++
	}

	return pos + 1 // Convert to 1-based
}
