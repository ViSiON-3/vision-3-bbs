package menu

import (
	"errors"
	"io"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/editor"
)

func TestParseChallengeKey(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"ESC", editor.KeyEsc},
		{"esc", editor.KeyEsc},
		{"", editor.KeyEsc},
		{"*", int('*')},
		{"A", int('A')},
		{"12", int('1')}, // first rune wins
	}
	for _, c := range cases {
		if got := parseChallengeKey(c.in); got != c.want {
			t.Errorf("parseChallengeKey(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestFindCountdownField(t *testing.T) {
	// Matches the real BOTCHECK.ASC layout: ESC[0m, then two text lines.
	prompt := []byte("\x1b[0m\r\n Press ESC twice if you're not a bot.\r\n You have ## seconds.\r\n")
	row, col, width, found := findCountdownField(prompt)
	if !found {
		t.Fatal("found = false, want true")
	}
	if row != 3 {
		t.Errorf("row = %d, want 3", row)
	}
	if col != 11 { // " You have " = 10 chars, '#' starts at column 11
		t.Errorf("col = %d, want 11", col)
	}
	if width != 2 {
		t.Errorf("width = %d, want 2", width)
	}
}

func TestFindCountdownFieldSkipsColorCodes(t *testing.T) {
	// A color code before the field must not shift the computed column.
	prompt := []byte(" You have \x1b[1;32m### seconds.")
	row, col, width, found := findCountdownField(prompt)
	if !found || row != 1 || col != 11 || width != 3 {
		t.Errorf("got row=%d col=%d width=%d found=%v; want 1/11/3/true", row, col, width, found)
	}
}

func TestFindCountdownFieldNone(t *testing.T) {
	if _, _, _, found := findCountdownField([]byte("no placeholder here")); found {
		t.Error("found = true, want false")
	}
}

func TestFormatCountdownValue(t *testing.T) {
	cases := []struct {
		secs, width int
		want        string
	}{
		{20, 2, "20"},
		{5, 2, " 5"}, // right-aligned within width
		{5, 3, "  5"},
		{100, 2, "100"}, // exceeds width -> printed as-is
	}
	for _, c := range cases {
		if got := formatCountdownValue(c.secs, c.width); got != c.want {
			t.Errorf("formatCountdownValue(%d,%d) = %q, want %q", c.secs, c.width, got, c.want)
		}
	}
}

func TestSubstituteCountdown(t *testing.T) {
	prompt := []byte(" You have ## seconds.")
	got := string(substituteCountdown(prompt, 9, 2))
	if got != " You have  9 seconds." {
		t.Errorf("substituteCountdown = %q", got)
	}
}

func TestSubstituteGateTokens(t *testing.T) {
	prompt := []byte(" Press {KEY} {PRESSES} times.")
	got := string(substituteGateTokens(prompt, "*", 3))
	if got != " Press * 3 times." {
		t.Errorf("substituteGateTokens = %q", got)
	}

	noTokens := []byte(" nothing to replace here.")
	if got := string(substituteGateTokens(noTokens, "*", 3)); got != string(noTokens) {
		t.Errorf("substituteGateTokens with no tokens = %q, want unchanged %q", got, noTokens)
	}

	withCountdown := []byte(" Press {KEY} {PRESSES} times. You have ## seconds.")
	got = string(substituteGateTokens(withCountdown, "ESC", 2))
	if got != " Press ESC 2 times. You have ## seconds." {
		t.Errorf("substituteGateTokens should leave ## intact, got %q", got)
	}
}

// scriptedInput returns queued (key, err) pairs; when drained it returns
// ErrIdleTimeout so the loop relies on the injected clock for the deadline.
type scriptedInput struct {
	events []struct {
		key int
		err error
	}
	i int
}

func (s *scriptedInput) ReadKeyWithTimeout(time.Duration) (int, error) {
	if s.i >= len(s.events) {
		return 0, editor.ErrIdleTimeout
	}
	e := s.events[s.i]
	s.i++
	return e.key, e.err
}

func key(k int) struct {
	key int
	err error
} {
	return struct {
		key int
		err error
	}{k, nil}
}

func TestRunChallengeLoopPass(t *testing.T) {
	in := &scriptedInput{events: []struct {
		key int
		err error
	}{key('a'), key(editor.KeyEsc), key(editor.KeyEsc)}} // one stray, then two ESC
	now := func() time.Time { return time.Unix(0, 0) }
	passed, err := runChallengeLoop(in, now, time.Unix(100, 0), editor.KeyEsc, 2, 8, time.Second, func() {})
	if err != nil || !passed {
		t.Fatalf("passed=%v err=%v; want true/nil", passed, err)
	}
}

func TestRunChallengeLoopStrayBetweenMatches(t *testing.T) {
	in := &scriptedInput{events: []struct {
		key int
		err error
	}{key(editor.KeyEsc), key('x'), key(editor.KeyEsc)}} // match, stray, match
	now := func() time.Time { return time.Unix(0, 0) }
	passed, err := runChallengeLoop(in, now, time.Unix(100, 0), editor.KeyEsc, 2, 8, time.Second, func() {})
	if err != nil || !passed {
		t.Fatalf("passed=%v err=%v; want true/nil", passed, err)
	}
}

func TestRunChallengeLoopFloodFails(t *testing.T) {
	evts := make([]struct {
		key int
		err error
	}, 8)
	for i := range evts {
		evts[i] = key('x')
	}
	in := &scriptedInput{events: evts}
	now := func() time.Time { return time.Unix(0, 0) }
	passed, err := runChallengeLoop(in, now, time.Unix(100, 0), editor.KeyEsc, 2, 8, time.Second, func() {})
	if err != nil || passed {
		t.Fatalf("passed=%v err=%v; want false/nil (flood)", passed, err)
	}
}

func TestRunChallengeLoopTimeout(t *testing.T) {
	in := &scriptedInput{} // always idle-timeout
	ticks := 0
	// clock advances one second per call so the deadline is eventually hit.
	base := time.Unix(0, 0)
	calls := 0
	now := func() time.Time { calls++; return base.Add(time.Duration(calls) * time.Second) }
	passed, err := runChallengeLoop(in, now, base.Add(3*time.Second), editor.KeyEsc, 2, 8, time.Second, func() { ticks++ })
	if err != nil || passed {
		t.Fatalf("passed=%v err=%v; want false/nil (timeout)", passed, err)
	}
	if ticks == 0 {
		t.Error("onTick never called; countdown would not update")
	}
}

func TestRunChallengeLoopEOF(t *testing.T) {
	in := &scriptedInput{events: []struct {
		key int
		err error
	}{{0, io.EOF}}}
	now := func() time.Time { return time.Unix(0, 0) }
	passed, err := runChallengeLoop(in, now, time.Unix(100, 0), editor.KeyEsc, 2, 8, time.Second, func() {})
	if passed || !errors.Is(err, io.EOF) {
		t.Fatalf("passed=%v err=%v; want false + io.EOF", passed, err)
	}
}
