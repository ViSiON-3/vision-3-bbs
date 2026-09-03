package editor

import (
	"strings"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/editor/testterm"
)

func TestQuoteInitials(t *testing.T) {
	cases := []struct{ name, want string }{
		{"Shurato", "Sh"},
		{"Bucko", "Bu"},
		{"John Smith", "JS"},
		{"  spaced   out  ", "SO"},
		{"X", "X"},
		{"", ""},
	}
	for _, c := range cases {
		if got := quoteInitials(c.name); got != c.want {
			t.Errorf("quoteInitials(%q) = %q, want %q", c.name, got, c.want)
		}
	}
}

// The old quoter byte-sliced at 79 and silently dropped the tail of a long line.
func TestWrapQuotedWrapsInsteadOfTruncating(t *testing.T) {
	long := strings.Repeat("word ", 40)
	got := wrapQuoted("Bu> ", long, MaxLineLength)

	if len(got) < 3 {
		t.Fatalf("expected the line to wrap over several rows, got %d: %q", len(got), got)
	}
	var rebuilt []string
	for i, line := range got {
		if !strings.HasPrefix(line, "Bu> ") {
			t.Errorf("line %d = %q, want the quote prefix on every continuation line", i, line)
		}
		if runeLen(line) > MaxLineLength {
			t.Errorf("line %d is %d cells wide, want <= %d: %q", i, runeLen(line), MaxLineLength, line)
		}
		rebuilt = append(rebuilt, strings.TrimPrefix(line, "Bu> "))
	}
	if want := strings.TrimSpace(long); strings.Join(rebuilt, " ") != want {
		t.Errorf("wrapping lost text:\n got %q\nwant %q", strings.Join(rebuilt, " "), want)
	}
}

func TestWrapQuotedBlankAndOversizedWords(t *testing.T) {
	if got := wrapQuoted("Bu> ", "   ", MaxLineLength); len(got) != 1 || got[0] != "Bu>" {
		t.Errorf("blank source line = %q, want [\"Bu>\"] — a blank quoted line keeps the bare prefix", got)
	}

	// A single word longer than the line has to be split; it must not vanish.
	huge := strings.Repeat("x", 200)
	got := wrapQuoted("Bu> ", huge, MaxLineLength)
	var joined string
	for _, line := range got {
		if runeLen(line) > MaxLineLength {
			t.Errorf("line %q is %d cells wide, want <= %d", line, runeLen(line), MaxLineLength)
		}
		joined += strings.TrimPrefix(line, "Bu> ")
	}
	if joined != huge {
		t.Errorf("split word lost characters: got %d chars, want %d", len(joined), len(huge))
	}
}

// Nesting must not accumulate a space per reply: the margin a previous quoter
// left on the source line is dropped rather than carried through.
func TestWrapQuotedCollapsesSourceIndent(t *testing.T) {
	got := wrapQuoted("Bu> ", " Sh> I'd be interested in this as well", MaxLineLength)
	want := "Bu> Sh> I'd be interested in this as well"
	if len(got) != 1 || got[0] != want {
		t.Errorf("wrapQuoted() = %q, want [%q]", got, want)
	}
}

// An oversized word must not be welded onto the words already on the line:
// "see <200-char-url>" used to wrap as "Bu> seehttps://...".
func TestWrapQuotedOversizedWordAfterText(t *testing.T) {
	url := "https://" + strings.Repeat("a", 200)
	got := wrapQuoted("Bu> ", "see "+url+" ok", MaxLineLength)

	if got[0] != "Bu> see" {
		t.Errorf("line 0 = %q, want %q — the long word should start a new line", got[0], "Bu> see")
	}
	var payloads []string
	for i, line := range got {
		if runeLen(line) > MaxLineLength {
			t.Errorf("line %d is %d cells wide, want <= %d", i, runeLen(line), MaxLineLength)
		}
		payloads = append(payloads, strings.TrimPrefix(line, "Bu> "))
	}
	// The long word's chunks rejoin into it exactly, and the text after it
	// rides along on the last chunk's line rather than being dropped.
	if joined := strings.Join(payloads[1:], ""); joined != url+" ok" {
		t.Errorf("rejoined long word = %q, want %q", joined, url+" ok")
	}
}

