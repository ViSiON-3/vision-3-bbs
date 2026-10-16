package menu

// strings.json values that run a script or display a file in place of their
// text (#605). The syntax is parsed by internal/stringhook; this file runs it.
//
// Every string output goes through writeString / writeStringf, which need only
// the terminal the call site already has. The rest of what a script door needs
// (the SSH session, the user, the node) is found from the terminal, which is
// created once per session in the session handler and registered here with
// RegisterStringHookSession.
//
// Output that may run off the session's own goroutine (a chat line printed by
// the receive loop, a page delivered while the caller types) or while a lock is
// held uses writeStringNoScript instead: a script there
// would read keys out from under the code that owns the input, or hold the
// lock for as long as the caller sat in it. Those print a !script value as
// text, as does a script value met while another one is already running.

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/gliderlabs/ssh"
	"golang.org/x/term"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/editor"
	"github.com/ViSiON-3/vision-3-bbs/internal/session"
	"github.com/ViSiON-3/vision-3-bbs/internal/stringhook"
	"github.com/ViSiON-3/vision-3-bbs/internal/terminalio"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// stringHookSession is what a terminal's string hooks need from its session.
type stringHookSession struct {
	s       ssh.Session
	userMgr *user.UserMgr
	bbs     *session.BbsSession
	// running is set while a string's script runs, so a string the script's
	// door path prints (a busy or error message) cannot start another.
	running atomic.Bool
}

// stringHookSessions maps a *term.Terminal to its *stringHookSession.
var stringHookSessions sync.Map

// RegisterStringHookSession records the session behind t so strings written
// to t can run scripts. The user is read from bbs when a script runs, so it
// follows the caller through login.
func RegisterStringHookSession(t *term.Terminal, s ssh.Session, userMgr *user.UserMgr, bbs *session.BbsSession) {
	stringHookSessions.Store(t, &stringHookSession{s: s, userMgr: userMgr, bbs: bbs})
}

// ClearStringHookSession drops t's session when the session ends.
func ClearStringHookSession(t *term.Terminal) {
	stringHookSessions.Delete(t)
}

func lookupStringHookSession(t *term.Terminal) *stringHookSession {
	if v, ok := stringHookSessions.Load(t); ok {
		return v.(*stringHookSession)
	}
	return nil
}

// stringHookWarned holds the hooks already logged as unable to run, so a
// misconfigured string shown on every menu does not flood the log.
var stringHookWarned sync.Map

func warnStringHookOnce(value, reason string, attrs ...any) {
	if _, seen := stringHookWarned.LoadOrStore(value+"\x00"+reason, struct{}{}); seen {
		return
	}
	slog.Warn("string hook printed as text: "+reason, append([]any{"value", value}, attrs...)...)
}

// writeString writes a strings.json value to the terminal: its text with pipe
// codes expanded, or the script or file it names (see stringhook).
func (e *MenuExecutor) writeString(terminal *term.Terminal, outputMode ansi.OutputMode, value string) error {
	return e.renderString(terminal, outputMode, value, true, nil)
}

// writeStringf is writeString for a value the call site formats with args.
// The hook is recognised before formatting, so a script receives the
// arguments instead of having them appended to its name.
func (e *MenuExecutor) writeStringf(terminal *term.Terminal, outputMode ansi.OutputMode, format string, args ...any) error {
	return e.renderString(terminal, outputMode, format, true, args)
}

// writeStringNoScript is writeString for output that must not run a script
// (see the file comment). A file value is still displayed.
func (e *MenuExecutor) writeStringNoScript(terminal *term.Terminal, outputMode ansi.OutputMode, value string) error {
	return e.renderString(terminal, outputMode, value, false, nil)
}

