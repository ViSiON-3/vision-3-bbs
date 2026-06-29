package menu

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/editor"
	"github.com/ViSiON-3/vision-3-bbs/internal/terminalio"
	"github.com/gliderlabs/ssh"
	"golang.org/x/term"
)

// colorCodeToAnsi converts a DOS-style color code (0-255) to ANSI escape sequence.
// Assumes Color = Background*16 + Foreground
// calculateVisibleWidth calculates the visible width of text, excluding ANSI escape sequences.
// This is used for centering text that contains color codes.
func calculateVisibleWidth(text string) int {
	width := 0
	inEscape := false

	for i := 0; i < len(text); i++ {
		ch := text[i]

		if ch == '\x1b' {
			// Start of ANSI escape sequence
			inEscape = true
			continue
		}

		if inEscape {
			// Skip characters until we hit a letter (end of ANSI sequence)
			if (ch >= 'A' && ch <= 'Z') || (ch >= 'a' && ch <= 'z') {
				inEscape = false
			}
			continue
		}

		// Count visible characters (excluding control characters)
		if ch >= 32 {
			width++
		}
	}

	return width
}

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

func colorCodeToAnsi(code int) string {
	fgCode := code % 16
	bgCode := code / 16

	fgAnsi, okFg := ansiFg[fgCode]
	if !okFg {
		fgAnsi = 97 // Default to bright white if invalid fg code
	}

	// Use standard background colors (40-47). Bright backgrounds (100-107) have less support.
	bgAnsi, okBg := ansiBg[bgCode%8]
	if !okBg {
		bgAnsi = 40 // Default to black background if invalid bg code
	}

	// Reset first, then apply colors (ensures clean state)
	return fmt.Sprintf("\x1b[0m\x1b[%d;%dm", fgAnsi, bgAnsi)
}

// loadLightbarOptions loads and parses lightbar options from configuration files
func loadLightbarOptions(menuName string, e *MenuExecutor) ([]LightbarOption, error) {
	// Determine paths using MenuSetPath
	cfgFilename := menuName + ".CFG"
	barFilename := menuName + ".BAR"
	cfgPath := filepath.Join(e.MenuSetPath, "cfg", cfgFilename)
	barPath := filepath.Join(e.MenuSetPath, "bar", barFilename)

	slog.Debug("loading CFG", "path", cfgPath)
	slog.Debug("loading BAR", "path", barPath)

	// Load commands from CFG file using the proper JSON loader
	commandsByHotkey := make(map[string]string)
	configPath := filepath.Join(e.MenuSetPath, "cfg")
	commands, err := LoadCommands(menuName, configPath)
	if err != nil {
		slog.Warn("failed to load CFG file", "path", cfgPath, "error", err)
	} else {
		// Build hotkey -> command mapping for validation
		for _, cmd := range commands {
			hotkey := strings.ToUpper(strings.TrimSpace(cmd.Keys))
			commandsByHotkey[hotkey] = cmd.Command
		}
	}

	// Parse BAR file
	barFile, err := os.Open(barPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open BAR file %s: %w", barPath, err)
	}
	defer barFile.Close()

	var options []LightbarOption
	scanner := bufio.NewScanner(barFile)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, ";") {
			continue // Skip empty lines and comments
		}

		// Parse record in format: X,Y,HotKey,DisplayText // OLD Format
		// Parse record in format: X,Y,HiLitedColor,RegularColor,HotKey,ReturnValue,HiLitedString // NEW Format
		parts := strings.SplitN(line, ",", 7) // Split into 7 parts
		if len(parts) != 7 {                  // Check for 7 parts
			slog.Warn("malformed BAR line (expected 7 fields)", "line", line)
			continue
		}

		x, xerr := strconv.Atoi(strings.TrimSpace(parts[0]))
		y, yerr := strconv.Atoi(strings.TrimSpace(parts[1]))
		if xerr != nil || yerr != nil {
			slog.Warn("invalid coordinates in BAR line", "line", line)
			continue
		}

		// Parse color codes
		highlightColor, hcErr := strconv.Atoi(strings.TrimSpace(parts[2]))
		regularColor, rcErr := strconv.Atoi(strings.TrimSpace(parts[3]))
		if hcErr != nil || rcErr != nil {
			slog.Warn("invalid color codes in BAR line", "line", line)
			// Default colors? Or skip?
			highlightColor = 7 // Default: White on Black (inverse)
			regularColor = 15  // Default: Bright White on Black
		}

		hotkey := strings.ToUpper(strings.TrimSpace(parts[4])) // HotKey is the 5th field (index 4)
		returnValue := strings.TrimSpace(parts[5])             // ReturnValue is the 6th field (index 5)
		displayText := strings.TrimSpace(parts[6])             // DisplayText is the 7th field (index 6)

		// Verify the hotkey maps to a command
		if _, exists := commandsByHotkey[hotkey]; !exists {
			slog.Warn("hotkey in BAR file has no matching command in CFG", "hotkey", hotkey)
		}

		options = append(options, LightbarOption{
			X:              x,
			Y:              y,
			Text:           displayText,
			HotKey:         hotkey,
			ReturnValue:    returnValue,
			HighlightColor: highlightColor,
			RegularColor:   regularColor,
		})
	}

	return options, nil
}

