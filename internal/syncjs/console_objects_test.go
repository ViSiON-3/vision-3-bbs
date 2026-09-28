package syncjs

import (
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestConsoleOutput pins the bytes each console output method emits.
func TestConsoleOutput(t *testing.T) {
	tests := []struct{ src, want string }{
		{`console.write("a", 1, true)`, "a1true"},
		{`console.write()`, ""},
		{`console.writeln("hi")`, "hi\r\n"},
		{`console.print("\x01rred\x01n")`, "\x1b[0;31mred\x1b[0m"},
		{`console.clear()`, "\x1b[2J\x1b[H"},
		{`console.home()`, "\x1b[H"},
		{`console.cleartoeol()`, "\x1b[K"},
		{`console.gotoxy(3, 7)`, "\x1b[7;3H"},
		{`console.gotoxy(3)`, ""},
		{`console.center("hi")`, strings.Repeat(" ", 39) + "hi\r\n"},
		{`console.center("\x01hhi")`, strings.Repeat(" ", 39) + "\x01hhi\r\n"},
		{`console.center("` + strings.Repeat("x", 81) + `")`, strings.Repeat("x", 81) + "\r\n"},
		{`console.right(); console.right(4); console.right(0)`, "\x1b[1C\x1b[4C"},
		{`console.left(); console.left(2); console.left(-1)`, "\x1b[1D\x1b[2D"},
		{`console.up(); console.up(3); console.up(0)`, "\x1b[1A\x1b[3A"},
		{`console.down(); console.down(5); console.down(0)`, "\x1b[1B\x1b[5B"},
		{`console.attributes = 0x1E`, AttrToANSI(0x1E)},
	}
	for _, tt := range tests {
		h := newDoor(t, doorOpts{})
		h.mustRun(tt.src)
		if got := h.output(); got != tt.want {
			t.Errorf("%s wrote %q, want %q", tt.src, got, tt.want)
		}
	}
}

// TestConsoleProperties checks console state properties and strlen.
func TestConsoleProperties(t *testing.T) {
	h := newDoor(t, doorOpts{session: func(sc *SessionContext) { sc.ScreenWidth, sc.ScreenHeight = 100, 40 }})
	tests := []struct{ expr, want string }{
		{`console.screen_columns + "x" + console.screen_rows`, "100x40"},
		{`String(console.attributes)`, "7"},
		{`console.attributes = 0x4F; String(console.attributes)`, "79"},
		{`String(console.line_counter)`, "0"},
		{`String(console.autoterm)`, "1"},
		{`String(console.ctrlkey_passthru)`, "0"},
		{`String(console.strlen("\x01hab\x1b[1;31mc"))`, "3"},
		{`String(console.strlen("é☃"))`, "2"},
	}
	for _, tt := range tests {
		if got := h.eval(tt.expr).String(); got != tt.want {
			t.Errorf("%s = %q, want %q", tt.expr, got, tt.want)
		}
	}
}

// TestConsoleInput drives the console input methods with scripted keys.
func TestConsoleInput(t *testing.T) {
	tests := []struct {
		name, input, expr, want, wantOut string
	}{
		{name: "getkey", input: "q", expr: `console.getkey()`, want: "q"},
		{name: "getkey arrow", input: "\x1b[A", expr: `console.getkey() === "\x01\x48"`, want: "true"},
		{name: "inkey timeout", expr: `console.inkey(20)`, want: ""},
		{name: "inkey", input: "z", expr: `console.inkey(1000)`, want: "z"},
		{name: "getstr", input: "hello\r", expr: `console.getstr()`, want: "hello", wantOut: "hello\r\n"},
		{name: "getstr maxlen", input: "abcdef\r", expr: `console.getstr(3)`, want: "abc"},
		{name: "getstr upper", input: "ab\r", expr: `console.getstr(10, 1)`, want: "AB", wantOut: "AB\r\n"},
		{name: "getstr number", input: "1x2\r", expr: `console.getstr(10, 4)`, want: "12"},
		{name: "getstr noecho", input: "pw\r", expr: `console.getstr(10, 16)`, want: "pw", wantOut: "\r\n"},
		{name: "getstr nocrlf", input: "ok\r", expr: `console.getstr(10, 32)`, want: "ok", wantOut: "ok"},
		{name: "getstr backspace", input: "ab\x7fc\r", expr: `console.getstr(10)`, want: "ac", wantOut: "ab\x08 \x08c\r\n"},
		{name: "getstr backspace on empty", input: "\x08x\r", expr: `console.getstr(10)`, want: "x"},
		{name: "getstr escape", input: "ab\x1b", expr: `console.getstr(10)`, want: ""},
		{name: "getkeys filters", input: "xzb", expr: `console.getkeys("ab")`, want: "B"},
		{name: "getkeys any", input: "q", expr: `console.getkeys()`, want: "Q"},
		{name: "pause", input: " ", expr: `console.pause(), "ok"`, want: "ok", wantOut: "\r\n[Hit a key] \r\n"},
		{name: "yesno default", input: "\r", expr: `String(console.yesno("Play"))`, want: "true", wantOut: "Play (Y/n)? \r\n"},
		{name: "yesno no", input: "n", expr: `String(console.yesno("Play"))`, want: "false"},
		{name: "noyes default", input: "\r", expr: `String(console.noyes("Quit"))`, want: "true", wantOut: "Quit (N/y)? \r\n"},
		{name: "noyes yes", input: "y", expr: `String(console.noyes("Quit"))`, want: "false"},
		{name: "getnum", input: "42\r", expr: `String(console.getnum(99))`, want: "42"},
		{name: "getnum clamps", input: "75\r", expr: `String(console.getnum(50))`, want: "50"},
		{name: "getnum empty", input: "\r", expr: `String(console.getnum(9))`, want: "0"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newDoor(t, doorOpts{input: tt.input})
			if got := h.eval(tt.expr).String(); got != tt.want {
				t.Errorf("%s = %q, want %q", tt.expr, got, tt.want)
			}
			if tt.wantOut != "" && h.output() != tt.wantOut {
				t.Errorf("output = %q, want %q", h.output(), tt.wantOut)
			}
		})
	}
}

