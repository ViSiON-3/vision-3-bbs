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
