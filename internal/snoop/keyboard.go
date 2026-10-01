package snoop

import (
	"errors"
	"fmt"
	"time"
)

var (
	ErrKeyboardHeld   = errors.New("another sysop has the keyboard")
	ErrNotHolder      = errors.New("you do not have the keyboard")
	ErrBusy           = errors.New("caller is busy")
	ErrChatNotStarted = errors.New("caller's session is not waiting for input")
	ErrTapClosed      = errors.New("caller disconnected")
	ErrNotWatching    = errors.New("attach a watch before taking the keyboard")
)

const inputQueue = 64

// keyboard is the sysop-input half of a Tap. Its fields are guarded by Tap.mu.
type keyboard struct {
	holder   string
	since    time.Time     // when holder took the keyboard
	injected int           // bytes holder has injected
	input    chan []byte   // type-in bytes for the transport's Read
	ready    chan struct{} // buffered 1: signals queued type-in without consuming it
	chatIn   chan []byte   // sysop bytes while chat is active
	breakIn  chan struct{} // buffered 1: a pending chat request
	began    chan struct{} // closed by ChatBegan for the current request
	endChat  chan struct{} // closed by StopChat/ReleaseKeyboard
	chatting bool
	chats    uint64 // chats started over the tap's life
	// chatTook is the handle whose RequestChat took a free keyboard for the
	// current chat; ChatEnded gives it back. Empty when the sysop already
	// held it for type-in.
	chatTook string
}

func (k *keyboard) init() {
	k.input = make(chan []byte, inputQueue)
	k.ready = make(chan struct{}, 1)
	k.chatIn = make(chan []byte, inputQueue)
	k.breakIn = make(chan struct{}, 1)
}

// take makes handle the holder. It keeps the existing hold if handle already
// has it.
func (k *keyboard) take(handle string) {
	if k.holder == handle {
		return
	}
	k.holder = handle
	k.since = time.Now()
	k.injected = 0
}

// drop clears the holder and returns how long it held and what it injected.
func (k *keyboard) drop() (time.Duration, int) {
	held, n := time.Since(k.since), k.injected
	k.holder, k.since, k.injected = "", time.Time{}, 0
	k.chatTook = ""
	return held, n
}

func (t *Tap) TakeKeyboard(handle string) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return ErrTapClosed
	}
	if !t.watchingLocked(handle) {
		return ErrNotWatching
	}
	if t.kb.holder != "" && t.kb.holder != handle {
		return fmt.Errorf("%w: %s", ErrKeyboardHeld, t.kb.holder)
	}
	t.kb.take(handle)
	return nil
}

// ReleaseKeyboard gives up the keyboard and ends chat if handle was in it.
// It returns how long handle held the keyboard and how many bytes it
// injected; both are zero when handle was not the holder.
func (t *Tap) ReleaseKeyboard(handle string) (held time.Duration, injected int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.kb.holder != handle {
		return 0, 0
	}
	held, injected = t.kb.drop()
	t.stopChatLocked()
	return held, injected
}

func (t *Tap) KeyboardHolder() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.kb.holder
}

// Inject queues sysop bytes from handle. It returns how many bytes were
// accepted: 0 when handle does not hold the keyboard or the queue is full.
func (t *Tap) Inject(handle string, p []byte) int {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed || len(p) == 0 || t.kb.holder != handle {
		return 0
	}
	dst := t.kb.input
	if t.kb.chatting {
		dst = t.kb.chatIn
	}
	select {
	case dst <- append([]byte(nil), p...):
		t.kb.injected += len(p)
		if dst == t.kb.input {
			select {
			case t.kb.ready <- struct{}{}:
			default:
			}
		}
		return len(p)
	default:
		return 0
	}
}

func (t *Tap) Input() <-chan []byte { return t.kb.input }

// InputReady fires (without consuming) when type-in bytes are queued, so a
// transport blocked on a socket read can wake itself.
func (t *Tap) InputReady() <-chan struct{} { return t.kb.ready }

func (t *Tap) ChatInput() <-chan []byte { return t.kb.chatIn }
func (t *Tap) BreakIn() <-chan struct{} { return t.kb.breakIn }

// RequestChat takes the keyboard for handle and asks the caller's session to
// open chat. It waits up to wait for ChatBegan; on failure nothing is left
// pending and the keyboard is released if this call took it.
func (t *Tap) RequestChat(handle string, wait time.Duration) error {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return ErrTapClosed
	}
	if !t.watchingLocked(handle) {
		t.mu.Unlock()
		return ErrNotWatching
	}
	if t.mode != ModeBBS {
		m := t.mode
		t.mu.Unlock()
		return fmt.Errorf("%w: in a %s", ErrBusy, m)
	}
	if t.kb.holder != "" && t.kb.holder != handle {
		h := t.kb.holder
		t.mu.Unlock()
		return fmt.Errorf("%w: %s", ErrKeyboardHeld, h)
	}
	if t.kb.chatting {
		t.mu.Unlock()
		return nil
	}
	tookIt := t.kb.holder == ""
	t.kb.take(handle)
	t.kb.chatTook = ""
	if tookIt {
		t.kb.chatTook = handle
	}
	began := make(chan struct{})
	t.kb.began = began
	t.kb.endChat = make(chan struct{})
	select {
	case t.kb.breakIn <- struct{}{}:
	default:
	}
	t.mu.Unlock()

	select {
	case <-began:
		return nil
	case <-t.done:
		return ErrTapClosed
	case <-time.After(wait):
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.kb.chatting { // began raced the timer
		return nil
	}
	select {
	case <-t.kb.breakIn:
	default:
	}
	t.kb.began = nil
	t.kb.chatTook = ""
	if tookIt && t.kb.holder == handle {
		t.kb.drop()
	}
	// Release anyone already waiting on this request's EndChat.
	t.stopChatLocked()
	return ErrChatNotStarted
}

// ChatBegan is called by the session when it is ready to open chat. It
// returns false, and changes nothing, when no request is pending (the
// request timed out first); the session must then not open chat.
func (t *Tap) ChatBegan() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.kb.began == nil {
		return false
	}
	t.kb.chatting = true
	t.kb.chats++
	// Bytes left from a previous chat must not open this one.
	for drained := false; !drained; {
		select {
		case <-t.kb.chatIn:
		default:
			drained = true
		}
	}
	close(t.kb.began)
	t.kb.began = nil
	return true
}

// ChatEnded is called by the session after chat has closed and the screen
// has been restored. A keyboard taken only for the chat is released, so the
// sysop's next keys do not land on the caller's prompt as type-in.
func (t *Tap) ChatEnded() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.kb.chatting = false
	if t.kb.chatTook != "" && t.kb.holder == t.kb.chatTook {
		t.kb.drop()
	}
	t.kb.chatTook = ""
}

// EndChat is closed when the sysop ends chat or drops the keyboard.
func (t *Tap) EndChat() <-chan struct{} {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.kb.endChat == nil {
		t.kb.endChat = make(chan struct{})
	}
	return t.kb.endChat
}

func (t *Tap) StopChat(handle string) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.kb.holder != handle {
		return ErrNotHolder
	}
	t.stopChatLocked()
	return nil
}

func (t *Tap) stopChatLocked() {
	if t.kb.endChat != nil {
		select {
		case <-t.kb.endChat:
		default:
			close(t.kb.endChat)
		}
	}
}

// Chats returns how many chats have started on this tap. A caller that
// sees it change knows a sysop answered.
func (t *Tap) Chats() uint64 {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.kb.chats
}

func (t *Tap) Chatting() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.kb.chatting
}
