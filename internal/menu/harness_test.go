package menu

import (
	"bytes"
	"context"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gliderlabs/ssh"
	"golang.org/x/term"
)

// How long a test waits for the code under test to do something: handlerWait
// for a handler to return once its input has run out, or a goroutine it
// started to stop; screenWait for output, an event or a timeout to show.
//
// They are backstops that turn a hang into a failure with a message, not
// bounds on how long the work should take. A Windows runner under the race
// detector has needed over 10s for chat tests that take 0.1s elsewhere, and
// the waits here were 3-10s, so otherwise green runs failed (#906). go test's
// own -timeout still bounds a real hang.
const (
	handlerWait = 60 * time.Second
	screenWait  = 30 * time.Second
)

// testSession is a minimal ssh.Session for driving interactive menu functions
// in tests. Read replays scripted keystrokes and then returns io.EOF (which the
// menu loops treat as a disconnect); Write captures everything the function
// renders. The embedded nil ssh.Session supplies the rest of the interface —
// only methods a function under test actually calls need to be overridden here.
type testSession struct {
	ssh.Session
	in   *bytes.Reader
	out  bytes.Buffer
	addr net.Addr // RemoteAddr result; nil means 127.0.0.1

	// hooks run once each, from Write, as soon as the output first contains
	// their marker. See whenOutput.
	hooks []outputHook

	closed atomic.Bool // Close was called: the BBS hung up

	// ctx is what Context returns; nil unless a test needs one, as a
	// script door does.
	ctx ssh.Context
}

type outputHook struct {
	marker string
	fn     func()
	fired  bool
}

func newTestSession(input string) *testSession {
	return &testSession{in: bytes.NewReader([]byte(input))}
}

func (ts *testSession) Read(p []byte) (int, error) { return ts.in.Read(p) }

// Close records that the BBS hung up on the caller.
func (ts *testSession) Close() error {
	ts.closed.Store(true)
	return nil
}

func (ts *testSession) Write(p []byte) (int, error) {
	n, err := ts.out.Write(p)
	for i := range ts.hooks {
		h := &ts.hooks[i]
		if !h.fired && strings.Contains(ts.out.String(), h.marker) {
			h.fired = true
			h.fn()
		}
	}
	return n, err
}

// whenOutput runs fn once, the first time the function under test has written
// marker. Because a prompt is written before its reply is read, a hook keyed
// on the prompt text runs before the scripted reply is acted on, which lets a
// test play "another session" changing shared data in between.
func (ts *testSession) whenOutput(marker string, fn func()) {
	ts.hooks = append(ts.hooks, outputHook{marker: marker, fn: fn})
}

// hookFired reports whether the hook registered for marker has run, so a test
// can tell its simulated interleaving actually happened.
func (ts *testSession) hookFired(marker string) bool {
	for _, h := range ts.hooks {
		if h.marker == marker {
			return h.fired
		}
	}
	return false
}

// testCtx is a minimal ssh.Context for a testSession that needs one: the
// script engine derives its own context from the session's.
type testCtx struct {
	context.Context
	sync.Mutex
}

func (*testCtx) User() string                  { return "" }
func (*testCtx) SessionID() string             { return "" }
func (*testCtx) ClientVersion() string         { return "" }
func (*testCtx) ServerVersion() string         { return "" }
func (*testCtx) RemoteAddr() net.Addr          { return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 2222} }
func (*testCtx) LocalAddr() net.Addr           { return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 2323} }
func (*testCtx) Permissions() *ssh.Permissions { return &ssh.Permissions{} }
func (*testCtx) SetValue(_, _ interface{})     {}

// Pty reports no PTY, so functions under test fall back to their default
// dimensions (typically 80x24). Returning false avoids the nil-interface panic
// the embedded ssh.Session would otherwise cause.
func (ts *testSession) Pty() (ssh.Pty, <-chan ssh.Window, bool) {
	return ssh.Pty{}, nil, false
}

// RemoteAddr reports addr, or a loopback address when none was set, so
// handlers that log or lock out by IP work without a network connection.
func (ts *testSession) RemoteAddr() net.Addr {
	if ts.addr != nil {
		return ts.addr
	}
	return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 2222}
}

// Context returns nil, which transferContext treats as context.Background,
// so handlers that start ZipLab or a transfer can run. Code that derives a
// context directly from s.Context() (context.WithTimeout and the like) would
// panic on nil; give such tests a session with a real ssh.Context.
func (ts *testSession) Context() ssh.Context { return ts.ctx }

// output returns everything written to the session so far.
func (ts *testSession) output() string { return ts.out.String() }

// newTestTerminal wraps a test session in a term.Terminal for output capture,
// mirroring how the real session handler builds the terminal.
func newTestTerminal(ts *testSession) *term.Terminal {
	return term.NewTerminal(ts, "")
}
