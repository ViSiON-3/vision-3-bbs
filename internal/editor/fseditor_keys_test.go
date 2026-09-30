package editor

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/editor/testterm"
)

// Key sequences as a terminal sends them, for scripting Run.
const (
	keyUp     = "\x1b[A"
	keyDown   = "\x1b[B"
	keyRight  = "\x1b[C"
	keyLeft   = "\x1b[D"
	keyHome   = "\x1b[H"
	keyEnd    = "\x1b[F"
	keyPgUp   = "\x1b[5~"
	keyPgDn   = "\x1b[6~"
	keyInsert = "\x1b[2~"
	keyDel    = "\x1b[3~"
	keySave   = "\x1a" // CTRL-Z
	keyAbort  = "\x01" // CTRL-A
	keyEsc    = "\x1b"
)

// newRunHarness builds an 80x24 editor with no menu set (so the built-in
// header and no footer) drawing to a fake terminal and fed the scripted keys.
func newRunHarness(t *testing.T, keys string) (*testterm.Term, *FSEditor) {
	t.Helper()
	tt := testterm.New(80, 24)
	sess := testterm.NewSession(tt, keys)
	ed := NewFSEditor(sess, tt, ansi.OutputModeUTF8, 80, 24, "", "", "", "", "", "", nil)
	ed.input.SetEscTimeout(10 * time.Millisecond) // don't wait out the real ESC window
	return tt, ed
}

// runKeys loads initial into a fresh editor, plays keys and returns what Run
// reports.
func runKeys(t *testing.T, initial, keys string) (content string, saved bool) {
	t.Helper()
	_, ed := newRunHarness(t, keys)
	ed.LoadContent(initial)
	content, saved, err := ed.Run()
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	return content, saved
}

// Each case types into an empty editor and saves; the saved text shows where
// the cursor went and what the edit did.
func TestRunNavigationAndEditKeys(t *testing.T) {
	for _, tc := range []struct {
		name, keys, want string
	}{
		{"left arrow steps back one column", "ac" + keyLeft + "b", "abc"},
		{"left arrow at column 1 wraps to the end of the previous line", "ab\rcd" + keyHome + keyLeft + "X", "abX\ncd"},
		{"left arrow at the very start stays put", "ab" + keyHome + keyLeft + "X", "Xab"},
		{"right arrow at end of line wraps to the next line", "ab\rcd" + keyUp + keyRight + "X", "ab\nXcd"},
		{"right arrow steps forward one column", "abc" + keyHome + keyRight + "X", "aXbc"},
		{"right arrow at the very end stays put", "ab" + keyRight + "X", "abX"},
		{"up arrow clamps the column to a shorter line", "a\rlonger" + keyUp + "X", "aX\nlonger"},
		{"up arrow on the first line stays put", "ab" + keyUp + "X", "abX"},
		{"down arrow clamps the column to a shorter line", "longer\ra" + keyUp + keyEnd + keyDown + "X", "longer\naX"},
		{"down arrow on the last line stays put", "ab" + keyDown + "X", "abX"},
		{"home and end", "bc" + keyHome + "a" + keyEnd + "d", "abcd"},
		{"ctrl-f moves to the next word", "one two" + keyHome + "\x06" + "X", "one Xtwo"},
		{"tab inserts four spaces", "a\tb", "a    b"},
		{"insert key switches to overwrite", "abc" + keyHome + keyInsert + "XY", "XYc"},
		{"overwrite past the end appends", "ab" + keyInsert + "cd", "abcd"},
		{"insert key twice returns to insert mode", "abc" + keyHome + keyInsert + keyInsert + "X", "Xabc"},
		{"delete key removes the character under the cursor", "abc" + keyHome + keyDel, "bc"},
		{"delete key at end of the last line does nothing", "abc" + keyDel, "abc"},
		// The word goes; the space that followed it stays.
		{"ctrl-t deletes the word to the right", "one two three" + keyHome + "\x14", " two three"},
		{"ctrl-t at end of line does nothing", "one two" + "\x14", "one two"},
		{"ctrl-y deletes the line", "one\rtwo" + "\x19", "one"},
		{"ctrl-n splits the line at the cursor", "onetwo" + keyLeft + keyLeft + keyLeft + "\x0e", "one\ntwo"},
		{"ctrl-j joins the next line on", "one \rtwo" + keyUp + "\x0a", "one two"},
		{"ctrl-j on the last line does nothing", "one\rtwo" + "\x0a", "one\ntwo"},
		// Ctrl-J joins words, so it supplies the space the line break stood for
		// (#516), but only where there is none already.
		{"ctrl-j puts a space between words", "one\rtwo" + keyUp + "\x0a", "one two"},
		{"ctrl-j keeps leading space on the next line", "one\r  two" + keyUp + "\x0a", "one  two"},
		{"ctrl-j onto an empty line adds no space", "\rtwo" + keyUp + "\x0a", "two"},
		{"ctrl-j with an empty next line adds no space", "one\r" + keyUp + "\x0a", "one"},
		{"unbound control key is ignored", "a\x0fb", "ab"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			content, saved := runKeys(t, "", tc.keys+keySave)
			if !saved || content != tc.want {
				t.Errorf("Run = (%q, saved=%v), want (%q, true)", content, saved, tc.want)
			}
		})
	}
}

