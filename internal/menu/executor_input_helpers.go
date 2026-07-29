package menu

import (
	"bytes"
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
