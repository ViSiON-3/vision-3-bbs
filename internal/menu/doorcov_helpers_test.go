//go:build !windows

package menu

import (
	"bytes"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
	"github.com/gliderlabs/ssh"
	"golang.org/x/term"
)

// doorcovSession is a session for driving real door processes. testSession
// cannot: a door reads the session from its own goroutines while the test
// looks at the output, a door error is written to Stderr, and the PTY path
// wants Pty, Signals and Break.
//
// Reads block until the test sends keystrokes, so input can be held back
// until the door has asked for it (see whenOutput); eof ends the input. Like
// the SSH and telnet adapters it supports SetReadInterrupt, so the executors
// take their clean-shutdown path and no reader goroutine outlives a test.
type doorcovSession struct {
	ssh.Session
	keys  chan []byte
	winCh chan ssh.Window
	isPty bool

	mu      sync.Mutex
	pending []byte // read from keys but not yet handed to a Read
	intr    <-chan struct{}
	out     bytes.Buffer
	hooks   []*outputHook
	eofOnce sync.Once
}

func newDoorcovSession() *doorcovSession {
	return &doorcovSession{keys: make(chan []byte, 16), winCh: make(chan ssh.Window, 1)}
}

// newDoorcovScripted is a session whose whole input is already typed: reads
// return input and then io.EOF, as testSession's do.
func newDoorcovScripted(input string) *doorcovSession {
	s := newDoorcovSession()
	if input != "" {
		s.send(input)
	}
	s.eof()
	return s
}

func (s *doorcovSession) Read(p []byte) (int, error) {
	s.mu.Lock()
	if len(s.pending) > 0 {
		n := copy(p, s.pending)
		s.pending = s.pending[n:]
		s.mu.Unlock()
		return n, nil
	}
	intr := s.intr
	s.mu.Unlock()

	select {
	case b, ok := <-s.keys:
		if !ok {
			return 0, io.EOF
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		n := copy(p, b)
		s.pending = append(s.pending, b[n:]...)
		return n, nil
	case <-intr:
		return 0, errors.New("read interrupted")
	}
}

func (s *doorcovSession) Write(p []byte) (int, error) {
	s.mu.Lock()
	n, err := s.out.Write(p)
	var fire []func()
	for _, h := range s.hooks {
		if !h.fired && strings.Contains(s.out.String(), h.marker) {
			h.fired = true
			fire = append(fire, h.fn)
		}
	}
	s.mu.Unlock()
	for _, fn := range fire {
		fn()
	}
	return n, err
}

// SetReadInterrupt makes a blocked Read return once ch is closed.
func (s *doorcovSession) SetReadInterrupt(ch <-chan struct{}) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.intr = ch
}

func (s *doorcovSession) Pty() (ssh.Pty, <-chan ssh.Window, bool) {
	return ssh.Pty{}, s.winCh, s.isPty
}

func (s *doorcovSession) Stderr() io.ReadWriter     { return s }
func (s *doorcovSession) Signals(chan<- ssh.Signal) {}
func (s *doorcovSession) Break(chan<- bool)         {}
func (s *doorcovSession) Context() ssh.Context      { return nil }
func (s *doorcovSession) RemoteAddr() net.Addr      { return doorcovAddr }
func (s *doorcovSession) send(keys string)          { s.keys <- []byte(keys) }
func (s *doorcovSession) eof()                      { s.eofOnce.Do(func() { close(s.keys) }) }
func (s *doorcovSession) whenOutput(m string, f func()) {
	s.hooks = append(s.hooks, &outputHook{marker: m, fn: f})
}

// doorcovAddr is where every doorcovSession connects from.
var doorcovAddr = &net.TCPAddr{IP: net.IPv4(203, 0, 113, 7), Port: 51234}

// output is everything written to the session so far.
func (s *doorcovSession) output() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.out.String()
}

