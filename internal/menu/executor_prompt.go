package menu

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/editor"
	"github.com/ViSiON-3/vision-3-bbs/internal/terminalio"
	"github.com/gliderlabs/ssh"
	"golang.org/x/term"
)

// writeCenteredPausePrompt writes a centered pause prompt and waits for Enter.
// Returns error on write/read failure or io.EOF on disconnect.
func writeCenteredPausePrompt(s ssh.Session, terminal *term.Terminal, pausePrompt string, outputMode ansi.OutputMode, termWidth int, termHeight int) error {
	// Check if we need to add newline before pause (handle it separately from centering)
	needsNewline := !strings.HasPrefix(pausePrompt, "\r\n") && !strings.HasPrefix(pausePrompt, "\n")

	// Strip any leading newlines from the prompt text for processing
	pauseText := pausePrompt
	if strings.HasPrefix(pauseText, "\r\n") {
		pauseText = strings.TrimPrefix(pauseText, "\r\n")
	} else if strings.HasPrefix(pauseText, "\n") {
		pauseText = strings.TrimPrefix(pauseText, "\n")
	}

	// Process pipe codes and convert to CP437 if needed
	var pauseBytesToWrite []byte
	processedPausePrompt := ansi.ReplacePipeCodes([]byte(pauseText))
	if outputMode == ansi.OutputModeCP437 {
		var cp437Buf bytes.Buffer
		for _, r := range string(processedPausePrompt) {
			if r < 128 {
				cp437Buf.WriteByte(byte(r))
			} else if cp437Byte, ok := ansi.UnicodeToCP437[r]; ok {
				cp437Buf.WriteByte(cp437Byte)
			} else {
				cp437Buf.WriteByte('?')
			}
		}
		pauseBytesToWrite = cp437Buf.Bytes()
	} else {
		pauseBytesToWrite = processedPausePrompt
	}

	// Write newline first if needed
	if needsNewline {
		wErr := terminalio.WriteProcessedBytes(terminal, []byte("\r\n"), outputMode)
		if wErr != nil {
			return wErr
		}
	}

	// Center the pause prompt if terminal width is available
	if termWidth > 0 {
		// Calculate visible text width (excluding ANSI escape sequences)
		visibleWidth := calculateVisibleWidth(string(pauseBytesToWrite))

		if visibleWidth < termWidth {
			// Calculate centering offset
			leftPadding := (termWidth - visibleWidth) / 2
			if leftPadding > 0 {
				// Move cursor to center position
				centerPosBytes := []byte(fmt.Sprintf("\r\x1b[%dC", leftPadding))
				wErr := terminalio.WriteProcessedBytes(terminal, centerPosBytes, outputMode)
				if wErr != nil {
					slog.Warn("failed positioning for centered pause", "error", wErr)
				}
			}
		}
	}

	wErr := terminalio.WriteProcessedBytes(terminal, pauseBytesToWrite, outputMode)
	if wErr != nil {
		return wErr
	}

	for {
		key, err := getSessionIH(s).ReadKey()
		if err != nil {
			return err
		}
		if key == editor.KeyEnter {
			break
		}
	}
	return nil
}

// PromptYesNo is the canonical Yes/No prompt entrypoint for menu flows.
// defaultYes controls which option is pre-selected (true = Yes, false = No).
// Keep all call sites routed here so prompt behavior can be changed in one place.
func (e *MenuExecutor) PromptYesNo(s ssh.Session, terminal *term.Terminal, promptText string, outputMode ansi.OutputMode, nodeNumber int, termWidth int, termHeight int, defaultYes bool) (bool, error) {
	return e.promptYesNoLightbar(s, terminal, promptText, outputMode, nodeNumber, termWidth, termHeight, defaultYes)
}

