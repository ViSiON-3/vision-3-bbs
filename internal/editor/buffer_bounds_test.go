package editor

import (
	"strings"
	"testing"
)

// Every line-addressed operation rejects a line number outside 1..MaxLines
// without touching the text.
func TestMessageBuffer_OutOfRangeLinesAreRejected(t *testing.T) {
	for _, lineNum := range []int{0, -1, MaxLines + 1} {
		mb := NewMessageBuffer()
		mb.LoadContent("one\ntwo")

		mb.SetLine(lineNum, "stray")
		mb.SetHardNewline(lineNum, true)
		if got := mb.GetLine(lineNum); got != "" {
			t.Errorf("GetLine(%d) = %q, want empty", lineNum, got)
		}
		if got := mb.GetLineLength(lineNum); got != 0 {
			t.Errorf("GetLineLength(%d) = %d, want 0", lineNum, got)
		}
		if mb.IsHardNewline(lineNum) {
			t.Errorf("IsHardNewline(%d) = true, want false", lineNum)
		}
		if !mb.IsLineEmpty(lineNum) {
			t.Errorf("IsLineEmpty(%d) = false, want true", lineNum)
		}
		for name, ok := range map[string]bool{
			"InsertChar":    mb.InsertChar(lineNum, 1, 'x'),
			"OverwriteChar": mb.OverwriteChar(lineNum, 1, 'x'),
			"DeleteChar":    mb.DeleteChar(lineNum, 1),
			"InsertLine":    mb.InsertLine(lineNum),
			"DeleteLine":    mb.DeleteLine(lineNum),
			"SplitLine":     mb.SplitLine(lineNum, 1),
			"JoinLines":     mb.JoinLines(lineNum),
		} {
			if ok {
				t.Errorf("%s(%d) = true, want false", name, lineNum)
			}
		}
		if got := mb.GetContent(); got != "one\ntwo" {
			t.Errorf("content after out-of-range calls on line %d = %q, want it untouched", lineNum, got)
		}
	}
}

func TestMessageBuffer_OverwriteChar(t *testing.T) {
	for _, tc := range []struct {
		name string
		col  int
		want string
	}{
		{"replaces the character under the cursor", 2, "aXc"},
		{"at the end appends", 4, "abcX"},
		{"past the end pads with spaces", 6, "abc  X"},
		{"column below 1 is treated as 1", 0, "Xbc"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mb := NewMessageBuffer()
			mb.LoadContent("abc")
			if !mb.OverwriteChar(1, tc.col, 'X') {
				t.Fatal("OverwriteChar returned false")
			}
			if got := mb.GetLine(1); got != tc.want {
				t.Errorf("line = %q, want %q", got, tc.want)
			}
		})
	}

	// Overwriting on a line past the current end grows the buffer to reach it.
	mb := NewMessageBuffer()
	mb.OverwriteChar(3, 1, 'X')
	if mb.GetLineCount() != 3 || mb.GetLine(3) != "X" {
		t.Errorf("count=%d line 3=%q, want 3 lines with \"X\" on the third", mb.GetLineCount(), mb.GetLine(3))
	}
}

// Unlike OverwriteChar, InsertChar clamps the column to the line: inserting
// past the end appends without padding.
func TestMessageBuffer_InsertCharClampsColumn(t *testing.T) {
	mb := NewMessageBuffer()
	mb.LoadContent("abc")

	if !mb.InsertChar(1, 6, 'X') || mb.GetLine(1) != "abcX" {
		t.Errorf("insert past the end: line = %q, want %q", mb.GetLine(1), "abcX")
	}
	if !mb.InsertChar(1, 0, 'Y') || mb.GetLine(1) != "YabcX" {
		t.Errorf("insert at column 0: line = %q, want %q", mb.GetLine(1), "YabcX")
	}
	// Inserting on a line past the current end grows the buffer to reach it.
	if !mb.InsertChar(3, 1, 'Z') || mb.GetLineCount() != 3 || mb.GetLine(3) != "Z" {
		t.Errorf("count=%d line 3=%q, want 3 lines with \"Z\" on the third", mb.GetLineCount(), mb.GetLine(3))
	}
}

func TestMessageBuffer_DeleteCharOutsideTheLine(t *testing.T) {
	mb := NewMessageBuffer()
	mb.LoadContent("abc")
	for _, col := range []int{0, 4} {
		if mb.DeleteChar(1, col) {
			t.Errorf("DeleteChar(1, %d) = true, want false", col)
		}
	}
	if got := mb.GetLine(1); got != "abc" {
		t.Errorf("line = %q, want %q", got, "abc")
	}
}

// Content longer than the buffer is cut at MaxLines rather than overflowing.
func TestMessageBuffer_LoadContentStopsAtMaxLines(t *testing.T) {
	mb := NewMessageBuffer()
	mb.LoadContent(numberedLines(MaxLines + 20))

	if got := mb.GetLineCount(); got != MaxLines {
		t.Errorf("GetLineCount() = %d, want %d", got, MaxLines)
	}
	lines := strings.Split(mb.GetContent(), "\n")
	if len(lines) != MaxLines || lines[MaxLines-1] != "line 100" {
		t.Errorf("content ends %q after %d lines, want \"line 100\" after %d", lines[len(lines)-1], len(lines), MaxLines)
	}

	// A full buffer has no room for another line, by insert or by split.
	if mb.InsertLine(1) {
		t.Error("InsertLine succeeded on a full buffer")
	}
	if mb.SplitLine(1, 3) {
		t.Error("SplitLine succeeded on a full buffer")
	}
	if got := mb.GetLine(1); got != "line 1" {
		t.Errorf("line 1 = %q, want it unsplit", got)
	}
}

// Deleting the only line leaves one empty line, never zero.
func TestMessageBuffer_DeleteLastRemainingLine(t *testing.T) {
	mb := NewMessageBuffer()
	mb.LoadContent("only")
	if !mb.DeleteLine(1) {
		t.Fatal("DeleteLine(1) returned false")
	}
	if mb.GetLineCount() != 1 || mb.GetLine(1) != "" {
		t.Errorf("count=%d line 1=%q, want a single empty line", mb.GetLineCount(), mb.GetLine(1))
	}
}
