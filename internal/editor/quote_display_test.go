package editor

import (
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/editor/testterm"
)

// A quote line is drawn the way the message reader draws it: one blank, the
// prefix normalized to single spaces, then the text with its own indent (#962).
func TestDisplayQuoteLineLayout(t *testing.T) {
	for _, tc := range []struct {
		name, line, want string
		shift            int
		quote            bool
	}{
		{"default prefix already has the blank", " RW> some text", " RW> some text", 0, true},
		{"prefix without a blank gains one", "RW> some text", " RW> some text", 1, true},
		{"nested prefix is normalized", "  RW>  Sh> text", " RW> Sh> text", -2, true},
		{"body keeps its own indent", " RW>   indented", " RW>   indented", 0, true},
		{"bare marker", ">text", " >text", 1, true},
		{"deeper bare marker", ">> text", " >> text", 1, true},
		{"bare prefix has no trailing blank", "RW>", " RW>", 1, true},
		{"plain text", "Hello there", "Hello there", 0, false},
		{"arrow is not a quote", "-> not a quote", "-> not a quote", 0, false},
		{"tearline", "--- ViSiON/3", "--- ViSiON/3", 0, false},
		{"origin", " * Origin: somewhere (1:2/3)", " * Origin: somewhere (1:2/3)", 0, false},
		{"block art initials", "▓▒> art", "▓▒> art", 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := displayQuoteLine(tc.line)
			if d.quote != tc.quote {
				t.Fatalf("displayQuoteLine(%q).quote = %v, want %v", tc.line, d.quote, tc.quote)
			}
			if got := stripANSI(d.text); got != tc.want {
				t.Errorf("drawn as %q, want %q", got, tc.want)
			}
			if d.shift != tc.shift {
				t.Errorf("shift = %d, want %d", d.shift, tc.shift)
			}
			if tc.quote && !strings.HasPrefix(d.text, quoteSGR) {
				t.Errorf("drawn as %q, want it to open in the quote colour %q", d.text, quoteSGR)
			}
			if !tc.quote && d.text != tc.line {
				t.Errorf("drawn as %q, want the line untouched", d.text)
			}
		})
	}
}

// Escapes before the text, such as a colour the sysop's prefix sets, are kept.
func TestDisplayQuoteLineKeepsHeadEscapes(t *testing.T) {
	line := "\x1b[1;36mRW>\x1b[0m text"
	d := displayQuoteLine(line)
	if !d.quote {
		t.Fatalf("displayQuoteLine(%q) is not a quote", line)
	}
	if got, want := d.text, quoteSGR+"\x1b[1;36m\x1b[0m RW> text"; got != want {
		t.Errorf("drawn as %q, want %q", got, want)
	}
}

