package menu

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/mattn/go-runewidth"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
)

// codefenixSignature is the tail of a real fsxNet message (21:4/141) as it is
// stored in the JAM base: CP437 bytes, CR line endings, Renegade pipe codes.
// Every row of the box is exactly 30 visible columns wide before the right
// border, so the border must land in the same column on all five rows.
const codefenixSignature = "Good to be back!\r" +
	"\r" +
	"|08\xc4\xc4\xc4\xc4\xc4\xc4\xc4\xc4\xc4\xc4\xc4\xc4\xc4\xc4\xc4\xc4\xc4\xc4\xc4\xc4\xc4\xc4\xc4\xc4\xc4\xc4\xc4\xc4\xc4\xc4\xbf\r" +
	"|15 \xdc|07 \xdc |08\xdc  |11codefenix             |08\xb3\r" +
	"|15 \xdb|07\xdb\xdb\xdb|08\xdb  |09ConstructiveChaos BBS |08\xb3\r" +
	"|15 \xde|07\xdc\xdb\xdc|08\xdd  |01conchaos.synchro.net  |08\xb3\r" +
	"|08\xc4\xc4\xc4\xc4\xc4\xc4\xc4\xc4\xc4\xc4\xc4\xc4\xc4\xc4\xc4\xc4\xc4\xc4\xc4\xc4\xc4\xc4\xc4\xc4\xc4\xc4\xc4\xc4\xc4\xc4\xd9\r"

// readBodyForTest mirrors what the message reader does to a body before it
// paints it: format, expand colour pipe codes, wrap to the terminal width and
// cut whatever is still too wide.
func readBodyForTest(body string, termWidth int) []string {
	formatted := formatMessageBody(body, "", false)
	return breakOversizedLines(wrapAnsiString(string(ansi.ReplaceColorPipeCodes([]byte(formatted))), termWidth, ansi.OutputModeUTF8), termWidth, ansi.OutputModeUTF8)
}

// A signature block that already fits the terminal must reach the screen
// unchanged. Re-flowing it on whitespace pulls the right-hand border of every
// row left onto the text, which is what the ragged "|" column looked like.
func TestMessageBodyPreservesSignatureBoxAlignment(t *testing.T) {
	lines := readBodyForTest(codefenixSignature, 79)

	borders := map[byte]bool{0xbf: true, 0xb3: true, 0xd9: true}

	var cols []int
	for _, ln := range lines {
		plain := reWrapEsc.ReplaceAllString(ln, "")
		if plain == "" {
			continue
		}
		if last := plain[len(plain)-1]; borders[last] {
			cols = append(cols, len(plain))
		}
	}
	if len(cols) != 5 {
		t.Fatalf("expected 5 bordered rows, got %d\nlines: %q", len(cols), lines)
	}
	for i, c := range cols {
		if c != 31 {
			t.Errorf("row %d ends at visible column %d, want 31", i, c)
		}
	}
}

// The narrower symptom, stated directly: runs of spaces inside a line that
// already fits must not be collapsed, and leading indent must survive.
func TestMessageBodyKeepsInteriorSpacing(t *testing.T) {
	body := "|11codefenix             |08X\r  indented two\r"
	lines := readBodyForTest(body, 79)

	if len(lines) < 2 {
		t.Fatalf("expected 2 lines, got %d: %q", len(lines), lines)
	}
	if plain := reWrapEsc.ReplaceAllString(lines[0], ""); !strings.Contains(plain, "codefenix             X") {
		t.Errorf("interior spaces collapsed: %q", plain)
	}
	if plain := reWrapEsc.ReplaceAllString(lines[1], ""); !strings.HasPrefix(plain, "  indented") {
		t.Errorf("leading indent stripped: %q", plain)
	}
}

