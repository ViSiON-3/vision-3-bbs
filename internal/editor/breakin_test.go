package editor

import (
	"io"
	"testing"
	"time"
)

func TestBreakInRunsDuringReadKeyThenReadContinues(t *testing.T) {
	pr, pw := io.Pipe()
	ih := NewInputHandler(pr)
	defer ih.Close()
	brk := make(chan struct{}, 1)
	ran := make(chan struct{})
	ih.SetBreakIn(brk, func() { close(ran) })

	keyc := make(chan int, 1)
	go func() {
		k, _ := ih.ReadKey()
		keyc <- k
	}()
	brk <- struct{}{}
	select {
	case <-ran:
	case <-time.After(time.Second):
		t.Fatal("break-in not serviced")
	}
	go func() { _, _ = pw.Write([]byte("z")) }()
	select {
	case k := <-keyc:
		if k != 'z' {
			t.Fatalf("key = %q", k)
		}
	case <-time.After(time.Second):
		t.Fatal("original read did not continue")
	}
}

func TestBreakInFnCanReadKeys(t *testing.T) {
	pr, pw := io.Pipe()
	ih := NewInputHandler(pr)
	defer ih.Close()
	brk := make(chan struct{}, 1)
	inner := make(chan int, 1)
	ih.SetBreakIn(brk, func() {
		k, _ := ih.ReadKey()
		inner <- k
	})
	outer := make(chan int, 1)
	go func() { k, _ := ih.ReadKey(); outer <- k }()
	brk <- struct{}{}
	_, _ = pw.Write([]byte("a"))
	if k := <-inner; k != 'a' {
		t.Fatalf("inner = %q", k)
	}
	_, _ = pw.Write([]byte("b"))
	if k := <-outer; k != 'b' {
		t.Fatalf("outer = %q", k)
	}
}

// writeAfter writes b to pw after d.
func writeAfter(pw *io.PipeWriter, d time.Duration, b string) {
	go func() {
		time.Sleep(d)
		_, _ = pw.Write([]byte(b))
	}()
}

func TestBreakInSuspendsDeadline(t *testing.T) {
	pr, pw := io.Pipe()
	ih := NewInputHandler(pr)
	defer ih.Close()
	ih.SetSessionDeadline(time.Now().Add(200 * time.Millisecond))
	brk := make(chan struct{}, 1)
	inner := make(chan error, 1)
	// The key arrives well after the deadline; the read inside fn only
	// succeeds if the deadline is suspended while fn runs.
	ih.SetBreakIn(brk, func() {
		writeAfter(pw, 400*time.Millisecond, "k")
		_, err := ih.ReadKey()
		inner <- err
	})
	brk <- struct{}{}
	go func() { _, _ = ih.ReadKey() }()
	select {
	case err := <-inner:
		if err != nil {
			t.Fatalf("read inside break-in failed: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("break-in did not finish")
	}
}

func TestBreakInSuspendsIdleTimeout(t *testing.T) {
	pr, pw := io.Pipe()
	ih := NewInputHandler(pr)
	defer ih.Close()
	ih.SetSessionIdleTimeout(50 * time.Millisecond)
	brk := make(chan struct{}, 1)
	inner := make(chan error, 1)
	ih.SetBreakIn(brk, func() {
		writeAfter(pw, 300*time.Millisecond, "k")
		_, err := ih.ReadKey()
		inner <- err
	})
	brk <- struct{}{}
	go func() { _, _ = ih.ReadKey() }()
	select {
	case err := <-inner:
		if err != nil {
			t.Fatalf("read inside break-in failed: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("break-in did not finish")
	}
	if got := ih.sessionIdleTimeout(); got != 50*time.Millisecond {
		t.Fatalf("idle timeout after break-in = %v, want 50ms", got)
	}
}

func TestBreakInDoesNotChargeChatTime(t *testing.T) {
	pr, pw := io.Pipe()
	ih := NewInputHandler(pr)
	defer ih.Close()
	ih.SetSessionDeadline(time.Now().Add(300 * time.Millisecond))
	brk := make(chan struct{}, 1)
	fnDone := make(chan struct{})
	ih.SetBreakIn(brk, func() {
		time.Sleep(600 * time.Millisecond)
		close(fnDone)
	})
	type result struct {
		key int
		err error
	}
	resc := make(chan result, 1)
	go func() { k, err := ih.ReadKey(); resc <- result{k, err} }()
	brk <- struct{}{}
	<-fnDone
	select {
	case r := <-resc:
		t.Fatalf("outer read returned after chat: key=%d err=%v", r.key, r.err)
	case <-time.After(100 * time.Millisecond):
	}
	_, _ = pw.Write([]byte("x"))
	select {
	case r := <-resc:
		if r.err != nil || r.key != 'x' {
			t.Fatalf("outer read = %d, %v; want 'x'", r.key, r.err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("outer read did not get the key")
	}
}

func TestBreakInNotServicedInsideEscapeSequence(t *testing.T) {
	pr, pw := io.Pipe()
	ih := NewInputHandler(pr)
	defer ih.Close()
	brk := make(chan struct{}, 1)
	ran := make(chan struct{})
	ih.SetBreakIn(brk, func() { close(ran) })

	keyc := make(chan int, 1)
	go func() { k, _ := ih.ReadKey(); keyc <- k }()
	// Write returns once ESC is queued, so an empty queue means the reader
	// took ESC and is now in the inter-byte wait.
	_, _ = pw.Write([]byte("\x1b["))
	for i := 0; i < 1000 && len(ih.incoming) > 0; i++ {
		time.Sleep(time.Millisecond)
	}
	brk <- struct{}{}
	_, _ = pw.Write([]byte("A"))
	select {
	case k := <-keyc:
		if k != KeyArrowUp {
			t.Fatalf("key = %#x, want arrow up", k)
		}
	case <-time.After(time.Second):
		t.Fatal("read did not finish")
	}
	select {
	case <-ran:
		t.Fatal("break-in serviced inside an escape sequence")
	default:
	}
}

func TestReadKeyOrEventServicesBreakIn(t *testing.T) {
	pr, pw := io.Pipe()
	ih := NewInputHandler(pr)
	defer ih.Close()
	brk := make(chan struct{}, 1)
	ran := make(chan struct{})
	ih.SetBreakIn(brk, func() { close(ran) })
	events := make(chan int)
	go func() { _, _, _, _ = ReadKeyOrEvent(ih, events) }()
	brk <- struct{}{}
	select {
	case <-ran:
	case <-time.After(time.Second):
		t.Fatal("break-in not serviced in ReadKeyOrEvent")
	}
	_ = pw.Close()
}