// numberedLines returns "line 1" .. "line n" joined by newlines.
func numberedLines(n int) string {
	lines := make([]string, n)
	for i := range lines {
		lines[i] = fmt.Sprintf("line %d", i+1)
	}
	return strings.Join(lines, "\n")
}

// markedLine reports which line of content the typed marker landed on.
func markedLine(t *testing.T, content string) string {
	t.Helper()
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(line, "X") {
			return strings.TrimPrefix(line, "X")
		}
	}
	t.Fatalf("no line carries the marker:\n%s", content)
	return ""
}

// An 80x24 screen with the default six-row header has 17 editing rows, and a
// page is one row less than that so a line of context carries over.
func TestRunPageKeysMoveAPageAtATime(t *testing.T) {
	for _, tc := range []struct {
		name, keys, want string
	}{
		// LoadContent leaves the cursor on the last of the 40 lines.
		{"page up from the end", keyPgUp, "line 24"},
		{"page up twice", keyPgUp + keyPgUp, "line 8"},
		{"page up stops at the first line", keyPgUp + keyPgUp + keyPgUp, "line 1"},
		{"page down from the top", keyPgUp + keyPgUp + keyPgUp + keyPgDn, "line 17"},
		{"page down stops at the last line", keyPgUp + keyPgDn + keyPgDn, "line 40"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			content, _ := runKeys(t, numberedLines(40), tc.keys+keyHome+"X"+keySave)
			if got := markedLine(t, content); got != tc.want {
				t.Errorf("marker landed on %q, want %q", got, tc.want)
			}
		})
	}
}

// Moving the cursor out of the visible window has to scroll the text with it.
func TestRunScrollsToKeepTheCursorVisible(t *testing.T) {
	tt, ed := newRunHarness(t, keyPgUp+keyPgUp+keyPgUp+keySave)
	ed.LoadContent(numberedLines(40))
	if _, _, err := ed.Run(); err != nil {
		t.Fatalf("Run: %v", err)
	}

	first := ed.screen.GetEditingStartY()
	if got := tt.Row(first); got != "line 1" {
		t.Errorf("first editing row = %q, want %q after paging to the top", got, "line 1")
	}
	last := first + ed.screen.GetScreenLines() - 1
	if got, want := tt.Row(last), fmt.Sprintf("line %d", ed.screen.GetScreenLines()); got != want {
		t.Errorf("last editing row = %q, want %q", got, want)
	}
}

// CTRL-B rewraps the paragraph the cursor is in and leaves the cursor at the
// start of its line. A line can only be over-long when it was loaded that way,
// as a quoted or imported message is.
func TestRunReformatParagraph(t *testing.T) {
	long := strings.TrimSpace(strings.Repeat("word ", 30))
	if len(long) <= MaxLineLength {
		t.Fatalf("setup: line is %d columns, want more than %d", len(long), MaxLineLength)
	}
	content, _ := runKeys(t, long, "\x02"+"X"+keySave)

	lines := strings.Split(content, "\n")
	if len(lines) != 2 {
		t.Fatalf("reformatted into %d lines, want 2:\n%s", len(lines), content)
	}
	for i, line := range lines {
		if len(line) > MaxLineLength {
			t.Errorf("line %d is %d columns, want <= %d: %q", i+1, len(line), MaxLineLength, line)
		}
	}
	if got := strings.Join(strings.Fields(content), " "); got != "X"+long {
		t.Errorf("reformat changed the words:\n got %q\nwant %q", got, "X"+long)
	}
}