// loadBarFile loads and parses a standalone BAR file (no matching CFG required).
// Returns nil, nil if the file does not exist.
func loadBarFile(barName string, e *MenuExecutor) ([]LightbarOption, error) {
	barPath := filepath.Join(e.MenuSetPath, "bar", barName+".BAR")

	barFile, err := os.Open(barPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to open BAR file %s: %w", barPath, err)
	}
	defer barFile.Close()

	var options []LightbarOption
	scanner := bufio.NewScanner(barFile)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, ";") {
			continue
		}

		parts := strings.SplitN(line, ",", 7)
		if len(parts) != 7 {
			slog.Warn("malformed BAR line (expected 7 fields)", "bar", barName, "line", line)
			continue
		}

		x, xerr := strconv.Atoi(strings.TrimSpace(parts[0]))
		y, yerr := strconv.Atoi(strings.TrimSpace(parts[1]))
		if xerr != nil || yerr != nil {
			slog.Warn("invalid coordinates in BAR line", "bar", barName, "line", line)
			continue
		}

		highlightColor, hcErr := strconv.Atoi(strings.TrimSpace(parts[2]))
		regularColor, rcErr := strconv.Atoi(strings.TrimSpace(parts[3]))
		if hcErr != nil || rcErr != nil {
			slog.Warn("invalid color codes in BAR line", "bar", barName, "line", line)
			highlightColor = 7
			regularColor = 15
		}

		hotkey := strings.ToUpper(strings.TrimSpace(parts[4]))
		returnValue := strings.TrimSpace(parts[5])
		displayText := strings.TrimSpace(parts[6])

		options = append(options, LightbarOption{
			X:              x,
			Y:              y,
			Text:           displayText,
			HotKey:         hotkey,
			ReturnValue:    returnValue,
			HighlightColor: highlightColor,
			RegularColor:   regularColor,
		})
	}

	return options, nil
}

// drawLightbarMenu draws the lightbar menu with the specified option selected
func drawLightbarOption(terminal *term.Terminal, opt LightbarOption, selected bool, outputMode ansi.OutputMode) error {
	posCmd := fmt.Sprintf("\x1b[%d;%dH", opt.Y, opt.X)
	err := terminalio.WriteProcessedBytes(terminal, []byte(posCmd), outputMode)
	if err != nil {
		return fmt.Errorf("failed positioning cursor for lightbar option: %w", err)
	}

	colorCode := opt.RegularColor
	if selected {
		colorCode = opt.HighlightColor
	}
	ansiColorSequence := colorCodeToAnsi(colorCode)
	err = terminalio.WriteProcessedBytes(terminal, []byte(ansiColorSequence), outputMode)
	if err != nil {
		return fmt.Errorf("failed setting color for lightbar option: %w", err)
	}

	err = terminalio.WriteProcessedBytes(terminal, []byte(opt.Text), outputMode)
	if err != nil {
		return fmt.Errorf("failed writing lightbar option text: %w", err)
	}

	err = terminalio.WriteProcessedBytes(terminal, []byte(attrReset), outputMode)
	if err != nil {
		return fmt.Errorf("failed resetting attributes after lightbar option: %w", err)
	}

	return nil
}

