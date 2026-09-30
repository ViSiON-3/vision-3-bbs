package menu

import (
	"bytes"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/rlogin"
	"github.com/ViSiON-3/vision-3-bbs/internal/telnetclient"
)

// Telnet protocol bytes, spelled out so the tests read as the wire does.
const (
	tIAC  = 255
	tDONT = 254
	tDO   = 253
	tWONT = 252
	tWILL = 251
	tSB   = 250
	tSE   = 240

	tEcho     = 1
	tTermType = 24
	tNAWS     = 31
)

// telnetDoorServer is a stand-in door server that sends nothing until the
// test tells it to. Unlike the rlogin one it has no handshake to wait for.
type telnetDoorServer struct {
	ln    net.Listener
	conns chan net.Conn
}

func newTelnetDoorServer(t *testing.T) *telnetDoorServer {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	ds := &telnetDoorServer{ln: ln, conns: make(chan net.Conn, 1)}
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		ds.conns <- c
	}()
	t.Cleanup(func() { _ = ln.Close() })
	return ds
}

func (ds *telnetDoorServer) hostPort(t *testing.T) (string, int) {
	t.Helper()
	return (&doorServer{ln: ds.ln}).hostPort(t)
}

// accept waits for the door to connect, failing rather than blocking if the
// executor returned without dialling.
func (ds *telnetDoorServer) accept(t *testing.T, done <-chan error) net.Conn {
	t.Helper()
	select {
	case c := <-ds.conns:
		t.Cleanup(func() { _ = c.Close() })
		return c
	case err := <-done:
		t.Fatalf("executor returned before dialling: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("door server never received a connection")
	}
	return nil
}

// expect reads exactly what the door should have sent the server.
func expect(t *testing.T, c net.Conn, want []byte, what string) {
	t.Helper()
	_ = c.SetReadDeadline(time.Now().Add(5 * time.Second))
	got := make([]byte, len(want))
	n, err := io.ReadFull(c, got)
	if err != nil {
		t.Fatalf("%s: door server read: %v (got % x)", what, err, got[:n])
	}
	if !bytes.Equal(got, want) {
		t.Errorf("%s: door server received % x\nwant % x", what, got, want)
	}
}

func awaitDone(t *testing.T, done <-chan error, what string) {
	t.Helper()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("%s: %v", what, err)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("%s: relay did not finish", what)
	}
}

func TestExecuteTelnetDoorNegotiatesAndRelays(t *testing.T) {
	ds := newTelnetDoorServer(t)
	host, port := ds.hostPort(t)
	sess := newRelaySession()

	ctx := newRLoginDoorCtx(t, sess, config.DoorConfig{
		Type: "telnet", Host: host, Port: port, TerminalType: "ANSI-BBS",
	})
	ctx.User.ScreenWidth, ctx.User.ScreenHeight = 132, 37

	done := make(chan error, 1)
	go func() { done <- executeTelnetDoor(ctx) }()
	conn := ds.accept(t, done)

	// What a BBS opens with, then its banner.
	opening := []byte{tIAC, tWILL, tEcho, tIAC, tDO, tNAWS, tIAC, tDO, tTermType}
	if _, err := conn.Write(append(opening, "Welcome to LORD\r\n"...)); err != nil {
		t.Fatalf("door write: %v", err)
	}
	waitFor(t, func() bool { return strings.Contains(sess.rendered(), "Welcome to LORD") },
		"door output never reached the caller")
	if strings.Contains(sess.rendered(), "\xff") {
		t.Errorf("negotiation reached the caller's screen: %q", sess.rendered())
	}

	// The caller's stored screen size is what the door server is told.
	expect(t, conn, []byte{
		tIAC, tDO, tEcho,
		tIAC, tWILL, tNAWS,
		tIAC, tSB, tNAWS, 0, 132, 0, 37, tIAC, tSE,
		tIAC, tWILL, tTermType,
	}, "opening negotiation")

	if _, err := conn.Write([]byte{tIAC, tSB, tTermType, 1, tIAC, tSE}); err != nil {
		t.Fatalf("door write: %v", err)
	}
	expect(t, conn, append(append([]byte{tIAC, tSB, tTermType, 0}, "ANSI-BBS"...), tIAC, tSE), "terminal type")

	// Caller keystrokes reach the door, with Enter as the protocol wants it.
	sess.send(t, "Y\r")
	expect(t, conn, []byte("Y\r\x00"), "keystrokes")

	_ = conn.Close()
	awaitDone(t, done, "executeTelnetDoor")

	out := sess.rendered()
	if !strings.Contains(out, "CONNECTING:DOORSRV") {
		t.Errorf("connect notice missing from %q", out)
	}
	if !strings.Contains(out, "DISCONNECTED:DOORSRV") {
		t.Errorf("disconnect notice missing from %q", out)
	}
}

