package menu

import (
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/gliderlabs/ssh"
	"golang.org/x/term"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/editor"
	"github.com/ViSiON-3/vision-3-bbs/internal/session"
	"github.com/ViSiON-3/vision-3-bbs/internal/snoop"
	"github.com/ViSiON-3/vision-3-bbs/internal/terminalio"
	"github.com/ViSiON-3/vision-3-bbs/internal/toast"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// Toasts reach a caller in one of two ways. While a read waits for a key, the
// screen's owner is idle, so the input handler's toast hook draws the toast on
// the configured position and replays the tap's catch-up buffer to restore the screen.
// At a menu prompt, or at login, toasts are written inline and bodies are
// shown. Whatever cannot be drawn yet stays queued for the next of those.

// toastDwell is how long an overlay stays up when no key clears it.
var toastDwell = 5 * time.Second

// toastRearm is how long a key that cleared an overlay holds off the next one,
// so a queue of toasts does not flash past someone who is typing.
var toastRearm = time.Second

// toastHook is what getSessionIH needs to give a new handler the toast hook.
type toastHook struct {
	wake <-chan struct{}
	run  func(ih *editor.InputHandler)
}

// sessionToastHooks holds the toast hook of each attached session.
var sessionToastHooks sync.Map

// applyToastHook gives ih the toast hook of s, if it has one.
func applyToastHook(s ssh.Session, ih *editor.InputHandler) {
	if v, ok := sessionToastHooks.Load(unwrapSession(s)); ok {
		h := v.(toastHook)
		ih.SetToastHook(h.wake, func() { h.run(ih) })
	}
}

// AttachToasts lets s show toasts for sess during key waits. Call it on the
// session's goroutine.
func (e *MenuExecutor) AttachToasts(s ssh.Session, sess *session.BbsSession) {
	sessionToastHooks.Store(unwrapSession(s), toastHook{
		wake: sess.ToastWake(),
		run:  func(ih *editor.InputHandler) { e.serviceToasts(s, ih, sess) },
	})
	if v, ok := sessionInputHandlers.Load(s); ok {
		applyToastHook(s, v.(*editor.InputHandler))
	}
}

// DetachToasts drops what AttachToasts stored when a session ends.
func DetachToasts(s ssh.Session) { sessionToastHooks.Delete(unwrapSession(s)) }

func (e *MenuExecutor) toastStore() *toast.Store {
	return toast.Open(e.GetServerConfig().DataDir)
}

// toastLine is a toast's text flattened to one line: its own control
// characters could otherwise move the cursor or scroll the screen.
func toastLine(text string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7F {
			return ' '
		}
		return r
	}, text)
}

// toastBody prepares a body for the screen. It is a caller's own text, so pipe
// codes stay literal and control characters other than line ends are dropped.
func toastBody(body string) []byte {
	body = strings.ReplaceAll(body, "\r\n", "\n")
	body = strings.Map(func(r rune) rune {
		if r == '\n' {
			return r
		}
		if r < 0x20 || r == 0x7F {
			return -1
		}
		return r
	}, body)
	return []byte(strings.ReplaceAll(body, "\n", "\r\n") + "\r\n")
}

// nextOverlayToast returns the next toast to draw over the screen, claimed if
// it came from the store. Live toasts go first: they are the ones that go
// stale. A toast with a body is drawn once; its body waits for a menu.
func (e *MenuExecutor) nextOverlayToast(sess *session.BbsSession) (t toast.Toast, stored, ok bool) {
	if page, ok := sess.PeekPage(); ok {
		return toast.Toast{Text: page}, false, true
	}
	sess.Mutex.RLock()
	u := sess.User
	sess.Mutex.RUnlock()
	if u == nil {
		return t, false, false
	}
	store := e.toastStore()
	queue, err := store.Peek(u.ID)
	if err != nil {
		slog.Warn("failed to read toasts", "node", sess.NodeID, "error", err)
		return t, false, false
	}
	for _, q := range queue {
		if q.Body != "" && sess.ToastAnnounced(q.ID) {
			continue
		}
		if store.Claim(q.ID) {
			return q, true, true
		}
	}
	return t, false, false
}

