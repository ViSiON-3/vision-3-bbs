package snoop

import (
	"bytes"
	"sync"
)

// Mode is what the caller's session is doing, as far as snoop cares.
type Mode int

const (
	ModeBBS Mode = iota
	ModeDoor
	ModeTransfer
	// ModeTeleconf is the teleconference, whose room output is drawn with
	// absolute positioning that a split screen would garble.
	ModeTeleconf
)

func (m Mode) String() string {
	switch m {
	case ModeDoor:
		return "door"
	case ModeTransfer:
		return "transfer"
	case ModeTeleconf:
		return "teleconference"
	default:
		return "bbs"
	}
}

// TransferMarker is sent to watchers in place of binary transfer data.
const TransferMarker = "\r\n[transfer in progress]\r\n"

// watcherQueue is how many output chunks a watcher may lag before it is
// resynced from the catch-up buffer.
const watcherQueue = 256

// Tapped is implemented by transport sessions that carry a Tap.
type Tapped interface{ Tap() *Tap }

// Tap is one node's snoop point. Output is called by the transport after
// each write to the caller; it never blocks.
type Tap struct {
	mu       sync.Mutex
	buf      *catchup
	watchers map[*Watcher]struct{}
	mode     Mode
	cp437    bool
	marked   bool // TransferMarker sent for the current transfer
	closed   bool
	done     chan struct{}

	kb keyboard
}

// NewTap returns an open Tap with an empty catch-up buffer.
func NewTap() *Tap {
	t := &Tap{
		buf:      newCatchup(CatchupLimit),
		watchers: make(map[*Watcher]struct{}),
		done:     make(chan struct{}),
	}
	t.kb.init()
	return t
}

// Watcher receives a node's output. The first chunk is the catch-up buffer.
type Watcher struct {
	tap    *Tap
	ch     chan []byte
	handle string // sysop this watch belongs to; "" for an anonymous watch
	once   sync.Once
}

// resync replaces whatever is queued with the catch-up buffer so a lagging
// watcher ends on the current screen. Caller holds t.mu, which makes it the
// only sender, so the send after the drain cannot block.
func (t *Tap) resync(w *Watcher) {
	for {
		select {
		case <-w.ch:
			continue
		default:
		}
		break
	}
	snap, _ := t.buf.snapshot()
	if !bytes.HasPrefix(snap, clearScreen) {
		// The queue held the rest of the old screen. Start the snapshot on
		// a clean one.
		snap = append([]byte("\x1b[0m\x1b[2J\x1b[H"), snap...)
	}
	w.ch <- snap
}

// C carries output chunks; it is closed by Close or when the tap closes.
func (w *Watcher) C() <-chan []byte { return w.ch }

// Close detaches the watcher. Safe to call more than once.
func (w *Watcher) Close() {
	w.once.Do(func() {
		w.tap.mu.Lock()
		if _, ok := w.tap.watchers[w]; ok {
			delete(w.tap.watchers, w)
			close(w.ch)
			w.tap.lostWatcherLocked(w.handle)
		}
		w.tap.mu.Unlock()
	})
}

// watchingLocked reports whether handle has an attached watcher.
func (t *Tap) watchingLocked(handle string) bool {
	for w := range t.watchers {
		if w.handle == handle {
			return true
		}
	}
	return false
}

// lostWatcherLocked releases handle's keyboard and chat once its last watcher
// is gone, so a sysop who stops watching cannot keep typing into the node.
func (t *Tap) lostWatcherLocked(handle string) {
	if handle == "" || t.kb.holder != handle || t.watchingLocked(handle) {
		return
	}
	t.kb.drop()
	t.stopChatLocked()
}

// Attach adds an anonymous watcher primed with the current screen. On a
// closed tap the returned watcher's channel is already closed.
func (t *Tap) Attach() *Watcher { return t.AttachAs("") }

// AttachAs is Attach for a named sysop. Only a handle with an attached
// watcher can take the keyboard or request chat, and closing its last
// watcher releases both.
func (t *Tap) AttachAs(handle string) *Watcher {
	w := &Watcher{tap: t, ch: make(chan []byte, watcherQueue), handle: handle}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		close(w.ch)
		return w
	}
	if snap, _ := t.buf.snapshot(); len(snap) > 0 {
		w.ch <- snap
	}
	t.watchers[w] = struct{}{}
	return w
}

// Output records p and copies it to every watcher.
func (t *Tap) Output(p []byte) {
	if len(p) == 0 {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return
	}
	t.buf.write(p)
	for w := range t.watchers {
		select {
		case w.ch <- append([]byte(nil), p...):
		default:
			t.resync(w)
		}
	}
}

// TransferStarted sends TransferMarker to watchers once per transfer. The
// transport calls it instead of Output while a binary transfer is active.
func (t *Tap) TransferStarted() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed || t.marked {
		return
	}
	t.marked = true
	for w := range t.watchers {
		select {
		case w.ch <- []byte(TransferMarker):
		default:
			t.resync(w)
			w.ch <- []byte(TransferMarker)
		}
	}
}

// Snapshot returns the catch-up buffer and whether it overflowed.
func (t *Tap) Snapshot() ([]byte, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.buf.snapshot()
}

func (t *Tap) SetMode(m Mode) {
	t.mu.Lock()
	t.mode = m
	if m != ModeTransfer {
		t.marked = false
	}
	t.mu.Unlock()
}

// SetTransfer records a binary transfer starting or ending. A transfer
// started from the BBS moves the tap to ModeTransfer and back; ModeDoor and
// ModeTeleconf are left alone, since chat must stay refused for their whole
// length.
func (t *Tap) SetTransfer(active bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	switch {
	case active && t.mode == ModeBBS:
		t.mode = ModeTransfer
	case !active && t.mode == ModeTransfer:
		t.mode = ModeBBS
		t.marked = false
	}
}

func (t *Tap) Mode() Mode {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.mode
}

func (t *Tap) SetCP437(on bool) {
	t.mu.Lock()
	t.cp437 = on
	t.mu.Unlock()
}

func (t *Tap) CP437() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.cp437
}

// Close ends every watcher and releases the keyboard. Idempotent.
func (t *Tap) Close() {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return
	}
	t.closed = true
	for w := range t.watchers {
		delete(t.watchers, w)
		close(w.ch)
	}
	if t.kb.holder != "" {
		t.kb.drop()
	}
	t.stopChatLocked()
	close(t.done)
	t.mu.Unlock()
}

// Done is closed when the tap closes (the caller disconnected).
func (t *Tap) Done() <-chan struct{} { return t.done }
