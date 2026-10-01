package menu

import (
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/gliderlabs/ssh"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/editor"
)

func TestDoorIdleWatch(t *testing.T) {
	w := newDoorIdleWatch(150 * time.Millisecond)
	defer w.stop()

	// Input keeps restarting the countdown, so it outlasts the timeout.
	for range 5 {
		time.Sleep(50 * time.Millisecond)
		w.touch()
	}
	if w.hasFired() {
		t.Fatal("watch fired although the caller kept typing")
	}

	select {
	case <-w.Fired():
	case <-time.After(2 * time.Second):
		t.Fatal("watch never fired once input stopped")
	}
	// Late input doesn't revive a watch that has fired.
	w.touch()
	if !w.hasFired() {
		t.Error("touch after firing revived the watch")
	}
}

func TestDoorIdleWatchNil(t *testing.T) {
	var w *doorIdleWatch
	w.stop()
	if w.hasFired() || w.Fired() != nil {
		t.Error("a nil watch, for a caller with no idle timeout, must never fire")
	}
}

// plainReadSession is a session with no SetReadInterrupt.
type plainReadSession struct {
	ssh.Session
	r io.Reader
}

func (s plainReadSession) Read(p []byte) (int, error) { return s.r.Read(p) }

// interruptReadSession is a session with SetReadInterrupt.
type interruptReadSession struct {
	plainReadSession
	got <-chan struct{}
}

func (s *interruptReadSession) SetReadInterrupt(ch <-chan struct{}) { s.got = ch }

func TestWrapDoorSession(t *testing.T) {
	w := newDoorIdleWatch(time.Hour)
	defer w.stop()

	// Reads pass through, and the wrapper is seen through by type checks.
	plain := plainReadSession{r: strings.NewReader("hi")}
	ws := wrapDoorSession(plain, w)
	buf := make([]byte, 8)
	if n, err := ws.Read(buf); err != nil || string(buf[:n]) != "hi" {
		t.Errorf("Read = %q, %v", buf[:n], err)
	}
	if unwrapSession(ws) != ssh.Session(plain) {
		t.Error("unwrapSession did not return the wrapped session")
	}

	// The wrapper offers SetReadInterrupt exactly when the session does:
	// door executors wait for an input goroutine they can interrupt.
	if _, ok := ws.(readInterrupter); ok {
		t.Error("wrapper claims SetReadInterrupt over a session without it")
	}
	ir := &interruptReadSession{plainReadSession: plainReadSession{r: strings.NewReader("")}}
	wi := wrapDoorSession(ir, w)
	ri, ok := wi.(readInterrupter)
	if !ok {
		t.Fatal("wrapper hides SetReadInterrupt")
	}
	ch := make(chan struct{})
	ri.SetReadInterrupt(ch)
	if ir.got != ch {
		t.Error("SetReadInterrupt did not reach the wrapped session")
	}
}

// Reads from the caller restart the countdown; a read that returns nothing
// does not.
func TestIdleTrackingSessionTouches(t *testing.T) {
	w := newDoorIdleWatch(150 * time.Millisecond)
	defer w.stop()
	pr, pw := io.Pipe()
	ws := wrapDoorSession(plainReadSession{r: pr}, w)
	go func() {
		for range 5 {
			time.Sleep(50 * time.Millisecond)
			_, _ = pw.Write([]byte("k"))
		}
	}()
	buf := make([]byte, 1)
	for range 5 {
		if _, err := ws.Read(buf); err != nil {
			t.Fatalf("Read: %v", err)
		}
	}
	if w.hasFired() {
		t.Error("watch fired although every read brought input")
	}
	_ = pw.Close()
}

func TestBuildDoorCtxIdleTimeout(t *testing.T) {
	tests := []struct {
		name    string
		minutes int
		level   int
		want    time.Duration
	}{
		{"regular user", 5, 50, 5 * time.Minute},
		{"cosysop exempt", 5, 250, 0},
		{"sysop exempt", 5, 255, 0},
		{"timeout disabled", 0, 50, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newExecutorWithServerConfig(config.ServerConfig{
				SessionIdleTimeoutMinutes: tt.minutes, CoSysOpLevel: 250, SysOpLevel: 255,
			})
			ctx := buildDoorCtx(e, nil, nil, 1, "Neo", "", tt.level, 60, 1, "", 80, 25,
				1, time.Now(), 0, config.DoorConfig{}, "GAME")
			if ctx.IdleTimeout != tt.want {
				t.Errorf("IdleTimeout = %v, want %v", ctx.IdleTimeout, tt.want)
			}
		})
	}
}

// An idle caller in a remote door has the connection to the door server
// closed, and executeDoor reports the idle timeout.
func TestDoorIdleEndsRemoteDoor(t *testing.T) {
	ds := newTelnetDoorServer(t)
	host, port := ds.hostPort(t)
	sess := newRelaySession()
	ctx := newRLoginDoorCtx(t, sess, config.DoorConfig{Type: "telnet", Host: host, Port: port, RawTCP: true})
	ctx.IdleTimeout = 300 * time.Millisecond

	done := make(chan error, 1)
	go func() { done <- executeDoor(ctx) }()
	conn := ds.accept(t, done)

	select {
	case err := <-done:
		if !errors.Is(err, editor.ErrIdleTimeout) {
			t.Errorf("err = %v, want editor.ErrIdleTimeout", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("remote door still running after the caller went idle")
	}
	// The door server sees the connection close.
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, err := conn.Read(make([]byte, 1)); !errors.Is(err, io.EOF) {
		t.Errorf("door server read after idle hang-up: %v, want EOF", err)
	}
}