// CTRL-L repaints from scratch, so line noise on the screen goes away.
func TestCtrlLRedrawsTheScreen(t *testing.T) {
	tt, ed := newRunHarness(t, "")
	defer ed.input.CloseAndWait()
	ed.LoadContent("hello")
	ed.redrawScreen()

	row := ed.screen.GetEditingStartY()
	ed.screen.GoXY(1, row)
	ed.screen.WriteDirect("#### line noise ####")
	ed.screen.GoXY(1, row+3)
	ed.screen.WriteDirect("more noise")

	ed.handleKey(KeyCtrlL)

	if got := tt.Row(row); got != "hello" {
		t.Errorf("editing row after redraw = %q, want %q", got, "hello")
	}
	if got := tt.Row(row + 3); got != "" {
		t.Errorf("blank row after redraw = %q, want empty", got)
	}
	if r, c := tt.Cursor(); r != row || c != 6 {
		t.Errorf("Cursor() = (%d,%d), want (%d,6) — after the loaded text", r, c, row)
	}
}

// A resize recomputes the editing area for the new height and repaints.
func TestHandleResizeRepaintsAtTheNewSize(t *testing.T) {
	tt := testterm.New(100, 40)
	sess := testterm.NewSession(tt, "")
	ed := NewFSEditor(sess, tt, ansi.OutputModeUTF8, 80, 24, "", "", "", "", "", "", nil)
	defer ed.input.CloseAndWait()
	ed.LoadContent("hello")
	if got := ed.screen.GetScreenLines(); got != 17 {
		t.Fatalf("GetScreenLines() at 80x24 = %d, want 17", got)
	}

	ed.HandleResize(100, 40)

	if got := ed.screen.GetScreenLines(); got != 33 {
		t.Errorf("GetScreenLines() at 100x40 = %d, want 33", got)
	}
	if got := ed.screen.PromptRow(); got != 40 {
		t.Errorf("PromptRow() at 100x40 = %d, want 40", got)
	}
	if got := tt.Row(ed.screen.GetEditingStartY()); got != "hello" {
		t.Errorf("editing row after resize = %q, want %q", got, "hello")
	}
}

// CTRL-Z on an empty message is refused with a notice; any key dismisses it
// and editing carries on.
func TestRunRefusesToSaveAnEmptyMessage(t *testing.T) {
	// The 'k' after the first CTRL-Z only dismisses the notice.
	tt, ed := newRunHarness(t, keySave+"k"+"hi"+keySave)
	content, saved, err := ed.Run()
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !saved || content != "hi" {
		t.Errorf("Run = (%q, saved=%v), want (%q, true)", content, saved, "hi")
	}
	if got := tt.Row(ed.screen.PromptRow()); got != "Saving..." {
		t.Errorf("prompt row = %q, want %q", got, "Saving...")
	}
}

func TestRunAbort(t *testing.T) {
	for _, tc := range []struct {
		name, answer string
		wantSaved    bool
	}{
		{"Y aborts", "y", false},
		{"N carries on", "n", true},
		{"Enter takes the default, No", "\r", true},
		{"arrow to Yes then Enter aborts", keyLeft + "\r", false},
		{"arrow there and back then space carries on", keyRight + keyRight + " ", true},
		{"other keys are ignored until an answer", "zq" + "y", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// When the abort is declined the trailing CTRL-Z saves; when it is
			// confirmed Run has already returned and never reads it.
			content, saved := runKeys(t, "", "draft"+keyAbort+tc.answer+keySave)
			if saved != tc.wantSaved {
				t.Errorf("saved = %v, want %v", saved, tc.wantSaved)
			}
			// An aborted message is still handed back; the caller decides what
			// to do with it from the saved flag.
			if content != "draft" {
				t.Errorf("content = %q, want %q", content, "draft")
			}
		})
	}
}

