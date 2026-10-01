package menu

import (
	"context"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/gliderlabs/ssh"

	"github.com/ViSiON-3/vision-3-bbs/internal/editor"
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

	mu     sync.Mutex
	frozen bool // the countdown has stopped for good
}

// newDoorIdleWatch starts a countdown of timeout.
func newDoorIdleWatch(timeout time.Duration) *doorIdleWatch {
	w := &doorIdleWatch{timeout: timeout, fired: make(chan struct{})}
	w.timer = time.AfterFunc(timeout, w.fire)
	return w
}

// fire marks the caller idle, unless the countdown was frozen first.
func (w *doorIdleWatch) fire() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.frozen {
		return
	}
	w.frozen = true
	close(w.fired)
}

// touch restarts the countdown after input from the caller. Input that
// arrives once the watch has fired or been frozen does not revive it.
func (w *doorIdleWatch) touch() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.frozen {
		w.timer.Reset(w.timeout)
	}
}

// freeze stops the countdown for good, keeping whether it had fired. The
// door executors call it as soon as the door itself has ended, so time
// spent afterwards, such as running a cleanup command, is never taken for
// the caller being idle. It is safe on a nil watch.
func (w *doorIdleWatch) freeze() {
	if w == nil {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.frozen = true
	w.timer.Stop()
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

// runDoorWithIdleTimeout runs a door with run, enforcing the caller's idle
// timeout for as long as it takes. It returns editor.ErrIdleTimeout if the
// caller went idle, whatever run returned.
func runDoorWithIdleTimeout(ctx *DoorCtx, run func(*DoorCtx) error) error {
	if ctx.IdleTimeout <= 0 {
		return run(ctx)
	}
	ctx.idle = newDoorIdleWatch(ctx.IdleTimeout)
	defer ctx.idle.freeze()
	ctx.Session = wrapDoorSession(ctx.Session, ctx.idle)

	err := run(ctx)
	if ctx.idle.hasFired() {
		slog.Info("door ended: caller idle", "node", ctx.NodeNumber, "door", ctx.DoorName, "doorError", err)
		return editor.ErrIdleTimeout
	}
	return err
}

// idleDialContext returns a context that is cancelled if the caller goes
// idle, for connecting to a remote door server: a connect timeout can be
// longer than the idle timeout. cancel must be called once connected.
func idleDialContext(ctx *DoorCtx) (context.Context, context.CancelFunc) {
	dialCtx, cancel := context.WithCancel(context.Background())
	go func() {
		select {
		case <-ctx.idle.Fired():
			cancel()
		case <-dialCtx.Done():
		}
	}()
	return dialCtx, cancel
}

// watchDoorIdle hangs up on the door process p if the caller goes idle while
// it runs. Call it once p has started; the returned stop must be called once
// the process has been waited for, and freezes the idle countdown. It does
// nothing for a caller with no idle timeout.
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
	return func() {
		once.Do(func() {
			ctx.idle.freeze()
			close(exited)
		})
	}
}
