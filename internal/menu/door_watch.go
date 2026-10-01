package menu

import (
	"context"
	"io"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/gliderlabs/ssh"

	"github.com/ViSiON-3/vision-3-bbs/internal/editor"
	"github.com/ViSiON-3/vision-3-bbs/internal/timeleft"
)

// A door reads the caller's session directly, with the InputHandler that
// enforces the idle timeout and time limit in the menus stopped, and a native
// or DOS door is cut off from the session by the PTY, socket or pipe the BBS
// bridges it through. So executeDoor watches the session itself (doorWatch)
// and ends the door when:
//
//   - the caller sends nothing for their idle timeout;
//   - the caller's time limit runs out;
//   - the caller disconnects.
//
// It then reports why, as editor.ErrIdleTimeout, editor.ErrTimeLimit or
// io.EOF, which the menu loop turns into the matching notice and a logoff.
// CoSysOps and above have neither an idle timeout nor a time limit, here as
// in the menus.

// doorHangupGrace is how long a door has to exit after it is hung up on
// before it is killed. A variable so tests can shorten it.
var doorHangupGrace = 5 * time.Second

// readInterrupter is a session whose blocked Read can be cancelled.
type readInterrupter interface {
	SetReadInterrupt(<-chan struct{})
}

// doorEndReason is why a door was ended by the BBS rather than by itself.
type doorEndReason int

const (
	doorEndIdle doorEndReason = iota + 1
	doorEndTimeLimit
	doorEndDisconnect
)

func (r doorEndReason) String() string {
	switch r {
	case doorEndIdle:
		return "caller idle"
	case doorEndTimeLimit:
		return "time limit reached"
	case doorEndDisconnect:
		return "caller disconnected"
	}
	return "none"
}

// err is the error executeDoor returns for r, which the menu loop shows and
// logs the caller off for.
func (r doorEndReason) err() error {
	switch r {
	case doorEndIdle:
		return editor.ErrIdleTimeout
	case doorEndTimeLimit:
		return editor.ErrTimeLimit
	}
	return io.EOF
}

// doorWatch decides when the BBS must end a door: see the comment above.
type doorWatch struct {
	idleTimeout time.Duration
	idleTimer   *time.Timer // nil with no idle timeout
	limitTimer  *time.Timer // nil with no time limit
	ended       chan struct{}

	mu     sync.Mutex
	frozen bool          // the watch has stopped for good
	reason doorEndReason // why the door was ended, once ended is closed
}

// newDoorWatch starts watching. idleTimeout 0 means no idle timeout, and a
// zero deadline means no time limit.
func newDoorWatch(idleTimeout time.Duration, deadline time.Time) *doorWatch {
	w := &doorWatch{idleTimeout: idleTimeout, ended: make(chan struct{})}
	// A timer can fire before AfterFunc returns (a deadline already past),
	// and end reads the timer fields, so they are set under the lock.
	w.mu.Lock()
	defer w.mu.Unlock()
	if idleTimeout > 0 {
		w.idleTimer = time.AfterFunc(idleTimeout, func() { w.end(doorEndIdle) })
	}
	if !deadline.IsZero() {
		w.limitTimer = time.AfterFunc(time.Until(deadline), func() { w.end(doorEndTimeLimit) })
	}
	return w
}

// end ends the door for reason, unless the watch has already ended or been
// frozen. It is safe on a nil watch.
func (w *doorWatch) end(reason doorEndReason) {
	if w == nil {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.frozen {
		return
	}
	w.frozen = true
	w.reason = reason
	w.stopTimersLocked()
	close(w.ended)
}

func (w *doorWatch) stopTimersLocked() {
	if w.idleTimer != nil {
		w.idleTimer.Stop()
	}
	if w.limitTimer != nil {
		w.limitTimer.Stop()
	}
}

// touch restarts the idle countdown after input from the caller. Input that
// arrives once the watch has ended or been frozen does not revive it.
func (w *doorWatch) touch() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.frozen && w.idleTimer != nil {
		w.idleTimer.Reset(w.idleTimeout)
	}
}

