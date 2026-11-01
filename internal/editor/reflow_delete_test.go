package editor

import (
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/editor/testterm"
)

// fullLine is a 71-column soft-wrapped line: "listic" (6) fits after it, but
// "supercalifragilistic" does not.
var fullLine = strings.TrimSpace(strings.Repeat("abc ", 18))

const longWord = "supercalifragilistic"

// checkBuffer compares the buffer's lines and hard-newline flags.
func checkBuffer(t *testing.T, mb *MessageBuffer, want []string, hard ...int) {
	t.Helper()
	got := make([]string, 0, mb.GetLineCount())
	for i := 1; i <= mb.GetLineCount(); i++ {
		got = append(got, mb.GetLine(i))
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("lines:\n got %q\nwant %q", got, want)
	}
	isHard := map[int]bool{}
	for _, h := range hard {
		isHard[h] = true
	}
	for i := 1; i <= mb.GetLineCount(); i++ {
		if mb.IsHardNewline(i) != isHard[i] {
			t.Errorf("line %d (%q): hard = %v, want %v", i, mb.GetLine(i), mb.IsHardNewline(i), isHard[i])
		}
	}
}

// Issue #853, cause 1: removing the start of a soft-wrapped line's first word
// lets what is left of it move back up.
func TestHandleDelete_StartOfSoftLinePullsBack(t *testing.T) {
	mb, ww := setupBuffer([]string{fullLine, longWord + " tail words here"}, 2)
	line, col := 2, 1
	for i := 0; i < 14; i++ {
		var changed bool
		line, col, changed = ww.HandleDelete(line, col)
		if !changed {
			t.Fatalf("delete %d made no change", i)
		}
	}
	checkBuffer(t, mb, []string{fullLine + " listic", "tail words here"}, 2)
	if line != 1 || col != len(fullLine)+2 {
		t.Errorf("cursor = (%d,%d), want (1,%d): just before the word that moved up", line, col, len(fullLine)+2)
	}
}

func TestHandleBackspace_ShortenedWordPullsBack(t *testing.T) {
	mb, ww := setupBuffer([]string{fullLine, longWord}, 2)
	line, col := 2, len(longWord)+1
	for i := 0; i < 14; i++ {
		line, col, _ = ww.HandleBackspace(line, col)
	}
	checkBuffer(t, mb, []string{fullLine + " superc"}, 1)
	if line != 1 || col != len(fullLine)+8 {
		t.Errorf("cursor = (%d,%d), want (1,%d): after the word that moved up", line, col, len(fullLine)+8)
	}
}

// Ctrl-T pulls back too, and a hard break above the paragraph stops the walk.
func TestDeleteWord_PullsBackAndStopsAtHardBreak(t *testing.T) {
	mb, ww := setupBuffer([]string{"own paragraph", fullLine, "x" + longWord + " tail"}, 1, 3)
	line, col, changed := ww.DeleteWord(3, 1)
	if !changed {
		t.Fatal("DeleteWord made no change")
	}
	checkBuffer(t, mb, []string{"own paragraph", fullLine + "  tail"}, 1, 2)
	if line != 2 || col != len(fullLine)+2 {
		t.Errorf("cursor = (%d,%d), want (2,%d)", line, col, len(fullLine)+2)
	}
}

// The walk goes to the top of the paragraph, so rows left short further up by
// an earlier Ctrl-K refill as well.
func TestDeleteRefillsWholeParagraph(t *testing.T) {
	mb, ww := setupBuffer([]string{"short", "words here", "more words"}, 3)
	line, col, _ := ww.HandleBackspace(3, 11)
	checkBuffer(t, mb, []string{"short words here more word"}, 1)
	if line != 1 || col != 27 {
		t.Errorf("cursor = (%d,%d), want (1,27)", line, col)
	}
}

