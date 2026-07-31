package testterm

import (
	"errors"
	"io"
	"sync"

	"github.com/gliderlabs/ssh"
)

// Session is a scripted ssh.Session for driving interactive terminal code.
// Read replays the keystrokes it was given; Write forwards to a Term so one
// object covers both directions of a test.
//
// Once the scripted input is exhausted, Read blocks until the read interrupt
// fires if one is set, and returns io.EOF otherwise. That mirrors the ssh and
// telnet adapters: a live connection does not report EOF just because the user
// has stopped typing, and code that relies on a read returning is what leaks
// goroutines when it is wrong.
type Session struct {
	ssh.Session // nil; supplies the rest of the interface

	term *Term

	mu        sync.Mutex
	data      []byte
	interrupt <-chan struct{}
}

// NewSession creates a session that replays keys and renders output to term.
// term may be nil, in which case output is discarded.
func NewSession(term *Term, keys string) *Session {
	return &Session{term: term, data: []byte(keys)}
}

// Send appends more input, for tests that feed keys in stages.
func (s *Session) Send(keys string) {
	s.mu.Lock()
	s.data = append(s.data, keys...)
	s.mu.Unlock()
}

// SetReadInterrupt installs the channel that unblocks a pending Read.
func (s *Session) SetReadInterrupt(ch <-chan struct{}) {
	s.mu.Lock()
	s.interrupt = ch
	s.mu.Unlock()
}

// Read replays scripted input. See the type comment for exhausted-input
// behaviour.
func (s *Session) Read(p []byte) (int, error) {
	s.mu.Lock()
	if len(s.data) > 0 {
		n := copy(p, s.data)
		s.data = s.data[n:]
		s.mu.Unlock()
		return n, nil
	}
	interrupt := s.interrupt
	s.mu.Unlock()

	if interrupt == nil {
		return 0, io.EOF
	}
	<-interrupt
	return 0, errors.New("read interrupted")
}

// Write renders output to the session's Term, or discards it when there is none.
func (s *Session) Write(p []byte) (int, error) {
	if s.term == nil {
		return len(p), nil
	}
	return s.term.Write(p)
}
