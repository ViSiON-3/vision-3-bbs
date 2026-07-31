package editor

// Text-changing operations: inserting, deleting, splitting and joining lines,
// and reflowing a paragraph.

// insertCharacter inserts a printable character at the cursor
func (e *FSEditor) insertCharacter(ch rune) {
	if e.insertMode {
		e.buffer.InsertChar(e.currentLine, e.currentCol, ch)
	} else {
		e.buffer.OverwriteChar(e.currentLine, e.currentCol, ch)
	}

	e.currentCol++
	e.modified = true

	// Check for word wrap
	newLine, newCol := e.wordWrapper.WrapAfterInsert(e.currentLine, e.currentCol)
	e.currentLine = newLine
	e.currentCol = newCol
}

// handleReturn processes the Enter key
func (e *FSEditor) handleReturn() {
	// Split line at cursor position
	if e.buffer.SplitLine(e.currentLine, e.currentCol) {
		// Mark the split line as a hard newline (user-created break)
		e.buffer.SetHardNewline(e.currentLine, true)
		e.currentLine++
		e.currentCol = 1
		e.modified = true
	}
}

// handleBackspace processes the Backspace key
func (e *FSEditor) handleBackspace() {
	newLine, newCol, changed := e.wordWrapper.HandleBackspace(e.currentLine, e.currentCol)
	if changed {
		e.currentLine = newLine
		e.currentCol = newCol
		e.modified = true
	}
}

// handleDeleteKey processes the Delete key
func (e *FSEditor) handleDeleteKey() {
	newLine, newCol, changed := e.wordWrapper.HandleDelete(e.currentLine, e.currentCol)
	if changed {
		e.currentLine = newLine
		e.currentCol = newCol
		e.modified = true
	}
}

// handleTab inserts tab spaces
func (e *FSEditor) handleTab() {
	// Insert 4 spaces for tab
	for i := 0; i < 4; i++ {
		e.insertCharacter(' ')
	}
}

// toggleInsertMode toggles between insert and overwrite modes
func (e *FSEditor) toggleInsertMode() {
	e.insertMode = !e.insertMode
}

// deleteWord deletes the word to the right of the cursor
func (e *FSEditor) deleteWord() {
	newLine, newCol, changed := e.wordWrapper.DeleteWord(e.currentLine, e.currentCol)
	if changed {
		e.currentLine = newLine
		e.currentCol = newCol
		e.modified = true
	}
}

// deleteLine deletes the current line
func (e *FSEditor) deleteLine() {
	if e.buffer.DeleteLine(e.currentLine) {
		e.modified = true
		// Ensure cursor is on a valid line
		if e.currentLine > e.buffer.GetLineCount() {
			e.currentLine = e.buffer.GetLineCount()
			if e.currentLine < 1 {
				e.currentLine = 1
			}
		}
		e.currentCol = 1
	}
}

// joinLines joins current line with next line and reflows
func (e *FSEditor) joinLines() {
	if e.currentLine >= e.buffer.GetLineCount() {
		return
	}
	// Clear hardNewline so reflow can flow across the boundary
	e.buffer.SetHardNewline(e.currentLine, false)
	if e.buffer.JoinLines(e.currentLine) {
		e.modified = true
		newLine, newCol := e.wordWrapper.ReflowRange(e.currentLine, e.currentLine, e.currentCol)
		e.currentLine = newLine
		e.currentCol = newCol
	}
}

// splitLine splits the current line at the cursor (user-initiated Ctrl+N)
func (e *FSEditor) splitLine() {
	if e.buffer.SplitLine(e.currentLine, e.currentCol) {
		// Mark the split line as a hard newline (user-created break)
		e.buffer.SetHardNewline(e.currentLine, true)
		e.currentLine++
		e.currentCol = 1
		e.modified = true
	}
}

// reformatParagraph reformats the current paragraph
func (e *FSEditor) reformatParagraph() {
	lastLine := e.wordWrapper.ReformatParagraph(e.currentLine)
	e.modified = true
	// Keep cursor on a valid line
	if e.currentLine > lastLine {
		e.currentLine = lastLine
	}
	e.currentCol = 1
	// Force full redraw
	e.screen.FullRedraw(e.buffer, e.topLine, e.currentLine, e.currentCol, e.insertMode)
}
