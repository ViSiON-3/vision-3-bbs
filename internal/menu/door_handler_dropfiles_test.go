package menu

import (
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gliderlabs/ssh"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/config"
)

func TestDropfileName(t *testing.T) {
	tests := []struct {
		name         string
		dropfileType string
		dropfileCase string
		want         string
	}{
		{"default empty is upper", "DOOR32.SYS", "", "DOOR32.SYS"},
		{"explicit upper", "DOOR32.SYS", "upper", "DOOR32.SYS"},
		{"lower", "DOOR32.SYS", "lower", "door32.sys"},
		{"lower case-insensitive key", "DOOR32.SYS", "Lower", "door32.sys"},
		{"unknown case defaults upper", "DOOR32.SYS", "weird", "DOOR32.SYS"},
		{"door.sys lower", "DOOR.SYS", "lower", "door.sys"},
		{"chain.txt lower", "CHAIN.TXT", "lower", "chain.txt"},
		{"dorinfo lower", "DORINFO1.DEF", "lower", "dorinfo1.def"},
		{"dropfile.ini lower", "DROPFILE.INI", "lower", "dropfile.ini"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := dropfileName(tt.dropfileType, tt.dropfileCase); got != tt.want {
				t.Errorf("dropfileName(%q, %q) = %q, want %q", tt.dropfileType, tt.dropfileCase, got, tt.want)
			}
		})
	}
}

func newTestDoorCtx() *DoorCtx {
	return &DoorCtx{
		Executor:    newExecutorWithServerConfig(config.ServerConfig{BoardName: "Test BBS"}),
		User:        doorUserInfo{ID: 1, Handle: "Neo", RealName: "Thomas Anderson", AccessLevel: 50, ScreenWidth: 80, ScreenHeight: 25},
		NodeNumStr:  "1",
		UserIDStr:   "1",
		TimeLeftMin: 30,
	}
}

