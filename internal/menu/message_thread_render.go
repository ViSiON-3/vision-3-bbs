package menu

import (
	"fmt"

	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
)

// threadGlyphs are the characters the thread pane and menu keys draw that
// differ between a UTF-8 and a CP437 terminal. The box-drawing characters
// are written as UTF-8 either way; the output writer converts them.
type threadGlyphs struct {
	marker   string // the current row's pointer
	up       string // the thread menu's Move key
	down     string
	ellipsis string // marks a clipped handle or excerpt
}

// threadGlyphsFor picks the glyphs for mode. CP437 draws glyphs for the
// control bytes 0x10, 0x18 and 0x19, and has no ellipsis, so a clipped name
// ends in "~" there.
func threadGlyphsFor(mode ansi.OutputMode) threadGlyphs {
	if mode == ansi.OutputModeCP437 {
		return threadGlyphs{marker: "\x10", up: "\x18", down: "\x19", ellipsis: "~"}
	}
	return threadGlyphs{marker: "►", up: "↑", down: "↓", ellipsis: "…"}
}

// threadPlainText prepares text taken from a message (a subject, a handle,
// a body line) for a pane cell. A body stored as raw CP437, as
// imported echomail can be, is decoded so it is clipped by character; then
// escape sequences and control characters are dropped, so a message cannot
// clear the screen or move the cursor from inside the pane.
//
// Pipes are left in. The finished cell is passed through the pipe-code
// expander with the colours around it, so threadEscapePipes doubles them
// after the cell has been fitted, where the extra byte costs no column.
func threadPlainText(s string) string {
	if !utf8.ValidString(s) {
		s = string(ansi.CP437BytesToUTF8([]byte(s)))
	}
	s = ansi.StripAnsi(s)
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, s)
}

// threadEscapePipes doubles each pipe so the expander prints it rather than
// reading a colour code out of the message. Applied after fitting: "||"
// still draws as one column.
func threadEscapePipes(s string) string {
	return strings.ReplaceAll(s, "|", "||")
}

// threadCell clips s to width columns with g's ellipsis and pads it, in the
// terminal's encoding, as listCell does for the message list.
//
// The cell stays UTF-8 whatever the terminal: the output writer converts it
// for CP437, one byte per rune ("?" where CP437 has no character), so the
// width holds. Converting here would put CP437 bytes next to the UTF-8
// glyphs around them, and a pair that happened to form a valid UTF-8
// sequence would be folded into one byte.
func threadCell(s string, width int, g threadGlyphs, _ ansi.OutputMode) string {
	s = ansi.TruncateRunes(threadPlainText(s), width, g.ellipsis)
	if n := utf8.RuneCountInString(s); n < width {
		s += strings.Repeat(" ", width-n)
	}
	return threadEscapePipes(s)
}

// threadText prepares s for the terminal without padding it; UTF-8, as for
// threadCell.
func threadText(s string, _ ansi.OutputMode) string {
	return threadEscapePipes(threadPlainText(s))
}

// excerpt is the line of body that stands for the message in a thread
// view: the first that is not blank, quoted, a tear line or an origin line.
// A body stored as raw CP437 is decoded first, so the line is text.
func excerpt(body string) string {
	if !utf8.ValidString(body) {
		body = string(ansi.CP437BytesToUTF8([]byte(body)))
	}
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.TrimSpace(line) == "" || isQuoteLine(line) || isTearLine(line) || isOriginLine(line) {
			continue
		}
		return strings.TrimSpace(line)
	}
	return ""
}

// Thread pane geometry. The pane takes paneRows tree rows plus a title row
// and a closing rule, and shrinks or goes away rather than leave the body
// fewer than paneMinBodyLines.
const (
	paneRows         = 5
	paneRowsReduced  = 3
	paneMinBodyLines = 6
	paneAuthorWidth  = 9
	paneDateWidth    = 8
	// maxTreeDepth is the deepest level drawn with glyphs; deeper rows
	// collapse their prefix to "+N".
	maxTreeDepth = 8
)

// paneRowsFor returns how many tree rows the pane may take out of
// bodyAvail body lines: 5, else 3, else none.
func paneRowsFor(bodyAvail int) int {
	for _, rows := range []int{paneRows, paneRowsReduced} {
		if bodyAvail-(rows+2) >= paneMinBodyLines {
			return rows
		}
	}
	return 0
}

// treePrefix is a row's tree glyphs, collapsed past maxTreeDepth.
func treePrefix(r threadRow) string {
	if r.Depth > maxTreeDepth {
		return strings.Repeat("  ", maxTreeDepth-1) + "+" + strconv.Itoa(r.Depth-(maxTreeDepth-1))
	}
	return r.Prefix
}

// renderThreadMenuKeys builds the separator row while the thread menu is
// open: its keys, then a rule to fill width columns. The result is
// pipe-coded.
func renderThreadMenuKeys(width int, g threadGlyphs) string {
	keys := []struct{ key, label string }{
		{g.up + g.down, "Move"},
		{"Enter", "Read"},
		{"T/Esc", "Close"},
	}
	var b strings.Builder
	used := 0
	for _, k := range keys {
		b.WriteString("|08─|03[|11" + k.key + "|03]|07" + k.label + " ")
		used += 4 + utf8.RuneCountInString(k.key) + len(k.label)
	}
	if used < width {
		b.WriteString("|08" + strings.Repeat("─", width-used))
	}
	b.WriteString("|07")
	return b.String()
}

