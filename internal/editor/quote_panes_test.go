package editor

import (
	"fmt"
	"strings"
	"testing"
)

// The 80x24 harness has no template, so the editing area is rows 7-23: the
// compose pane takes rows 7-14, the divider row 15 and the source pane 16-23.
const (
	composeFirstRow = 7
	composeLastRow  = 14
	dividerRow      = 15
)

// sourceLines returns "src 1" .. "src n".
func sourceLines(n int) []string {
	lines := make([]string, n)
	for i := range lines {
		lines[i] = fmt.Sprintf("src %d", i+1)
	}
	return lines
}

// TAB hands the arrow and paging keys to the message pane so a long reply can
// be scrolled without leaving quote mode. The quote is opened on line 30 of a
// 30-line draft, which puts line 26 at the top of the pane.
func TestQuoteModeComposePaneScrolls(t *testing.T) {
	const (
		up, down   = "\x1b[A", "\x1b[B"
		pgUp, pgDn = "\x1b[5~", "\x1b[6~"
	)
	for _, tc := range []struct {
		name, keys string
		wantTop    string
	}{
		{"opens around the insertion point", "", "line 26"},
		{"up scrolls back a line", up + up, "line 24"},
		{"down scrolls forward a line", down, "line 27"},
		{"page up scrolls a pane", pgUp, "line 18"},
		{"page up stops at the first line", pgUp + pgUp + pgUp + pgUp + up, "line 1"},
		{"page down stops at the last line", pgDn + pgDn + down, "line 30"},
		{"keys the pane does not use are ignored", " x", "line 26"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tt, ch, ih, cleanup := newQuoteHarness(t, "\t"+tc.keys+"\x1b", sourceLines(30))
			defer cleanup()
			fillBuffer(ch.buffer, 30)
			before := ch.buffer.GetContent()

			line, col := ch.HandleQuote(ih, 30, 1)

			if got := tt.Row(composeFirstRow); got != tc.wantTop {
				t.Errorf("top of the message pane = %q, want %q", got, tc.wantTop)
			}
			// Scrolling is not editing: nothing is quoted and the cursor goes
			// back where it was.
			if got := ch.buffer.GetContent(); got != before {
				t.Errorf("message changed while only scrolling:\n%s", got)
			}
			if line != 30 || col != 1 {
				t.Errorf("cursor = (%d,%d), want (30,1)", line, col)
			}
		})
	}
}

// The divider's label and key legend say which pane has the keys.
func TestQuoteModeDividerFollowsFocus(t *testing.T) {
	tt, ch, ih, cleanup := newQuoteHarness(t, "\t\x11", sourceLines(30))
	defer cleanup()
	fillBuffer(ch.buffer, 30)

	// CTRL-Q leaves quote mode from the message pane, as ESC does.
	ch.HandleQuote(ih, 30, 1)

	got := tt.Row(dividerRow)
	if !strings.HasPrefix(got, "─ Message ─") {
		t.Errorf("divider = %q, want it labelled \"Message\" while that pane has focus", got)
	}
	if !strings.HasSuffix(got, " Up/Dn Scroll  TAB Back  ESC Done ─") {
		t.Errorf("divider = %q, want the message-pane key legend", got)
	}
	if strings.Contains(got, "SPACE") {
		t.Errorf("divider = %q, still offers SPACE although the message pane has focus", got)
	}
}

// A second TAB gives the keys back to the source pane: the message pane snaps
// back to the insertion point and SPACE quotes again.
func TestQuoteModeTabReturnsToSourcePane(t *testing.T) {
	// TAB, page up to the top of the message, TAB back, quote one line, leave.
	keys := "\t" + strings.Repeat("\x1b[5~", 4) + "\t" + " " + "\x1b"
	tt, ch, ih, cleanup := newQuoteHarness(t, keys, sourceLines(30))
	defer cleanup()
	fillBuffer(ch.buffer, 30)

	line, _ := ch.HandleQuote(ih, 30, 1)

	if got := tt.Row(dividerRow); !strings.HasPrefix(got, "─ Quoting Bucko ─") {
		t.Errorf("divider = %q, want it labelled \"Quoting Bucko\" again", got)
	}
	got := strings.Split(stripANSI(ch.buffer.GetContent()), "\n")
	want := []string{"line 29", "--- Bucko Said ---", "Bu> src 1", "--- Bucko Done ---", "", "line 30"}
	if len(got) != 34 || strings.Join(got[28:], "\n") != strings.Join(want, "\n") {
		t.Errorf("message tail = %q, want %q", got[len(got)-len(want):], want)
	}
	if line != 34 {
		t.Errorf("cursor line = %d, want 34 — on the text below the quote block", line)
	}
	// The pane followed the insertion point back down: the new block is on screen.
	var pane []string
	for row := composeFirstRow; row <= composeLastRow; row++ {
		pane = append(pane, tt.Row(row))
	}
	if !strings.Contains(strings.Join(pane, "\n"), "Bu> src 1") {
		t.Errorf("message pane does not show the quoted line:\n%s", strings.Join(pane, "\n"))
	}
}

