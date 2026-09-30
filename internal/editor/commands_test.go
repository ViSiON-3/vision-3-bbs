package editor

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/editor/testterm"
)

// lightbarBg is the background lbSelected paints: blue.
const lightbarBg = 44

// highlighted returns the text drawn in the lightbar colour on a row.
func highlighted(tt *Term, row int) string {
	var b strings.Builder
	for col := 1; col <= 80; col++ {
		if c := tt.Cell(row, col); c.Bg == lightbarBg {
			b.WriteRune(c.Rune)
		}
	}
	return b.String()
}

// endedInput returns an InputHandler that plays keys and then reports the
// connection closed.
func endedInput(keys string) *InputHandler {
	return NewInputHandler(strings.NewReader(keys))
}

func TestHandleSaveRefusesBlankMessage(t *testing.T) {
	tt, ch, _, cleanup := newQuoteHarness(t, "", nil)
	defer cleanup()

	ch.buffer.LoadContent("   \n\n  ")
	if ch.HandleSave() {
		t.Error("HandleSave() = true for a message that is only whitespace")
	}
	if got, want := tt.Row(ch.screen.PromptRow()), "Cannot save empty message! Press any key..."; got != want {
		t.Errorf("prompt row = %q, want %q", got, want)
	}

	ch.buffer.LoadContent("text")
	if !ch.HandleSave() {
		t.Error("HandleSave() = false for a message with text")
	}
}

// The abort prompt opens with the bar on No, so a stray Enter cannot throw the
// message away.
func TestHandleAbortDrawsPromptWithNoSelected(t *testing.T) {
	tt, ch, _, cleanup := newQuoteHarness(t, "", nil)
	defer cleanup()

	// Text on the row above the prompt is cleared as a separator when there is
	// no footer to keep.
	ch.screen.GoXY(1, 23)
	ch.screen.WriteDirect("leftover text")

	// The connection closing while the prompt is up counts as "no".
	if ch.HandleAbort(endedInput("")) {
		t.Error("HandleAbort() = true when input ended without an answer")
	}
	if got, want := tt.Row(24), " Abort message?   Yes    No"; got != want {
		t.Errorf("prompt row = %q, want %q", got, want)
	}
	if got := highlighted(tt, 24); got != " No " {
		t.Errorf("highlighted = %q, want %q", got, " No ")
	}
	if got := tt.Row(23); got != "" {
		t.Errorf("row above the prompt = %q, want it cleared", got)
	}
	if !tt.CursorVisible() {
		t.Error("cursor left hidden after the prompt closed")
	}
	if got := tt.Unhandled(); len(got) != 0 {
		t.Errorf("Unhandled() = %q, want empty", got)
	}
}

func TestHandleAbortArrowMovesBarToYes(t *testing.T) {
	tt, ch, _, cleanup := newQuoteHarness(t, "", nil)
	defer cleanup()

	ch.HandleAbort(endedInput("\x1b[D"))
	if got := highlighted(tt, 24); got != " Yes " {
		t.Errorf("highlighted after Left = %q, want %q", got, " Yes ")
	}
}

// The labels and the question come from strings.json; blank ones fall back to
// the built-in wording.
func TestHandleAbortUsesConfiguredLabels(t *testing.T) {
	tt := testterm.New(80, 24)
	screen := NewScreen(tt, ansi.OutputModeUTF8, 80, 24)
	ch := NewCommandHandler(screen, NewMessageBuffer(), "", "", "", " Yep ", "Nope", "|12Really quit?")

	if !ch.HandleAbort(endedInput("\x1b[C\r")) {
		t.Error("HandleAbort() = false after moving the bar to Yes and pressing Enter")
	}
	if got, want := tt.Row(24), " Really quit?   Yep    Nope"; got != want {
		t.Errorf("prompt row = %q, want %q", got, want)
	}
	if got := highlighted(tt, 24); got != " Yep " {
		t.Errorf("highlighted = %q, want %q", got, " Yep ")
	}
}

