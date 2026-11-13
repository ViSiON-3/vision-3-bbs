package editor

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// Columns are characters, not bytes (#1051). Each edit on a line holding
// multi-byte UTF-8 text keeps it valid and lands where a character count
// says it should.

func TestBackspaceRemovesAWholeCharacter(t *testing.T) {
	mb, ww := setupBuffer([]string{"café"}, 1)
	if got := mb.GetLineLength(1); got != 4 {
		t.Fatalf("GetLineLength(café) = %d, want 4", got)
	}
	line, col, ok := ww.HandleBackspace(1, mb.GetLineLength(1)+1)
	if !ok || mb.GetLine(1) != "caf" || line != 1 || col != 4 {
		t.Errorf("after Backspace: %q at (%d,%d), want \"caf\" at (1,4)", mb.GetLine(1), line, col)
	}
}

func TestEditsInTheMiddleOfMultiByteText(t *testing.T) {
	mb, ww := setupBuffer([]string{"naïve café"}, 1)
	mb.InsertChar(1, 4, 'X') // after "naï"
	if got := mb.GetLine(1); got != "naïXve café" {
		t.Errorf("insert: %q", got)
	}
	mb.DeleteChar(1, 3) // the ï
	if got := mb.GetLine(1); got != "naXve café" {
		t.Errorf("delete: %q", got)
	}
	mb.OverwriteChar(1, 10, 'E') // the é
	if got := mb.GetLine(1); got != "naXve cafE" {
		t.Errorf("overwrite: %q", got)
	}
	if !mb.SplitLine(1, 7) || mb.GetLine(1) != "naXve " || mb.GetLine(2) != "cafE" {
		t.Errorf("split: %q / %q", mb.GetLine(1), mb.GetLine(2))
	}
	if got := ww.FindWordRight(1, 1); got != 7 {
		t.Errorf("FindWordRight = %d, want 7", got)
	}
	mb.SetLine(3, "über straße")
	if got := ww.FindWordLeft(3, 12); got != 6 {
		t.Errorf("FindWordLeft = %d, want 6", got)
	}
	for i := 1; i <= mb.GetLineCount(); i++ {
		if !utf8.ValidString(mb.GetLine(i)) {
			t.Errorf("line %d isn't valid UTF-8: %q", i, mb.GetLine(i))
		}
	}
}

// A line of accented text wraps at 79 characters, not 79 bytes.
func TestWrapCountsCharacters(t *testing.T) {
	word := strings.Repeat("é", 10)
	text := strings.TrimSpace(strings.Repeat(word+" ", 7)) // 76 characters, 146 bytes
	mb, ww := setupBuffer([]string{text}, 1)
	ww.WrapAfterInsert(1, utf8.RuneCountInString(text)+1)
	if mb.GetLineCount() != 1 || mb.GetLine(1) != text {
		t.Errorf("a 76-character line was wrapped: %q", mb.GetLine(1))
	}
	mb.SetLine(1, text+" "+word) // 87 characters
	ww.WrapAfterInsert(1, utf8.RuneCountInString(mb.GetLine(1))+1)
	if mb.GetLine(1) != text || mb.GetLine(2) != word {
		t.Errorf("wrap: %q / %q", mb.GetLine(1), mb.GetLine(2))
	}
}

// Through the real editor: Backspace at the end of a loaded accented line,
// and typing after it, leave valid text.
func TestEditorBackspaceOnAccentedText(t *testing.T) {
	_, ed := runSeeded(t, []string{"Grüße aus Köln"}, []int{1}, 1, 1, 15, "\x7f\x7fXY")
	got := ed.buffer.GetLine(1)
	if got != "Grüße aus KöXY" || !utf8.ValidString(got) {
		t.Errorf("line = %q, want \"Grüße aus KöXY\"", got)
	}
}

// Raw CP437 bytes a quote carries aren't valid UTF-8. Editing next to them,
// splitting, joining and rewrapping keep them byte for byte (#1059 review):
// each byte is one character, and no line is converted to []rune.
func TestRawCP437BytesSurviveEdits(t *testing.T) {
	quoted := "> \xdb\xc4 box"
	mb, ww := setupBuffer([]string{quoted}, 1)
	if got := mb.GetLineLength(1); got != 8 {
		t.Fatalf("GetLineLength = %d, want 8 (each raw byte is one column)", got)
	}
	mb.InsertChar(1, 5, 'X') // after the two drawing bytes
	if got := mb.GetLine(1); got != "> \xdb\xc4X box" {
		t.Errorf("insert: %q", got)
	}
	mb.DeleteChar(1, 5)         // the X again
	mb.OverwriteChar(1, 7, 'B') // the o of "box"
	if got := mb.GetLine(1); got != "> \xdb\xc4 bBx" {
		t.Errorf("overwrite: %q", got)
	}
	if !mb.SplitLine(1, 5) || mb.GetLine(1) != "> \xdb\xc4" || mb.GetLine(2) != " bBx" {
		t.Errorf("split: %q / %q", mb.GetLine(1), mb.GetLine(2))
	}
	mb.JoinLines(1)
	ww.ReflowRange(1, 1, 1)
	if got := mb.GetLine(1); got != "> \xdb\xc4 bBx" {
		t.Errorf("after join and reflow: %q", got)
	}
	if got := ww.FindWordRight(1, 1); got != 3 {
		t.Errorf("FindWordRight = %d, want 3", got)
	}
}

// On a quote whose initials are multi-byte, the cursor is drawn at the
// character being edited (#1059 review): buffer columns are characters, so
// the screen mapping counts characters too.
func TestQuoteCursorWithMultiByteInitials(t *testing.T) {
	d := displayQuoteLine("ÉS> hello")
	// Column 5 is the "h": the lead (1) and "ÉS> " (4) come before it.
	if got := d.screenCol(5); got != 6 {
		t.Errorf("screenCol(5) = %d, want 6, on the h", got)
	}
	if got := d.screenCol(2); got != 3 {
		t.Errorf("screenCol(2) = %d, want 3, on the S", got)
	}
}
