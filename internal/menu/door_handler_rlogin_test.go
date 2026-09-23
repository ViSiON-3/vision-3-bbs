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
	"github.com/ViSiON-3/vision-3-bbs/internal/rlogin"
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

	awaitHandshake(t, ds, done)
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

// The deadline maths is where the caller's time is actually spent or given
// away, and a loopback dial is too fast to observe it end to end, so it is
// tested directly.
func TestDoorDeadline(t *testing.T) {
	now := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	start := now.Add(-10 * time.Minute)

	t.Run("no limit means no deadline", func(t *testing.T) {
		deadline, timeout, expired := doorDeadline(0, start, 0, now)
		if !deadline.IsZero() || expired {
			t.Errorf("got deadline=%v expired=%v, want no limit", deadline, expired)
		}
		if timeout != rlogin.DefaultTimeout {
			t.Errorf("timeout = %v, want the default %v", timeout, rlogin.DefaultTimeout)
		}
	})

	t.Run("deadline is absolute, not relative to the dial", func(t *testing.T) {
		deadline, _, expired := doorDeadline(30, start, 0, now)
		if expired {
			t.Fatal("expired with 20 minutes left")
		}
		// 30 minutes from the session start, not from now.
		if want := start.Add(30 * time.Minute); !deadline.Equal(want) {
			t.Errorf("deadline = %v, want %v", deadline, want)
		}
	})

	t.Run("exhausted limit is refused", func(t *testing.T) {
		if _, _, expired := doorDeadline(10, start, 0, now); !expired {
			t.Error("a caller whose limit ran out exactly now should be refused")
		}
		if _, _, expired := doorDeadline(5, start, 0, now); !expired {
			t.Error("a caller past their limit should be refused")
		}
	})

	t.Run("connect timeout is capped to the time left", func(t *testing.T) {
		// 30s left, but the door is configured to wait two minutes.
		_, timeout, expired := doorDeadline(10, now.Add(-9*time.Minute-30*time.Second), 120, now)
		if expired {
			t.Fatal("expired with 30s left")
		}
		if timeout != 30*time.Second {
			t.Errorf("timeout = %v, want it capped to the 30s remaining", timeout)
		}
	})

	t.Run("configured timeout is kept when it fits", func(t *testing.T) {
		_, timeout, _ := doorDeadline(30, start, 5, now)
		if timeout != 5*time.Second {
			t.Errorf("timeout = %v, want the configured 5s", timeout)
		}
	})

	// rlogin.Dial reads a nonpositive timeout as "unset" and substitutes its
	// own default, so a caller out of time must never reach it with one.
	t.Run("never yields a nonpositive timeout", func(t *testing.T) {
		for _, tc := range []struct {
			name  string
			limit int
			start time.Time
		}{
			{"exactly out of time", 10, start},
			{"well past the limit", 1, start},
			{"a hair left", 10, start.Add(time.Nanosecond)},
		} {
			_, timeout, expired := doorDeadline(tc.limit, tc.start, 60, now)
			if expired {
				continue // refused before dialling, which is the point
			}
			if timeout <= 0 {
				t.Errorf("%s: timeout = %v; Dial would replace it with its default and outlive the deadline",
					tc.name, timeout)
			}
		}
	})
}

// The relay must stop at the deadline even while the door server is still
// happily connected.
func TestRelayRLoginSessionStopsAtDeadline(t *testing.T) {
	ds := newDoorServer(t)
	host, port := ds.hostPort(t)
	sess := newRelaySession()
	ctx := newRLoginDoorCtx(t, sess, config.DoorConfig{Type: "rlogin", Host: host, Port: port})

	conn, err := net.Dial("tcp", net.JoinHostPort(host, fmt.Sprint(port)))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	done := make(chan struct{})
	started := time.Now()
	go func() {
		defer close(done)
		relayRLoginSession(ctx, conn, 0x1D, true, time.Now().Add(300*time.Millisecond))
	}()

	select {
	case <-done:
		if elapsed := time.Since(started); elapsed > 5*time.Second {
			t.Errorf("relay ran %v past a 300ms deadline", elapsed)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("relay outlived its deadline")
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

// awaitHandshake waits for the stand-in server to receive a handshake, failing
// rather than blocking if the executor returned without dialling.
func awaitHandshake(t *testing.T, ds *doorServer, done <-chan error) string {
	t.Helper()
	select {
	case hs := <-ds.handshake:
		return hs
	case err := <-done:
		t.Fatalf("executeRLoginDoor returned before dialling: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("door server never received a handshake")
	}
	return ""
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
