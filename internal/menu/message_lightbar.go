package menu

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"slices"
	"strings"
	"unicode"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/editor"
	"github.com/ViSiON-3/vision-3-bbs/internal/terminalio"
	"golang.org/x/term"
)

// MsgLightbarOption defines an option in the message reader lightbar.
type MsgLightbarOption struct {
	Label   string // Display text including padding, e.g. " Next "
	HotKey  byte   // Single-char hotkey, e.g. 'N'
	LoColor int    // Override unselected color (0 = use default loColor)
}

// drawMsgLightbarStatic draws the lightbar menu without waiting for input.
// This is used to display the menu during screen redraws.
// selectedIdx can be -1 for no highlight, or 0-9 to highlight a specific option.
func drawMsgLightbarStatic(terminal *term.Terminal, options []MsgLightbarOption,
	outputMode ansi.OutputMode, hiColor int, loColor int, suffix string, selectedIdx int,
	addBounds bool, boundsColor int) {

	// Move to beginning of line
	terminalio.WriteProcessedBytes(terminal, []byte("\r"), outputMode)

	if addBounds {
		boundSeq := colorCodeToAnsi(boundsColor)
		terminalio.WriteProcessedBytes(terminal, []byte("\x1b[0m"+boundSeq+"\xB3"), outputMode)
	}

	for i, opt := range options {
		var colorSeq string
		if i == selectedIdx {
			colorSeq = colorCodeToAnsi(hiColor)
		} else if opt.LoColor != 0 {
			colorSeq = colorCodeToAnsi(opt.LoColor)
		} else {
			colorSeq = colorCodeToAnsi(loColor)
		}
		terminalio.WriteProcessedBytes(terminal, []byte(colorSeq+opt.Label), outputMode)
	}
	if addBounds {
		boundSeq := colorCodeToAnsi(boundsColor)
		terminalio.WriteProcessedBytes(terminal, []byte("\x1b[0m"+boundSeq+"\xB3"), outputMode)
	}
	// Reset and write suffix
	terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte("\x1b[0m"+suffix)), outputMode)
}

// runMsgLightbar displays a horizontal lightbar and returns the selected hotkey.
// It handles arrow key navigation and direct hotkey presses.
// The bar is drawn on a single line starting at the current cursor position.
//
// Pascal-style: options are displayed horizontally, the highlighted one is drawn
// with hiColor, others with loColor. Arrow keys move the highlight.
// Direct key presses (matching HotKey) select immediately.
// Enter selects the currently highlighted option.
//
// initialDirection: 0=none, -1=left (move to previous), 1=right (move to next)
//
// Keys are decoded by the session InputHandler, so arrow and paging keys read
// the same here as everywhere else. A key listed in passKeys ends the lightbar
// without selecting anything: it is returned as the second value with a zero
// hotkey, and the bar is redrawn with the first option highlighted, the state
// the caller shows when the bar is idle. The message reader passes its
// scrolling keys this way so Up/Down keep scrolling after Left/Right (#412).
func runMsgLightbar(ih *editor.InputHandler, terminal *term.Terminal,
	options []MsgLightbarOption, outputMode ansi.OutputMode,
	hiColor int, loColor int, suffix string, initialDirection int,
	addBounds bool, boundsColor int, passKeys []int) (byte, int, error) {

	if len(options) == 0 {
		return 0, 0, fmt.Errorf("no lightbar options provided")
	}

	currentIdx := 0

	// Apply initial direction if provided
	if initialDirection < 0 {
		// Left arrow pressed initially - move to previous (wrap to end)
		currentIdx = len(options) - 1
	} else if initialDirection > 0 {
		// Right arrow pressed initially - move to next
		currentIdx = 1
		if currentIdx >= len(options) {
			currentIdx = 0
		}
	}

	// Build a map of hotkeys to indices for direct selection
	hotkeyMap := make(map[byte]int)
	for i, opt := range options {
		hotkeyMap[opt.HotKey] = i
	}

	// Pre-calculate column positions for each option
	// Options are laid out: [opt1][opt2][opt3]...
	cols := make([]int, len(options))
	col := 1 // Start at column 1
	for i, opt := range options {
		cols[i] = col
		col += len(opt.Label)
	}

	// Function to draw the bar
	drawBar := func(selectedIdx int) {
		drawMsgLightbarStatic(terminal, options, outputMode, hiColor, loColor, suffix, selectedIdx, addBounds, boundsColor)
	}

	drawBar(currentIdx)

	for {
		key, err := ih.ReadKey()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return 0, 0, io.EOF
			}
			return 0, 0, fmt.Errorf("failed reading lightbar input: %w", err)
		}

		if slices.Contains(passKeys, key) {
			drawBar(0)
			return 0, key, nil
		}

		switch key {
		case editor.KeyArrowLeft, '4': // '4' = numpad left (Pascal)
			drawBar(-1) // Unhighlight current by drawing with no selection
			currentIdx--
			if currentIdx < 0 {
				currentIdx = len(options) - 1
			}
			drawBar(currentIdx)

		case editor.KeyArrowRight, ' ', '6': // '6' = numpad right (Pascal)
			drawBar(-1)
			currentIdx++
			if currentIdx >= len(options) {
				currentIdx = 0
			}
			drawBar(currentIdx)

		case editor.KeyEnter, '\n':
			// Select current option
			return options[currentIdx].HotKey, 0, nil

		default:
			// Check for direct hotkey
			if key < 32 || key > 126 {
				continue
			}
			upperKey := byte(unicode.ToUpper(rune(key)))
			if _, ok := hotkeyMap[upperKey]; ok {
				return upperKey, 0, nil
			}
		}
	}
}

