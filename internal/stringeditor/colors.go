package stringeditor

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
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

// dosBgColors is the palette colour of each background code |B0-|B7, in code
// order. The codes follow the ANSI order the BBS emits (ESC[40m-ESC[47m:
// black, red, green, brown, blue, magenta, cyan, gray), which is not the DOS
// order of the palette and the foreground codes (ansi.ColorOrder converts):
// |B1 is red, |B4 is blue. The codes cannot address the bright half of the
// palette.
var dosBgColors = func() (c [8]string) {
	for code := range c {
		c[code] = tuiart.Palette[ansi.ColorOrder(code)]
	}
	return c
}()

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
// Pipe codes are read with ansi.PipeCodeAt, the BBS's own longest-match
// lookup, and their colours worked out from the escape sequence the BBS would
// send (previewColor), so the preview draws what a caller sees.
func parseColorCodes(s string) []styledSpan {
	var spans []styledSpan
	col := previewColor{fg: dosLightBlue, bg: noPreviewBG} // Pascal DataColor = 9
	data := []byte(s)

	i := 0
	textBuf := strings.Builder{}

	flushText := func() {
		if textBuf.Len() > 0 {
			spans = append(spans, styledSpan{text: textBuf.String(), fg: dosColors[col.fg], bg: col.bgColor()})
			textBuf.Reset()
		}
	}

	for i < len(s) {
		// "||" is the BBS's escape for one literal pipe, whatever follows it.
		if s[i] == '|' && i+1 < len(s) && s[i+1] == '|' {
			textBuf.WriteByte('|')
			i += 2
			continue
		}

		if s[i] == '|' {
			// A strings.json value has its session placeholders filled in
			// before its pipe codes, and the preview shows placeholders as
			// written, so one that begins like a code (|PV, |POSTS) stays.
			if k := placeholderAt(s[i:]); k != "" {
				textBuf.WriteString(k)
				i += len(k)
				continue
			}
			if _, expansion, n := ansi.PipeCodeAt(data, i); n > 0 {
				if expansion == "\r\n" {
					textBuf.WriteByte(' ') // |CR: the preview is one row
				} else {
					// Colours change the style; screen codes (|CL, |DE,
					// |P, |PP, |XY) act on the caller's screen, so they
					// draw nothing here.
					next := col
					if next.apply(expansion) && next != col {
						flushText()
						col = next
					}
				}
				i += n
				continue
			}
			// Unrecognized pipe code: the BBS prints it as written.
			textBuf.WriteByte('|')
			i++
			continue
		}

		// Check for dollar-sign codes: $x
		if s[i] == '$' && i+1 < len(s) {
			if colorIdx := dollarColorIndex(s[i+1]); colorIdx >= 0 {
				flushText()
				col.fg = colorIdx
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

// noPreviewBG is previewColor's background when none is set: the panel's own.
const noPreviewBG = -1

// previewColor is the colour a caller's terminal would be drawing in: a DOS
// foreground 0-15 and a DOS background 0-15, or noPreviewBG.
type previewColor struct {
	fg, bg int
}

func (c previewColor) bgColor() string {
	if c.bg == noPreviewBG {
		return ""
	}
	return dosColors[c.bg]
}

// sgrRe matches one SGR escape sequence: ESC [ params m.
var sgrRe = regexp.MustCompile(`\x1b\[([0-9;]*)m`)

// apply applies the SGR sequences in seq, as a terminal would, and reports
// whether there were any. 0 resets to light gray on the default background,
// 1 brightens the foreground, 30-37 and 40-47 set the colours (in ANSI order,
// which ansi.ColorOrder maps to DOS), and 100-107 set a bright background.
func (c *previewColor) apply(seq string) bool {
	found := false
	for _, m := range sgrRe.FindAllStringSubmatch(seq, -1) {
		found = true
		params := strings.Split(m[1], ";")
		for j := 0; j < len(params); j++ {
			n, err := strconv.Atoi(params[j])
			if err != nil {
				n = 0 // ESC[m is a reset
			}
			switch {
			case n == 0:
				c.fg, c.bg = dosLightGray, noPreviewBG
			case n == 1:
				c.fg |= 8
			case n == 22:
				c.fg &^= 8
			case n >= 30 && n <= 37:
				c.fg = c.fg&8 | ansi.ColorOrder(n-30)
			case n >= 40 && n <= 47:
				c.bg = ansi.ColorOrder(n - 40)
			case n >= 100 && n <= 107:
				c.bg = ansi.ColorOrder(n-100) | 8
			case (n == 38 || n == 48) && j+1 < len(params) && params[j+1] == "5":
				j += 2 // a 256-colour value the 16-colour code already set
			}
		}
	}
	return found
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

// renderDimString previews a value the BBS supplies rather than the file, with
// its color codes stripped: the codes describe how the BBS will draw it, but
// the row itself is informational and reads as one dimmed run.
func renderDimString(s string, maxWidth int) string {
	var b strings.Builder
	used := 0
	budget := maxWidth - 1
	for _, span := range parseColorCodes(s) {
		for _, ch := range span.text {
			text := string(ch)
			if esc, escaped := escapeRune(ch); escaped {
				text = esc
			}
			w := cellWidth(text)
			if used+w > budget {
				b.WriteString(tuiart.Color(dosMagenta, dosWhite).Render("»"))
				return b.String()
			}
			b.WriteString(dimStyle.Render(text))
			used += w
		}
	}
	return b.String()
}

// dimStyle draws a runtime-supplied preview.
var dimStyle = tuiart.Color(dosBlack, dosDarkGray)

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

// PreviewPlaceholders are the session placeholders that begin with a pipe
// code: |P is save-cursor, so |PV and |POSTS would otherwise preview as a
// hidden code and text. A test in the menu package checks this covers every
// such placeholder there.
var PreviewPlaceholders = []string{"|POSTS", "|PV"}

// placeholderAt returns the one of PreviewPlaceholders s starts with, or "".
func placeholderAt(s string) string {
	for _, p := range PreviewPlaceholders {
		if strings.HasPrefix(s, p) {
			return p
		}
	}
	return ""
}
