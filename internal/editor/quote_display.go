package editor

import (
	"strings"
	"unicode/utf8"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
)

// How a quote line looks on screen, shared by the message reader and the
// editor so that what a caller sees while writing a reply is what readers see
// once it is posted (#962). The stored message is never changed by this: both
// rewrite a copy of each line for the screen.
//
// A quote line is drawn in QuoteColor as QuoteLead, its prefix normalized to
// single spaces ("RW> Sh> "), then the text, its own indent kept. The reader
// rewraps quotes no wider than QuoteDisplayWidth for its terminal, the same
// margin the editor keeps on its own screen.

// QuoteColor is the pipe code quote lines are drawn in.
const QuoteColor = "|14"

// QuoteLead is drawn before the prefix on every row of a quote.
const QuoteLead = " "

// quoteSGR is QuoteColor as an escape, for the editor's own drawing.
var quoteSGR = string(ansi.ReplaceColorPipeCodes([]byte(QuoteColor)))

// QuoteDisplayWidth is the widest a quote row is drawn on a terminal width
// columns wide: one short of the edge. The editor never writes a message line
// into the last column (MaxLineLength is 79 for an 80-column screen), so a
// quote the reader reflowed into it would wrap one column wider than it was
// written, moving words between rows.
func QuoteDisplayWidth(width int) int {
	return width - 1
}

// SplitDisplayQuotePrefix splits a plain line, no escapes, into the quote
// prefix it is drawn with and the text after it. It is SplitQuotePrefix less the
// rows whose "initials" are block or box glyphs, which are colour art, plus the
// Usenet-style ">text" and ">>text" lines, whose bare marker is the prefix. The
// line may be raw CP437 or UTF-8. A line that is not a quote returns "" and the
// line unchanged.
func SplitDisplayQuotePrefix(line string) (prefix, body string) {
	if prefix, body = SplitQuotePrefix(line); prefix != "" {
		decoded := prefix
		if !utf8.ValidString(decoded) {
			decoded = string(ansi.CP437BytesToUTF8([]byte(decoded)))
		}
		if !strings.ContainsFunc(decoded, func(r rune) bool { return r >= 0x2500 && r <= 0x259F }) {
			return prefix, body
		}
	}
	// A bare marker: one or more '>' with no space before the text. Leading
	// blanks are dropped, as SplitQuotePrefix drops them.
	trimmed := strings.TrimLeft(line, " \t")
	n := len(trimmed) - len(strings.TrimLeft(trimmed, ">"))
	if n == 0 {
		return "", line
	}
	return trimmed[:n], trimmed[n:]
}

// quoteDisplay is one buffer line as the screen shows it.
type quoteDisplay struct {
	text  string // what is drawn, escapes included
	shift int    // columns the drawing adds before the text (may be negative)
	at    int    // byte offset in the stored line where the text starts
	// atChars is at in characters, the unit of buffer columns (#1051).
	atChars int
	lead    int // columns drawn before the text
	// prefixCols is the 0-based screen column of each character before at.
	// The prefix is drawn with its blanks normalized, so a cursor in it has
	// to be placed character by character, not by the text's shift.
	prefixCols []int
	quote      bool
}

// screenCol is the 1-based screen column buffer column col is drawn at.
func (d quoteDisplay) screenCol(col int) int {
	if !d.quote || col < 1 {
		return col
	}
	if i := col - 1; i < d.atChars {
		return d.prefixCols[i] + 1
	}
	return d.lead + col - d.atChars
}

