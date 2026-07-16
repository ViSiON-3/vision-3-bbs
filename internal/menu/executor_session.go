package menu

import (
	"errors"
	"sync"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/editor"
	"github.com/ViSiON-3/vision-3-bbs/internal/terminalio"
	"github.com/gliderlabs/ssh"
	"golang.org/x/term"
)

// sessionInputHandlers stores a single *editor.InputHandler per ssh.Session.
// A background goroutine inside InputHandler reads raw bytes from the session
// into a channel; lightbar menus and the full-screen editor both read from that
// channel. This prevents orphaned goroutines from consuming keystrokes after
// the editor exits, which caused the "double key press" bug on return to a menu.
var sessionInputHandlers sync.Map

// sessionIdleTimeouts remembers the session-level idle timeout for each
// ssh.Session so getSessionIH can re-apply it whenever the InputHandler is
// recreated (doors and zmodem call resetSessionIH; without this the recreated
// handler silently lost the timeout and the user could idle forever).
var sessionIdleTimeouts sync.Map

// applySessionIdleTimeout records the idle timeout for s and applies it to the
// current InputHandler. It survives resetSessionIH: recreated handlers get the
// same timeout.
func applySessionIdleTimeout(s ssh.Session, d time.Duration) {
	sessionIdleTimeouts.Store(s, d)
	getSessionIH(s).SetSessionIdleTimeout(d)
}

// clearSessionIdleTimeout drops the remembered timeout when a session ends.
func clearSessionIdleTimeout(s ssh.Session) {
	sessionIdleTimeouts.Delete(s)
}

// getSessionIH returns (creating if necessary) the session-scoped InputHandler
// for s. All callers within the same session share a single goroutine that
// reads from the ssh.Session, so bytes are never lost when control passes
// between the lightbar, message reader, scan, and full-screen editor.
func getSessionIH(s ssh.Session) *editor.InputHandler {
	if v, ok := sessionInputHandlers.Load(s); ok {
		return v.(*editor.InputHandler)
	}
	ih := editor.NewInputHandler(s)
	if d, ok := sessionIdleTimeouts.Load(s); ok {
		ih.SetSessionIdleTimeout(d.(time.Duration))
	}
	sessionInputHandlers.Store(s, ih)
	return ih
}

// resetSessionIH stops and removes any session-scoped InputHandler for s.
// Use this before flows that must read from ssh.Session directly (doors/zmodem),
// then recreate via getSessionIH(s) after returning to menu input.
// CloseAndWait is used to ensure the goroutine's deferred setReadInterrupt(nil)
// has run before the door installs its own SetReadInterrupt, preventing the race
// where the handler's cleanup clears the door's interrupt channel.
func resetSessionIH(s ssh.Session) {
	if v, ok := sessionInputHandlers.Load(s); ok {
		if ih, ok := v.(*editor.InputHandler); ok {
			ih.CloseAndWait()
		}
		sessionInputHandlers.Delete(s)
	}
}

type cursorHideContext int

const (
	cursorHideContextDefault cursorHideContext = iota
	cursorHideContextPromptYesNo
)

// shouldHideCursorForSoftwareKeyboard returns true when the cursor should be
// hidden. Default contexts (lightbar menus, admin lists) hide the cursor;
// promptYesNoLightbar keeps it visible so iOS/MuffinTerm software keyboards
// remain active.
func (e *MenuExecutor) shouldHideCursorForSoftwareKeyboard(ctx cursorHideContext) bool {
	switch ctx {
	case cursorHideContextPromptYesNo:
		return false
	default:
		return true
	}
}

func (e *MenuExecutor) hideCursorIfNeeded(terminal *term.Terminal, outputMode ansi.OutputMode, ctx cursorHideContext) bool {
	if !e.shouldHideCursorForSoftwareKeyboard(ctx) {
		return false
	}
	_ = terminalio.WriteProcessedBytes(terminal, []byte("\x1b[?25l"), outputMode)
	return true
}

func (e *MenuExecutor) showCursorIfHidden(terminal *term.Terminal, outputMode ansi.OutputMode, hidden bool) {
	if hidden {
		_ = terminalio.WriteProcessedBytes(terminal, []byte("\x1b[?25h"), outputMode)
	}
}

// holdScreen displays the configured PauseString (centered) and waits for the
// user to press Enter before continuing. Matches Pascal HoldScreen behaviour.
func (e *MenuExecutor) holdScreen(s ssh.Session, terminal *term.Terminal, outputMode ansi.OutputMode, termWidth, termHeight int) {
	pausePrompt := e.LoadedStrings.PauseString
	if pausePrompt == "" {
		pausePrompt = "\r\n|07Press |15[ENTER]|07 to continue... "
	}
	_ = writeCenteredPausePrompt(s, terminal, pausePrompt, outputMode, termWidth, termHeight)
}

// readLineFromSessionIH reads a simple command line from the shared session
// InputHandler so menu input never races with other session readers.
func readLineFromSessionIH(s ssh.Session, terminal *term.Terminal) (string, error) {
	ih := getSessionIH(s)
	line := make([]byte, 0, 64)

	for {
		key, err := ih.ReadKey()
		if err != nil {
			return "", err
		}

		switch key {
		case editor.KeyEnter:
			_, _ = terminal.Write([]byte("\r\n"))
			return string(line), nil
		case editor.KeyBackspace:
			if len(line) > 0 {
				line = line[:len(line)-1]
				_, _ = terminal.Write([]byte("\b \b"))
			}
		default:
			if key >= 32 && key < 127 {
				line = append(line, byte(key))
				_, _ = terminal.Write([]byte{byte(key)})
			}
		}
	}
}

// readLineFromSessionIHAllowAbort reads a simple command line like
// readLineFromSessionIH, but returns errInputAborted when ESC is pressed.
func readLineFromSessionIHAllowAbort(s ssh.Session, terminal *term.Terminal) (string, error) {
	ih := getSessionIH(s)
	line := make([]byte, 0, 64)

	for {
		key, err := ih.ReadKey()
		if err != nil {
			return "", err
		}

		switch key {
		case editor.KeyEnter:
			_, _ = terminal.Write([]byte("\r\n"))
			return string(line), nil
		case editor.KeyBackspace:
			if len(line) > 0 {
				line = line[:len(line)-1]
				_, _ = terminal.Write([]byte("\b \b"))
			}
		case editor.KeyEsc:
			_, _ = terminal.Write([]byte("\r\n"))
			return "", errInputAborted
		default:
			if key >= 32 && key < 127 {
				line = append(line, byte(key))
				_, _ = terminal.Write([]byte{byte(key)})
			}
		}
	}
}

// errInputAborted is returned by styledInput when the user presses ESC to cancel entry.
var errInputAborted = errors.New("input aborted")
