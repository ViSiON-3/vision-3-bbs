package editor

import (
	"strings"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/editor/testterm"
)

// previewRow finds the row the preview divider is drawn on, 0 for none.
func previewRow(tt *testterm.Term, height int) int {
	for row := 1; row <= height; row++ {
		if strings.Contains(tt.Row(row), "Preview") {
			return row
		}
	}
	return 0
}

// waitFor waits until the screen shows text, or fails the test.
func waitFor(t *testing.T, tt *testterm.Term, text string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(tt.Snapshot(), text) {
		if time.Now().After(deadline) {
			t.Fatalf("screen never showed %q:\n%s", text, tt.Snapshot())
		}
		time.Sleep(time.Millisecond)
	}
}

// With PreviewLines set, the text's first lines are shown under it with
// their colour codes in colour, and redrawn as the caller types (#352).
func TestPreviewShowsColours(t *testing.T) {
	tt := testterm.New(80, 24)
	sess := testterm.NewSession(tt, "")
	ed := NewFSEditor(sess, tt, ansi.OutputModeUTF8, 80, 24, "", "", "", "", "", "", nil)
	ed.SetEditorContext(EditorContext{PreviewLines: 5})
	ed.LoadContent("|12Red line\n|09Blue")
	go func() {
		waitFor(t, tt, "Blue")
		sess.Send("!")
		waitFor(t, tt, "Blue!")
		sess.Send("\x1a")
	}()
	content, saved, err := ed.Run()
	if err != nil || !saved || content != "|12Red line\n|09Blue!" {
		t.Fatalf("Run = (%q,%v,%v)", content, saved, err)
	}

	div := previewRow(tt, 24)
	if div == 0 {
		t.Fatalf("no preview divider:\n%s", tt.Snapshot())
	}
	if got := ed.screen.GetScreenLines(); div != ed.screen.GetEditingStartY()+got {
		t.Errorf("divider on row %d, text area ends on row %d", div, ed.screen.GetEditingStartY()+got-1)
	}
	if got := strings.TrimRight(tt.Row(div+1), " "); got != "Red line" {
		t.Errorf("preview row 1 = %q, want the line without its code", got)
	}
	if got := strings.TrimRight(tt.Row(div+2), " "); got != "Blue!" {
		t.Errorf("preview row 2 = %q, want the edited line", got)
	}
	red, blue := tt.Cell(div+1, 1), tt.Cell(div+2, 1)
	if red.Fg != 31 || !red.Bold || blue.Fg != 34 || !blue.Bold {
		t.Errorf("preview colours: red %+v, blue %+v", red, blue)
	}
	// The text itself still shows the codes as typed.
	if !strings.Contains(tt.Snapshot(), "|12Red line") {
		t.Errorf("the text lost its codes:\n%s", tt.Snapshot())
	}
}

// Screen codes in the text don't reach the preview: only colours do, so a
// |CL can't clear the editor's screen.
func TestPreviewOnlyColours(t *testing.T) {
	s := NewScreen(nil, ansi.OutputModeUTF8, 80, 24)
	s.SetPreview(2)
	s.reservePreview()
	buf := NewMessageBuffer()
	buf.LoadContent("|CL|15Hi|XY10,10\n" + strings.Repeat("x", 100))
	rows := s.previewRows(buf)
	if strings.Contains(rows[0], "\x1b[2J") || !strings.Contains(rows[0], "Hi") {
		t.Errorf("row 1 = %q", rows[0])
	}
	if got := ansi.VisibleLength(rows[1]); got != 79 {
		t.Errorf("row 2 is %d wide, want it cut to 79", got)
	}
}

// Without PreviewLines there is no preview, and the text area is as before;
// a screen too short to keep 5 text rows under a preview shows none.
func TestPreviewOffAndTooShort(t *testing.T) {
	plain := NewScreen(nil, ansi.OutputModeUTF8, 80, 24)
	plain.reservePreview()
	with := NewScreen(nil, ansi.OutputModeUTF8, 80, 24)
	with.SetPreview(5)
	with.reservePreview()
	if plain.previewY != 0 || with.GetScreenLines() != plain.GetScreenLines()-6 {
		t.Errorf("text rows: plain %d, with preview %d", plain.GetScreenLines(), with.GetScreenLines())
	}
	short := NewScreen(nil, ansi.OutputModeUTF8, 80, 24)
	short.SetPreview(15)
	short.reservePreview()
	if short.previewY != 0 || short.GetScreenLines() != plain.GetScreenLines() {
		t.Errorf("a preview that leaves under 5 rows: previewY %d, rows %d", short.previewY, short.GetScreenLines())
	}
	// A resize lays the screen out again and takes the rows once.
	with.Resize(100, 30)
	big := NewScreen(nil, ansi.OutputModeUTF8, 100, 30)
	if with.GetScreenLines() != big.GetScreenLines()-6 {
		t.Errorf("after a resize: %d rows, want %d", with.GetScreenLines(), big.GetScreenLines()-6)
	}
}
