package main

import (
	"bytes"
	"log/slog"
	"net"
	"strings"
	"sync"
	"testing"

	"github.com/gliderlabs/ssh"

	"github.com/ViSiON-3/vision-3-bbs/internal/session"
)

// panicTestSession is the least of an ssh.Session that sessionHandler reads
// before its first screen.
type panicTestSession struct {
	ssh.Session // nil; only the methods below are used
	addr        net.Addr
	closed      bool
}

func (s *panicTestSession) RemoteAddr() net.Addr { return s.addr }
func (s *panicTestSession) Context() ssh.Context { return nil }
func (s *panicTestSession) User() string         { return "caller" }
func (s *panicTestSession) Environ() []string    { return nil }
func (s *panicTestSession) Command() []string    { return nil }
func (s *panicTestSession) Close() error         { s.closed = true; return nil }

// lockedLog is a log destination safe for concurrent writes.
type lockedLog struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedLog) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

// A panic inside one caller's session ends that session only: the panic is
// logged with its node and stack, the node is released, and the next caller
// is given it and served.
func TestSessionPanicEndsOnlyThatSession(t *testing.T) {
	oldTracker, oldReg, oldHook, oldLog := connectionTracker, sessionRegistry, sessionSetUp, slog.Default()
	connectionTracker = NewConnectionTracker(1, 0, 0, 0, "", "") // one node
	sessionRegistry = session.NewSessionRegistry()
	logs := &lockedLog{}
	slog.SetDefault(slog.New(slog.NewTextHandler(logs, nil)))
	t.Cleanup(func() {
		connectionTracker, sessionRegistry, sessionSetUp = oldTracker, oldReg, oldHook
		slog.SetDefault(oldLog)
	})

	var nodes []int32
	sessionSetUp = func(s ssh.Session, node int32) {
		nodes = append(nodes, node)
		sessionRegistry.Register(&session.BbsSession{NodeID: int(node)})
		if len(nodes) == 1 {
			panic("first caller broke it")
		}
		// The second caller is served: it has the node, and its session is
		// registered, until it hangs up.
		if sessionRegistry.Get(int(node)) == nil {
			t.Error("second caller's session is not registered")
		}
		panic("the test ends the second session here")
	}

	first := &panicTestSession{addr: &net.TCPAddr{IP: net.IPv4(10, 0, 0, 1), Port: 1}}
	second := &panicTestSession{addr: &net.TCPAddr{IP: net.IPv4(10, 0, 0, 2), Port: 2}}
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("the panic left sessionHandler: %v", r)
			}
		}()
		sessionHandler(first)
		sessionHandler(second)
	}()

	if len(nodes) != 2 || nodes[0] != 1 || nodes[1] != 1 {
		t.Errorf("nodes = %v, want the one node given to both callers", nodes)
	}
	if !first.closed || !second.closed {
		t.Errorf("sessions closed: first=%v second=%v", first.closed, second.closed)
	}
	activeSessionsMutex.Lock()
	_, firstHeld := activeSessions[first]
	_, secondHeld := activeSessions[second]
	activeSessionsMutex.Unlock()
	if firstHeld || secondHeld {
		t.Errorf("a node is still held: first=%v second=%v", firstHeld, secondHeld)
	}
	if sessionRegistry.Get(1) != nil {
		t.Error("node 1 is still registered")
	}
	log := logs.String()
	if !strings.Contains(log, "first caller broke it") || !strings.Contains(log, "node=1") || !strings.Contains(log, "session_recover_test.go") {
		t.Errorf("panic not logged with node and stack:\n%s", log)
	}
}

// A session that panics before its cleanup is in place, here in reading its
// own address, still gives its node back.
func TestSessionPanicBeforeCleanupFreesTheNode(t *testing.T) {
	oldTracker, oldReg, oldHook, oldLog := connectionTracker, sessionRegistry, sessionSetUp, slog.Default()
	connectionTracker = NewConnectionTracker(1, 0, 0, 0, "", "") // one node
	sessionRegistry = session.NewSessionRegistry()
	logs := &lockedLog{}
	slog.SetDefault(slog.New(slog.NewTextHandler(logs, nil)))
	t.Cleanup(func() {
		connectionTracker, sessionRegistry, sessionSetUp = oldTracker, oldReg, oldHook
		slog.SetDefault(oldLog)
	})
	var nodes []int32
	sessionSetUp = func(s ssh.Session, node int32) {
		nodes = append(nodes, node)
		panic("the test ends the session here")
	}

	early := &panicTestSession{} // no address: RemoteAddr().String() panics
	later := &panicTestSession{addr: &net.TCPAddr{IP: net.IPv4(10, 0, 0, 3), Port: 3}}
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("the panic left sessionHandler: %v", r)
			}
		}()
		sessionHandler(early)
		sessionHandler(later)
	}()

	if len(nodes) != 1 || nodes[0] != 1 {
		t.Errorf("nodes = %v, want node 1 given to the next caller", nodes)
	}
	if !early.closed {
		t.Error("the early session was not closed")
	}
	activeSessionsMutex.Lock()
	_, held := activeSessions[early]
	activeSessionsMutex.Unlock()
	if held {
		t.Error("the early session still holds its node")
	}
	if log := logs.String(); !strings.Contains(log, "node=1") || !strings.Contains(log, "addr=unknown") {
		t.Errorf("early panic not logged with its node:\n%s", log)
	}
}

// A cleanup step that panics is logged and the steps after it run.
func TestGuardCleanupGoesOn(t *testing.T) {
	oldLog := slog.Default()
	logs := &lockedLog{}
	slog.SetDefault(slog.New(slog.NewTextHandler(logs, nil)))
	t.Cleanup(func() { slog.SetDefault(oldLog) })

	after := false
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("guardCleanup let the panic through: %v", r)
			}
		}()
		guardCleanup("announcement", 4, func() { panic("announce broke") })
		after = true
	}()
	if !after {
		t.Error("the step after the panic did not run")
	}
	if log := logs.String(); !strings.Contains(log, "announce broke") || !strings.Contains(log, "node=4") || !strings.Contains(log, "session_recover_test.go") {
		t.Errorf("panic not logged with node and stack:\n%s", log)
	}
}
