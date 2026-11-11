package menu

import (
	"bytes"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/gliderlabs/ssh"
	"golang.org/x/term"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/editor"
	"github.com/ViSiON-3/vision-3-bbs/internal/editor/testterm"
	"github.com/ViSiON-3/vision-3-bbs/internal/session"
	"github.com/ViSiON-3/vision-3-bbs/internal/snoop"
	"github.com/ViSiON-3/vision-3-bbs/internal/terminalio"
	"github.com/ViSiON-3/vision-3-bbs/internal/toast"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// Toasts reach a caller in one of two ways. While a read waits for a key, the
// screen's owner is idle, so the input handler's toast hook draws the toast on
// the configured position and restores the screen: by its own redraw if it has
// one, otherwise by replaying the tap's catch-up buffer. Teleconference takes
// them as lines in its scroll region instead.
// At a menu prompt, or at login, toasts are written inline and bodies are
// shown. Whatever cannot be drawn yet stays queued for the next of those, and so
// does a toast too long for its row, once its first row has been drawn.

// toastDwell is how long an overlay stays up when no key clears it.
var toastDwell = 5 * time.Second

// toastRearm is how long a key that cleared an overlay holds off the next one,
// so a queue of toasts does not flash past someone who is typing.
var toastRearm = time.Second

// toastHook is what getSessionIH needs to give a new handler the toast hook.
type toastHook struct {
	wake <-chan struct{}
	run  func(ih *editor.InputHandler, limit time.Duration)
}

// sessionToastHooks holds the toast hook of each attached session.
var sessionToastHooks sync.Map

// applyToastHook gives ih the toast hook of s, if it has one.
func applyToastHook(s ssh.Session, ih *editor.InputHandler) {
	if v, ok := sessionToastHooks.Load(unwrapSession(s)); ok {
		h := v.(toastHook)
		ih.SetToastHook(h.wake, func(limit time.Duration) { h.run(ih, limit) })
	}
}

// AttachToasts lets s show toasts for sess during key waits. Call it on the
// session's goroutine.
func (e *MenuExecutor) AttachToasts(s ssh.Session, sess *session.BbsSession) {
	sessionToastHooks.Store(unwrapSession(s), toastHook{
		wake: sess.ToastWake(),
		run: func(ih *editor.InputHandler, limit time.Duration) {
			e.serviceToasts(s, ih, sess, limit)
		},
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
// characters could otherwise move the cursor or scroll the screen. The C1 range
// is included because U+009B is the 8-bit form of CSI.
func toastLine(text string) string {
	return ansi.OneLine(text)
}

// toastPipes expands a toast's pipe codes. Its text comes from another caller,
// so only colours apply: a page must not be able to clear or scroll the screen.
func toastPipes(text string) []byte {
	return ansi.ReplacePipeCodes([]byte(ansi.EscapeScreenPipeCodes(text)))
}

// nextOverlayToast returns the next toast to draw over the screen, claimed if
// it came from the store. Live toasts go first: they are the ones that go
// stale. A toast is drawn once: one too long for the row waits for a menu to
// show the rest, and one whose removal failed is not drawn again.
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
		if sess.ToastAnnounced(q.ID) || sess.ToastShown(q.ID) {
			continue
		}
		if store.Claim(u.ID, q.ID) {
			return q, true, true
		}
	}
	return t, false, false
}

// serviceToasts is the input handler's toast hook. It runs on the reading
// goroutine, so the screen's owner is waiting for this key and draws nothing
// meanwhile. Only ModeBBS screens get the overlay: teleconference draws from a
// second goroutine, and doors and transfers do not read through this hook at all.
//
// It shows one toast and wakes itself for the next, so the read can check its
// idle timeout and time limit in between. limit is what remains of the read's
// wait, or 0 for no bound; the toast comes down in that time.
func (e *MenuExecutor) serviceToasts(s ssh.Session, ih *editor.InputHandler, sess *session.BbsSession, limit time.Duration) {
	// Everything here, the snapshot and the lookup as well as the frames
	// and waits, ends by this deadline, so slow output cannot stretch a
	// toast past the read's own limit.
	dwell := toastDwell
	if limit > 0 && limit < dwell {
		dwell = limit
	}
	deadline := time.Now().Add(dwell)
	tap := tapOf(s)
	if !sess.ToastsReady() || tap == nil {
		return
	}
	if tap.Mode() == snoop.ModeTeleconf {
		e.serviceToastLines(s, sess)
		return
	}
	v, hasOut := sessionOutputs.Load(s)
	if !hasOut || tap.Mode() != snoop.ModeBBS {
		return
	}
	w := v.(io.Writer)
	// The snapshot must predate the overlay, or the replay would redraw it.
	// One that has overflowed cannot restore the screen, so without a redraw
	// of its own the toast waits for the next menu (see rearmToasts).
	redraw := ih.ScreenRedraw()
	var snap []byte
	if redraw == nil {
		var overflowed bool
		if snap, overflowed = tap.Snapshot(); overflowed {
			ih.HoldToasts()
			return
		}
	}
	t, stored, ok := e.nextOverlayToast(sess)
	if !ok {
		return
	}
	// A fading toast comes in and goes out through the theme's ramp, inside
	// the same dwell; one too short for the fade, or with fading off, just
	// appears and goes.
	ramp, step := e.toastFade()
	if fadeTime := time.Duration(2*len(ramp)) * step; len(ramp) == 0 || fadeTime >= time.Until(deadline) {
		ramp = nil
	}
	fadeOut := time.Duration(len(ramp)) * step
	lay := e.toastLayout(s, sess, t)
	if time.Until(deadline) <= 0 {
		// Getting here took the read's time: draw nothing, leave the toast
		// queued, and let the next wait show it.
		if stored {
			e.toastStore().Release(t.ID)
		}
		sess.WakeToasts()
		return
	}
	first := lay.line
	if len(ramp) > 0 {
		first = lay.frame(ramp[0])
	}
	err := lay.write(w, first)
	if err == nil {
		e.settleOverlayToast(sess, t, stored, lay.clipped)
	}
	if stored {
		e.toastStore().Release(t.ID)
	}
	if err != nil {
		return
	}
	row := lay.row
	// A key, or a sysop's chat request or a window change (which the read
	// services once this returns), ends any wait here: the fade stops where
	// it is and the row goes straight back.
	ended := fadeToast(ih, lay, w, ramp, step, true, deadline)
	if ended == editor.WaitElapsed && time.Until(deadline) > 0 && (len(ramp) == 0 || lay.write(w, lay.line) == nil) {
		// Hold for what is left, keeping the fade-out's share.
		if hold := time.Until(deadline) - fadeOut; hold > 0 {
			ended = ih.WaitFor(hold)
		}
		if ended == editor.WaitElapsed {
			ended = fadeToast(ih, lay, w, reversed(ramp), step, false, deadline)
		}
	}
	keyed := ended == editor.WaitInput
	if redraw != nil {
		redraw()
	} else if !e.restoreOverlayRow(s, w, sess, snap, row) {
		// Same reset as the end of sysop chat: the snapshot carries its own
		// scroll region, colours and cursor position.
		_, _ = fmt.Fprint(w, "\x1b[r\x1b[0m\x1b[2J\x1b[H") // best-effort display
		if _, err := w.Write(snap); err != nil {
			return
		}
	}
	if keyed {
		time.AfterFunc(toastRearm, sess.WakeToasts)
		return
	}
	sess.WakeToasts()
}

// restoreOverlayRow redraws only the row an overlay covered, from a model of the
// screen built by replaying snap through a terminal of the session's size, and
// leaves the pen as the screen had it. That is a few hundred bytes where the
// replay of the whole snapshot is the whole screen again, which on a slow link
// is a visible redraw at every toast.
//
// It reports false, having written nothing, when the model cannot be trusted:
// snap holds a sequence the model does not handle or an operation it is in
// doubt about (a scroll, a character whose width on the caller's terminal it
// does not know), or the last cell of the last row is in use, which some
// terminals scroll for. The caller then replays snap.
func (e *MenuExecutor) restoreOverlayRow(s ssh.Session, w io.Writer, sess *session.BbsSession, snap []byte, row int) bool {
	width, height := sess.TermSize()
	if width <= 0 || height <= 0 || row < 1 || row > height {
		return false
	}
	mode := sessionOutputMode(s)
	var opts []testterm.Option
	if mode == ansi.OutputModeCP437 {
		opts = append(opts, testterm.CP437())
	}
	model := testterm.New(width, height, opts...)
	_, _ = model.Write(snap)
	if len(model.Unhandled()) > 0 || len(model.Doubts()) > 0 {
		return false
	}
	cells := model.RowCells(row)
	blank := testterm.Cell{Rune: ' ', Fg: -1, Bg: -1}
	end := len(cells)
	for end > 0 && cells[end-1] == blank {
		end--
	}
	if row == height && end == width {
		return false
	}
	var out strings.Builder
	out.WriteString("\x1b7" + ansi.MoveCursor(row, 1))
	pen := blank
	out.WriteString(pen.SGR())
	for _, c := range cells[:end] {
		if !c.SameAttrs(pen) {
			pen = c
			out.WriteString(pen.SGR())
		}
		out.WriteRune(c.Rune)
	}
	if end < width {
		out.WriteString(blank.SGR() + "\x1b[K")
	}
	out.WriteString(model.Pen().SGR() + "\x1b8")
	return terminalio.WriteProcessedBytes(w, []byte(out.String()), mode) == nil
}

// toastLineScreen is a screen that takes toasts as lines of its own.
type toastLineScreen struct {
	ready func() bool             // false holds toasts, as while chat is paged back
	write func(text string) error // shows one line, or the toast stays queued
}

// sessionToastLines holds the toastLineScreen of each session in teleconference.
var sessionToastLines sync.Map

// setToastLineScreen gives s a toastLineScreen until the returned func runs, and
// wakes the session for whatever was held until now.
func setToastLineScreen(s ssh.Session, sess *session.BbsSession, screen toastLineScreen) (clear func()) {
	key := unwrapSession(s)
	sessionToastLines.Store(key, screen)
	if sess != nil {
		sess.WakeToasts()
	}
	return func() { sessionToastLines.Delete(key) }
}

// serviceToastLines hands the next waiting toast to the session's
// toastLineScreen and wakes itself for the one after, so the read can check its
// timeouts in between. With no such screen, as at chat's room pickers, they wait.
// A toast the screen could not write stays queued for the next login.
func (e *MenuExecutor) serviceToastLines(s ssh.Session, sess *session.BbsSession) {
	v, ok := sessionToastLines.Load(unwrapSession(s))
	if !ok {
		return
	}
	screen := v.(toastLineScreen)
	if !screen.ready() {
		return
	}
	t, stored, ok := e.nextOverlayToast(sess)
	if !ok {
		return
	}
	err := screen.write(e.toastText(t))
	if err == nil {
		e.settleOverlayToast(sess, t, stored, false)
	}
	if stored {
		e.toastStore().Release(t.ID)
	}
	if err != nil {
		slog.Warn("failed to write toast", "node", sess.NodeID, "error", err)
		return
	}
	sess.WakeToasts()
}

// toastAnchorCode is the coordinate code that places toasts from menu art:
// ~TO or |TO.
const toastAnchorCode = "TO"

// setToastAnchor places toasts where this menu's art has the anchor code. Art
// coordinates count from the top of the art, so they only match the screen when
// the menu clears it first, the art does not scroll it (see menuArtScrolls) and
// the anchor's row is on it at all; otherwise the configured position stands.
func (e *MenuExecutor) setToastAnchor(nodeNumber int, res ansi.ProcessAnsiResult, clearsScreen, scrolled bool) {
	sess := e.nodeSession(nodeNumber)
	if sess == nil {
		return
	}
	_, height := sess.TermSize()
	if pos, ok := res.FieldCoords[toastAnchorCode]; ok && clearsScreen && !scrolled && (height <= 0 || pos.Y <= height) {
		sess.SetToastAnchor(pos.Y, pos.X)
		return
	}
	sess.ClearToastAnchor()
}

// menuArtScrolls reports whether menu art, written as displayMenuScreen writes
// it, scrolls a terminal width by height. Lines past the height are cut before
// the write, so only autowrap on the lines that remain can push the screen up
// and leave an anchor's coordinates pointing at the wrong row.
func menuArtScrolls(displayBytes []byte, width, height int) bool {
	if height <= 0 {
		return false
	}
	if lines := bytes.Split(displayBytes, []byte("\n")); len(lines) > height {
		displayBytes = bytes.Join(lines[:height], []byte("\n"))
	}
	return ansi.ArtOverflowsHeight(displayBytes, width, height)
}

// wrapToastAnchor moves the anchor in res to where it lands on a terminal that
// wraps raw at width: art that runs past the last column continues a row down.
func wrapToastAnchor(res *ansi.ProcessAnsiResult, raw []byte, width int) {
	if _, ok := res.FieldCoords[toastAnchorCode]; !ok {
		return
	}
	coords, err := ansi.FieldCoordsAtWidth(raw, width)
	if err != nil {
		return
	}
	if pos, ok := coords[toastAnchorCode]; ok {
		res.FieldCoords[toastAnchorCode] = pos
	}
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

// toastText is t as one line of pipe-coded text, formatted for the screen and
// safe for ReplacePipeCodes: only its colour codes survive as codes, so a screen
// that stores and replays the line needs no rendering of its own.
func (e *MenuExecutor) toastText(t toast.Toast) string {
	st := e.Strings()
	text := toastLine(t.Text)
	if st.ToastOverlayFormat != "" {
		text = fmt.Sprintf(st.ToastOverlayFormat, text)
	}
	return ansi.EscapeScreenPipeCodes(text)
}

// drawToastOverlay writes t at the configured position, leaving the cursor and
// colours where they were, and reports the row it took and whether the row cut
// it short. At a configured position the rest of the row is cleared, so the
// toast reads over whatever was there; at a menu's anchor only the toast's own
// cells are written, since the sysop laid out the art around it.
func (e *MenuExecutor) drawToastOverlay(s ssh.Session, w io.Writer, sess *session.BbsSession, t toast.Toast) (row int, clipped bool, err error) {
	lay := e.toastLayout(s, sess, t)
	return lay.row, lay.clipped, lay.write(w, lay.line)
}

// toastDraw is where and how an overlay toast is drawn: its row and column,
// its text as rendered and cut to the row, and whether the rest of the row is
// cleared. A fade draws the same cells in one colour at a time.
type toastDraw struct {
	row, col  int
	line      string // the toast, colour sequences included, cut to the row
	clipped   bool   // the row cut it short
	clearRest string // "\x1b[K" at a configured position, "" at an anchor
	mode      ansi.OutputMode
}

// write draws text, one of lay's renderings, in lay's cells.
func (lay toastDraw) write(w io.Writer, text string) error {
	out := "\x1b7" + ansi.MoveCursor(lay.row, lay.col) + "\x1b[0m" + lay.clearRest + text + "\x1b[0m\x1b8"
	return terminalio.WriteProcessedBytes(w, []byte(out), lay.mode)
}

// frame is the toast's text in one DOS colour, for a fade step: the same
// characters as line, without their own colours.
func (lay toastDraw) frame(color int) string {
	return string(ansi.ReplacePipeCodes([]byte(fmt.Sprintf("|%02d", color)))) + ansi.StripAnsi(lay.line)
}

// toastFade returns the colours a toast fades through, in fade-in order,
// and how long each shows, or no colours when the board does not fade
// toasts.
func (e *MenuExecutor) toastFade() (ramp []int, step time.Duration) {
	cfg := e.GetServerConfig()
	if !cfg.ToastFade {
		return nil, 0
	}
	return e.Theme().FadeColors(), cfg.ToastFadeStep()
}

// fadeToast draws lay through ramp, step apart, and reports what ended it:
// WaitElapsed when it ran to the end or reached deadline, or the key or
// interruption that cut it short. No frame is drawn, and no wait runs, past
// deadline. drawn says the first step is on screen already. An empty ramp
// draws nothing. A write that fails ends it as an interruption, so the
// caller restores the row.
func fadeToast(ih *editor.InputHandler, lay toastDraw, w io.Writer, ramp []int, step time.Duration, drawn bool, deadline time.Time) editor.WaitOutcome {
	for i, c := range ramp {
		if time.Until(deadline) <= 0 {
			return editor.WaitElapsed
		}
		if i > 0 || !drawn {
			if lay.write(w, lay.frame(c)) != nil {
				return editor.WaitInterrupted
			}
		}
		// The write may have taken the time that was left.
		left := time.Until(deadline)
		if left <= 0 {
			return editor.WaitElapsed
		}
		if out := ih.WaitFor(min(step, left)); out != editor.WaitElapsed {
			return out
		}
	}
	return editor.WaitElapsed
}

// reversed returns ramp back to front, for the fade out.
func reversed(ramp []int) []int {
	out := make([]int, len(ramp))
	for i, c := range ramp {
		out[len(ramp)-1-i] = c
	}
	return out
}

// toastLayout works out where and how t is drawn on sess's screen now.
func (e *MenuExecutor) toastLayout(s ssh.Session, sess *session.BbsSession, t toast.Toast) toastDraw {
	width, height := sess.TermSize()
	if width <= 0 {
		width = 80
	}
	if height <= 0 {
		height = 24
	}
	cfg := e.GetServerConfig()
	mode := sessionOutputMode(s)
	full := string(ansi.ReplacePipeCodes([]byte(e.toastText(t))))
	cfgRow, cfgCol := toastEdgeRow(cfg.ToastEdge, cfg.ToastOffset, height), cfg.ToastCol
	if cfg.ToastCenter {
		_, konfigWidth, _ := sess.ToastViewer()
		cfgCol = toastCenterCol(columnWidth(ansi.StripAnsi(full), utf8.ValidString(full), mode), konfigWidth, width)
	}
	clearRest := "\x1b[K"
	if r, c, ok := sess.ToastAnchor(); ok {
		cfgRow, cfgCol = r, c
		clearRest = ""
	}
	row, col := toastPlacement(cfgRow, cfgCol, width, height)
	// One column short: a character in the last cell of the last row can scroll
	// the screen on some terminals.
	line := clipColumns(full, width-col, mode)
	return toastDraw{row: row, col: col, line: line, clipped: line != full, clearRest: clearRest, mode: mode}
}

// settleOverlayToast records that t has been drawn: it leaves its queue, unless
// the rest of a clipped line is still to be shown.
func (e *MenuExecutor) settleOverlayToast(sess *session.BbsSession, t toast.Toast, stored, clipped bool) {
	if !stored {
		if clipped {
			sess.HoldPage()
		} else {
			sess.PopPage()
		}
		return
	}
	if clipped {
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
		sess.NoteToastShown(t.ID)
	}
}

// rearmToasts wakes the session's next key wait. Whatever an overflowing screen
// held back has no other wake coming, and a new screen may have room for it.
func (e *MenuExecutor) rearmToasts(nodeNumber int) {
	if sess := e.nodeSession(nodeNumber); sess != nil {
		sess.WakeToasts()
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
	for _, q := range []struct {
		peek func() (string, bool)
		pop  func()
	}{{sess.PeekHeldPage, sess.PopHeldPage}, {sess.PeekPage, sess.PopPage}} {
		for {
			page, ok := q.peek()
			if !ok {
				break
			}
			if err := terminalio.WriteProcessedBytes(terminal, toastPipes("\r\n"+toastLine(page)+"\r\n"), outputMode); err != nil {
				slog.Error("failed to deliver page", "node", nodeNumber, "error", err)
				return wrote
			}
			q.pop()
			wrote = true
		}
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
		if !store.Claim(u.ID, t.ID) {
			continue
		}
		if sess.ToastShown(t.ID) {
			if rmErr := store.Remove(u.ID, t.ID); rmErr != nil {
				slog.Warn("failed to remove shown toast", "node", nodeNumber, "error", rmErr)
			}
			store.Release(t.ID)
			continue
		}
		out := toastPipes("\r\n" + toastLine(t.Text) + "\r\n")
		err := terminalio.WriteProcessedBytes(terminal, out, outputMode)
		if err == nil {
			wrote = true
			if rmErr := store.Remove(u.ID, t.ID); rmErr != nil {
				slog.Warn("failed to remove shown toast", "node", nodeNumber, "error", rmErr)
				sess.NoteToastShown(t.ID)
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

// deliverHeldToasts shows any queued toast the overlay could not show whole, a
// line too long for its row, and holds the screen so it can be read. Boards with
// only lightbar menus have no prompt to wait for, so this runs between menus.
// Other toasts stay for the overlay.
func (e *MenuExecutor) deliverHeldToasts(s ssh.Session, terminal *term.Terminal, nodeNumber int, outputMode ansi.OutputMode, termWidth, termHeight int) {
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
	if _, held := sess.PeekHeldPage(); held {
		if e.deliverToastsInline(s, terminal, nodeNumber, outputMode) {
			e.holdScreen(s, terminal, outputMode, termWidth, termHeight)
		}
		return
	}
	queue, err := e.toastStore().Peek(u.ID)
	if err != nil {
		return
	}
	for _, t := range queue {
		if sess.ToastAnnounced(t.ID) {
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
