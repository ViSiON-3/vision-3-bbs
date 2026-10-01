package menu

import (
	"errors"
	"io"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/editor"
	"github.com/ViSiON-3/vision-3-bbs/internal/snoop"
	"github.com/ViSiON-3/vision-3-bbs/internal/terminalio"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
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

// ClearSessionIdleTimeout is clearSessionIdleTimeout for the session handler,
// which can set a timeout (SSH pre-auth, login sequence) before any menu runs
// and so before MenuExecutor.Run's own deferred cleanup is in place. It drops
// the session's time-limit deadline too.
func ClearSessionIdleTimeout(s ssh.Session) {
	clearSessionIdleTimeout(s)
	sessionDeadlines.Delete(s)
	sessionChatCredits.Delete(s)
}

// sessionDeadlines remembers when each session's time limit runs out, for the
// same reason as sessionIdleTimeouts: getSessionIH re-applies it to a
// recreated InputHandler. A session with no limit has no entry.
var sessionDeadlines sync.Map

// sessionChatCredits holds, per session, the time spent in sysop chat. Chat
// is not charged to the caller, so it is added to every deadline armed for
// the session. It outlives MenuExecutor.Run, which runs once per menu.
var sessionChatCredits sync.Map

func chatCredit(s ssh.Session) time.Duration {
	if v, ok := sessionChatCredits.Load(s); ok {
		return v.(time.Duration)
	}
	return 0
}

// addChatCredit credits d of chat to s and moves its recorded deadline out
// by d. The InputHandler's own deadline is moved by the break-in itself.
func addChatCredit(s ssh.Session, d time.Duration) {
	if d <= 0 {
		return
	}
	sessionChatCredits.Store(s, chatCredit(s)+d)
	if v, ok := sessionDeadlines.Load(s); ok {
		sessionDeadlines.Store(s, v.(time.Time).Add(d))
	}
}

// applySessionDeadline records the time-limit deadline for s, extended by
// its chat credit, and applies it to the current InputHandler. The zero time
// means no limit.
func applySessionDeadline(s ssh.Session, deadline time.Time) {
	if !deadline.IsZero() {
		deadline = deadline.Add(chatCredit(s))
	}
	if deadline.IsZero() {
		sessionDeadlines.Delete(s)
	} else {
		sessionDeadlines.Store(s, deadline)
	}
	getSessionIH(s).SetSessionDeadline(deadline)
}

// ApplyUserIdleTimeout applies u's idle timeout (including the SysOp
// exemption) to the session. The session handler calls it once a caller is
// authenticated, so the login sequence runs under their timeout rather than
// the pre-login one the login screens installed.
func (e *MenuExecutor) ApplyUserIdleTimeout(s ssh.Session, u *user.User) {
	applySessionIdleTimeout(s, e.idleTimeout(u))
}

// ApplyUserTimeLimit arms u's time limit (including the CoSysOp exemption)
// for a session that started at sessionStart. Like ApplyUserIdleTimeout, the
// session handler calls it once the caller is authenticated, so the login
// sequence counts against their time; Run re-arms it on every menu.
func (e *MenuExecutor) ApplyUserTimeLimit(s ssh.Session, u *user.User, sessionStart time.Time) {
	applySessionDeadline(s, e.sessionDeadline(u, sessionStart))
}

// sessionOutputModes remembers the negotiated ansi.OutputMode for each
// ssh.Session. readLineFromSessionIH and readLineFromSessionIHAllowAbort need
// this to decode keystroke bytes >= 128 correctly: CP437 terminals send one
// raw byte per glyph, while UTF-8 terminals send a multi-byte sequence one
// byte per ReadKey call. This mirrors sessionInputHandlers/sessionIdleTimeouts
// above rather than threading a parameter through readLineFromSessionIH,
// which has well over a hundred call sites.
var sessionOutputModes sync.Map

// SetSessionOutputMode records the output mode for s. Call this once the
// mode is finalized for the session (see cmd/vision3/main.go, alongside the
// other per-session setup) so later input reads decode extended characters
// with the same encoding the terminal is using for output.
func SetSessionOutputMode(s ssh.Session, mode ansi.OutputMode) {
	sessionOutputModes.Store(s, mode)
	if t := tapOf(s); t != nil {
		t.SetCP437(mode == ansi.OutputModeCP437)
	}
}

// tapOf returns the snoop tap carried by s, or nil.
func tapOf(s ssh.Session) *snoop.Tap {
	if tp, ok := s.(snoop.Tapped); ok {
		return tp.Tap()
	}
	return nil
}

// sessionOutputMode returns the recorded output mode for s, defaulting to
// ansi.OutputModeCP437 when unset. CP437 is the safe default: most users of
// this BBS are on CP437 terminals, and a CP437 byte is never mistaken for
// part of a UTF-8 continuation sequence (so getting this wrong for a UTF-8
// session degrades gracefully — see ansi.DecodeExtendedKey — while getting
// it wrong for a CP437 session the other way around does not).
func sessionOutputMode(s ssh.Session) ansi.OutputMode {
	if v, ok := sessionOutputModes.Load(s); ok {
		return v.(ansi.OutputMode)
	}
	return ansi.OutputModeCP437
}

// ClearSessionOutputMode drops the remembered output mode when a session
// ends, mirroring clearSessionIdleTimeout above so sessionOutputModes does
// not accumulate an entry per connection for the life of the process.
func ClearSessionOutputMode(s ssh.Session) {
	sessionOutputModes.Delete(s)
}

// sessionTermSizes carries a terminal size changed mid-session (the user
// Konfig editor) back to the menu loop, which otherwise keeps the size it was
// started with. Run takes it at the top of each iteration.
var sessionTermSizes sync.Map

type termSize struct{ width, height int }

// setSessionTermSize records a new terminal size for s.
func setSessionTermSize(s ssh.Session, width, height int) {
	sessionTermSizes.Store(s, termSize{width, height})
}

// takeSessionTermSize returns and forgets a size recorded by
// setSessionTermSize.
func takeSessionTermSize(s ssh.Session) (width, height int, ok bool) {
	v, ok := sessionTermSizes.LoadAndDelete(s)
	if !ok {
		return 0, 0, false
	}
	ts := v.(termSize)
	return ts.width, ts.height, true
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
	if d, ok := sessionDeadlines.Load(s); ok {
		ih.SetSessionDeadline(d.(time.Time))
	}
	if tap := tapOf(s); tap != nil {
		ih.SetBreakIn(tap.BreakIn(), func() { serviceSysopChat(s, ih, tap) })
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
	pausePrompt := e.Strings().PauseString
	if pausePrompt == "" {
		pausePrompt = "\r\n|07Press |15[ENTER]|07 to continue... "
	}
	_ = writeCenteredPausePrompt(s, terminal, pausePrompt, outputMode, termWidth, termHeight)
}

// readLineFromSessionIH reads a simple command line from the shared session
// InputHandler so menu input never races with other session readers.
func readLineFromSessionIH(s ssh.Session, terminal *term.Terminal) (string, error) {
	return readLineFromSessionIHImpl(s, terminal, false, 0, "")
}

// readLineFromSessionIHFrom reads a command line like readLineFromSessionIH,
// starting with initial already typed. It echoes initial itself.
func readLineFromSessionIHFrom(s ssh.Session, terminal *term.Terminal, initial string) (string, error) {
	return readLineFromSessionIHImpl(s, terminal, false, 0, initial)
}

// readLineFromSessionIHMax reads a simple command line like
// readLineFromSessionIH, but stops accepting characters once the line holds
// maxLen runes: further printable or extended keystrokes are dropped without
// echo, so the caller never sees a value longer than maxLen. A maxLen of 0
// means unlimited.
func readLineFromSessionIHMax(s ssh.Session, terminal *term.Terminal, maxLen int) (string, error) {
	return readLineFromSessionIHImpl(s, terminal, false, maxLen, "")
}

// readLineFromSessionIHAllowAbort reads a simple command line like
// readLineFromSessionIH, but returns errInputAborted when ESC is pressed.
func readLineFromSessionIHAllowAbort(s ssh.Session, terminal *term.Terminal) (string, error) {
	return readLineFromSessionIHImpl(s, terminal, true, 0, "")
}

// readLineFromSessionIHImpl is the shared implementation behind
// readLineFromSessionIH, readLineFromSessionIHMax, readLineFromSessionIHFrom
// and readLineFromSessionIHAllowAbort; they differ only in whether ESC aborts
// the read, whether the line length is capped (maxLen > 0, counted in runes)
// and whether it starts with text already typed.
//
// Extended keystrokes (byte >= 128) are decoded per the session's output
// mode via ansi.DecodeExtendedKey: a CP437 byte is a complete character on its
// own, while a UTF-8 byte may be one of several making up a single rune and
// is accumulated in utf8Pending across loop iterations until ansi.DecodeExtendedKey
// reports it complete. Backspace deletes one whole rune (see ansi.BackspaceRune)
// rather than one byte, and clears any in-progress utf8Pending sequence
// without touching line or echoing, since nothing was displayed for it yet.
func readLineFromSessionIHImpl(s ssh.Session, terminal *term.Terminal, allowAbort bool, maxLen int, initial string) (string, error) {
	ih := getSessionIH(s)
	mode := sessionOutputMode(s)
	line := make([]byte, 0, 64)
	if initial != "" {
		line = append(line, initial...)
		_, _ = terminal.Write([]byte(initial))
	}
	var utf8Pending []byte
	atLimit := func() bool {
		return maxLen > 0 && utf8.RuneCount(line) >= maxLen
	}

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
			if len(utf8Pending) > 0 {
				utf8Pending = nil
			} else if len(line) > 0 {
				line = ansi.BackspaceRune(line)
				_, _ = terminal.Write([]byte("\b \b"))
			}
		case editor.KeyEsc:
			if allowAbort {
				_, _ = terminal.Write([]byte("\r\n"))
				return "", errInputAborted
			}
			// Ignored (ESC has no other meaning here): drop any partial
			// UTF-8 sequence rather than let it survive to swallow whatever
			// comes next.
			utf8Pending = nil
		default:
			if atLimit() && ((key >= 32 && key < 127) || (key >= 128 && key <= 255)) {
				// Line is full: swallow the keystroke silently. Any partial
				// multi-byte sequence is abandoned too, since its remaining
				// bytes would otherwise be treated as fresh input.
				utf8Pending = nil
			} else if key >= 32 && key < 127 {
				line = append(line, byte(key))
				_, _ = terminal.Write([]byte{byte(key)})
				// A completed ASCII keystroke means any partial multi-byte
				// sequence still buffered belongs to a different, abandoned
				// character (e.g. a stray lead byte with no valid
				// continuation) and must not be left around to absorb the
				// bytes of the NEXT legitimate character.
				utf8Pending = nil
			} else if key >= 128 && key <= 255 {
				var echo []byte
				line, echo, utf8Pending = ansi.DecodeExtendedKey(line, mode, byte(key), utf8Pending)
				if len(echo) > 0 {
					_, _ = terminal.Write(echo)
				}
			} else {
				// Any other ignored key (e.g. an untranslated control code
				// or synthetic key we don't act on here): same reasoning as
				// the ASCII branch above.
				utf8Pending = nil
			}
		}
	}
}

// errInputAborted is returned by styledInput when the user presses ESC to cancel entry.
var errInputAborted = errors.New("input aborted")

// sessionOutputs remembers the writer each session's term.Terminal writes
// through (terminalio.CompatWriter, see cmd/vision3/main.go). Code that writes
// to the session without going through the terminal — the full-screen editor —
// uses it too, so its output gets the same translations as everything else.
var sessionOutputs sync.Map

// SetSessionOutput records w as the output writer for s.
func SetSessionOutput(s ssh.Session, w io.Writer) {
	sessionOutputs.Store(s, w)
}

// ClearSessionOutput drops the remembered writer when a session ends.
func ClearSessionOutput(s ssh.Session) {
	sessionOutputs.Delete(s)
}

// sessionOutput returns the writer registered for s, or s itself.
func sessionOutput(s ssh.Session) io.Writer {
	if v, ok := sessionOutputs.Load(s); ok {
		return v.(io.Writer)
	}
	return s
}
