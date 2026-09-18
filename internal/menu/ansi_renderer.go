package menu

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
)

// ANSICell represents a single character cell with its attributes
type ANSICell struct {
	Char  byte   // Keep as byte to preserve CP437 encoding
	Style string // ANSI escape sequence for this cell's style
}

// ANSIRenderer renders ANSI art into a 2D buffer
type ANSIRenderer struct {
	Width        int
	Height       int
	Buffer       [][]ANSICell
	CursorX      int
	CursorY      int
	CurrentStyle string
	// Accumulated graphic state. ANSI art overwhelmingly sets one attribute at
	// a time - "\x1b[46m" to change only the background, say - and expects the
	// rest to persist. Keeping just the last sequence dropped everything it did
	// not mention, so a foreground set earlier reverted to the terminal default
	// the moment a background was chosen.
	sgr ansi.SGRState
	// Auto-wrap (DECAWM), on by default as on a real terminal and toggled by
	// ESC[?7h and ESC[?7l. Art of this era is written for an 80-column screen
	// and leans on the wrap to start each new row.
	autoWrap bool
	// Deferred wrap. Filling the last column does not move the cursor; it arms
	// the wrap, and the *next* printable character starts the new row. Wrapping
	// eagerly instead would insert a blank row after every full-width line.
	pendingWrap bool
	// Cursor position stashed by ESC[s and brought back by ESC[u. ANSI.SYS
	// semantics: position only, not the graphic attributes (that is DECSC,
	// ESC 7, which the art in these messages does not use). savedValid keeps a
	// restore before any save from jumping the cursor to the origin.
	savedX     int
	savedY     int
	savedValid bool
}

// NewANSIRenderer creates a new renderer with given dimensions
func NewANSIRenderer(width, height int) *ANSIRenderer {
	// Validate dimensions and use sensible defaults
	if width <= 0 {
		width = 80
	}
	if height <= 0 {
		height = 25
	}
	buffer := make([][]ANSICell, height)
	for i := range buffer {
		buffer[i] = make([]ANSICell, width)
		// Initialize with spaces
		for j := range buffer[i] {
			buffer[i][j] = ANSICell{Char: ' ', Style: "\x1b[0m"}
		}
	}

	return &ANSIRenderer{
		Width:        width,
		Height:       height,
		Buffer:       buffer,
		CursorX:      0,
		CursorY:      0,
		CurrentStyle: "\x1b[0m",
		sgr:          ansi.NewSGRState(),
		autoWrap:     true,
	}
}

// Render processes ANSI text and renders it into the buffer
func (r *ANSIRenderer) Render(text string) {
	i := 0
	lastChar := byte(0)
	for i < len(text) {
		// Check for ANSI escape sequence
		if i < len(text) && text[i] == '\x1b' {
			// Parse escape sequence
			seq, length := r.parseEscapeSequence(text[i:])
			if length > 0 {
				// Check if this is a cursor-affecting command
				resetLastChar := r.handleEscapeSequence(seq)
				i += length
				// Only reset lastChar for cursor-affecting commands
				// so that \r<color-code>\n is still treated as \r\n
				if resetLastChar {
					lastChar = 0
				}
				continue
			}
		}

		// Handle regular characters
		// CRITICAL: Keep as byte, not rune, to preserve CP437 encoding
		ch := text[i]
		switch ch {
		case '\r':
			r.CursorX = 0
			r.pendingWrap = false
		case '\n':
			// Handle both \r\n and standalone \n
			// If \n is NOT preceded by \r, reset column to 0
			if lastChar != '\r' {
				r.CursorX = 0
			}
			r.CursorY++
			if r.CursorY >= r.Height {
				r.CursorY = r.Height - 1
			}
			r.pendingWrap = false
		case '\t':
			r.pendingWrap = false
			r.CursorX = ((r.CursorX / 8) + 1) * 8
			if r.CursorX >= r.Width {
				r.CursorX = r.Width - 1
			}
		default:
			// A wrap armed by the previous character takes effect now.
			if r.pendingWrap {
				r.pendingWrap = false
				r.CursorX = 0
				r.CursorY++
				if r.CursorY >= r.Height {
					r.CursorY = r.Height - 1
				}
			}
			// Write character to buffer (only if within bounds)
			if r.CursorY >= 0 && r.CursorY < r.Height && r.CursorX >= 0 && r.CursorX < r.Width {
				r.Buffer[r.CursorY][r.CursorX] = ANSICell{
					Char:  ch,
					Style: r.CurrentStyle,
				}
			}
			if r.CursorX >= r.Width-1 {
				// Last column. With wrapping on, arm it rather than moving now;
				// with it off, stay put and let further writes overwrite here,
				// which is what a terminal does.
				r.CursorX = r.Width - 1
				r.pendingWrap = r.autoWrap
			} else {
				r.CursorX++
			}
		}
		lastChar = ch // Track last character for \n handling
		i++
	}
}