// doorcovRun is menuEnv.run over a doorcovSession: it calls fn as u and
// returns what it did, failing the test if fn has not returned in 10 seconds.
func doorcovRun(env *menuEnv, s *doorcovSession, fn RunnableFunc, u *user.User, args string) runResult {
	env.t.Helper()
	SetSessionOutputMode(s, env.outputMode)
	env.t.Cleanup(func() {
		resetSessionIH(s)
		ClearSessionOutputMode(s)
	})
	c := &cmdCtx{
		e:                env.e,
		s:                s,
		terminal:         newDoorcovTerminal(s),
		userManager:      env.um,
		currentUser:      u,
		nodeNumber:       1,
		sessionStartTime: time.Now(),
		outputMode:       env.outputMode,
		termWidth:        80,
		termHeight:       24,
	}

	type ret struct {
		u    *user.User
		next string
		err  error
	}
	done := make(chan ret, 1)
	go func() {
		u, next, err := fn(c, args)
		done <- ret{u, next, err}
	}()
	select {
	case r := <-done:
		if errors.Is(r.err, io.EOF) || errors.Is(r.err, errInputAborted) {
			r.err = nil
		}
		return runResult{raw: s.output(), user: r.u, next: r.next, err: r.err}
	case <-time.After(10 * time.Second):
		env.t.Fatalf("handler still running after 10s; output so far:\n%s", s.output())
		return runResult{}
	}
}

// doorcovIsolateTemp points os.TempDir at a fresh directory for the test, so
// door locks, per-node directories and packet files cannot collide with a real
// BBS on this machine and a test can check they were cleaned up.
func doorcovIsolateTemp(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("TMPDIR", dir)
	return dir
}

// doorcovScript writes body as an executable /bin/sh script and returns its
// path.
func doorcovScript(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "door.sh")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

// doorcovCtx builds the DoorCtx a menu would for doorCfg, through
// buildDoorCtx: Neo (user 7, level 50, 132x37) on node 3 with a full hour
// left, launching TESTDOOR.
func doorcovCtx(env *menuEnv, s *doorcovSession, doorCfg config.DoorConfig) *DoorCtx {
	return buildDoorCtx(env.e, s, newDoorcovTerminal(s),
		7, "Neo", "Thomas Anderson", 50, 60, 12, "Zion", 132, 37,
		3, time.Now(), ansi.OutputModeUTF8, doorCfg, "TESTDOOR")
}

// doorcovExec runs exec(ctx) and returns its error, failing the test rather
// than hanging if the door never exits.
func doorcovExec(t *testing.T, s *doorcovSession, exec func(*DoorCtx) error, ctx *DoorCtx) error {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- exec(ctx) }()
	select {
	case err := <-done:
		return err
	case <-time.After(10 * time.Second):
		t.Fatalf("door still running after 10s; output so far:\n%s", s.output())
		return nil
	}
}

// doorcovLines splits a dropfile into its CRLF-terminated lines, failing the
// test if the file does not end with one.
func doorcovLines(t *testing.T, path string) []string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(b), "\r\n")
	if lines[len(lines)-1] != "" {
		t.Fatalf("%s does not end with CRLF", filepath.Base(path))
	}
	return lines[:len(lines)-1]
}

// doorcovWantLines checks 1-based lines of a dropfile, numbered as the format
// documents them.
func doorcovWantLines(t *testing.T, lines []string, want map[int]string) {
	t.Helper()
	for n, w := range want {
		if n > len(lines) {
			t.Errorf("line %d missing: file has %d lines", n, len(lines))
			continue
		}
		if lines[n-1] != w {
			t.Errorf("line %d = %q, want %q", n, lines[n-1], w)
		}
	}
}

// doorcovExists reports whether path exists.
func doorcovExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// newDoorcovTerminal wraps a door session the way the session handler does.
func newDoorcovTerminal(s *doorcovSession) *term.Terminal {
	return term.NewTerminal(s, "")
}
