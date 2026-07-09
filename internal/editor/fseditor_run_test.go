package editor

import (
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/gliderlabs/ssh"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
)

// fakeEditorSession is a minimal ssh.Session for driving FSEditor in tests.
// Read replays scripted keystrokes; once they are exhausted it blocks like a
// live connection until the read interrupt fires (mirroring the ssh/telnet
// adapters' SetReadInterrupt support). The embedded nil ssh.Session supplies
// the rest of the interface.
type fakeEditorSession struct {
	ssh.Session
	mu        sync.Mutex
	data      []byte
	interrupt <-chan struct{}
}

func (fs *fakeEditorSession) Read(p []byte) (int, error) {
	fs.mu.Lock()
	if len(fs.data) > 0 {
		n := copy(p, fs.data)
		fs.data = fs.data[n:]
		fs.mu.Unlock()
		return n, nil
	}
	interrupt := fs.interrupt
	fs.mu.Unlock()
	if interrupt == nil {
		return 0, io.EOF
	}
	<-interrupt
	return 0, errors.New("read interrupted")
}

func (fs *fakeEditorSession) Write(p []byte) (int, error) { return len(p), nil }

func (fs *fakeEditorSession) SetReadInterrupt(ch <-chan struct{}) {
	fs.mu.Lock()
	fs.interrupt = ch
	fs.mu.Unlock()
}

// TestRunClosesSelfCreatedInputHandler guards against the "double key press"
// bug: when NewFSEditor is passed a nil InputHandler it creates its own, and
// that handler's background goroutine must be stopped when Run returns.
// Otherwise it keeps reading the session and steals alternate keystrokes from
// the menu's reader for the rest of the session.
func TestRunClosesSelfCreatedInputHandler(t *testing.T) {
	// "hi" then Ctrl-Z (save and exit).
	sess := &fakeEditorSession{data: []byte("hi\x1a")}
	ed := NewFSEditor(sess, io.Discard, ansi.OutputModeUTF8, 80, 24,
		"", "", "", "", "", "", nil)

	content, saved, err := ed.Run()
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if !saved || content == "" {
		t.Fatalf("expected saved content, got saved=%v content=%q", saved, content)
	}

	select {
	case <-ed.input.done:
		// Self-created handler goroutine exited with Run — no orphaned reader.
	case <-time.After(2 * time.Second):
		t.Fatal("self-created InputHandler goroutine still reading the session after Run returned")
	}
}

// TestRunLeavesSharedInputHandlerOpen verifies the complementary invariant:
// a caller-provided (session-scoped, shared) InputHandler must survive Run so
// the menu keeps receiving keystrokes through it after the editor exits.
func TestRunLeavesSharedInputHandlerOpen(t *testing.T) {
	sess := &fakeEditorSession{data: []byte("hi\x1a")}
	shared := NewInputHandler(sess)
	ed := NewFSEditor(sess, io.Discard, ansi.OutputModeUTF8, 80, 24,
		"", "", "", "", "", "", shared)

	if _, saved, err := ed.Run(); err != nil || !saved {
		t.Fatalf("Run: saved=%v err=%v", saved, err)
	}

	select {
	case <-shared.done:
		t.Fatal("Run closed the caller-provided shared InputHandler")
	case <-time.After(50 * time.Millisecond):
		// Still open — menu reader keeps working after the editor exits.
	}
	shared.CloseAndWait()
}