// Lines that genuinely overflow must still wrap on word boundaries, keep their
// leading indent, and never exceed the budget.
func TestMessageBodyStillWrapsOverlongLines(t *testing.T) {
	const width = 40
	body := "    The quick brown fox jumps over the lazy dog while the " +
		"enterprising aardvark contemplates its considerable misfortune.\r"

	lines := readBodyForTest(body, width)
	if len(lines) < 3 {
		t.Fatalf("expected the paragraph to wrap, got %d lines: %q", len(lines), lines)
	}
	for i, ln := range lines {
		if got := visibleCols(ln); got > width {
			t.Errorf("line %d is %d cols, over the %d budget: %q", i, got, width, ln)
		}
	}
	if plain := reWrapEsc.ReplaceAllString(lines[0], ""); !strings.HasPrefix(plain, "    The quick") {
		t.Errorf("first line lost its indent: %q", plain)
	}
	// Continuation lines start at the margin, not on a swallowed space.
	for i, ln := range lines[1:] {
		if plain := reWrapEsc.ReplaceAllString(ln, ""); strings.HasPrefix(plain, " ") {
			t.Errorf("continuation line %d starts with the break whitespace: %q", i+1, plain)
		}
	}
	// No word may be lost or invented.
	got := strings.Join(strings.Fields(strings.Join(lines, " ")), " ")
	want := strings.Join(strings.Fields(reWrapEsc.ReplaceAllString(body, "")), " ")
	if got != want {
		t.Errorf("wrapping altered the text:\n got  %q\n want %q", got, want)
	}
}

// Colour must survive a line break, including one that swallows the run of
// spaces the colour code was sitting in.
func TestMessageBodyCarriesColourAcrossABreak(t *testing.T) {
	const width = 20
	// The three spaces straddle the margin, so the break consumes them.
	lines := readBodyForTest("aaaaaaaaaaaaaaaaaaaa   |09bbbbbbbbbb", width)

	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d: %q", len(lines), lines)
	}
	if !strings.Contains(lines[1], "\x1b[") {
		t.Errorf("colour code was swallowed by the line break: %q", lines[1])
	}
	if plain := reWrapEsc.ReplaceAllString(lines[1], ""); plain != "bbbbbbbbbb" {
		t.Errorf("break whitespace leaked onto the next line: %q", plain)
	}
}

// A second real report: the clrghouz new-node announcement (21:3/100) is a
// plain CP437 frame with no pipe codes and no escapes at all, every row padded
// to exactly 79 columns. Reflowing it crushed the right-hand ": O :" rail into
// the text. Rows that exactly fill the width are the boundary case, so they
// must pass through byte-for-byte.
func TestMessageBodyPreservesFullWidthFrame(t *testing.T) {
	const width = 79

	// rail builds one row of the frame: a left rail, the interior padded out,
	// then the right rail, landing on exactly `width` columns.
	rail := func(post string, interior string) string {
		row := ": " + post + " : " + interior
		return row + strings.Repeat(" ", width-len(row)-5) + ": " + post + " :"
	}
	rows := []string{
		rail("O", strings.TrimRight(strings.Repeat("\xf9 ", 34), " ")),
		rail(" ", "\xda\xc4\xdc\xb3 \xda\xc4\xdc\xda\xc4\xdc\xdd\xc4\xdc\xda\xc4\xdc\xda \xdc\xda\xc4\xdc"),
		rail("O", "Hi Everyone,"),
		rail(" ", ""),
		rail("O", "* 21:3/255.0 - Grzegorz Worona (Dead Socket Church) from Warsaw."),
	}
	for i, r := range rows {
		if len(r) != width {
			t.Fatalf("test row %d is %d cols, fixture must be exactly %d", i, len(r), width)
		}
	}

	lines := readBodyForTest(strings.Join(rows, "\r"), width)

	var got []string
	for _, ln := range lines {
		if plain := reWrapEsc.ReplaceAllString(ln, ""); plain != "" {
			got = append(got, plain)
		}
	}
	if len(got) != len(rows) {
		t.Fatalf("frame reflowed onto %d rows, want %d: %q", len(got), len(rows), got)
	}
	for i := range rows {
		if got[i] != rows[i] {
			t.Errorf("row %d altered:\n got  %q\n want %q", i, got[i], rows[i])
		}
	}
}

