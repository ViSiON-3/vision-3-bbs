package winch

import (
	"testing"
	"time"

	"github.com/gliderlabs/ssh"
)

// ptySession is a session whose Pty hands out one channel, as the transports do.
type ptySession struct {
	ssh.Session
	ch chan ssh.Window
}

func (p *ptySession) Pty() (ssh.Pty, <-chan ssh.Window, bool) { return ssh.Pty{}, p.ch, true }

// wrapped is a session seen through a wrapper, as the editor sees it.
type wrapped struct{ ssh.Session }

func (w *wrapped) Unwrap() ssh.Session { return w.Session }

func recv(t *testing.T, ch <-chan ssh.Window, what string) ssh.Window {
	t.Helper()
	select {
	case w, ok := <-ch:
		if !ok {
			t.Fatalf("%s: channel closed", what)
		}
		return w
	case <-time.After(2 * time.Second):
		t.Fatalf("%s: nothing arrived", what)
	}
	return ssh.Window{}
}

func none(t *testing.T, ch <-chan ssh.Window, what string) {
	t.Helper()
	select {
	case w, ok := <-ch:
		t.Fatalf("%s: got %+v (open=%v), want nothing", what, w, ok)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestEveryReaderGetsEachChange(t *testing.T) {
	src := make(chan ssh.Window, 1)
	s := &ptySession{ch: src}
	src <- ssh.Window{Width: 80, Height: 25} // the transport's initial size
	hub, owner := Own(s, src)
	defer hub.Close()

	if w := recv(t, owner, "owner's initial size"); w.Width != 80 {
		t.Errorf("owner got %+v first", w)
	}
	editor, stopEditor := Subscribe(&wrapped{s})
	defer stopEditor()
	none(t, editor, "a new subscriber before any change")

	src <- ssh.Window{Width: 100, Height: 30}
	if w := recv(t, owner, "owner"); w.Width != 100 {
		t.Errorf("owner got %+v", w)
	}
	if w := recv(t, editor, "editor"); w.Width != 100 {
		t.Errorf("editor got %+v", w)
	}
}

func TestBusyReaderKeepsOnlyTheLatest(t *testing.T) {
	src := make(chan ssh.Window)
	s := &ptySession{ch: src}
	hub, owner := Own(s, src)
	defer hub.Close()

	for w := 81; w <= 85; w++ {
		src <- ssh.Window{Width: w, Height: 24} // would block if the Hub did
	}
	// The Hub may still be handing over 85, so take what is there and then
	// expect nothing older afterwards.
	got := recv(t, owner, "latest")
	if got.Width < 84 {
		t.Fatalf("got %+v, want the newest change or the one before it", got)
	}
	if got.Width == 84 {
		got = recv(t, owner, "newest")
	}
	if got.Width != 85 {
		t.Fatalf("got %+v, want 85 wide", got)
	}
	none(t, owner, "after the newest")
}

func TestStopEndsOneSubscriptionOnly(t *testing.T) {
	src := make(chan ssh.Window)
	s := &ptySession{ch: src}
	hub, owner := Own(s, src)
	defer hub.Close()
	door, stopDoor := Subscribe(s)
	stopDoor()

	src <- ssh.Window{Width: 90, Height: 40}
	recv(t, owner, "owner")
	none(t, door, "a stopped subscription")
}

func TestSourceClosingClosesEveryone(t *testing.T) {
	src := make(chan ssh.Window)
	s := &ptySession{ch: src}
	hub, owner := Own(s, src)
	defer hub.Close()
	editor, _ := Subscribe(s)
	close(src)
	for name, ch := range map[string]<-chan ssh.Window{"owner": owner, "editor": editor} {
		select {
		case _, ok := <-ch:
			if ok {
				t.Errorf("%s: got a value, want the channel closed", name)
			}
		case <-time.After(2 * time.Second):
			t.Errorf("%s: channel not closed", name)
		}
	}
	// A late subscriber on a closed Hub is closed too, and the Hub is forgotten.
	late, _ := hub.Subscribe()
	if _, ok := <-late; ok {
		t.Error("late subscription open on a closed Hub")
	}
	if _, ok := hubs.Load(s); ok {
		t.Error("closed Hub still registered")
	}
}

func TestCloseStopsTakingFromASourceThatNeverCloses(t *testing.T) {
	src := make(chan ssh.Window, 1)
	s := &ptySession{ch: src}
	hub, owner := Own(s, src)
	editor, _ := Subscribe(s)
	hub.Close()
	hub.Close() // twice is fine
	if _, ok := <-owner; ok {
		t.Error("owner open after Close")
	}
	if _, ok := <-editor; ok {
		t.Error("editor open after Close")
	}
	select {
	case <-hub.stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("the Hub's goroutine did not stop after Close")
	}
	// Nothing takes from the source any more: a second value cannot be sent.
	src <- ssh.Window{Width: 1, Height: 1}
	select {
	case src <- ssh.Window{Width: 2, Height: 2}:
		t.Error("the Hub took from the source after it stopped")
	case <-time.After(50 * time.Millisecond):
	}
}

// Without a Hub, as in tests that run a screen on its own, Subscribe hands out
// what Pty does.
func TestSubscribeFallsBackToPty(t *testing.T) {
	src := make(chan ssh.Window, 1)
	s := &ptySession{ch: src}
	ch, stop := Subscribe(&wrapped{s})
	stop()
	src <- ssh.Window{Width: 132, Height: 50}
	if w := recv(t, ch, "fallback"); w.Width != 132 {
		t.Errorf("got %+v", w)
	}
}
