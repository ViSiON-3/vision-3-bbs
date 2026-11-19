package winch

import (
	"testing"
	"time"

	"github.com/gliderlabs/ssh"

	"github.com/ViSiON-3/vision-3-bbs/internal/safego"
	"github.com/ViSiON-3/vision-3-bbs/internal/safego/safegotest"
)

// A panic on one session's window-change watcher stops only that session's
// resizes; the next session's are delivered.
func TestResizeWatcherPanicEndsOnlyItsSession(t *testing.T) {
	logs := safegotest.Capture(t)
	safegotest.Inject(t, safego.KindResize)

	src1 := make(chan ssh.Window, 1)
	hub1, _ := Own(&ptySession{ch: src1}, src1)
	defer hub1.Close()
	defer close(src1) // the transport closing ends the panic's drain
	logs.WaitPanic(t, safego.KindResize, "winch.go")

	src2 := make(chan ssh.Window, 1)
	hub2, owner2 := Own(&ptySession{ch: src2}, src2)
	defer hub2.Close()
	src2 <- ssh.Window{Width: 100, Height: 30}
	if w := recv(t, owner2, "second session"); w.Width != 100 {
		t.Errorf("second session got %+v", w)
	}
}

// After a watcher panic, the source closing still closes the hub, as it does
// without one: its subscribers' channels close.
func TestResizeWatcherPanicStillClosesTheHub(t *testing.T) {
	logs := safegotest.Capture(t)
	safegotest.Inject(t, safego.KindResize)

	src := make(chan ssh.Window, 1)
	s := &ptySession{ch: src}
	_, owner := Own(s, src)
	logs.WaitPanic(t, safego.KindResize, "winch.go")
	close(src)
	select {
	case _, ok := <-owner:
		if ok {
			t.Fatal("owner got a window after the source closed")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the hub was not closed when its source closed after the panic")
	}
}
