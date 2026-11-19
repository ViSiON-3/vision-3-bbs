package safego

import (
	"bytes"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"
)

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

func captureLog(t *testing.T) *lockedLog {
	t.Helper()
	l := &lockedLog{}
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(l, nil)))
	t.Cleanup(func() { slog.SetDefault(old) })
	return l
}

func wait(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for %s", what)
	}
}

func TestGoRecoversLogsAndRunsOnPanic(t *testing.T) {
	logs := captureLog(t)
	type sess struct{}
	owner := &sess{}
	SetNodeOf(func(o any) (int, bool) { return 7, o == owner })
	t.Cleanup(func() { SetNodeOf(func(any) (int, bool) { return 0, false }) })

	deferred, handled := make(chan struct{}), make(chan struct{})
	Go(KindDoorIO, owner, func() {
		defer close(deferred)
		panic("door relay broke")
	}, func() { close(handled) })
	wait(t, deferred, "fn's own deferred call")
	wait(t, handled, "onPanic")
	log := logs.String()
	for _, want := range []string{"door relay broke", "goroutine=\"door I/O\"", "node=7", "safego_test.go"} {
		if !strings.Contains(log, want) {
			t.Errorf("log lacks %q:\n%s", want, log)
		}
	}
}

func TestGoNodeNumberOwnerAndPanickingOnPanic(t *testing.T) {
	logs := captureLog(t)
	done := make(chan struct{})
	Go(KindTransfer, 3, func() { panic("first") }, func() {
		defer close(done)
		panic("second")
	})
	wait(t, done, "onPanic")
	// The second line is logged once onPanic has unwound.
	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(logs.String(), "second") {
		if time.Now().After(deadline) {
			t.Fatalf("onPanic's panic was not logged:\n%s", logs.String())
		}
		time.Sleep(time.Millisecond)
	}
	if log := logs.String(); !strings.Contains(log, "node=3") {
		t.Errorf("log:\n%s", log)
	}
}

func TestInjectPanicHitsTheNextTestPointOfItsKindOnce(t *testing.T) {
	captureLog(t)
	restore := InjectPanic(KindChat)
	defer restore()
	TestPoint(KindInput) // another kind: nothing
	var runs []bool
	for i := 0; i < 2; i++ {
		// Each run reports once: true when fn got past its test point,
		// false from onPanic.
		ran := make(chan bool, 1)
		Go(KindChat, 1, func() {
			TestPoint(KindChat)
			ran <- true
		}, func() { ran <- false })
		select {
		case r := <-ran:
			runs = append(runs, r)
		case <-time.After(5 * time.Second):
			t.Fatal("a run reported nothing")
		}
	}
	if runs[0] || !runs[1] {
		t.Errorf("runs = %v; want the first to panic and the second to run", runs)
	}
}

// Taking back one request leaves a later one for the same kind armed, and a
// used request's restore takes back nothing.
func TestInjectPanicRestoreTakesBackOnlyItsOwnRequest(t *testing.T) {
	restoreA := InjectPanic(KindMailPoll)
	func() {
		defer func() { _ = recover() }()
		TestPoint(KindMailPoll) // uses A
	}()
	restoreB := InjectPanic(KindMailPoll)
	defer restoreB()
	restoreA() // A was used: B must stay
	panicked := func() (p bool) {
		defer func() { p = recover() != nil }()
		TestPoint(KindMailPoll)
		return false
	}()
	if !panicked {
		t.Error("restoring a used request took back a later one")
	}

	restoreC := InjectPanic(KindMailPoll)
	restoreC()
	TestPoint(KindMailPoll) // nothing armed: must not panic
}
