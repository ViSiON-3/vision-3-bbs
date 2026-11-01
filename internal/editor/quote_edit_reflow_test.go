package editor

import (
	"strings"
	"testing"
)

// quotedParagraph sets up a buffer holding one quoted paragraph, wrapped by
// the quote picker's own wrapper and marked as the picker marks it, followed
// by a line of the caller's own text.
func quotedParagraph(t *testing.T, prefix, text string) (*MessageBuffer, *WordWrapper, int) {
	t.Helper()
	wrapped := wrapQuoted(prefix, text, MaxLineLength)
	if len(wrapped) < 3 {
		t.Fatalf("want at least 3 quoted lines, got %q", wrapped)
	}
	lines := append(append([]string{}, wrapped...), "my own reply")
	hard := []int{len(wrapped), len(lines)}
	mb, ww := setupBuffer(lines, hard...)
	for i := range wrapped {
		mb.SetQuotePrefix(i+1, prefix)
	}
	return mb, ww, len(wrapped)
}

// checkQuote requires lines 1..n to be a filled paragraph under prefix holding
// want, and the caller's own line to follow untouched.
func checkQuote(t *testing.T, mb *MessageBuffer, prefix, want string) {
	t.Helper()
	var words []string
	n := 0
	for i := 1; i <= mb.GetLineCount(); i++ {
		line := mb.GetLine(i)
		if !strings.HasPrefix(line, prefix) {
			break
		}
		n = i
		if len(line) > MaxLineLength {
			t.Errorf("line %d is %d columns: %q", i, len(line), line)
		}
		body := strings.Fields(line[len(prefix):])
		if i > 1 && len(body) > 0 && len(mb.GetLine(i-1))+1+len(body[0]) <= MaxLineLength {
			t.Errorf("line %d = %q leaves room for %q", i-1, mb.GetLine(i-1), body[0])
		}
		words = append(words, body...)
	}
	if got := strings.Join(words, " "); got != want {
		t.Errorf("quoted text:\n got %q\nwant %q", got, want)
	}
	if !mb.IsHardNewline(n) {
		t.Errorf("last quoted line %d lost its hard break", n)
	}
	if mb.GetLine(n+1) != "my own reply" || mb.QuotePrefix(n+1) != "" {
		t.Errorf("own text after the quote = %q (prefix %q)", mb.GetLine(n+1), mb.QuotePrefix(n+1))
	}
}

const quoteText = "The quick brown fox jumps over the lazy dog while the five boxing " +
	"wizards jump quickly and a wizard's job is to vex chumps quickly in fog, " +
	"which is more than anyone could reasonably expect of a fox."

// #866: typing into a quoted line rewraps the paragraph under its prefix.
func TestTypingInQuoteReflowsUnderPrefix(t *testing.T) {
	const prefix = " Bu> "
	mb, ww, _ := quotedParagraph(t, prefix, quoteText)
	line, col := 1, len(prefix)+5 // before "quick"
	for _, r := range "very " {
		mb.InsertChar(line, col, r)
		line, col = ww.WrapAfterInsert(line, col+1)
	}
	if line != 1 || col != len(prefix)+10 {
		t.Errorf("cursor = (%d,%d), want (1,%d) before \"quick\"", line, col, len(prefix)+10)
	}
	checkQuote(t, mb, prefix, strings.Replace(quoteText, "The ", "The very ", 1))
}

// Deleting from a quoted line pulls the words after it up, without their
// copies of the prefix.
func TestDeletingInQuotePullsWordsUp(t *testing.T) {
	const prefix = " Bu> "
	mb, ww, _ := quotedParagraph(t, prefix, quoteText)
	line, col := 1, len(prefix)+1
	for range "The quick brown " {
		line, col, _ = ww.HandleDelete(line, col)
	}
	checkQuote(t, mb, prefix, strings.TrimPrefix(quoteText, "The quick brown "))
	if line != 1 || col != len(prefix)+1 {
		t.Errorf("cursor = (%d,%d), want (1,%d)", line, col, len(prefix)+1)
	}
}

// Backspace at the start of a soft quoted line joins it to the line above as
// the editor does for any soft line, removing the space the wrap stood for,
// and the second line's copy of the prefix goes with it.
func TestBackspaceAtStartOfQuotedLineDropsItsPrefix(t *testing.T) {
	const prefix = " Bu> "
	mb, ww, _ := quotedParagraph(t, prefix, quoteText)
	a := strings.Fields(mb.GetLine(1))
	b := strings.Fields(mb.GetLine(2)[len(prefix):])
	join := a[len(a)-1] + " " + b[0]
	ww.HandleBackspace(2, 1)
	checkQuote(t, mb, prefix, strings.Replace(quoteText, join, strings.Replace(join, " ", "", 1), 1))
}