// renderThreadPane draws the docked thread pane for tv: a title row, rows
// tree rows windowed around message hl, and a closing rule. hl is the row
// drawn highlighted: the thread menu's cursor while the menu is open, else
// the message being read, which always carries the marker. Each line is
// pipe-coded (with raw escapes for the highlight's background) and exactly
// width columns. title is the pane's label, " Thread ".
func renderThreadPane(tv *threadView, hl, rows, width int, title string, g threadGlyphs, mode ansi.OutputMode) []string {
	lines := make([]string, 0, rows+2)

	// Title: ──[ Thread ]─ subject ──────── pos of count · depth d ──
	pos, count := tv.position(tv.cur)
	right := fmt.Sprintf(" %d of %d · depth %d ", pos, count, tv.depth(tv.cur))
	left := "──" + title + "─ "
	subjectRoom := width - utf8.RuneCountInString(left) - 1 - utf8.RuneCountInString(right) - 2 - 3
	subject := ""
	if subjectRoom >= minSubjectRoom {
		subject = ansi.TruncateRunes(threadPlainText(tv.rootSubject()), subjectRoom, g.ellipsis) + " "
	}
	fill := width - utf8.RuneCountInString(left) - utf8.RuneCountInString(subject) - utf8.RuneCountInString(right) - 2
	lines = append(lines, "|08──|03"+title+"|08─ |11"+threadText(subject, mode)+
		"|08"+strings.Repeat("─", max(fill, 0))+"|03"+right+"|08──|07")

	// Tree rows: a window of rows lines that keeps the highlighted row two
	// from the top where the thread allows.
	hi := tv.pos[hl]
	start := max(0, min(hi-2, len(tv.rows)-rows))
	for i := 0; i < rows; i++ {
		if start+i >= len(tv.rows) {
			lines = append(lines, strings.Repeat(" ", width))
			continue
		}
		r := tv.rows[start+i]
		lines = append(lines, renderTreeRow(r, r.Num == tv.cur, start+i == hi, width, g, mode))
	}

	lines = append(lines, "|08"+strings.Repeat("─", width)+"|07")
	return lines
}

// minSubjectRoom is the narrowest the pane title shows a subject in.
const minSubjectRoom = 4

// renderTreeRow draws one pane row, width columns wide:
//
//	► ├─#5 Phosphor… That was it. Font sticks now, but the menus…  10/20/26
//
// The highlighted row is drawn on a blue background, in white; the rest use
// cyan glyphs and number, grey author and date and a dark excerpt. The
// message being read (current) carries the marker, in yellow. A deleted
// placeholder shows only "#n deleted".
func renderTreeRow(r threadRow, current, highlight bool, width int, g threadGlyphs, mode ansi.OutputMode) string {
	prefix := treePrefix(r)
	var b strings.Builder
	used := 0
	marker := " "
	if current {
		marker = g.marker
	}
	if highlight {
		b.WriteString("\x1b[44m|14" + marker + " |11")
	} else {
		b.WriteString("|14" + marker + " |03")
	}
	used += 2
	b.WriteString(prefix)
	used += utf8.RuneCountInString(prefix)

	if r.Deleted {
		text := fmt.Sprintf("#%d deleted", r.Num)
		b.WriteString("|08" + text)
		used += len(text)
		b.WriteString(strings.Repeat(" ", max(width-used, 0)))
		b.WriteString("\x1b[0m|07")
		return b.String()
	}

	num := fmt.Sprintf("#%d ", r.Num)
	numColour, authorColour, textColour, dateColour := "|11", "|07", "|08", "|07"
	if highlight {
		numColour, authorColour, textColour, dateColour = "|15", "|15", "|15", "|15"
	}
	b.WriteString(numColour + num)
	used += len(num)

	author := ""
	if r.Msg != nil {
		author = r.Msg.From
	}
	b.WriteString(authorColour + threadCell(author, paneAuthorWidth, g, mode) + " ")
	used += paneAuthorWidth + 1

	// The text, then a two-column gap, then the date in the last columns.
	room := width - used - 2 - paneDateWidth
	text := ""
	if room > minSubjectRoom && r.Msg != nil {
		if r.Depth == 0 {
			text = r.Msg.Subject
		} else {
			text = excerpt(r.Msg.Body)
		}
	}
	if room > 0 {
		b.WriteString(textColour + threadCell(text, room, g, mode))
		used += room
	}
	date := strings.Repeat(" ", paneDateWidth)
	if r.Msg != nil && !r.Msg.DateTime.IsZero() {
		date = r.Msg.DateTime.Format("01/02/06")
	}
	if used+2+paneDateWidth <= width {
		b.WriteString("  " + dateColour + date)
		used += 2 + paneDateWidth
	}
	b.WriteString(strings.Repeat(" ", max(width-used, 0)))
	b.WriteString("\x1b[0m|07")
	return b.String()
}
