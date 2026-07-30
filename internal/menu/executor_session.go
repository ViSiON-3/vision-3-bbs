package menu

import (
	"errors"
	"sync"
	"time"
	"unicode/utf8"

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
}

// sessionOutputMode returns the recorded output mode for s, defaulting to
// ansi.OutputModeCP437 when unset. CP437 is the safe default: most users of
// this BBS are on CP437 terminals, and a CP437 byte is never mistaken for
// part of a UTF-8 continuation sequence (so getting this wrong for a UTF-8
// session degrades gracefully — see decodeExtendedKey below — while getting
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

// decodeExtendedKey processes one keystroke byte b (128-255) according to
// mode, returning the line with the decoded character appended (if any), the
// raw bytes to echo back to the terminal (if any), and the updated
// utf8Pending accumulator for the next call.
//
// CP437 mode is a single-byte table lookup: b maps through
// ansi.Cp437ToUnicode to exactly one rune, which is appended to line as
// UTF-8 (so callers only ever store valid UTF-8) while the RAW byte b is
// echoed unchanged -- a CP437 terminal draws directly from the byte value,
// so echoing anything else would not round-trip. A byte with no mapping
// (Cp437ToUnicode[b] == 0) is dropped: it is not stored, matching how the
// reader already drops any other keystroke it won't accept, and it avoids
// ever writing invalid/unintended data into users.json.
//
// UTF-8 mode receives one byte of a multi-byte sequence per call (that is
// how bytes >= 128 arrive from ReadKey on a real connection), so b is
// accumulated into utf8Pending until utf8.FullRune reports a complete
// sequence (or 4 bytes -- utf8.UTFMax, the longest possible UTF-8 encoding --
// have accumulated without one, which guards against a malformed sequence
// that would otherwise never complete and silently swallow all subsequent
// input). Once complete, the sequence is appended to line and echoed
// verbatim.
//
// A malformed sequence is not stored or echoed, and the decoder resynchronises
// by discarding one byte at a time rather than the whole accumulator: a stray
// lead byte is often immediately followed by a real character's lead byte, and
// dropping the buffer wholesale would swallow that character too.
func decodeExtendedKey(line []byte, mode ansi.OutputMode, b byte, utf8Pending []byte) (newLine []byte, echo []byte, pending []byte) {
	if mode == ansi.OutputModeUTF8 {
		utf8Pending = append(utf8Pending, b)
		for len(utf8Pending) > 0 {
			if !utf8.FullRune(utf8Pending) && len(utf8Pending) < utf8.UTFMax {
				return line, nil, utf8Pending
			}
			r, size := utf8.DecodeRune(utf8Pending)
			if r == utf8.RuneError && size <= 1 {
				// Malformed: drop one byte and retry, so a stray lead byte does
				// not take the following character down with it. Discarding the
				// whole buffer would swallow a valid lead byte sitting behind it.
				utf8Pending = utf8Pending[1:]
				continue
			}
			seq := append([]byte(nil), utf8Pending[:size]...)
			return append(line, seq...), seq, nil
		}
		return line, nil, nil
	}
	r := ansi.Cp437ToUnicode[b]
	if r == 0 {
		return line, nil, utf8Pending
	}
	var buf [utf8.UTFMax]byte
	n := utf8.EncodeRune(buf[:], r)
	return append(line, buf[:n]...), []byte{b}, utf8Pending
}

// backspaceRune removes the last RUNE (not byte) from line. line always holds
// valid UTF-8 (ASCII, or a CP437/UTF-8 extended character decoded via
// decodeExtendedKey above), so a byte-based backspace would cut a multi-byte
// character in half. The caller still echoes "\b \b" exactly once, matching
// the single terminal column every stored character occupies.
func backspaceRune(line []byte) []byte {
	if len(line) == 0 {
		return line
	}
	_, size := utf8.DecodeLastRune(line)
	return line[:len(line)-size]
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
	return readLineFromSessionIHImpl(s, terminal, false)
}

// readLineFromSessionIHAllowAbort reads a simple command line like
// readLineFromSessionIH, but returns errInputAborted when ESC is pressed.
func readLineFromSessionIHAllowAbort(s ssh.Session, terminal *term.Terminal) (string, error) {
	return readLineFromSessionIHImpl(s, terminal, true)
}

// readLineFromSessionIHImpl is the shared implementation behind
// readLineFromSessionIH and readLineFromSessionIHAllowAbort; the two differ
// only in whether ESC aborts the read.
//
// Extended keystrokes (byte >= 128) are decoded per the session's output
// mode via decodeExtendedKey: a CP437 byte is a complete character on its
// own, while a UTF-8 byte may be one of several making up a single rune and
// is accumulated in utf8Pending across loop iterations until decodeExtendedKey
// reports it complete. Backspace deletes one whole rune (see backspaceRune)
// rather than one byte, and clears any in-progress utf8Pending sequence
// without touching line or echoing, since nothing was displayed for it yet.
func readLineFromSessionIHImpl(s ssh.Session, terminal *term.Terminal, allowAbort bool) (string, error) {
	ih := getSessionIH(s)
	mode := sessionOutputMode(s)
	line := make([]byte, 0, 64)
	var utf8Pending []byte

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
				line = backspaceRune(line)
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
			if key >= 32 && key < 127 {
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
				line, echo, utf8Pending = decodeExtendedKey(line, mode, byte(key), utf8Pending)
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
