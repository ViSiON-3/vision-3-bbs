package wfcui

import (
	"strings"
	"testing"
)

type pos struct {
	row, col int
	known    bool
}

func trackOf(w, h int, chunks ...string) *cursor {
	var tr seqTracker
	tr.reset(w, h)
	for _, c := range chunks {
		tr.feed([]byte(c))
	}
	return &tr.cur
}

func TestCursorTracking(t *testing.T) {
	cases := []struct {
		name   string
		chunks []string
		want   pos
	}{
		{"printable", []string{"abc"}, pos{1, 4, true}},
		{"cr lf", []string{"ab\r\n"}, pos{2, 1, true}},
		{"lf keeps column", []string{"ab\n"}, pos{2, 3, true}},
		{"backspace", []string{"abc\b"}, pos{1, 3, true}},
		{"backspace at margin", []string{"\b"}, pos{1, 1, true}},
		{"tab", []string{"ab\t"}, pos{1, 9, true}},
		{"fills the row, wrap pending", []string{"0123456789"}, pos{1, 10, true}},
		{"wraps on the next glyph", []string{"0123456789a"}, pos{2, 2, true}},
		{"lf at the bottom keeps the row", []string{"\x1b[5;1H\n\n"}, pos{5, 1, true}},
		{"wrap at the bottom keeps the row", []string{"\x1b[5;10Hab"}, pos{5, 2, true}},
		{"cup", []string{"\x1b[3;4H"}, pos{3, 4, true}},
		{"hvp", []string{"\x1b[3;4f"}, pos{3, 4, true}},
		{"cup default", []string{"abc\x1b[H"}, pos{1, 1, true}},
		{"cup clamps", []string{"\x1b[99;99H"}, pos{5, 10, true}},
		{"cuu cud", []string{"\x1b[4;4H\x1b[2A\x1b[B"}, pos{3, 4, true}},
		{"cuf cub", []string{"\x1b[3;5H\x1b[3C\x1b[2D"}, pos{3, 6, true}},
		{"cuu clamps", []string{"\x1b[2;2H\x1b[9A"}, pos{1, 2, true}},
		{"csi split across chunks", []string{"\x1b[3", ";4", "H"}, pos{3, 4, true}},
		{"sco save and restore", []string{"\x1b[3;4H\x1b[s\x1b[5;5Hxx\x1b[u"}, pos{3, 4, true}},
		{"decsc and decrc", []string{"\x1b[3;4H\x1b7\x1b[5;5Hxx\x1b8"}, pos{3, 4, true}},
		{"restore with no save", []string{"ab\x1b[u"}, pos{1, 3, false}},
		{"ed does not move", []string{"\x1b[3;4H\x1b[2J"}, pos{3, 4, true}},
		{"ed then home", []string{"\x1b[3;4H\x1b[2J\x1b[H"}, pos{1, 1, true}},
		{"sgr and erase do not move", []string{"a\x1b[1;31m\x1b[Kb"}, pos{1, 3, true}},
		{"wide rune takes two cells", []string{"a日"}, pos{1, 4, true}},
		{"wide rune split across chunks", []string{"a\xe6\x97", "\xa5"}, pos{1, 4, true}},
		{"wide rune wraps whole", []string{"012345678日"}, pos{2, 3, true}},
		{"unknown mover", []string{"ab\x1b[5b"}, pos{1, 3, false}},
		{"unknown until cup", []string{"\x1b[5bxyz\x1b[2;2H"}, pos{2, 2, true}},
		{"clear alone leaves it unknown", []string{"\x1b[5b\x1b[2J"}, pos{1, 1, false}},
		{"origin mode is not modelled", []string{"\x1b[?6h\x1b[2;2H"}, pos{1, 1, false}},
		{"autowrap off", []string{"\x1b[?7l0123456789ab"}, pos{1, 10, true}},
		{"region keeps the row at its bottom", []string{"\x1b[2;3r\x1b[3;1H\n"}, pos{3, 1, true}},
		{"index", []string{"ab\x1bD"}, pos{2, 3, true}},
		{"next line", []string{"ab\x1bE"}, pos{2, 1, true}},
		{"reverse index at top", []string{"ab\x1bM"}, pos{1, 3, true}},
		{"osc text is not printed", []string{"\x1b]0;title\x07a"}, pos{1, 2, true}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := trackOf(10, 5, tc.chunks...)
			if got := (pos{c.row, c.col, c.known}); got != tc.want {
				t.Fatalf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestCursorPendingWrapBlocksRestore(t *testing.T) {
	c := trackOf(10, 5, "0123456789")
	if c.canRestore() {
		t.Fatal("an absolute move would drop the pending wrap")
	}
	c = trackOf(10, 5, "0123456789", "\r")
	if !c.canRestore() {
		t.Fatal("cr clears the pending wrap")
	}
}

func TestCursorSGRReplay(t *testing.T) {
	cases := []struct {
		in   []string
		want string
	}{
		{nil, "\x1b[0m"},
		{[]string{"\x1b[1;31m"}, "\x1b[0;1;31m"},
		{[]string{"\x1b[1;31m", "\x1b[44m", "\x1b[22m"}, "\x1b[0;31;44m"},
		{[]string{"\x1b[1;31m", "\x1b[0m"}, "\x1b[0m"},
		{[]string{"\x1b[31m", "\x1b[m", "\x1b[7m"}, "\x1b[0;7m"},
		{[]string{"\x1b[38;5;196;48;2;1;2;3m"}, "\x1b[0;38;5;196;48;2;1;2;3m"},
		{[]string{"\x1b[31;39;42;49m"}, "\x1b[0m"},
		{[]string{"\x1b[4;5m\x1b[24m"}, "\x1b[0;5m"},
		{[]string{"\x1b[7m\x1b[27m"}, "\x1b[0m"},
		{[]string{"\x1b[1m\x1b7\x1b[0m\x1b8"}, "\x1b[0;1m"},
	}
	for _, tc := range cases {
		c := trackOf(10, 5, tc.in...)
		if got := c.sgr.seq(); got != tc.want {
			t.Errorf("%q: got %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestSnoopBarRestoresTrackedCursorAndColours(t *testing.T) {
	r := newRig(t, utf8Hdr, 100, 40)
	r.out.waitFor(t, "NODE 3")
	before := len(r.out.String())
	_, _ = r.server.Write([]byte("\x1b[1;32m\x1b[5;7Hab"))
	r.out.waitFor(t, "\x1b[5;9H")
	r.send(t, "\x1bx")
	r.wait(t)
	out := r.out.String()[before:]
	i := strings.Index(out, "\x1b[40;1H")
	if i < 0 {
		t.Fatalf("no bar after the chunk: %q", out)
	}
	if !strings.Contains(out[i:], "\x1b[5;9H\x1b[0;1;32m") {
		t.Fatalf("bar did not restore the cursor and colours: %q", out[i:])
	}
	if strings.Contains(out, "\x1b7") || strings.Contains(out, "\x1b8") {
		t.Fatalf("bar used the shared save slot: %q", out)
	}
}

func TestSnoopBarKeepsCallersSavedCursor(t *testing.T) {
	r := newRig(t, utf8Hdr, 100, 40)
	r.out.waitFor(t, "NODE 3")
	_, _ = r.server.Write([]byte("\x1b[3;4H\x1b[s"))
	r.out.waitFor(t, "\x1b[s")
	// The bar redraws after that chunk and after this one.
	_, _ = r.server.Write([]byte("\x1b[10;10Hxx"))
	r.out.waitFor(t, "xx")
	_, _ = r.server.Write([]byte("\x1b[u"))
	r.out.waitFor(t, "\x1b[u")
	r.out.waitFor(t, "\x1b[3;4H\x1b[0m")
	r.send(t, "\x1bx")
	r.wait(t)
	out := r.out.String()
	if strings.Contains(out, "\x1b7") || strings.Contains(out, "\x1b8") {
		t.Fatalf("bar used the shared save slot: %q", out)
	}
	if strings.Count(out, "\x1b[s") != 1 || strings.Count(out, "\x1b[u") != 1 {
		t.Fatalf("bar wrote its own save or restore: %q", out)
	}
	if !strings.Contains(out[strings.Index(out, "\x1b[u"):], "\x1b[3;4H") {
		t.Fatalf("cursor not back at the caller's saved spot after the restore: %q", out)
	}
}

func TestSnoopBarSkippedWhileCursorUnknown(t *testing.T) {
	r := newRig(t, utf8Hdr, 100, 40)
	r.out.waitFor(t, "NODE 3")
	_, _ = r.server.Write([]byte("\x1b[5bxyz"))
	r.out.waitFor(t, "xyz")
	n := strings.Count(r.out.String(), "NODE 3")
	_, _ = r.server.Write([]byte("more"))
	r.out.waitFor(t, "more")
	if got := strings.Count(r.out.String(), "NODE 3"); got != n {
		t.Fatal("bar drawn while the cursor position was unknown")
	}
	_, _ = r.server.Write([]byte("\x1b[2;2Hz"))
	r.out.waitFor(t, "\x1b[2;3H")
	r.send(t, "\x1bx")
	r.wait(t)
	if got := strings.Count(r.out.String(), "NODE 3"); got <= n {
		t.Fatal("bar not drawn once a CUP made the position known")
	}
}
