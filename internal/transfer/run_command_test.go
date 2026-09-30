package transfer

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gliderlabs/ssh"
	"golang.org/x/term"
)

// --- helper process ---

const (
	helperEnv     = "VISION3_TRANSFER_HELPER"
	helperRunFlag = "-test.run=^TestTransferHelperProcess$"
	helperTimeout = 10 * time.Second
	// Under -race the helper would otherwise sleep a second on a clean exit.
	helperGORACE = "GORACE=atexit_sleep_ms=0"
)

// TestTransferHelperProcess is not a test: it is the body of the fake
// transfer program. The tests re-execute the test binary with helperEnv set to
// one of the modes below, so no external sz/rz/sexyz is needed.
func TestTransferHelperProcess(t *testing.T) {
	mode := os.Getenv(helperEnv)
	if mode == "" {
		return
	}
	// Arguments meant for the helper follow the "--" separator.
	var args []string
	for i, a := range os.Args {
		if a == "--" {
			args = os.Args[i+1:]
			break
		}
	}
	switch mode {
	case "echo":
		// Copy stdin to stdout until the session side closes stdin.
		io.Copy(os.Stdout, os.Stdin)
	case "args":
		// Report the argument vector and working directory.
		cwd, _ := os.Getwd()
		fmt.Fprintf(os.Stdout, "%s|cwd=%s", strings.Join(args, "|"), cwd)
	case "list":
		// Report the file list named by an "@path" argument, sexyz style.
		for _, a := range args {
			if path, ok := strings.CutPrefix(a, "@"); ok {
				data, err := os.ReadFile(path)
				if err != nil {
					os.Exit(5)
				}
				fmt.Fprintf(os.Stdout, "list=%s\n%s", path, data)
			}
		}
	case "fail":
		fmt.Fprintln(os.Stderr, "helper failure on stderr")
		os.Stdout.WriteString("partial")
		os.Exit(3)
	case "block":
		// Never exits by itself: the parent is expected to kill it.
		os.Stdout.WriteString("READY")
		io.Copy(io.Discard, os.Stdin)
		time.Sleep(time.Minute)
		os.Exit(4)
	case "pty":
		// Announce, report the first key received and whether stdin is a
		// terminal, then wait for another key before exiting so the parent
		// has read everything before the PTY is torn down.
		var key [1]byte
		os.Stdout.WriteString("READY")
		os.Stdin.Read(key[:])
		fmt.Fprintf(os.Stdout, "key=%s tty=%v;", key[:], term.IsTerminal(int(os.Stdin.Fd())))
		os.Stdin.Read(key[:])
	default:
		os.Exit(6)
	}
	os.Exit(0)
}

// helperCommand builds a command that runs the helper process in mode.
func helperCommand(t *testing.T, mode string, args ...string) *exec.Cmd {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(exe, append([]string{helperRunFlag, "--"}, args...)...)
	cmd.Env = append(os.Environ(), helperEnv+"="+mode, helperGORACE)
	return cmd
}

// --- fake sessions ---

// fakeSession is a minimal ssh.Session: each chunk sent on in is returned by
// one Read, closing in yields io.EOF, and SetReadInterrupt unblocks a pending
// Read the way the real session adapters do. It deliberately has no RawWrite.
type fakeSession struct {
	ssh.Session
	in chan []byte

	mu       sync.Mutex
	pending  []byte
	intr     <-chan struct{}
	changed  chan struct{} // closed whenever intr is replaced
	intrSets []bool        // one entry per SetReadInterrupt call; true = armed
	out      bytes.Buffer  // bytes passed to Write
	wrote    chan struct{} // signalled after every Write

	hasPty bool
	window ssh.Window // initial window size reported by Pty
	winCh  chan ssh.Window
}

func newFakeSession() *fakeSession {
	return &fakeSession{
		in:      make(chan []byte, 16),
		changed: make(chan struct{}),
		wrote:   make(chan struct{}, 1),
	}
}

