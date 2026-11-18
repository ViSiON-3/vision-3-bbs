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
				t.Errorf("events=%v: key = %q", withEvents, rune(k))
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

// queueByte types b and returns once the handler's reader has queued it, so
// a wait that follows finds it already waiting.
func queueByte(t *testing.T, ih *InputHandler, pw *io.PipeWriter, b byte) {
	t.Helper()
	go func() { _, _ = pw.Write([]byte{b}) }()
	deadline := time.Now().Add(5 * time.Second)
	for len(ih.incoming) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("the typed byte never reached the handler")
		}
		time.Sleep(time.Millisecond)
	}
}

// A wait of 0, or one shorter than any timer can measure, reports what is
// already waiting every time, not at random against a timer that has run
// out, and a wait of 0 reports elapsed when nothing is.
func TestShortWaitTakesWhatIsWaiting(t *testing.T) {
	for _, d := range []time.Duration{0, time.Nanosecond, 50 * time.Microsecond} {
		pr, pw := io.Pipe()
		ih := NewInputHandler(pr)
		if d == 0 {
			if got := ih.WaitFor(d); got != WaitElapsed {
				t.Fatalf("wait %s, nothing waiting: got %v, want elapsed", d, got)
			}
		}
		for i := 0; i < 100; i++ {
			ih.Interrupt()
			if got := ih.WaitFor(d); got != WaitInterrupted {
				t.Fatalf("wait %s, try %d: interrupt waiting: got %v, want interrupted", d, i, got)
			}
		}
		queueByte(t, ih, pw, 'x')
		for i := 0; i < 100; i++ {
			if got := ih.WaitFor(d); got != WaitInput {
				t.Fatalf("wait %s, try %d: key waiting: got %v, want input", d, i, got)
			}
		}
		if key, err := ih.ReadKey(); err != nil || key != 'x' {
			t.Fatalf("wait %s: ReadKey = %q, %v; want 'x'", d, rune(key), err)
		}
		_ = pw.Close() // so the handler's reader gets EOF and exits
		ih.Close()
	}
}

// A wait outside the toast hook leaves a sysop's chat request on its
// channel: only the hook's caller services one a wait has taken, so taking
// it here would strand it until a toast runs. The next read services it.
func TestWaitOutsideTheHookLeavesABreakIn(t *testing.T) {
	pr, pw := io.Pipe()
	defer pw.Close()
	ih := NewInputHandler(pr)
	defer ih.Close()
	brk := make(chan struct{}, 1)
	ran := make(chan struct{})
	ih.SetBreakIn(brk, func() { close(ran) })
	brk <- struct{}{}
	for _, d := range []time.Duration{0, 20 * time.Millisecond} {
		if got := ih.WaitFor(d); got != WaitElapsed {
			t.Fatalf("wait %s: got %v, want elapsed", d, got)
		}
	}
	if _, ok, err := ih.PendingByte(0); ok || err != nil {
		t.Fatalf("PendingByte: ok %v, err %v; want neither", ok, err)
	}
	if len(brk) != 1 || ih.breakInPending {
		t.Fatalf("break-in taken outside the hook: queued %d, pending %v", len(brk), ih.breakInPending)
	}
	keyc := make(chan int, 1)
	go func() {
		k, _ := ih.ReadKey()
		keyc <- k
	}()
	select {
	case <-ran:
	case <-time.After(time.Second):
		t.Fatal("the next read did not service the break-in")
	}
	_, _ = pw.Write([]byte("z"))
	if k := <-keyc; k != 'z' {
		t.Errorf("key = %q, want z", rune(k))
	}
}

// A chat request and a window change during the same dwell: the chat request
// was taken off its channel, so it is serviced, and the event still arrives.
func TestBreakInAndEventDuringOneDwell(t *testing.T) {
	pr, _ := io.Pipe()
	ih := NewInputHandler(pr)
	defer ih.Close()
	brk := make(chan struct{}, 1)
	ran := make(chan struct{})
	ih.SetBreakIn(brk, func() { close(ran) })
	wake, ended := dwellHook(t, ih)
	events := make(chan int, 1)

	res := make(chan int, 1)
	go func() {
		_, ev, isEvent, _ := ReadKeyOrEvent(ih, events)
		if !isEvent {
			ev = -1
		}
		res <- ev
	}()
	wake <- struct{}{}
	time.Sleep(20 * time.Millisecond)
	events <- 7
	brk <- struct{}{}
	select {
	case <-ended:
	case <-time.After(time.Second):
		t.Fatal("dwell did not end")
	}
	select {
	case <-ran:
	case <-time.After(time.Second):
		t.Fatal("break-in lost behind the queued event")
	}
	select {
	case ev := <-res:
		if ev != 7 {
			t.Errorf("got %d, want event 7", ev)
		}
	case <-time.After(time.Second):
		t.Fatal("event not returned")
	}
}

// An Interrupt sent for an event that was taken without a toast showing must
// not end a later toast's dwell.
func TestHandledEventDoesNotEndALaterDwell(t *testing.T) {
	pr, _ := io.Pipe()
	ih := NewInputHandler(pr)
	defer ih.Close()
	wake, ended := dwellHook(t, ih)
	events := make(chan int, 1)

	// The forwarder's order: the event, then the wake.
	events <- 1
	ih.Interrupt()
	if _, ev, isEvent, _ := ReadKeyOrEvent(ih, events); !isEvent || ev != 1 {
		t.Fatalf("first read = %d,%v, want event 1", ev, isEvent)
	}

	go func() { _, _, _, _ = ReadKeyOrEvent(ih, events) }()
	wake <- struct{}{}
	select {
	case <-ended:
		t.Fatal("a stale interrupt ended the dwell")
	case <-time.After(150 * time.Millisecond):
	}
}