// TestConsoleGetkeysDisconnectThrows: getkeys has no empty-string escape
// hatch, so a hangup surfaces as a JS exception.
func TestConsoleGetkeysDisconnectThrows(t *testing.T) {
	h := newDoor(t, doorOpts{})
	h.disconnect()
	msg := h.evalErr(`console.getkeys("YN")`)
	if !strings.Contains(msg, "disconnected") && !strings.Contains(msg, "terminated") {
		t.Errorf("getkeys after hangup threw %q", msg)
	}
}

// TestSessionObjects checks bbs, user, system, server and client reflect the
// session context.
func TestSessionObjects(t *testing.T) {
	start := time.Now().Add(-5 * time.Minute)
	h := newDoor(t, doorOpts{session: func(sc *SessionContext) { sc.SessionStartTime = start }})
	tests := []struct{ expr, want string }{
		{`String(bbs.node_num)`, "2"},
		{`String(bbs.logon_time)`, strconv.FormatInt(start.Unix(), 10)},
		{`String(bbs.online)`, "true"},
		{`bbs.sys_status = 12; String(bbs.sys_status)`, "12"},
		{`typeof bbs.mods`, "object"},
		{`["USER","ALIAS","NAME","REALNAME","NODE","SYSOP","BBS","NOPE"].map(bbs.atcode).join("|")`, "Tester|Tester|Test User|Test User|2|Sysop|My Test Board|"},
		{`bbs.atcode()`, ""},
		{`[user.alias, user.name, user.number, user.security.level, user.level, user.full_name,
		   user.location, user.handle, user.settings, user.stats.total_logons, user.security.password].join("|")`,
			"Tester|Test User|5|60|60|Test User|Testville|Tester|2|9|"},
		{`[system.name, system.operator, system.qwk_id, system.nodes].join("|")`, "My Test Board|Sysop|MYTESTBO|4"},
		{`system.exec_dir === system.ctrl_dir && system.data_dir === system.text_dir`, "true"},
		{`system.timer > 0`, "true"},
		{`server.version`, "ViSiON/3 SyncJS"},
		{`client.protocol + "|" + client.socket.descriptor + "|" + client.ip_address`, "SSH|-1|127.0.0.1"},
	}
	for _, tt := range tests {
		if got := h.eval(tt.expr).String(); got != tt.want {
			t.Errorf("%s = %q, want %q", tt.expr, got, tt.want)
		}
	}
	// 30-minute limit, 5 used.
	if left := h.eval(`bbs.get_time_left()`).ToInteger(); left < 24*60 || left > 25*60 {
		t.Errorf("get_time_left = %d, want ~1500", left)
	}
}

// TestTimeLeftEdges: no limit reports an hour; an overrun limit floors at 0.
func TestTimeLeftEdges(t *testing.T) {
	h := newDoor(t, doorOpts{session: func(sc *SessionContext) { sc.TimeLimit = 0 }})
	if got := h.eval(`bbs.get_time_left()`).ToInteger(); got != 3600 {
		t.Errorf("no-limit time left = %d, want 3600", got)
	}
	h2 := newDoor(t, doorOpts{session: func(sc *SessionContext) {
		sc.TimeLimit = 1
		sc.SessionStartTime = time.Now().Add(-time.Hour)
	}})
	if got := h2.eval(`bbs.get_time_left()`).ToInteger(); got != 0 {
		t.Errorf("overrun time left = %d, want 0", got)
	}
}

// TestMakeQWKID uppercases, drops spaces and caps at eight characters.
func TestMakeQWKID(t *testing.T) {
	for in, want := range map[string]string{"vision bbs": "VISIONBB", "abc": "ABC", "": ""} {
		if got := makeQWKID(in); got != want {
			t.Errorf("makeQWKID(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestCtrlAToANSICodes covers every Ctrl-A code mapping.
func TestCtrlAToANSICodes(t *testing.T) {
	want := map[string]string{
		"Kk": "\x1b[0;30m", "Rr": "\x1b[0;31m", "Gg": "\x1b[0;32m", "Yy": "\x1b[0;33m",
		"Bb": "\x1b[0;34m", "Mm": "\x1b[0;35m", "Cc": "\x1b[0;36m", "Ww": "\x1b[0;37m",
		"Hh": "\x1b[1m", "Ii": "\x1b[5m", "Nn-": "\x1b[0m",
		"0": "\x1b[40m", "1": "\x1b[44m", "2": "\x1b[42m", "3": "\x1b[46m",
		"4": "\x1b[41m", "5": "\x1b[45m", "6": "\x1b[43m", "7": "\x1b[47m",
		"[": "\x1b[s", "]": "\x1b[u", "Ll": "\x1b[K", "?z": "",
	}
	for codes, seq := range want {
		for i := 0; i < len(codes); i++ {
			if got := ctrlAToANSI(codes[i]); got != seq {
				t.Errorf("ctrlAToANSI(%q) = %q, want %q", codes[i], got, seq)
			}
		}
	}
}