// The banners and prefix come from strings.json when the sysop sets them.
func TestQuoteModeUsesConfiguredQuoteStrings(t *testing.T) {
	_, ch, ih, cleanup := newQuoteHarness(t, " \x1b", []string{"hello there"})
	defer cleanup()
	ch.SetQuoteData(&QuoteData{
		From: "Bucko", Title: "Re: doors", Date: "08/28/26", Time: "9:15 pm",
		Lines: []string{"hello there"},
	})
	ch.SetQuoteStrings("|08^N wrote on ^D at ^W about ^T:", "|08end of ^N", "^N> ")

	ch.HandleQuote(ih, 1, 1)

	got := strings.Split(stripANSI(ch.buffer.GetContent()), "\n")
	want := []string{
		"Bucko wrote on 08/28/26 at 9:15 pm about Re: doors:",
		"Bucko> hello there",
		"end of Bucko",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("quote block = %q, want %q", got, want)
	}
	// The banner's pipe colour is stored as a real escape, since the editing
	// area draws buffer text without expanding pipe codes.
	if first := ch.buffer.GetLine(1); strings.Contains(first, "|08") || !strings.HasPrefix(first, "\x1b[") {
		t.Errorf("banner line = %q, want the |08 colour converted to an ANSI escape", first)
	}
}

func TestQuoteModeAnonymousAuthor(t *testing.T) {
	tt, ch, ih, cleanup := newQuoteHarness(t, " \x1b", nil)
	defer cleanup()
	ch.SetQuoteData(&QuoteData{From: "Bucko", IsAnon: true, Lines: []string{"hello there"}})

	ch.HandleQuote(ih, 1, 1)

	got := strings.Split(stripANSI(ch.buffer.GetContent()), "\n")
	want := []string{"--- Anonymous Said ---", "An> hello there", "--- Anonymous Done ---"}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("quote block = %q, want %q", got, want)
	}
	if strings.Contains(tt.Snapshot(), "Bucko") {
		t.Errorf("the anonymous author's name is on screen:\n%s", tt.Snapshot())
	}
}

// Paging and Home/End move the bar through a source longer than the pane.
// SPACE quotes whichever line the bar is on, which makes its position visible.
func TestQuoteModePagingKeysMoveTheBar(t *testing.T) {
	const (
		home, end  = "\x1b[H", "\x1b[F"
		pgUp, pgDn = "\x1b[5~", "\x1b[6~"
		up         = "\x1b[A"
	)
	for _, tc := range []struct {
		name, keys, want string
	}{
		{"page down moves a pane", pgDn, "src 9"},
		{"page down then page up returns", pgDn + pgDn + pgUp, "src 9"},
		{"end jumps to the last line", end, "src 30"},
		{"page down stops at the last line", end + pgDn, "src 30"},
		{"home jumps back to the first line", end + home, "src 1"},
		{"up at the first line stays put", up + pgUp, "src 1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, ch, ih, cleanup := newQuoteHarness(t, tc.keys+" \x1b", sourceLines(30))
			defer cleanup()

			ch.HandleQuote(ih, 1, 1)

			if got, want := stripANSI(ch.buffer.GetLine(2)), "Bu> "+tc.want; got != want {
				t.Errorf("quoted line = %q, want %q", got, want)
			}
		})
	}
}

// Near the line limit the quote block takes what room there is: the blank
// line after it is dropped first, then quoted lines are refused with a notice.
// Either way the cursor comes back on the text that followed the block.
func TestQuoteModeNearTheLineLimit(t *testing.T) {
	const full = "Message is full — no room for more quoted lines."
	for _, tc := range []struct {
		name       string
		draftLines int
		wantQuoted int // quoted lines that fitted, of the two asked for
		wantGap    bool
		wantNotice string
	}{
		{"room for the block and a gap", MaxLines - 5, 2, true, ""},
		{"room for the block only", MaxLines - 4, 2, false, ""},
		{"room for one quoted line", MaxLines - 3, 1, false, full},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tt, ch, ih, cleanup := newQuoteHarness(t, "  \x1b", quoteBody)
			defer cleanup()
			fillBuffer(ch.buffer, tc.draftLines)

			line, col := ch.HandleQuote(ih, 5, 3)

			want := []string{"line 4", "--- Bucko Said ---", "Bu> On 28 Aug 2026, Shurato said the following..."}
			if tc.wantQuoted == 2 {
				want = append(want, "Bu> Sh> I'd be interested in this as well")
			}
			want = append(want, "--- Bucko Done ---")
			if tc.wantGap {
				want = append(want, "")
			}
			want = append(want, "line 5")

			got := strings.Split(stripANSI(ch.buffer.GetContent()), "\n")
			if len(got) != MaxLines {
				t.Fatalf("message is %d lines, want it filled to %d", len(got), MaxLines)
			}
			if block := got[3 : 3+len(want)]; strings.Join(block, "\n") != strings.Join(want, "\n") {
				t.Errorf("lines 4-%d = %q, want %q", 3+len(want), block, want)
			}
			// Nothing the user wrote is pushed off the end.
			if last := got[MaxLines-1]; last != fmt.Sprintf("line %d", tc.draftLines) {
				t.Errorf("last line = %q, want %q", last, fmt.Sprintf("line %d", tc.draftLines))
			}
			if got := ch.buffer.GetLine(line); got != "line 5" || col != 1 {
				t.Errorf("cursor = (%d,%d) on %q, want column 1 of \"line 5\"", line, col, got)
			}
			if got := tt.Row(ch.screen.PromptRow()); got != tc.wantNotice {
				t.Errorf("prompt row = %q, want %q", got, tc.wantNotice)
			}
		})
	}
}