func TestShowEscapeMenu(t *testing.T) {
	const menuRow = " Select an Option:  Save    Abort    Edit    Help    Quote"

	for _, tc := range []struct {
		name, keys string
		want       CommandType
		wantBar    string
	}{
		{"opens on Edit", "\r", CommandNone, " Edit "},
		{"left once is Abort", "\x1b[D ", CommandAbort, " Abort "},
		{"left stops at Save", "\x1b[D\x1b[D\x1b[D\x1b[D\r", CommandSave, " Save "},
		{"right once is Help", "\x1b[C\r", CommandHelp, " Help "},
		{"right stops at Quote", "\x1b[C\x1b[C\x1b[C\r", CommandQuote, " Quote "},
		{"escape cancels whatever is selected", "\x1b[D\x1b", CommandNone, " Abort "},
		{"input ending cancels", "\x1b[C", CommandNone, " Help "},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tt, ch, _, cleanup := newQuoteHarness(t, "", nil)
			defer cleanup()

			if got := ch.ShowEscapeMenu(endedInput(tc.keys)); got != tc.want {
				t.Errorf("ShowEscapeMenu() = %d, want %d", got, tc.want)
			}
			// No footer is loaded, so nothing repaints the row and the menu is
			// still there to inspect.
			if got := tt.Row(ch.screen.PromptRow()); got != menuRow {
				t.Errorf("menu row = %q, want %q", got, menuRow)
			}
			if got := highlighted(tt, ch.screen.PromptRow()); got != tc.wantBar {
				t.Errorf("highlighted = %q, want %q", got, tc.wantBar)
			}
			if !tt.CursorVisible() {
				t.Error("cursor left hidden after the menu closed")
			}
			if got := tt.Unhandled(); len(got) != 0 {
				t.Errorf("Unhandled() = %q, want empty", got)
			}
		})
	}
}

// With a footer loaded, closing the menu puts the footer's tagline row back.
func TestShowEscapeMenuRestoresFooter(t *testing.T) {
	// Like the shipped footer, the tagline row spans the width of the screen.
	tagline := "   ViSiON/3 Edit" + strings.Repeat(" ", 42) + "Press ESCape For Help"
	menuSet := writeFooterTemplate(t, tagline)
	tt, ch, _, cleanup := newQuoteHarness(t, "", nil)
	defer cleanup()
	if err := ch.screen.LoadFooterTemplate(menuSet); err != nil {
		t.Fatalf("LoadFooterTemplate: %v", err)
	}

	ch.ShowEscapeMenu(endedInput("\x1b"))

	if got := tt.Row(24); got != tagline {
		t.Errorf("row 24 after the menu closed = %q, want the footer tagline %q", got, tagline)
	}
}

// When the menu set has an EDITHELP.ANS it is shown in place of the built-in text.
func TestHandleHelpShowsHelpFile(t *testing.T) {
	menuSet := t.TempDir()
	if err := os.MkdirAll(filepath.Join(menuSet, "ansi"), 0o755); err != nil {
		t.Fatal(err)
	}
	art := "\x1b[1;36mSysop's own help\r\nsecond line\r\n"
	if err := os.WriteFile(filepath.Join(menuSet, "ansi", "EDITHELP.ANS"), []byte(art), 0o644); err != nil {
		t.Fatal(err)
	}

	tt := testterm.New(80, 24)
	screen := NewScreen(tt, ansi.OutputModeUTF8, 80, 24)
	ch := NewCommandHandler(screen, NewMessageBuffer(), menuSet, "", "", "", "", "")
	screen.GoXY(1, 10)
	screen.WriteDirect("message text under the help")

	ch.HandleHelp(endedInput("k"))

	if got := tt.Row(1); got != "Sysop's own help" {
		t.Errorf("Row(1) = %q, want %q", got, "Sysop's own help")
	}
	if got := tt.Row(2); got != "second line" {
		t.Errorf("Row(2) = %q, want %q", got, "second line")
	}
	if got := tt.Row(10); got != "" {
		t.Errorf("Row(10) = %q, want the screen cleared before the help is drawn", got)
	}
	if got := tt.Row(24); got != "Press any key to continue..." {
		t.Errorf("Row(24) = %q, want the key prompt", got)
	}
	if strings.Contains(tt.Snapshot(), "Full Screen Message Editor Help") {
		t.Error("built-in help was drawn although EDITHELP.ANS exists")
	}
}