// Multibyte text must not be cut mid-rune the way the old byte slicing did.
func TestWrapQuotedIsRuneSafe(t *testing.T) {
	text := strings.Repeat("héllo wörld ", 10)
	for _, line := range wrapQuoted("Bü> ", text, MaxLineLength) {
		if strings.ContainsRune(line, '�') {
			t.Errorf("line %q contains a replacement rune — a multibyte character was split", line)
		}
	}
}

func TestPrepareQuoteSourceDropsFTNTrailer(t *testing.T) {
	body := []string{
		"",
		"On 28 Aug 2026, Shurato said the following...",
		" Sh> I'd be interested in this as well",
		"",
		"I haven't had much luck with it.",
		"",
		"... A book misplaced is a book lost",
		"--- Mystic BBS v1.12 A48 (Linux/64)",
		" * Origin: The Wrong Number Family Of BBS' (21:4/131)",
		"SEEN-BY: 4/131 4/0",
		"\x01PATH: 4/131",
	}

	got := prepareQuoteSource(body)
	want := []string{
		"On 28 Aug 2026, Shurato said the following...",
		" Sh> I'd be interested in this as well",
		"",
		"I haven't had much luck with it.",
	}
	if len(got) != len(want) {
		t.Fatalf("prepareQuoteSource() = %q\nwant %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("line %d = %q, want %q", i, got[i], want[i])
		}
	}
}

// A body that is nothing but a trailer must not leave the picker with an empty
// list — HandleQuote falls back to the raw lines in that case.
func TestPrepareQuoteSourceAllTrailer(t *testing.T) {
	if got := prepareQuoteSource([]string{"--- Mystic", " * Origin: x"}); len(got) != 0 {
		t.Errorf("prepareQuoteSource() = %q, want empty", got)
	}
}

func TestStripANSI(t *testing.T) {
	if got := stripANSI("\x1b[1;31mred\x1b[0m text"); got != "red text" {
		t.Errorf("stripANSI() = %q, want %q", got, "red text")
	}
	if got := stripANSI("plain"); got != "plain" {
		t.Errorf("stripANSI() = %q, want %q", got, "plain")
	}
}

// newQuoteHarness wires a CommandHandler to a fake terminal and a scripted
// keyboard, which is all HandleQuote needs.
func newQuoteHarness(t *testing.T, keys string, lines []string) (*Term, *CommandHandler, *InputHandler, func()) {
	t.Helper()
	tt := testterm.New(80, 24)
	screen := NewScreen(tt, ansi.OutputModeUTF8, 80, 24)
	buffer := NewMessageBuffer()
	ch := NewCommandHandler(screen, buffer, "", "", "", "", "", "")
	ch.SetQuoteData(&QuoteData{From: "Bucko", Title: "Re: startup.mps", Lines: lines})

	sess := testterm.NewSession(nil, keys)
	ih := NewInputHandler(sess)
	ih.SetEscTimeout(10 * time.Millisecond) // don't wait out the real ESC window
	return tt, ch, ih, func() { ih.CloseAndWait() }
}

// Term is an alias so the harness signature stays readable.
type Term = testterm.Term

var quoteBody = []string{
	"On 28 Aug 2026, Shurato said the following...",
	" Sh> I'd be interested in this as well",
	"I haven't had much luck with it.",
	"--- Mystic BBS v1.12",
	" * Origin: The Wrong Number (21:4/131)",
}

// The bug this whole change exists for: the old quoter printed the numbered
// source list with \r\n into the editing area, scrolling the header off the top
// and the footer off the bottom. Quote mode must stay inside the editing rows.
func TestQuoteModeLeavesHeaderAndFooterIntact(t *testing.T) {
	// ' ' quotes the first line, ESC leaves quote mode.
	tt, ch, ih, cleanup := newQuoteHarness(t, " \x1b", quoteBody)
	defer cleanup()

	// Paint sentinels in the rows the editor does not own.
	ch.screen.GoXY(1, 1)
	ch.screen.WriteDirect("HEADER-ROW-1")
	ch.screen.GoXY(1, 24)
	ch.screen.WriteDirect("FOOTER-ROW-24")

	ch.HandleQuote(ih, 1, 1)

	if got := tt.Row(1); !strings.Contains(got, "HEADER-ROW-1") {
		t.Errorf("Row(1) = %q, want the header still there — quote mode scrolled the screen", got)
	}
	if got := tt.Row(24); !strings.Contains(got, "FOOTER-ROW-24") {
		t.Errorf("Row(24) = %q, want the footer still there — quote mode scrolled the screen", got)
	}
}

