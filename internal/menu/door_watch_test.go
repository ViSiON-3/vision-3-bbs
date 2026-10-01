package menu

import (
	"errors"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/gliderlabs/ssh"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/editor"
)

func TestDoorWatchIdle(t *testing.T) {
	w := newDoorWatch(150*time.Millisecond, time.Time{})
	defer w.freeze()

	// Input keeps restarting the countdown, so it outlasts the timeout.
	for range 5 {
		time.Sleep(50 * time.Millisecond)
		w.touch()
	}
	if w.endReason() != 0 {
		t.Fatal("watch fired although the caller kept typing")
	}

	select {
	case <-w.Ended():
	case <-time.After(2 * time.Second):
		t.Fatal("watch never fired once input stopped")
	}
	// Late input doesn't revive a watch that has fired.
	w.touch()
	if w.endReason() == 0 {
		t.Error("touch after firing revived the watch")
	}
}

func TestDoorWatchNil(t *testing.T) {
	var w *doorWatch
	w.freeze()
	if w.endReason() != 0 || w.Ended() != nil {
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
	w := newDoorWatch(time.Hour, time.Time{})
	defer w.freeze()

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
	w := newDoorWatch(150*time.Millisecond, time.Time{})
	defer w.freeze()
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
	if w.endReason() != 0 {
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

// A frozen watch never fires, keeps a firing that already happened, and
// isn't revived by input.
func TestDoorWatchFreeze(t *testing.T) {
	w := newDoorWatch(100*time.Millisecond, time.Time{})
	w.freeze()
	w.touch()
	time.Sleep(250 * time.Millisecond)
	if w.endReason() != 0 {
		t.Error("frozen watch fired")
	}

	w = newDoorWatch(50*time.Millisecond, time.Time{})
	<-w.Ended()
	w.freeze()
	if w.endReason() == 0 {
		t.Error("freezing a fired watch forgot that it fired")
	}
}

// Connecting to a remote door server is abandoned when the caller goes idle.
func TestIdleDialContext(t *testing.T) {
	ctx := &DoorCtx{watch: newDoorWatch(100*time.Millisecond, time.Time{})}
	defer ctx.watch.freeze()
	dialCtx, cancel := doorDialContext(ctx)
	defer cancel()
	select {
	case <-dialCtx.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("dial context outlived the idle timeout")
	}

	// With no idle timeout it lasts until the caller cancels it.
	dialCtx, cancel = doorDialContext(&DoorCtx{})
	select {
	case <-dialCtx.Done():
		t.Fatal("dial context ended with no idle timeout")
	case <-time.After(150 * time.Millisecond):
	}
	cancel()
}

// The time limit ends the door at the deadline, and reports it as such.
func TestDoorWatchTimeLimit(t *testing.T) {
	w := newDoorWatch(0, time.Now().Add(100*time.Millisecond))
	defer w.freeze()
	select {
	case <-w.Ended():
	case <-time.After(2 * time.Second):
		t.Fatal("watch outlived the time limit")
	}
	if got := w.endReason(); got != doorEndTimeLimit {
		t.Errorf("reason = %v, want time limit", got)
	}
	if err := w.endReason().err(); !errors.Is(err, editor.ErrTimeLimit) {
		t.Errorf("err = %v, want editor.ErrTimeLimit", err)
	}
}

// A failed read means the caller hung up, unless the door cancelled it with
// its read interrupt, which on telnet also looks like end of file.
func TestWatchedSessionDisconnect(t *testing.T) {
	w := newDoorWatch(0, time.Time{})
	ws := wrapDoorSession(plainReadSession{r: strings.NewReader("")}, w)
	if _, err := ws.Read(make([]byte, 1)); !errors.Is(err, io.EOF) {
		t.Fatalf("Read = %v, want EOF", err)
	}
	if got := w.endReason(); got != doorEndDisconnect {
		t.Errorf("reason = %v, want disconnect", got)
	}

	w = newDoorWatch(0, time.Time{})
	ir := &interruptReadSession{plainReadSession: plainReadSession{r: strings.NewReader("")}}
	wi := wrapDoorSession(ir, w).(readInterrupter)
	ch := make(chan struct{})
	wi.SetReadInterrupt(ch)
	close(ch)
	wi.SetReadInterrupt(nil) // executors clear it on their way out
	if _, err := wi.(io.Reader).Read(make([]byte, 1)); !errors.Is(err, io.EOF) {
		t.Fatalf("Read = %v, want EOF", err)
	}
	if got := w.endReason(); got != 0 {
		t.Errorf("an interrupted read ended the door: %v", got)
	}
}

// A caller with no time left isn't let into a door at all.
func TestRunDoorWatchedRefusesExhaustedTime(t *testing.T) {
	ctx := &DoorCtx{SessionStartTime: time.Now().Add(-31 * time.Minute)}
	ctx.User.TimeLimit = 30
	ran := false
	err := runDoorWatched(ctx, func(*DoorCtx) error { ran = true; return nil })
	if !errors.Is(err, editor.ErrTimeLimit) || ran {
		t.Errorf("err=%v ran=%v, want editor.ErrTimeLimit without running the door", err, ran)
	}
}

// An outbound RLogin door is ended for each reason the BBS ends a door: its
// connection to the door server is closed, and executeDoor reports why.
func TestDoorWatchEndsRLoginDoor(t *testing.T) {
	tests := []struct {
		name         string
		setup        func(ctx *DoorCtx, sess *relaySession)
		afterConnect bool // run setup once the door server has the connection
		want         error
	}{
		{"caller hangs up", func(_ *DoorCtx, sess *relaySession) { _ = sess.pw.Close() }, true, io.EOF},
		{"caller idle", func(ctx *DoorCtx, _ *relaySession) { ctx.IdleTimeout = 300 * time.Millisecond }, false, editor.ErrIdleTimeout},
		{"time limit", func(ctx *DoorCtx, _ *relaySession) {
			ctx.User.TimeLimit = 1
			ctx.SessionStartTime = time.Now().Add(-time.Minute + 300*time.Millisecond)
		}, false, editor.ErrTimeLimit},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ds := newDoorServer(t)
			host, port := ds.hostPort(t)
			sess := newRelaySession()
			ctx := newRLoginDoorCtx(t, sess, config.DoorConfig{Type: "rlogin", Host: host, Port: port})

			if !tt.afterConnect {
				tt.setup(ctx, sess)
			}
			done := make(chan error, 1)
			go func() { done <- executeDoor(ctx) }()

			var conn net.Conn
			select {
			case conn = <-ds.conns:
				t.Cleanup(func() { _ = conn.Close() })
			case err := <-done:
				t.Fatalf("door ended before connecting: %v", err)
			case <-time.After(5 * time.Second):
				t.Fatal("door server never received a connection")
			}
			if tt.afterConnect {
				tt.setup(ctx, sess)
			}

			select {
			case err := <-done:
				// errors.Is is exact here: ErrTimeLimit wraps ErrIdleTimeout,
				// but a plain idle error doesn't match ErrTimeLimit.
				if !errors.Is(err, tt.want) {
					t.Errorf("err = %v, want %v", err, tt.want)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("RLogin door still running")
			}
			_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
			if _, err := io.Copy(io.Discard, conn); err != nil {
				t.Errorf("door server read after the door ended: %v, want EOF", err)
			}
		})
	}
}
