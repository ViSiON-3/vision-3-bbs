package menu

import (
	"bytes"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/editor/testterm"
)

// recordingWriter keeps what toast delivery writes to the caller, so a test can
// tell a row redraw from a replay of the whole screen.
type recordingWriter struct {
	w  io.Writer
	mu sync.Mutex
	b  bytes.Buffer
}

func (r *recordingWriter) Write(p []byte) (int, error) {
	r.mu.Lock()
	r.b.Write(p)
	r.mu.Unlock()
	return r.w.Write(p)
}

func (r *recordingWriter) String() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.b.String()
}

// overlayThenKey shows one live toast during a key read, records what the
// restore wrote, and returns it with the screen as the caller sees it after.
func overlayThenKey(t *testing.T, env *toastEnv, text string) (restore string) {
	t.Helper()
	toastDwell = 10 * time.Second
	rec := &recordingWriter{w: env.s}
	SetSessionOutput(env.s, rec)
	keys := env.readKey()
	env.sess.AddPage(text)
	toastWaitFor(t, "the overlay", func() bool { return strings.Contains(env.term.Snapshot(), ">> "+text) })
	drawn := len(rec.String())
	env.s.Send("x")
	select {
	case <-keys:
	case <-time.After(3 * time.Second):
		t.Fatal("key never reached the read")
	}
	toastWaitFor(t, "the restore", func() bool { return len(rec.String()) > drawn })
	time.Sleep(20 * time.Millisecond) // let the restore finish writing
	return rec.String()[drawn:]
}

// Clearing a toast redraws the row it covered, colours included, and leaves the
// pen as the screen had it, instead of clearing and replaying the whole screen.
func TestToastRestoreRedrawsOnlyItsRow(t *testing.T) {
	env := newToastEnv(t)
	// A coloured bottom row, a pen left green for whatever is typed next, and
	// the cursor back where the screen's owner waits.
	_, _ = env.s.Write([]byte("\x1b[24;1H\x1b[1;33;44mBOTTOM TEXT\x1b[0m\x1b[5;7H\x1b[32m"))

	restore := overlayThenKey(t, env, "Hello")
	if strings.Contains(restore, "\x1b[2J") {
		t.Errorf("restore replayed the whole screen: %q", restore)
	}
	if !strings.Contains(env.term.Row(24), "BOTTOM TEXT") || !strings.Contains(env.term.Row(1), "FULL SCREEN") {
		t.Errorf("screen not restored:\n%s", env.term.Snapshot())
	}
	if c := env.term.Cell(24, 1); c.Rune != 'B' || c.Fg != 33 || c.Bg != 44 || !c.Bold {
		t.Errorf("row 24 lost its colours: %+v", c)
	}
	if c := env.term.Cell(24, 12); c.Rune != ' ' || c.Fg != -1 || c.Bg != -1 {
		t.Errorf("rest of row 24 not blank: %+v", c)
	}
	if row, col := env.term.Cursor(); row != 5 || col != 7 {
		t.Errorf("cursor at %d,%d after restore, want 5,7", row, col)
	}
	if pen := env.term.Pen(); pen.Fg != 32 {
		t.Errorf("pen after restore = %+v, want green", pen)
	}
}

// A screen the model does not understand is replayed whole, as before.
func TestToastRestoreReplaysAScreenItCannotModel(t *testing.T) {
	cases := map[string]struct {
		mode  ansi.OutputMode
		paint string
	}{
		"scroll region":  {ansi.OutputModeAuto, "\x1b[1;23r\x1b[5;7H"},
		"wide character": {ansi.OutputModeUTF8, "\x1b[2;1H漢字\x1b[5;7H"},
		"last cell used": {ansi.OutputModeAuto, "\x1b[24;80HZ\x1b[5;7H"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			env := newToastEnvWith(t, tc.mode)
			_, _ = env.s.Write([]byte(tc.paint))
			restore := overlayThenKey(t, env, "Fallback")
			if !strings.Contains(restore, "\x1b[2J") {
				t.Errorf("restore did not replay the screen: %q", restore)
			}
			if !strings.Contains(env.term.Row(1), "FULL SCREEN") || !strings.Contains(env.term.Row(24), "BOTTOM TEXT") {
				t.Errorf("screen not restored:\n%s", env.term.Snapshot())
			}
		})
	}
}

// On a CP437 session the row's high bytes go back as the bytes they were.
func TestToastRestoreKeepsCP437Art(t *testing.T) {
	env := newToastEnvWith(t, ansi.OutputModeCP437)
	_, _ = env.s.Write([]byte("\x1b[24;1H\x1b[36m\xcd\xcd\xcd\xb9 BOTTOM TEXT \xcc\xcd\xcd\x1b[0m\x1b[5;7H"))
	before := env.term.RowCells(24)

	restore := overlayThenKey(t, env, "Boxed")
	if strings.Contains(restore, "\x1b[2J") {
		t.Errorf("restore replayed the whole screen: %q", restore)
	}
	after := env.term.RowCells(24)
	for i := range before {
		if before[i] != after[i] {
			t.Fatalf("row 24 col %d = %+v after restore, want %+v", i+1, after[i], before[i])
		}
	}
	if env.term.Cell(24, 1).Rune != '═' {
		t.Errorf("row 24 = %q", env.term.Row(24))
	}
}

// Blink is how BBS art gets bright backgrounds; the row keeps it.
func TestToastRestoreKeepsBlink(t *testing.T) {
	env := newToastEnv(t)
	_, _ = env.s.Write([]byte("\x1b[24;1H\x1b[5;37;41mBOTTOM TEXT\x1b[0m\x1b[5;7H"))
	restore := overlayThenKey(t, env, "Bright")
	if strings.Contains(restore, "\x1b[2J") {
		t.Errorf("restore replayed the whole screen: %q", restore)
	}
	if c := env.term.Cell(24, 3); !c.Blink || c.Bg != 41 {
		t.Errorf("row 24 lost blink: %+v", c)
	}
}

