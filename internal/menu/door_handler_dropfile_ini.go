package menu

import (
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/version"
)

// DROPFILE.INI is the named-value drop file drafted by Synchronet (draft 0.6):
// https://github.com/SynchronetBBS/sbbs/blob/0fc1667377/docs/dropfile_ini.md
//
// SYS_FTN_ADDR is the one key the spec defines that this writer leaves out:
// it lists the primary address first, and FTN networks here are configured
// with no primary among them.

const (
	dropfileIniType = "DROPFILE.INI"
	// dropfileIniEnvVar names the environment variable that carries the file's
	// absolute path to the door.
	dropfileIniEnvVar = "DROPFILE_INI"
	// dropfileIniVendor is the vendor name used in SYS_VENDOR and X_ keys.
	dropfileIniVendor = "VISION3"
	// dropfileIniMaxLine is the longest line the format allows, excluding CRLF.
	dropfileIniMaxLine = 255
	// doorSocketFD is the descriptor a SOCKET I/O door inherits its socket on.
	doorSocketFD = 3
)

// qwkIDPattern matches a system ID that is valid for SYS_QWKID.
var qwkIDPattern = regexp.MustCompile(`^[A-Z][A-Z0-9_-]{0,7}$`)

// isGeneratedDropfileType reports whether upperType (already upper-cased) is a
// dropfile format the BBS knows how to write.
func isGeneratedDropfileType(upperType string) bool {
	switch upperType {
	case "DOOR.SYS", "DOOR32.SYS", "CHAIN.TXT", "DORINFO1.DEF", dropfileIniType:
		return true
	}
	return false
}

// doorUsesNodeDir reports whether a native door's dropfile goes in a per-node
// temporary directory instead of the working directory. That is the sysop's
// choice, except that DROPFILE.INI may never share a path between nodes: a
// door that several nodes can run at once always gets a per-node directory.
func doorUsesNodeDir(cfg config.DoorConfig) bool {
	if strings.EqualFold(cfg.DropfileLocation, "node") {
		return true
	}
	return strings.EqualFold(cfg.DropfileType, dropfileIniType) && !cfg.SingleInstance
}

// dropfileIniEnv returns the DROPFILE_INI environment entry for the dropfile at
// path. The door must be given an absolute path.
func dropfileIniEnv(path string) string {
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	return dropfileIniEnvVar + "=" + path
}

// dropfileIniText makes a stored string safe for a text value: it is converted
// to CP437 (the file never sets FILE_UTF8), control characters become '?',
// non-breaking spaces become spaces, surrounding whitespace is removed and the
// result is cut to fit the line limit for key.
func dropfileIniText(key, val string) string {
	b := []byte(toCP437Safe(strings.ReplaceAll(val, "\u00a0", " ")))
	for i, c := range b {
		switch {
		case c == 0xFF:
			b[i] = ' '
		case c < 0x20, c == 0x7F:
			b[i] = '?'
		}
	}
	if maxLen := dropfileIniMaxLine - len(key) - 1; len(b) > maxLen {
		b = b[:maxLen]
	}
	return strings.TrimSpace(string(b))
}

// dropfileIniASCII returns val if it is a valid ascii value (printable ASCII,
// no spaces) that fits the line limit for key, and "" otherwise.
func dropfileIniASCII(key, val string) string {
	val = strings.TrimSpace(val)
	if len(val) > dropfileIniMaxLine-len(key)-1 {
		return ""
	}
	for i := 0; i < len(val); i++ {
		if val[i] < 0x21 || val[i] > 0x7E {
			return ""
		}
	}
	return val
}

// dropfileIniPath returns val if it can be written as a path value, and ""
// otherwise. A path is written byte for byte as the file system gives it,
// never converted to the file's text encoding and never cut, so one that
// contains a control character or surrounding whitespace, or doesn't fit
// the line limit for key, is left out.
func dropfileIniPath(key, val string) string {
	// Whitespace here is the spec's: ASCII space and tab only. Other Unicode
	// spaces are ordinary characters in a file name.
	if val == "" || len(val) > dropfileIniMaxLine-len(key)-1 || strings.Trim(val, " \t") != val {
		return ""
	}
	for i := 0; i < len(val); i++ {
		if c := val[i]; c < 0x20 || c == 0x7F {
			return ""
		}
	}
	// A UTF-8 path is checked for the control characters the spec defines
	// for UTF-8 text, since those split lines in some readers.
	if utf8.ValidString(val) && strings.IndexFunc(val, dropfileIniUTF8Control) >= 0 {
		return ""
	}
	return val
}

