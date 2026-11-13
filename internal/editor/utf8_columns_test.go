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