// At a menu's anchor the sysop laid the art out around the toast, so only the
// toast's own cells are written; at a configured position the rest of the row
// is cleared so the toast reads over whatever was there.
func TestAnchoredToastKeepsArtToItsRight(t *testing.T) {
	env := newToastEnv(t)
	toastDwell = 10 * time.Second
	_, _ = env.s.Write([]byte("\x1b[3;1HLEFT\x1b[3;40HRIGHT EDGE\x1b[5;7H"))
	res, err := ansi.ProcessAnsiAndExtractCoords([]byte("\x1b[3;9H~TO"), ansi.OutputModeCP437)
	if err != nil {
		t.Fatal(err)
	}
	env.e.setToastAnchor(1, res, true, false)
	keys := env.readKey()
	env.sess.AddPage("Framed")
	toastWaitFor(t, "the overlay", func() bool { return strings.Contains(env.term.Row(3), ">> Framed") })
	if row := env.term.Row(3); !strings.HasPrefix(row, "LEFT    >> Framed") || !strings.Contains(row, "RIGHT EDGE") {
		t.Errorf("row 3 = %q, want the art either side of the toast", row)
	}
	env.s.Send("x")
	<-keys
	env.e.clearToastAnchor(1)

	// The configured position clears the row from the toast on.
	keys = env.readKey()
	env.sess.AddPage("Loose")
	toastWaitFor(t, "the overlay", func() bool { return strings.Contains(env.term.Row(24), ">> Loose") })
	if row := env.term.Row(24); strings.Contains(row, "BOTTOM") {
		t.Errorf("row 24 = %q, want the old text cleared", row)
	}
	env.s.Send("x")
	<-keys
}

// Art that scrolls the screen, or an anchor on a row the terminal does not
// have, leaves the configured position in force.
func TestToastAnchorIgnoredWhenArtScrollsOrIsOffScreen(t *testing.T) {
	env := newToastEnv(t)
	res, err := ansi.ProcessAnsiAndExtractCoords([]byte("\x1b[3;9H~TO"), ansi.OutputModeCP437)
	if err != nil {
		t.Fatal(err)
	}
	env.e.setToastAnchor(1, res, true, true)
	if _, _, ok := env.sess.ToastAnchor(); ok {
		t.Error("anchor set although the art scrolled the screen")
	}

	far, err := ansi.ProcessAnsiAndExtractCoords([]byte("\x1b[30;9H~TO"), ansi.OutputModeCP437)
	if err != nil {
		t.Fatal(err)
	}
	env.e.setToastAnchor(1, far, true, false)
	if _, _, ok := env.sess.ToastAnchor(); ok {
		t.Error("anchor set on row 30 of a 24-row terminal")
	}

	env.e.setToastAnchor(1, res, true, false)
	if r, c, ok := env.sess.ToastAnchor(); !ok || r != 3 || c != 9 {
		t.Errorf("anchor = %d,%d,%v, want 3,9 set", r, c, ok)
	}
}

func TestMenuArtScrolls(t *testing.T) {
	line := strings.Repeat("x", 79)
	full := strings.Repeat("x", 80)
	cases := map[string]struct {
		art  string
		want bool
	}{
		"fits":                       {strings.Repeat(line+"\r\n", 23) + line, false},
		"cut to the height first":    {strings.Repeat(line+"\r\n", 40), false},
		"autowrap off the last row":  {strings.Repeat(full, 24), true},
		"full rows with line breaks": {strings.Repeat(full+"\r\n", 23) + full, true},
		"unknown height":             {strings.Repeat(full, 40), false},
	}
	for name, tc := range cases {
		height := 24
		if name == "unknown height" {
			height = 0
		}
		if got := menuArtScrolls([]byte(tc.art), 80, height); got != tc.want {
			t.Errorf("%s: menuArtScrolls = %v, want %v", name, got, tc.want)
		}
	}
}

// |TO places toasts like ~TO does.
func TestPipeToastAnchorCode(t *testing.T) {
	env := newToastEnv(t)
	toastDwell = 10 * time.Second
	res, err := ansi.ProcessAnsiAndExtractCoords([]byte("\x1b[7;5H|TO"), ansi.OutputModeCP437)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(res.DisplayBytes), "TO") {
		t.Errorf("code left in the art: %q", res.DisplayBytes)
	}
	env.e.setToastAnchor(1, res, true, false)
	keys := env.readKey()
	env.sess.AddPage("Piped")
	toastWaitFor(t, "the overlay", func() bool { return strings.Contains(env.term.Row(7), ">> Piped") })
	if got := env.term.Cell(7, 5).Rune; got != '>' {
		t.Errorf("column 5 of row 7 = %q, want the toast's start", got)
	}
	env.s.Send("x")
	<-keys
}

// The second toast's snapshot holds the first one's overlay and restore, which
// the model must read back to the same screen.
func TestToastRestoreSurvivesASecondToast(t *testing.T) {
	env := newToastEnv(t)
	overlayThenKey(t, env, "First")
	restore := overlayThenKey(t, env, "Second")
	if strings.Contains(restore, "\x1b[2J") {
		t.Errorf("second restore replayed the whole screen: %q", restore)
	}
	if !strings.Contains(env.term.Row(24), "BOTTOM TEXT") || !strings.Contains(env.term.Row(1), "FULL SCREEN") {
		t.Errorf("screen not restored:\n%s", env.term.Snapshot())
	}
	if _, ok := any(env.term).(*testterm.Term); !ok {
		t.Fatal("test terminal type changed")
	}
}
