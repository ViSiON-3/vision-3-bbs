//go:build !windows

package menu

import (
	"net"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/editor"
)

// closeHookConn runs onClose once Close has closed the connection.
type closeHookConn struct {
	net.Conn
	onClose func()
}

func (c *closeHookConn) Close() error {
	err := c.Conn.Close()
	c.onClose()
	return err
}

// doorcovReadKeys fails the test unless the prompt's handler reads want.
func doorcovReadKeys(t *testing.T, ih *editor.InputHandler, want string) {
	t.Helper()
	stop := make(chan struct{})
	timer := time.AfterFunc(2*time.Second, func() { close(stop) })
	defer timer.Stop()
	for _, w := range []byte(want) {
		if b, err := ih.ReadByteUntil(stop); err != nil || b != w {
			t.Fatalf("prompt read %q, %v; want %q", b, err, w)
		}
	}
}

// Keys the handler held before a remote door, then keys typed once the
// door server has hung up, reach the next prompt in order (#769).
func TestDoorcovRemoteDoorKeepsTypeAhead(t *testing.T) {
	env := newMenuEnv(t)
	s := newDoorcovSession()
	t.Cleanup(func() {
		resetSessionIH(s)
		sessionTypeAhead.Delete(s)
	})

	s.send("abc")
	doorcovReadKeys(t, getSessionIH(s), "a")
	// Wait until the handler has taken b and c from the session.
	waitFor(t, func() bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		return len(s.pending)+len(s.keys) == 0
	}, "input handler did not read ahead")

	// The door server says BYE and hangs up.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		_, _ = c.Write([]byte("BYE"))
		_ = c.Close()
	}()
	raw, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	// Keys typed once the relay has closed the connection cannot be sent.
	// They are typed before the relay stops its input copier, which reads
	// them (the session gives sent keys ahead of the interrupt).
	conn := &closeHookConn{Conn: raw, onClose: func() { s.send("xyz") }}

	ctx := doorcovCtx(env, s, config.DoorConfig{Type: "rlogin"})
	relay := func(c *DoorCtx) error {
		relayRemoteSession(c, conn, conn, 0x1D, true, time.Time{})
		return nil
	}
	handOffSessionIH(s)
	watched := func(c *DoorCtx) error { return runDoorWatched(c, relay) }
	if err := doorcovExec(t, s, watched, ctx); err != nil {
		t.Fatalf("relayRemoteSession: %v", err)
	}
	doorcovHas(t, s, "BYE")
	if v, ok := sessionTypeAhead.Load(s); !ok || string(v.([]byte)) != "bcxyz" {
		t.Errorf("kept type-ahead = %q, want %q", v, "bcxyz")
	}
	doorcovReadKeys(t, getSessionIH(s), "bcxyz")
}

// The prompt that launched a remote door read the CR of an Enter whose LF
// had not arrived. The LF reaches the door after the hand-off, and is not
// sent to the door server.
func TestDoorcovRemoteDoorDropsEnterTrailer(t *testing.T) {
	env := newMenuEnv(t)
	s := newDoorcovSession()
	t.Cleanup(func() {
		resetSessionIH(s)
		sessionTypeAhead.Delete(s)
	})

	s.send("\r")
	ih := getSessionIH(s)
	doorcovReadKeys(t, ih, "\r")
	ih.SkipEnterTrailer() // as a line prompt does; the LF is not there yet

	ds := newDoorServer(t)
	host, port := ds.hostPort(t)
	ctx := doorcovCtx(env, s, config.DoorConfig{Type: "rlogin", Host: host, Port: port})
	handOffSessionIH(s)
	done := make(chan error, 1)
	go func() { done <- runDoorWatched(ctx, executeRLoginDoor) }()
	awaitHandshake(t, ds, done)
	conn := <-ds.conns
	defer conn.Close()

	s.send("\nY")
	buf := make([]byte, 16)
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("door read: %v", err)
	}
	if string(buf[:n]) != "Y" {
		t.Errorf("door received %q, want %q", buf[:n], "Y")
	}

	_ = conn.Close()
	awaitDone(t, done, "rlogin door")
	if takeEnterTrailer(s) {
		t.Error("trailer still marked after the door dropped it")
	}
}

// A remote door that could not be reached read nothing, so the trailer of
// the Enter that launched it is still the next reader's to drop.
func TestDoorcovRemoteDoorUnreachableKeepsEnterTrailer(t *testing.T) {
	env := newMenuEnv(t)
	s := newDoorcovSession()
	t.Cleanup(func() { takeEnterTrailer(s) })

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().(*net.TCPAddr)
	_ = ln.Close() // nothing listens there now

	for _, typ := range []string{"rlogin", "telnet"} {
		keepEnterTrailer(s, true)
		ctx := doorcovCtx(env, s, config.DoorConfig{Type: typ, Host: "127.0.0.1", Port: addr.Port})
		if err := doorcovExec(t, s, func(c *DoorCtx) error { return runDoorWatched(c, runDoorByType) }, ctx); err != nil {
			t.Fatalf("%s door: %v", typ, err)
		}
		if !takeEnterTrailer(s) {
			t.Errorf("%s door: trailer mark lost though the door read nothing", typ)
		}
	}
}