// serviceToasts is the input handler's toast hook. It runs on the reading
// goroutine, so the screen's owner is waiting for this key and draws nothing
// meanwhile. Only ModeBBS screens qualify: teleconference draws from a second
// goroutine, and doors and transfers do not read through this hook at all.
func (e *MenuExecutor) serviceToasts(s ssh.Session, ih *editor.InputHandler, sess *session.BbsSession) {
	tap := tapOf(s)
	v, hasOut := sessionOutputs.Load(s)
	if !sess.ToastsReady() || tap == nil || !hasOut || tap.Mode() != snoop.ModeBBS {
		return
	}
	w := v.(io.Writer)
	for {
		// The snapshot must predate the overlay, or the replay would redraw it.
		// One that has overflowed cannot restore the screen, so the toast waits
		// for the next menu prompt.
		snap, overflowed := tap.Snapshot()
		if overflowed {
			return
		}
		t, stored, ok := e.nextOverlayToast(sess)
		if !ok {
			return
		}
		err := e.drawToastOverlay(s, w, sess, t)
		if err == nil {
			e.settleOverlayToast(sess, t, stored)
		}
		if stored {
			e.toastStore().Release(t.ID)
		}
		if err != nil {
			return
		}
		keyed := ih.WaitForInput(toastDwell)
		// Same reset as the end of sysop chat: the snapshot carries its own
		// scroll region, colours and cursor position.
		_, _ = fmt.Fprint(w, "\x1b[r\x1b[0m\x1b[2J\x1b[H") // best-effort display
		if _, err := w.Write(snap); err != nil {
			return
		}
		if keyed {
			time.AfterFunc(toastRearm, sess.WakeToasts)
			return
		}
	}
}

// toastAnchorCode is the coordinate code that places toasts from menu art:
// ~TO or |TO.
const toastAnchorCode = "TO"

// setToastAnchor places toasts where this menu's art has the anchor code. Art
// coordinates count from the top of the art, so they only match the screen when
// the menu clears it first; otherwise the configured position stands.
func (e *MenuExecutor) setToastAnchor(nodeNumber int, res ansi.ProcessAnsiResult, clearsScreen bool) {
	sess := e.nodeSession(nodeNumber)
	if sess == nil {
		return
	}
	if pos, ok := res.FieldCoords[toastAnchorCode]; ok && clearsScreen {
		sess.SetToastAnchor(pos.Y, pos.X)
		return
	}
	sess.ClearToastAnchor()
}

// clearToastAnchor ends setToastAnchor when the menu stops waiting for input.
func (e *MenuExecutor) clearToastAnchor(nodeNumber int) {
	if sess := e.nodeSession(nodeNumber); sess != nil {
		sess.ClearToastAnchor()
	}
}

// toastMinCols is how many columns a toast keeps when its configured column
// would otherwise leave it too little room.
const toastMinCols = 20

// toastEdgeRow is the row offset rows in from the configured edge. An offset
// too big for this terminal stops at the opposite edge.
func toastEdgeRow(edge string, offset, height int) int {
	offset = max(0, offset)
	if edge == config.ToastEdgeTop {
		return min(1+offset, height)
	}
	return max(height-offset, 1)
}

// toastCenterCol is the column that centers textLen cells on the caller's Konfig
// screen width. It uses the terminal's width when Konfig has none, or when Konfig
// claims more than the terminal has, so the text is never cut short by centering.
func toastCenterCol(textLen, konfigWidth, termWidth int) int {
	width := termWidth
	if konfigWidth > 0 && konfigWidth < termWidth {
		width = konfigWidth
	}
	return max(1, (width-textLen)/2+1)
}

// toastPlacement fits a position to this terminal. A row past the bottom is
// drawn there, and a column too far right moves left so toastMinCols still fit.
func toastPlacement(cfgRow, cfgCol, width, height int) (row, col int) {
	row = max(1, min(cfgRow, height))
	col = max(1, min(cfgCol, width-toastMinCols))
	return row, col
}

// drawToastOverlay writes t at the configured position, leaving the cursor and colours
// where they were.
func (e *MenuExecutor) drawToastOverlay(s ssh.Session, w io.Writer, sess *session.BbsSession, t toast.Toast) error {
	width, height := sess.TermSize()
	if width <= 0 {
		width = 80
	}
	if height <= 0 {
		height = 24
	}
	st := e.Strings()
	text := toastLine(t.Text)
	if t.Body != "" {
		text += st.ToastBodyHint
	}
	if st.ToastOverlayFormat != "" {
		text = fmt.Sprintf(st.ToastOverlayFormat, text)
	}
	cfg := e.GetServerConfig()
	line := string(ansi.ReplacePipeCodes([]byte(text)))
	cfgRow, cfgCol := toastEdgeRow(cfg.ToastEdge, cfg.ToastOffset, height), cfg.ToastCol
	if cfg.ToastCenter {
		konfigWidth := 0
		sess.Mutex.RLock()
		if sess.User != nil {
			konfigWidth = sess.User.ScreenWidth
		}
		sess.Mutex.RUnlock()
		cfgCol = toastCenterCol(ansi.VisibleLength(line), konfigWidth, width)
	}
	if r, c, ok := sess.ToastAnchor(); ok {
		cfgRow, cfgCol = r, c
	}
	row, col := toastPlacement(cfgRow, cfgCol, width, height)
	// One column short: a character in the last cell of the last row can scroll
	// the screen on some terminals.
	line = ansi.TruncateVisible(line, width-col)
	out := "\x1b7" + ansi.MoveCursor(row, col) + "\x1b[0m\x1b[K" + line + "\x1b[0m\x1b8"
	return terminalio.WriteProcessedBytes(w, []byte(out), sessionOutputMode(s))
}