// promptYesNoLightbar displays a Yes/No prompt with lightbar selection.
// Returns true for Yes, false for No, and error on issues like disconnect.
// defaultYes controls the initial selection: true = Yes highlighted, false = No highlighted.
func (e *MenuExecutor) promptYesNoLightbar(s ssh.Session, terminal *term.Terminal, promptText string, outputMode ansi.OutputMode, nodeNumber int, termWidth int, termHeight int, defaultYes bool) (bool, error) {
	// Strip trailing ' @' — ViSiON/2 convention for Yes/No prompt terminator.
	// The '@' signals WriteStr to render an interactive Yes/No lightbar.
	promptText = strings.TrimSuffix(promptText, " @")
	promptText = strings.TrimSuffix(promptText, "@")

	// Use termHeight from user preferences instead of reading from PTY
	if termHeight > 0 {
		// --- Inline Lightbar Logic (prints at current cursor position) ---
		slog.Debug("terminal height known from user preferences, using inline lightbar prompt", "height", termHeight)

		// NOTE: We intentionally do NOT hide the cursor (\x1b[?25l) here.
		// On iOS, MuffinTerm ties the software keyboard to cursor visibility —
		// hiding the cursor can dismiss the keyboard and block all input.

		yesLabel := strings.TrimSpace(e.LoadedStrings.YesPromptText)
		if yesLabel == "" {
			yesLabel = "Yes"
		}
		noLabel := strings.TrimSpace(e.LoadedStrings.NoPromptText)
		if noLabel == "" {
			noLabel = "No"
		}

		yesOptionText := " " + yesLabel + " "
		noOptionText := " " + noLabel + " "
		yesNoSpacing := 2  // Spaces between prompt and first option
		optionSpacing := 2 // Spaces between Yes and No
		highlightColor := e.Theme.YesNoHighlightColor
		regularColor := e.Theme.YesNoRegularColor

		// Write the prompt text inline
		promptDisplayBytes := ansi.ReplacePipeCodes([]byte(promptText))
		slog.Debug("writing prompt text bytes", "node", nodeNumber, "bytes", promptDisplayBytes)
		err := terminalio.WriteStringCP437(terminal, promptDisplayBytes, outputMode)
		if err != nil {
			slog.Error("failed writing Yes/No prompt text (lightbar mode)", "node", nodeNumber, "error", err)
			return false, fmt.Errorf("failed writing prompt text: %w", err)
		}

		// Add spacing before options
		spacingBytes := []byte(strings.Repeat(" ", yesNoSpacing))
		wErr := terminalio.WriteProcessedBytes(terminal, spacingBytes, outputMode)
		if wErr != nil {
			slog.Warn("failed writing spacing", "error", wErr)
		}

		// Total visible width of the options area (used for cursor-backward repositioning).
		// This avoids cursor save/restore which is unreliable across terminals.
		optionsWidth := len(noOptionText) + optionSpacing + len(yesOptionText)

		// Track current selection: 0 = No, 1 = Yes
		selectedIndex := 0
		if defaultYes {
			selectedIndex = 1
		}

		firstDraw := true

		// Function to draw the inline options (only the options, not the prompt).
		// Uses CUB (cursor backward) to reposition instead of save/restore.
		drawInlineOptions := func(currentSelection int) {
			if !firstDraw {
				// Move cursor back to the start of the options area
				wErr := terminalio.WriteProcessedBytes(terminal, []byte(ansi.CursorBackward(optionsWidth)), outputMode)
				if wErr != nil {
					slog.Warn("failed moving cursor backward", "error", wErr)
				}
			}
			firstDraw = false

			// Clear from cursor to end of line to remove old options
			wErr := terminalio.WriteProcessedBytes(terminal, []byte("\x1b[K"), outputMode)
			if wErr != nil {
				slog.Warn("failed clearing old options", "error", wErr)
			}

			// Draw No option
			noColorCode := regularColor
			if currentSelection == 0 {
				noColorCode = highlightColor
			}
			noColorBytes := []byte(colorCodeToAnsi(noColorCode))
			wErr = terminalio.WriteProcessedBytes(terminal, noColorBytes, outputMode)
			if wErr != nil {
				slog.Warn("failed setting No color", "error", wErr)
			}
			wErr = terminalio.WriteProcessedBytes(terminal, []byte(noOptionText), outputMode)
			if wErr != nil {
				slog.Warn("failed writing No option", "error", wErr)
			}
			wErr = terminalio.WriteProcessedBytes(terminal, []byte("\x1b[0m"), outputMode)
			if wErr != nil {
				slog.Warn("failed resetting attributes", "error", wErr)
			}

			// Add spacing between options
			wErr = terminalio.WriteProcessedBytes(terminal, []byte(strings.Repeat(" ", optionSpacing)), outputMode)
			if wErr != nil {
				slog.Warn("failed writing option spacing", "error", wErr)
			}

			// Draw Yes option
			yesColorCode := regularColor
			if currentSelection == 1 {
				yesColorCode = highlightColor
			}
			yesColorBytes := []byte(colorCodeToAnsi(yesColorCode))
			wErr = terminalio.WriteProcessedBytes(terminal, yesColorBytes, outputMode)
			if wErr != nil {
				slog.Warn("failed setting Yes color", "error", wErr)
			}
			wErr = terminalio.WriteProcessedBytes(terminal, []byte(yesOptionText), outputMode)
			if wErr != nil {
				slog.Warn("failed writing Yes option", "error", wErr)
			}
			wErr = terminalio.WriteProcessedBytes(terminal, []byte("\x1b[0m"), outputMode)
			if wErr != nil {
				slog.Warn("failed resetting attributes", "error", wErr)
			}
		}

		// Draw initial options
		drawInlineOptions(selectedIndex)

		// Use session-scoped InputHandler so we share the single goroutine
		// reading from the SSH session (prevents "double key press" race).
		yesNoIH := getSessionIH(s)
		for {
			key, err := yesNoIH.ReadKey()
			if err != nil {
				if errors.Is(err, io.EOF) {
					return false, io.EOF
				}
				return false, fmt.Errorf("failed reading yes/no input: %w", err)
			}

			newSelectedIndex := selectedIndex
			selectionMade := false
			result := false

			switch {
			case key == int('Y') || key == int('y'):
				selectionMade = true
				result = true
			case key == int('N') || key == int('n'):
				selectionMade = true
				result = false
			case key == int(' ') || key == int('\r') || key == int('\n'):
				selectionMade = true
				result = (selectedIndex == 1)
			case key == editor.KeyArrowLeft || key == editor.KeyArrowRight:
				newSelectedIndex = 1 - selectedIndex
			case key == editor.KeyEsc:
				// Bare ESC (InputHandler consumed any ANSI sequence) — ignore
			default:
				// Ignore other keys
			}

			if selectionMade {
				// Move back to option start, clear, print the chosen label, then newline
				selectedLabel := noLabel
				if result {
					selectedLabel = yesLabel
				}
				wErr := terminalio.WriteProcessedBytes(terminal, []byte(ansi.CursorBackward(optionsWidth)+"\x1b[K"+selectedLabel+"\r\n"), outputMode)
				if wErr != nil {
					slog.Warn("failed writing selection result", "error", wErr)
				}
				return result, nil
			}

			if newSelectedIndex != selectedIndex {
				selectedIndex = newSelectedIndex
				drawInlineOptions(selectedIndex)
			}
		}
		// Lightbar logic ends here

	} else {
		// --- Text Input Fallback (if terminal height is unknown) ---
		slog.Debug("terminal height unknown, using text fallback for Yes/No prompt")

		// Construct the simple text prompt
		yesNoHint := "[y/N]"
		if defaultYes {
			yesNoHint = "[Y/n]"
		}
		fullPrompt := promptText + " " + yesNoHint + "? "

		// Write the prompt after one blank row: newline + blank line, then prompt.
		wErr := terminalio.WriteProcessedBytes(terminal, []byte("\r\n\r\n"), outputMode)
		if wErr != nil {
			slog.Warn("failed writing fallback pre-prompt spacing", "error", wErr)
		}

		processedPromptBytes := ansi.ReplacePipeCodes([]byte(fullPrompt))
		err := terminalio.WriteStringCP437(terminal, processedPromptBytes, outputMode)
		if err != nil {
			slog.Error("failed writing Yes/No prompt text (fallback mode)", "node", nodeNumber, "error", err) // Use nodeNumber
			return false, fmt.Errorf("failed writing fallback prompt text: %w", err)
		}

		// Read user input
		input, err := readLineFromSessionIH(s, terminal)
		if err != nil {
			// Clean up line on error using WriteProcessedBytes
			wErr := terminalio.WriteProcessedBytes(terminal, []byte("\r\n"), outputMode) // Assuming CRLF is enough cleanup here
			if wErr != nil {
				slog.Warn("failed writing CRLF on read error", "error", wErr)
			}

			if errors.Is(err, io.EOF) {
				return false, io.EOF // Signal disconnect
			}
			return false, fmt.Errorf("failed reading yes/no fallback input: %w", err)
		}

		// Process input
		trimmedInput := strings.ToUpper(strings.TrimSpace(input))
		if len(trimmedInput) == 0 {
			return defaultYes, nil // empty = accept default
		}
		return trimmedInput[0] == 'Y', nil
	}
}