// The Escape menu opens on "Edit"; arrows move the bar and Enter runs the choice.
func TestRunEscapeMenu(t *testing.T) {
	t.Run("default choice returns to editing", func(t *testing.T) {
		content, saved := runKeys(t, "", "ab"+keyEsc+"\r"+"c"+keySave)
		if !saved || content != "abc" {
			t.Errorf("Run = (%q, saved=%v), want (%q, true)", content, saved, "abc")
		}
	})
	t.Run("escape closes the menu", func(t *testing.T) {
		content, saved := runKeys(t, "", "ab"+keyEsc+keyLeft+keyEsc+"c"+keySave)
		if !saved || content != "abc" {
			t.Errorf("Run = (%q, saved=%v), want (%q, true)", content, saved, "abc")
		}
	})
	t.Run("save", func(t *testing.T) {
		// Edit -> Abort -> Save; a third Left is absorbed at the end of the bar.
		content, saved := runKeys(t, "", "ab"+keyEsc+keyLeft+keyLeft+keyLeft+"\r")
		if !saved || content != "ab" {
			t.Errorf("Run = (%q, saved=%v), want (%q, true)", content, saved, "ab")
		}
	})
	t.Run("abort asks for confirmation", func(t *testing.T) {
		content, saved := runKeys(t, "", "ab"+keyEsc+keyLeft+" "+"y")
		if saved || content != "ab" {
			t.Errorf("Run = (%q, saved=%v), want (%q, false)", content, saved, "ab")
		}
	})
	t.Run("help returns to the message", func(t *testing.T) {
		// The built-in help is two pages on 24 rows: 'k' turns the page, the
		// second 'k' dismisses the help screen.
		tt, ed := newRunHarness(t, "ab"+keyEsc+keyRight+"\r"+"k"+"k"+"c"+keySave)
		content, saved, err := ed.Run()
		if err != nil || !saved || content != "abc" {
			t.Errorf("Run = (%q, saved=%v, %v), want (%q, true, nil)", content, saved, err, "abc")
		}
		// The help screen took over the whole terminal; the message is back.
		if got := tt.Row(ed.screen.GetEditingStartY()); got != "abc" {
			t.Errorf("editing row after help = %q, want %q", got, "abc")
		}
	})
	t.Run("quote with nothing to quote shows a notice", func(t *testing.T) {
		// Edit -> Help -> Quote; a third Right is absorbed at the end of the
		// bar. 'k' dismisses the notice.
		tt, ed := newRunHarness(t, "ab"+keyEsc+keyRight+keyRight+keyRight+"\r"+"k"+"c"+keySave)
		content, saved, err := ed.Run()
		if err != nil || !saved || content != "abc" {
			t.Errorf("Run = (%q, saved=%v, %v), want (%q, true, nil)", content, saved, err, "abc")
		}
		if got := tt.Row(ed.screen.PromptRow()); got != "Saving..." {
			t.Errorf("prompt row = %q, want the notice replaced by %q", got, "Saving...")
		}
	})
}

// The view command is not on any key or menu yet, but it is wired into the
// dispatcher: it shows the message full-screen and returns to the editor.
func TestViewCommandShowsMessageThenRestoresEditor(t *testing.T) {
	tt, ed := newRunHarness(t, "k")
	defer ed.input.CloseAndWait()
	ed.LoadContent("first\nsecond")
	ed.redrawScreen()

	ed.handleCommand(CommandView)

	row := ed.screen.GetEditingStartY()
	if got := tt.Row(row) + "|" + tt.Row(row+1); got != "first|second" {
		t.Errorf("editing rows after view = %q, want %q", got, "first|second")
	}
	if got := tt.Row(24); got != "" {
		t.Errorf("row 24 = %q, want the \"press any key\" prompt cleared", got)
	}
}

// Run reports a dropped connection instead of returning a half-typed message
// as if it had been finished.
func TestRunReturnsErrorWhenInputEnds(t *testing.T) {
	tt := testterm.New(80, 24)
	ih := NewInputHandler(strings.NewReader("abc"))
	ed := NewFSEditor(nil, tt, ansi.OutputModeUTF8, 80, 24, "", "", "", "", "", "", ih)

	content, saved, err := ed.Run()
	if err == nil || saved || content != "" {
		t.Errorf("Run = (%q, saved=%v, %v), want (\"\", false, an error)", content, saved, err)
	}
}
