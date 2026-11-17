package menu

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/gliderlabs/ssh"

	"github.com/ViSiON-3/vision-3-bbs/internal/acs"
	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// The session terms read a call through the BBS's adapter: the node and
// width recorded in the session's context, the output mode, the transport
// and the address. A caller's IPv4 address arriving mapped into IPv6 matches its
// IPv4 network.
func TestACSSessionTermsReadTheCall(t *testing.T) {
	ts := newTestSession("")
	ts.addr = &net.TCPAddr{IP: net.ParseIP("::ffff:10.20.30.40"), Port: 50000}
	ts.ctx = &testCtx{Context: context.Background()}
	SetSessionOutputMode(ts, ansi.OutputModeUTF8)
	t.Cleanup(func() { ClearSessionOutputMode(ts) })
	u := &user.User{AccessLevel: 20}

	// Before the call is recorded: no node, the terminal's width.
	if checkACS("NODE=2", u, ts, nil, time.Now()) || !checkACS("WIDTH=80", u, ts, nil, time.Now()) {
		t.Error("unrecorded: want no node and the terminal's 80 columns")
	}
	SetACSCall(ts, 2, 132)
	for acsStr, want := range map[string]bool{
		"NODE=2": true, "NODE=3": false, "WIDTH>=132": true,
		"CONN=ssh": true, "CONN=telnet": false, "CONN=local": false,
		"ENC=utf8": true, "ENC=cp437": false,
		"IP:10.0.0.0/8": true, "IP:192.168.0.0/16": false,
	} {
		if got := checkACS(acsStr, u, ts, nil, time.Now()); got != want {
			t.Errorf("checkACS(%q) = %v, want %v", acsStr, got, want)
		}
	}
	// The width is clamped as the screens clamp it.
	SetACSCall(ts, 2, 400)
	if got := (acsSession{ts}).Width(); got != user.MaxScreenWidth {
		t.Errorf("width 400 recorded as %d, want %d", got, user.MaxScreenWidth)
	}
	// A wrapped session reads the same call.
	if got := (acsSession{wrappedTestSession{ts}}).Node(); got != 2 {
		t.Errorf("wrapped session node = %d, want 2", got)
	}
	// A session with no context records nothing and reads node 0.
	bare := newTestSession("")
	SetACSCall(bare, 3, 80)
	if got := (acsSession{bare}).Node(); got != 0 {
		t.Errorf("no context: node = %d", got)
	}
	// With no session at all, every session term fails the string.
	if checkACS("S10|!NODE=2", u, nil, nil, time.Now()) {
		t.Error("a session term passed with no session")
	}
}

// A pipe or Unix socket is the local transport, which L counts as local
// too; an SSH call over loopback is L but not CONN=local.
func TestACSSessionLocalTransport(t *testing.T) {
	pipe := newTestSession("")
	pipe.addr = &net.UnixAddr{Name: "/run/vision3.sock", Net: "unix"}
	if got := (acsSession{pipe}).Transport(); got != acs.TransportLocal {
		t.Errorf("unix socket: %s", got)
	}
	loop := newTestSession("") // 127.0.0.1
	u := &user.User{AccessLevel: 20}
	if !checkACS("L", u, loop, nil, time.Now()) || checkACS("CONN=local", u, loop, nil, time.Now()) {
		t.Error("loopback SSH: want L and not CONN=local")
	}
}

// wrappedTestSession wraps a session the way the door watch and snoop
// wrappers do, sharing its context.
type wrappedTestSession struct{ *testSession }

func (w wrappedTestSession) Unwrap() ssh.Session { return w.testSession }
