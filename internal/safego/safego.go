// Package safego starts the goroutines that serve one caller's session, so
// that a panic on one of them is logged and ends at most that caller's
// session instead of the whole process.
//
// A Go panic on any goroutine ends the process unless that goroutine
// recovers it. The session's own goroutine recovers (see sessionHandler in
// cmd/vision3), but the goroutines a session starts for itself, to read
// input, relay a door, run a transfer or chat, do not inherit that. Go starts
// them with a recover in place. A test (TestNoBareGoOnSessionPaths) keeps new
// code on the session paths from starting one any other way.
package safego

import (
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"
	"sync"
	"sync/atomic"
)

// The kinds of goroutine a session starts, as Go is told them. They name the
// goroutine in the log, and tests use them to make one of a kind panic.
const (
	KindResize      = "window-change watcher"
	KindInput       = "input reader"
	KindTerminal    = "terminal I/O"
	KindChat        = "teleconference"
	KindSysopChat   = "sysop chat"
	KindLoginStep   = "login step"
	KindMailPoll    = "mail poll"
	KindV3NetOnline = "V3Net online count"
	KindNotice      = "menu notice watcher"
	KindDoorIO      = "door I/O"
	KindDoorWatch   = "door watch"
	KindScriptInput = "script input"
	KindScriptWatch = "script watch"
	KindTransfer    = "file transfer"
	KindWFC         = "WFC session"
	KindBlocklist   = "blocklist write"
)

// ErrPanicked is what a goroutine that panicked hands over in place of its
// result, where something waits for one.
var ErrPanicked = errors.New("goroutine panicked")

// nodeOf finds the node an owner serves; see SetNodeOf.
var nodeOf atomic.Pointer[func(owner any) (int, bool)]

// SetNodeOf sets how Go finds the node of an owner that is not a node
// number itself: the BBS maps a caller's session, or a wrapper around it, to
// the node it holds.
func SetNodeOf(f func(owner any) (int, bool)) { nodeOf.Store(&f) }

// node is the node owner serves, or 0 when that is not known.
func node(owner any) int {
	if n, ok := owner.(int); ok {
		return n
	}
	if f := nodeOf.Load(); f != nil && owner != nil {
		if n, ok := (*f)(owner); ok {
			return n
		}
	}
	return 0
}

// Go runs fn on a new goroutine started for one caller's session. kind says
// what it is (one of the Kind constants). owner identifies the session: its
// node number, or the session value the goroutine serves, which SetNodeOf's
// function maps to a node.
//
// A panic in fn is recovered once fn's own deferred calls have run. It is
// logged with kind, node and stack, and then onPanic, if not nil, runs on the
// same goroutine to end or degrade that caller's session: hand a waiter an
// error, close a pipe, end a door. A panic in onPanic is logged too.
func Go(kind string, owner any, fn func(), onPanic func()) {
	go Run(kind, owner, fn, onPanic)
}

// Run is Go without the new goroutine, for a goroutine some code other than
// Go starts on a session's behalf (a timer's func, say).
func Run(kind string, owner any, fn func(), onPanic func()) {
	defer func() {
		r := recover()
		if r == nil {
			return
		}
		slog.Error("session goroutine panicked; only its session is affected",
			"goroutine", kind, "node", node(owner), "panic", fmt.Sprint(r), "stack", string(debug.Stack()))
		if onPanic == nil {
			return
		}
		defer func() {
			if r := recover(); r != nil {
				slog.Error("session goroutine's panic handling panicked too",
					"goroutine", kind, "node", node(owner), "panic", fmt.Sprint(r), "stack", string(debug.Stack()))
			}
		}()
		onPanic()
	}()
	fn()
}

// armed holds, per kind, the panics InjectPanic has asked for and not yet
// had, oldest first; armedAny is how many there are in all, so TestPoint
// costs one atomic load when none is asked for.
var (
	armedMu  sync.Mutex
	armed    map[string][]*panicRequest
	armedAny atomic.Int32
)

// panicRequest is one InjectPanic call.
type panicRequest struct{ _ byte } // not zero-size: each request has its own address

// InjectPanic makes the next TestPoint of kind panic, as a test of what that
// kind of goroutine does with a panic. restore takes this request back if no
// TestPoint has used it; it leaves any other request alone.
func InjectPanic(kind string) (restore func()) {
	req := &panicRequest{}
	armedMu.Lock()
	if armed == nil {
		armed = map[string][]*panicRequest{}
	}
	armed[kind] = append(armed[kind], req)
	armedAny.Add(1)
	armedMu.Unlock()
	return func() {
		armedMu.Lock()
		defer armedMu.Unlock()
		for i, r := range armed[kind] {
			if r == req {
				armed[kind] = append(armed[kind][:i:i], armed[kind][i+1:]...)
				armedAny.Add(-1)
				return
			}
		}
	}
}

// TestPoint panics if InjectPanic asked for a panic in kind. A goroutine
// started with Go calls it once its own deferred calls are in place, where a
// real panic in its work would come from.
func TestPoint(kind string) {
	if armedAny.Load() == 0 {
		return
	}
	armedMu.Lock()
	hit := len(armed[kind]) > 0
	if hit {
		armed[kind] = armed[kind][1:]
		armedAny.Add(-1)
	}
	armedMu.Unlock()
	if hit {
		panic("injected panic in " + kind)
	}
}
