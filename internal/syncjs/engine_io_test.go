package syncjs

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestRunErrors pins how Engine.Run reports failures: missing files, thrown
// errors and syntax errors are wrapped; exit() stops the script cleanly.
func TestRunErrors(t *testing.T) {
	h := newDoor(t, doorOpts{})
	if err := h.eng.Run("absent.js"); err == nil || !strings.Contains(err.Error(), filepath.Join(h.game, "absent.js")) {
		t.Errorf("Run(absent) = %v, want read error naming the joined path", err)
	}
	if err := h.run(`throw new Error("kaboom")`); err == nil || !strings.Contains(err.Error(), "script error: Error: kaboom") {
		t.Errorf("thrown error = %v", err)
	}
	if err := h.run(`function (`); err == nil || !strings.Contains(err.Error(), "script error") {
		t.Errorf("syntax error = %v", err)
	}

	h2 := newDoor(t, doorOpts{})
	if err := h2.run(`console.write("a"); exit(0); console.write("b")`); err != nil {
		t.Errorf("exit() = %v, want nil", err)
	}
	if h2.output() != "a" {
		t.Errorf("output after exit = %q, want %q", h2.output(), "a")
	}
	if !h2.evalCancelled(`js.terminated`).ToBoolean() {
		t.Error("exit() did not cancel the engine")
	}
}

// TestRunAbsolutePathAndUseStrict: absolute script paths are used as-is and
// 'use strict' is stripped so SpiderMonkey-era sloppy code still runs.
func TestRunAbsolutePathAndUseStrict(t *testing.T) {
	h := newDoor(t, doorOpts{})
	p := h.write("elsewhere/door.js", `'use strict'; undeclared = 5; console.write(js.exec_dir + "|" + undeclared);`)
	if err := h.eng.Run(p); err != nil {
		t.Fatalf("Run(abs) = %v", err)
	}
	want := filepath.Dir(p) + string(filepath.Separator) + "|5"
	if h.output() != want {
		t.Errorf("output = %q, want %q", h.output(), want)
	}
}