// Message bodies are raw bytes and may be UTF-8 rather than CP437, where a
// column is a rune, not a byte. Measuring width in bytes made a UTF-8 line
// look wider than it is, so an aligned UTF-8 signature was reflowed even
// though it fits. (Counting runes unconditionally would be just as wrong: a
// CP437 art line is one column per byte, and many CP437 pairs happen to form
// a valid UTF-8 sequence, so the encoding is decided per line.)
func TestMessageBodyMeasuresUTF8BodiesInColumns(t *testing.T) {
	const width = 30
	line := "│ café    naïve    résumé │"

	if cols := utf8.RuneCountInString(line); cols > width {
		t.Fatalf("fixture is %d columns, must fit in %d", cols, width)
	}
	if len(line) <= width {
		t.Fatalf("fixture is %d bytes; it must exceed %d for this test to bite", len(line), width)
	}

	lines := wrapAnsiString(line, width, ansi.OutputModeUTF8)
	if len(lines) != 1 || lines[0] != line {
		t.Errorf("UTF-8 line was reflowed:\n got  %q\n want %q", lines, line)
	}
}

// CP437 art must still be measured in bytes: the clrghouz frame rows are 79
// columns of single-byte CP437, and counting them as runes would let an
// over-wide row through unwrapped.
func TestMessageBodyMeasuresCP437BodiesInBytes(t *testing.T) {
	const width = 20
	// Single-byte CP437 art with break opportunities, and not valid UTF-8.
	// 6 groups of 5 columns = 30 columns, over the budget.
	line := strings.TrimRight(strings.Repeat("\xc4\xdc\xc4\xdc ", 6), " ")
	if utf8.ValidString(line) {
		t.Fatalf("fixture must be invalid UTF-8 to exercise the CP437 path")
	}

	for _, ln := range wrapAnsiString(line, width, ansi.OutputModeUTF8) {
		if got := len(reWrapEsc.ReplaceAllString(ln, "")); got > width {
			t.Errorf("CP437 row measured as runes and left %d cols wide, over %d: %q", got, width, ln)
		}
	}
}

// A break that swallows the whitespace an ANSI reset sits in must not discard
// the reset, or the colour it closes bleeds into the rest of the body.
func TestMessageBodyKeepsTrailingResetAfterABreak(t *testing.T) {
	const width = 30
	line := strings.Repeat("a", width) + " \x1b[0m"

	lines := wrapAnsiString(line, width, ansi.OutputModeUTF8)
	if joined := strings.Join(lines, ""); !strings.Contains(joined, "\x1b[0m") {
		t.Errorf("trailing reset was dropped: %q", lines)
	}
	// It is zero-width, so it must not cost an extra row.
	if len(lines) != 1 {
		t.Errorf("expected the reset to ride on the last line, got %d lines: %q", len(lines), lines)
	}
}

// Display width, not rune count: a CJK rune occupies two terminal columns, so
// a line can fit comfortably by rune count and still overrun the margin.
func TestMessageBodyMeasuresWideRunesAsTwoColumns(t *testing.T) {
	const width = 20
	line := "你好世界 你好世界 你好世界"

	if n := utf8.RuneCountInString(line); n > width {
		t.Fatalf("fixture is %d runes; it must fit by rune count for this test to bite", n)
	}
	if runewidth.StringWidth(line) <= width {
		t.Fatalf("fixture must exceed %d display columns", width)
	}

	for i, ln := range wrapAnsiString(line, width, ansi.OutputModeUTF8) {
		if got := runewidth.StringWidth(reWrapEsc.ReplaceAllString(ln, "")); got > width {
			t.Errorf("line %d is %d display columns, over the %d budget: %q", i, got, width, ln)
		}
	}
}