// Without a help file the built-in key reference is shown. The terminal here
// is taller than the text so its opening line is still on screen.
func TestHandleHelpFallsBackToBuiltInText(t *testing.T) {
	tt := testterm.New(80, 120)
	screen := NewScreen(tt, ansi.OutputModeUTF8, 80, 120)
	ch := NewCommandHandler(screen, NewMessageBuffer(), t.TempDir(), "", "", "", "", "")

	ch.HandleHelp(endedInput("k"))

	if got := tt.Row(1); got != "Full Screen Message Editor Help" {
		t.Errorf("Row(1) = %q, want the help title", got)
	}
	// |15 on the title: bright white.
	if c := tt.Cell(1, 1); c.Fg != 37 || !c.Bold {
		t.Errorf("Cell(1,1) = %+v, want bright white from |15", c)
	}
	snap := tt.Snapshot()
	for _, want := range []string{"Navigation Commands:", "Ctrl+Z", "Quote Mode (Ctrl+Q when replying):", "Word Wrapping:"} {
		if !strings.Contains(snap, want) {
			t.Errorf("help screen is missing %q", want)
		}
	}
	if got := tt.Row(120); got != "Press any key to continue..." {
		t.Errorf("Row(120) = %q, want the key prompt", got)
	}
	if got := tt.Unhandled(); len(got) != 0 {
		t.Errorf("Unhandled() = %q, want empty", got)
	}
}

// On a 24-row terminal the built-in help is longer than the screen. It must
// be paged rather than scrolled off, and each line must start in column 1
// rather than where the one above ended (#515).
func TestHandleHelpBuiltInTextIsPaged(t *testing.T) {
	newHelp := func() (*testterm.Term, *strings.Builder, *CommandHandler) {
		tt := testterm.New(80, 24)
		var raw strings.Builder
		screen := NewScreen(io.MultiWriter(tt, &raw), ansi.OutputModeUTF8, 80, 24)
		return tt, &raw, NewCommandHandler(screen, NewMessageBuffer(), t.TempDir(), "", "", "", "", "")
	}

	t.Run("first page", func(t *testing.T) {
		tt, raw, ch := newHelp()
		ch.HandleHelp(endedInput("")) // input ends at the first "more" prompt

		for row, want := range map[int]string{
			1: "Full Screen Message Editor Help",
			3: "Navigation Commands:",
			4: "  Ctrl+E or Up Arrow     - Move up one line",
			5: "  Ctrl+X or Down Arrow   - Move down one line",
		} {
			if got := tt.Row(row); got != want {
				t.Errorf("Row(%d) = %q, want %q", row, got, want)
			}
		}
		if !strings.Contains(raw.String(), "Press any key for more...") {
			t.Error("first page did not pause for a key")
		}
		if strings.Contains(tt.Snapshot(), "Word Wrapping:") {
			t.Error("the end of the help was drawn on the first page")
		}
	})

	t.Run("second page", func(t *testing.T) {
		tt, raw, ch := newHelp()
		ch.HandleHelp(endedInput("k"))

		snap := tt.Snapshot()
		if strings.Contains(snap, "Full Screen Message Editor Help") {
			t.Error("first page still on screen after a key was pressed")
		}
		if !strings.Contains(snap, "Word Wrapping:") {
			t.Errorf("second page is missing the end of the help:\n%s", snap)
		}
		if tt.Row(1) == "" {
			t.Error("second page opens on a blank row")
		}
		if got := tt.Row(24); got != "Press any key to continue..." {
			t.Errorf("Row(24) = %q, want the key prompt", got)
		}
		// Between them the two pages show every line of the help.
		out := raw.String()
		for _, line := range strings.Split(builtInHelp, "\n") {
			text := regexp.MustCompile(`\|\d\d`).ReplaceAllString(line, "")
			if strings.TrimSpace(text) != "" && !strings.Contains(out, text) {
				t.Errorf("help line %q was never shown", text)
			}
		}
		if got := tt.Unhandled(); len(got) != 0 {
			t.Errorf("Unhandled() = %q, want empty", got)
		}
	})
}

