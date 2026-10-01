package editor

import (
	"strings"
	"testing"
)

// reflowText is one paragraph long enough to wrap over several lines.
const reflowText = "The quick brown fox jumps over the lazy dog while the sysop watches " +
	"the nodelist compile overnight, and by morning every echo has tossed and " +
	"every netmail has gone out, which is about as much as anyone could ask " +
	"of a Tuesday on a hobby network run from a closet."

// wrapSource wraps text the way the original author's editor would have.
func wrapSource(text string, width int) []string {
	return wrapQuoted("", text, width)
}

func TestSplitQuotePrefix(t *testing.T) {
	for _, tc := range []struct{ line, prefix, body string }{
		{"plain text", "", "plain text"},
		{" Sh> I'd be interested", "Sh> ", "I'd be interested"},
		{"Sh>> deeper", "Sh>> ", "deeper"},
		{" Bu> Sh> two levels", "Bu> Sh> ", "two levels"},
		{"> usenet style", "> ", "usenet style"},
		{" Sh>", "Sh> ", ""},
		{"a>b is not a quote", "", "a>b is not a quote"},
		{"Sh>   indented body", "Sh> ", "indented body"},
	} {
		prefix, body := splitQuotePrefix(tc.line)
		if prefix != tc.prefix || body != tc.body {
			t.Errorf("splitQuotePrefix(%q) = (%q, %q), want (%q, %q)", tc.line, prefix, body, tc.prefix, tc.body)
		}
	}
}

func TestContinuesParagraph(t *testing.T) {
	full := "The quick brown fox jumps over the lazy dog while the sysop watches the" // 72
	const width = 76
	for _, tc := range []struct {
		name, prev, next string
		width            int
		want             bool
	}{
		{"full line runs on", full, "nodelist compile overnight", width, true},
		{"short line ends a paragraph", "Hi all,", "Here is the thing.", width, false},
		{"room left for the next word", full, "and so on", width, false},
		{"blank next line", full, "", width, false},
		{"blank prev line", "", "text", width, false},
		{"quote depth changes", full, "Sh> nodelist compile", width, false},
		{"same quote depth runs on", "Sh> " + full, "Sh> nodelist compile", width + 4, true},
		{"indented next line", full, "  nodelist compile", width, false},
		{"bullet item", full, "- nodelist compile", width, false},
		{"numbered item", full, "2. nodelist compile", width, false},
		{"narrow source is never joined", full[:30], "nodelist", 30, false},
		{"unwrapped long line stands alone", full + full, "nodelist", width, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := continuesParagraph(tc.prev, tc.next, tc.width); got != tc.want {
				t.Errorf("continuesParagraph(%q, %q, %d) = %v, want %v", tc.prev, tc.next, tc.width, got, tc.want)
			}
		})
	}
}

func TestQuoteSourceWidthIgnoresUnwrappedLines(t *testing.T) {
	lines := []string{"short", strings.Repeat("x", 76), strings.Repeat("y", 300)}
	if got := quoteSourceWidth(lines); got != 76 {
		t.Errorf("quoteSourceWidth() = %d, want 76", got)
	}
}

// A nested quote keeps its own prefix on every line it wraps to, rather than
// the inner prefix riding along as the first word of the first line only.
func TestReflowQuotedKeepsNestedPrefix(t *testing.T) {
	src := wrapQuoted(" Sh> ", reflowText, MaxLineLength)
	got := reflowQuoted("Bu> ", src, MaxLineLength)
	assertFilledParagraph(t, got, "Bu> Sh> ", reflowText)
}

// assertFilledParagraph checks that lines are one paragraph of want under
// prefix, filled greedily: no line could have taken the next line's first word.
func assertFilledParagraph(t *testing.T, lines []string, prefix, want string) {
	t.Helper()
	var words []string
	for i, line := range lines {
		if !strings.HasPrefix(line, prefix) {
			t.Fatalf("line %d = %q, want prefix %q", i, line, prefix)
		}
		if runeLen(line) > MaxLineLength {
			t.Errorf("line %d is %d cells wide, want <= %d: %q", i, runeLen(line), MaxLineLength, line)
		}
		body := strings.Fields(strings.TrimPrefix(line, prefix))
		if i > 0 {
			if prev := lines[i-1]; runeLen(prev)+1+runeLen(body[0]) <= MaxLineLength {
				t.Errorf("line %d = %q leaves room for %q — the paragraph is not reflowed", i-1, prev, body[0])
			}
		}
		words = append(words, body...)
	}
	if got := strings.Join(words, " "); got != want {
		t.Errorf("paragraph text:\n got %q\nwant %q", got, want)
	}
}

// quotedBlock returns the quoted lines between the Said and Done banners.
func quotedBlock(t *testing.T, ch *CommandHandler) []string {
	t.Helper()
	lines := strings.Split(stripANSI(ch.buffer.GetContent()), "\n")
	if len(lines) < 2 || lines[0] != "--- Bucko Said ---" {
		t.Fatalf("buffer = %q, want it to open with the Said banner", lines)
	}
	for i, line := range lines {
		if line == "--- Bucko Done ---" {
			return lines[1:i]
		}
	}
	t.Fatalf("buffer = %q, want a Done banner", lines)
	return nil
}