func TestQuoteModeDrawsSplitPanes(t *testing.T) {
	tt, ch, ih, cleanup := newQuoteHarness(t, "\x1b", quoteBody)
	defer cleanup()

	ch.HandleQuote(ih, 1, 1)

	// 24 rows, header 6, status 1 => 17 editing rows: compose 7-14, divider 15,
	// source pane 16-23.
	if got := tt.Row(15); !strings.Contains(got, "Quoting Bucko") {
		t.Errorf("Row(15) = %q, want the divider naming who is being quoted", got)
	}
	if got := tt.Row(15); !strings.Contains(got, "SPACE Add") || !strings.Contains(got, "ESC Done") {
		t.Errorf("Row(15) = %q, want the key legend on the divider", got)
	}
	if got := tt.Row(16); !strings.Contains(got, "On 28 Aug 2026") {
		t.Errorf("Row(16) = %q, want the first source line in the pane", got)
	}
	// The FTN trailer is filtered out, so the pane holds exactly three lines.
	if got := tt.Row(19); strings.TrimSpace(got) != "" {
		t.Errorf("Row(19) = %q, want blank — the tearline and origin should be filtered out", got)
	}
	// Lightbar sits on the first source line: bright white on blue, and it runs
	// the width of the pane rather than stopping at the end of the text.
	for _, col := range []int{6, 79} {
		if c := tt.Cell(16, col); c.Bg != 44 {
			t.Errorf("Cell(16,%d).Bg = %d, want 44 (blue) — the lightbar does not span the row", col, c.Bg)
		}
	}
	// Column 80 is deliberately never written: touching it autowraps some clients.
	if c := tt.Cell(16, 80); c.Bg == 44 {
		t.Error("Cell(16,80) is painted — the last column must be left alone")
	}
}

func TestQuoteModeInsertsSelectedLinesWithBannerAndPrefix(t *testing.T) {
	// Quote lines 1 and 2, skip line 3 (down past it), then leave.
	_, ch, ih, cleanup := newQuoteHarness(t, "  \x1b", quoteBody)
	defer cleanup()

	line, col := ch.HandleQuote(ih, 1, 1)

	got := stripANSI(ch.buffer.GetContent())
	want := []string{
		"--- Bucko Said ---",
		"Bu> On 28 Aug 2026, Shurato said the following...",
		"Bu> Sh> I'd be interested in this as well", // the source line's own indent is dropped
		"--- Bucko Done ---",
	}
	gotLines := strings.Split(got, "\n")
	if len(gotLines) < len(want) {
		t.Fatalf("buffer = %q, want at least %d lines", got, len(want))
	}
	for i, w := range want {
		if gotLines[i] != w {
			t.Errorf("line %d = %q, want %q", i+1, gotLines[i], w)
		}
	}
	if line != 5 || col != 1 {
		t.Errorf("cursor = (%d,%d), want (5,1) — just past the quote block", line, col)
	}
}

// Quoting must never overwrite text the user has already typed: the old code
// used SetLine and clobbered whatever was on the cursor line.
func TestQuoteModeDoesNotOverwriteExistingText(t *testing.T) {
	_, ch, ih, cleanup := newQuoteHarness(t, " \x1b", quoteBody)
	defer cleanup()

	ch.buffer.LoadContent("my own words")
	ch.HandleQuote(ih, 1, 1)

	if got := stripANSI(ch.buffer.GetContent()); !strings.Contains(got, "my own words") {
		t.Errorf("buffer = %q, want the user's existing text preserved", got)
	}
}

