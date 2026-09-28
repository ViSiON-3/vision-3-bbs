package scripting

import (
	"strings"
	"testing"
)

// TestConsoleOutput pins the bytes each v3.console output call emits.
func TestConsoleOutput(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
	}{
		{"write concatenates args", `v3.console.write("a", 1, true)`, "a1true"},
		{"write no args", `v3.console.write()`, ""},
		{"writeln", `v3.console.writeln("hi")`, "hi\r\n"},
		{"write leaves pipe codes", `v3.console.write("|09x")`, "|09x"},
		{"print expands pipe codes", `v3.console.print("|09x")`, pipe("|09x")},
		{"println", `v3.console.println("|12y")`, pipe("|12y") + "\r\n"},
		{"clear", `v3.console.clear()`, "\x1b[2J\x1b[H"},
		{"cls alias", `v3.console.cls()`, "\x1b[2J\x1b[H"},
		{"gotoxy is x,y -> row;col", `v3.console.gotoxy(5, 10)`, "\x1b[10;5H"},
		{"gotoxy needs two args", `v3.console.gotoxy(5)`, ""},
		{"color fg", `v3.console.color(9)`, pipe("|09")},
		{"color fg bg", `v3.console.color(14, 1)`, pipe("|14|B1")},
		{"color no args", `v3.console.color()`, ""},
		{"reset", `v3.console.reset()`, "\x1b[0m"},
		{"center pads to width", `v3.console.center("hi")`, strings.Repeat(" ", 39) + "hi\r\n"},
		{"center ignores pipe codes in width", `v3.console.center("|09hi")`, strings.Repeat(" ", 39) + pipe("|09hi") + "\r\n"},
		{"center overwide unpadded", `v3.console.center("` + strings.Repeat("x", 90) + `")`, strings.Repeat("x", 90) + "\r\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t, harnessOpts{})
			h.mustRun(tt.src)
			if got := h.output(); got != tt.want {
				t.Errorf("output = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestConsoleDimensions exposes the session's screen size.
func TestConsoleDimensions(t *testing.T) {
	h := newHarness(t, harnessOpts{session: func(sc *SessionContext) { sc.ScreenWidth, sc.ScreenHeight = 132, 50 }})
	if w, hh := h.eval("v3.console.width").ToInteger(), h.eval("v3.console.height").ToInteger(); w != 132 || hh != 50 {
		t.Errorf("width/height = %d/%d, want 132/50", w, hh)
	}
}

// TestConsoleInput drives the input calls with scripted keystrokes and checks
// the value returned to JS plus the echoed output.
func TestConsoleInput(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		expr    string
		want    string
		wantOut string // checked only when non-empty
	}{
		{name: "getkey", input: "A", expr: `v3.console.getkey()`, want: "A"},
		{name: "getkey arrow swallowed", input: "\x1b[Ab", expr: `v3.console.getkey() + "|" + v3.console.getkey()`, want: "|b"},
		{name: "getkey CRLF is one CR", input: "\r\nz", expr: `JSON.stringify([v3.console.getkey(), v3.console.getkey()])`, want: `["\r","z"]`},
		{name: "getkey timeout", expr: `v3.console.getkey(20)`, want: ""},
		{name: "getstr basic", input: "hello\r", expr: `v3.console.getstr()`, want: "hello", wantOut: "hello\r\n"},
		{name: "getstr backspace", input: "ab\x08c\r", expr: `v3.console.getstr(10)`, want: "ac", wantOut: "ab\x08 \x08c\r\n"},
		{name: "getstr DEL on empty", input: "\x7fx\r", expr: `v3.console.getstr(10)`, want: "x"},
		{name: "getstr maxlen", input: "abcd\r", expr: `v3.console.getstr(2)`, want: "ab", wantOut: "ab\r\n"},
		{name: "getstr upper", input: "hi\r", expr: `v3.console.getstr(10, {upper: true})`, want: "HI", wantOut: "HI\r\n"},
		{name: "getstr number only", input: "1a2\r", expr: `v3.console.getstr(10, {number: true})`, want: "12"},
		{name: "getstr no echo", input: "pw\x08x\r", expr: `v3.console.getstr(10, {echo: false})`, want: "px", wantOut: "\r\n"},
		{name: "getstr escape aborts", input: "ab\x1b", expr: `v3.console.getstr(10)`, want: ""},
		{name: "getstr LF ends line", input: "ok\n", expr: `v3.console.getstr(10)`, want: "ok"},
		{name: "yesno default yes", input: "\r", expr: `String(v3.console.yesno("Go"))`, want: "true", wantOut: "Go (Y/n)? \r\n"},
		{name: "yesno n", input: "n", expr: `String(v3.console.yesno("Go"))`, want: "false"},
		{name: "noyes default no", input: "x", expr: `String(v3.console.noyes("Quit"))`, want: "true", wantOut: "Quit (N/y)? \r\n"},
		{name: "noyes y", input: "Y", expr: `String(v3.console.noyes("Quit"))`, want: "false"},
		{name: "pause", input: " ", expr: `v3.console.pause(), "ok"`, want: "ok", wantOut: "\r\n[Press any key] \r\n"},
		{name: "getnum", input: "42\r", expr: `String(v3.console.getnum(0))`, want: "42"},
		{name: "getnum clamps to max", input: "250\r", expr: `String(v3.console.getnum(100))`, want: "100"},
		{name: "getnum empty is 0", input: "\r", expr: `String(v3.console.getnum(5))`, want: "0"},
		{name: "getnum no arg", input: "7\r", expr: `String(v3.console.getnum())`, want: "7"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t, harnessOpts{input: tt.input})
			if got := h.eval(tt.expr).String(); got != tt.want {
				t.Errorf("%s = %q, want %q", tt.expr, got, tt.want)
			}
			if tt.wantOut != "" && h.output() != tt.wantOut {
				t.Errorf("output = %q, want %q", h.output(), tt.wantOut)
			}
		})
	}
}