func TestGenerateDoor32SysCase(t *testing.T) {
	dir := t.TempDir()
	ctx := newTestDoorCtx()

	if err := generateDoor32Sys(ctx, dir, "door32.sys"); err != nil {
		t.Fatalf("generateDoor32Sys: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "door32.sys")); err != nil {
		t.Errorf("expected lowercase door32.sys to exist: %v", err)
	}
}

// readDropfileIni generates a DROPFILE.INI for ctx and returns its raw bytes
// and its keys.
func readDropfileIni(t *testing.T, ctx *DoorCtx, tempDir string) (string, map[string]string) {
	t.Helper()
	dir := t.TempDir()
	if err := generateDropfileIni(ctx, dir, "DROPFILE.INI", tempDir); err != nil {
		t.Fatalf("generateDropfileIni: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "DROPFILE.INI"))
	if err != nil {
		t.Fatalf("read DROPFILE.INI: %v", err)
	}
	raw := string(data)
	keys := map[string]string{}
	for _, line := range strings.Split(raw, "\r\n") {
		if k, v, ok := strings.Cut(line, "="); ok {
			if _, dup := keys[k]; dup {
				t.Errorf("key %s written twice", k)
			}
			keys[k] = v
		}
	}
	return raw, keys
}

func TestGenerateDropfileIni(t *testing.T) {
	ctx := newTestDoorCtx()
	ctx.Executor = newExecutorWithServerConfig(config.ServerConfig{
		BoardName: "Test BBS", SysOpName: "Morpheus", SysOpLevel: 255, CoSysOpLevel: 250, MaxNodes: 4, QWKID: "testbbs",
	})
	ctx.NodeNumber = 2
	ctx.User.ScreenWidth, ctx.User.ScreenHeight = 132, 37
	ctx.OutputMode = ansi.OutputModeCP437
	ctx.Config.Code, ctx.Config.Name = "LORD", "Legend of the Red Dragon"

	raw, keys := readDropfileIni(t, ctx, "/tmp/node2")

	if !strings.HasSuffix(raw, "\r\n") || strings.Contains(strings.ReplaceAll(raw, "\r\n", ""), "\n") {
		t.Errorf("every line must end with CRLF")
	}
	if first := strings.Index(raw, "["); !strings.HasPrefix(raw[first:], "[file]\r\n") {
		t.Errorf("[file] must be the first section")
	}
	var sections []string
	for _, line := range strings.Split(raw, "\r\n") {
		if strings.HasPrefix(line, "[") {
			sections = append(sections, line)
		}
	}
	if got, want := strings.Join(sections, ""), "[file][system][comm][user][terminal][session][door][x-vision3]"; got != want {
		t.Errorf("sections = %s, want %s", got, want)
	}
	for _, line := range strings.Split(raw, "\r\n") {
		if len(line) > dropfileIniMaxLine {
			t.Errorf("line exceeds %d bytes: %q", dropfileIniMaxLine, line)
		}
	}

	want := map[string]string{
		"SYS_NAME":        "Test BBS",
		"SYS_OP":          "Morpheus",
		"SYS_VENDOR":      "VISION3",
		"SYS_NODE_NUM":    "2",
		"SYS_NODE_COUNT":  "4",
		"SYS_QWKID":       "TESTBBS",
		"COMM_TYPE":       "stdio",
		"COMM_CHARSET":    "CP437",
		"USER_ALIAS":      "Neo",
		"USER_NUMBER":     "1",
		"USER_ROLE":       "user",
		"USER_REALNAME":   "Thomas Anderson",
		"TERM_COLS":       "132",
		"TERM_ROWS":       "37",
		"TERM_TYPE":       "ansi",
		"TERM_CHARSET":    "CP437",
		"TIME_LEFT":       "1800",
		"TEMP_DIR":        "/tmp/node2",
		"X_VISION3_LEVEL": "50",
		"LOCAL_DISPLAY":   "0",
		"DOOR_CODE":       "LORD",
		"DOOR_NAME":       "Legend of the Red Dragon",
	}
	for k, v := range want {
		if got, ok := keys[k]; !ok || got != v {
			t.Errorf("%s = %q (present=%v), want %q", k, got, ok, v)
		}
	}
	for _, k := range []string{"SYS_SOFTWARE", "FILE_TIME"} {
		if keys[k] == "" {
			t.Errorf("%s missing", k)
		}
	}
	// Nothing in the file is UTF-8, and empty optional keys are left out.
	// IDLE_LIMIT and SYS_FTN_ADDR are never written (see the writer's notes),
	// and with no session there is no IP or terminal type to report.
	for _, k := range []string{"FILE_UTF8", "USER_LOCATION", "COMM_HANDLE", "COMM_PORT",
		"IDLE_LIMIT", "SYS_FTN_ADDR", "USER_IP", "TERM_TERMINFO"} {
		if _, ok := keys[k]; ok {
			t.Errorf("%s should not be written", k)
		}
	}
}

func TestGenerateDropfileIniSanitizesText(t *testing.T) {
	ctx := newTestDoorCtx()
	ctx.Executor = newExecutorWithServerConfig(config.ServerConfig{BoardName: "  ", SysOpLevel: 255, CoSysOpLevel: 250})
	ctx.User.Handle = " J\u00f6rg\tthe\x07Red\r\nCOMM_TYPE=local "
	// Line and paragraph separators split lines in some readers, and
	// bidirectional and C1 controls are invisible: all are control
	// characters, so none may reach the file.
	ctx.Config.Name = "A\u2028USER_ROLE=sysop\u2029\u202e\u2066\u0085x"
	ctx.User.RealName = strings.Repeat("x", 400)
	ctx.User.GroupLocation = "\u00a0"
	ctx.User.AccessLevel = 255

	_, keys := readDropfileIni(t, ctx, "")

	if got, want := keys["USER_ALIAS"], "J\x94rg?the?Red??COMM_TYPE=local"; got != want {
		t.Errorf("USER_ALIAS = %q, want %q", got, want)
	}
	if got := keys["COMM_TYPE"]; got != "stdio" {
		t.Errorf("COMM_TYPE = %q; a user value must not inject a key", got)
	}
	if got := keys["SYS_NAME"]; got != "?" {
		t.Errorf("SYS_NAME = %q, want ? for an empty required key", got)
	}
	if got := len("USER_REALNAME=" + keys["USER_REALNAME"]); got != dropfileIniMaxLine {
		t.Errorf("USER_REALNAME line is %d bytes, want %d", got, dropfileIniMaxLine)
	}
	if _, ok := keys["USER_LOCATION"]; ok {
		t.Errorf("USER_LOCATION should be left out when empty after sanitizing")
	}
	if _, ok := keys["TEMP_DIR"]; ok {
		t.Errorf("TEMP_DIR should be left out when there is no per-node directory")
	}
	if got := keys["USER_ROLE"]; got != "sysop" {
		t.Errorf("USER_ROLE = %q, want sysop", got)
	}
	if got, want := keys["DOOR_NAME"], "A?USER_ROLE=sysop????x"; got != want {
		t.Errorf("DOOR_NAME = %q, want %q", got, want)
	}
}

// dropfileIniSession is a session that reports a terminal type and a remote
// address, as the SSH and telnet adapters do.
type dropfileIniSession struct {
	ssh.Session
	term string
}

func (s dropfileIniSession) Pty() (ssh.Pty, <-chan ssh.Window, bool) {
	return ssh.Pty{Term: s.term}, nil, s.term != ""
}

func (s dropfileIniSession) RemoteAddr() net.Addr {
	return &net.TCPAddr{IP: net.IPv4(203, 0, 113, 7), Port: 51234}
}

func TestGenerateDropfileIniSessionKeys(t *testing.T) {
	ctx := newTestDoorCtx()
	ctx.User.GroupLocation = "Zion"
	ctx.Session = dropfileIniSession{term: "xterm-256color"}

	_, keys := readDropfileIni(t, ctx, "")
	want := map[string]string{
		"TERM_TERMINFO": "xterm-256color",
		"USER_IP":       "203.0.113.7",
		"USER_REALNAME": "Thomas Anderson",
		"USER_LOCATION": "Zion",
		"USER_PROTOCOL": "ssh",
	}
	for k, v := range want {
		if keys[k] != v {
			t.Errorf("%s = %q, want %q", k, keys[k], v)
		}
	}

	// A type that isn't a valid ascii value is left out, and so is one from
	// a client that sent none.
	for _, term := range []string{"vt 100", ""} {
		ctx.Session = dropfileIniSession{term: term}
		if _, keys := readDropfileIni(t, ctx, ""); keys["TERM_TERMINFO"] != "" {
			t.Errorf("terminal type %q: TERM_TERMINFO = %q, want it left out", term, keys["TERM_TERMINFO"])
		}
	}
}

// The sysop can keep a door from seeing the user's personal details.
func TestGenerateDropfileIniHidePersonal(t *testing.T) {
	ctx := newTestDoorCtx()
	ctx.User.GroupLocation = "Zion"
	ctx.Session = dropfileIniSession{term: "ansi"}
	ctx.Config.DropfileHidePersonal = true

	_, keys := readDropfileIni(t, ctx, "")
	for _, k := range []string{"USER_REALNAME", "USER_LOCATION", "USER_IP"} {
		if v, ok := keys[k]; ok {
			t.Errorf("%s = %q written although personal details are hidden", k, v)
		}
	}
	if keys["USER_ALIAS"] != "Neo" || keys["USER_PROTOCOL"] != "ssh" || keys["TERM_TERMINFO"] != "ansi" {
		t.Errorf("hiding personal details dropped other keys: %v", keys)
	}
}

func TestDropfileIniPath(t *testing.T) {
	long := "/" + strings.Repeat("d", dropfileIniMaxLine-len("TEMP_DIR=")-1)
	tests := []struct {
		name, val, want string
	}{
		{"posix", "/tmp/node2", "/tmp/node2"},
		{"dos", `C:\NODE1`, `C:\NODE1`},
		// Paths are bytes from the file system: UTF-8 and Latin-1 names are
		// written as they are, never converted to CP437.
		{"utf-8 name kept", "/home/j\u00f6rg", "/home/j\u00f6rg"},
		{"latin-1 name kept", "/home/j\xf6rg", "/home/j\xf6rg"},
		{"exactly fits", long, long},
		// Anything that would need changing is left out rather than cut.
		{"too long", long + "d", ""},
		{"control character", "/tmp/a\tb", ""},
		{"DEL", "/tmp/a\x7fb", ""},
		{"line separator", "/tmp/a\u2028b", ""},
		{"bidi override", "/tmp/\u202eb", ""},
		{"trailing space", "/tmp/a ", ""},
		{"leading tab", "\t/tmp/a", ""},
		// Only space and tab are whitespace to the spec: a name ending in an
		// em space or ideographic space is kept.
		{"unicode space kept", "/tmp/a\u2003", "/tmp/a\u2003"},
		{"ideographic space kept", "/tmp/\u3000a", "/tmp/\u3000a"},
		{"empty", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := dropfileIniPath("TEMP_DIR", tt.val); got != tt.want {
				t.Errorf("dropfileIniPath(%q) = %q, want %q", tt.val, got, tt.want)
			}
		})
	}
}

func TestGenerateDropfileIniComm(t *testing.T) {
	tests := []struct {
		name        string
		cfg         config.DoorConfig
		mode        ansi.OutputMode
		wantType    string
		wantCharset string
		wantKey     string
		wantVal     string
	}{
		{"native stdio utf8", config.DoorConfig{}, ansi.OutputModeUTF8, "stdio", "UTF-8", "", ""},
		{"native socket", config.DoorConfig{IOMode: "socket"}, ansi.OutputModeCP437, "socket", "CP437", "COMM_HANDLE", "3"},
		{"dos fossil", config.DoorConfig{IsDOS: true, FossilDriver: "C:\\BNU\\BNU.COM"}, ansi.OutputModeUTF8, "fossil", "UTF-8", "COMM_PORT", "1"},
		{"dos terminal", config.DoorConfig{IsDOS: true, IOMode: "SOCKET"}, ansi.OutputModeUTF8, "stdio", "CP437", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := newTestDoorCtx()
			ctx.Config = tt.cfg
			ctx.OutputMode = tt.mode
			ctx.User.ScreenWidth, ctx.User.ScreenHeight = 132, 50
			_, keys := readDropfileIni(t, ctx, "")
			if keys["COMM_TYPE"] != tt.wantType {
				t.Errorf("COMM_TYPE = %q, want %q", keys["COMM_TYPE"], tt.wantType)
			}
			if keys["COMM_CHARSET"] != tt.wantCharset {
				t.Errorf("COMM_CHARSET = %q, want %q", keys["COMM_CHARSET"], tt.wantCharset)
			}
			if tt.wantKey != "" && keys[tt.wantKey] != tt.wantVal {
				t.Errorf("%s = %q, want %q", tt.wantKey, keys[tt.wantKey], tt.wantVal)
			}
			if tt.cfg.IsDOS && (keys["TERM_COLS"] != "80" || keys["TERM_ROWS"] != "25") {
				t.Errorf("DOS door screen = %sx%s, want 80x25", keys["TERM_COLS"], keys["TERM_ROWS"])
			}
		})
	}
}

func TestDoorUsesNodeDir(t *testing.T) {
	tests := []struct {
		name string
		cfg  config.DoorConfig
		want bool
	}{
		{"startup door.sys", config.DoorConfig{DropfileType: "DOOR.SYS"}, false},
		{"node door.sys", config.DoorConfig{DropfileType: "DOOR.SYS", DropfileLocation: "Node"}, true},
		{"startup ini multi-node", config.DoorConfig{DropfileType: "dropfile.ini", DropfileLocation: "startup"}, true},
		{"startup ini single instance", config.DoorConfig{DropfileType: "DROPFILE.INI", SingleInstance: true}, false},
		{"no dropfile", config.DoorConfig{}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := doorUsesNodeDir(tt.cfg); got != tt.want {
				t.Errorf("doorUsesNodeDir = %v, want %v", got, tt.want)
			}
		})
	}
}