func TestQuoteModeBackspaceUndoesTheBlock(t *testing.T) {
	// Add two lines, undo both, leave. The banners go with the last one.
	_, ch, ih, cleanup := newQuoteHarness(t, "  \x08\x08\x1b", quoteBody)
	defer cleanup()

	ch.buffer.LoadContent("draft")
	line, _ := ch.HandleQuote(ih, 1, 1)

	if got := stripANSI(ch.buffer.GetContent()); got != "draft" {
		t.Errorf("buffer = %q, want %q — undoing every quoted line should remove the banners too", got, "draft")
	}
	if line != 1 {
		t.Errorf("cursor line = %d, want 1 — no block left to sit after", line)
	}
}

// Quoting the same source line twice then undoing once must leave it marked as
// still quoted — a copy of it is still in the reply.
func TestQuoteModeDimmingTracksRepeatedQuotes(t *testing.T) {
	_, ch, _, cleanup := newQuoteHarness(t, "", quoteBody)
	defer cleanup()

	// Driving the session directly keeps the key script out of the assertion.
	qs := &quoteSession{ch: ch, src: quoteBody[:3], quoted: make([]int, 3), prefix: "Bu> ", origLine: 1}
	qs.layout()
	qs.quoteSelected() // quotes line 1, bar steps to line 2
	qs.moveTo(0)
	qs.quoteSelected() // quotes line 1 a second time
	qs.undoLast()

	if qs.quoted[0] != 1 {
		t.Errorf("quoted[0] = %d, want 1 — one copy is still in the reply", qs.quoted[0])
	}
	body := stripANSI(ch.buffer.GetContent())
	if n := strings.Count(body, "Bu> On 28 Aug 2026"); n != 1 {
		t.Errorf("reply contains %d copies of the line, want 1:\n%s", n, body)
	}
}

// A message whose whole body is an FTN trailer has nothing quotable in it. The
// picker must not open over an empty list, and must not offer the trailer.
func TestQuoteModeWithOnlyTrailerShowsNotice(t *testing.T) {
	tt, ch, ih, cleanup := newQuoteHarness(t, "x", []string{
		"--- Mystic BBS v1.12",
		" * Origin: The Wrong Number (21:4/131)",
	})
	defer cleanup()
	ch.buffer.LoadContent("draft")

	line, col := ch.HandleQuote(ih, 1, 4)

	if line != 1 || col != 4 {
		t.Errorf("cursor = (%d,%d), want (1,4) — nothing was quoted", line, col)
	}
	if got := stripANSI(ch.buffer.GetContent()); got != "draft" {
		t.Errorf("buffer = %q, want %q — no trailer line should be insertable", got, "draft")
	}
	if got := tt.Row(24); !strings.Contains(got, "Nothing to quote") {
		t.Errorf("Row(24) = %q, want the 'nothing to quote' notice", got)
	}
}

func TestQuoteModeWithoutDataShowsNotice(t *testing.T) {
	tt, ch, ih, cleanup := newQuoteHarness(t, "x", nil)
	defer cleanup()
	ch.SetQuoteData(nil)

	line, col := ch.HandleQuote(ih, 3, 7)

	// Nothing was quoted, so the cursor must not move — column included.
	if line != 3 || col != 7 {
		t.Errorf("cursor = (%d,%d), want (3,7)", line, col)
	}
	if got := tt.Row(24); !strings.Contains(got, "not replying") {
		t.Errorf("Row(24) = %q, want the 'not replying to anything' notice", got)
	}
}

func TestQuotePrefixHonoursConfiguredTemplate(t *testing.T) {
	_, ch, _, cleanup := newQuoteHarness(t, "", quoteBody)
	defer cleanup()

	if got := ch.quotePrefix(); got != "Bu> " {
		t.Errorf("default quotePrefix() = %q, want %q", got, "Bu> ")
	}

	// A template with no token is a deliberate sysop choice and is used verbatim.
	ch.SetQuoteStrings("", "", "> ")
	if got := ch.quotePrefix(); got != "> " {
		t.Errorf("plain quotePrefix() = %q, want %q", got, "> ")
	}

	ch.SetQuoteStrings("", "", "^N said: ")
	if got := ch.quotePrefix(); got != "Bucko said: " {
		t.Errorf("^N quotePrefix() = %q, want %q", got, "Bucko said: ")
	}
}
