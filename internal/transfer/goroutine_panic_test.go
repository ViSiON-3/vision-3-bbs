package transfer

import (
	"context"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/safego"
	"github.com/ViSiON-3/vision-3-bbs/internal/safego/safegotest"
)

// A panic on one of a transfer's goroutines ends that transfer with an
// error, its program killed, instead of the board; the next transfer runs.
func TestTransferGoroutinePanicEndsOnlyItsTransfer(t *testing.T) {
	logs := safegotest.Capture(t)
	safegotest.Inject(t, safego.KindTransfer)

	first := newRawSession()
	first.in <- []byte("first")
	err := runDirect(t, context.Background(), first, helperCommand(t, "echo"), time.Minute)
	logs.WaitPanic(t, safego.KindTransfer, "transfer/zmodem.go")
	if err == nil {
		t.Error("first transfer: RunCommandDirect = nil, want the panic's error")
	}
	close(first.in)

	second := newRawSession()
	second.in <- []byte("second")
	close(second.in)
	if err := runDirect(t, context.Background(), second, helperCommand(t, "echo"), time.Minute); err != nil {
		t.Fatalf("second transfer: %v", err)
	}
	if got := second.rawWritten(); got != "second" {
		t.Errorf("second transfer wrote %q, want second", got)
	}
}
