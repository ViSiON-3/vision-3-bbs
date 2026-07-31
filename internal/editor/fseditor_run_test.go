package editor

import (
	"io"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/editor/testterm"
)

// TestRunClosesSelfCreatedInputHandler guards against the "double key press"
// bug: when NewFSEditor is passed a nil InputHandler it creates its own, and
// that handler's background goroutine must be stopped when Run returns.
// Otherwise it keeps reading the session and steals alternate keystrokes from
// the menu's reader for the rest of the session.
func TestRunClosesSelfCreatedInputHandler(t *testing.T) {
	// "hi" then Ctrl-Z (save and exit).
	sess := testterm.NewSession(nil, "hi\x1a")
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
	sess := testterm.NewSession(nil, "hi\x1a")
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
