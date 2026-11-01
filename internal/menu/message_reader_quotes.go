package menu

import (
	"strings"
	"unicode/utf8"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/editor"
)

const (
	// quoteReflowMinSourceWidth is the narrowest wrap width at which the reader
	// joins quote lines. The editor's floor is lower because a caller is there
	// to see the result; here it runs on every message, and verse quoted on its
	// own would otherwise have its longest line read as a full one.
	quoteReflowMinSourceWidth = 60
	// quoteReflowMinColumns is the least room for text beside a prefix. Below
	// it, on a very narrow screen or under very deep nesting, the prefix would
	// crowd out the text, so the line is left to the plain hard break.
	quoteReflowMinColumns = 10
)

// quoteLine is one stored line of a quote, taken apart for reflowing.
type quoteLine struct {
	raw    string // the line as it arrived
	plain  string // no escapes or trailing blanks: what the #551 rules measure
	prefix string // normalized, such as "Bu> Sh> "
	head   string // escapes from before the end of the prefix
	indent string // the body's own leading blanks
	body   string // the rest, with its escapes
}

// parseQuoteLine takes line apart. It reports false for anything that is not a
// quote line with a prefix: tearlines, origin lines, banners, and block or box
// glyphs followed by '>', which are colour art rather than initials.
func parseQuoteLine(line string, banners quoteBannerMatcher) (quoteLine, bool) {
	plain := strings.TrimRight(stripEscapes(line), " \t\r")
	prefix, plainBody := editor.SplitQuotePrefix(plain)
	if prefix == "" || isTearLine(plain) || isOriginLine(plain) || banners.match(line) {
		return quoteLine{}, false
	}
	if strings.ContainsFunc(prefix, func(r rune) bool { return r >= 0x2500 && r <= 0x259F }) {
		return quoteLine{}, false
	}

	var head strings.Builder
	rest := line
	for visible := len(plain) - len(plainBody); visible > 0 && rest != ""; {
		if n := escapeLen(rest, 0); n > 0 {
			head.WriteString(rest[:n])
			rest = rest[n:]
			continue
		}
		rest = rest[1:]
		visible--
	}
	indent, body := splitQuoteBody(rest)
	return quoteLine{raw: line, plain: plain, prefix: prefix, head: head.String(), indent: indent, body: body}, true
}

// splitQuoteBody separates a body's leading blanks and drops its trailing ones,
// keeping every escape, so that joining two bodies leaves a single space.
func splitQuoteBody(s string) (indent, body string) {
	var codes, text, ind, pending strings.Builder
	leading := true
	for i := 0; i < len(s); {
		if n := escapeLen(s, i); n > 0 {
			if leading {
				codes.WriteString(s[i : i+n])
			} else {
				pending.WriteString(s[i : i+n])
			}
			i += n
			continue
		}
		c := s[i]
		i++
		switch {
		case c == ' ' || c == '\t' || c == '\r':
			if leading && c != '\r' {
				ind.WriteByte(c)
			} else if !leading {
				pending.WriteByte(c)
			}
		default:
			leading = false
			text.WriteString(pending.String())
			pending.Reset()
			text.WriteByte(c)
		}
	}
	// What is left pending trails the text: keep its escapes, drop its blanks.
	tail := pending.String()
	for i := 0; i < len(tail); {
		if n := escapeLen(tail, i); n > 0 {
			text.WriteString(tail[i : i+n])
			i += n
			continue
		}
		i++
	}
	return ind.String(), codes.String() + text.String()
}

// reflowQuotes rewrites the quote lines of a colour-expanded UTF-8 body for
// display at width, one leading space and the prefix on every row. It works on
// a copy for the screen: the stored message is not touched.
//
// Which lines join is decided by the rules the editor uses for quoting (#551),
// so a line that ended early, a blank line, a list item, or a change of depth
// or indent all break the paragraph. A quote line that is not joined and fits
// keeps its interior spacing, so a quoted table stays aligned.
func reflowQuotes(body string, width int, mode ansi.OutputMode, banners quoteBannerMatcher) string {
	if width <= 0 || containsAnsiArt(body) {
		return body
	}
	rows := strings.Split(body, "\n")

	// Each depth of a message was wrapped once, by whoever quoted it, so each
	// has its own source width. The author's own text says nothing about it.
	parsed := make([]*quoteLine, len(rows))
	byPrefix := map[string][]string{}
	for i, row := range rows {
		if q, ok := parseQuoteLine(row, banners); ok {
			parsed[i] = &q
			byPrefix[q.prefix] = append(byPrefix[q.prefix], q.plain)
		}
	}
	if len(byPrefix) == 0 {
		return body
	}
	sourceWidth := make(map[string]int, len(byPrefix))
	for prefix, lines := range byPrefix {
		if w := editor.QuoteSourceWidth(lines); w >= quoteReflowMinSourceWidth {
			sourceWidth[prefix] = w
		}
	}

	out := make([]string, 0, len(rows))
	for i := 0; i < len(rows); i++ {
		if parsed[i] == nil {
			out = append(out, rows[i])
			continue
		}
		para := []*quoteLine{parsed[i]}
		for i+1 < len(rows) && parsed[i+1] != nil &&
			editor.ContinuesParagraph(parsed[i].plain, parsed[i+1].plain, sourceWidth[parsed[i].prefix]) {
			i++
			para = append(para, parsed[i])
		}
		out = append(out, wrapQuoteParagraph(para, width, mode)...)
	}
	return strings.Join(out, "\n")
}

// wrapQuoteParagraph lays one paragraph out in rows no wider than width.
func wrapQuoteParagraph(para []*quoteLine, width int, mode ansi.OutputMode) []string {
	first := para[0]
	lead := " " + first.prefix + first.indent
	avail := width - visibleColumns(lead, mode)
	if avail < quoteReflowMinColumns {
		raw := make([]string, len(para))
		for i, q := range para {
			raw[i] = q.raw
		}
		return raw
	}

	if len(para) == 1 {
		// An empty body is a bare "RW>": no trailing blank to spend a column on.
		if stripEscapes(first.body) == "" {
			return []string{first.head + strings.TrimRight(lead, " ") + first.body}
		}
		if visibleColumns(first.body, mode) <= avail {
			return []string{first.head + lead + first.body}
		}
	}

	bodies := make([]string, len(para))
	for i, q := range para {
		bodies[i] = q.body
	}
	joined := strings.Join(bodies, " ")

	var out []string
	carry := ""
	for _, row := range wrapVisualLine(joined, avail, utf8.ValidString(joined), mode) {
		chunks := []string{row}
		// A word wider than the room has to be cut, but under the prefix.
		if visibleColumns(row, mode) > avail {
			chunks = hardBreak(row, avail, mode)
		}
		for _, chunk := range chunks {
			out = append(out, first.head+lead+carry+chunk)
			carry = activeColour(carry, chunk)
		}
	}
	return out
}

// activeColour returns the colour sequences still in force after s, given those
// in force before it. Each row's prefix is drawn in the quote's colour, so a
// colour the text set on an earlier row has to be put back after the prefix.
func activeColour(colour, s string) string {
	for i := 0; i < len(s); {
		n := escapeLen(s, i)
		if n == 0 {
			i++
			continue
		}
		if seq := s[i : i+n]; strings.HasSuffix(seq, "m") {
			// A sequence opening with a reset cancels all that came before.
			params := strings.TrimSuffix(strings.TrimPrefix(seq, "\x1b["), "m")
			if params == "" || params == "0" || strings.HasPrefix(params, "0;") {
				colour = seq
			} else {
				colour += seq
			}
		}
		i += n
	}
	return colour
}
