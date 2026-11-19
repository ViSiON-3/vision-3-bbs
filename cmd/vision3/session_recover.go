package main

import (
	"fmt"
	"log/slog"
	"runtime/debug"

	"github.com/gliderlabs/ssh"

	"github.com/ViSiON-3/vision-3-bbs/internal/safego"
)

// Goroutines a session starts log the node they serve (safego.Go).
func init() { safego.SetNodeOf(nodeOfOwner) }

// nodeOfOwner is the node held by owner, a caller's session or a wrapper
// around it (a door's, a script's), for safego's log lines.
func nodeOfOwner(owner any) (int, bool) {
	s, ok := owner.(ssh.Session)
	if !ok || s == nil {
		return 0, false
	}
	activeSessionsMutex.Lock()
	defer activeSessionsMutex.Unlock()
	for {
		if n, ok := activeSessions[s]; ok {
			return int(n), true
		}
		u, ok := s.(interface{ Unwrap() ssh.Session })
		if !ok {
			return 0, false
		}
		s = u.Unwrap()
	}
}

// recoverSession, deferred first in sessionHandler so it runs last, ends a
// session whose goroutine panicked without ending the board: the panic and
// its stack are logged with the node. The session's own deferred cleanup
// frees the node and closes the session, but a panic before that cleanup is
// in place skips it, so both are done here again; neither does harm twice.
// Every caller's session, SSH or telnet, goes through sessionHandler. node is
// read when the panic is recovered, so it is the node allocated by then.
func recoverSession(s ssh.Session, node *int32) {
	r := recover()
	if r == nil {
		return
	}
	addr := "unknown"
	func() {
		defer func() { _ = recover() }() // a broken session may not even say where it is
		if a := s.RemoteAddr(); a != nil {
			addr = a.String()
		}
	}()
	slog.Error("session panicked; it is ended, the board goes on",
		"node", *node, "addr", addr, "panic", fmt.Sprint(r), "stack", string(debug.Stack()))
	activeSessionsMutex.Lock()
	delete(activeSessions, s)
	activeSessionsMutex.Unlock()
	func() {
		defer func() { _ = recover() }()
		_ = s.Close() // best-effort; the cleanup may have closed it already
	}()
}

// guardCleanup runs fn, one step of a session's cleanup, so that a panic in
// it is logged with its stack and the steps after it still run.
func guardCleanup(what string, node int32, fn func()) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("session cleanup step panicked; the rest of the cleanup goes on",
				"step", what, "node", node, "panic", fmt.Sprint(r), "stack", string(debug.Stack()))
		}
	}()
	fn()
}

// sessionSetUp runs once a session has its node and its cleanup is in place.
// It does nothing; tests set it to panic inside a session.
var sessionSetUp = func(s ssh.Session, node int32) {}