func TestHandleViewListsTheMessage(t *testing.T) {
	tt, ch, _, cleanup := newQuoteHarness(t, "", nil)
	defer cleanup()
	ch.buffer.LoadContent("first\nsecond\n\nfourth")
	ch.screen.GoXY(1, 20)
	ch.screen.WriteDirect("stale editor row")

	ch.HandleView(endedInput("k"))

	want := []string{"Current Message:", "", "first", "second", "", "fourth"}
	for i, w := range want {
		if got := tt.Row(i + 1); got != w {
			t.Errorf("Row(%d) = %q, want %q", i+1, got, w)
		}
	}
	if got := tt.Row(20); got != "" {
		t.Errorf("Row(20) = %q, want the screen cleared first", got)
	}
	if got := tt.Row(24); got != "Press any key to continue..." {
		t.Errorf("Row(24) = %q, want the key prompt", got)
	}
}

func TestProcessQuoteCodes(t *testing.T) {
	_, ch, _, cleanup := newQuoteHarness(t, "", nil)
	defer cleanup()
	ch.SetQuoteData(&QuoteData{From: "John Smith", Title: "Re: doors", Date: "08/28/26", Time: "9:15 pm"})

	for _, tc := range []struct{ in, want string }{
		{"^N wrote", "John Smith wrote"},
		{"^n on ^D at ^W", "John Smith on 08/28/26 at 9:15 pm"},
		{"^d ^w ^t", "08/28/26 9:15 pm Re: doors"},
		{"[^T]", "[Re: doors]"},
		{"^I> ^i>", "JS> JS>"},
		{"2^8 and a^", "2^8 and a^"}, // an unknown code and a trailing caret pass through
		{"", ""},
	} {
		if got := ch.processQuoteCodes(tc.in); got != tc.want {
			t.Errorf("processQuoteCodes(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}

	// An anonymous author is never named, in the banner or in the initials.
	ch.SetQuoteData(&QuoteData{From: "John Smith", IsAnon: true})
	if got := ch.processQuoteCodes("^N (^I)"); got != "Anonymous (An)" {
		t.Errorf("anonymous author: processQuoteCodes = %q, want %q", got, "Anonymous (An)")
	}
}

func TestFilterPipeCodes(t *testing.T) {
	_, ch, _, cleanup := newQuoteHarness(t, "", nil)
	defer cleanup()

	for _, tc := range []struct{ in, want string }{
		{"|15bright|07 text", "bright text"},
		{"a|09", "a"},
		{"a | b |x1 |1", "a | b |x1 |1"}, // only |NN is a colour code
		{"", ""},
	} {
		if got := ch.filterPipeCodes(tc.in); got != tc.want {
			t.Errorf("filterPipeCodes(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestColorCodeToAnsi(t *testing.T) {
	for code, want := range map[int]string{
		15:  "\x1b[97;40m", // bright white on black
		112: "\x1b[30;47m", // black on white: the default highlight
		31:  "\x1b[97;44m", // bright white on blue
		4:   "\x1b[31;40m", // DOS red is ANSI 31
		// A blink/bright background bit folds onto the standard background.
		0x9E: "\x1b[93;44m",
	} {
		if got := colorCodeToAnsi(code); got != want {
			t.Errorf("colorCodeToAnsi(%d) = %q, want %q", code, got, want)
		}
	}
}

// fillBuffer loads n numbered lines.
func fillBuffer(buffer *MessageBuffer, n int) {
	buffer.LoadContent(numberedLines(n))
}

// Quoting into a message with no room left must say so and leave the message
// exactly as it was, with no stray banner. Two lines short of full, the banner
// pair fits but no quoted line does, and the empty pair must go again (#516).
func TestQuoteModeReportsFullMessage(t *testing.T) {
	for _, lines := range []int{MaxLines, MaxLines - 1, MaxLines - 2} {
		t.Run(fmt.Sprintf("%d lines", lines), func(t *testing.T) {
			tt, ch, ih, cleanup := newQuoteHarness(t, " \x1b", quoteBody)
			defer cleanup()
			fillBuffer(ch.buffer, lines)
			before := ch.buffer.GetContent()

			line, col := ch.HandleQuote(ih, 5, 3)

			if got := ch.buffer.GetContent(); got != before {
				t.Errorf("a full message was changed by a refused quote:\n%s", got)
			}
			if line != 5 || col != 1 {
				t.Errorf("cursor = (%d,%d), want (5,1) — back on the line it came from", line, col)
			}
			want := "Message is full — no room for more quoted lines."
			if got := tt.Row(ch.screen.PromptRow()); got != want {
				t.Errorf("prompt row = %q, want %q", got, want)
			}
		})
	}
}
