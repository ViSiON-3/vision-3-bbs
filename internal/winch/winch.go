// Package winch distributes a session's window-size changes to every reader
// that wants them.
//
// ssh.Session.Pty returns one channel per session, on SSH and telnet alike, so
// only one reader can take each change. The session loop that resizes the
// terminal ranged over it for the whole connection, and the full-screen editor,
// a door and a file transfer each took from the same channel while they ran, so
// whichever reader won the receive got the change and the others missed it. The
// session loop now owns the channel through a Hub and the others subscribe.
package winch

import (
	"sync"

	"github.com/gliderlabs/ssh"
)

// Hub takes a session's window changes from the one channel its transport
// provides and hands each to every subscriber.
type Hub struct {
	key     ssh.Session
	done    chan struct{} // closed by Close: run stops taking from the source
	stopped chan struct{} // closed by run on its way out

	mu     sync.Mutex
	subs   map[chan ssh.Window]struct{}
	closed bool
}

// hubs holds the Hub of each session that has one, keyed by the session as
// its transport made it (see base).
var hubs sync.Map

// Own gives s a Hub that takes src's changes from now on, and returns the
// owner's subscription, which sees every change src delivers, including one
// already waiting in it. Call Close when the session ends: a transport that
// never closes src would otherwise keep the Hub's goroutine.
func Own(s ssh.Session, src <-chan ssh.Window) (*Hub, <-chan ssh.Window) {
	h := &Hub{key: base(s), done: make(chan struct{}), stopped: make(chan struct{}), subs: map[chan ssh.Window]struct{}{}}
	owner, _ := h.Subscribe()
	hubs.Store(h.key, h)
	go h.run(src)
	return h, owner
}

// run takes from src until it closes or Close is called. A change that was
// already waiting in src when Close was called may still be taken; it goes
// nowhere, since Close has emptied the subscribers.
func (h *Hub) run(src <-chan ssh.Window) {
	defer close(h.stopped)
	for {
		select {
		case win, ok := <-src:
			if !ok {
				h.Close()
				return
			}
			h.publish(win)
		case <-h.done:
			return
		}
	}
}

// publish hands win to every subscriber. A subscriber that has not taken the
// previous change loses it: only the final size matters.
func (h *Hub) publish(win ssh.Window) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.subs {
		select {
		case <-ch:
		default:
		}
		// This goroutine is the only sender, so the drain above left room.
		select {
		case ch <- win:
		default:
		}
	}
}

// Subscribe returns a channel of the changes that arrive from now on, holding
// the latest while its reader is busy, and a func that ends the subscription.
// The channel is closed when the Hub closes; on a Hub already closed it comes
// back closed.
func (h *Hub) Subscribe() (<-chan ssh.Window, func()) {
	ch := make(chan ssh.Window, 1)
	h.mu.Lock()
	if h.closed {
		close(ch)
	} else {
		h.subs[ch] = struct{}{}
	}
	h.mu.Unlock()
	return ch, func() {
		h.mu.Lock()
		delete(h.subs, ch)
		h.mu.Unlock()
	}
}

// Close ends every subscription by closing its channel, stops taking from the
// source and forgets the Hub, so Subscribe for the session falls back to Pty.
func (h *Hub) Close() {
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return
	}
	h.closed = true
	close(h.done)
	for ch := range h.subs {
		close(ch)
		delete(h.subs, ch)
	}
	h.mu.Unlock()
	hubs.CompareAndDelete(h.key, h)
}

// Subscribe is Hub.Subscribe on the Hub that owns s's window changes. With no
// Hub, as in tests and tools that run a screen without the session loop, it
// returns the channel s.Pty gives, which only one reader may take from, and a
// stop that does nothing.
func Subscribe(s ssh.Session) (<-chan ssh.Window, func()) {
	if v, ok := hubs.Load(base(s)); ok {
		return v.(*Hub).Subscribe()
	}
	_, ch, _ := s.Pty()
	return ch, func() {}
}

// base returns the session under any wrappers, which is how the Hub is keyed:
// the editor and doors see the session through wrappers that add type-ahead
// or a door watch, and each implements Unwrap.
func base(s ssh.Session) ssh.Session {
	for {
		u, ok := s.(interface{ Unwrap() ssh.Session })
		if !ok {
			return s
		}
		s = u.Unwrap()
	}
}