// styledInput reads input with character-by-character display styling.
// Mimics Pascal NoCRInput with a shaded cursor cell, solid blue typed area,
// and a bright blue background fill for remaining space.
func styledInput(terminal *term.Terminal, session ssh.Session, outputMode ansi.OutputMode, maxLen int, defaultValue string) (string, error) {
	typedStyle := string(ansi.ReplacePipeCodes([]byte("|B4|15")))
	cursorStyle := string(ansi.ReplacePipeCodes([]byte("|B4|15")))
	remainingStyle := string(ansi.ReplacePipeCodes([]byte("|B12|15")))
	resetColor := "\x1b[0m"

	shadeChar := "░"

	input := make([]byte, 0, maxLen)
	if defaultValue != "" {
		input = append(input, []byte(defaultValue)...)
		if len(input) > maxLen {
			input = input[:maxLen]
		}
	}
	cursorStyleSet := false
	savedCursor := false

	// Function to render the current state of the input box
	renderBox := func(moveBack bool) {
		var display strings.Builder
		if savedCursor {
			display.WriteString("\x1b[u")
		}
		display.WriteString(typedStyle)
		if len(input) > 0 {
			display.Write(input)
		}
		cursorPos := len(input)
		remainingLen := 0
		if len(input) < maxLen {
			display.WriteString(cursorStyle)
			display.WriteString(shadeChar)
			remainingLen = maxLen - len(input) - 1
		}
		if remainingLen > 0 {
			display.WriteString(remainingStyle)
			display.WriteString(strings.Repeat(" ", remainingLen))
		}
		display.WriteString(resetColor)

		moveToCursor := ""
		if cursorPos < maxLen {
			moveToCursor = fmt.Sprintf("\x1b[%dD", maxLen-cursorPos)
		}
		terminalio.WriteStringCP437(terminal, []byte(display.String()+moveToCursor), outputMode)
	}

	// Display initial empty box with cursor and default padding
	if maxLen > 0 {
		terminalio.WriteProcessedBytes(terminal, []byte("\x1b[s"), outputMode)
		savedCursor = true
		terminalio.WriteProcessedBytes(terminal, []byte("\x1b[3 q"), outputMode)
		cursorStyleSet = true
		defer func() {
			if cursorStyleSet {
				terminalio.WriteProcessedBytes(terminal, []byte("\x1b[0 q"), outputMode)
			}
		}()
		renderBox(false)
	}

	// Read character by character via the session-scoped InputHandler so we share
	// the single goroutine reading from the SSH session, preventing the race that
	// caused the "double key press" bug when the lightbar's goroutine was also active.
	ih := getSessionIH(session)
	readBuf := make([]byte, 1)

	for {
		n, err := ih.Read(readBuf)
		if err != nil {
			if err == io.EOF {
				return "", err
			}
			return "", err
		}
		if n == 0 {
			continue
		}

		ch := readBuf[0]

		switch ch {
		case 13, 10: // Enter or LF
			// User pressed Enter
			result := string(input)
			terminalio.WriteProcessedBytes(terminal, []byte("\r\n"), outputMode)
			return strings.TrimSpace(result), nil

		case 8, 127: // Backspace or Delete
			if len(input) > 0 {
				input = input[:len(input)-1]
				renderBox(true)
			}

		case 27: // ESC - abort input
			terminalio.WriteProcessedBytes(terminal, []byte("\r\n"), outputMode)
			return "", errInputAborted

		case 3: // Ctrl+C - abort
			terminalio.WriteProcessedBytes(terminal, []byte("\r\n"), outputMode)
			return "", io.EOF

		default:
			// Printable ASCII character
			if ch >= 32 && ch < 127 && len(input) < maxLen {
				input = append(input, ch)
				renderBox(true)
			}
		}
	}
}
