package scripting

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestRunOutcomes pins how Engine.Run maps script endings to errors: a clean
// finish is success, JS errors are wrapped with their message.
func TestRunOutcomes(t *testing.T) {
	tests := []struct {
		name    string
		src     string
		wantErr string // "" = nil error
		wantOut string
	}{
		{name: "clean finish", src: `v3.console.write("done")`, wantOut: "done"},
		{name: "thrown error", src: `throw new Error("boom")`, wantErr: "script error: Error: boom"},
		{name: "syntax error", src: `function (`, wantErr: "script error:"},
		{name: "reference error", src: `nosuchthing()`, wantErr: "nosuchthing is not defined"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t, harnessOpts{})
			err := h.run(tt.src)
			switch {
			case tt.wantErr == "" && err != nil:
				t.Fatalf("Run = %v, want nil", err)
			case tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)):
				t.Fatalf("Run = %v, want error containing %q", err, tt.wantErr)
			}
			if got := h.output(); got != tt.wantOut {
				t.Errorf("output = %q, want %q", got, tt.wantOut)
			}
		})
	}
}

// TestRunMissingScript returns a read error naming the absolute script path.
func TestRunMissingScript(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	err := h.eng.Run("absent.js")
	if err == nil || !strings.Contains(err.Error(), filepath.Join(h.scriptsDir, "absent.js")) {
		t.Fatalf("Run(absent) = %v, want read error for the joined path", err)
	}
	// An absolute path is used as-is rather than joined to the working dir.
	abs := filepath.Join(h.root, "elsewhere.js")
	h.writeFile("elsewhere.js", `v3.console.write("abs")`)
	if err := h.eng.Run(abs); err != nil {
		t.Fatalf("Run(abs) = %v", err)
	}
	if h.output() != "abs" {
		t.Errorf("output = %q, want %q", h.output(), "abs")
	}
}

// TestRunTimeoutInterruptsBusyLoop proves MaxRunTime halts a CPU-bound script
// and surfaces ErrTimeout.
func TestRunTimeoutInterruptsBusyLoop(t *testing.T) {
	h := newHarness(t, harnessOpts{maxRunTime: 50 * time.Millisecond})
	start := time.Now()
	err := h.run(`while (true) {}`)
	if !errors.Is(err, ErrTimeout) {
		t.Fatalf("Run = %v, want ErrTimeout", err)
	}
	if el := time.Since(start); el > 5*time.Second {
		t.Errorf("timeout took %v", el)
	}
}

// TestRunParentCancelIsDisconnect maps cancellation of the session context
// (the caller hanging up) to ErrDisconnect, not ErrTimeout.
func TestRunParentCancelIsDisconnect(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	h := newHarness(t, harnessOpts{ctx: ctx})
	go func() {
		time.Sleep(30 * time.Millisecond)
		cancel()
	}()
	if err := h.run(`while (true) {}`); !errors.Is(err, ErrDisconnect) {
		t.Fatalf("Run = %v, want ErrDisconnect", err)
	}
}

// TestSleepAbortsOnCancel makes v3.util.sleep return early (as a JS error)
// when the run-time limit expires mid-sleep.
func TestSleepAbortsOnCancel(t *testing.T) {
	h := newHarness(t, harnessOpts{maxRunTime: 40 * time.Millisecond})
	start := time.Now()
	err := h.run(`v3.util.sleep(60000)`)
	if err == nil {
		t.Fatal("Run returned nil; want an error from the aborted sleep")
	}
	if el := time.Since(start); el > 5*time.Second {
		t.Fatalf("sleep not interrupted: took %v", el)
	}
}

// TestRegisteredNamespaces verifies optional namespaces only appear when
// their provider is configured.
func TestRegisteredNamespaces(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	for _, ns := range []string{"console", "session", "data", "ansi", "util", "fs", "args"} {
		if h.eval("typeof v3."+ns).String() != "object" {
			t.Errorf("v3.%s missing", ns)
		}
	}
	for _, ns := range []string{"user", "users", "message", "file", "nodes"} {
		if got := h.eval("typeof v3." + ns).String(); got != "undefined" {
			t.Errorf("v3.%s = %s without a provider, want undefined", ns, got)
		}
	}
}