// renderString writes value, running its hook if it has one and allowScript
// permits. args is nil for a value printed verbatim, which is never passed to
// fmt: some verbatim strings hold a literal "%".
func (e *MenuExecutor) renderString(terminal *term.Terminal, outputMode ansi.OutputMode, value string, allowScript bool, args []any) error {
	h := stringhook.Parse(value)
	switch h.Kind {
	case stringhook.Escaped:
		return writeStringText(terminal, outputMode, formatStringValue(h.Rest, args))
	case stringhook.File:
		width, height := stringHookTermSize(terminal)
		if err := e.displayFileIfPresent(terminal, h.Target, outputMode, width, height); err != nil {
			warnStringHookOnce(value, "file not found", "file", h.Target, "error", err)
			if werr := writeStringText(terminal, outputMode, h.Prefix); werr != nil {
				return werr
			}
		}
		if h.Rest == "" {
			return nil
		}
		return writeStringText(terminal, outputMode, formatStringValue(h.Rest, args))
	case stringhook.Script:
		ran, err := e.runStringScript(terminal, outputMode, value, h, allowScript, args)
		if ran || err != nil {
			return err
		}
		// A script value has no verbs, so formatting it would only append
		// %!(EXTRA ...); it is printed as written.
		return writeStringText(terminal, outputMode, value)
	}
	return writeStringText(terminal, outputMode, formatStringValue(value, args))
}

// formatStringValue formats value with args, or returns it untouched when the
// call site passes none.
func formatStringValue(value string, args []any) string {
	if args == nil {
		return value
	}
	return fmt.Sprintf(value, args...)
}

func writeStringText(terminal *term.Terminal, outputMode ansi.OutputMode, text string) error {
	if text == "" {
		return nil
	}
	return terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(text)), outputMode)
}

// stringHookTermSize returns the caller's terminal size for a file shown in
// place of a string: the session's live size where it is known, else 80
// columns and an unknown height.
func stringHookTermSize(terminal *term.Terminal) (int, int) {
	if hs := lookupStringHookSession(terminal); hs != nil && hs.bbs != nil {
		if w, h := hs.bbs.TermSize(); w > 0 {
			return w, h
		}
	}
	return 80, 0
}

// runStringScript runs the V3 script door a !script value names. ran reports
// whether it did; when it did not, the caller prints the value as text. err is
// returned only for an error that ends the session (a hang-up, the idle
// timeout or the time limit), which the caller's next read would also report.
func (e *MenuExecutor) runStringScript(terminal *term.Terminal, outputMode ansi.OutputMode, value string, h stringhook.Hook, allowScript bool, args []any) (ran bool, err error) {
	if !allowScript {
		warnStringHookOnce(value, "scripts cannot run where this string is shown")
		return false, nil
	}
	hs := lookupStringHookSession(terminal)
	if hs == nil || hs.bbs == nil {
		warnStringHookOnce(value, "no session to run the script in")
		return false, nil
	}
	hs.bbs.Mutex.RLock()
	u := hs.bbs.User
	hs.bbs.Mutex.RUnlock()
	if u == nil {
		// Before login there is no user for the door's dropfile and ACS check.
		// Not logged: login screens are shown on every connection.
		return false, nil
	}

	name := strings.ToUpper(h.Target)
	doorConfig, ok := e.GetDoorConfig(name)
	if !ok {
		warnStringHookOnce(value, "no such door", "door", name)
		return false, nil
	}
	if doorConfig.Type != "v3_script" {
		warnStringHookOnce(value, "door is not a v3_script door", "door", name, "type", doorConfig.Type)
		return false, nil
	}
	// A missing script fails to load before running anything; print the value
	// rather than leave the string blank.
	if !stringScriptExists(doorConfig) {
		warnStringHookOnce(value, "script file not found", "door", name, "script", doorConfig.Script)
		return false, nil
	}
	if doorConfig.MinAccessLevel > 0 && u.AccessLevel < doorConfig.MinAccessLevel {
		// Expected for a string shown to every caller but scripted only for
		// some, so not a warning.
		slog.Debug("string script below caller's access level, printed as text",
			"door", name, "level", u.AccessLevel, "required", doorConfig.MinAccessLevel)
		return false, nil
	}
	if !hs.running.CompareAndSwap(false, true) {
		warnStringHookOnce(value, "a string script is already running", "door", name)
		return false, nil
	}
	defer hs.running.Store(false)

	// The script's arguments: the door's own, then the words after the door
	// name in the string, then the call site's formatted values.
	doorConfig.Args = append(append([]string(nil), doorConfig.Args...), h.Args...)
	for _, a := range args {
		doorConfig.Args = append(doorConfig.Args, fmt.Sprint(a))
	}

	width, height := stringHookTermSize(terminal)
	ctx := buildDoorCtx(e, borrowInputSession(hs.s), terminal,
		u.ID, u.Handle, u.RealName,
		u.AccessLevel, u.TimeLimit, u.TimesCalled,
		u.GroupLocation,
		width, height,
		hs.bbs.NodeID, hs.bbs.StartTime, outputMode,
		doorConfig, name)
	ctx.UserManager = hs.userMgr
	ctx.CurrentUser = u

	// executeDoor puts the snoop tap back in BBS mode afterwards; a string can
	// be shown from a screen in another mode (chat's pickers set teleconference
	// mode), so restore whatever was set before.
	var restoreTap func()
	if t := tapOf(hs.s); t != nil {
		mode := t.Mode()
		restoreTap = func() { t.SetMode(mode) }
	}
	runErr := executeDoor(ctx)
	if restoreTap != nil {
		restoreTap()
	}
	switch {
	case runErr == nil:
		return true, nil
	case isSessionFatal(runErr) || errors.Is(runErr, editor.ErrTimeLimit):
		// Most callers do not check this error, and their next read would
		// start a fresh idle interval. Latch it on the session's input
		// handler so that read ends the session instead.
		if v, ok := sessionInputHandlers.Load(hs.s); ok {
			v.(*editor.InputHandler).Fail(runErr)
		}
		return true, runErr
	case errors.Is(runErr, ErrDoorBusy):
		slog.Info("string script door busy, printed as text", "door", name, "node", hs.bbs.NodeID)
		return false, nil
	}
	slog.Error("string script failed", "door", name, "node", hs.bbs.NodeID, "error", runErr)
	return true, nil
}