func (s *fakeSession) Read(p []byte) (int, error) {
	for {
		s.mu.Lock()
		if len(s.pending) > 0 {
			n := copy(p, s.pending)
			s.pending = s.pending[n:]
			s.mu.Unlock()
			return n, nil
		}
		intr, changed := s.intr, s.changed
		s.mu.Unlock()

		select {
		case b, ok := <-s.in:
			if !ok {
				return 0, io.EOF
			}
			s.mu.Lock()
			s.pending = b
			s.mu.Unlock()
		case <-intr:
			return 0, io.EOF
		case <-changed:
			// Interrupt channel was swapped; pick up the new one.
		}
	}
}

func (s *fakeSession) Write(p []byte) (int, error) {
	s.mu.Lock()
	s.out.Write(p)
	s.mu.Unlock()
	select {
	case s.wrote <- struct{}{}:
	default:
	}
	return len(p), nil
}

func (s *fakeSession) SetReadInterrupt(ch <-chan struct{}) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.intr = ch
	s.intrSets = append(s.intrSets, ch != nil)
	close(s.changed)
	s.changed = make(chan struct{})
}

func (s *fakeSession) Pty() (ssh.Pty, <-chan ssh.Window, bool) {
	if !s.hasPty {
		return ssh.Pty{}, nil, false
	}
	return ssh.Pty{Term: "ansi", Window: s.window}, s.winCh, true
}

func (s *fakeSession) written() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.out.String()
}

// interruptCleared reports whether the last SetReadInterrupt call disarmed the
// interrupt, which is what lets the next menu read block normally.
func (s *fakeSession) interruptCleared() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.intrSets) > 0 && !s.intrSets[len(s.intrSets)-1]
}

// rawSession adds the binary-safe write path and the transfer lock that
// sshserver.BBSSession provides.
type rawSession struct {
	*fakeSession
	raw      bytes.Buffer // bytes passed to RawWrite
	rawWrote chan struct{}
	active   []bool // SetTransferActive calls, in order
}

func newRawSession() *rawSession {
	return &rawSession{fakeSession: newFakeSession(), rawWrote: make(chan struct{}, 1)}
}

func (s *rawSession) RawWrite(p []byte) (int, error) {
	s.mu.Lock()
	s.raw.Write(p)
	s.mu.Unlock()
	select {
	case s.rawWrote <- struct{}{}:
	default:
	}
	return len(p), nil
}

func (s *rawSession) SetTransferActive(active bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.active = append(s.active, active)
}

func (s *rawSession) rawWritten() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.raw.String()
}

// waitForRaw blocks until the bytes passed to RawWrite contain want.
func (s *rawSession) waitForRaw(t *testing.T, want string) {
	t.Helper()
	deadline := time.After(helperTimeout)
	for !strings.Contains(s.rawWritten(), want) {
		select {
		case <-s.rawWrote:
		case <-deadline:
			t.Fatalf("timed out waiting for %q in raw output %q", want, s.rawWritten())
		}
	}
}

// nilCtx is a nil context: every entry point documents that it accepts one and
// treats it as context.Background().
var nilCtx context.Context

// zmodemAbortSeq is what RunCommandDirect sends when a transfer ends abnormally.
var zmodemAbortSeq = string(bytes.Repeat([]byte{0x18}, 8)) + "\r\n"

// runDirect runs RunCommandDirect and fails the test if it does not return.
func runDirect(t *testing.T, ctx context.Context, s ssh.Session, cmd *exec.Cmd, idle time.Duration) error {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- RunCommandDirect(ctx, s, cmd, idle) }()
	select {
	case err := <-done:
		return err
	case <-time.After(3 * helperTimeout):
		t.Fatal("RunCommandDirect did not return")
		return nil
	}
}

// --- RunCommandDirect ---

