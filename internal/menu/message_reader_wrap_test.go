package menu

import (
	"strings"
	"testing"

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
// paints it: format, expand pipe codes, wrap to the terminal width.
func readBodyForTest(body string, termWidth int) []string {
	formatted := formatMessageBody(body, "", false)
	return wrapAnsiString(string(ansi.ReplacePipeCodes([]byte(formatted))), termWidth)
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