// Width must be measured the way the writer will render it, not the way the
// bytes happen to decode. 0xDC 0xB3 is a CP437 pair (▄│) that is also a valid
// UTF-8 encoding of U+0733, a zero-width combining mark, so display width says
// it costs nothing - but WriteStringCP437 folds each rune to one CP437 byte,
// or to '?' where it does not map, and emits a column the margin has no room
// for. See #280 for the same ambiguity biting the writer.
func TestMessageBodyMeasuresForTheOutputTerminal(t *testing.T) {
	const width = 80
	line := strings.Repeat("a", width-1) + " \xdc\xb3"

	if !utf8.ValidString(line) {
		t.Fatalf("fixture must be valid UTF-8 for this ambiguity to bite")
	}
	if got := columnWidth(line, true, ansi.OutputModeUTF8); got != width {
		t.Fatalf("UTF-8 mode: %d columns, want %d", got, width)
	}
	if got := columnWidth(line, true, ansi.OutputModeCP437); got != width+1 {
		t.Errorf("CP437 mode: %d columns, want %d", got, width+1)
	}
	if got := wrapAnsiString(line, width, ansi.OutputModeCP437); len(got) != 2 {
		t.Errorf("CP437 mode: should have wrapped, got %d lines: %q", len(got), got)
	}
	if got := wrapAnsiString(line, width, ansi.OutputModeUTF8); len(got) != 1 {
		t.Errorf("UTF-8 mode: fits, should not wrap, got %d lines: %q", len(got), got)
	}
}

// kuehlboxRow is a row of a real fsxNet/FidoNet BBS ad (Kuehlbox BBS). The
// author drew the column divider as "||", which is text, not an escape.
const kuehlboxRow = " | :[FidoNet ]: || 2:240/5853         | | |> Radical Rhythms WHQ             |\r"

// A "||" in a network message is two pipes. Collapsing it to one, as
// ViSiON/3's own strings are, pulled the right border of every such row one
// column left of the rows around it (#407).
func TestMessageBodyKeepsDoublePipes(t *testing.T) {
	lines := readBodyForTest(kuehlboxRow, 80)
	if len(lines) == 0 || !strings.Contains(lines[0], "|| 2:240/5853") {
		t.Fatalf("double pipe was not kept: %q", lines)
	}
	if got, want := visibleCols(lines[0]), len(strings.TrimRight(kuehlboxRow, "\r")); got != want {
		t.Errorf("row is %d cols, want %d", got, want)
	}
}

// Only colour pipe codes mean anything in a message body. The screen-control
// codes fired a clear, a cursor save or an erase partway through painting the
// body, and "|P" also ate the letter after the pipe.
func TestMessageBodyLeavesControlPipeCodesAsText(t *testing.T) {
	lines := readBodyForTest("|CL|12Menu:|07 |Pimp Wars |DE done|CR", 80)
	joined := strings.Join(lines, "\n")
	for _, esc := range []string{"\x1b[2J", "\x1b[s", "\x1b[u", "\x1b[K"} {
		if strings.Contains(joined, esc) {
			t.Errorf("body expanded a control code to %q: %q", esc, joined)
		}
	}
	if plain := reWrapEsc.ReplaceAllString(joined, ""); plain != "|CLMenu: |Pimp Wars |DE done|CR" {
		t.Errorf("body text changed: %q", plain)
	}
	if !strings.Contains(joined, "\x1b[1;31mMenu:") {
		t.Errorf("colour code was not expanded: %q", joined)
	}
}

// A run with no spaces that is wider than the screen, such as an ASCII rule,
// has nowhere to word-wrap. Left oversized it spilled onto the row below when
// drawn and overwrote it, so it is cut at the margin instead (#407).
func TestMessageBodyCutsRunsWiderThanTheScreen(t *testing.T) {
	const width = 40
	rule := strings.Repeat("_", 75)
	lines := readBodyForTest("  "+rule+"\r|08"+strings.Repeat("\xdf", 60)+"\r", width)
	for i, ln := range lines {
		if got := visibleCols(ln); got > width {
			t.Errorf("line %d is %d cols, over the %d budget: %q", i, got, width, ln)
		}
	}
	joined := reWrapEsc.ReplaceAllString(strings.Join(lines, ""), "")
	if got := strings.Count(joined, "_"); got != len(rule) {
		t.Errorf("rule has %d underscores after wrapping, want %d", got, len(rule))
	}
	if got := strings.Count(joined, "\xdf"); got != 60 {
		t.Errorf("block row has %d blocks after wrapping, want 60", got)
	}
}
