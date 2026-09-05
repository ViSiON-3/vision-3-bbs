package menu

import (
	"regexp"
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
)

var reWrapEsc = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

// visibleCols is the on-screen width of an already-pipe-converted line.
func visibleCols(s string) int { return len(reWrapEsc.ReplaceAllString(s, "")) }

// wrapNewsBodyForTest mirrors what displayNewsItem does to a body.
func wrapNewsBodyForTest(body string, termWidth int) []string {
	converted := ansi.ReplacePipeCodes([]byte(normalizeNewsBody(body)))
	return wrapAnsiString(string(converted), newsBodyWidth(termWidth))
}

func TestNewsBodyWidthReservesAColumn(t *testing.T) {
	cases := []struct{ term, want int }{
		{80, 79}, // the common case: one column short of the margin
		{132, 131},
		{40, 39},
		{0, 79},  // width unknown -> assume 80 columns, still wrap
		{1, 79},  // degenerate
		{-5, 79}, // degenerate
	}
	for _, c := range cases {
		if got := newsBodyWidth(c.term); got != c.want {
			t.Errorf("newsBodyWidth(%d) = %d, want %d", c.term, got, c.want)
		}
	}
}

func TestNormalizeNewsBodyStripsCR(t *testing.T) {
	got := normalizeNewsBody("one\r\ntwo\r\nthree")
	if strings.Contains(got, "\r") {
		t.Errorf("carriage returns survived normalization: %q", got)
	}
	if want := "one\ntwo\nthree"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// The reported bug: a long paragraph was written out raw, so the terminal
// hard-wrapped it at its own margin and split words down the middle.
func TestNewsBodyWrapsOnWordBoundaries(t *testing.T) {
	body := "The quick brown fox jumps over the lazy dog while the " +
		"enterprising aardvark contemplates its considerable misfortune " +
		"in the general vicinity of the woodpile."

	width := newsBodyWidth(80)
	lines := wrapAnsiString(normalizeNewsBody(body), width)

	if len(lines) < 2 {
		t.Fatalf("expected the paragraph to wrap onto several lines, got %d", len(lines))
	}
	for i, ln := range lines {
		if len(ln) > width {
			t.Errorf("line %d is %d cols, over the %d budget: %q", i, len(ln), width, ln)
		}
	}

	// Nothing may be lost or invented: the words must round-trip exactly.
	if got, want := strings.Join(strings.Fields(strings.Join(lines, " ")), " "),
		strings.Join(strings.Fields(body), " "); got != want {
		t.Errorf("wrapping altered the text:\n got  %q\n want %q", got, want)
	}

	// And no line may start or end mid-word, i.e. every line is whole words.
	for i, ln := range lines {
		for _, w := range strings.Fields(ln) {
			if !strings.Contains(body, w) {
				t.Errorf("line %d contains %q, which is not a word from the body", i, w)
			}
		}
	}
}

// Explicit newlines the sysop typed are paragraph breaks and must survive.
func TestNewsBodyPreservesAuthoredLineBreaks(t *testing.T) {
	body := "First paragraph.\n\nSecond paragraph."
	lines := wrapAnsiString(normalizeNewsBody(body), newsBodyWidth(80))

	if len(lines) != 3 {
		t.Fatalf("expected 3 lines (para, blank, para), got %d: %q", len(lines), lines)
	}
	if lines[1] != "" {
		t.Errorf("blank separator line was lost: %q", lines)
	}
}

// A sysop who pastes ANSI art into an item must not have it reflowed, since
// art relies on absolute positioning.
func TestNewsBodyLeavesAnsiArtAlone(t *testing.T) {
	art := "\x1b[5;10Hsome art\n\x1b[6;10Hmore art"
	lines := wrapAnsiString(normalizeNewsBody(art), newsBodyWidth(80))
	if len(lines) != 2 {
		t.Fatalf("art should split on newlines only, got %d lines: %q", len(lines), lines)
	}
}

// News bodies support pipe colour codes (wv runs them through
// ReplacePipeCodes). Wrapping must therefore measure *visible* columns:
// converting after wrapping would count "|04" as three columns and break
// lines far too early.
func TestNewsBodyWrapsOnVisibleWidthNotPipeCodeBytes(t *testing.T) {
	// 12 coloured words: 95 raw bytes but only 59 visible columns, so it fits
	// on one 79-column line and must not be split.
	words := make([]string, 0, 12)
	for i := 0; i < 12; i++ {
		words = append(words, "|1"+string(rune('0'+i%6))+"word")
	}
	body := strings.Join(words, " ")

	if len(body) <= newsBodyWidth(80) {
		t.Fatalf("test body is only %d raw bytes; it must exceed the budget to be meaningful", len(body))
	}

	lines := wrapNewsBodyForTest(body, 80)
	if len(lines) != 1 {
		t.Errorf("body is %d visible columns and should fit on one line, got %d lines",
			visibleCols(string(ansi.ReplacePipeCodes([]byte(body)))), len(lines))
	}
	for i, ln := range lines {
		if got := visibleCols(ln); got > newsBodyWidth(80) {
			t.Errorf("line %d is %d visible columns, over the %d budget", i, got, newsBodyWidth(80))
		}
	}
}

// Colour codes must survive wrapping, not be stripped or mangled.
func TestNewsBodyWrappingPreservesColour(t *testing.T) {
	body := "|10Hello |12world"
	lines := wrapNewsBodyForTest(body, 80)
	joined := strings.Join(lines, "")

	if strings.Contains(joined, "|10") || strings.Contains(joined, "|12") {
		t.Errorf("pipe codes were not converted to ANSI: %q", joined)
	}
	if !strings.Contains(joined, "\x1b[") {
		t.Errorf("no ANSI escape in output; colour was lost: %q", joined)
	}
	if !strings.Contains(joined, "Hello") || !strings.Contains(joined, "world") {
		t.Errorf("text lost during wrapping: %q", joined)
	}
}

// A long coloured paragraph still wraps, and every line stays within the
// visible budget.
func TestNewsBodyLongColouredParagraphWrapsWithinBudget(t *testing.T) {
	body := strings.TrimSpace(strings.Repeat("|15colourful |07prose about aardvarks ", 12))
	width := newsBodyWidth(80)

	lines := wrapNewsBodyForTest(body, 80)
	if len(lines) < 3 {
		t.Fatalf("expected the paragraph to wrap onto several lines, got %d", len(lines))
	}
	for i, ln := range lines {
		if got := visibleCols(ln); got > width {
			t.Errorf("line %d is %d visible columns, over the %d budget: %q", i, got, width, ln)
		}
	}
}

// wrapAnsiString breaks on spaces only, so a token with no break opportunity
// comes back oversized and the client terminal chops it — the exact mid-word
// splitting this wrapping exists to prevent. Every emitted line must fit.
func TestNewsBodyBreaksOversizedTokens(t *testing.T) {
	url := "https://example.com/downloads/" + strings.Repeat("abcdefghij", 10) + "/file.zip"
	if len(url) <= newsBodyWidth(80) {
		t.Fatalf("test URL is only %d cols; it must exceed the budget", len(url))
	}
	body := "Grab it from " + url + " today."
	width := newsBodyWidth(80)

	lines := breakOversizedLines(wrapNewsBodyForTest(body, 80), width)

	for i, ln := range lines {
		if got := visibleCols(ln); got > width {
			t.Errorf("line %d is %d visible columns, over the %d budget: %q", i, got, width, ln)
		}
	}

	// The URL must survive intact once the line breaks are removed.
	if joined := strings.Join(lines, ""); !strings.Contains(strings.ReplaceAll(joined, " ", ""),
		strings.ReplaceAll(url, " ", "")) {
		t.Errorf("URL was corrupted by breaking; not found in reassembled output")
	}
}

func TestBreakOversizedLinesKeepsShortLinesUntouched(t *testing.T) {
	in := []string{"short", "also short", ""}
	got := breakOversizedLines(in, 79)
	if len(got) != len(in) {
		t.Fatalf("short lines were altered: %q", got)
	}
	for i := range in {
		if got[i] != in[i] {
			t.Errorf("line %d changed from %q to %q", i, in[i], got[i])
		}
	}
}

// Breaking must never split an ANSI escape sequence, and escapes must not
// count toward the column budget.
func TestBreakOversizedLinesIsAnsiAware(t *testing.T) {
	// 100 visible columns, with a colour change every 10.
	var b strings.Builder
	for i := 0; i < 10; i++ {
		b.WriteString("\x1b[1;3" + string(rune('0'+i%8)) + "m")
		b.WriteString(strings.Repeat("x", 10))
	}
	line := b.String()
	if visibleCols(line) != 100 {
		t.Fatalf("fixture is %d visible cols, expected 100", visibleCols(line))
	}

	got := breakOversizedLines([]string{line}, 40)

	for i, ln := range got {
		if w := visibleCols(ln); w > 40 {
			t.Errorf("chunk %d is %d visible columns, over 40", i, w)
		}
		// A split escape would leave a bare ESC or an unterminated CSI.
		if strings.Contains(ln, "\x1b") && !reWrapEsc.MatchString(ln) {
			t.Errorf("chunk %d contains a broken escape sequence: %q", i, ln)
		}
	}

	// No visible character may be lost.
	total := 0
	for _, ln := range got {
		total += visibleCols(ln)
	}
	if total != 100 {
		t.Errorf("visible columns after breaking = %d, want 100", total)
	}
}

func TestBreakOversizedLinesInvalidWidthIsANoOp(t *testing.T) {
	in := []string{strings.Repeat("x", 200)}
	for _, w := range []int{0, -1} {
		got := breakOversizedLines(in, w)
		if len(got) != 1 || got[0] != in[0] {
			t.Errorf("width %d should be a no-op, got %q", w, got)
		}
	}
}

// Art rows are positioned absolutely, so hard-breaking a full-width row would
// push everything below it down a line. displayNewsItem skips the break pass
// for art; this pins the detection that gate relies on.
func TestNewsBodyArtIsExemptFromHardBreaking(t *testing.T) {
	art := "\x1b[5;1H" + strings.Repeat("\xB0", 80) + "\n\x1b[6;1H" + strings.Repeat("\xB1", 80)

	if !containsAnsiArt(art) {
		t.Fatal("fixture is not detected as ANSI art; the exemption would not apply")
	}

	lines := wrapAnsiString(art, newsBodyWidth(80))
	if len(lines) != 2 {
		t.Fatalf("art should stay 2 rows, got %d", len(lines))
	}
	// Demonstrates why the gate exists: breaking would double the row count.
	if broken := breakOversizedLines(lines, newsBodyWidth(80)); len(broken) == len(lines) {
		t.Skip("break pass no longer splits these rows; gate may be redundant")
	}
}