// parseEscapeSequence extracts an ANSI escape sequence and returns it with its length
func (r *ANSIRenderer) parseEscapeSequence(text string) (string, int) {
	if len(text) == 0 || text[0] != 0x1b {
		return "", 0
	}
	if len(text) == 1 {
		// Bare ESC at the end of the input. Consume it; an ESC drawn as a cell
		// is never what was meant.
		return "", 1
	}

	if text[1] == '[' {
		// CSI: ESC [ parameter bytes 0x30-0x3F, then intermediate bytes
		// 0x20-0x2F, then a final byte 0x40-0x7E. Returning ("", n) consumes n
		// bytes and dispatches nothing, which is how anything malformed or
		// cut short is discarded rather than drawn.
		i := 2
		intermediate := false
		malformed := false
		for i < len(text) {
			ch := text[i]
			switch {
			case ch >= 0x30 && ch <= 0x3f: // parameter byte
				// Parameters must precede intermediates; ESC[ 31m is not a
				// colour change, whatever its digits say.
				if intermediate {
					malformed = true
				}
				i++
			case ch >= 0x20 && ch <= 0x2f: // intermediate byte
				intermediate = true
				i++
			case ch >= 0x40 && ch <= 0x7e: // final byte
				// An intermediate byte selects a different control function
				// from the bare form, and none of the functions this renderer
				// implements take one. Consume and dispatch nothing rather
				// than acting on a sequence that was not what it looked like.
				if malformed || intermediate {
					return "", i + 1
				}
				return text[:i+1], i + 1
			default:
				// A byte that cannot appear in a CSI, such as the newline left
				// behind when this echo truncates art at 79 bytes. Stop before
				// it so it is still processed as itself.
				return "", i
			}
		}
		return "", i // ran off the end mid-sequence
	}

	switch text[1] {
	case '(', ')', '*', '+': // charset designation, e.g. ESC ( B
		if len(text) >= 3 {
			return text[:3], 3
		}
		return "", 2
	case '7', '8', '=', '>', 'c', 'D', 'E', 'H', 'M': // known two-byte forms
		return text[:2], 2
	}

	// Unknown escape: consume only the ESC, so a control character following
	// it - a line break, most importantly - is still handled normally.
	return "", 1
}

