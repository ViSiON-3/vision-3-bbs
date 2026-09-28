package sshserver

import (
	"errors"
	"io"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gliderlabs/ssh"
)

type blockedReadSession struct {
	ssh.Session
	started chan struct{}
	replies chan readResult
	calls   atomic.Int32
}

func (s *blockedReadSession) Read(p []byte) (int, error) {
	s.calls.Add(1)
	s.started <- struct{}{}
	r := <-s.replies
	return copy(p, r.data), r.err
}
func readAsync(s *BBSSession, size int) <-chan readResult {
	out := make(chan readResult, 1)
	go func() { b := make([]byte, size); n, err := s.Read(b); out <- readResult{data: b[:n], err: err} }()
	return out
}
func awaitRead(t *testing.T, ch <-chan readResult) readResult {
	t.Helper()
	select {
	case r := <-ch:
		return r
	case <-time.After(time.Second):
		t.Fatal("read did not complete")
		return readResult{}
	}
}
func TestReadInterruptHandoff(t *testing.T) {
	raw := &blockedReadSession{started: make(chan struct{}, 4), replies: make(chan readResult, 4)}
	// Release a blocked underlying reader even if an assertion fails.
	defer close(raw.replies)
	s := &BBSSession{Session: raw}
	first := readAsync(s, 32)
	<-raw.started
	interrupt := make(chan struct{})
	close(interrupt)
	// Transfers install the interrupt only after the external process exits.
	s.SetReadInterrupt(interrupt)
	if r := awaitRead(t, first); !errors.Is(r.err, ErrReadInterrupted) {
		t.Fatalf("first read: %+v", r)
	}

	// Post-transfer draining must also time out while reusing that read.
	drainInterrupt := make(chan struct{})
	s.SetReadInterrupt(drainInterrupt)
	drain := readAsync(s, 32)
	close(drainInterrupt)
	if r := awaitRead(t, drain); !errors.Is(r.err, ErrReadInterrupted) {
		t.Fatalf("drain: %+v", r)
	}
	s.SetReadInterrupt(nil)
	menu := readAsync(s, 1)
	raw.replies <- readResult{data: []byte("\rNEXT"), err: io.EOF}
	if r := awaitRead(t, menu); string(r.data) != "\r" || r.err != nil {
		t.Fatalf("first Enter lost: %+v", r)
	}
	if r := awaitRead(t, readAsync(s, 10)); string(r.data) != "NEXT" || r.err != io.EOF {
		t.Fatalf("pending data/EOF lost: %+v", r)
	}
	if n := raw.calls.Load(); n != 1 {
		t.Fatalf("underlying readers = %d, want one", n)
	}
}

func TestReadUsesReplacementInterrupt(t *testing.T) {
	raw := &blockedReadSession{started: make(chan struct{}, 4), replies: make(chan readResult, 4)}
	defer close(raw.replies)
	s := &BBSSession{Session: raw}
	s.SetReadInterrupt(make(chan struct{}))
	result := readAsync(s, 32)
	<-raw.started
	replacement := make(chan struct{})
	s.SetReadInterrupt(replacement)
	close(replacement)
	if r := awaitRead(t, result); !errors.Is(r.err, ErrReadInterrupted) {
		t.Fatalf("read: %+v", r)
	}
}