// TestWriteEncodesCP437 checks Unicode output is transcoded to CP437 bytes.
func TestWriteEncodesCP437(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	h.mustRun(`v3.console.write("½░")`)
	if got := h.output(); got != "\xab\xb0" {
		t.Errorf("output = %q, want CP437 \\xab\\xb0", got)
	}
	// Characters with no CP437 mapping fall back to raw UTF-8.
	h.resetOutput()
	h.mustRun(`v3.console.write("€")`)
	if got := h.output(); got != "€" {
		t.Errorf("unmappable output = %q, want UTF-8 passthrough", got)
	}
}

// failWriter is a session whose writes always fail.
type failWriter struct{ interruptibleSession }

func (f *failWriter) Write(p []byte) (int, error) { return 0, errors.New("broken pipe") }

// TestWriteFailureCancelsEngine: a failed session write means the caller is
// gone, so the engine context must be cancelled.
func TestWriteFailureCancelsEngine(t *testing.T) {
	sess := &failWriter{interruptibleSession: *newInterruptibleSession("")}
	eng := NewEngine(context.Background(), &SessionContext{Session: sess}, ScriptConfig{}, nil)
	defer eng.Close()
	eng.writeRaw("x")
	select {
	case <-eng.ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("writeRaw failure did not cancel the engine")
	}

	eng2 := NewEngine(context.Background(), &SessionContext{Session: sess}, ScriptConfig{}, nil)
	defer eng2.Close()
	eng2.writeBytes([]byte("x"))
	eng2.writeBytes(nil) // no-op
	select {
	case <-eng2.ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("writeBytes failure did not cancel the engine")
	}
}

// TestReadKeyDisconnect: once the session read fails, readKey and readLine
// report an error (disconnect or terminated, whichever wins the race with the
// cancelled context), the engine is cancelled and the session reads offline.
func TestReadKeyDisconnect(t *testing.T) {
	h := newHarness(t, harnessOpts{input: "ab\r"})
	isGone := func(err error) bool { return errors.Is(err, ErrDisconnect) || errors.Is(err, ErrTerminated) }
	if got, err := h.eng.readLine(10, lineOpts{}); got != "ab" || err != nil {
		t.Fatalf("readLine = %q, %v; want %q", got, err, "ab")
	}
	h.disconnect()
	if _, err := h.eng.readKey(2 * time.Second); !isGone(err) {
		t.Fatalf("readKey after hangup = %v, want ErrDisconnect/ErrTerminated", err)
	}
	select {
	case <-h.eng.ctx.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("engine context not cancelled after disconnect")
	}
	if _, err := h.eng.readLine(10, lineOpts{}); !isGone(err) {
		t.Errorf("readLine after hangup = %v, want ErrDisconnect/ErrTerminated", err)
	}
	if h.eng.vm.Get("v3").ToObject(h.eng.vm).Get("session").ToObject(h.eng.vm).Get("online").ToBoolean() {
		t.Error("session.online = true after disconnect")
	}
}

// TestParseInput covers key decoding: CSI sequences and ESC pairs are
// swallowed, CR/LF variants become "\r", and leftovers are buffered.
func TestParseInput(t *testing.T) {
	tests := []struct {
		in       string
		want     string
		leftover string
	}{
		{"", "", ""},
		{"a", "a", ""},
		{"abc", "a", "bc"},
		{"\x1b[A", "", ""},
		{"\x1b[1;5Cx", "", "x"},
		{"\x1b[12", "", ""}, // unterminated CSI consumes the rest
		{"\x1bq", "", ""},
		{"\x1b", "\x1b", ""},
		{"\r\nz", "\r", "z"},
		{"\r", "\r", ""},
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

// TestToUpperASCII only folds ASCII letters.
func TestToUpperASCII(t *testing.T) {
	if got := toUpperASCII("abZ9-é"); got != "ABZ9-é" {
		t.Errorf("toUpperASCII = %q", got)
	}
}