// handleEscapeSequence processes an ANSI escape sequence
// Returns true if the sequence affects cursor position (should reset lastChar)
func (r *ANSIRenderer) handleEscapeSequence(seq string) bool {
	if len(seq) < 2 {
		return false
	}

	// CSI sequences (ESC [)
	if seq[1] == '[' {
		// Extract parameters and command
		params, cmd := r.parseCSI(seq)

		switch cmd {
		case 'A': // Cursor up
			r.pendingWrap = false
			count := 1
			if len(params) > 0 && params[0] > 0 {
				count = params[0]
			}
			r.CursorY -= count
			if r.CursorY < 0 {
				r.CursorY = 0
			}
			return true

		case 'B': // Cursor down
			r.pendingWrap = false
			count := 1
			if len(params) > 0 && params[0] > 0 {
				count = params[0]
			}
			r.CursorY += count
			if r.CursorY >= r.Height {
				r.CursorY = r.Height - 1
			}
			return true

		case 'C': // Cursor forward (right)
			r.pendingWrap = false
			count := 1
			if len(params) > 0 && params[0] > 0 {
				count = params[0]
			}
			r.CursorX += count
			if r.CursorX >= r.Width {
				r.CursorX = r.Width - 1
			}
			return true

		case 'D': // Cursor back (left)
			r.pendingWrap = false
			count := 1
			if len(params) > 0 && params[0] > 0 {
				count = params[0]
			}
			r.CursorX -= count
			if r.CursorX < 0 {
				r.CursorX = 0
			}
			return true

		case 'H', 'f': // Cursor position
			r.pendingWrap = false
			row, col := 1, 1
			if len(params) > 0 && params[0] > 0 {
				row = params[0]
			}
			if len(params) > 1 && params[1] > 0 {
				col = params[1]
			}
			r.CursorY = row - 1 // Convert to 0-based
			r.CursorX = col - 1
			// Clamp to bounds (silently - common for ANSI art designed for different terminal sizes)
			if r.CursorY < 0 {
				r.CursorY = 0
			}
			if r.CursorY >= r.Height {
				r.CursorY = r.Height - 1
			}
			if r.CursorX < 0 {
				r.CursorX = 0
			}
			if r.CursorX >= r.Width {
				r.CursorX = r.Width - 1
			}
			// Silently clamp cursor positioning that exceeds buffer bounds
			// (common for ANSI art designed for different terminal sizes)
			return true

		case 'J': // Erase display
			mode := 0
			if len(params) > 0 {
				mode = params[0]
			}
			switch mode {
			case 0: // Clear from cursor to end of screen
				// Clear rest of current line
				for x := r.CursorX; x < r.Width; x++ {
					r.Buffer[r.CursorY][x] = ANSICell{Char: ' ', Style: r.CurrentStyle}
				}
				// Clear lines below
				for y := r.CursorY + 1; y < r.Height; y++ {
					for x := 0; x < r.Width; x++ {
						r.Buffer[y][x] = ANSICell{Char: ' ', Style: r.CurrentStyle}
					}
				}
			case 1: // Clear from cursor to beginning of screen
				// Clear lines above
				for y := 0; y < r.CursorY; y++ {
					for x := 0; x < r.Width; x++ {
						r.Buffer[y][x] = ANSICell{Char: ' ', Style: r.CurrentStyle}
					}
				}
				// Clear beginning of current line
				for x := 0; x <= r.CursorX; x++ {
					r.Buffer[r.CursorY][x] = ANSICell{Char: ' ', Style: r.CurrentStyle}
				}
			case 2, 3: // Clear entire screen
				for y := 0; y < r.Height; y++ {
					for x := 0; x < r.Width; x++ {
						r.Buffer[y][x] = ANSICell{Char: ' ', Style: r.CurrentStyle}
					}
				}
			}
			return true

		case 'K': // Erase line
			mode := 0
			if len(params) > 0 {
				mode = params[0]
			}
			switch mode {
			case 0: // Clear from cursor to end of line
				for x := r.CursorX; x < r.Width; x++ {
					r.Buffer[r.CursorY][x] = ANSICell{Char: ' ', Style: r.CurrentStyle}
				}
			case 1: // Clear from cursor to beginning of line
				for x := 0; x <= r.CursorX; x++ {
					r.Buffer[r.CursorY][x] = ANSICell{Char: ' ', Style: r.CurrentStyle}
				}
			case 2: // Clear entire line
				for x := 0; x < r.Width; x++ {
					r.Buffer[r.CursorY][x] = ANSICell{Char: ' ', Style: r.CurrentStyle}
				}
			}
			return true

		case 'm': // SGR - Select Graphic Rendition (colors, styles)
			// Fold into the running state; the emitted style is the whole
			// state, not just this sequence. Does not affect cursor position.
			r.sgr.Write(seq)
			r.CurrentStyle = r.sgr.Escape()
			return false

		case 'h', 'l': // Mode set / reset
			// One sequence may carry several modes - ESC[?7;25h sets both
			// auto-wrap and cursor visibility - so look through all of them
			// rather than only the first. Only DECAWM (7) matters here.
			if strings.Contains(seq, "?") {
				for _, p := range params {
					if p != 7 {
						continue
					}
					r.autoWrap = cmd == 'h'
					if !r.autoWrap {
						r.pendingWrap = false
					}
				}
			}
			return false

		case 's': // Save cursor position
			r.savedX, r.savedY = r.CursorX, r.CursorY
			r.savedValid = true
			return false // stashes the cursor, does not move it

		case 'u': // Restore cursor position
			// A restore with nothing saved is a no-op rather than a jump to
			// the origin, which is what a terminal does and what the art
			// expects.
			if r.savedValid {
				r.CursorX, r.CursorY = r.savedX, r.savedY
				r.pendingWrap = false
			}
			return true
		}
	}
	return false
}

