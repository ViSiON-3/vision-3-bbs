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
//
// Session embeds a nil ssh.Session only to satisfy the rest of the interface;
// Read, Write and Pty are implemented here, but any other promoted method
// (Environ, Command, Exit, ...) panics on the nil receiver if called.
type Session struct {
	ssh.Session // nil; supplies the rest of the interface

	term *Term

	mu        sync.Mutex
	data      []byte
	interrupt <-chan struct{}
	sent      chan struct{} // signaled (non-blocking) by Send to wake a pending Read
}

// NewSession creates a session that replays keys and renders output to term.
// term may be nil, in which case output is discarded.
func NewSession(term *Term, keys string) *Session {
	return &Session{term: term, data: []byte(keys), sent: make(chan struct{}, 1)}
}

// Send appends more input, for tests that feed keys in stages. It wakes a
// Read that is already blocked waiting for the interrupt, so keys fed after
// Read has been called are delivered instead of sitting unseen until the
// interrupt fires.
func (s *Session) Send(keys string) {
	s.mu.Lock()
	s.data = append(s.data, keys...)
	s.mu.Unlock()

	select {
	case s.sent <- struct{}{}:
	default:
		// A signal is already pending; Read will see the new data when it
		// re-checks the buffer, so there is nothing more to do here.
	}
}

// Pty reports that the session has no PTY. Session does not model PTY
// negotiation; RunEditorWithMetadata and similar callers treat a false isPty
// as "use default dimensions", which is what tests driving a Session want.
func (s *Session) Pty() (ssh.Pty, <-chan ssh.Window, bool) {
	return ssh.Pty{}, nil, false
}

// SetReadInterrupt installs the channel that unblocks a pending Read.
func (s *Session) SetReadInterrupt(ch <-chan struct{}) {
	s.mu.Lock()
	s.interrupt = ch
	s.mu.Unlock()
}

// Read replays scripted input. See the type comment for exhausted-input
// behaviour.
//
// When input is exhausted and an interrupt is set, Read also watches for a
// Send: Send is how a test feeds keys in stages to code running concurrently
// on another goroutine, which only makes sense if a Read blocked waiting for
// those keys actually wakes up when they arrive. Send's non-blocking signal
// on s.sent is buffered (capacity 1), so a Send landing between the buffer
// check below and the select is not lost — it is sitting in the channel
// ready for the select to consume immediately.
func (s *Session) Read(p []byte) (int, error) {
	for {
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

		select {
		case <-s.sent:
			continue // new input may have arrived; re-check the buffer
		case <-interrupt:
			return 0, errors.New("read interrupted")
		}
	}
}

// Write renders output to the session's Term, or discards it when there is none.
func (s *Session) Write(p []byte) (int, error) {
	if s.term == nil {
		return len(p), nil
	}
	return s.term.Write(p)
}