// dropfileIniUTF8Control reports whether r is a control character in UTF-8
// text beyond the ASCII ones: C1 controls, the line and paragraph separators
// (which Python's splitlines and JavaScript multiline regular expressions
// treat as line breaks), and the bidirectional format characters.
func dropfileIniUTF8Control(r rune) bool {
	switch {
	case r >= 0x80 && r <= 0x9F,
		r == 0x2028, r == 0x2029,
		r >= 0x202A && r <= 0x202E,
		r >= 0x2066 && r <= 0x2069:
		return true
	}
	return false
}

// dropfileIniWriter accumulates the sections and keys of a DROPFILE.INI.
type dropfileIniWriter struct {
	b strings.Builder
}

// line writes one CRLF-terminated line.
func (w *dropfileIniWriter) line(s string) {
	w.b.WriteString(s + "\r\n")
}

// section starts a new section, separated from the previous one by a blank line.
func (w *dropfileIniWriter) section(name string) {
	w.line("")
	w.line("[" + name + "]")
}

// text writes an optional text key, leaving it out when the value is empty.
func (w *dropfileIniWriter) text(key, val string) {
	if v := dropfileIniText(key, val); v != "" {
		w.line(key + "=" + v)
	}
}

// requiredText writes a required text key, substituting "?" for an empty value.
func (w *dropfileIniWriter) requiredText(key, val string) {
	v := dropfileIniText(key, val)
	if v == "" {
		v = "?"
	}
	w.line(key + "=" + v)
}

// ascii writes an optional ascii or token key, leaving it out when the value
// is empty or not valid ASCII.
func (w *dropfileIniWriter) ascii(key, val string) {
	if v := dropfileIniASCII(key, val); v != "" {
		w.line(key + "=" + v)
	}
}

// path writes an optional path key, leaving it out when the value can't be
// written unchanged.
func (w *dropfileIniWriter) path(key, val string) {
	if v := dropfileIniPath(key, val); v != "" {
		w.line(key + "=" + v)
	}
}

// num writes an int key; a negative value is written as 0.
func (w *dropfileIniWriter) num(key string, val int) {
	if val < 0 {
		val = 0
	}
	w.line(key + "=" + strconv.Itoa(val))
}

// doorUsesSocketIO reports whether a native door is handed the caller's
// connection as an inherited socket rather than over standard I/O. It mirrors
// the I/O mode selection in executeNativeDoor, where a PTY takes precedence.
func doorUsesSocketIO(ctx *DoorCtx) bool {
	if !strings.EqualFold(ctx.Config.IOMode, "SOCKET") {
		return false
	}
	if ctx.Config.RequiresRawTerminal && ctx.Session != nil {
		if _, _, isPty := ctx.Session.Pty(); isPty {
			return false
		}
	}
	return true
}

