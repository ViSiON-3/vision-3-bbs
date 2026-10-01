package editor

import (
	"errors"
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

func TestBreakInSuspendsDeadline(t *testing.T) {
	pr, pw := io.Pipe()
	ih := NewInputHandler(pr)
	defer ih.Close()
	ih.SetSessionDeadline(time.Now().Add(50 * time.Millisecond))
	brk := make(chan struct{}, 1)
	inner := make(chan error, 1)
	// The key arrives well after the 50ms deadline; the read inside fn only
	// succeeds if the deadline is suspended while fn runs.
	ih.SetBreakIn(brk, func() {
		go func() {
			time.Sleep(150 * time.Millisecond)
			_, _ = pw.Write([]byte("k"))
		}()
		_, err := ih.ReadKey()
		inner <- err
	})
	errc := make(chan error, 1)
	go func() { _, err := ih.ReadKey(); errc <- err }()
	brk <- struct{}{}

	select {
	case err := <-inner:
		if err != nil {
			t.Fatalf("read inside break-in failed: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("break-in did not finish")
	}
	// The deadline is back after fn and has passed, so the outer read ends.
	select {
	case err := <-errc:
		if !errors.Is(err, ErrTimeLimit) {
			t.Fatalf("outer err = %v, want ErrTimeLimit", err)
		}
	case <-time.After(time.Second):
		t.Fatal("outer read still blocked after deadline restored")
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
	_, _ = pw.Write([]byte("\x1b["))
	time.Sleep(30 * time.Millisecond)
	brk <- struct{}{}
	time.Sleep(10 * time.Millisecond)
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
