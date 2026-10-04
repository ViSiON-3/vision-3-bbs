// Forked with the v3agents/internal/vt parser for the BBS regression MCP.

package vt

import (
	"os"
	"strings"
	"testing"
)

func lines(s Snapshot) []string { return strings.Split(s.Text, "\n") }

func TestPlainText(t *testing.T) {
	s := New(80, 25, CP437, nil)
	s.Write([]byte("hello\r\nworld"))
	got := s.Snapshot()
	if l := lines(got); l[0] != "hello" || l[1] != "world" || got.Row != 1 || got.Col != 5 {
		t.Fatalf("%q at %d,%d", l[:2], got.Row, got.Col)
	}
	if len(lines(got)) != 25 {
		t.Fatalf("want 25 lines, got %d", len(lines(got)))
	}
}

func TestCursorAndErase(t *testing.T) {
	s := New(80, 25, CP437, nil)
	s.Write([]byte("XXXXXXXX\x1b[1;3HAB\x1b[K\x1b[3;5Hz\x1b[2J\x1b[2;2Hq"))
	l := lines(s.Snapshot())
	if l[0] != "" || l[1] != " q" {
		t.Fatalf("%q", l[:3])
	}
	s.Write([]byte("\x1b[1;1Habcdef\x1b[1;3H\x1b[1K"))
	if l := lines(s.Snapshot()); l[0] != "   def" {
		t.Fatalf("%q", l[0])
	}
}

func TestCP437(t *testing.T) {
	s := New(80, 25, CP437, nil)
	s.Write([]byte{0xC9, 0xCD, 0xBB, ' ', 0xDB, 0xB0})
	if l := lines(s.Snapshot()); l[0] != "╔═╗ █░" {
		t.Fatalf("%q", l[0])
	}
}

func TestUTF8(t *testing.T) {
	s := New(80, 25, UTF8, nil)
	s.Write([]byte("╔═╗ ok"))
	if l := lines(s.Snapshot()); l[0] != "╔═╗ ok" {
		t.Fatalf("%q", l[0])
	}
}

func TestSplitWrites(t *testing.T) {
	whole := []byte("\x1b[2;3H╔═\x1b[7mhi\x1b[0m")
	ref := New(80, 25, UTF8, nil)
	ref.Write(whole)
	for i := 1; i < len(whole); i++ {
		s := New(80, 25, UTF8, nil)
		s.Write(whole[:i])
		s.Write(whole[i:])
		if s.Snapshot() != ref.Snapshot() {
			t.Fatalf("split at %d: %q vs %q", i, s.Snapshot().Text[:120], ref.Snapshot().Text[:120])
		}
	}
}

func TestCPRRepliesEveryTime(t *testing.T) {
	var replies []string
	s := New(80, 25, CP437, func(b []byte) { replies = append(replies, string(b)) })
	s.Write([]byte("\x1b[6n"))
	s.Write([]byte("\x1b[5;10Hx\x1b[6n"))
	if len(replies) != 2 || replies[0] != "\x1b[1;1R" || replies[1] != "\x1b[5;11R" {
		t.Fatalf("%q", replies)
	}
}

func TestHighlightRuns(t *testing.T) {
	s := New(80, 25, CP437, nil)
	s.Write([]byte("  \x1b[45mMAINLINE      \x1b[0m  Local\r\n  WORLD WIRE\r\n\x1b[7mQuit\x1b[27m now"))
	l := lines(s.Snapshot())
	if l[0] != "  «MAINLINE»        Local" || l[1] != "  WORLD WIRE" || l[2] != "«Quit» now" {
		t.Fatalf("%q", l[:3])
	}
	s.Write([]byte("\x1b[4;1H\x1b[40mblack bg is not a highlight"))
	if l := lines(s.Snapshot()); l[3] != "black bg is not a highlight" {
		t.Fatalf("%q", l[3])
	}
}

func TestFullScreenBackgroundIsNotHighlight(t *testing.T) {
	s := New(80, 25, CP437, nil)
	s.Write([]byte("\x1b[44m"))
	for i := 0; i < 20; i++ {
		s.Write([]byte("blue line of text\r\n"))
	}
	if strings.Contains(s.Snapshot().Text, "«") {
		t.Fatal("a fully painted screen should carry no markers")
	}
}

func TestLastColumnDoesNotScroll(t *testing.T) {
	s := New(80, 25, CP437, nil)
	s.Write([]byte("top\x1b[25;1H" + strings.Repeat("x", 80)))
	got := s.Snapshot()
	if l := lines(got); l[0] != "top" || l[24] != strings.Repeat("x", 80) || got.Row != 24 || got.Col != 79 {
		t.Fatalf("%q / %q at %d,%d", l[0], l[24], got.Row, got.Col)
	}
	s.Write([]byte("y"))
	if l := lines(s.Snapshot()); l[0] != "" || l[23] != strings.Repeat("x", 80) || l[24] != "y" {
		t.Fatalf("after wrap: %q %q %q", l[0], l[23], l[24])
	}
}

func TestScrollRegion(t *testing.T) {
	s := New(80, 25, CP437, nil)
	s.Write([]byte("head\x1b[2;4r\x1b[2;1Ha\r\nb\r\nc\r\nd\x1b[r"))
	l := lines(s.Snapshot())
	if l[0] != "head" || l[1] != "b" || l[2] != "c" || l[3] != "d" {
		t.Fatalf("%q", l[:5])
	}
}

