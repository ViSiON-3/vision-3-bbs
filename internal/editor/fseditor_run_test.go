package editor

import (
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/editor/testterm"
)

func TestRunBackspacePreservesSpaces(t *testing.T) {
	for _, backspace := range []string{"\x08", "\x7f"} {
		t.Run(fmt.Sprintf("key_%x", backspace[0]), func(t *testing.T) {
			for _, initial := range []string{"", "hello w"} {
				t.Run(fmt.Sprintf("initial_%q", initial), func(t *testing.T) {
					keys := backspace + "world\x1a"
					if initial == "" {
						keys = "hello w" + keys
					}
					sess := testterm.NewSession(nil, keys)
					ed := NewFSEditor(sess, io.Discard, ansi.OutputModeUTF8, 80, 24,
						"", "", "", "", "", "", nil)
					ed.LoadContent(initial)
					content, saved, err := ed.Run()
					if err != nil || !saved || content != "hello world" {
						t.Fatalf("Run = (%q,%v,%v), want (%q,true,nil)", content, saved, err, "hello world")
					}
				})
			}
		})
	}
}

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

// TestRunQuotePickerIgnoresSyncTERMCtrlQTrailer replays what SyncTERM on macOS
// sends for CTRL-Q: 0x11 immediately followed by 0x10. The 0x10 used to reach
// the quote picker as End, so it opened with the lightbar on the last source
// line (#318). SPACE then quotes whichever line the bar is on, which makes the
// bar's position observable in the saved message.
func TestRunQuotePickerIgnoresSyncTERMCtrlQTrailer(t *testing.T) {
	lines := []string{"first line", "middle line", "last line"}
	for _, tc := range []struct {
		name, open string
	}{
		{"plain CTRL-Q", "\x11"},
		{"SyncTERM CTRL-Q with 0x10 trailer", "\x11\x10"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// open picker, SPACE quotes the selected line, ESC closes, CTRL-Z saves.
			sess := testterm.NewSession(nil, tc.open+" \x1b\x1a")
			ed := NewFSEditor(sess, io.Discard, ansi.OutputModeUTF8, 80, 24,
				"", "", "", "", "", "", nil)
			ed.input.SetEscTimeout(10 * time.Millisecond)
			ed.SetQuoteData(&QuoteData{From: "Bucko", Title: "t", Lines: lines})
			content, saved, err := ed.Run()
			if err != nil || !saved {
				t.Fatalf("Run: saved=%v err=%v", saved, err)
			}
			if !strings.Contains(content, "first line") || strings.Contains(content, "last line") {
				t.Fatalf("picker did not open on the first line; saved message:\n%s", content)
			}
		})
	}
}

// A deliberate End after the picker is open must still work: only a 0x10 that
// rides in with the CTRL-Q keypress is dropped.
func TestDiscardPendingByteOnlyDropsTheMatchingByte(t *testing.T) {
	sess := testterm.NewSession(nil, "\x10x")
	ih := NewInputHandler(sess)
	defer ih.CloseAndWait()
	if !ih.DiscardPendingByte(KeyCtrlP, 200*time.Millisecond) {
		t.Fatal("pending 0x10 was not dropped")
	}
	if ih.DiscardPendingByte(KeyCtrlP, 200*time.Millisecond) {
		t.Fatal("a non-matching byte must not be dropped")
	}
	if key, err := ih.ReadKey(); err != nil || key != 'x' {
		t.Fatalf("the non-matching byte must stay readable: key=%q err=%v", key, err)
	}
	if ih.DiscardPendingByte(KeyCtrlP, 20*time.Millisecond) {
		t.Fatal("nothing pending must report false")
	}
}

// TestRunSpaceTypedAtMarginIsKept guards #412: when the space typed after a
// word is what pushes the line past the margin, the wrap consumes it as the
// soft break. The cursor must move to a fresh line so the next word stays a
// separate word instead of being glued to the last one ("thecursor").
func TestRunSpaceTypedAtMarginIsKept(t *testing.T) {
	// The first line is exactly 79 columns once "the" is typed.
	first := "insight as well as it causes repeating numbers to show up instead of moving the"
	if len(first) != MaxLineLength {
		t.Fatalf("setup: first line is %d columns, want %d", len(first), MaxLineLength)
	}
	sess := testterm.NewSession(nil, first+" cursor.\x1a")
	ed := NewFSEditor(sess, io.Discard, ansi.OutputModeUTF8, 80, 24,
		"", "", "", "", "", "", nil)
	content, saved, err := ed.Run()
	if err != nil || !saved {
		t.Fatalf("Run: saved=%v err=%v", saved, err)
	}
	if want := first + "\ncursor."; content != want {
		t.Fatalf("content = %q, want %q", content, want)
	}
}

// TestRunCtrlKDeletesLine checks #419: Ctrl-K removes the whole current line,
// as Ctrl-Y does and as it does in Mystic.
func TestRunCtrlKDeletesLine(t *testing.T) {
	// Three lines, Up to the middle one, Ctrl-K, save.
	sess := testterm.NewSession(nil, "one\rtwo\rthree\x1b[A\x0b\x1a")
	ed := NewFSEditor(sess, io.Discard, ansi.OutputModeUTF8, 80, 24,
		"", "", "", "", "", "", nil)
	content, saved, err := ed.Run()
	if err != nil || !saved {
		t.Fatalf("Run: saved=%v err=%v", saved, err)
	}
	if want := "one\nthree"; content != want {
		t.Fatalf("content = %q, want %q", content, want)
	}
}
