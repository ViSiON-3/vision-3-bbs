package editor

import (
	"strings"
	"testing"
)

// Issue #868: Ctrl-K and Ctrl-Y close up the paragraph they delete from.
func TestDeleteLineReflowsParagraph(t *testing.T) {
	for _, key := range []string{"\x0b", "\x19"} {
		// fullLine / middle / "tail words" form one soft paragraph, then a
		// second paragraph that must not be pulled into it.
		lines := []string{"short", "middle words", "tail words", "next paragraph"}
		_, ed := runSeeded(t, lines, []int{3, 4}, 1, 2, 1, key)
		checkBuffer(t, ed.buffer, []string{"short tail words", "next paragraph"}, 1, 2)
		if ed.currentLine != 1 || ed.currentCol != 7 {
			t.Errorf("key %q: cursor = (%d,%d), want (1,7) at the start of the text that moved up",
				key, ed.currentLine, ed.currentCol)
		}
	}
}

// Deleting the line that ends a paragraph keeps the paragraph break, so the
// line above does not run on into the next paragraph.
func TestDeleteLastLineOfParagraphKeepsBreak(t *testing.T) {
	lines := []string{"first soft", "last of para", "next paragraph"}
	_, ed := runSeeded(t, lines, []int{2, 3}, 1, 2, 1, "\x0b")
	checkBuffer(t, ed.buffer, []string{"first soft", "next paragraph"}, 1, 2)
}

// Deleting the line holding the middle of a split word leaves the parts either
// side of it as two words, not glued into one.
func TestDeleteLineInsideSplitWord(t *testing.T) {
	_, ed := runSeeded(t, nil, nil, 1, 1, 1,
		strings.Repeat("a", MaxLineLength)+strings.Repeat("b", MaxLineLength)+"cc"+keyUp+"\x0b")
	checkBuffer(t, ed.buffer, []string{strings.Repeat("a", MaxLineLength), "cc"})
	if ed.buffer.IsWordSplit(1) {
		t.Error("line 1 still marked as a split word; a later edit would glue on cc")
	}
}

// A space typed into the first word of a soft-wrapped line can let the front
// of that word move up onto the line above.
func TestTypedSpacePullsWordUp(t *testing.T) {
	lines := []string{fullLine, "abcdefghij tail"}
	_, ed := runSeeded(t, lines, []int{2}, 1, 2, 4, " ")
	checkBuffer(t, ed.buffer, []string{fullLine + " abc", "defghij tail"}, 2)
	if ed.currentLine != 2 || ed.currentCol != 1 {
		t.Errorf("cursor = (%d,%d), want (2,1)", ed.currentLine, ed.currentCol)
	}
}