// freeze stops the watch for good, keeping whether and why it had ended the
// door. The door executors call it as soon as the door itself has ended, so
// time spent afterwards, such as running a cleanup command, never counts
// against the caller, and the read interrupt that stops the input goroutine
// is never taken for a disconnect. It is safe on a nil watch.
func (w *doorWatch) freeze() {
	if w == nil {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.frozen = true
	w.stopTimersLocked()
}

// Ended is closed when the BBS must end the door. A nil watch returns a nil
// channel, which never fires.
func (w *doorWatch) Ended() <-chan struct{} {
	if w == nil {
		return nil
	}
	return w.ended
}

// endReason returns why the door was ended, or 0 if it wasn't. 0 on a nil
// watch.
func (w *doorWatch) endReason() doorEndReason {
	if w == nil {
		return 0
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.reason
}

// watchedSession is a session whose reads feed a doorWatch: input restarts
// the idle countdown, and a failed read means the caller has gone.
type watchedSession struct {
	ssh.Session
	watch *doorWatch

	mu        sync.Mutex
	interrupt <-chan struct{} // the read interrupt the door set, if any
}

func (s *watchedSession) Read(p []byte) (int, error) {
	n, err := s.Session.Read(p)
	if n > 0 {
		s.watch.touch()
	}
	if err != nil && !s.interrupted() {
		s.watch.end(doorEndDisconnect)
	}
	return n, err
}

// interrupted reports whether the door has fired its read interrupt. A
// read that fails then was cancelled by the door, which on telnet looks
// exactly like end of file, rather than ended by the caller hanging up.
func (s *watchedSession) interrupted() bool {
	s.mu.Lock()
	ch := s.interrupt
	s.mu.Unlock()
	if ch == nil {
		return false
	}
	select {
	case <-ch:
		return true
	default:
		return false
	}
}

// Unwrap returns the session underneath, for code that checks its type.
func (s *watchedSession) Unwrap() ssh.Session { return s.Session }

// watchedInterruptSession is a watchedSession over a session that supports
// SetReadInterrupt. It is a separate type so the wrapper claims that support
// only when the session underneath has it: the door executors wait for their
// input goroutine to finish only when a read can be interrupted.
type watchedInterruptSession struct {
	*watchedSession
}

// SetReadInterrupt passes ch on, and remembers it so a read it cancels isn't
// taken for a disconnect. Clearing the interrupt (nil), as executors do on
// their way out, keeps the last one: the input goroutine may still be
// returning from the read it cancelled.
func (s watchedInterruptSession) SetReadInterrupt(ch <-chan struct{}) {
	if ch != nil {
		s.mu.Lock()
		s.interrupt = ch
		s.mu.Unlock()
	}
	s.Session.(readInterrupter).SetReadInterrupt(ch)
}

// wrapDoorSession returns s with every read feeding w.
func wrapDoorSession(s ssh.Session, w *doorWatch) ssh.Session {
	base := &watchedSession{Session: s, watch: w}
	if _, ok := s.(readInterrupter); ok {
		return watchedInterruptSession{base}
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

// doorTimeLimitDeadline returns when the caller's time runs out, or the zero
// time with no limit, pushed out by the time the caller spent in sysop chat
// as the menus' own deadline is. buildDoorCtx has already cleared the limit
// for CoSysOps and above.
func doorTimeLimitDeadline(ctx *DoorCtx) time.Time {
	now := time.Now()
	left, limited := timeleft.Remaining(ctx.User.TimeLimit, ctx.SessionStartTime, now, chatCredit(ctx.Session))
	if !limited {
		return time.Time{}
	}
	return now.Add(left)
}

// runDoorWatched runs a door with run under a doorWatch. If the BBS ended the
// door, it returns the reason's error (see doorEndReason.err), whatever run
// returned. A caller whose time has already run out isn't let in.
func runDoorWatched(ctx *DoorCtx, run func(*DoorCtx) error) error {
	deadline := doorTimeLimitDeadline(ctx)
	if !deadline.IsZero() && !time.Now().Before(deadline) {
		slog.Info("door not started: time limit reached", "node", ctx.NodeNumber, "door", ctx.DoorName)
		return editor.ErrTimeLimit
	}
	ctx.watch = newDoorWatch(ctx.IdleTimeout, deadline)
	defer ctx.watch.freeze()
	ctx.Session = wrapDoorSession(ctx.Session, ctx.watch)

	err := run(ctx)
	if reason := ctx.watch.endReason(); reason != 0 {
		slog.Info("door ended by the BBS", "node", ctx.NodeNumber, "door", ctx.DoorName,
			"reason", reason.String(), "doorError", err)
		return reason.err()
	}
	return err
}

// doorDialContext returns a context that is cancelled if the BBS must end
// the door, for connecting to a remote door server: a connect timeout can
// outlast the caller's idle timeout or time limit. cancel must be called
// once connected.
func doorDialContext(ctx *DoorCtx) (context.Context, context.CancelFunc) {
	dialCtx, cancel := context.WithCancel(context.Background())
	go func() {
		select {
		case <-ctx.watch.Ended():
			cancel()
		case <-dialCtx.Done():
		}
	}()
	return dialCtx, cancel
}

// watchDoorProcess hangs up on the door process p if the BBS must end the
// door while it runs. Call it once p has started; the returned stop must be
// called once the process has been waited for, and freezes the watch.
func watchDoorProcess(ctx *DoorCtx, p *os.Process) (stop func()) {
	if ctx.watch == nil || p == nil {
		return func() {}
	}
	exited := make(chan struct{})
	grace := doorHangupGrace
	go func() {
		select {
		case <-exited:
			return
		case <-ctx.watch.Ended():
		}
		slog.Info("hanging up on door", "node", ctx.NodeNumber, "door", ctx.DoorName,
			"reason", ctx.watch.endReason().String())
		hangUpDoorProcess(p, exited, grace)
	}()
	var once sync.Once
	return func() {
		once.Do(func() {
			ctx.watch.freeze()
			close(exited)
		})
	}
}