// parseCSI extracts parameters and command from a CSI sequence
func (r *ANSIRenderer) parseCSI(seq string) ([]int, byte) {
	if len(seq) < 3 || seq[0] != '\x1b' || seq[1] != '[' {
		return nil, 0
	}

	// Find the final command byte
	cmdIdx := len(seq) - 1
	cmd := seq[cmdIdx]

	// Extract parameter string (skip private parameter bytes like ?, >, =)
	paramStr := seq[2:cmdIdx]

	// Skip leading private parameter bytes
	startIdx := 0
	for startIdx < len(paramStr) {
		ch := paramStr[startIdx]
		if ch == '?' || ch == '>' || ch == '=' || ch == '<' {
			startIdx++
		} else {
			break
		}
	}
	paramStr = paramStr[startIdx:]

	if paramStr == "" {
		return nil, cmd
	}

	// Parse parameters
	var params []int
	parts := strings.Split(paramStr, ";")
	for _, part := range parts {
		if part == "" {
			params = append(params, 0)
			continue
		}
		var num int
		_, _ = fmt.Sscanf(part, "%d", &num) // best-effort parse; num stays 0
		params = append(params, num)
	}

	return params, cmd
}

// ExtractLines converts the buffer into an array of strings with ANSI codes
func (r *ANSIRenderer) ExtractLines() []string {
	var lines []string

	for y := 0; y < r.Height; y++ {
		// Use bytes.Buffer instead of strings.Builder to preserve CP437 bytes
		var line bytes.Buffer
		lastStyle := ""

		// Find the rightmost non-space character OR styled space (preserve background colors)
		// Spaces with non-default styles (e.g. background colors) should not be trimmed
		rightmost := -1
		for x := r.Width - 1; x >= 0; x-- {
			cell := r.Buffer[y][x]
			if cell.Char != ' ' || cell.Style != "\x1b[0m" {
				rightmost = x
				break
			}
		}

		// If entire line is spaces, add empty line
		if rightmost == -1 {
			lines = append(lines, "")
			continue
		}

		// Build line with style changes
		for x := 0; x <= rightmost; x++ {
			cell := r.Buffer[y][x]

			// Add style change if needed
			if cell.Style != lastStyle {
				line.WriteString(cell.Style)
				lastStyle = cell.Style
			}

			// Add character (write raw byte to preserve CP437)
			line.WriteByte(cell.Char)
		}

		// Reset at end of line
		if lastStyle != "\x1b[0m" {
			line.WriteString("\x1b[0m")
		}

		// Convert buffer to string (preserves raw bytes)
		lines = append(lines, line.String())
	}

	// Trim trailing empty lines
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}

	return lines
}

// RenderANSIArtToLines takes ANSI art text and renders it to scrollable lines
// The text is rendered into a virtual buffer where cursor positioning is relative
// to the buffer, not the terminal screen. This allows ANSI art with absolute
// positioning to display correctly.
func RenderANSIArtToLines(text string, width, height int) []string {
	// Create renderer with generous height to capture all content
	renderer := NewANSIRenderer(width, height)

	// Render the ANSI art (keeping CP437 bytes as-is)
	renderer.Render(text)

	// Extract lines
	return renderer.ExtractLines()
}
