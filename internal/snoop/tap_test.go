package snoop

import (
	"bytes"
	"sync"
	"testing"
	"time"
)

func recv(t *testing.T, w *Watcher) []byte {
	t.Helper()
	select {
	case b, ok := <-w.C():
		if !ok {
			t.Fatal("watcher channel closed")
		}
		return b
	case <-time.After(time.Second):
		t.Fatal("no data")
	}
	return nil
}

func TestAttachReceivesCatchupThenLive(t *testing.T) {
	tp := NewTap()
	tp.Output([]byte("\x1b[2Jmenu"))
	w := tp.Attach()
	defer w.Close()
	if got := recv(t, w); string(got) != "\x1b[2Jmenu" {
		t.Fatalf("catch-up = %q", got)
	}
	tp.Output([]byte("live"))
	if got := recv(t, w); string(got) != "live" {
		t.Fatalf("live = %q", got)
	}
}

func TestOutputCopiesBytes(t *testing.T) {
	tp := NewTap()
	w := tp.Attach()
	defer w.Close()
	buf := []byte("abc")
	tp.Output(buf)
	buf[0] = 'X'
	if got := recv(t, w); string(got) != "abc" {
		t.Fatalf("got %q; Output must copy", got)
	}
}

func TestSlowWatcherResyncsAndNeverBlocks(t *testing.T) {
	tp := NewTap()
	w := tp.Attach()
	defer w.Close()
	done := make(chan struct{})
	go func() {
		for i := 0; i < watcherQueue*4; i++ {
			tp.Output([]byte("x"))
		}
		tp.Output([]byte("\x1b[2Jfresh"))
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Output blocked on a slow watcher")
	}
	var all []byte
	for {
		select {
		case b := <-w.C():
			all = append(all, b...)
			continue
		case <-time.After(100 * time.Millisecond):
		}
		break
	}
	if !bytes.HasSuffix(all, []byte("fresh")) {
		t.Fatalf("watcher did not resync to the latest screen: tail %q", all[max(0, len(all)-20):])
	}
}

func TestCloseEndsWatchers(t *testing.T) {
	tp := NewTap()
	w := tp.Attach()
	tp.Close()
	for range w.C() {
	}
	select {
	case <-tp.Done():
	default:
		t.Fatal("Done not closed")
	}
	tp.Output([]byte("after close")) // must not panic
}

func TestTransferStartedSendsMarkerOnce(t *testing.T) {
	tp := NewTap()
	w := tp.Attach()
	defer w.Close()
	tp.SetMode(ModeTransfer)
	tp.TransferStarted()
	tp.TransferStarted()
	if got := recv(t, w); string(got) != TransferMarker {
		t.Fatalf("got %q", got)
	}
	select {
	case b := <-w.C():
		t.Fatalf("second marker %q", b)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestTapRace(t *testing.T) {
	tp := NewTap()
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			for j := 0; j < 500; j++ {
				tp.Output([]byte("data"))
			}
		}()
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				w := tp.Attach()
				w.Close()
			}
		}()
	}
	wg.Wait()
	tp.Close()
}

func TestSetTransferMovesBBSAndBack(t *testing.T) {
	tp := NewTap()
	w := tp.Attach()
	defer w.Close()
	tp.SetTransfer(true)
	if tp.Mode() != ModeTransfer {
		t.Fatalf("mode = %v, want transfer", tp.Mode())
	}
	tp.TransferStarted()
	recv(t, w)
	tp.SetTransfer(false)
	if tp.Mode() != ModeBBS {
		t.Fatalf("mode = %v, want bbs", tp.Mode())
	}
	tp.SetTransfer(true)
	tp.TransferStarted()
	if got := recv(t, w); string(got) != TransferMarker {
		t.Fatalf("second transfer marker = %q", got)
	}
}

func TestSetTransferLeavesDoorAndTeleconfMode(t *testing.T) {
	for _, m := range []Mode{ModeDoor, ModeTeleconf} {
		tp := NewTap()
		tp.SetMode(m)
		tp.SetTransfer(true)
		if tp.Mode() != m {
			t.Fatalf("mode = %v after start, want %v", tp.Mode(), m)
		}
		tp.SetTransfer(false)
		if tp.Mode() != m {
			t.Fatalf("mode = %v after end, want %v", tp.Mode(), m)
		}
	}
}

func TestResyncClearsScreenFirst(t *testing.T) {
	tp := NewTap()
	w := tp.Attach()
	for i := 0; i < watcherQueue+1; i++ {
		tp.Output([]byte("x"))
	}
	first := <-w.C()
	if !bytes.HasPrefix(first, []byte("\x1b[2J\x1b[H")) {
		t.Fatalf("resync snapshot = %q; want it to start with clear and home", first[:min(len(first), 12)])
	}
}

func TestResyncKeepsOwnClearScreen(t *testing.T) {
	tp := NewTap()
	w := tp.Attach()
	tp.Output([]byte("\x1b[2Jscreen"))
	<-w.C()
	for i := 0; i < watcherQueue+1; i++ {
		tp.Output([]byte("x"))
	}
	first := <-w.C()
	if !bytes.HasPrefix(first, []byte("\x1b[2Jscreen")) {
		t.Fatalf("resync snapshot = %q; want the buffer's own clear first", first[:min(len(first), 12)])
	}
}
