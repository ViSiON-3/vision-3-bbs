package scripting

import (
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
)

// TestInputEncoding types non-ASCII characters on CP437 and UTF-8 sessions
// (#483). V3 scripts see Unicode strings: a CP437 byte is mapped through
// ansi.Cp437ToUnicode and a UTF-8 character is assembled from its bytes. The
// echo is in the session's encoding, maxLen counts characters and backspace
// removes a whole character.
func TestInputEncoding(t *testing.T) {
	cp437, utf8 := ansi.OutputModeCP437, ansi.OutputModeUTF8
	tests := []struct {
		name    string
		mode    ansi.OutputMode
		input   string
		chunks  []string
		expr    string
		want    string
		wantOut string // checked only when non-empty
	}{
		{name: "cp437 e-acute", mode: cp437, input: "caf\x82\r", expr: `v3.console.getstr(10)`, want: "café", wantOut: "caf\x82\r\n"},
		{name: "cp437 pound", mode: cp437, input: "\x9c5\r", expr: `v3.console.getstr(10)`, want: "£5", wantOut: "\x9c5\r\n"},
		{name: "cp437 box chars", mode: cp437, input: "\xc4\xdb\r", expr: `v3.console.getstr(10)`, want: "─█", wantOut: "\xc4\xdb\r\n"},
		{name: "cp437 backspace", mode: cp437, input: "a\x82\x08b\r", expr: `v3.console.getstr(10)`, want: "ab", wantOut: "a\x82\x08 \x08b\r\n"},
		{name: "cp437 maxlen counts characters", mode: cp437, input: "\x82\x82\x82\r", expr: `v3.console.getstr(2)`, want: "éé", wantOut: "\x82\x82\r\n"},
		{name: "cp437 upper leaves non-ASCII", mode: cp437, input: "a\x82\r", expr: `v3.console.getstr(10, {upper: true})`, want: "Aé", wantOut: "A\x82\r\n"},
		{name: "cp437 NBSP echoes its own byte", mode: cp437, input: "a\xff\r", expr: `v3.console.getstr(10)`, want: "a ", wantOut: "a\xff\r\n"},
		{name: "cp437 escape aborts", mode: cp437, input: "\x82\x1b", expr: `v3.console.getstr(10)`, want: ""},
		{name: "cp437 getkey", mode: cp437, input: "\x82", expr: `v3.console.getkey()`, want: "é"},
		{name: "utf8 e-acute", mode: utf8, input: "café\r", expr: `v3.console.getstr(10)`, want: "café", wantOut: "café\r\n"},
		{name: "utf8 pound", mode: utf8, input: "£5\r", expr: `v3.console.getstr(10)`, want: "£5", wantOut: "£5\r\n"},
		{name: "utf8 box chars", mode: utf8, input: "─█\r", expr: `v3.console.getstr(10)`, want: "─█", wantOut: "─█\r\n"},
		{name: "utf8 euro split across reads", mode: utf8, input: "a\xe2", chunks: []string{"\x82\xac", "b\r"}, expr: `v3.console.getstr(10)`, want: "a€b", wantOut: "a€b\r\n"},
		{name: "utf8 backspace removes whole char", mode: utf8, input: "a€\x08b\r", expr: `v3.console.getstr(10)`, want: "ab", wantOut: "a€\x08 \x08b\r\n"},
		{name: "utf8 backspace two-byte char", mode: utf8, input: "é\x7f\r", expr: `JSON.stringify(v3.console.getstr(10))`, want: `""`, wantOut: "é\x08 \x08\r\n"},
		{name: "utf8 maxlen counts characters", mode: utf8, input: "ééé\r", expr: `v3.console.getstr(2)`, want: "éé", wantOut: "éé\r\n"},
		{name: "utf8 maxlen three-byte", mode: utf8, input: "€€€\r", expr: `v3.console.getstr(2)`, want: "€€", wantOut: "€€\r\n"},
		{name: "utf8 malformed dropped", mode: utf8, input: "\xc3\xc3\xa9x\r", expr: `v3.console.getstr(10)`, want: "éx", wantOut: "éx\r\n"},
		{name: "utf8 number rejects non-ASCII", mode: utf8, input: "1é2\r", expr: `v3.console.getstr(10, {number: true})`, want: "12"},
		{name: "utf8 escape aborts", mode: utf8, input: "é\x1b", expr: `v3.console.getstr(10)`, want: ""},
		{name: "utf8 getkey", mode: utf8, input: "é", expr: `v3.console.getkey()`, want: "é"},
		{name: "utf8 getkey split across reads", mode: utf8, input: "\xe2", chunks: []string{"\x82\xac"}, expr: `v3.console.getkey()`, want: "€"},
		{name: "utf8 string length is characters", mode: utf8, input: "a€\r", expr: `String(v3.console.getstr(10).length)`, want: "2"},
		{name: "arrow key in own read discarded", mode: utf8, input: "a", chunks: []string{"\x1b[D", "b\r"}, expr: `v3.console.getstr(10)`, want: "ab", wantOut: "ab\r\n"},
		{name: "arrow key after typed keys discarded", mode: utf8, input: "a\x1b[Db\r", expr: `v3.console.getstr(10)`, want: "ab", wantOut: "ab\r\n"},
		{name: "CRLF after typed keys is one Enter", mode: cp437, input: "ab\r\ncd\r", expr: `v3.console.getstr(10) + "|" + v3.console.getstr(10)`, want: "ab|cd"},
		{name: "ascii unchanged", mode: utf8, input: "hi\r", expr: `v3.console.getstr(10)`, want: "hi", wantOut: "hi\r\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t, harnessOpts{input: tt.input, inputChunks: tt.chunks,
				session: func(sc *SessionContext) { sc.OutputMode = tt.mode }})
			if got := h.eval(tt.expr).String(); got != tt.want {
				t.Errorf("%s = %q, want %q", tt.expr, got, tt.want)
			}
			if tt.wantOut != "" && h.output() != tt.wantOut {
				t.Errorf("output = %q, want %q", h.output(), tt.wantOut)
			}
		})
	}
}