// The editor draws quote lines in the reader's colour, moved over by the
// column the reader adds, and puts the cursor on the character it is at.
func TestScreenDrawsQuoteLinesAsTheReaderDoes(t *testing.T) {
	tt := testterm.New(80, 24)
	s := NewScreen(tt, ansi.OutputModeUTF8, 80, 24)
	buf := NewMessageBuffer()
	buf.SetLine(1, "RW> quoted text")
	buf.SetLine(2, "my reply")

	s.RefreshScreen(buf, 1, 1, 5, true, true) // cursor on the 'q'

	row := s.GetEditingStartY()
	if got := tt.Row(row); got != " RW> quoted text" {
		t.Errorf("quote row = %q, want %q", got, " RW> quoted text")
	}
	if got := tt.Row(row + 1); got != "my reply" {
		t.Errorf("reply row = %q, want %q", got, "my reply")
	}
	cell := tt.RowCells(row)[5]
	if cell.Rune != 'q' || cell.Fg != 33 || !cell.Bold {
		t.Errorf("quote cell = %+v, want a bold yellow 'q' (the reader's |14)", cell)
	}
	if reply := tt.RowCells(row + 1)[0]; reply.Fg != -1 || reply.Bold {
		t.Errorf("reply cell = %+v, want the default colour after a quote line", reply)
	}
	if r, c := tt.Cursor(); r != row || c != 6 {
		t.Errorf("cursor = (%d,%d), want (%d,6), on the 'q' as drawn", r, c, row)
	}

	// The same line with the blank stored: drawn the same, no shift.
	buf.SetLine(1, " RW> quoted text")
	s.RefreshScreen(buf, 1, 1, 6, true, true)
	if got := tt.Row(row); got != " RW> quoted text" {
		t.Errorf("quote row = %q, want %q", got, " RW> quoted text")
	}
	if r, c := tt.Cursor(); r != row || c != 6 {
		t.Errorf("cursor = (%d,%d), want (%d,6)", r, c, row)
	}

	// Once the line is no longer a quote, the cursor follows the buffer again.
	buf.SetLine(1, "RW quoted text")
	s.RefreshScreen(buf, 1, 1, 4, true, true)
	if got := tt.Row(row); got != "RW quoted text" {
		t.Errorf("plain row = %q, want %q", got, "RW quoted text")
	}
	if r, c := tt.Cursor(); r != row || c != 4 {
		t.Errorf("cursor = (%d,%d), want (%d,4)", r, c, row)
	}
}

// A quote line the display widens to the last column keeps its last character:
// erasing to the end of the row there would take it.
func TestScreenQuoteLineFillingTheRowKeepsItsLastColumn(t *testing.T) {
	tt := testterm.New(80, 24)
	s := NewScreen(tt, ansi.OutputModeUTF8, 80, 24)
	buf := NewMessageBuffer()
	line := ">" + strings.Repeat("x", 77) + "z" // 79 stored, 80 drawn
	buf.SetLine(1, line)

	s.RefreshScreen(buf, 1, 1, 1, true, true)

	got := tt.Row(s.GetEditingStartY())
	if want := " " + line; got != want {
		t.Errorf("row = %q, want %q", got, want)
	}
}

// The quote picker wraps under a prefix with no blank of its own one column
// short, leaving room for the one the reader draws, so neither wraps a word
// differently from the other.
func TestWrapQuotedLeavesTheReadersColumn(t *testing.T) {
	text := strings.Repeat("lorem ipsum dolor sit amet ", 12)
	for _, prefix := range []string{" RW> ", "RW> ", "> ", " RW> Sh> "} {
		for _, line := range wrapQuoted(prefix, text, MaxLineLength) {
			if w := runeLen(stripANSI(displayQuoteLine(line).text)); w > MaxLineLength {
				t.Errorf("prefix %q: %q is drawn %d columns wide, want <= %d", prefix, line, w, MaxLineLength)
			}
		}
	}
}

// Typing in a quote reflows it to the same width the picker wrapped it to.
func TestReflowRangeLeavesTheReadersColumn(t *testing.T) {
	buf := NewMessageBuffer()
	prefix := "RW> "
	buf.SetLine(1, prefix+strings.Repeat("word ", 40))
	buf.SetQuotePrefix(1, prefix)
	buf.SetHardNewline(1, true)
	NewWordWrapper(buf).ReflowRange(1, 1, 1)

	if buf.GetLineCount() < 2 {
		t.Fatalf("buffer has %d lines, want the paragraph wrapped", buf.GetLineCount())
	}
	for n := 1; n <= buf.GetLineCount(); n++ {
		line := buf.GetLine(n)
		if w := runeLen(stripANSI(displayQuoteLine(line).text)); w > MaxLineLength {
			t.Errorf("line %d %q is drawn %d columns wide, want <= %d", n, line, w, MaxLineLength)
		}
	}
	if got := buf.GetLine(1); runeLen(got) != MaxLineLength-1 {
		t.Errorf("line 1 = %q (%d), want it filled to %d", got, runeLen(got), MaxLineLength-1)
	}
}