func TestRunCommandDirect_roundTripUsesRawWrite(t *testing.T) {
	s := newRawSession()
	// 0x0A is the ZDATA frame type: it must reach the client unexpanded.
	payload := []byte("ZDATA\x0a\x0d\x00\xff binary \x18\x18\x18\x18 four CANs are not an abort")
	s.in <- payload[:10]
	s.in <- payload[10:]
	close(s.in)

	// A nil context must behave like context.Background(); the idle timeout
	// is far longer than the test, so only its reset path runs.
	err := runDirect(t, nilCtx, s, helperCommand(t, "echo"), time.Minute)
	if err != nil {
		t.Fatalf("RunCommandDirect: %v", err)
	}
	if got := s.rawWritten(); got != string(payload) {
		t.Errorf("raw output = %q, want %q", got, payload)
	}
	if got := s.written(); got != "" {
		t.Errorf("clean transfer wrote %q through Write, want nothing", got)
	}
	if len(s.active) != 2 || !s.active[0] || s.active[1] {
		t.Errorf("SetTransferActive calls = %v, want [true false]", s.active)
	}
	if !s.interruptCleared() {
		t.Error("read interrupt left armed after transfer")
	}
}

func TestRunCommandDirect_fallsBackToWrite(t *testing.T) {
	// A session without RawWrite still gets the command's output.
	s := newFakeSession()
	s.in <- []byte("plain session")
	close(s.in)

	err := runDirect(t, context.Background(), s, helperCommand(t, "echo"), 0)
	if err != nil {
		t.Fatalf("RunCommandDirect: %v", err)
	}
	if got := s.written(); got != "plain session" {
		t.Errorf("output = %q, want %q", got, "plain session")
	}
}

func TestRunCommandDirect_failureSendsAbort(t *testing.T) {
	s := newRawSession()

	err := runDirect(t, context.Background(), s, helperCommand(t, "fail"), 0)
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 3 {
		t.Fatalf("err = %v, want exit status 3", err)
	}
	// stderr must never be merged into the binary stream.
	if got := s.rawWritten(); got != "partial" {
		t.Errorf("raw output = %q, want %q", got, "partial")
	}
	if got := s.written(); got != zmodemAbortSeq {
		t.Errorf("abort sequence = %q, want %q", got, zmodemAbortSeq)
	}
	if !s.interruptCleared() {
		t.Error("read interrupt left armed after failed transfer")
	}
}

func TestRunCommandDirect_startError(t *testing.T) {
	s := newRawSession()
	cmd := exec.Command(filepath.Join(t.TempDir(), "no-such-binary"))

	err := runDirect(t, context.Background(), s, cmd, 0)
	if err == nil || !strings.Contains(err.Error(), "failed to start command") {
		t.Fatalf("err = %v, want start failure", err)
	}
	if got := s.written() + s.rawWritten(); got != "" {
		t.Errorf("session output = %q, want nothing", got)
	}
	// The transfer lock is released even though nothing ran.
	if len(s.active) != 2 || s.active[1] {
		t.Errorf("SetTransferActive calls = %v, want [true false]", s.active)
	}
}

func TestRunCommandDirect_canAbortKillsProcess(t *testing.T) {
	s := newRawSession()
	cmd := helperCommand(t, "block")
	done := make(chan error, 1)
	go func() { done <- RunCommandDirect(context.Background(), s, cmd, 0) }()

	s.waitForRaw(t, "READY")
	// Five consecutive CANs split across two reads still count as one run.
	s.in <- []byte{0x18, 0x18, 0x18}
	s.in <- []byte{0x18, 0x18}

	select {
	case err := <-done:
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) || exitErr.ExitCode() == 4 {
			t.Fatalf("err = %v, want killed process", err)
		}
	case <-time.After(helperTimeout):
		t.Fatal("CAN abort did not end the transfer")
	}
	if got := s.written(); got != zmodemAbortSeq {
		t.Errorf("abort sequence = %q, want %q", got, zmodemAbortSeq)
	}
}

func TestRunCommandDirect_idleTimeoutKillsProcess(t *testing.T) {
	// The client never sends a byte, as when an upload dialog is cancelled.
	s := newRawSession()

	err := runDirect(t, context.Background(), s, helperCommand(t, "block"), 50*time.Millisecond)
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() == 4 {
		t.Fatalf("err = %v, want killed process", err)
	}
	if got := s.written(); got != zmodemAbortSeq {
		t.Errorf("abort sequence = %q, want %q", got, zmodemAbortSeq)
	}
}

