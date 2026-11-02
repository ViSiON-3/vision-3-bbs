package editor

import (
	"io"
	"testing"
	"time"
)

// dwellHook is a toast hook that holds a WaitForInput open and reports how it
// ended, as the menu package's overlay does while a toast dwells.
func dwellHook(t *testing.T, ih *InputHandler) (wake chan struct{}, ended chan bool) {
	t.Helper()
	wake = make(chan struct{}, 1)
	ended = make(chan bool, 1)
	ih.SetToastHook(wake, func(time.Duration) {
		ended <- ih.WaitForInput(time.Hour)
	})
	return wake, ended
}

// A sysop's chat request must not wait behind a toast's dwell: the wait ends
// and the break-in runs at once, then the read goes on.
func TestBreakInEndsAToastDwell(t *testing.T) {
	for _, withEvents := range []bool{false, true} {
		pr, pw := io.Pipe()
		ih := NewInputHandler(pr)
		brk := make(chan struct{}, 1)
		ran := make(chan struct{})
		ih.SetBreakIn(brk, func() { close(ran) })
		wake, ended := dwellHook(t, ih)

		keyc := make(chan int, 1)
		go func() {
			var k int
			if withEvents {
				k, _, _, _ = ReadKeyOrEvent(ih, make(chan struct{}))
			} else {
				k, _ = ih.ReadKey()
			}
			keyc <- k
		}()
		wake <- struct{}{}
		time.Sleep(20 * time.Millisecond) // the hook is now in its dwell
		brk <- struct{}{}
		select {
		case keyed := <-ended:
			if keyed {
				t.Errorf("events=%v: dwell reported a key for a break-in", withEvents)
			}
		case <-time.After(time.Second):
			t.Fatalf("events=%v: break-in did not end the dwell", withEvents)
		}
		select {
		case <-ran:
		case <-time.After(time.Second):
			t.Fatalf("events=%v: break-in not serviced after the dwell", withEvents)
		}
		go func() { _, _ = pw.Write([]byte("z")) }()
		select {
		case k := <-keyc:
			if k != 'z' {
				t.Errorf("events=%v: key = %q", withEvents, k)
			}
		case <-time.After(time.Second):
			t.Fatalf("events=%v: read did not continue", withEvents)
		}
		ih.Close()
	}
}

// A window change reaches the editor while a toast dwells: Interrupt ends the
// dwell and the event is returned before any key or further toast.
func TestInterruptEndsAToastDwellForAnEvent(t *testing.T) {
	pr, _ := io.Pipe()
	ih := NewInputHandler(pr)
	defer ih.Close()
	wake, ended := dwellHook(t, ih)
	events := make(chan int, 1)

	type result struct {
		ev      int
		isEvent bool
	}
	res := make(chan result, 1)
	go func() {
		_, ev, isEvent, _ := ReadKeyOrEvent(ih, events)
		res <- result{ev, isEvent}
	}()
	wake <- struct{}{}
	time.Sleep(20 * time.Millisecond)
	// Another toast is already waiting, as the forwarder's wake and the
	// event arrive in that order.
	events <- 42
	wake <- struct{}{}
	ih.Interrupt()
	select {
	case keyed := <-ended:
		if keyed {
			t.Error("dwell reported a key for an interrupt")
		}
	case <-time.After(time.Second):
		t.Fatal("interrupt did not end the dwell")
	}
	select {
	case r := <-res:
		if !r.isEvent || r.ev != 42 {
			t.Errorf("got %+v, want event 42", r)
		}
	case <-time.After(time.Second):
		t.Fatal("event not returned after the dwell")
	}
}

// An Interrupt with no dwell in progress ends the next one, so a change that
// lands just before the toast is drawn is still taken promptly.
func TestInterruptBeforeTheDwellEndsIt(t *testing.T) {
	pr, _ := io.Pipe()
	ih := NewInputHandler(pr)
	defer ih.Close()
	ih.Interrupt()
	start := time.Now()
	if ih.WaitForInput(time.Second) {
		t.Fatal("wait reported a key")
	}
	if time.Since(start) > 500*time.Millisecond {
		t.Fatal("an early interrupt did not end the wait")
	}
	// The wake was used up: the next wait runs its course.
	start = time.Now()
	if ih.WaitForInput(30*time.Millisecond) || time.Since(start) < 30*time.Millisecond {
		t.Fatal("a used interrupt ended a later wait")
	}
}
