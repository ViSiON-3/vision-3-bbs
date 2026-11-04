package editor

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/editor/testterm"
)

// acRun types keys into a fresh editor with autocorrect on (unless off),
// saves, and returns the text and each toggle the editor reported.
func acRun(t *testing.T, configDir string, off bool, keys string) (string, []bool) {
	t.Helper()
	_, ed := newRunHarness(t, keys+keySave)
	var toggles []bool
	ed.EnableAutocorrect(configDir, !off, func(on bool) { toggles = append(toggles, on) })
	content, _, err := ed.Run()
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	return strings.TrimRight(content, "\r\n"), toggles
}

func TestAutocorrectTyping(t *testing.T) {
	for _, tc := range []struct{ name, keys, want string }{
		{"fixes at a space", "teh cat ", "the cat "},
		{"keeps a leading capital", "Teh end.", "The end."},
		{"fixes before punctuation", "i dont know, really", "I don't know, really"},
		{"fixes at Enter", "teh\rnext", "the\nnext"},
		{"leaves acronyms", "TEH ", "TEH "},
		{"leaves words touching digits", "teh2 2teh ", "teh2 2teh "},
		{"leaves handles and paths", "@teh /teh ", "@teh /teh "},
		{"leaves pipe codes", "|teh ", "|teh "},
		{"leaves quoted lines", " DI> teh cat ", " DI> teh cat "},
		{"only listed typos", "thier ", "their "},
		{"unknown words untouched", "phreak ", "phreak "},
		{"backspace right after puts the word back", "teh \x08cat", "teh cat"},
		{"backspace later is an ordinary backspace", "teh c\x08\x08 x", "the x"},
		{"Ctrl-O turns it off", "\x0fteh ", "teh "},
		{"and on again", "\x0f\x0fteh ", "the "},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got, _ := acRun(t, "", false, tc.keys); got != tc.want {
				t.Errorf("typed %q: got %q, want %q", tc.keys, got, tc.want)
			}
		})
	}
}

// Ctrl-O reports each change so the caller's record can keep it, and a
// caller who saved "off" starts with it off.
func TestAutocorrectToggleIsSaved(t *testing.T) {
	_, toggles := acRun(t, "", false, "x\x0f\x0f")
	if len(toggles) != 2 || toggles[0] || !toggles[1] {
		t.Errorf("toggles = %v, want [false true]", toggles)
	}
	if got, _ := acRun(t, "", true, "teh "); got != "teh " {
		t.Errorf("with the caller's choice off: got %q, want no fix", got)
	}
}

// Overwrite mode leaves words alone: a fix would type over what follows.
func TestAutocorrectNotInOverwrite(t *testing.T) {
	if got, _ := acRun(t, "", false, keyInsert+"teh "); got != "teh " {
		t.Errorf("overwrite mode: got %q, want no fix", got)
	}
}