// Issue #853, cause 3: a join removes one line boundary, not the next one.
func TestJoinAcrossHardBreakKeepsNextParagraph(t *testing.T) {
	lines := []string{"line one", "line two", "line three", "line four"}
	t.Run("backspace at column 1", func(t *testing.T) {
		mb, ww := setupBuffer(lines, 1, 2, 3, 4)
		line, col, changed := ww.HandleBackspace(2, 1)
		if !changed || line != 1 || col != 9 {
			t.Fatalf("got (%d,%d,%v), want (1,9,true)", line, col, changed)
		}
		checkBuffer(t, mb, []string{"line oneline two", "line three", "line four"}, 1, 2, 3)
	})
	t.Run("delete at end of line", func(t *testing.T) {
		mb, ww := setupBuffer(lines, 1, 2, 3, 4)
		line, col, changed := ww.HandleDelete(1, 9)
		if !changed || line != 1 || col != 9 {
			t.Fatalf("got (%d,%d,%v), want (1,9,true)", line, col, changed)
		}
		checkBuffer(t, mb, []string{"line oneline two", "line three", "line four"}, 1, 2, 3)
	})
	t.Run("a soft line that is joined still flows on", func(t *testing.T) {
		mb, ww := setupBuffer([]string{"one", "two", "three", "four"}, 1, 3, 4)
		ww.HandleBackspace(2, 1)
		checkBuffer(t, mb, []string{"onetwo three", "four"}, 1, 2)
	})
}

// Blank lines and hard returns stay paragraph boundaries, in both directions.
func TestDeleteStaysInsideParagraph(t *testing.T) {
	mb, ww := setupBuffer([]string{"first paragraph", "", longWord, "", "last"}, 1, 2, 3, 4, 5)
	if _, _, changed := ww.HandleBackspace(3, 3); !changed {
		t.Fatal("no change")
	}
	checkBuffer(t, mb, []string{"first paragraph", "", "s" + longWord[2:], "", "last"}, 1, 2, 3, 4, 5)

	// A line above that ended in a hard return has room but takes nothing.
	mb, ww = setupBuffer([]string{"short", "words here"}, 1, 2)
	ww.HandleDelete(2, 1)
	checkBuffer(t, mb, []string{"short", "ords here"}, 1, 2)
}

// A word longer than the margin is split with no separator space. Reflowing
// from above the split would write one into the word, so the walk back stops
// at such a line: it is full, so nothing could be pulled onto it anyway.
func TestDeleteBelowForceBrokenWordLeavesItWhole(t *testing.T) {
	head := strings.Repeat("x", MaxLineLength)
	mb, ww := setupBuffer([]string{"before", head, "xxxx tail"}, 3)
	line, col, _ := ww.HandleBackspace(3, 10)
	checkBuffer(t, mb, []string{"before", head, "xxxx tai"}, 3)
	if line != 3 || col != 9 {
		t.Errorf("cursor = (%d,%d), want (3,9)", line, col)
	}
}

// Byte lengths set the margin, as everywhere in WordWrapper, so a multi-byte
// word that would fit by characters but not by bytes stays down.
func TestPullBackUsesTheWrappersByteMargin(t *testing.T) {
	word := "ééé" // 3 characters, 6 bytes: 71+1+6 = 78 fits
	mb, ww := setupBuffer([]string{fullLine, "a" + word + " tail"}, 2)
	ww.HandleDelete(2, 1)
	checkBuffer(t, mb, []string{fullLine + " " + word, "tail"}, 2)

	word = "éééé" // 4 characters, 8 bytes: 71+1+8 = 80 does not
	mb, ww = setupBuffer([]string{fullLine, "a" + word + " tail"}, 2)
	ww.HandleDelete(2, 1)
	checkBuffer(t, mb, []string{fullLine, word + " tail"}, 2)
}

// --- Through the real editor on the fake terminal ---

// runSeeded runs the editor over the given lines, with the cursor placed, until
// its keys run out. It returns the terminal and editor in their final state.
func runSeeded(t *testing.T, lines []string, hard []int, top, line, col int, keys string) (*testterm.Term, *FSEditor) {
	t.Helper()
	// A plain reader, unlike testterm.Session, reports EOF when the keys run
	// out, so Run returns with the screen as the last key left it.
	tt := testterm.New(80, 24)
	ih := NewInputHandler(strings.NewReader(keys))
	ed := NewFSEditor(testterm.NewSession(nil, ""), tt, ansi.OutputModeUTF8, 80, 24, "", "", "", "", "", "", ih)
	for i, l := range lines {
		ed.buffer.SetLine(i+1, l)
	}
	for _, h := range hard {
		ed.buffer.SetHardNewline(h, true)
	}
	ed.topLine, ed.lastTopLine = top, top
	ed.currentLine, ed.currentCol = line, col
	if _, _, err := ed.Run(); !errors.Is(err, io.EOF) {
		t.Fatalf("Run: %v, want EOF when the keys run out", err)
	}
	return tt, ed
}

