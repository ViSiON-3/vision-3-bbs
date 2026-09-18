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
	"github.com/mattn/go-runewidth"
	"golang.org/x/term"
)

// colorCodeToAnsi converts a DOS-style color code (0-255) to ANSI escape sequence.
// Assumes Color = Background*16 + Foreground

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

// reWrapAnsi matches the escape sequences ReplacePipeCodes emits. Package
// level because wrapping recompiled it on every call.
var reWrapAnsi = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

// visibleColumns is the on-screen width of s for mode, escapes excluded. It is
// the measure wrapping uses, so anything that re-checks a wrapped line against
// the same budget must use it too or the two will disagree about the same
// line.
func visibleColumns(s string, mode ansi.OutputMode) int {
	plain := reWrapAnsi.ReplaceAllString(s, "")
	return columnWidth(plain, utf8.ValidString(plain), mode)
}

func wrapAnsiString(text string, width int, mode ansi.OutputMode) []string {
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

	for _, line := range inputLines {
		plainLine := reWrapAnsi.ReplaceAllString(line, "")
		if strings.TrimSpace(plainLine) == "" {
			wrappedLines = append(wrappedLines, "")
			continue
		}
		if isQuoteLine(plainLine) || isTearLine(plainLine) || isOriginLine(plainLine) {
			wrappedLines = append(wrappedLines, line)
			continue
		}

		// Message bodies arrive as raw bytes that may be CP437 or UTF-8, so a
		// column is not always a byte. asUTF8 settles it once for the whole
		// line, the same way terminalio decides a span's encoding.
		asUTF8 := utf8.ValidString(plainLine)

		// A line that already fits is left exactly as the author typed it.
		// Re-flowing it would collapse the runs of spaces that column-aligned
		// signatures, tables and ASCII boxes are built out of.
		if columnWidth(plainLine, asUTF8, mode) <= width {
			wrappedLines = append(wrappedLines, line)
			continue
		}

		wrappedLines = append(wrappedLines, wrapVisualLine(line, width, asUTF8, mode)...)
	}

	return wrappedLines
}

// columnWidth is the on-screen width of escape-free text. Measure it the way
// the writer will actually render it, or wrapping decides against a width the
// terminal never sees.
//
// Message bodies are raw bytes that may be CP437 or UTF-8, and the two cannot
// be told apart byte by byte - plenty of adjacent CP437 pairs form a valid
// UTF-8 sequence - so asUTF8 is decided once per line, exactly as terminalio
// resolves the same ambiguity per span.
//
// A span that is not valid UTF-8 is CP437 and reaches the terminal untouched:
// one column per byte in either mode. A valid UTF-8 span depends on where it
// is going. UTF-8 mode passes it through, so display width applies and a CJK
// rune is two columns while a combining mark is none. CP437 mode folds each
// rune to a single CP437 byte, or to '?' where it does not map, so every rune
// is exactly one column whatever its display width.
func columnWidth(plain string, asUTF8 bool, mode ansi.OutputMode) int {
	if !asUTF8 {
		return len(plain)
	}
	if mode == ansi.OutputModeCP437 {
		return utf8.RuneCountInString(plain)
	}
	return runewidth.StringWidth(plain)
}

// wrapSeg is one run of a line: either whitespace or non-whitespace, never a
// mix. text is the run as it appeared, escapes included; codes is just those
// escapes, so a whitespace run that a line break swallows can still hand its
// colour on to the next line.
type wrapSeg struct {
	text  string
	codes string
	width int
	space bool
}

// ansiSeqEnd returns the index just past the escape sequence starting at i.
func ansiSeqEnd(s string, i int) int {
	j := i + 1
	if j < len(s) && s[j] == '[' {
		j++
		for j < len(s) {
			c := s[j]
			j++
			if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') {
				break
			}
		}
		return j
	}
	if j < len(s) {
		j++
	}
	return j
}

// splitWrapSegments breaks a line into alternating whitespace and word runs.
// Escape sequences carry no width, so they ride along with the run they sit in
// (or with the run that follows, when they open the line).
func splitWrapSegments(line string, asUTF8 bool, mode ansi.OutputMode) []wrapSeg {
	var segs []wrapSeg
	var text, codes, pending strings.Builder
	width := 0
	space := false
	open := false

	closeSeg := func() {
		if !open {
			return
		}
		segs = append(segs, wrapSeg{text: text.String(), codes: codes.String(), width: width, space: space})
		text.Reset()
		codes.Reset()
		width = 0
		open = false
	}

	for i := 0; i < len(line); {
		if line[i] == 0x1b {
			end := ansiSeqEnd(line, i)
			if open {
				text.WriteString(line[i:end])
				codes.WriteString(line[i:end])
			} else {
				pending.WriteString(line[i:end])
			}
			i = end
			continue
		}
		isSpace := line[i] == ' ' || line[i] == '\t'
		if !open || isSpace != space {
			closeSeg()
			space = isSpace
			open = true
			text.WriteString(pending.String())
			codes.WriteString(pending.String())
			pending.Reset()
		}
		n, w := 1, 1
		if asUTF8 {
			r, size := utf8.DecodeRuneInString(line[i:])
			n = size
			// One CP437 byte per rune on the way out, so one column; only a
			// UTF-8 terminal sees the rune's real display width.
			if mode != ansi.OutputModeCP437 {
				w = runewidth.RuneWidth(r)
			}
		}
		text.WriteString(line[i : i+n])
		width += w
		i += n
	}
	closeSeg()

	if pending.Len() > 0 {
		// A trailing reset, normally. Keep it on the tail of the line.
		if n := len(segs); n > 0 {
			segs[n-1].text += pending.String()
			segs[n-1].codes += pending.String()
		} else {
			segs = append(segs, wrapSeg{text: pending.String(), codes: pending.String()})
		}
	}
	return segs
}

// wrapVisualLine greedily wraps one over-long line at whitespace. Leading
// indent and interior spacing are kept; only the whitespace a break lands on
// is consumed, the way a terminal would. A word wider than the whole line is
// emitted oversized rather than cut mid-word.
func wrapVisualLine(line string, width int, asUTF8 bool, mode ansi.OutputMode) []string {
	var (
		out      []string
		cur      strings.Builder
		curWidth int
		broken   bool
	)

	flush := func() {
		out = append(out, cur.String())
		cur.Reset()
		curWidth = 0
		broken = true
	}

	for _, seg := range splitWrapSegments(line, asUTF8, mode) {
		if seg.space {
			switch {
			case curWidth == 0 && broken:
				// This run is the break itself; keep only its colour.
				cur.WriteString(seg.codes)
			case curWidth+seg.width <= width:
				cur.WriteString(seg.text)
				curWidth += seg.width
			default:
				flush()
				cur.WriteString(seg.codes)
			}
			continue
		}
		if curWidth > 0 && curWidth+seg.width > width {
			flush()
		}
		cur.WriteString(seg.text)
		curWidth += seg.width
	}

	switch {
	case curWidth > 0:
		out = append(out, cur.String())
	case cur.Len() > 0 && len(out) > 0:
		// Nothing visible left, only escapes - a trailing reset, typically.
		// Keep them on the last line rather than spending a row on them, so
		// the colour they close cannot bleed into the rest of the body.
		out[len(out)-1] += cur.String()
	case cur.Len() > 0:
		out = append(out, cur.String())
	}
	return out
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