// A door server that asks for a login gets it before anything the caller
// types, with the caller's own details filled in.
func TestExecuteTelnetDoorSendsOnConnect(t *testing.T) {
	ds := newTelnetDoorServer(t)
	host, port := ds.hostPort(t)
	sess := newRelaySession()

	ctx := newRLoginDoorCtx(t, sess, config.DoorConfig{
		Type: "telnet", Host: host, Port: port,
		SendOnConnect: "{USERHANDLE}\rsecret\r",
	})

	done := make(chan error, 1)
	go func() { done <- executeTelnetDoor(ctx) }()
	conn := ds.accept(t, done)

	expect(t, conn, []byte("Neo\r\x00secret\r\x00"), "send on connect")

	_ = conn.Close()
	awaitDone(t, done, "executeTelnetDoor")
}

// Raw TCP is for a server that does not speak telnet at all: bytes that look
// like negotiation are the door's output, and nothing is answered.
func TestExecuteTelnetDoorRawTCP(t *testing.T) {
	ds := newTelnetDoorServer(t)
	host, port := ds.hostPort(t)
	sess := newRelaySession()

	ctx := newRLoginDoorCtx(t, sess, config.DoorConfig{
		Type: "telnet", Host: host, Port: port, RawTCP: true,
	})

	done := make(chan error, 1)
	go func() { done <- executeTelnetDoor(ctx) }()
	conn := ds.accept(t, done)

	sent := string([]byte{'a', tIAC, tDO, tNAWS, 'b'})
	if _, err := conn.Write([]byte(sent)); err != nil {
		t.Fatalf("door write: %v", err)
	}
	waitFor(t, func() bool { return strings.Contains(sess.rendered(), sent) },
		"raw door output did not reach the caller unchanged")

	sess.send(t, "x\r\xff")
	expect(t, conn, []byte("x\r\xff"), "keystrokes")

	_ = conn.Close()
	awaitDone(t, done, "executeTelnetDoor")
}

func TestExecuteTelnetDoorDisconnectKeyEndsSession(t *testing.T) {
	ds := newTelnetDoorServer(t)
	host, port := ds.hostPort(t)
	sess := newRelaySession()

	ctx := newRLoginDoorCtx(t, sess, config.DoorConfig{Type: "telnet", Host: host, Port: port})

	done := make(chan error, 1)
	go func() { done <- executeTelnetDoor(ctx) }()
	ds.accept(t, done)

	sess.send(t, "\x1d")
	awaitDone(t, done, "executeTelnetDoor after the hang-up key")
}

func TestTelnetDoorDispatches(t *testing.T) {
	ds := newTelnetDoorServer(t)
	host, port := ds.hostPort(t)
	sess := newRelaySession()

	ctx := newRLoginDoorCtx(t, sess, config.DoorConfig{Type: "telnet", Host: host, Port: port})

	done := make(chan error, 1)
	go func() { done <- executeDoor(ctx) }() // the dispatcher, not the executor
	conn := ds.accept(t, done)

	_ = conn.Close()
	awaitDone(t, done, "executeDoor")
}

func TestExecuteTelnetDoorRequiresHost(t *testing.T) {
	ctx := newRLoginDoorCtx(t, newRelaySession(), config.DoorConfig{Type: "telnet", Host: "  "})
	err := executeTelnetDoor(ctx)
	if err == nil || !strings.Contains(err.Error(), "no host") {
		t.Errorf("executeTelnetDoor = %v, want a missing-host error", err)
	}
}