// The caller's own soft-wrapped paragraph directly above a quote never takes
// quoted text in, even with no hard break between them.
func TestOwnTextAboveQuoteStaysApart(t *testing.T) {
	const prefix = " Bu> "
	mb, ww := setupBuffer([]string{"mine", prefix + "quoted words here"}, 2)
	mb.SetQuotePrefix(2, prefix)
	ww.HandleBackspace(1, 5)
	checkBuffer(t, mb, []string{"min", prefix + "quoted words here"}, 2)
	ww.HandleBackspace(2, len(prefix)+2)
	checkBuffer(t, mb, []string{"min", prefix + "uoted words here"}, 2)
}

// Enter inside a quoted line, to reply between its lines, leaves the text
// after the cursor as the caller's own: it wraps without the prefix.
func TestEnterInsideQuoteMakesOwnText(t *testing.T) {
	const prefix = " Bu> "
	mb, _ := setupBuffer([]string{prefix + "quoted words here"}, 1)
	mb.SetQuotePrefix(1, prefix)
	mb.SplitLine(1, len(prefix)+8)
	if mb.QuotePrefix(1) != prefix || mb.QuotePrefix(2) != "" {
		t.Errorf("prefixes after split: %q %q", mb.QuotePrefix(1), mb.QuotePrefix(2))
	}
	// A caller who deletes the prefix itself turns the line into their own.
	mb.DeleteChar(1, 3)
	if mb.QuotePrefix(1) != "" {
		t.Errorf("line with its prefix edited away still reports %q", mb.QuotePrefix(1))
	}
}

// End to end: the picker writes a paragraph's wraps soft under its prefix, and
// the banners stay hard lines of their own.
func TestQuotePickerWritesSoftWrapsUnderPrefix(t *testing.T) {
	src := wrapSource(reflowText, 78)
	_, ch, _, cleanup := newQuoteHarness(t, "", src)
	defer cleanup()
	qs := newQuoteSession(ch, src, 1)
	for range src {
		qs.quoteSelected()
	}
	mb := ch.buffer
	n := mb.GetLineCount()
	if n < 4 {
		t.Fatalf("buffer has %d lines", n)
	}
	if !mb.IsHardNewline(1) || mb.QuotePrefix(1) != "" {
		t.Error("Said banner should be a hard line of its own")
	}
	last := 0
	for i := 2; i <= n && mb.QuotePrefix(i) != ""; i++ {
		last = i
	}
	for i := 2; i < last; i++ {
		if mb.IsHardNewline(i) {
			t.Errorf("quoted line %d is hard; wraps inside a paragraph should be soft", i)
		}
	}
	if !mb.IsHardNewline(last) {
		t.Errorf("last quoted line %d should end the paragraph with a hard break", last)
	}
	// Reformat (Ctrl-B) over the quote changes nothing: it is already filled.
	before := mb.GetContent()
	ww := NewWordWrapper(mb)
	ww.ReformatParagraph(2)
	if mb.GetContent() != before {
		t.Errorf("reformatting the quote changed it:\n%s\nwant\n%s", mb.GetContent(), before)
	}
}

// Ctrl-J joins two lines of the same quote as words: the second line's prefix
// goes, and a space goes between the words either side of the join.
func TestCtrlJJoinsQuotedLinesWithASpace(t *testing.T) {
	const prefix = " Bu> "
	mb, _ := setupBuffer([]string{prefix + "one", prefix + "two"}, 1, 2)
	mb.SetQuotePrefix(1, prefix)
	mb.SetQuotePrefix(2, prefix)
	e := &FSEditor{buffer: mb, wordWrapper: NewWordWrapper(mb), currentLine: 1, currentCol: 1}
	e.joinLines()
	if got := mb.GetLine(1); got != prefix+"one two" {
		t.Errorf("joined line = %q, want %q", got, prefix+"one two")
	}
}

// A word too long for the line beside the prefix, such as a URL, is split by
// the quote picker with no space; the break is marked so a later edit rejoins
// it whole.
func TestQuotePickerMarksSplitLongWords(t *testing.T) {
	url := "http://example.com/" + strings.Repeat("x", 150)
	src := []string{"see " + url + " there"}
	_, ch, _, cleanup := newQuoteHarness(t, "", src)
	defer cleanup()
	qs := newQuoteSession(ch, src, 1)
	qs.quoteSelected()
	mb := ch.buffer
	ww := NewWordWrapper(mb)
	// Reformat the quote (line 2, below the Said banner): a split marked as a
	// plain wrap would get a space written into the URL.
	ww.ReformatParagraph(2)
	var text strings.Builder
	for i := 2; i <= mb.GetLineCount() && mb.QuotePrefix(i) != ""; i++ {
		text.WriteString(mb.GetLine(i)[len(mb.QuotePrefix(i)):])
		if !mb.IsWordSplit(i) {
			text.WriteString(" ")
		}
	}
	if !strings.Contains(text.String(), url) {
		t.Errorf("the URL did not survive whole:\n%s", mb.GetContent())
	}
}
