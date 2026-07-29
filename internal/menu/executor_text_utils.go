package menu

import (
	"bytes"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/terminalio"
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

// ANSI foreground color codes (standard and bright)
var ansiFg = map[int]int{
	0: 30, 1: 34, 2: 32, 3: 36, 4: 31, 5: 35, 6: 33, 7: 37, // Standard
	8: 90, 9: 94, 10: 92, 11: 96, 12: 91, 13: 95, 14: 93, 15: 97, // Bright
}

// ANSI background color codes (standard, non-bright)
var ansiBg = map[int]int{
	0: 40, 1: 44, 2: 42, 3: 46, 4: 41, 5: 45, 6: 43, 7: 47,
	// Note: 40-47 are standard (darker) backgrounds
	// 100-107 would be bright backgrounds (less terminal support)
}