func TestRunCommandDirect_contextCancel(t *testing.T) {
	s := newRawSession()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cmd := helperCommand(t, "block")
	done := make(chan error, 1)
	go func() { done <- RunCommandDirect(ctx, s, cmd, time.Minute) }()

	s.waitForRaw(t, "READY")
	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v, want context.Canceled", err)
		}
	case <-time.After(helperTimeout):
		t.Fatal("cancellation did not end the transfer")
	}
	if got := s.written(); got != zmodemAbortSeq {
		t.Errorf("abort sequence = %q, want %q", got, zmodemAbortSeq)
	}
	if cmd.ProcessState == nil || cmd.ProcessState.ExitCode() == 4 {
		t.Errorf("process state = %v, want killed process", cmd.ProcessState)
	}
}

// captureDebugLog routes slog to a buffer for the duration of the test. The
// ZRPOS backoff counter is internal to RunCommandDirect, so its debug log is
// the only place a detection can be observed.
func captureDebugLog(t *testing.T) func() string {
	t.Helper()
	var mu sync.Mutex
	var buf bytes.Buffer
	w := writeFunc(func(p []byte) (int, error) {
		mu.Lock()
		defer mu.Unlock()
		return buf.Write(p)
	})
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(w, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return func() string {
		mu.Lock()
		defer mu.Unlock()
		return buf.String()
	}
}

func TestRunCommandDirect_detectsZRPOS(t *testing.T) {
	const (
		hexZRPOS = "**\x18B09"  // ZPAD ZPAD ZDLE ZHEX, frame type 09
		binZRPOS = "*\x18A\x09" // ZPAD ZDLE ZBIN, frame type 09
		// Padding keeps a header out of the tail carried into the next read.
		pad = "0000000000"
	)
	logs := captureDebugLog(t)
	s := newRawSession()
	chunks := []string{
		"data" + hexZRPOS + pad,     // hex header inside one read
		"data" + binZRPOS + pad,     // binary header inside one read
		pad + "**\x18B", "09" + pad, // hex header split across two reads
		pad + "*\x18A", "\x09" + pad, // binary header split across two reads
		pad + "**\x18B01" + pad, // ZRINIT (frame type 01) is not a ZRPOS
	}
	for _, c := range chunks {
		s.in <- []byte(c)
	}
	close(s.in)

	err := runDirect(t, context.Background(), s, helperCommand(t, "echo"), 0)
	if err != nil {
		t.Fatalf("RunCommandDirect: %v", err)
	}
	// Detection only observes the stream; every byte still reaches the command.
	if got, want := s.rawWritten(), strings.Join(chunks, ""); got != want {
		t.Errorf("echoed stream = %q, want %q", got, want)
	}
	out := logs()
	for _, msg := range []string{
		"ZRPOS hex header detected in stdin",
		"ZRPOS binary header detected in stdin",
		"ZRPOS hex header detected across boundary",
		"ZRPOS binary header detected across boundary",
	} {
		if n := strings.Count(out, msg); n != 1 {
			t.Errorf("%q logged %d times, want 1", msg, n)
		}
	}
	if !strings.Contains(out, "backoff=4") || strings.Contains(out, "backoff=5") {
		t.Errorf("want exactly 4 backoff signals, log:\n%s", out)
	}
}

// A header at the very end of a read also lands in the tail carried into
// the next read; it must still produce only one backoff signal.
func TestRunCommandDirect_ZRPOSAtEndOfReadCountedOnce(t *testing.T) {
	for name, header := range map[string]string{
		"hex":    "**\x18B09",
		"binary": "*\x18A\x09",
	} {
		t.Run(name, func(t *testing.T) {
			logs := captureDebugLog(t)
			s := newRawSession()
			s.in <- []byte("data" + header)
			s.in <- []byte("more data")
			close(s.in)

			if err := runDirect(t, context.Background(), s, helperCommand(t, "echo"), 0); err != nil {
				t.Fatalf("RunCommandDirect: %v", err)
			}
			out := logs()
			if n := strings.Count(out, "ZRPOS "+name+" header detected"); n != 1 {
				t.Errorf("header detected %d times, want 1; log:\n%s", n, out)
			}
			if !strings.Contains(out, "backoff=1") || strings.Contains(out, "backoff=2") {
				t.Errorf("want exactly 1 backoff signal, log:\n%s", out)
			}
		})
	}
}

func TestAdaptiveCopy_shortWriteAndReadError(t *testing.T) {
	short := writeFunc(func(p []byte) (int, error) { return len(p) - 1, nil })
	n, err := adaptiveCopy(short, strings.NewReader("abcdef"), nil)
	if err != io.ErrShortWrite || n != 5 {
		t.Errorf("short write: n=%d err=%v, want 5, io.ErrShortWrite", n, err)
	}

	var dst bytes.Buffer
	errRead := errors.New("source failed")
	src := io.MultiReader(strings.NewReader("abc"), errReader{errRead})
	n, err = adaptiveCopy(&dst, src, nil)
	if err != errRead || n != 3 || dst.String() != "abc" {
		t.Errorf("read error: n=%d err=%v dst=%q, want 3, %v, \"abc\"", n, err, dst.String(), errRead)
	}
}

type errReader struct{ err error }

func (r errReader) Read([]byte) (int, error) { return 0, r.err }

// --- ExecuteSend / ExecuteReceive ---

// helperProtocol returns a protocol whose send and receive commands are the
// helper process running in mode.
func helperProtocol(t *testing.T, mode string) ProtocolConfig {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(helperEnv, mode)
	t.Setenv("GORACE", strings.TrimPrefix(helperGORACE, "GORACE="))
	return ProtocolConfig{
		Key: "H", Name: "Helper",
		SendCmd: exe, SendArgs: []string{helperRunFlag, "--", "-b", "{filePath}"},
		RecvCmd: exe, RecvArgs: []string{helperRunFlag, "--", "-r", "{targetDir}"},
		BatchSend: true,
	}
}

// absPath returns an absolute path that is valid on the host OS; the files
// need not exist because the helper never opens them.
func absPath(t *testing.T, name string) string {
	t.Helper()
	p, err := filepath.Abs(name)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestExecuteSend_validation(t *testing.T) {
	s := newRawSession()
	a, b := absPath(t, "a.zip"), absPath(t, "b.zip")

	tests := []struct {
		name    string
		p       ProtocolConfig
		files   []string
		wantErr string
	}{
		{"no files", ProtocolConfig{Name: "Zmodem", BatchSend: true}, nil, "no files provided"},
		{"batch unsupported", ProtocolConfig{Name: "Xmodem"}, []string{a, b}, "does not support batch"},
		{"relative path", ProtocolConfig{Name: "Zmodem", BatchSend: true}, []string{a, "b.zip"}, "must be absolute"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.p.ExecuteSend(context.Background(), s, tt.files...)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("err = %v, want it to contain %q", err, tt.wantErr)
			}
		})
	}

	p := ProtocolConfig{Name: "Zmodem", SendCmd: filepath.Join(t.TempDir(), "missing-sz")}
	err := p.ExecuteSend(context.Background(), s, a)
	if !errors.Is(err, ErrBinaryNotFound) {
		t.Fatalf("err = %v, want ErrBinaryNotFound", err)
	}
	if got := s.written() + s.rawWritten(); got != "" {
		t.Errorf("rejected sends wrote %q to the session", got)
	}
}

