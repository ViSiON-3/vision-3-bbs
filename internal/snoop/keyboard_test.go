package snoop

import (
	"errors"
	"testing"
	"time"
)

func TestInjectDroppedWithoutKeyboard(t *testing.T) {
	tp := NewTap()
	if n := tp.Inject("sysop", []byte("x")); n != 0 {
		t.Fatalf("accepted %d bytes without the keyboard", n)
	}
}

func TestOnlyOneHolder(t *testing.T) {
	tp := NewTap()
	if err := tp.TakeKeyboard("a"); err != nil {
		t.Fatal(err)
	}
	if err := tp.TakeKeyboard("b"); !errors.Is(err, ErrKeyboardHeld) {
		t.Fatalf("second take = %v; want ErrKeyboardHeld", err)
	}
	if n := tp.Inject("b", []byte("x")); n != 0 {
		t.Fatal("non-holder injected")
	}
	if n := tp.Inject("a", []byte("hi")); n != 2 {
		t.Fatalf("holder injected %d", n)
	}
	if got := <-tp.Input(); string(got) != "hi" {
		t.Fatalf("Input = %q", got)
	}
	tp.ReleaseKeyboard("a")
	if err := tp.TakeKeyboard("b"); err != nil {
		t.Fatalf("take after release: %v", err)
	}
}

func TestReleaseKeyboardReportsHoldAndBytes(t *testing.T) {
	tp := NewTap()
	if held, n := tp.ReleaseKeyboard("a"); held != 0 || n != 0 {
		t.Fatalf("release without holding = %v, %d; want 0, 0", held, n)
	}
	if err := tp.TakeKeyboard("a"); err != nil {
		t.Fatal(err)
	}
	tp.Inject("a", []byte("abc"))
	tp.Inject("a", []byte("de"))
	tp.Inject("b", []byte("zzz"))
	time.Sleep(10 * time.Millisecond)
	if held, n := tp.ReleaseKeyboard("b"); held != 0 || n != 0 {
		t.Fatalf("non-holder release = %v, %d; want 0, 0", held, n)
	}
	held, n := tp.ReleaseKeyboard("a")
	if held < 10*time.Millisecond || n != 5 {
		t.Fatalf("release = %v, %d; want >=10ms, 5", held, n)
	}
	if err := tp.TakeKeyboard("a"); err != nil {
		t.Fatal(err)
	}
	if held, n := tp.ReleaseKeyboard("a"); n != 0 || held >= 10*time.Millisecond {
		t.Fatalf("second hold = %v, %d; counters were not reset", held, n)
	}
}

func TestRequestChatRefusedInDoorAndTransfer(t *testing.T) {
	for _, m := range []Mode{ModeDoor, ModeTransfer} {
		tp := NewTap()
		tp.SetMode(m)
		err := tp.RequestChat("a", 50*time.Millisecond)
		if !errors.Is(err, ErrBusy) {
			t.Fatalf("mode %v: err = %v; want ErrBusy", m, err)
		}
	}
}

func TestRequestChatTimesOutWhenNobodyServicesBreakIn(t *testing.T) {
	tp := NewTap()
	err := tp.RequestChat("a", 50*time.Millisecond)
	if !errors.Is(err, ErrChatNotStarted) {
		t.Fatalf("err = %v; want ErrChatNotStarted", err)
	}
	select {
	case <-tp.BreakIn():
		t.Fatal("stale break-in left pending after timeout")
	default:
	}
	if tp.KeyboardHolder() != "" {
		t.Fatal("keyboard not released after failed chat")
	}
}

func TestChatHandshakeAndRouting(t *testing.T) {
	tp := NewTap()
	go func() {
		<-tp.BreakIn()
		tp.ChatBegan()
	}()
	if err := tp.RequestChat("a", time.Second); err != nil {
		t.Fatal(err)
	}
	if !tp.Chatting() {
		t.Fatal("not chatting")
	}
	tp.Inject("a", []byte("yo"))
	if got := <-tp.ChatInput(); string(got) != "yo" {
		t.Fatalf("ChatInput = %q", got)
	}
	if err := tp.StopChat("a"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-tp.EndChat():
	case <-time.After(time.Second):
		t.Fatal("EndChat not closed")
	}
	tp.ChatEnded()
	if tp.Chatting() {
		t.Fatal("still chatting")
	}
}

func TestReleaseKeyboardEndsChat(t *testing.T) {
	tp := NewTap()
	go func() { <-tp.BreakIn(); tp.ChatBegan() }()
	if err := tp.RequestChat("a", time.Second); err != nil {
		t.Fatal(err)
	}
	end := tp.EndChat()
	tp.ReleaseKeyboard("a")
	select {
	case <-end:
	case <-time.After(time.Second):
		t.Fatal("dropping the sysop must end chat")
	}
}
