package sshserver

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gliderlabs/ssh"
	gossh "golang.org/x/crypto/ssh"

	"github.com/ViSiON-3/vision-3-bbs/internal/safego"
	"github.com/ViSiON-3/vision-3-bbs/internal/safego/safegotest"
)

// lockedBuffer is a log destination safe for the server's goroutines.
type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

// captureLog sends the default logger to a buffer for the test.
func captureLog(t *testing.T) *lockedBuffer {
	t.Helper()
	buf := &lockedBuffer{}
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(buf, nil)))
	t.Cleanup(func() { slog.SetDefault(old) })
	return buf
}

// startPanicServer serves sessions with handler on a free local port.
func startPanicServer(t *testing.T, cfg Config) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	cfg.HostKeyPath = writeTestHostKey(t)
	cfg.Host, cfg.Port = "127.0.0.1", port
	srv, err := NewServer(cfg)
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = srv.ListenAndServe() }()
	t.Cleanup(func() { _ = srv.Close() })
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	for i := 0; i < 100; i++ {
		if c, err := net.Dial("tcp", addr); err == nil {
			_ = c.Close()
			return addr
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("server at %s never listened", addr)
	return ""
}

// run opens a session on addr, starts a shell or the subsystem, and returns
// what the server wrote before the session ended.
func run(t *testing.T, addr, subsystem string) string {
	t.Helper()
	c, err := gossh.Dial("tcp", addr, &gossh.ClientConfig{
		User:            "caller",
		HostKeyCallback: gossh.InsecureIgnoreHostKey(),
		Timeout:         5 * time.Second,
	})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()
	s, err := c.NewSession()
	if err != nil {
		t.Fatalf("session: %v", err)
	}
	defer s.Close()
	// A pipe, not Stdout: RequestSubsystem, unlike Shell, does not start the
	// copy into Stdout.
	stdout, err := s.StdoutPipe()
	if err != nil {
		t.Fatalf("stdout: %v", err)
	}
	if subsystem != "" {
		err = s.RequestSubsystem(subsystem)
	} else {
		err = s.Shell()
	}
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	type result struct {
		out []byte
		err error
	}
	done := make(chan result, 1)
	go func() {
		// Enough for "served"; the subsystem then waits for the close.
		buf := make([]byte, len("served"))
		n, err := io.ReadFull(stdout, buf)
		done <- result{buf[:n], err}
	}()
	select {
	case r := <-done:
		return string(r.out)
	case <-time.After(10 * time.Second):
		t.Fatal("session did not end")
		return ""
	}
}

// A panic while serving one session ends that session, is logged with its
// stack, and the next session is served.
func TestPanicEndsOnlyItsSession(t *testing.T) {
	logs := captureLog(t)
	var n atomic.Int32
	addr := startPanicServer(t, Config{SessionHandler: func(s ssh.Session) {
		if n.Add(1) == 1 {
			panic("caller one broke it")
		}
		_, _ = s.Write([]byte("served"))
	}})

	run(t, addr, "")
	if got := run(t, addr, ""); got != "served" {
		t.Errorf("second session got %q, want served", got)
	}
	log := logs.String()
	if !strings.Contains(log, "caller one broke it") || !strings.Contains(log, "panic_test.go") {
		t.Errorf("panic not logged with its stack:\n%s", log)
	}
}

// The same holds for a subsystem session.
func TestPanicEndsOnlyItsSubsystemSession(t *testing.T) {
	logs := captureLog(t)
	var n atomic.Int32
	addr := startPanicServer(t, Config{
		SessionHandler: func(ssh.Session) {},
		SubsystemHandlers: map[string]func(ssh.Session){"probe": func(s ssh.Session) {
			if n.Add(1) == 1 {
				panic("subsystem broke")
			}
			_, _ = s.Write([]byte("served"))
			// Hold the channel open until the client is done with it, so
			// the write is not cut off by the subsystem's end.
			_, _ = io.Copy(io.Discard, s)
		}},
	})

	run(t, addr, "probe")
	if got := run(t, addr, "probe"); got != "served" {
		t.Errorf("second session got %q, want served", got)
	}
	if log := logs.String(); !strings.Contains(log, "subsystem broke") || !strings.Contains(log, "panic_test.go") {
		t.Errorf("panic not logged with its stack:\n%s", log)
	}
}

// A panic in an auth callback refuses that attempt and leaves the server
// taking the next.
func TestPanicInPasswordAuthRefusesThatAttempt(t *testing.T) {
	logs := captureLog(t)
	addr := startPanicServer(t, Config{
		PasswordHandler: func(_ ssh.Context, password string) bool {
			if password == "boom" {
				panic("password check broke")
			}
			return password == "good"
		},
		SessionHandler: func(s ssh.Session) { _, _ = s.Write([]byte("served")) },
	})
	dial := func(password string) (*gossh.Client, error) {
		return gossh.Dial("tcp", addr, &gossh.ClientConfig{
			User:            "caller",
			Auth:            []gossh.AuthMethod{gossh.Password(password)},
			HostKeyCallback: gossh.InsecureIgnoreHostKey(),
			Timeout:         5 * time.Second,
		})
	}
	if c, err := dial("boom"); err == nil {
		_ = c.Close()
		t.Fatal("a panicking password check let the caller in")
	}
	c, err := dial("good")
	if err != nil {
		t.Fatalf("next caller refused: %v", err)
	}
	defer c.Close()
	s, err := c.NewSession()
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if out, _ := s.Output(""); string(out) != "served" {
		t.Errorf("next caller got %q, want served", out)
	}
	if log := logs.String(); !strings.Contains(log, "password check broke") || !strings.Contains(log, "panic_test.go") {
		t.Errorf("panic not logged with its stack:\n%s", log)
	}
}

// The client of a session that panicked is told it failed.
func TestPanickedSessionExitsNonZero(t *testing.T) {
	captureLog(t)
	addr := startPanicServer(t, Config{SessionHandler: func(ssh.Session) { panic("broke") }})
	c, err := gossh.Dial("tcp", addr, &gossh.ClientConfig{
		User:            "caller",
		HostKeyCallback: gossh.InsecureIgnoreHostKey(),
		Timeout:         5 * time.Second,
	})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()
	s, err := c.NewSession()
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	err = s.Run("")
	var exit *gossh.ExitError
	if !errors.As(err, &exit) || exit.ExitStatus() != 1 {
		t.Errorf("Run = %v, want exit status 1", err)
	}
}

// A panic on the goroutine that reads an SSH session for its caller ends
// that caller's reads with an error, so the session ends; the next session
// reads.
func TestTerminalReadPanicEndsOnlyItsSession(t *testing.T) {
	logs := safegotest.Capture(t)
	safegotest.Inject(t, safego.KindTerminal)

	raw1 := &blockedReadSession{started: make(chan struct{}, 4), replies: make(chan readResult, 4)}
	defer close(raw1.replies)
	first := &BBSSession{Session: raw1}
	r := awaitRead(t, readAsync(first, 8))
	logs.WaitPanic(t, safego.KindTerminal, "sshserver/server.go")
	if !errors.Is(r.err, safego.ErrPanicked) {
		t.Errorf("first session's read = %+v, want safego.ErrPanicked", r)
	}

	raw2 := &blockedReadSession{started: make(chan struct{}, 4), replies: make(chan readResult, 4)}
	defer close(raw2.replies)
	second := &BBSSession{Session: raw2}
	read := readAsync(second, 8)
	<-raw2.started
	raw2.replies <- readResult{data: []byte("x")}
	if r := awaitRead(t, read); string(r.data) != "x" || r.err != nil {
		t.Errorf("second session's read = %+v, want x", r)
	}
}
