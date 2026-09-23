package menu

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
)

func TestParseDisconnectKeyViaDoorConfig(t *testing.T) {
	for _, tc := range []struct {
		setting     string
		wantKey     byte
		wantEnabled bool
		wantErr     bool
	}{
		{"", 0x1D, true, false},       // blank means the default, not "off"
		{"^]", 0x1D, true, false},     // the documented spelling
		{"^d", 0x04, true, false},     // lowercase accepted
		{"^D", 0x04, true, false},     // uppercase accepted
		{"none", 0, false, false},     // opting out has to be explicit
		{"NONE", 0, false, false},     // case-insensitive
		{"  ^]  ", 0x1D, true, false}, // surrounding whitespace tolerated
		{"ctrl-]", 0, false, true},    // not a recognised spelling
		{"^", 0, false, true},         // truncated
		{"x", 0, false, true},         // not a control key
	} {
		key, enabled, err := config.ParseDisconnectKey(tc.setting)
		if tc.wantErr {
			if err == nil {
				t.Errorf("ParseDisconnectKey(%q): expected an error", tc.setting)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseDisconnectKey(%q): %v", tc.setting, err)
			continue
		}
		if key != tc.wantKey || enabled != tc.wantEnabled {
			t.Errorf("ParseDisconnectKey(%q) = %#x/%v, want %#x/%v",
				tc.setting, key, enabled, tc.wantKey, tc.wantEnabled)
		}
	}
}

func TestCopyToRemoteForwardsInput(t *testing.T) {
	var dst bytes.Buffer
	if err := copyToRemote(&dst, strings.NewReader("hello door"), 0x1D, true); err != nil {
		t.Fatalf("copyToRemote: %v", err)
	}
	if dst.String() != "hello door" {
		t.Errorf("forwarded %q, want %q", dst.String(), "hello door")
	}
}

// Anything typed before the hang-up key was meant for the door and must still
// reach it; anything after it must not.
func TestCopyToRemoteStopsAtDisconnectKey(t *testing.T) {
	var dst bytes.Buffer
	err := copyToRemote(&dst, strings.NewReader("ab\x1dcd"), 0x1D, true)
	if err != nil {
		t.Fatalf("copyToRemote: %v", err)
	}
	if dst.String() != "ab" {
		t.Errorf("forwarded %q, want %q", dst.String(), "ab")
	}
}

// With the key disabled the byte is door input like any other, so a door that
// uses Ctrl-] itself keeps working.
func TestCopyToRemotePassesDisconnectKeyWhenDisabled(t *testing.T) {
	var dst bytes.Buffer
	if err := copyToRemote(&dst, strings.NewReader("ab\x1dcd"), 0x1D, false); err != nil {
		t.Fatalf("copyToRemote: %v", err)
	}
	if dst.String() != "ab\x1dcd" {
		t.Errorf("forwarded %q, want the disconnect key passed through", dst.String())
	}
}

func TestSubstituteDoorPlaceholders(t *testing.T) {
	subs := map[string]string{"{USERHANDLE}": "Neo", "{NODE}": "3"}
	for _, tc := range []struct{ in, want string }{
		{"[V3]{USERHANDLE}", "[V3]Neo"},
		{"xtrn=LORD", "xtrn=LORD"},
		{"node{NODE}", "node3"},
		{"", ""},
		// Whitespace a sysop configured is part of the value: the handshake
		// fields are sent as written, so only an all-blank field is "unset".
		{"  {USERHANDLE}  ", "  Neo  "},
		{"   ", ""},
		{"{MISSING}", "{MISSING}"},
	} {
		if got := substituteDoorPlaceholders(tc.in, subs); got != tc.want {
			t.Errorf("substituteDoorPlaceholders(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// relaySession is a session whose reads block until the test feeds it, which a
// buffer-backed one cannot do: the relay has to be observed while it is live.
type relaySession struct {
	testSession
	pr *io.PipeReader
	pw *io.PipeWriter

	mu  sync.Mutex
	out bytes.Buffer
}

func newRelaySession() *relaySession {
	pr, pw := io.Pipe()
	return &relaySession{pr: pr, pw: pw}
}

func (rs *relaySession) Read(p []byte) (int, error) { return rs.pr.Read(p) }

func (rs *relaySession) Write(p []byte) (int, error) {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	return rs.out.Write(p)
}

func (rs *relaySession) rendered() string {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	return rs.out.String()
}

// send delivers caller keystrokes to the relay.
func (rs *relaySession) send(t *testing.T, s string) {
	t.Helper()
	if _, err := rs.pw.Write([]byte(s)); err != nil {
		t.Fatalf("send %q: %v", s, err)
	}
}

// doorServer is a stand-in door server: it records the handshake and lets the
// test drive what the door sends back.
type doorServer struct {
	ln        net.Listener
	handshake chan string
	conns     chan net.Conn
}

func newDoorServer(t *testing.T) *doorServer {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	ds := &doorServer{ln: ln, handshake: make(chan string, 1), conns: make(chan net.Conn, 1)}
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		hs, err := readHandshake(c)
		if err != nil {
			_ = c.Close()
			return
		}
		ds.handshake <- hs
		ds.conns <- c
	}()
	t.Cleanup(func() { _ = ln.Close() })
	return ds
}

func (ds *doorServer) hostPort(t *testing.T) (string, int) {
	t.Helper()
	host, portStr, err := net.SplitHostPort(ds.ln.Addr().String())
	if err != nil {
		t.Fatalf("split addr: %v", err)
	}
	var port int
	if _, err := fmt.Sscan(portStr, &port); err != nil {
		t.Fatalf("parse port: %v", err)
	}
	return host, port
}

func newRLoginDoorCtx(t *testing.T, s *relaySession, doorCfg config.DoorConfig) *DoorCtx {
	t.Helper()
	return &DoorCtx{
		Executor: newExecutorWithStrings(config.StringsConfig{
			DoorRemoteConnecting:    "CONNECTING:%s\r\n",
			DoorRemoteConnectFailed: "FAILED:%s\r\n",
			DoorRemoteDisconnected:  "DISCONNECTED:%s\r\n",
		}),
		Session:          s,
		User:             doorUserInfo{ID: 1, Handle: "Neo", RealName: "Thomas Anderson"},
		NodeNumber:       1,
		SessionStartTime: time.Now(),
		Config:           doorCfg,
		DoorName:         "DOORSRV",
		BaudStr:          "38400",
		Subs:             map[string]string{"{USERHANDLE}": "Neo", "{NODE}": "1"},
	}
}

func TestExecuteRLoginDoorRelaysBothDirections(t *testing.T) {
	ds := newDoorServer(t)
	host, port := ds.hostPort(t)
	sess := newRelaySession()

	ctx := newRLoginDoorCtx(t, sess, config.DoorConfig{
		Type: "rlogin", Host: host, Port: port,
		ServerUsername: "[V3]{USERHANDLE}", TerminalType: "xtrn=LORD",
	})

	done := make(chan error, 1)
	go func() { done <- executeRLoginDoor(ctx) }()

	select {
	case got := <-ds.handshake:
		want := "\x00Neo\x00[V3]Neo\x00xtrn=LORD\x00"
		if got != want {
			t.Errorf("handshake = %q, want %q", got, want)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for the handshake")
	}

	conn := <-ds.conns

	// Door output reaches the caller, with the protocol ack stripped.
	if _, err := conn.Write([]byte("\x00Welcome to LORD\r\n")); err != nil {
		t.Fatalf("door write: %v", err)
	}
	waitFor(t, func() bool { return strings.Contains(sess.rendered(), "Welcome to LORD") },
		"door output never reached the caller")

	// Caller keystrokes reach the door.
	sess.send(t, "Y")
	buf := make([]byte, 16)
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("door read: %v", err)
	}
	if string(buf[:n]) != "Y" {
		t.Errorf("door received %q, want %q", buf[:n], "Y")
	}

	// The door hanging up ends the session.
	_ = conn.Close()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("executeRLoginDoor: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("relay did not finish after the door server hung up")
	}

	out := sess.rendered()
	if !strings.Contains(out, "CONNECTING:DOORSRV") {
		t.Errorf("connect notice missing from %q", out)
	}
	if !strings.Contains(out, "DISCONNECTED:DOORSRV") {
		t.Errorf("disconnect notice missing from %q", out)
	}
}

// Pressing the hang-up key must return the caller to the BBS even though the
// door server is still perfectly happy to keep talking.
func TestExecuteRLoginDoorDisconnectKeyEndsSession(t *testing.T) {
	ds := newDoorServer(t)
	host, port := ds.hostPort(t)
	sess := newRelaySession()

	ctx := newRLoginDoorCtx(t, sess, config.DoorConfig{Type: "rlogin", Host: host, Port: port})

	done := make(chan error, 1)
	go func() { done <- executeRLoginDoor(ctx) }()

	<-ds.handshake
	conn := <-ds.conns
	defer conn.Close()

	sess.send(t, "\x1d")

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("executeRLoginDoor: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the disconnect key did not end the session")
	}
}

// An unset time limit means unlimited; an exhausted one must not open a door
// the user has no time to play.
func TestExecuteRLoginDoorRefusesWhenTimeExhausted(t *testing.T) {
	ds := newDoorServer(t)
	host, port := ds.hostPort(t)
	sess := newRelaySession()

	ctx := newRLoginDoorCtx(t, sess, config.DoorConfig{Type: "rlogin", Host: host, Port: port})
	ctx.User.TimeLimit = 30
	ctx.SessionStartTime = time.Now().Add(-31 * time.Minute)

	done := make(chan error, 1)
	go func() { done <- executeRLoginDoor(ctx) }()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("executeRLoginDoor: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("door did not return when the caller had no time left")
	}

	// The door server must never have been dialled: connecting only to hang up
	// wastes a slot on somebody else's machine.
	select {
	case <-ds.handshake:
		t.Error("door server was contacted despite the caller having no time left")
	default:
	}
}

// Time spent dialling has to come out of the caller's limit. Before this was
// one absolute deadline, a slow connect handed back a fresh full allowance:
// 30 seconds left plus a 10-second dial gave 40 seconds of door time.
func TestExecuteRLoginDoorCountsDialTimeAgainstTheLimit(t *testing.T) {
	ds := newDoorServer(t)
	host, port := ds.hostPort(t)
	sess := newRelaySession()

	ctx := newRLoginDoorCtx(t, sess, config.DoorConfig{Type: "rlogin", Host: host, Port: port})
	// One minute of allowance, all but 300ms of it already spent.
	ctx.User.TimeLimit = 1
	ctx.SessionStartTime = time.Now().Add(-time.Minute + 300*time.Millisecond)

	started := time.Now()
	done := make(chan error, 1)
	go func() { done <- executeRLoginDoor(ctx) }()

	<-ds.handshake
	conn := <-ds.conns
	defer conn.Close()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("executeRLoginDoor: %v", err)
		}
		// The session must end at the original deadline, not 300ms after the
		// connection happened to be established.
		if elapsed := time.Since(started); elapsed > 3*time.Second {
			t.Errorf("session ran for %v; the deadline was reset by the dial", elapsed)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("session outlived the caller's time limit")
	}
}

// A connect timeout longer than the caller's remaining time must not let the
// dial alone overrun the limit.
func TestExecuteRLoginDoorCapsConnectTimeoutToTimeLeft(t *testing.T) {
	// A port with nothing listening: the dial fails rather than connecting.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()
	ln.Close()
	host, portStr, _ := net.SplitHostPort(addr)
	var port int
	if _, err := fmt.Sscan(portStr, &port); err != nil {
		t.Fatalf("parse port: %v", err)
	}

	sess := newRelaySession()
	ctx := newRLoginDoorCtx(t, sess, config.DoorConfig{
		Type: "rlogin", Host: host, Port: port, ConnectTimeout: 120,
	})
	ctx.User.TimeLimit = 1
	ctx.SessionStartTime = time.Now().Add(-time.Minute + time.Second)

	started := time.Now()
	if err := executeRLoginDoor(ctx); err != nil {
		t.Errorf("executeRLoginDoor: %v", err)
	}
	if elapsed := time.Since(started); elapsed > 10*time.Second {
		t.Errorf("dial ran for %v, past the caller's remaining second", elapsed)
	}
}

func TestExecuteRLoginDoorRequiresHost(t *testing.T) {
	sess := newRelaySession()
	ctx := newRLoginDoorCtx(t, sess, config.DoorConfig{Type: "rlogin"})

	err := executeRLoginDoor(ctx)
	if err == nil {
		t.Fatal("expected an error for a door with no host")
	}
	if !strings.Contains(err.Error(), "host") {
		t.Errorf("error = %v, want it to mention the missing host", err)
	}
}

func TestExecuteRLoginDoorRejectsBadDisconnectKey(t *testing.T) {
	sess := newRelaySession()
	ctx := newRLoginDoorCtx(t, sess, config.DoorConfig{
		Type: "rlogin", Host: "127.0.0.1", Port: 1, DisconnectKey: "banana",
	})

	// The bad key must be caught before dialling, not after a failed connect.
	if err := executeRLoginDoor(ctx); err == nil {
		t.Fatal("expected an error for an unparseable disconnect key")
	}
}

// A door server that is down must leave the caller at the menu with a notice,
// not a dead session.
func TestExecuteRLoginDoorReportsConnectFailure(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()
	ln.Close() // nothing is listening there now

	host, portStr, _ := net.SplitHostPort(addr)
	var port int
	if _, err := fmt.Sscan(portStr, &port); err != nil {
		t.Fatalf("parse port: %v", err)
	}

	sess := newRelaySession()
	ctx := newRLoginDoorCtx(t, sess, config.DoorConfig{
		Type: "rlogin", Host: host, Port: port, ConnectTimeout: 1,
	})

	// The caller is told once, by the configured message. Returning an error
	// too would make every caller of executeDoor print a second, generic
	// failure on top of it.
	if err := executeRLoginDoor(ctx); err != nil {
		t.Errorf("executeRLoginDoor returned %v; the caller was already notified", err)
	}
	if out := sess.rendered(); !strings.Contains(out, "FAILED:DOORSRV") {
		t.Errorf("failure notice missing from %q", out)
	}
}

// The default handshake must identify the caller rather than connecting as
// nobody, which some door servers accept and then show the wrong player.
func TestExecuteRLoginDoorDefaultsHandshakeToHandle(t *testing.T) {
	ds := newDoorServer(t)
	host, port := ds.hostPort(t)
	sess := newRelaySession()

	ctx := newRLoginDoorCtx(t, sess, config.DoorConfig{Type: "rlogin", Host: host, Port: port})

	done := make(chan error, 1)
	go func() { done <- executeRLoginDoor(ctx) }()

	select {
	case got := <-ds.handshake:
		if want := "\x00Neo\x00Neo\x00ANSI/38400\x00"; got != want {
			t.Errorf("handshake = %q, want %q", got, want)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for the handshake")
	}
	conn := <-ds.conns
	_ = conn.Close()
	<-done
}

func TestRLoginDoorDispatches(t *testing.T) {
	ds := newDoorServer(t)
	host, port := ds.hostPort(t)
	sess := newRelaySession()

	ctx := newRLoginDoorCtx(t, sess, config.DoorConfig{Type: "rlogin", Host: host, Port: port})

	done := make(chan error, 1)
	go func() { done <- executeDoor(ctx) }() // the dispatcher, not the executor

	select {
	case <-ds.handshake:
	case <-time.After(5 * time.Second):
		t.Fatal("executeDoor did not route the rlogin door to the rlogin executor")
	}
	conn := <-ds.conns
	_ = conn.Close()
	<-done
}

// readHandshake reads until the four NUL separators of a complete rlogin
// handshake have arrived. TCP may split the client's single write, so a lone
// Read can return a truncated handshake and fail the assertion for a reason
// that has nothing to do with the client being wrong.
func readHandshake(c net.Conn) (string, error) {
	_ = c.SetReadDeadline(time.Now().Add(5 * time.Second))
	defer func() { _ = c.SetReadDeadline(time.Time{}) }()

	var got []byte
	buf := make([]byte, 64)
	for bytes.Count(got, []byte{0}) < 4 {
		n, err := c.Read(buf)
		got = append(got, buf[:n]...)
		if err != nil {
			return string(got), err
		}
	}
	return string(got), nil
}

// waitFor polls cond until it holds or the test times out. The relay is
// asynchronous, so there is no single point at which output has certainly
// arrived.
func waitFor(t *testing.T, cond func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal(msg)
}