// The issue (#551): source lines wrapped near full width each grew past the
// limit once prefixed, and every one of them left a one or two word stub on a
// line of its own. Quoting the lines in a row reflows them as one paragraph.
func TestQuoteModeReflowsAParagraph(t *testing.T) {
	src := wrapSource(reflowText, 78)
	src = append(src, "", "Second paragraph here.")
	_, ch, _, cleanup := newQuoteHarness(t, "", src)
	defer cleanup()

	qs := newQuoteSession(ch, src, 1)
	for range src {
		qs.quoteSelected()
	}

	got := quotedBlock(t, ch)
	n := len(got)
	if n < 3 {
		t.Fatalf("quote block = %q, want the paragraph, a blank line and the second paragraph", got)
	}
	assertFilledParagraph(t, got[:n-2], "Bu> ", reflowText)
	if got[n-2] != "Bu>" || got[n-1] != "Bu> Second paragraph here." {
		t.Errorf("block ends %q, want the blank line and second paragraph kept apart", got[n-2:])
	}
}

// Skipping a source line means the next one quoted starts a paragraph of its
// own: text that was left out is not papered over by joining across it.
func TestQuoteModeDoesNotJoinAcrossSkippedLines(t *testing.T) {
	src := wrapSource(reflowText, 78)
	_, ch, _, cleanup := newQuoteHarness(t, "", src)
	defer cleanup()

	qs := newQuoteSession(ch, src, 1)
	qs.quoteSelected() // line 1
	qs.moveTo(2)
	qs.quoteSelected() // line 3, line 2 skipped

	got := strings.Join(quotedBlock(t, ch), "\n")
	want := strings.Join(append(wrapQuoted("Bu> ", src[0], MaxLineLength), wrapQuoted("Bu> ", src[2], MaxLineLength)...), "\n")
	if got != want {
		t.Errorf("quote block:\n%s\nwant each line wrapped on its own:\n%s", got, want)
	}
}

// Backspace takes the last line back out of the paragraph it joined, and the
// paragraph returns to exactly how it read before.
func TestQuoteModeUndoUnjoinsTheLastLine(t *testing.T) {
	src := wrapSource(reflowText, 78)
	_, ch, _, cleanup := newQuoteHarness(t, "", src)
	defer cleanup()
	ch.buffer.LoadContent("draft")

	qs := newQuoteSession(ch, src, 1)
	qs.quoteSelected()
	qs.quoteSelected()
	twoLines := ch.buffer.GetContent()
	qs.quoteSelected()
	qs.undoLast()

	if got := ch.buffer.GetContent(); got != twoLines {
		t.Errorf("after undo:\n%s\nwant:\n%s", got, twoLines)
	}
	if qs.quoted[2] != 0 || qs.sel != 2 {
		t.Errorf("quoted[2] = %d, sel = %d; want 0 and the bar back on line 3", qs.quoted[2], qs.sel)
	}
	assertFilledParagraph(t, quotedBlock(t, ch), "Bu> ", src[0]+" "+src[1])

	qs.undoLast()
	qs.undoLast()
	if got := stripANSI(ch.buffer.GetContent()); got != "draft" {
		t.Errorf("buffer = %q after undoing everything, want %q", got, "draft")
	}
}

// When the message fills up partway through a joined paragraph, what fits is
// kept, the notice shows, and the next line does not join the cut-short text.
func TestQuoteModeReflowNearTheLineLimit(t *testing.T) {
	src := wrapSource(reflowText, 78)
	tt, ch, _, cleanup := newQuoteHarness(t, "", src)
	defer cleanup()
	// Room for the banners and one quoted line.
	fillBuffer(ch.buffer, MaxLines-3)

	qs := newQuoteSession(ch, src, 1)
	qs.quoteSelected() // the first source line fits on one line
	if ch.buffer.GetLineCount() != MaxLines {
		t.Fatalf("message is %d lines, want it full", ch.buffer.GetLineCount())
	}
	qs.quoteSelected() // joins, rewraps to two lines, only one fits

	para := qs.paras[len(qs.paras)-1]
	if !para.closed || len(para.src) != 2 {
		t.Errorf("paragraph = %+v, want both lines in it and closed", para)
	}
	if got := tt.Row(ch.screen.PromptRow()); !strings.Contains(got, "Message is full") {
		t.Errorf("prompt row = %q, want the message-full notice", got)
	}

	// Undo pulls the second line out and the first is whole again.
	qs.undoLast()
	got := quotedBlock(t, ch)
	assertFilledParagraph(t, got, "Bu> ", src[0])
	if qs.paras[0].closed {
		t.Error("paragraph is still closed after undo, though it fits again")
	}
}
