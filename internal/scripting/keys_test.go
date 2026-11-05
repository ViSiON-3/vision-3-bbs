package scripting

import (
	"testing"
	"time"
)

// TestReadkeyNames checks the names readkey gives special keys, from the
// sequences VT100/xterm, VT220 and SyncTERM send, alone and among typed
// characters, and split across reads.
func TestReadkeyNames(t *testing.T) {
	two := `v3.console.readkey() + "|" + v3.console.readkey()`
	tests := []struct {
		name   string
		input  string
		chunks []string
		expr   string
		want   string
	}{
		{name: "up", input: "\x1b[A", want: "UP"},
		{name: "down", input: "\x1b[B", want: "DOWN"},
		{name: "right", input: "\x1b[C", want: "RIGHT"},
		{name: "left", input: "\x1b[D", want: "LEFT"},
		{name: "home xterm", input: "\x1b[H", want: "HOME"},
		{name: "end xterm", input: "\x1b[F", want: "END"},
		{name: "page up syncterm", input: "\x1b[V", want: "PGUP"},
		{name: "page down syncterm", input: "\x1b[U", want: "PGDN"},
		{name: "erase in line is not a key", input: "\x1b[Kx", want: "x"},
		{name: "home vt220", input: "\x1b[1~", want: "HOME"},
		{name: "insert", input: "\x1b[2~", want: "INS"},
		{name: "delete", input: "\x1b[3~", want: "DEL"},
		{name: "end vt220", input: "\x1b[4~", want: "END"},
		{name: "page up", input: "\x1b[5~", want: "PGUP"},
		{name: "page down", input: "\x1b[6~", want: "PGDN"},
		{name: "f5", input: "\x1b[15~", want: "F5"},
		{name: "f12", input: "\x1b[24~", want: "F12"},
		{name: "shift tab", input: "\x1b[Z", want: "BACKTAB"},
		{name: "ctrl up ignores the modifier", input: "\x1b[1;5A", want: "UP"},
		{name: "application mode up", input: "\x1bOA", want: "UP"},
		{name: "ss3 f1", input: "\x1bOP", want: "F1"},
		{name: "plain key", input: "q", want: "q"},
		{name: "enter", input: "\r", want: "\r"},
		{name: "keys around an arrow", input: "a\x1b[Bb", expr: `v3.console.readkey() + v3.console.readkey() + v3.console.readkey()`, want: "aDOWNb"},
		{name: "two arrows in one read", input: "\x1b[A\x1b[B", expr: two, want: "UP|DOWN"},
		{name: "unknown sequence skipped", input: "\x1b[99zx", want: "x"},
		{name: "lone escape", input: "\x1b", want: "ESC"},
		{name: "escape split from its sequence", input: "\x1b", chunks: []string{"[A"}, want: "UP"},
		{name: "sequence split after the bracket", input: "\x1b[", chunks: []string{"6~"}, want: "PGDN"},
		{name: "sequence split in its parameters", input: "\x1b[1", chunks: []string{";5B"}, want: "DOWN"},
		{name: "ss3 split", input: "\x1bO", chunks: []string{"B"}, want: "DOWN"},
		{name: "alt key", input: "\x1bx", expr: two, want: "ESC|"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t, harnessOpts{input: tt.input, inputChunks: tt.chunks})
			expr := tt.expr
			if expr == "" {
				expr = `v3.console.readkey()`
			}
			if tt.name == "alt key" {
				// the second read has nothing to read: give it a timeout
				expr = `v3.console.readkey() + "|" + v3.console.readkey(50)`
			}
			if got := h.eval(expr).String(); got != tt.want {
				t.Errorf("%s = %q, want %q", expr, got, tt.want)
			}
		})
	}
}

// TestReadkeyTimeout returns "" when no key comes in time, and does not
// count a skipped sequence as the timeout.
func TestReadkeyTimeout(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	start := time.Now()
	if got := h.eval(`v3.console.readkey(80)`).String(); got != "" {
		t.Errorf("readkey(80) with no input = %q, want \"\"", got)
	}
	if el := time.Since(start); el < 80*time.Millisecond || el > 2*time.Second {
		t.Errorf("readkey(80) took %v", el)
	}
	h2 := newHarness(t, harnessOpts{input: "\x1b[99z", inputChunks: []string{"k"}})
	if got := h2.eval(`v3.console.readkey(2000)`).String(); got != "k" {
		t.Errorf("readkey after an unknown sequence = %q, want k", got)
	}
}

// TestGetkeyStillDiscardsSequences keeps getkey as it was: special keys are
// swallowed, so existing scripts see no change. An SS3 arrow used to leave
// its letter behind as a key; now it is swallowed whole too.
func TestGetkeyStillDiscardsSequences(t *testing.T) {
	h := newHarness(t, harnessOpts{input: "\x1b[A\x1bOBz"})
	got := h.eval(`v3.console.getkey() + "|" + v3.console.getkey() + "|" + v3.console.getkey()`).String()
	if got != "||z" {
		t.Errorf("getkey x3 = %q, want %q", got, "||z")
	}
}

// TestCsiKeyName covers the names readkey takes from the editor's table,
// including sequences that name no key.
func TestCsiKeyName(t *testing.T) {
	for seq, want := range map[string]string{
		"\x1b[A": "UP", "\x1b[6~": "PGDN", "\x1b[3;2~": "DEL", "\x1b[99~": "", "\x1b[m": "", "\x1b[": "",
	} {
		if got := csiKeyName([]byte(seq)); got != want {
			t.Errorf("csiKeyName(%q) = %q, want %q", seq, got, want)
		}
	}
}