func TestSaveRestoreAndBackspace(t *testing.T) {
	s := New(80, 25, CP437, nil)
	s.Write([]byte("ab\x1b[s\x1b[10;10Hz\x1b[uc\x08d\tX"))
	if l := lines(s.Snapshot()); l[0] != "abd     X" {
		t.Fatalf("%q", l[0])
	}
}

func TestFormFeedClears(t *testing.T) {
	s := New(80, 25, CP437, nil)
	s.Write([]byte("junk\x0cclean"))
	if l := lines(s.Snapshot()); l[0] != "clean" {
		t.Fatalf("%q", l[0])
	}
	s.Write([]byte("\x1b[5;5H\x1b[7m\x1bcafter reset"))
	if l := lines(s.Snapshot()); l[0] != "after reset" || strings.Contains(l[0], "«") {
		t.Fatalf("%q", l[0])
	}
}

func TestCapturedAreaList(t *testing.T) {
	raw, err := os.ReadFile("testdata/areas.ans")
	if err != nil {
		t.Skip("no capture yet")
	}
	want, _ := os.ReadFile("testdata/areas.txt")
	s := New(80, 25, CP437, nil)
	s.Write(raw)
	if got := s.Snapshot().Text; got != strings.TrimRight(string(want), "\n") {
		t.Fatalf("got\n%s", got)
	}
}

func fullRow(s *Screen, mid string) {
	s.Write([]byte(strings.Repeat("x", 80) + mid + "next"))
}

func TestWrapPendingSurvivesSGR(t *testing.T) {
	for name, mid := range map[string]string{"sgr": "\x1b[0;33m", "cpr": "\x1b[6n"} {
		s := New(80, 25, CP437, func([]byte) {})
		fullRow(s, mid)
		l := lines(s.Snapshot())
		if l[0] != strings.Repeat("x", 80) || l[1] != "next" {
			t.Fatalf("%s: %q / %q", name, l[0], l[1])
		}
	}
}

func TestExtendedSGR(t *testing.T) {
	cases := []struct {
		seq  string
		want string
	}{
		{"\x1b[38;5;7mab", "ab"},
		{"\x1b[38;5;45mab", "ab"},
		{"\x1b[38;2;0;0;0mab", "ab"},
		{"\x1b[48;5;4mab", "«ab»"},
		{"\x1b[48;5;0mab", "ab"},
		{"\x1b[48;2;0;0;0mab", "ab"},
		{"\x1b[48;2;10;0;0mab", "«ab»"},
		{"\x1b[38:5:45mab", "ab"},
		{"\x1b[48:5:4mab", "«ab»"},
		{"\x1b[48:2::0:0:0mab", "ab"},
		{"\x1b[48:2::1:2:3mab", "«ab»"},
	}
	for _, c := range cases {
		s := New(80, 25, CP437, nil)
		s.Write([]byte("zz\r\n" + c.seq))
		// a second, plain line keeps the highlight under the half-screen threshold
		s.Write([]byte("\x1b[0m\r\nplain text here"))
		if got := lines(s.Snapshot())[1]; got != c.want {
			t.Errorf("%q: got %q want %q", c.seq, got, c.want)
		}
	}
	s := New(80, 25, CP437, nil)
	s.Write([]byte("\x1b[7m\x1b[38:5:45mab\x1b[0m long plain text here"))
	if got := lines(s.Snapshot())[0]; got != "«ab» long plain text here" {
		t.Errorf("colon form reset reverse: %q", got)
	}
}

func TestSwallowedEscapes(t *testing.T) {
	for _, in := range []string{"\x1b(Bhi", "\x1b]0;title\x07hi", "\x1b]0;title\x1b\\hi", "\x1bPq;data\x1b\\hi", "\x1b_app\x07hi"} {
		s := New(80, 25, CP437, nil)
		s.Write([]byte(in))
		if got := lines(s.Snapshot())[0]; got != "hi" {
			t.Errorf("%q: got %q", in, got)
		}
		for i := 1; i < len(in); i++ {
			s := New(80, 25, CP437, nil)
			s.Write([]byte(in[:i]))
			s.Write([]byte(in[i:]))
			if got := lines(s.Snapshot())[0]; got != "hi" {
				t.Errorf("%q split at %d: got %q", in, i, got)
			}
		}
	}
	s := New(80, 25, CP437, nil)
	s.Write([]byte("\x1b]0;" + strings.Repeat("a", 10000) + "tail"))
	if got := lines(s.Snapshot())[0]; got == "" {
		t.Error("unterminated string was never capped")
	}
}

func TestUTF8InvalidKeepsNextByte(t *testing.T) {
	s := New(80, 25, UTF8, nil)
	s.Write([]byte("a\xe9bc\xe2ok"))
	if got := lines(s.Snapshot())[0]; got != "a�bc�ok" {
		t.Fatalf("%q", got)
	}
	s = New(80, 25, UTF8, nil)
	s.Write([]byte("\xe2\x1b[2;1Hq"))
	if got := lines(s.Snapshot())[1]; got != "q" {
		t.Fatalf("escape after invalid lead lost: %q", got)
	}
}
