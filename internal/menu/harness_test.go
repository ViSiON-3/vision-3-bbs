package menu

import (
	"bytes"

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
	in  *bytes.Reader
	out bytes.Buffer
}

func newTestSession(input string) *testSession {
	return &testSession{in: bytes.NewReader([]byte(input))}
}

func (ts *testSession) Read(p []byte) (int, error)  { return ts.in.Read(p) }
func (ts *testSession) Write(p []byte) (int, error) { return ts.out.Write(p) }

// output returns everything written to the session so far.
func (ts *testSession) output() string { return ts.out.String() }

// newTestTerminal wraps a test session in a term.Terminal for output capture,
// mirroring how the real session handler builds the terminal.
func newTestTerminal(ts *testSession) *term.Terminal {
	return term.NewTerminal(ts, "")
}