// displayQuoteLine lays a stored editor line out the way the reader draws it,
// if it is a quote line. A line that is not one comes back as it is.
func displayQuoteLine(line string) quoteDisplay {
	plain := stripANSI(line)
	if isDisplayTrailerLine(plain) {
		return quoteDisplay{text: line}
	}
	prefix, body := SplitDisplayQuotePrefix(plain)
	if prefix == "" {
		return quoteDisplay{text: line}
	}

	// Find where the text starts in the stored line, keeping the escapes that
	// come before it so the line keeps any colour they set.
	var head strings.Builder
	at := 0
	for visible := len(plain) - len(body); visible > 0 && at < len(line); {
		if n := escapeAt(line, at); n > 0 {
			head.WriteString(line[at : at+n])
			at += n
			continue
		}
		at++
		visible--
	}

	lead := QuoteLead + prefix
	if body == "" {
		// A bare "RW>": no trailing blank, as the reader draws it.
		lead = strings.TrimRight(lead, " ")
	}
	stored := utf8.RuneCountInString(plain[:len(plain)-len(body)])
	leadRunes := []rune(lead)
	return quoteDisplay{
		text:       quoteSGR + head.String() + lead + line[at:],
		shift:      len(leadRunes) - stored,
		at:         at,
		atChars:    utf8.RuneCountInString(line[:at]),
		lead:       len(leadRunes),
		prefixCols: charCols(line[:at], mapPrefixCols(line[:at], leadRunes)),
		quote:      true,
	}
}

// mapPrefixCols gives each byte of stored, a line up to where its text starts,
// the screen column it is drawn at under lead. Normalizing changes only blanks,
// so each other character is found in lead in order; a blank, or an escape,
// sits just after the character before it.
func mapPrefixCols(stored string, lead []rune) []int {
	cols := make([]int, len(stored))
	li, last := 0, 0
	for j := 0; j < len(stored); {
		n := escapeAt(stored, j)
		col := last
		if n == 0 {
			r, size := utf8.DecodeRuneInString(stored[j:])
			n = size
			if r != ' ' && r != '\t' {
				for li < len(lead) && lead[li] != r {
					li++
				}
				col = min(li, max(0, len(lead)-1))
				li++
				last = min(li, len(lead))
			}
		}
		for k := j; k < j+n && k < len(cols); k++ {
			cols[k] = min(col, len(lead))
		}
		j += n
	}
	return cols
}

// quoteLeadExtra is the columns the display adds beside prefix, the quote
// prefix a paragraph is wrapped under. The editor leaves them free when it wraps
// so that the reader, which draws them too, wraps the paragraph the same way.
func quoteLeadExtra(prefix string) int {
	if prefix == "" {
		return 0
	}
	return max(0, displayQuoteLine(prefix+"x").shift)
}

// isDisplayTrailerLine reports a tearline or origin line, which the reader never
// draws as a quote.
func isDisplayTrailerLine(plain string) bool {
	t := strings.TrimSpace(plain)
	return strings.HasPrefix(t, "---") || strings.HasPrefix(t, "* Origin:")
}

// escapeAt is the length of the escape sequence starting at s[i], or 0 if
// there is none there. It measures what stripANSI removes, a lone ESC at the
// end included.
func escapeAt(s string, i int) int {
	if s[i] != 0x1B {
		return 0
	}
	j := i + 1
	if j >= len(s) {
		return 1
	}
	switch c := s[j]; {
	case c == '[':
		j++
		for j < len(s) && (s[j] < 0x40 || s[j] > 0x7E) {
			j++
		}
		if j < len(s) {
			j++ // final byte
		}
	case c == ']' || c == 'P' || c == 'X' || c == '^' || c == '_':
		j++
		for j < len(s) {
			if s[j] == 0x07 {
				j++
				break
			}
			if s[j] == 0x1B && j+1 < len(s) && s[j+1] == '\\' {
				j += 2
				break
			}
			j++
		}
	case c >= 0x20 && c <= 0x7E:
		j++ // two-byte escape
	}
	return j - i
}

// charCols turns mapPrefixCols's column per byte of stored into one per
// character, the unit buffer columns count (#1051): each character takes
// the column of its first byte.
func charCols(stored string, byteCols []int) []int {
	out := make([]int, 0, len(stored))
	for i := 0; i < len(stored); {
		out = append(out, byteCols[i])
		_, size := utf8.DecodeRuneInString(stored[i:])
		i += size
	}
	return out
}