// checkScreen requires every editing row to show the buffer, including rows
// that are now past its end, and the terminal's cursor to be where the editor
// says it is.
func checkScreen(t *testing.T, tt *testterm.Term, ed *FSEditor, wantLine, wantCol, wantTop int) {
	t.Helper()
	if ed.currentLine != wantLine || ed.currentCol != wantCol || ed.topLine != wantTop {
		t.Errorf("cursor (%d,%d) top %d, want (%d,%d) top %d",
			ed.currentLine, ed.currentCol, ed.topLine, wantLine, wantCol, wantTop)
	}
	startY := ed.screen.GetEditingStartY()
	for i := 0; i < ed.screen.GetScreenLines(); i++ {
		want := ""
		if n := ed.topLine + i; n <= ed.buffer.GetLineCount() {
			want = ed.buffer.GetLine(n)
		}
		if got := tt.Row(startY + i); got != strings.TrimRight(want, " ") {
			t.Errorf("row %d = %q, want %q", startY+i, got, want)
		}
	}
	row, col := tt.Cursor()
	if wantRow := startY + wantLine - wantTop; row != wantRow || col != wantCol {
		t.Errorf("terminal cursor (%d,%d), want (%d,%d)", row, col, wantRow, wantCol)
	}
}

const (
	ctrlGDelete    = "\x07" // Ctrl-G, the editor's Delete
	ctrlHBackspace = "\x08"
)

// The changed rows include one above the cursor, the cursor's own and the ones
// below that shift up, with the row they leave blanked.
func TestRunDeletePullsWordBackOnScreen(t *testing.T) {
	tt, ed := runSeeded(t,
		[]string{"intro", fullLine, longWord, "next paragraph", "last paragraph"},
		[]int{1, 3, 4, 5}, 1, 3, 1, strings.Repeat(ctrlGDelete, 14))
	checkBuffer(t, ed.buffer, []string{"intro", fullLine + " listic", "next paragraph", "last paragraph"}, 1, 2, 3, 4)
	checkScreen(t, tt, ed, 2, len(fullLine)+2, 1)
}

// When the cursor's row was the top of the view, the view scrolls up to follow.
func TestRunPullBackAboveViewScrolls(t *testing.T) {
	var lines []string
	var hard []int
	for i := 1; i <= 40; i++ {
		lines = append(lines, "filler")
		if i != 10 {
			hard = append(hard, i)
		}
	}
	lines[9], lines[10] = fullLine, longWord+" tail"
	tt, ed := runSeeded(t, lines, hard, 11, 11, 1, strings.Repeat(ctrlGDelete, 14))
	if got, want := ed.buffer.GetLine(10), fullLine+" listic"; got != want {
		t.Fatalf("line 10 = %q, want %q", got, want)
	}
	checkScreen(t, tt, ed, 10, len(fullLine)+2, 10)
}

func TestRunEditOnFirstLine(t *testing.T) {
	first := fullLine + " abcdef"
	t.Run("the next word stays down until it fits", func(t *testing.T) {
		tt, ed := runSeeded(t, []string{first, "tail words"}, []int{2}, 1, 1, len(first)+1,
			strings.Repeat(ctrlHBackspace, 3))
		checkBuffer(t, ed.buffer, []string{fullLine + " abc", "tail words"}, 2)
		checkScreen(t, tt, ed, 1, len(fullLine)+5, 1)
	})
	t.Run("then it comes up", func(t *testing.T) {
		tt, ed := runSeeded(t, []string{first, "tail words"}, []int{2}, 1, 1, len(first)+1,
			strings.Repeat(ctrlHBackspace, 4))
		checkBuffer(t, ed.buffer, []string{fullLine + " ab tail", "words"}, 2)
		checkScreen(t, tt, ed, 1, len(fullLine)+4, 1)
	})
	t.Run("backspace at the very start does nothing", func(t *testing.T) {
		tt, ed := runSeeded(t, []string{"one", "two"}, []int{1, 2}, 1, 1, 1, ctrlHBackspace)
		checkBuffer(t, ed.buffer, []string{"one", "two"}, 1, 2)
		checkScreen(t, tt, ed, 1, 1, 1)
	})
}

// Emptying a line leaves the cursor on it. The soft break above still stands
// for the space between words, so the next word typed must not fuse onto the
// line above.
func TestRunEmptiedLineKeepsCursorAndWordBreak(t *testing.T) {
	prev := strings.Repeat("x", 39) + " " + strings.Repeat("y", 38) // 78: "c" does not fit
	tt, ed := runSeeded(t, []string{prev, "c", "after"}, []int{2, 3}, 1, 2, 2, ctrlHBackspace)
	checkBuffer(t, ed.buffer, []string{prev, "", "after"}, 2, 3)
	checkScreen(t, tt, ed, 2, 1, 1)

	_, ed = runSeeded(t, []string{prev, "c", "after"}, []int{2, 3}, 1, 2, 2, ctrlHBackspace+"d")
	checkBuffer(t, ed.buffer, []string{prev, "d", "after"}, 2, 3)
}