// stringScriptExists reports whether a v3_script door's script file is there,
// resolved the way executeV3ScriptDoor resolves it.
func stringScriptExists(cfg config.DoorConfig) bool {
	if cfg.Script == "" {
		return false
	}
	dir := cfg.WorkingDirectory
	if dir == "" {
		dir = "."
	}
	path := cfg.Script
	if !filepath.IsAbs(path) {
		path = filepath.Join(dir, path)
	}
	fi, err := os.Stat(path)
	return err == nil && fi.Mode().IsRegular()
}

// borrowInputSession returns s for a script run in place of a string.
//
// A door normally stops the session's InputHandler first (resetSessionIH) so
// the two do not race for the caller's keys. A string can be printed by code
// that holds that handler and reads from it again afterwards; closing it under
// that code would hand it end-of-file and log the caller off. So when a handler
// is running, the script reads through it instead, and it is left open.
func borrowInputSession(s ssh.Session) ssh.Session {
	v, ok := sessionInputHandlers.Load(s)
	if !ok {
		return s
	}
	return &borrowedInputSession{Session: s, ih: v.(*editor.InputHandler)}
}

// borrowedInputSession is an ssh.Session whose reads come from the session's
// running InputHandler.
type borrowedInputSession struct {
	ssh.Session
	ih        *editor.InputHandler
	mu        sync.Mutex
	interrupt <-chan struct{}
}

// Read returns the input queued in the handler, waiting for at least one
// byte, or an error once the read interrupt fires.
func (b *borrowedInputSession) Read(p []byte) (int, error) {
	b.mu.Lock()
	stop := b.interrupt
	b.mu.Unlock()
	return b.ih.ReadUntil(p, stop)
}

// SetReadInterrupt sets the channel that stops a blocked Read, as the script
// engine does on its way out. A nil channel never fires.
func (b *borrowedInputSession) SetReadInterrupt(ch <-chan struct{}) {
	b.mu.Lock()
	b.interrupt = ch
	b.mu.Unlock()
}

// Unwrap returns the session underneath, for code keyed by it.
func (b *borrowedInputSession) Unwrap() ssh.Session { return b.Session }