func TestExecuteSend_passesFilesToCommand(t *testing.T) {
	p := helperProtocol(t, "args")
	a, b := absPath(t, "a.zip"), absPath(t, "b.zip")
	s := newRawSession()

	if err := p.ExecuteSend(nilCtx, s, a, b); err != nil {
		t.Fatalf("ExecuteSend: %v", err)
	}
	want := "-b|" + a + "|" + b + "|cwd="
	if got := s.rawWritten(); !strings.HasPrefix(got, want) {
		t.Errorf("command saw %q, want prefix %q", got, want)
	}
}

func TestExecuteSend_fileListIsWrittenThenRemoved(t *testing.T) {
	p := helperProtocol(t, "list")
	p.SendArgs = []string{helperRunFlag, "--", "sz", "@{fileListPath}"}
	// UsePTY with a session that has no PTY falls back to direct mode.
	p.UsePTY = true
	a, b := absPath(t, "a.zip"), absPath(t, "b.zip")
	s := newRawSession()

	if err := p.ExecuteSend(context.Background(), s, a, b); err != nil {
		t.Fatalf("ExecuteSend: %v", err)
	}
	header, body, ok := strings.Cut(s.rawWritten(), "\n")
	listFile, isList := strings.CutPrefix(header, "list=")
	if !ok || !isList {
		t.Fatalf("command output = %q, want list=<path> header", s.rawWritten())
	}
	if want := a + "\n" + b + "\n"; body != want {
		t.Errorf("file list contents = %q, want %q", body, want)
	}
	if _, err := os.Stat(listFile); !os.IsNotExist(err) {
		os.Remove(listFile)
		t.Errorf("file list %s not removed after send (stat err = %v)", listFile, err)
	}
}