// Joining two hard lines leaves the others on rows of their own.
func TestRunBackspaceJoinOnScreen(t *testing.T) {
	tt, ed := runSeeded(t, []string{"line one", "line two", "line three", "line four"},
		[]int{1, 2, 3, 4}, 1, 2, 1, ctrlHBackspace)
	checkBuffer(t, ed.buffer, []string{"line oneline two", "line three", "line four"}, 1, 2, 3)
	checkScreen(t, tt, ed, 1, 9, 1)
}

// --- Loaded content ---

// Text opened and saved without an edit comes back byte for byte, whatever it
// holds.
func TestRunLoadThenSaveIsByteIdentical(t *testing.T) {
	nearMargin := strings.TrimSpace(strings.Repeat("word ", 15)) + " endings"
	for _, tc := range []struct{ name, content string }{
		{"wrapped paragraph", nearMargin + "\nhunter who chased it\n\nnext paragraph"},
		{"quote block", "Bucko wrote:\nBu> " + nearMargin + "\nBu> more text\n-- end of Bucko --"},
		{"list", "- " + nearMargin + "\n- second item\n1. third"},
		{"trailer", "body\n--- ViSiON/3\n * Origin: Test BBS (1:2/3)"},
		{"ansi art", "\x1b[1;31m" + strings.Repeat("=", 70) + "\x1b[0m\n\x1b[32mart\x1b[0m"},
		{"plain art", strings.Repeat("=", 79) + "\n" + strings.Repeat("=", 79)},
		{"pipe codes", "|09" + nearMargin + "\n|07tail"},
		{"unicode", "ééé ░▒▓ " + nearMargin + "\n日本語"},
		{"inner spaces", "two  spaces \n  indented"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, saved := runKeys(t, tc.content, "\x1a")
			if !saved || got != tc.content {
				t.Fatalf("saved=%v\n got %q\nwant %q", saved, got, tc.content)
			}
		})
	}
}

// Loaded text has no record of where its author pressed Enter, so every loaded
// line keeps its break. The auto-signature, the only text loaded today, is the
// case in point: these two lines look just like a wrapped paragraph, and
// pulling the address up would ruin the signature.
func TestRunEditingLoadedTextKeepsItsLines(t *testing.T) {
	first := "Robbie W. - SysOp of the ViSiON/3 test board, running since the 1990s"
	second := "telnet://vision3.example.org:2323"
	if len(first)+1+len(second) <= MaxLineLength {
		t.Fatal("setup: the second line must not fit after the first")
	}
	// Up to the first line, End, Backspace, save.
	got, saved := runKeys(t, first+"!\n"+second, "\x05\x10"+ctrlHBackspace+"\x1a")
	if want := first + "\n" + second; !saved || got != want {
		t.Fatalf("saved=%v\n got %q\nwant %q", saved, got, want)
	}
}

// --- Quotes ---

// A block quoted into the middle of a wrapped paragraph ends the paragraph
// above it. With no banner in between (a networked area), an edit there must
// not pull quoted lines into the caller's words.
func TestRunQuotedLinesStayOutOfCallersParagraph(t *testing.T) {
	first := strings.TrimSpace(strings.Repeat("mine ", 15))
	// Type a wrapped paragraph, quote a line above its second row, then go to
	// the end of the first row and delete a character.
	sess := testterm.NewSession(nil, first+" "+longWord+"\x11 \x11"+"\x12\x10"+ctrlHBackspace+"\x1a")
	ed := NewFSEditor(sess, io.Discard, ansi.OutputModeUTF8, 80, 24, "", "", "", "", "", "", nil)
	ed.SetNetworkQuoteStrings("", "")
	ed.SetQuoteData(&QuoteData{From: "Bucko", Title: "t", Lines: []string{"quoted text"}})
	content, saved, err := ed.Run()
	if err != nil || !saved {
		t.Fatalf("Run: saved=%v err=%v", saved, err)
	}
	lines := strings.Split(content, "\n")
	if len(lines) < 2 || lines[0] != first[:len(first)-1] || !strings.HasSuffix(lines[1], "> quoted text") {
		t.Fatalf("quoted line was not kept apart: %q", lines)
	}
}
