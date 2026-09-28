package syncjs

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
)

// jsBytes returns the bytes a JS string stands for in syncjs: each character
// code 0-255 is one byte, the way writeRaw and File.write emit it.
func jsBytes(h *doorHarness, expr string) string {
	h.t.Helper()
	return string(runesToBytes(h.eval(expr).String()))
}

// TestGetstrEncoding types non-ASCII characters into console.getstr on CP437
// and UTF-8 sessions (#483). Input is kept as the session's bytes, one JS
// character per byte, so it round-trips through output unchanged; on UTF-8
// sessions characters are assembled whole for maxLen and backspace.
func TestGetstrEncoding(t *testing.T) {
	cp437, utf8 := ansi.OutputModeCP437, ansi.OutputModeUTF8
	tests := []struct {
		name    string
		mode    ansi.OutputMode
		input   string
		chunks  []string
		expr    string
		want    string // bytes the returned string stands for
		wantOut string
	}{
		{name: "cp437 e-acute", mode: cp437, input: "caf\x82\r", expr: `console.getstr(10)`, want: "caf\x82", wantOut: "caf\x82\r\n"},
		{name: "cp437 pound", mode: cp437, input: "\x9c5\r", expr: `console.getstr(10)`, want: "\x9c5", wantOut: "\x9c5\r\n"},
		{name: "cp437 box chars", mode: cp437, input: "\xc4\xdb\r", expr: `console.getstr(10)`, want: "\xc4\xdb", wantOut: "\xc4\xdb\r\n"},
		{name: "cp437 backspace", mode: cp437, input: "a\x82\x08b\r", expr: `console.getstr(10)`, want: "ab", wantOut: "a\x82\x08 \x08b\r\n"},
		{name: "cp437 maxlen", mode: cp437, input: "\x82\x82\x82\r", expr: `console.getstr(2)`, want: "\x82\x82", wantOut: "\x82\x82\r\n"},
		{name: "cp437 escape aborts", mode: cp437, input: "\x82\x1b", expr: `console.getstr(10)`, want: ""},
		{name: "utf8 e-acute", mode: utf8, input: "café\r", expr: `console.getstr(10)`, want: "café", wantOut: "café\r\n"},
		{name: "utf8 pound", mode: utf8, input: "£5\r", expr: `console.getstr(10)`, want: "£5", wantOut: "£5\r\n"},
		{name: "utf8 box char", mode: utf8, input: "─█\r", expr: `console.getstr(10)`, want: "─█", wantOut: "─█\r\n"},
		{name: "utf8 euro split across reads", mode: utf8, input: "a\xe2", chunks: []string{"\x82\xac", "b\r"}, expr: `console.getstr(10)`, want: "a€b", wantOut: "a€b\r\n"},
		{name: "utf8 backspace removes whole char", mode: utf8, input: "a€\x08b\r", expr: `console.getstr(10)`, want: "ab", wantOut: "a€\x08 \x08b\r\n"},
		{name: "utf8 backspace two-byte char", mode: utf8, input: "é\x7f\r", expr: `console.getstr(10)`, want: "", wantOut: "é\x08 \x08\r\n"},
		{name: "utf8 maxlen counts characters", mode: utf8, input: "ééé\r", expr: `console.getstr(2)`, want: "éé", wantOut: "éé\r\n"},
		{name: "utf8 malformed dropped", mode: utf8, input: "\xc3\xc3\xa9x\r", expr: `console.getstr(10)`, want: "éx", wantOut: "éx\r\n"},
		{name: "utf8 upper leaves non-ASCII", mode: utf8, input: "aé\r", expr: `console.getstr(10, 1)`, want: "Aé", wantOut: "Aé\r\n"},
		{name: "utf8 number rejects non-ASCII", mode: utf8, input: "1é2\r", expr: `console.getstr(10, 4)`, want: "12"},
		{name: "utf8 escape aborts", mode: utf8, input: "é\x1b", expr: `console.getstr(10)`, want: ""},
		{name: "arrow key in own read discarded", mode: cp437, input: "a", chunks: []string{"\x1b[D", "b\r"}, expr: `console.getstr(10)`, want: "ab", wantOut: "ab\r\n"},
		{name: "ascii unchanged", mode: utf8, input: "hi\r", expr: `console.getstr(10)`, want: "hi", wantOut: "hi\r\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newDoor(t, doorOpts{input: tt.input, inputChunks: tt.chunks,
				session: func(sc *SessionContext) { sc.OutputMode = tt.mode }})
			if got := jsBytes(h, tt.expr); got != tt.want {
				t.Errorf("%s = %q, want %q", tt.expr, got, tt.want)
			}
			if tt.wantOut != "" && h.output() != tt.wantOut {
				t.Errorf("output = %q, want %q", h.output(), tt.wantOut)
			}
		})
	}
}

// TestGetstrRoundTrip: a typed line printed back or written to a data file
// produces exactly the bytes the terminal sent.
func TestGetstrRoundTrip(t *testing.T) {
	for _, tt := range []struct {
		name  string
		mode  ansi.OutputMode
		typed string
	}{
		{"cp437", ansi.OutputModeCP437, "\x82\x9c\xc4"},
		{"utf8", ansi.OutputModeUTF8, "é£─€"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			h := newDoor(t, doorOpts{input: tt.typed + "\r",
				session: func(sc *SessionContext) { sc.OutputMode = tt.mode }})
			h.mustRun(`
				var s = console.getstr(20, 16 | 32);
				console.print(s);
				var f = new File("typed.dat");
				f.open("w");
				f.write(s);
				f.close();
			`)
			if got := h.output(); got != tt.typed {
				t.Errorf("printed = %q, want %q", got, tt.typed)
			}
			data, err := os.ReadFile(filepath.Join(h.game, "typed.dat"))
			if err != nil {
				t.Fatal(err)
			}
			if string(data) != tt.typed {
				t.Errorf("file = %q, want %q", data, tt.typed)
			}
		})
	}
}

// TestSingleKeyEncoding: single-key reads return the input byte as one
// character code, including after a poll() pushes the key back, and getkeys
// upper-cases ASCII only.
func TestSingleKeyEncoding(t *testing.T) {
	tests := []struct {
		name, input, expr, want string
	}{
		{"getkey high byte", "\x82", `console.getkey()`, "\x82"},
		{"input queue poll then read", "\xa9", `var iq = load(true, "t.js"); iq.poll(1000); iq.read()`, "\xa9"},
		{"getkeys high byte not case-mapped", "\xe9", `console.getkeys()`, "\xe9"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newDoor(t, doorOpts{input: tt.input})
			if got := jsBytes(h, tt.expr); got != tt.want {
				t.Errorf("%s = %q, want %q", tt.expr, got, tt.want)
			}
		})
	}
}