func TestExecuteSend_relativeCommandResolved(t *testing.T) {
	// A send_cmd given relative to the BBS working directory is made absolute
	// before the command is built.
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	p := helperProtocol(t, "args")
	t.Chdir(filepath.Dir(exe))
	p.SendCmd = "." + string(filepath.Separator) + filepath.Base(exe)
	a := absPath(t, "a.zip")
	s := newRawSession()

	if err := p.ExecuteSend(context.Background(), s, a); err != nil {
		t.Fatalf("ExecuteSend: %v", err)
	}
	if got, want := s.rawWritten(), "-b|"+a+"|cwd="; !strings.HasPrefix(got, want) {
		t.Errorf("command saw %q, want prefix %q", got, want)
	}
}

func TestExecuteReceive_validation(t *testing.T) {
	s := newRawSession()
	p := ProtocolConfig{Name: "Zmodem", RecvCmd: filepath.Join(t.TempDir(), "missing-rz")}

	err := p.ExecuteReceive(context.Background(), s, "")
	if err == nil || !strings.Contains(err.Error(), "cannot be empty") {
		t.Errorf("empty dir: err = %v", err)
	}
	err = p.ExecuteReceive(context.Background(), s, "relative/dir")
	if err == nil || !strings.Contains(err.Error(), "must be absolute") {
		t.Errorf("relative dir: err = %v", err)
	}
	err = p.ExecuteReceive(context.Background(), s, t.TempDir())
	if !errors.Is(err, ErrBinaryNotFound) {
		t.Errorf("missing binary: err = %v, want ErrBinaryNotFound", err)
	}
	if got := s.written() + s.rawWritten(); got != "" {
		t.Errorf("rejected receives wrote %q to the session", got)
	}
}

func TestExecuteReceive_runsInTargetDir(t *testing.T) {
	for _, usePTY := range []bool{false, true} {
		t.Run(fmt.Sprintf("usePTY=%v", usePTY), func(t *testing.T) {
			p := helperProtocol(t, "args")
			p.UsePTY = usePTY // no PTY on the session: both run direct
			// Resolve symlinks (macOS /var, Windows short names) so the
			// helper's os.Getwd() can be compared verbatim.
			dir, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			s := newRawSession()

			if err := p.ExecuteReceive(nilCtx, s, dir); err != nil {
				t.Fatalf("ExecuteReceive: %v", err)
			}
			// {targetDir} is expanded with a trailing separator; the command
			// itself runs inside the directory.
			want := "-r|" + dir + string(filepath.Separator) + "|cwd=" + dir
			if got := s.rawWritten(); got != want {
				t.Errorf("command saw %q, want %q", got, want)
			}
		})
	}
}

func TestLoadProtocols_unreadable(t *testing.T) {
	// A directory exists but cannot be read as a file.
	_, err := LoadProtocols(t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "failed to read protocols file") {
		t.Fatalf("err = %v, want read failure", err)
	}
}