// The board's list adds fixes and turns built-in ones off with "word -".
func TestAutocorrectBoardList(t *testing.T) {
	dir := t.TempDir()
	list := "# board fixes\nfone phone\nteh -\nbbbs BBS's\n"
	if err := os.WriteFile(filepath.Join(dir, "autocorrect.txt"), []byte(list), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, _ := acRun(t, dir, false, "fone teh bbbs "); got != "phone teh BBS's " {
		t.Errorf("board list: got %q", got)
	}
}

// Without EnableAutocorrect the editor types as before, and Ctrl-O does
// nothing.
func TestAutocorrectOffByDefault(t *testing.T) {
	_, ed := newRunHarness(t, "\x0fteh "+keySave)
	content, _, err := ed.Run()
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimRight(content, "\r\n"); got != "teh " {
		t.Errorf("got %q, want no fix", got)
	}
}

func TestBuiltinTyposParse(t *testing.T) {
	list := loadTypos("")
	if len(list) < 300 {
		t.Errorf("built-in list has %d fixes, expected the full list", len(list))
	}
	for wrong, right := range list {
		if wrong != strings.ToLower(wrong) || right == "" || strings.ContainsAny(wrong, " \t") {
			t.Errorf("bad entry %q -> %q", wrong, right)
		}
	}
}

// acScreen types keys into an editor with autocorrect on and a notice that
// stays up for notice, saves, and returns the screen, the editor and the
// row the first line of text is on.
func acScreen(t *testing.T, notice time.Duration, keys string) (*testterm.Term, *FSEditor, int) {
	t.Helper()
	tt, ed := newRunHarness(t, keys+keySave)
	ed.EnableAutocorrect("", true, nil)
	ed.noticeFor = notice
	if _, _, err := ed.Run(); err != nil {
		t.Fatalf("Run: %v", err)
	}
	return tt, ed, ed.screen.editingStartY
}

// fixColumns lists the columns on row drawn in the highlight colour.
func fixColumns(tt *testterm.Term, row int) []int {
	var cols []int
	for col := 1; col <= 80; col++ {
		if tt.Cell(row, col).Fg == 97 {
			cols = append(cols, col)
		}
	}
	return cols
}

// The fixed word is drawn in the highlight colour where it sits, and only
// the word: the text after it is back in the editor's own pen.
func TestAutocorrectHighlightsFix(t *testing.T) {
	tt, ed, row := acScreen(t, time.Hour, "ok teh cat")
	if got := fixColumns(tt, row); fmt.Sprint(got) != "[4 5 6]" {
		t.Errorf("highlighted columns = %v, want [4 5 6] (%q)", got, tt.Row(row))
	}
	if c := tt.Cell(row, 8); c.Rune != 'c' || c.Fg != -1 || c.Bg != -1 {
		t.Errorf("text after the fix = %+v, want the default pen", c)
	}
	if ed.screen.mark == nil {
		t.Error("the mark was dropped while the notice is up")
	}
}

// A highlight follows the word when the key that ended it wraps it.
func TestAutocorrectHighlightFollowsWrap(t *testing.T) {
	keys := strings.Repeat("x", 75) + " teh." // the full stop goes past the margin
	tt, _, row := acScreen(t, time.Hour, keys)
	if got := fixColumns(tt, row+1); fmt.Sprint(got) != "[1 2 3]" {
		t.Errorf("row %d: highlighted %v, want [1 2 3] (%q / %q)", row+1, got, tt.Row(row), tt.Row(row+1))
	}
	if got := fixColumns(tt, row); len(got) != 0 {
		t.Errorf("row %d still has highlight at %v", row, got)
	}
}

// When the notice clears, the word goes back to the text colour.
func TestAutocorrectHighlightClearsWithNotice(t *testing.T) {
	tt, ed, row := acScreen(t, time.Nanosecond, "teh x")
	if got := fixColumns(tt, row); len(got) != 0 {
		t.Errorf("highlight left at %v after the notice cleared", got)
	}
	if ed.screen.mark != nil {
		t.Error("mark kept after the notice cleared")
	}
	if !strings.HasPrefix(tt.Row(row), "the x") {
		t.Errorf("row = %q", tt.Row(row))
	}
}

// A word that has moved or changed is not highlighted: the mark is dropped
// and nothing is written over the text now there.
func TestAutocorrectHighlightDroppedWhenWordChanges(t *testing.T) {
	for _, tc := range []struct{ name, keys, want string }{
		{"moved", "teh \x17ab", "abthe"},
		{"changed", "teh " + keyLeft + keyLeft + "\x07", "th"}, // Ctrl-G deletes the e
	} {
		t.Run(tc.name, func(t *testing.T) {
			tt, ed, row := acScreen(t, time.Hour, tc.keys)
			if got := fixColumns(tt, row); len(got) != 0 {
				t.Errorf("highlight at %v on %q", got, tt.Row(row))
			}
			if ed.screen.mark != nil {
				t.Error("mark kept")
			}
			if !strings.HasPrefix(tt.Row(row), tc.want) {
				t.Errorf("row = %q, want %q", tt.Row(row), tc.want)
			}
		})
	}
}

// Backspace right after a fix puts the typed word back, uncoloured.
func TestAutocorrectUndoDropsHighlight(t *testing.T) {
	tt, ed, row := acScreen(t, time.Hour, "teh \x08")
	if got := fixColumns(tt, row); len(got) != 0 {
		t.Errorf("highlight at %v after undo", got)
	}
	if ed.screen.mark != nil {
		t.Error("mark kept after undo")
	}
	if tt.Row(row) != "teh" {
		t.Errorf("row = %q, want the original word", tt.Row(row))
	}
}

// The theme colour sets the highlight; a background other than black is
// drawn with it.
func TestHighlightSGR(t *testing.T) {
	for code, want := range map[int]string{15: "\x1b[0;97m", 14: "\x1b[0;93m", 0x1F: "\x1b[0;97;44m"} {
		if got := highlightSGR(code); got != want {
			t.Errorf("highlightSGR(%d) = %q, want %q", code, got, want)
		}
	}
}

// Case is carried over by character, so a board fix starting with a letter
// outside ASCII is capitalised whole.
func TestMatchCase(t *testing.T) {
	for _, tc := range []struct{ word, fix, want string }{
		{"teh", "the", "the"},
		{"Teh", "the", "The"},
		{"Cafe", "éclair", "Éclair"},
		{"i", "I", "I"},
	} {
		if got := matchCase(tc.word, tc.fix); got != tc.want {
			t.Errorf("matchCase(%q, %q) = %q, want %q", tc.word, tc.fix, got, tc.want)
		}
	}
}
