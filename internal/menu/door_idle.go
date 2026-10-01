package menu

import (
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/gliderlabs/ssh"
)

// The session idle timeout is enforced in the BBS's own input loops, which
// read through the session's InputHandler. A door reads the session directly
// instead, with the InputHandler stopped, so executeDoor enforces the same
// timeout itself: it wraps the session so every byte the caller sends
// restarts a countdown, and when the countdown runs out the door is ended
// and executeDoor reports editor.ErrIdleTimeout, which logs the caller off
// with the idle timeout notice as a menu would. Users exempt from the idle
// timeout (CoSysOps and above) are exempt here too.

// doorHangupGrace is how long a door has to exit after it is hung up on
// before it is killed. A variable so tests can shorten it.
var doorHangupGrace = 5 * time.Second

// readInterrupter is a session whose blocked Read can be cancelled.
type readInterrupter interface {
	SetReadInterrupt(<-chan struct{})
}

// doorIdleWatch counts down the caller's idle time while a door runs.
type doorIdleWatch struct {
	timeout time.Duration
	timer   *time.Timer
	fired   chan struct{}
	once    sync.Once
}

// newDoorIdleWatch starts a countdown of timeout.
func newDoorIdleWatch(timeout time.Duration) *doorIdleWatch {
	w := &doorIdleWatch{timeout: timeout, fired: make(chan struct{})}
	w.timer = time.AfterFunc(timeout, func() { w.once.Do(func() { close(w.fired) }) })
	return w
}

// touch restarts the countdown after input from the caller. Input that
// arrives once the watch has fired does not revive it: the door is already
// being ended.
func (w *doorIdleWatch) touch() {
	if !w.hasFired() {
		w.timer.Reset(w.timeout)
	}
}

// stop ends the countdown. It is safe on a nil watch.
func (w *doorIdleWatch) stop() {
	if w != nil {
		w.timer.Stop()
	}
}

// Fired is closed when the caller has been idle for the timeout. A nil watch,
// for a caller with no idle timeout, returns a nil channel, which never fires.
func (w *doorIdleWatch) Fired() <-chan struct{} {
	if w == nil {
		return nil
	}
	return w.fired
}

// hasFired reports whether the caller has gone idle. False on a nil watch.
func (w *doorIdleWatch) hasFired() bool {
	if w == nil {
		return false
	}
	select {
	case <-w.fired:
		return true
	default:
		return false
	}
}

// idleTrackingSession is a session whose reads restart an idle countdown.
type idleTrackingSession struct {
	ssh.Session
	watch *doorIdleWatch
}

func (s *idleTrackingSession) Read(p []byte) (int, error) {
	n, err := s.Session.Read(p)
	if n > 0 {
		s.watch.touch()
	}
	return n, err
}

// Unwrap returns the session underneath, for code that checks its type.
func (s *idleTrackingSession) Unwrap() ssh.Session { return s.Session }

// idleTrackingInterruptSession is an idleTrackingSession over a session that
// supports SetReadInterrupt. It is a separate type so the wrapper claims that
// support only when the session underneath has it: the door executors wait
// for their input goroutine to finish only when a read can be interrupted.
type idleTrackingInterruptSession struct {
	*idleTrackingSession
}

func (s idleTrackingInterruptSession) SetReadInterrupt(ch <-chan struct{}) {
	s.Session.(readInterrupter).SetReadInterrupt(ch)
}

// wrapDoorSession returns s with every read from the caller restarting w.
func wrapDoorSession(s ssh.Session, w *doorIdleWatch) ssh.Session {
	base := &idleTrackingSession{Session: s, watch: w}
	if _, ok := s.(readInterrupter); ok {
		return idleTrackingInterruptSession{base}
	}
	return base
}

// unwrapSession returns the session underneath any door wrapper.
func unwrapSession(s ssh.Session) ssh.Session {
	for {
		u, ok := s.(interface{ Unwrap() ssh.Session })
		if !ok {
			return s
		}
		s = u.Unwrap()
	}
}

// watchDoorIdle hangs up on the door process p if the caller goes idle while
// it runs. Call it once p has started; the returned stop must be called once
// the process has been waited for. It does nothing for a caller with no idle
// timeout.
func watchDoorIdle(ctx *DoorCtx, p *os.Process) (stop func()) {
	if ctx.idle == nil || p == nil {
		return func() {}
	}
	exited := make(chan struct{})
	grace := doorHangupGrace
	go func() {
		select {
		case <-exited:
			return
		case <-ctx.idle.Fired():
		}
		slog.Info("caller idle in door, hanging up",
			"node", ctx.NodeNumber, "door", ctx.DoorName, "timeout", ctx.IdleTimeout)
		hangUpDoorProcess(p, exited, grace)
	}()
	var once sync.Once
	return func() { once.Do(func() { close(exited) }) }
}