// generateDropfileIni writes a DROPFILE.INI file. tempDir is a directory only
// this node uses, in the path syntax the door sees, or "" when there is none.
func generateDropfileIni(ctx *DoorCtx, dir, filename, tempDir string) error {
	path := filepath.Join(dir, filename)
	slog.Info("generating dropfile", "type", dropfileIniType, "filename", filename, "path", path)
	cfg := ctx.Executor.GetServerConfig()
	isDOS := ctx.Config.IsDOS
	useFossil := isDOS && ctx.Config.FossilDriver != ""

	// The BBS relays door bytes untranslated, so the door must speak the
	// caller's own character set. The exception is a DOS door in terminal
	// mode, whose CP437 screen dosemu translates for the terminal.
	termCharset := "UTF-8"
	if ctx.OutputMode == ansi.OutputModeCP437 {
		termCharset = "CP437"
	}
	commCharset := termCharset
	if isDOS && !useFossil {
		commCharset = "CP437"
	}

	var w dropfileIniWriter
	software := "ViSiON/3 " + strings.TrimSpace(version.Number)
	w.b.WriteString("; Written by " + software + "\r\n")
	w.line("[file]")
	w.line("FILE_TIME=" + time.Now().Format("2006-01-02T15:04:05-07:00"))

	w.section("system")
	w.requiredText("SYS_SOFTWARE", software)
	w.line("SYS_VENDOR=" + dropfileIniVendor)
	w.ascii("SYS_VERSION", version.Number)
	w.requiredText("SYS_NAME", cfg.BoardName)
	w.requiredText("SYS_OP", cfg.SysOpName)
	w.num("SYS_NODE_NUM", max(ctx.NodeNumber, 1))
	if cfg.MaxNodes > 0 {
		w.num("SYS_NODE_COUNT", cfg.MaxNodes)
	}
	if id := strings.ToUpper(strings.TrimSpace(cfg.QWKID)); qwkIDPattern.MatchString(id) {
		w.line("SYS_QWKID=" + id)
	}
	w.text("SYS_LOCATION", cfg.BBSLocation)

	w.section("comm")
	switch {
	case useFossil:
		w.line("COMM_TYPE=fossil")
		w.line("COMM_PORT=1")
	case !isDOS && doorUsesSocketIO(ctx):
		w.line("COMM_TYPE=socket")
		w.num("COMM_HANDLE", doorSocketFD)
	default:
		w.line("COMM_TYPE=stdio")
	}
	w.line("COMM_CHARSET=" + commCharset)

	w.section("user")
	w.requiredText("USER_ALIAS", ctx.User.Handle)
	w.num("USER_NUMBER", ctx.User.ID)
	role := "user"
	switch {
	case cfg.SysOpLevel > 0 && ctx.User.AccessLevel >= cfg.SysOpLevel:
		role = "sysop"
	case cfg.CoSysOpLevel > 0 && ctx.User.AccessLevel >= cfg.CoSysOpLevel:
		role = "cosysop"
	}
	w.line("USER_ROLE=" + role)
	// The sysop may keep personal details from a door, which may be closed
	// source or send what it reads to other systems.
	personal := !ctx.Config.DropfileHidePersonal
	if personal {
		w.text("USER_REALNAME", ctx.User.RealName)
		w.text("USER_LOCATION", ctx.User.GroupLocation)
	}
	if ctx.Session != nil {
		if personal {
			w.ascii("USER_IP", doorUserIP(ctx.Session))
		}
		if isTelnetSession(ctx.Session) {
			w.line("USER_PROTOCOL=telnet")
		} else {
			w.line("USER_PROTOCOL=ssh")
		}
	}

	// A DOS door runs on a fixed 80x25 dosemu screen whatever the caller's
	// terminal size; other doors get the user's saved dimensions.
	cols, rows := 80, 25
	if !isDOS {
		if ctx.User.ScreenWidth > 0 {
			cols = ctx.User.ScreenWidth
		}
		if ctx.User.ScreenHeight > 0 {
			rows = ctx.User.ScreenHeight
		}
	}
	w.section("terminal")
	w.num("TERM_COLS", cols)
	w.num("TERM_ROWS", rows)
	w.line("TERM_TYPE=ansi")
	w.line("TERM_CHARSET=" + termCharset)
	if ctx.Session != nil {
		// The terminal type the client sent: Telnet TERMINAL-TYPE or the
		// SSH pty request, passed on as received.
		if pty, _, ok := ctx.Session.Pty(); ok {
			w.ascii("TERM_TERMINFO", pty.Term)
		}
	}

	w.section("session")
	w.num("TIME_LEFT", ctx.TimeLeftMin*60)
	w.path("TEMP_DIR", tempDir)
	w.line("LOCAL_DISPLAY=0")
	// The idle timeout executeDoor enforces; left out, meaning no limit, for
	// a caller exempt from it.
	if ctx.IdleTimeout > 0 {
		w.num("IDLE_LIMIT", int(ctx.IdleTimeout/time.Second))
	}

	w.section("door")
	w.ascii("DOOR_CODE", ctx.Config.Code)
	w.text("DOOR_NAME", ctx.Config.Name)

	w.section("x-" + strings.ToLower(dropfileIniVendor))
	w.num("X_"+dropfileIniVendor+"_LEVEL", ctx.User.AccessLevel)

	return os.WriteFile(path, []byte(w.b.String()), 0600)
}
