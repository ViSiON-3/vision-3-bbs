package editor

// Cursor movement and the scrolling that keeps the cursor on screen. These do
// not change the buffer.

// moveCursorUp moves cursor up one line
func (e *FSEditor) moveCursorUp() {
	if e.currentLine > 1 {
		e.currentLine--
		// Adjust column if line is shorter
		lineLen := e.buffer.GetLineLength(e.currentLine)
		if e.currentCol > lineLen+1 {
			e.currentCol = lineLen + 1
		}
	}
}

// moveCursorDown moves cursor down one line
func (e *FSEditor) moveCursorDown() {
	lineCount := e.buffer.GetLineCount()
	if e.currentLine < lineCount {
		e.currentLine++
		// Adjust column if line is shorter
		lineLen := e.buffer.GetLineLength(e.currentLine)
		if e.currentCol > lineLen+1 {
			e.currentCol = lineLen + 1
		}
	}
}

// moveCursorLeft moves cursor left one character
func (e *FSEditor) moveCursorLeft() {
	if e.currentCol > 1 {
		e.currentCol--
	} else if e.currentLine > 1 {
		// Move to end of previous line
		e.currentLine--
		e.currentCol = e.buffer.GetLineLength(e.currentLine) + 1
	}
}

// moveCursorRight moves cursor right one character
func (e *FSEditor) moveCursorRight() {
	lineLen := e.buffer.GetLineLength(e.currentLine)
	if e.currentCol <= lineLen {
		e.currentCol++
	} else if e.currentLine < e.buffer.GetLineCount() {
		// Move to start of next line
		e.currentLine++
		e.currentCol = 1
	}
}

// moveCursorHome moves cursor to start of line
func (e *FSEditor) moveCursorHome() {
	e.currentCol = 1
}

// moveCursorEnd moves cursor to end of line
func (e *FSEditor) moveCursorEnd() {
	lineLen := e.buffer.GetLineLength(e.currentLine)
	e.currentCol = lineLen + 1
}

// moveCursorWordRight moves cursor to start of next word
func (e *FSEditor) moveCursorWordRight() {
	e.currentCol = e.wordWrapper.FindWordRight(e.currentLine, e.currentCol)
}

// pageUp scrolls up one page
func (e *FSEditor) pageUp() {
	scrollAmount := e.screen.GetScreenLines() - 1
	e.currentLine -= scrollAmount
	if e.currentLine < 1 {
		e.currentLine = 1
	}
}

// pageDown scrolls down one page
func (e *FSEditor) pageDown() {
	scrollAmount := e.screen.GetScreenLines() - 1
	lineCount := e.buffer.GetLineCount()
	e.currentLine += scrollAmount
	if e.currentLine > lineCount {
		e.currentLine = lineCount
	}
	if e.currentLine < 1 {
		e.currentLine = 1
	}
}

// ensureCursorVisible adjusts the view to keep the cursor visible
func (e *FSEditor) ensureCursorVisible() {
	screenLines := e.screen.GetScreenLines()
	oldTopLine := e.topLine

	// Check if cursor is above visible area
	if e.currentLine < e.topLine {
		e.topLine = e.currentLine
	}

	// Check if cursor is below visible area
	if e.currentLine >= e.topLine+screenLines {
		e.topLine = e.currentLine - screenLines + 1
		if e.topLine < 1 {
			e.topLine = 1
		}
	}

	// If topLine changed (scrolling occurred), clear screen cache to force redraw
	if e.topLine != oldTopLine {
		e.screen.ClearCache()
		e.lastTopLine = e.topLine
	}
}