// readSingleKey reads a single keypress using the session's reader.
// It does NOT handle escape sequences.
func readSingleKey(reader *bufio.Reader) (rune, error) {
	r, _, err := reader.ReadRune()
	return r, err
}

// readLineInput reads a line of text input, echoing characters.
// Returns the entered string (trimmed). Empty string on just Enter.
// Returns errInputAborted if the user presses ESC.
func readLineInput(reader *bufio.Reader, terminal *term.Terminal, outputMode ansi.OutputMode, maxLen int) (string, error) {
	var buf strings.Builder

	for {
		r, _, err := reader.ReadRune()
		if err != nil {
			return "", err
		}

		switch {
		case r == '\r' || r == '\n':
			return strings.TrimSpace(buf.String()), nil
		case r == 0x1B: // ESC — abort input
			terminalio.WriteProcessedBytes(terminal, []byte("\r\n"), outputMode)
			return "", errInputAborted
		case r == 8 || r == 127: // Backspace or Delete
			if buf.Len() > 0 {
				s := buf.String()
				buf.Reset()
				buf.WriteString(s[:len(s)-1])
				terminalio.WriteProcessedBytes(terminal, []byte("\b \b"), outputMode)
			}
		case r >= 32 && r < 127:
			if maxLen <= 0 || buf.Len() < maxLen {
				buf.WriteRune(r)
				terminalio.WriteProcessedBytes(terminal, []byte(string(r)), outputMode)
			}
		}
	}
}

// promptSingleChar shows a prompt and waits for a single keypress.
// Returns the uppercase character pressed.
func promptSingleChar(reader *bufio.Reader, terminal *term.Terminal, prompt string, outputMode ansi.OutputMode) (rune, error) {
	processedPrompt := ansi.ReplacePipeCodes([]byte(prompt))
	wErr := terminalio.WriteProcessedBytes(terminal, processedPrompt, outputMode)
	if wErr != nil {
		slog.Error("failed writing prompt", "error", wErr)
	}

	r, err := readSingleKey(reader)
	if err != nil {
		return 0, err
	}

	return unicode.ToUpper(r), nil
}
