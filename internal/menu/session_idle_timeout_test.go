package menu

import (
	"errors"
	"io"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/editor"
)

// blockingSession never delivers input until unblocked — the shape of a user
// sitting idle at a prompt.
type blockingSession struct {
	*testSession
	unblock chan struct{}
}

func (b *blockingSession) Read(p []byte) (int, error) {
	<-b.unblock
	return 0, io.EOF
}

// readKeyResult runs ReadKey with a guard so a missing idle timeout fails the
// test instead of hanging it.
func readKeyResult(t *testing.T, ih *editor.InputHandler) error {
	t.Helper()
	errCh := make(chan error, 1)
	go func() {
		_, err := ih.ReadKey()
		errCh <- err
	}()
	select {
	case err := <-errCh:
		return err
	case <-time.After(1 * time.Second):
		return nil // no timeout fired — caller treats nil as failure
	}
}

// TestSessionIdleTimeoutSurvivesReset reproduces the post-door idle-timeout
// bypass: Run applies the idle timeout once, door handlers call
// resetSessionIH, and the recreated handler silently lost the timeout —
// letting users idle forever after running any door.
func TestSessionIdleTimeoutSurvivesReset(t *testing.T) {
	s := &blockingSession{testSession: newTestSession(""), unblock: make(chan struct{})}
	t.Cleanup(func() {
		close(s.unblock)
		resetSessionIH(s)
		clearSessionIdleTimeout(s)
	})

	applySessionIdleTimeout(s, 30*time.Millisecond)

	if err := readKeyResult(t, getSessionIH(s)); !errors.Is(err, editor.ErrIdleTimeout) {
		t.Fatalf("initial handler: want ErrIdleTimeout, got %v", err)
	}

	// Door cleanup path.
	resetSessionIH(s)

	// The recreated handler must still enforce the session idle timeout.
	if err := readKeyResult(t, getSessionIH(s)); !errors.Is(err, editor.ErrIdleTimeout) {
		t.Fatalf("handler recreated after reset: want ErrIdleTimeout, got %v (idle timeout lost)", err)
	}
}
