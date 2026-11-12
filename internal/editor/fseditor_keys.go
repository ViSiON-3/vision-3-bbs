package editor

// Key and command dispatch: turning a decoded key or an editor command into the
// edit and cursor operations that carry it out.

// handleKey processes a single key press
func (e *FSEditor) handleKey(key int) {
	// An autocorrect fix can only be undone by the very next key.
	undo := e.acUndo
	e.acUndo = nil
	e.clearNotice()

	// Handle key based on type
	switch key {
	case KeyEnter:
		fix := e.applyAutocorrect(key)
		e.handleReturn()
		e.acUndo = fix
	case KeyBackspace:
		if !e.undoAutocorrect(undo) {
			e.handleBackspace()
		}
	case KeyCtrlO: // Autocorrect on/off
		e.toggleAutocorrect()
	case KeyTab:
		fix := e.applyAutocorrect(key)
		e.handleTab()
		e.acUndo = fix

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
		// SyncTERM sends CTRL-Q as 0x11 0x10 (see ctrlQScancode). 0x10 is
		// CTRL-P, which the quote picker reads as End, so the lightbar jumped
		// to the last source line the moment the picker opened (#318). Nobody
		// can press End that fast after CTRL-Q, so a 0x10 arriving with the
		// keypress is dropped as part of it.
		e.input.DiscardPendingByte(ctrlQScancode, scancodeTrailerWindow)
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
	case KeyCtrlY, KeyCtrlK: // Delete line (Ctrl-K as in Mystic)
		if key == KeyCtrlK {
			// SyncTERM sends CTRL-K as 0x0B 0x25 (see ctrlKScancode), and the
			// "%" was typed onto the line that moved up into the deleted
			// one's place (#1000).
			e.input.DiscardPendingByte(ctrlKScancode, scancodeTrailerWindow)
		}
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
			fix := e.applyAutocorrect(key)
			e.insertCharacter(rune(key))
			e.acUndo = fix
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
		// Quote mode repaints only the editing area, so the header and footer are
		// still intact — restore the footer row (it may carry a notice) and let
		// the main loop repaint the text from a cleared cache. A FullRedraw here
		// would clear the screen and flash the header for no reason.
		e.screen.ClearCache()
		e.restorePromptRow()

	case CommandHelp:
		e.commands.HandleHelp(e.input)
		e.screen.FullRedraw(e.buffer, e.topLine, e.currentLine, e.currentCol, e.insertMode)

	case CommandView:
		e.commands.HandleView(e.input)
		e.screen.FullRedraw(e.buffer, e.topLine, e.currentLine, e.currentCol, e.insertMode)
	}
}

// restorePromptRow puts the footer back on the prompt row, over any notice
// left there. Without a footer template the prompt row is a bare terminal
// row outside the editing area, so a notice left there is never repainted
// over; it is cleared instead.
func (e *FSEditor) restorePromptRow() {
	if e.screen.HasFooter() {
		e.screen.DisplayFooter()
	} else {
		e.screen.GoXY(1, e.screen.PromptRow())
		e.screen.ClearEOL()
	}
}