func TestExecuteTelnetDoorRejectsBadDisconnectKey(t *testing.T) {
	ctx := newRLoginDoorCtx(t, newRelaySession(), config.DoorConfig{
		Type: "telnet", Host: "127.0.0.1", DisconnectKey: "ctrl-]",
	})
	if err := executeTelnetDoor(ctx); err == nil {
		t.Error("executeTelnetDoor accepted a disconnect key it cannot parse")
	}
}

func TestExecuteTelnetDoorReportsConnectFailure(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	host, port := (&doorServer{ln: ln}).hostPort(t)
	_ = ln.Close() // nothing is listening there now

	sess := newRelaySession()
	ctx := newRLoginDoorCtx(t, sess, config.DoorConfig{
		Type: "telnet", Host: host, Port: port, ConnectTimeout: 2,
	})

	if err := executeTelnetDoor(ctx); err != nil {
		t.Errorf("executeTelnetDoor returned %v; the caller was already notified", err)
	}
	if out := sess.rendered(); !strings.Contains(out, "FAILED:DOORSRV") {
		t.Errorf("connect failure notice missing from %q", out)
	}
}

func TestExecuteTelnetDoorRefusesWhenTimeExhausted(t *testing.T) {
	ds := newTelnetDoorServer(t)
	host, port := ds.hostPort(t)
	sess := newRelaySession()

	ctx := newRLoginDoorCtx(t, sess, config.DoorConfig{Type: "telnet", Host: host, Port: port})
	ctx.User.TimeLimit = 30
	ctx.SessionStartTime = time.Now().Add(-31 * time.Minute)

	if err := executeTelnetDoor(ctx); err != nil {
		t.Errorf("executeTelnetDoor: %v", err)
	}
	select {
	case c := <-ds.conns:
		_ = c.Close()
		t.Error("a caller with no time left reached the door server")
	case <-time.After(200 * time.Millisecond):
	}
}

// A bare carriage return is a real thing to send: it is what gets a server
// past "press Enter". The handshake fields' rule, that whitespace alone means
// unset, would swallow it.
func TestExpandSendOnConnectKeepsWhitespace(t *testing.T) {
	subs := map[string]string{"{USERHANDLE}": "Neo"}
	for _, tc := range []struct{ in, want string }{
		{"\r", "\r"},
		{"\r\n", "\r\n"},
		{"{USERHANDLE}\r", "Neo\r"},
		{"", ""},
	} {
		if got := expandSendOnConnect(tc.in, subs); got != tc.want {
			t.Errorf("expandSendOnConnect(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestRemoteDoorAddrUsesTheProtocolsPort(t *testing.T) {
	for _, tc := range []struct {
		door config.DoorConfig
		want string
	}{
		{config.DoorConfig{Type: "rlogin", Host: "doors.example.com"}, "doors.example.com:513"},
		{config.DoorConfig{Type: "telnet", Host: "doors.example.com"}, "doors.example.com:23"},
		{config.DoorConfig{Type: "telnet", Host: " doors.example.com ", Port: 2323}, "doors.example.com:2323"},
		{config.DoorConfig{Type: "rlogin", Host: "::1", Port: 3513}, "[::1]:3513"},
	} {
		if got := remoteDoorAddr(tc.door); got != tc.want {
			t.Errorf("remoteDoorAddr(%s %q port %d) = %q, want %q",
				tc.door.Type, tc.door.Host, tc.door.Port, got, tc.want)
		}
	}
}

// doorDeadline hands every dialer the same default, which is only right
// while they agree on what it is.
func TestRemoteDoorDefaultTimeoutMatchesTheDialers(t *testing.T) {
	if rlogin.DefaultTimeout != remoteDoorDefaultTimeout {
		t.Errorf("rlogin default %v, remote door default %v", rlogin.DefaultTimeout, remoteDoorDefaultTimeout)
	}
	if telnetclient.DefaultTimeout != remoteDoorDefaultTimeout {
		t.Errorf("telnet default %v, remote door default %v", telnetclient.DefaultTimeout, remoteDoorDefaultTimeout)
	}
}
