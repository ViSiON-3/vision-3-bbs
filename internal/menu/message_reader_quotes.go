package menu

import (
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/mattn/go-runewidth"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/editor"
)

const (
	// quoteReflowMinSourceWidth is the narrowest wrap width at which the reader
	// joins quote lines. The editor's floor is lower because a caller is there
	// to see the result; here it runs on every message, and verse quoted on its
	// own would otherwise have its longest line read as a full one.
	//
	// The floor only narrows that case (#863): a quoted poem whose longest
	// line is this wide or wider still reads as prose wrapped at that width,
	// so the line after its longest is joined onto it. Widths carry no better
	// signal, and the cost is two lines run together on screen, so this is a
	// documented limit (docs/sysop/messages/message-areas.md).
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

// splitReaderQuotePrefix is editor.SplitQuotePrefix less the rows whose
// "initials" are block or box glyphs, which are colour art. line may still be
// raw CP437, as formatMessageBody sees it, or already decoded.
func splitReaderQuotePrefix(line string) (prefix, body string) {
	prefix, body = editor.SplitQuotePrefix(line)
	decoded := prefix
	if !utf8.ValidString(decoded) {
		decoded = string(ansi.CP437BytesToUTF8([]byte(decoded)))
	}
	if strings.ContainsFunc(decoded, func(r rune) bool { return r >= 0x2500 && r <= 0x259F }) {
		return "", line
	}
	return prefix, body
}

// bareQuoteMarker splits a Usenet-style quote line, one or more '>' with no
// space before the text (">text", ">>text"), into the marker and the text.
// Leading blanks are dropped, as SplitQuotePrefix drops them. It returns ""
// for a line that does not start with '>'. These lines have no prefix in the
// editor's sense, but isQuoteLine has always coloured them as quotes.
func bareQuoteMarker(line string) (marker, body string) {
	trimmed := strings.TrimLeft(line, " \t")
	n := len(trimmed) - len(strings.TrimLeft(trimmed, ">"))
	return trimmed[:n], trimmed[n:]
}

// parseQuoteLine takes line apart. It reports false for anything that is not a
// quote line with a prefix, including tearlines, origin lines and banners. A
// ">text" line counts, with the bare marker as its prefix, so that it wraps at
// word boundaries with the marker on every row.
func parseQuoteLine(line string, banners quoteBannerMatcher) (quoteLine, bool) {
	plain := strings.TrimRight(stripEscapes(line), " \t\r")
	prefix, plainBody := splitReaderQuotePrefix(plain)
	if prefix == "" {
		prefix, plainBody = bareQuoteMarker(plain)
	}
	if prefix == "" || isTearLine(plain) || isOriginLine(plain) || banners.match(line) {
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
		switch c {
		case ' ', '\t', '\r':
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
	// The quoter's editor measured lines in characters or in columns, which
	// differ for double-width text, so each is tried. For text without any
	// double-width characters the two agree.
	sourceWidth := make(map[string]int, len(byPrefix))
	sourceColumns := make(map[string]int, len(byPrefix))
	for prefix, lines := range byPrefix {
		if w := editor.QuoteSourceWidth(lines); w >= quoteReflowMinSourceWidth {
			sourceWidth[prefix] = w
		}
		if w := editor.QuoteSourceWidthMeasured(lines, quoteColumns); w >= quoteReflowMinSourceWidth {
			sourceColumns[prefix] = w
		}
	}
	continues := func(prev, next *quoteLine) bool {
		// The editor's checks parse prefixes again and can't see a bare
		// marker (">text", ">>text"), so a change of depth there would
		// pass them. The prefixes parsed here have to match first.
		if prev.prefix != next.prefix {
			return false
		}
		return editor.ContinuesParagraph(prev.plain, next.plain, sourceWidth[prev.prefix]) ||
			editor.ContinuesParagraphMeasured(prev.plain, next.plain, sourceColumns[prev.prefix], quoteColumns)
	}

	out := make([]string, 0, len(rows))
	for i := 0; i < len(rows); i++ {
		if parsed[i] == nil {
			out = append(out, rows[i])
			continue
		}
		para := []*quoteLine{parsed[i]}
		for i+1 < len(rows) && parsed[i+1] != nil && continues(parsed[i], parsed[i+1]) {
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

	var b strings.Builder
	for i, q := range para {
		// A stored line usually closes its own colour, so each later line has
		// to open its colour again or the rest of the paragraph loses it.
		if i > 0 {
			b.WriteString(quoteJoinSeparator(para[i-1].body, q.body))
			b.WriteString(q.head)
		}
		b.WriteString(q.body)
	}
	joined := b.String()

	var out []string
	var colour sgrState
	for _, row := range wrapVisualLine(joined, avail, utf8.ValidString(joined), mode) {
		chunks := []string{row}
		// A word wider than the room has to be cut, but under the prefix.
		if visibleColumns(row, mode) > avail {
			chunks = hardBreak(row, avail, mode)
		}
		for _, chunk := range chunks {
			out = append(out, first.head+lead+colour.String()+chunk)
			colour.scan(chunk)
		}
	}
	return out
}

// sgrMaxParams bounds sgrState against a body built to grow it. Foreground and
// background replace themselves, so only odd attributes ever reach this.
const sgrMaxParams = 16

// sgrState is the colour text has set and not yet changed. Each row's prefix is
// drawn in the quote's colour, so a colour set on an earlier row has to be put
// back after the prefix. Only what is still in force is kept, so the cost of
// doing that does not grow with the number of colour changes in the paragraph.
type sgrState struct {
	reset  bool // the state starts from a reset, not from the prefix's colour
	params []string
}

// scan applies every colour sequence in s.
func (c *sgrState) scan(s string) {
	for i := 0; i < len(s); {
		n := escapeLen(s, i)
		if n == 0 {
			i++
			continue
		}
		if seq := s[i : i+n]; strings.HasPrefix(seq, "\x1b[") && strings.HasSuffix(seq, "m") {
			c.apply(strings.Split(seq[2:len(seq)-1], ";"))
		}
		i += n
	}
}

func (c *sgrState) apply(params []string) {
	for i := 0; i < len(params); i++ {
		p := params[i]
		if p == "" || p == "0" {
			c.reset = true
			c.params = nil
			continue
		}
		// An extended colour takes the parameters that follow it.
		if (p == "38" || p == "48") && i+1 < len(params) {
			extra := 0
			switch params[i+1] {
			case "5":
				extra = 2
			case "2":
				extra = 4
			}
			end := min(i+1+extra, len(params))
			p = strings.Join(params[i:end], ";")
			i = end - 1
		}
		class := sgrClass(p)
		c.params = slices.DeleteFunc(c.params, func(q string) bool { return sgrClass(q) == class })
		c.params = append(c.params, p)
		if len(c.params) > sgrMaxParams {
			c.params = c.params[1:]
		}
	}
}

// sgrClass names what a parameter sets, so that a later one of the same class
// replaces it.
func sgrClass(p string) string {
	code, _, _ := strings.Cut(p, ";")
	switch n, err := strconv.Atoi(code); {
	case err != nil:
		return p
	case n >= 30 && n <= 39, n >= 90 && n <= 97:
		return "fg"
	case n >= 40 && n <= 49, n >= 100 && n <= 107:
		return "bg"
	}
	return p
}

// String renders the state as one sequence, or "" if nothing has been set.
func (c *sgrState) String() string {
	if !c.reset && len(c.params) == 0 {
		return ""
	}
	params := strings.Join(c.params, ";")
	switch {
	case !c.reset:
	case params == "":
		params = "0"
	default:
		params = "0;" + params
	}
	return "\x1b[" + params + "m"
}

// quoteColumns measures a quote line in display columns, the way an editor
// that wraps double-width text by width would have. A line that is not valid
// UTF-8 is CP437, one column per byte.
func quoteColumns(s string) int {
	if !utf8.ValidString(s) {
		return len(s)
	}
	return runewidth.StringWidth(s)
}

// quoteJoinSeparator is what goes between two stored lines of a paragraph when
// they are joined: a space, which the wrap had stood for, except beside
// double-width text, which is written without spaces between its words.
func quoteJoinSeparator(prev, next string) string {
	last, _ := utf8.DecodeLastRuneInString(stripEscapes(prev))
	first, _ := utf8.DecodeRuneInString(stripEscapes(next))
	if runewidth.RuneWidth(last) > 1 || runewidth.RuneWidth(first) > 1 {
		return ""
	}
	return " "
}
