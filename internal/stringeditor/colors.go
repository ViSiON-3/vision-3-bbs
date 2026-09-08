package stringeditor

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"

	"github.com/ViSiON-3/vision-3-bbs/internal/tuiart"
)

// DOS palette index names, used by the editor's own chrome styles.
const (
	dosBlack        = 0
	dosBlue         = 1
	dosRed          = 4
	dosMagenta      = 5
	dosLightGray    = 7
	dosDarkGray     = 8
	dosLightBlue    = 9
	dosLightCyan    = 11
	dosLightMagenta = 13
	dosYellow       = 14
	dosWhite        = 15
)

// dosColors is the shared VGA palette, indexed by DOS color number. Pipe codes
// |00-|15 and the $x dollar codes both index straight into it, so a string
// previews here in the same colors the BBS sends to a caller — and in the same
// colors ./config uses for its own chrome.
var dosColors = tuiart.Palette

// dosBgColors indexes the same palette for the eight DOS background colors.
// Background codes |B0-|B7 cannot address the bright half of the palette.
var dosBgColors = tuiart.Palette[:8]

// styledSpan represents a chunk of text with a specific style.
type styledSpan struct {
	text string
	fg   string // lipgloss color string (ANSI 256 index)
	bg   string // lipgloss background color string
}

// RenderColorString converts a BBS pipe-coded string into a lipgloss-styled
// string suitable for TUI display. It parses |XX foreground codes (00-15),
// |BX background codes (B0-B7), $x/$X dollar-sign color codes, and |CR
// (converted to space). The maxWidth parameter truncates visible characters
// (not counting color codes) and adds a "»" overflow indicator.
func RenderColorString(s string, maxWidth int) string {
	spans := parseColorCodes(s)
	return renderSpans(spans, maxWidth)
}

// parseColorCodes parses a BBS string with pipe/dollar color codes into spans.
func parseColorCodes(s string) []styledSpan {
	var spans []styledSpan
	curFG := dosColors[dosLightBlue] // Pascal DataColor = 9
	curBG := ""                      // Default: the panel's own background

	i := 0
	textBuf := strings.Builder{}

	flushText := func() {
		if textBuf.Len() > 0 {
			spans = append(spans, styledSpan{text: textBuf.String(), fg: curFG, bg: curBG})
			textBuf.Reset()
		}
	}

	for i < len(s) {
		// Check for pipe codes: |XX
		if s[i] == '|' && i+2 < len(s) {
			code := s[i+1 : i+3]

			// Background: |B0 - |B7
			if code[0] == 'B' && code[1] >= '0' && code[1] <= '7' {
				flushText()
				bgIdx := int(code[1] - '0')
				curBG = dosBgColors[bgIdx]
				i += 3
				continue
			}

			// Foreground: |00 - |15
			if code[0] >= '0' && code[0] <= '1' && code[1] >= '0' && code[1] <= '9' {
				num := int(code[0]-'0')*10 + int(code[1]-'0')
				if num >= 0 && num <= 15 {
					flushText()
					curFG = dosColors[num]
					i += 3
					continue
				}
			}

			// Special codes
			if code == "CR" {
				textBuf.WriteByte(' ')
				i += 3
				continue
			}
			if code == "CL" || code == "DE" {
				// Clear screen / clear to EOL — skip in TUI context
				i += 3
				continue
			}

			// |@ position codes — skip
			if code[0] == '@' {
				// Skip |@ followed by position data
				i += 3
				// May have additional position bytes; skip digits
				for i < len(s) && s[i] >= '0' && s[i] <= '9' {
					i++
				}
				continue
			}

			// Unrecognized pipe code — pass through as literal
			textBuf.WriteByte('|')
			i++
			continue
		}

		// Check for dollar-sign codes: $x
		if s[i] == '$' && i+1 < len(s) {
			ch := s[i+1]
			colorIdx := dollarColorIndex(ch)
			if colorIdx >= 0 {
				flushText()
				curFG = dosColors[colorIdx]
				i += 2
				continue
			}
			// Unrecognized dollar code — pass through
			textBuf.WriteByte('$')
			i++
			continue
		}

		// Regular character
		textBuf.WriteByte(s[i])
		i++
	}

	flushText()
	return spans
}

// dollarColorIndex maps a dollar-sign color code character to a DOS color index.
// Returns -1 for unrecognized characters.
// Matches the Pascal WriteColor() procedure's $x handling.
func dollarColorIndex(ch byte) int {
	switch ch {
	case 'a':
		return 0 // Black
	case 'b':
		return 1 // Blue
	case 'g':
		return 2 // Green
	case 'c':
		return 3 // Cyan
	case 'r':
		return 4 // Red
	case 'p':
		return 5 // Magenta
	case 'y':
		return 6 // Brown
	case 'w':
		return 7 // Light Gray
	case 'A':
		return 8 // Dark Gray
	case 'B':
		return 9 // Light Blue
	case 'G':
		return 10 // Light Green
	case 'C':
		return 11 // Light Cyan
	case 'R':
		return 12 // Light Red
	case 'P':
		return 13 // Light Magenta
	case 'Y':
		return 14 // Yellow
	case 'W':
		return 15 // White
	default:
		return -1
	}
}

// renderSpans converts styled spans to a lipgloss-rendered string, fitting the
// result into maxWidth terminal cells.
//
// Control characters are never emitted raw: a stored CR, LF, tab or escape
// would move the cursor and corrupt the surrounding list, so each one is drawn
// as the same backslash sequence the editor accepts as input, in a contrasting
// style so it reads as a marker rather than as literal text.
func renderSpans(spans []styledSpan, maxWidth int) string {
	if maxWidth <= 0 {
		maxWidth = 80
	}

	// Leave the last cell for the overflow marker.
	budget := maxWidth - 1

	var result strings.Builder
	used := 0

	overflow := tuiart.Color(dosMagenta, dosWhite)

	for _, span := range spans {
		// The panel sits on the backdrop art, so a span with no background of
		// its own still paints the panel's black ground rather than letting
		// the art show through.
		bg := span.bg
		if bg == "" {
			bg = dosColors[dosBlack]
		}
		style := lipgloss.NewStyle().
			Foreground(lipgloss.Color(span.fg)).
			Background(lipgloss.Color(bg))

		for _, ch := range span.text {
			text := string(ch)
			chStyle := style
			if esc, escaped := escapeRune(ch); escaped {
				text = esc
				chStyle = controlStyle
			}
			w := cellWidth(text)
			if used+w > budget {
				result.WriteString(overflow.Render("»"))
				return result.String()
			}
			result.WriteString(chStyle.Render(text))
			used += w
		}
	}

	return result.String()
}

// controlStyle marks escaped control characters in a preview so they are
// distinguishable from a value that literally contains "\r".
var controlStyle = tuiart.Color(dosLightGray, dosBlack)

// cellWidth returns the number of terminal cells s occupies, counting wide
// characters as two and combining marks as zero.
func cellWidth(s string) int {
	w := 0
	for _, r := range s {
		w += runewidth.RuneWidth(r)
	}
	return w
}

// PlainTextLength returns the number of terminal cells a BBS pipe-coded string
// occupies once color codes are stripped and control characters are shown in
// their escaped form, matching what RenderColorString draws.
func PlainTextLength(s string) int {
	spans := parseColorCodes(s)
	total := 0
	for _, span := range spans {
		for _, r := range span.text {
			if esc, escaped := escapeRune(r); escaped {
				total += cellWidth(esc)
				continue
			}
			total += runewidth.RuneWidth(r)
		}
	}
	return total
}
