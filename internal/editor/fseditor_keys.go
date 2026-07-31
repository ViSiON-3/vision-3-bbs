package editor

// Key and command dispatch: turning a decoded key or an editor command into the
// edit and cursor operations that carry it out.

// handleKey processes a single key press
func (e *FSEditor) handleKey(key int) {
	// Handle key based on type
	switch key {
	case KeyEnter:
		e.handleReturn()
	case KeyBackspace:
		e.handleBackspace()
	case KeyTab:
		e.handleTab()

	// Escape: show option lightbar menu (Save / Abort / Edit / Help / Quote)
	case KeyEsc:
		cmdType := e.commands.ShowEscapeMenu(e.input)
		if cmdType != CommandNone {
			e.handleCommand(cmdType)
		}

	// Editor commands (shown in footer: CTRL (A)Abort (Z)Save (Q)Quote)
	case KeyCtrlA: // Abort
		e.handleCommand(CommandAbort)
	case KeyCtrlZ: // Save
		e.handleCommand(CommandSave)
	case KeyCtrlQ: // Quote
		e.handleCommand(CommandQuote)

	// Navigation
	case KeyCtrlE: // Up
		e.moveCursorUp()
	case KeyCtrlX: // Down
		e.moveCursorDown()
	case KeyCtrlS: // Left
		e.moveCursorLeft()
	case KeyCtrlD: // Right
		e.moveCursorRight()
	case KeyCtrlW: // Home
		e.moveCursorHome()
	case KeyCtrlP: // End
		e.moveCursorEnd()
	case KeyCtrlR: // Page Up
		e.pageUp()
	case KeyCtrlC: // Page Down (note: normally quit in terminals, but remapped here)
		e.pageDown()
	case KeyCtrlF: // Word Right
		e.moveCursorWordRight()

	// Edit commands
	case KeyCtrlV: // Toggle insert/overwrite
		e.toggleInsertMode()
	case KeyCtrlG: // Delete character at cursor
		e.handleDeleteKey()
	case KeyCtrlT: // Delete word
		e.deleteWord()
	case KeyCtrlY: // Delete line
		e.deleteLine()
	case KeyCtrlJ: // Join lines
		e.joinLines()
	case KeyCtrlN: // Split line
		e.splitLine()
	case KeyCtrlB: // Reformat paragraph
		e.reformatParagraph()
	case KeyCtrlL: // Redraw screen
		e.redrawScreen()

	default:
		// Check if it's a printable character
		if IsPrintable(key) {
			e.insertCharacter(rune(key))
		}
	}
}

// handleCommand processes editor commands (CTRL-A/Z/Q and help/view)
func (e *FSEditor) handleCommand(cmdType CommandType) {
	switch cmdType {
	case CommandSave:
		if e.commands.HandleSave() {
			// Show "Saving..." in the prompt row before exiting
			e.screen.GoXY(1, e.screen.PromptRow())
			e.screen.ClearEOL()
			e.screen.WriteDirectProcessed("|15Saving...")
			e.saved = true
			e.quit = true
		} else {
			// Error message already written; wait for key then restore footer
			_, _ = e.input.ReadKey() // wait for any key
			e.screen.DisplayFooter()
			e.screen.RefreshScreen(e.buffer, e.topLine, e.currentLine, e.currentCol, e.insertMode, true)
		}

	case CommandAbort:
		if e.commands.HandleAbort(e.input) {
			e.saved = false
			e.quit = true
		} else {
			e.screen.RefreshScreen(e.buffer, e.topLine, e.currentLine, e.currentCol, e.insertMode, true)
		}

	case CommandQuote:
		line, col := e.commands.HandleQuote(e.input, e.currentLine, e.currentCol)
		e.currentLine = line
		e.currentCol = col
		e.modified = true
		e.screen.FullRedraw(e.buffer, e.topLine, e.currentLine, e.currentCol, e.insertMode)

	case CommandHelp:
		e.commands.HandleHelp(e.input)
		e.screen.FullRedraw(e.buffer, e.topLine, e.currentLine, e.currentCol, e.insertMode)

	case CommandView:
		e.commands.HandleView(e.input)
		e.screen.FullRedraw(e.buffer, e.topLine, e.currentLine, e.currentCol, e.insertMode)
	}
}
