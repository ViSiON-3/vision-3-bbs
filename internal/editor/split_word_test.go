package editor

import (
	"strings"
	"testing"
)

// typeWord types s into an empty buffer one character at a time, wrapping as
// the editor does.
func typeWord(mb *MessageBuffer, ww *WordWrapper, s string) (int, int) {
	line, col := 1, 1
	for _, r := range s {
		mb.InsertChar(line, col, r)
		col++
		line, col = ww.WrapAfterInsert(line, col)
	}
	return line, col
}

// Issue #867: editing the front of a word split at the margin must not write a
// space into the word when the two parts are rejoined.
func TestEditFrontOfSplitWordKeepsItWhole(t *testing.T) {
	word := strings.Repeat("a", MaxLineLength) + "bbbb"
	t.Run("backspace", func(t *testing.T) {
		mb, ww := setupBuffer(nil)
		typeWord(mb, ww, word)
		line, col, _ := ww.HandleBackspace(1, 11)
		a := strings.Repeat("a", MaxLineLength-1)
		checkBuffer(t, mb, []string{a + "b", "bbb"})
		if !mb.IsWordSplit(1) {
			t.Error("line 1 lost its word-split flag")
		}
		if line != 1 || col != 10 {
			t.Errorf("cursor = (%d,%d), want (1,10)", line, col)
		}
	})
	t.Run("insert", func(t *testing.T) {
		mb, ww := setupBuffer(nil)
		typeWord(mb, ww, word)
		mb.InsertChar(1, 5, 'c')
		ww.WrapAfterInsert(1, 6)
		want := "aaaac" + word[4:]
		checkBuffer(t, mb, []string{want[:MaxLineLength], want[MaxLineLength:]})
	})
}

// A delete on the tail of a split word reflows from the top of the paragraph,
// across the split, and still leaves the word in one piece.
func TestDeleteOnTailOfSplitWordKeepsItWhole(t *testing.T) {
	mb, ww := setupBuffer(nil)
	typeWord(mb, ww, "lead "+strings.Repeat("a", MaxLineLength)+"bbbb tail")
	// lead / a*79 (split) / bbbb tail
	if !mb.IsWordSplit(2) {
		t.Fatalf("line 2 should be word-split; lines %q %q %q", mb.GetLine(1), mb.GetLine(2), mb.GetLine(3))
	}
	ww.HandleDelete(3, 1)
	checkBuffer(t, mb, []string{"lead", strings.Repeat("a", MaxLineLength), "bbb tail"})
}

// Enter on a split line makes a real break: the word-split flag moves with the
// text after the cursor, and the hard line before it does not keep it.
func TestSplitLineMovesWordSplitFlag(t *testing.T) {
	mb, ww := setupBuffer(nil)
	typeWord(mb, ww, strings.Repeat("a", MaxLineLength)+"bbbb")
	mb.SplitLine(1, 10)
	if mb.IsWordSplit(1) || !mb.IsWordSplit(2) {
		t.Errorf("flags after split: %v %v, want false true", mb.IsWordSplit(1), mb.IsWordSplit(2))
	}
	mb.JoinLines(1)
	if !mb.IsWordSplit(1) {
		t.Error("join should take the second line's word-split flag")
	}
}

// Ctrl-J on the front of a split word puts the word back together whole. The
// rewrap splits it again at the margin, so the break must still be marked as
// inside the word, or a later edit would put a space there.
func TestCtrlJRejoinsSplitWordWithoutSpace(t *testing.T) {
	word := strings.Repeat("a", MaxLineLength) + "bbbb"
	_, ed := runSeeded(t, nil, nil, 1, 1, 1, word+keyUp+"\x0a")
	checkBuffer(t, ed.buffer, []string{word[:MaxLineLength], "bbbb"})
	if !ed.buffer.IsWordSplit(1) {
		t.Error("line 1 is no longer marked as a split word")
	}
}