// TestRunCancelledContextStops: cancelling the session context halts a
// CPU-bound door (reported as a clean stop, not a script error).
func TestRunCancelledContextStops(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	h := newDoor(t, doorOpts{ctx: ctx})
	go func() {
		time.Sleep(30 * time.Millisecond)
		cancel()
	}()
	done := make(chan error, 1)
	go func() { done <- h.run(`while (true) {}`) }()
	select {
	case err := <-done:
		if err != nil && !errors.Is(err, ErrDisconnect) {
			t.Errorf("Run after cancel = %v, want nil or ErrDisconnect", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("busy loop not interrupted by context cancel")
	}
	if !h.evalCancelled(`js.terminated`).ToBoolean() {
		t.Error("js.terminated = false after cancel")
	}
}

// TestOnExitHandlersRunOnClose: js.on_exit functions and code strings run
// at Close, each group in reverse registration order, and a failing handler
// does not stop the rest.
func TestOnExitHandlersRunOnClose(t *testing.T) {
	h := newDoor(t, doorOpts{})
	h.mustRun(`
		js.on_exit(function(){ console.write("f1;") });
		js.on_exit(function(){ throw new Error("bad handler") });
		js.on_exit(function(){ console.write("f2;") });
		js.on_exit("console.write('s1;')");
		js.on_exit("this is not valid js");
		js.on_exit("console.write('s2;')");
		js.on_exit();
		console.write("body;");
	`)
	h.close()
	if got, want := h.output(), "body;f2;f1;s2;s1;"; got != want {
		t.Errorf("output = %q, want %q", got, want)
	}
}

// TestFileMutexCleanedOnClose: lock files made by file_mutex are removed at
// Close; a second mutex on the same path fails while held.
func TestFileMutexCleanedOnClose(t *testing.T) {
	h := newDoor(t, doorOpts{})
	got := h.eval(`[file_mutex("game.lck", "node 2"), file_mutex("game.lck"), file_mutex()].join()`).String()
	if got != "true,false,false" {
		t.Fatalf("file_mutex results = %q", got)
	}
	lock := filepath.Join(h.game, "game.lck")
	if b := readFile(t, lock); b != "node 2" {
		t.Errorf("lock content = %q", b)
	}
	h.close()
	if fileExists(lock) {
		t.Error("lock file survived Close")
	}
}

// TestParseInputKeys maps terminal escape sequences to Synchronet key codes.
func TestParseInputKeys(t *testing.T) {
	tests := []struct {
		in, want, leftover string
	}{
		{"", "", ""},
		{"a", "a", ""},
		{"ab", "a", "b"},
		{"\x1b[A", "\x01\x48", ""},
		{"\x1b[B", "\x01\x50", ""},
		{"\x1b[C", "\x01\x4d", ""},
		{"\x1b[Dz", "\x01\x4b", "z"},
		{"\x1b[H", "\x01\x47", ""},
		{"\x1b[F", "\x01\x4f", ""},
		{"\x1b[V", "\x01\x49", ""},
		{"\x1b[U", "\x01\x51", ""},
		{"\x1b[1~", "\x01\x47", ""},
		{"\x1b[2~", "\x01\x52", ""},
		{"\x1b[3~", "\x01\x53", ""},
		{"\x1b[4~", "\x01\x4f", ""},
		{"\x1b[5~", "\x01\x49", ""},
		{"\x1b[6~", "\x01\x51", ""},
		{"\x1b[9~q", "", "q"},
		{"\x1b[1;5Cq", "", "q"},
		{"\x1b[12", "", ""},
		{"\x1bOx", "", "x"},
		{"\x1b", "\x1b", ""},
		{"\r\nk", "\r", "k"},
		{"\n", "\r", ""},
	}
	for _, tt := range tests {
		eng := &Engine{}
		got := eng.parseInput([]byte(tt.in))
		if got != tt.want || string(eng.inputBuf) != tt.leftover {
			t.Errorf("parseInput(%q) = %q, leftover %q; want %q, %q", tt.in, got, eng.inputBuf, tt.want, tt.leftover)
		}
	}
}

// TestReadKeyDorkit formats raw input the way DORKit's ansi_input.js does.
func TestReadKeyDorkit(t *testing.T) {
	tests := []struct{ in, want string }{
		{"a", "a"},
		{"\x1b[A", "KEY_UP\x00\x1b[A"},
		{"\x1bOB", "KEY_DOWN\x00\x1bOB"},
		{"\x1bC", "KEY_RIGHT\x00\x1bC"},
		{"\x1b[D", "KEY_LEFT\x00\x1b[D"},
		{"\x1b[1~", "KEY_HOME\x00\x1b[1~"},
		{"\x1b[K", "KEY_END\x00\x1b[K"},
		{"\x1b[5~", "KEY_PGUP\x00\x1b[5~"},
		{"\x1b[U", "KEY_PGDOWN\x00\x1b[U"},
		{"\x1b[2~", "KEY_INS\x00\x1b[2~"},
		{"\x1b[3~", "KEY_DEL\x00\x1b[3~"},
		{"\x1b[24;80R", "POSITION_REPORT\x00\x1b[24;80R"},
		{"\x1b[99z", "UNKNOWN_ANSI\x00\x1b[99z"},
		{"\x1bQ", "UNKNOWN_ANSI\x00\x1bQ"},
		{"\x1b", "\x1b"},
	}
	for _, tt := range tests {
		h := newDoor(t, doorOpts{input: tt.in})
		got, err := h.eng.readKeyDorkit(time.Second)
		if err != nil || got != tt.want {
			t.Errorf("readKeyDorkit(%q) = %q, %v; want %q", tt.in, got, err, tt.want)
		}
	}

	// Timeout with no input: empty, no error.
	h := newDoor(t, doorOpts{})
	if got, err := h.eng.readKeyDorkit(10 * time.Millisecond); got != "" || err != nil {
		t.Errorf("idle readKeyDorkit = %q, %v", got, err)
	}
	// After the engine is cancelled it reports termination.
	h.eng.cancel()
	if _, err := h.eng.readKeyDorkit(10 * time.Millisecond); !errors.Is(err, ErrTerminated) {
		t.Errorf("cancelled readKeyDorkit err = %v, want ErrTerminated", err)
	}
}

// TestReadRawByteDisconnect: a dropped session makes raw reads fail and
// cancels the engine.
func TestReadRawByteDisconnect(t *testing.T) {
	h := newDoor(t, doorOpts{input: "xy"})
	if b, ok := h.eng.readRawByte(time.Second); !ok || b != 'x' {
		t.Fatalf("readRawByte = %q, %v", b, ok)
	}
	if b, ok := h.eng.readRawByte(time.Second); !ok || b != 'y' {
		t.Fatalf("buffered readRawByte = %q, %v", b, ok)
	}
	h.disconnect()
	if _, ok := h.eng.readRawByte(2 * time.Second); ok {
		t.Error("readRawByte succeeded after disconnect")
	}
	select {
	case <-h.eng.ctx.Done():
	case <-time.After(2 * time.Second):
		t.Error("engine not cancelled after disconnect")
	}
}

// TestReadKeyDisconnect: readKey and readLine report the hangup.
func TestReadKeyDisconnect(t *testing.T) {
	h := newDoor(t, doorOpts{input: "ab\r"})
	if got, err := h.eng.readLine(10, 0); got != "ab" || err != nil {
		t.Fatalf("readLine = %q, %v; want %q", got, err, "ab")
	}
	h.disconnect()
	if _, err := h.eng.readKey(2 * time.Second); !errors.Is(err, ErrDisconnect) && !errors.Is(err, ErrTerminated) {
		t.Errorf("readKey after hangup = %v", err)
	}
	if h.evalCancelled(`String(bbs.online)`).String() != "false" {
		t.Error("bbs.online still true after hangup")
	}
}

// TestWriteRawFiltering: soft-reset and DSR queries are stripped from
// output, DSR arms a synthetic position reply, and Latin-1 runes are sent
// as single CP437 bytes.
func TestWriteRawFiltering(t *testing.T) {
	h := newDoor(t, doorOpts{})
	h.mustRun(`console.write("a\x1b[!b\x1b[6nc\x1b[0md")`)
	if got := h.output(); got != "abc\x1b[0md" {
		t.Errorf("filtered output = %q", got)
	}
	if !h.eng.pendingDSR {
		t.Error("DSR query did not arm pendingDSR")
	}

	h.resetOutput()
	h.mustRun(`console.write("°Û", "☃😀", "")`)
	if got := h.output(); got != "\xb0\xdb☃😀" {
		t.Errorf("latin1/unicode output = %q", got)
	}
}

// TestEncodeRune checks the UTF-8 encoder for every length class.
func TestEncodeRune(t *testing.T) {
	for _, r := range []rune{'A', 'é', '☃', '😀'} {
		var buf [4]byte
		n := encodeRune(buf[:], r)
		if string(buf[:n]) != string(r) {
			t.Errorf("encodeRune(%U) = %q, want %q", r, buf[:n], string(r))
		}
	}
}

// failWriter is a session whose writes fail, as on a dropped connection.
type failWriter struct{ *interruptibleSession }

func (failWriter) Write([]byte) (int, error) { return 0, errors.New("broken pipe") }

// TestWriteFailureCancels: a failed session write cancels the engine.
func TestWriteFailureCancels(t *testing.T) {
	eng := NewEngine(context.Background(), &SessionContext{Session: failWriter{newInterruptibleSession("")}}, SyncJSDoorConfig{})
	defer eng.Close()
	eng.writeRaw("x")
	select {
	case <-eng.ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("write failure did not cancel the engine")
	}
}

// TestExecDirStack: pop never removes the base entry; an empty stack falls
// back to the working dir.
func TestExecDirStack(t *testing.T) {
	eng := &Engine{cfg: SyncJSDoorConfig{WorkingDir: "/w"}, execDirStack: []string{"/base/"}}
	eng.pushExecDir("/mod/")
	if eng.currentExecDir() != "/mod/" {
		t.Errorf("after push = %q", eng.currentExecDir())
	}
	eng.popExecDir()
	eng.popExecDir()
	if eng.currentExecDir() != "/base/" {
		t.Errorf("after pops = %q, want base kept", eng.currentExecDir())
	}
	eng.execDirStack = nil
	if eng.currentExecDir() != "/w/" {
		t.Errorf("empty stack = %q, want working dir", eng.currentExecDir())
	}
}