func drawLightbarMenu(terminal *term.Terminal, backgroundBytes []byte, options []LightbarOption, selectedIndex int, outputMode ansi.OutputMode, drawBackground bool) error {
	if drawBackground {
		// For CP437 mode, write raw bytes directly to avoid UTF-8 false positives
		var err error
		if outputMode == ansi.OutputModeCP437 {
			_, err = terminal.Write(backgroundBytes)
		} else {
			err = terminalio.WriteProcessedBytes(terminal, backgroundBytes, outputMode)
		}
		if err != nil {
			return fmt.Errorf("failed writing lightbar background: %w", err)
		}
	}

	for i, opt := range options {
		if err := drawLightbarOption(terminal, opt, i == selectedIndex, outputMode); err != nil {
			return err
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

// wrapAnsiString wraps a string containing ANSI codes to a given width.
// NOTE: This is a simplified version and does NOT perfectly handle ANSI state across wrapped lines.
// It primarily prevents lines from exceeding the terminal width visually.
// containsAnsiArt detects if text contains ANSI art by checking for cursor positioning
// or other non-color ANSI escape sequences. ANSI art should not be word-wrapped.
func containsAnsiArt(text string) bool {
	// Check for cursor positioning commands: ESC[<row>;<col>H or ESC[<row>;<col>f
	// Also check for save/restore cursor, cursor up/down/forward/back
	// These indicate the text is using absolute positioning (ANSI art)
	ansiArtPatterns := []string{
		"\x1b[", // Start of ANSI sequence
	}

	hasAnsiSequence := false
	for _, pattern := range ansiArtPatterns {
		if strings.Contains(text, pattern) {
			hasAnsiSequence = true
			break
		}
	}

	if !hasAnsiSequence {
		return false
	}

	// Look for specific ANSI art indicators:
	// - Cursor positioning: ESC[n;mH or ESC[n;mf
	// - Cursor movement: ESC[nA, ESC[nB, ESC[nC, ESC[nD
	// - Save/restore cursor: ESC[s, ESC[u
	ansiArtIndicators := regexp.MustCompile(`\x1b\[(\d+;\d+[HhFf]|\d*[ABCDsu])`)
	return ansiArtIndicators.MatchString(text)
}

func wrapAnsiString(text string, width int) []string {
	if width <= 0 {
		return strings.Split(text, "\n") // No wrapping if width is invalid
	}

	// Check if this is ANSI art (contains cursor positioning or movement commands)
	// ANSI art should NOT be word-wrapped as it uses absolute positioning
	if containsAnsiArt(text) {
		// Just split by newlines, don't word-wrap
		return strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	}

	var wrappedLines []string
	// Split input into lines first based on existing newlines
	inputLines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")

	reAnsi := regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`) // Basic regex for ANSI codes

	for _, line := range inputLines {
		plainLine := reAnsi.ReplaceAllString(line, "")
		if strings.TrimSpace(plainLine) == "" {
			wrappedLines = append(wrappedLines, "")
			continue
		}
		if isQuoteLine(plainLine) || isTearLine(plainLine) || isOriginLine(plainLine) {
			wrappedLines = append(wrappedLines, line)
			continue
		}

		currentLine := ""
		currentWidth := 0
		words := strings.Fields(line) // Split line into words

		for _, word := range words {
			// Calculate visible width of the word (stripping ANSI)
			plainWord := reAnsi.ReplaceAllString(word, "")
			wordWidth := len(plainWord)

			if currentWidth == 0 {
				// First word on the line
				if wordWidth > width {
					// Word is longer than the line width, just append it (will overflow)
					wrappedLines = append(wrappedLines, word)
					currentLine = ""
					currentWidth = 0
				} else {
					currentLine = word
					currentWidth = wordWidth
				}
			} else {
				// Subsequent words
				if currentWidth+1+wordWidth <= width {
					// Word fits on the current line
					currentLine += " " + word
					currentWidth += 1 + wordWidth
				} else {
					// Word doesn't fit, wrap to next line
					wrappedLines = append(wrappedLines, currentLine)
					if wordWidth > width {
						// Word itself is too long, put it on its own line
						wrappedLines = append(wrappedLines, word)
						currentLine = ""
						currentWidth = 0
					} else {
						// Start new line with the current word
						currentLine = word
						currentWidth = wordWidth
					}
				}
			}
		}
		// Add the last line being built
		if currentWidth > 0 {
			wrappedLines = append(wrappedLines, currentLine)
		}
	}

	return wrappedLines
}

// writeProcessedStringWithManualEncoding takes bytes that have already had pipe codes
// replaced with standard ANSI escapes and writes them to the terminal, handling
// character encoding manually based on the desired outputMode.
// It now correctly handles UTF-8 input strings containing ANSI codes.
func writeProcessedStringWithManualEncoding(terminal *term.Terminal, processedBytes []byte, outputMode ansi.OutputMode) error {
	var finalBuf bytes.Buffer
	i := 0
	processedString := string(processedBytes) // Work with the UTF-8 string

	for i < len(processedString) {
		// Check for ANSI escape sequence start
		if processedString[i] == '\x1b' { // <-- Corrected: Use character literal
			start := i
			// Find the end of the ANSI sequence (basic CSI parsing)
			if i+1 < len(processedString) && processedString[i+1] == '[' {
				i += 2 // Skip ESC [
				for i < len(processedString) {
					c := processedString[i]
					if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') { // Found terminator
						i++
						break
					}
					i++
					// Basic protection
					if i-start > 30 {
						slog.Warn("potential runaway ANSI sequence encountered")
						break
					}
				}
			} else {
				// Handle other potential escape sequences if necessary (e.g., ESC ( B )
				// For now, assume simple non-CSI escapes are short or handle known ones
				// Example: ESC ( B (designate US-ASCII) is 3 bytes
				if i+2 < len(processedString) && processedString[i+1] == '(' && processedString[i+2] == 'B' {
					i += 3
				} else {
					i++ // Just skip the ESC if unknown sequence
				}
			}
			// Write the entire ANSI sequence as is
			finalBuf.WriteString(processedString[start:i])
			continue // Continue outer loop
		}

		// Decode the next rune from the UTF-8 string
		r, size := utf8.DecodeRuneInString(processedString[i:])
		if r == utf8.RuneError && size <= 1 {
			// Invalid UTF-8 sequence, write a placeholder or skip
			finalBuf.WriteByte('?')
			i++ // Move past the invalid byte
			continue
		}

		// Now handle the valid rune 'r' based on outputMode
		if outputMode == ansi.OutputModeCP437 {
			if r < 128 {
				// ASCII character, write directly
				finalBuf.WriteByte(byte(r))
			} else if cp437Byte, ok := ansi.UnicodeToCP437[r]; ok {
				// Found a corresponding CP437 byte
				finalBuf.WriteByte(cp437Byte)
			} else {
				// Unicode character doesn't exist in CP437, write fallback
				finalBuf.WriteByte('?')
			}
		} else { // OutputModeUTF8 or OutputModeAuto (assuming UTF-8 if not CP437)
			// Write the original rune (which is already UTF-8)
			finalBuf.WriteRune(r)
		}

		i += size // Move past the processed rune
	}

	// Write the fully processed buffer to the terminal
	err := terminalio.WriteProcessedBytes(terminal, finalBuf.Bytes(), outputMode)
	return err
}

// styledInput reads input with character-by-character display styling.
// Mimics Pascal NoCRInput with a shaded cursor cell, solid blue typed area,
// and a bright blue background fill for remaining space.
func styledInput(terminal *term.Terminal, session ssh.Session, outputMode ansi.OutputMode, maxLen int, defaultValue string) (string, error) {
	typedStyle := string(ansi.ReplacePipeCodes([]byte("|B4|15")))
	cursorStyle := string(ansi.ReplacePipeCodes([]byte("|B4|15")))
	remainingStyle := string(ansi.ReplacePipeCodes([]byte("|B12|15")))
	resetColor := "\x1b[0m"

	shadeChar := "\u2591"

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
