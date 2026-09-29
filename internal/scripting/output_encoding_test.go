package scripting

import (
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
)

// TestOutputEncoding pins the bytes V3 console output sends for non-ASCII
// text (#491). A UTF-8 session gets the UTF-8 bytes; a CP437 session gets each
// character's CP437 byte, and a character CP437 lacks (€) becomes '?' without
// affecting the rest of the string. Pipe codes and escape sequences are
// unchanged in both.
func TestOutputEncoding(t *testing.T) {
	cp437, utf8 := ansi.OutputModeCP437, ansi.OutputModeUTF8
	const text = "café £ ─ €"
	tests := []struct {
		name string
		mode ansi.OutputMode
		src  string
		want string
	}{
		{"utf8 write", utf8, `v3.console.write(` + jsQuote(text) + `)`, text},
		{"utf8 write all mappable", utf8, `v3.console.write("café £ ─")`, "café £ ─"},
		{"cp437 write", cp437, `v3.console.write(` + jsQuote(text) + `)`, "caf\x82 \x9c \xc4 ?"},
		{"cp437 write all mappable", cp437, `v3.console.write("café £ ─ ½")`, "caf\x82 \x9c \xc4 \xab"},
		{"cp437 unmappable only affects itself", cp437, `v3.console.write("€é☃ü")`, "?\x82?\x81"},
		{"cp437 box corners", cp437, `v3.console.write("╜╛")`, "\xbd\xbe"},
		{"utf8 writeln", utf8, `v3.console.writeln("é")`, "é\r\n"},
		{"cp437 writeln", cp437, `v3.console.writeln("é€")`, "\x82?\r\n"},
		{"utf8 print expands pipes", utf8, `v3.console.print("|09café|07")`, pipe("|09") + "café" + pipe("|07")},
		{"cp437 print expands pipes", cp437, `v3.console.print("|09café €|07")`, pipe("|09") + "caf\x82 ?" + pipe("|07")},
		{"utf8 println", utf8, `v3.console.println("|12─")`, pipe("|12") + "─\r\n"},
		{"cp437 println", cp437, `v3.console.println("|12─")`, pipe("|12") + "\xc4\r\n"},
		{"utf8 escapes untouched", utf8, `v3.console.write("\x1b[1m€\x1b[0mé")`, "\x1b[1m€\x1b[0mé"},
		{"cp437 escapes untouched", cp437, `v3.console.write("\x1b[1m€\x1b[0mé")`, "\x1b[1m?\x1b[0m\x82"},
		{"utf8 center counts characters", utf8, `v3.console.center("|09café")`, strings.Repeat(" ", 38) + pipe("|09") + "café\r\n"},
		{"cp437 center", cp437, `v3.console.center("café")`, strings.Repeat(" ", 38) + "caf\x82\r\n"},
		{"utf8 clear", utf8, `v3.console.clear()`, "\x1b[2J\x1b[H"},
		{"cp437 gotoxy", cp437, `v3.console.gotoxy(2, 3)`, "\x1b[3;2H"},
		{"utf8 color", utf8, `v3.console.color(14, 1)`, pipe("|14|B1")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t, harnessOpts{session: func(sc *SessionContext) { sc.OutputMode = tt.mode }})
			h.mustRun(tt.src)
			if got := h.output(); got != tt.want {
				t.Errorf("output = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestPromptEncoding: yesno/noyes prompts are encoded for the session too.
func TestPromptEncoding(t *testing.T) {
	for _, tt := range []struct {
		mode ansi.OutputMode
		want string
	}{
		{ansi.OutputModeUTF8, "Café? (Y/n)? \r\n"},
		{ansi.OutputModeCP437, "Caf\x82? (Y/n)? \r\n"},
	} {
		h := newHarness(t, harnessOpts{input: "\r", session: func(sc *SessionContext) { sc.OutputMode = tt.mode }})
		h.eval(`v3.console.yesno("Café?")`)
		if got := h.output(); got != tt.want {
			t.Errorf("mode %d: output = %q, want %q", tt.mode, got, tt.want)
		}
	}
}

// TestAnsiDisplayEncoding: .ANS art is CP437. A CP437 session gets the file's
// bytes (with pipe codes expanded by display); a UTF-8 session gets them
// converted byte for byte, as menu screens are, even where a CP437 pair
// happens to be valid UTF-8 (█▓, DB B2 = U+06F2). Art already in UTF-8 is sent
// unchanged.
func TestAnsiDisplayEncoding(t *testing.T) {
	cp437, utf8 := ansi.OutputModeCP437, ansi.OutputModeUTF8
	// Not valid UTF-8 as a whole (C4 is followed by a space), so CP437.
	const art = "\x1b[1;30m\xdb\xb2|09\xc4 \x82"
	tests := []struct {
		name string
		mode ansi.OutputMode
		file string
		src  string
		want string
	}{
		{"cp437 display", cp437, art, `v3.ansi.display("art.ans")`, "\x1b[1;30m\xdb\xb2" + pipe("|09") + "\xc4 \x82"},
		{"cp437 displayRaw", cp437, art, `v3.ansi.displayRaw("art.ans")`, art},
		{"utf8 display", utf8, art, `v3.ansi.display("art.ans")`, "\x1b[1;30m█▓" + pipe("|09") + "─ é"},
		{"utf8 displayRaw", utf8, art, `v3.ansi.displayRaw("art.ans")`, "\x1b[1;30m█▓|09─ é"},
		{"utf8 art already utf8", utf8, "\x1b[0m█▓ é", `v3.ansi.displayRaw("art.ans")`, "\x1b[0m█▓ é"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t, harnessOpts{session: func(sc *SessionContext) { sc.OutputMode = tt.mode }})
			h.writeFile("scripts/art.ans", tt.file)
			h.mustRun(tt.src)
			if got := h.output(); got != tt.want {
				t.Errorf("output = %q, want %q", got, tt.want)
			}
		})
	}
}
