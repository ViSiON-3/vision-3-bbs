package menu

import (
	"bytes"
	"net"

	"github.com/gliderlabs/ssh"
	"golang.org/x/term"
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
}

func newTestSession(input string) *testSession {
	return &testSession{in: bytes.NewReader([]byte(input))}
}

func (ts *testSession) Read(p []byte) (int, error)  { return ts.in.Read(p) }
func (ts *testSession) Write(p []byte) (int, error) { return ts.out.Write(p) }

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
func (ts *testSession) Context() ssh.Context { return nil }

// output returns everything written to the session so far.
func (ts *testSession) output() string { return ts.out.String() }

// newTestTerminal wraps a test session in a term.Terminal for output capture,
// mirroring how the real session handler builds the terminal.
func newTestTerminal(ts *testSession) *term.Terminal {
	return term.NewTerminal(ts, "")
}