// settleOverlayToast records that t has been drawn: it leaves its queue, unless
// its body is still to be shown.
func (e *MenuExecutor) settleOverlayToast(sess *session.BbsSession, t toast.Toast, stored bool) {
	if !stored {
		sess.PopPage()
		return
	}
	if t.Body != "" {
		sess.NoteToastAnnounced(t.ID)
		return
	}
	sess.Mutex.RLock()
	u := sess.User
	sess.Mutex.RUnlock()
	if u == nil {
		return
	}
	if err := e.toastStore().Remove(u.ID, t.ID); err != nil {
		slog.Warn("failed to remove shown toast", "node", sess.NodeID, "error", err)
	}
}

// deliverToastsInline writes everything waiting for this node's session at the
// cursor, in the order sent. Each stored toast leaves the queue only after it
// is written, so a dropped call keeps the rest.
func (e *MenuExecutor) deliverToastsInline(s ssh.Session, terminal *term.Terminal, nodeNumber int, outputMode ansi.OutputMode) (wrote bool) {
	sess := e.nodeSession(nodeNumber)
	if sess == nil {
		return false
	}
	for {
		page, ok := sess.PeekPage()
		if !ok {
			break
		}
		if err := terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte("\r\n"+page+"\r\n")), outputMode); err != nil {
			slog.Error("failed to deliver page", "node", nodeNumber, "error", err)
			return wrote
		}
		sess.PopPage()
		wrote = true
	}
	sess.Mutex.RLock()
	u := sess.User
	sess.Mutex.RUnlock()
	if u == nil {
		return wrote
	}
	store := e.toastStore()
	queue, err := store.Peek(u.ID)
	if err != nil {
		slog.Warn("failed to read toasts", "node", nodeNumber, "error", err)
		return wrote
	}
	for _, t := range queue {
		if !store.Claim(t.ID) {
			continue
		}
		out := ansi.ReplacePipeCodes([]byte("\r\n" + toastLine(t.Text) + "\r\n"))
		if t.Body != "" {
			out = append(out, toastBody(t.Body)...)
		}
		err := terminalio.WriteProcessedBytes(terminal, out, outputMode)
		if err == nil {
			wrote = true
			if rmErr := store.Remove(u.ID, t.ID); rmErr != nil {
				slog.Warn("failed to remove shown toast", "node", nodeNumber, "error", rmErr)
			}
		}
		store.Release(t.ID)
		if err != nil {
			slog.Error("failed to deliver toast", "node", nodeNumber, "error", err)
			return wrote
		}
	}
	return wrote
}

// deliverToastBodies shows any queued toast with a body and holds the screen
// so it can be read. Boards with only lightbar menus have no prompt to wait
// for, so this runs between menus. Toasts without a body stay for the overlay.
func (e *MenuExecutor) deliverToastBodies(s ssh.Session, terminal *term.Terminal, nodeNumber int, outputMode ansi.OutputMode, termWidth, termHeight int) {
	sess := e.nodeSession(nodeNumber)
	if sess == nil || !sess.ToastsReady() {
		return
	}
	sess.Mutex.RLock()
	u := sess.User
	sess.Mutex.RUnlock()
	if u == nil {
		return
	}
	queue, err := e.toastStore().Peek(u.ID)
	if err != nil {
		return
	}
	for _, t := range queue {
		if t.Body != "" {
			if e.deliverToastsInline(s, terminal, nodeNumber, outputMode) {
				e.holdScreen(s, terminal, outputMode, termWidth, termHeight)
			}
			return
		}
	}
}

// DeliverLoginToasts shows what was queued while the caller was away, and holds
// the screen so the first menu does not wipe it.
func (e *MenuExecutor) DeliverLoginToasts(s ssh.Session, terminal *term.Terminal, u *user.User, nodeNumber int, outputMode ansi.OutputMode, termWidth, termHeight int) {
	if u == nil {
		return
	}
	if e.deliverToastsInline(s, terminal, nodeNumber, outputMode) {
		e.holdScreen(s, terminal, outputMode, termWidth, termHeight)
	}
}
