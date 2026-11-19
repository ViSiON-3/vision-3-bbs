package menu

import (
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/editor"
	"github.com/ViSiON-3/vision-3-bbs/internal/safego"
	"github.com/ViSiON-3/vision-3-bbs/internal/session"
	"github.com/ViSiON-3/vision-3-bbs/internal/snoop"
	"github.com/ViSiON-3/vision-3-bbs/internal/terminalio"
)

// A menu drawn by a script has no key wait of the menu's own for a toast or
// the time-left warning to be shown in. So while the script runs,
// menuNotices watches for them, and when one is due the script is stopped at
// its next key wait (scripting.ScriptConfig.Notice). The menu shows it as a
// built-in screen would, the toast through the same overlay as any key wait
// (serviceToasts) and the warning on the bottom row as at a lightbar menu,
// then runs the script again, which draws its screen anew. Keys the caller
// typed meanwhile reach the script's next run.

// menuNoticeKind is what a menuNotices run stopped the script for.
type menuNoticeKind int

const (
	menuNoticeToast menuNoticeKind = iota + 1
	menuNoticeTimeLeft
)

// menuNotices watches one visit to a scripted menu, across the runs of its
// script.
type menuNotices struct {
	st   *runLoopState
	sess *session.BbsSession // nil when the node has no session to take toasts
	// warned is set once the time-left warning has been given at this menu:
	// it is given once per menu, as before a built-in menu's prompt.
	warned bool
	// toastsOff is set when toasts could not be shown here (see
	// toastRetries); they wait for the next screen that can show them.
	toastsOff bool
	// toastFailures counts toasts in a row that failed to draw here.
	toastFailures int
	// quietUntil holds off the next toast after a key took one down.
	quietUntil time.Time
	// idleFrom is when the caller's idle countdown started, carried from one
	// run of the script to the next; zero before the first run.
	idleFrom time.Time

	kind menuNoticeKind // set before fired is closed
	done chan struct{}  // closed by stop
	exit chan struct{}  // closed when the watching goroutine returns
}

func (st *runLoopState) newMenuNotices() *menuNotices {
	return &menuNotices{st: st, sess: st.e.nodeSession(st.nodeNumber)}
}

// toastsOn reports whether toasts can be shown over this menu's script: the
// session takes them and has the screen output and the snoop tap the overlay
// draws with.
func (n *menuNotices) toastsOn() bool {
	if n.sess == nil || n.toastsOff || !n.sess.ToastsReady() {
		return false
	}
	tap := tapOf(n.st.s)
	_, hasOut := sessionOutputs.Load(n.st.s)
	return tap != nil && hasOut && tap.Mode() == snoop.ModeBBS
}

// warnAt returns when the time-left warning comes due, and false when there
// is none to give: it was given, or the caller has no time limit or no time
// left.
func (n *menuNotices) warnAt() (time.Time, bool) {
	if n.warned {
		return time.Time{}, false
	}
	d, ok := sessionDeadlines.Load(unwrapSession(n.st.s))
	if !ok {
		return time.Time{}, false
	}
	deadline := d.(time.Time)
	if !time.Now().Before(deadline) {
		return time.Time{}, false
	}
	return deadline.Add(-timeLimitWarnWindow), true
}

// start begins watching for one run of the script, and returns the channel
// for its ScriptConfig.Notice. stop must be called once the run is over.
func (n *menuNotices) start() <-chan struct{} {
	fired := make(chan struct{})
	n.kind = 0
	n.done = make(chan struct{})
	n.exit = make(chan struct{})
	var wake <-chan struct{}
	if n.toastsOn() {
		wake = n.sess.ToastWake()
	}
	var due <-chan time.Time
	var timer *time.Timer
	if at, ok := n.warnAt(); ok {
		timer = time.NewTimer(time.Until(at))
		due = timer.C
	}
	e, sess, done, exit := n.st.e, n.sess, n.done, n.exit
	quiet := time.Until(n.quietUntil)
	safego.Go(safego.KindNotice, n.st.nodeNumber, func() {
		defer close(exit)
		safego.TestPoint(safego.KindNotice)
		if timer != nil {
			defer timer.Stop()
		}
		if wake != nil && quiet > 0 {
			// Toasts wait out the rearm delay; the warning does not.
			hold := time.NewTimer(quiet)
			defer hold.Stop()
			select {
			case <-hold.C:
			case <-due:
				n.kind = menuNoticeTimeLeft
				close(fired)
				return
			case <-done:
				return
			}
		}
		for {
			// A warning already due goes first: a toast's dwell could
			// outlast the time left.
			select {
			case <-due:
				n.kind = menuNoticeTimeLeft
				close(fired)
				return
			default:
			}
			// A wake may come with nothing to show (the menu loop rearms
			// toasts on every screen), so only a toast waiting stops the
			// script.
			if wake != nil && e.waitingToast(sess) != "" {
				n.kind = menuNoticeToast
				close(fired)
				return
			}
			select {
			case <-wake:
			case <-due:
				n.kind = menuNoticeTimeLeft
				close(fired)
				return
			case <-done:
				return
			}
		}
	}, nil)
	return fired
}

// stop ends the watch start began. shown is true when the run ended for the
// notice, which show then shows. Otherwise a toast wake the watch took is
// given back, so the key wait that follows the script still looks for toasts:
// one that fired as the script ended on a key, after its last key wait, is
// shown at the next key wait, in whatever screen that key opened.
func (n *menuNotices) stop(shown bool) {
	close(n.done)
	<-n.exit
	if !shown && n.sess != nil && n.toastsOn() {
		n.sess.WakeToasts()
	}
}

// toastRetries is how many toasts in a row may fail to draw at one menu
// before toasts stop interrupting its script; they wait for the next screen
// then. A failure can pass (the dwell ran out before the draw), but stopping
// the script again at once for one that does not would only loop.
const toastRetries = 3

// timeLeft returns what is left of the caller's idle countdown and time
// limit, whichever is less, and false when neither applies.
func (n *menuNotices) timeLeft() (time.Duration, bool) {
	st := n.st
	var left time.Duration
	limited := false
	if idle := st.e.idleTimeout(st.currentUser); idle > 0 && !n.idleFrom.IsZero() {
		left, limited = idle-time.Since(n.idleFrom), true
	}
	if d, ok := sessionDeadlines.Load(unwrapSession(st.s)); ok {
		if l := time.Until(d.(time.Time)); !limited || l < left {
			left, limited = l, true
		}
	}
	return left, limited
}

// show shows what the script was stopped for, for no longer than the caller
// has left. Call it after stop, once the script has ended with
// scripting.ErrNotice.
func (n *menuNotices) show() {
	st := n.st
	ih := getSessionIH(st.s)
	// A key typed while the script was stopped is the caller's, whatever
	// the countdown says now.
	n.noteTyped(ih)
	defer n.afterShow(ih)
	left, limited := n.timeLeft()
	if limited && left <= 0 {
		return // the next run ends the call for it
	}
	if _, due := timeLeftWarningMinutes(st.s); due && !n.warned {
		// A warning that came due while a toast stopped the script goes
		// first; the toast stays queued for the next run.
		n.kind = menuNoticeTimeLeft
	}
	switch n.kind {
	case menuNoticeTimeLeft:
		// A deadline moved later meanwhile (sysop chat) leaves the warning
		// armed for the new one.
		if _, due := timeLeftWarningMinutes(st.s); !due {
			return
		}
		n.warned = true
		st.e.warnTimeLeftOnRow(st.s, st.terminal, st.outputMode, st.termHeight, st.termWidth)
		// Up as long as a toast, or until a key, which the script then
		// reads, or a sysop's chat request; never past what is left.
		dwell := toastDwell
		if left, limited = n.timeLeft(); limited && left < dwell {
			dwell = left
		}
		if dwell > 0 {
			toastWait(ih, dwell)
		}
		n.clearRow(max(st.termHeight, 1))
	case menuNoticeToast:
		// The script draws its screen again once the toast is down; the
		// row the toast was drawn on is cleared first, for a script that
		// does not repaint every row. The time left is taken again at the
		// last moment, since the toast's own deadline counts from there.
		left, limited = n.timeLeft()
		if limited && left <= 0 {
			return // the next run ends the call for it
		}
		var limit time.Duration // none
		if limited {
			limit = left
		}
		if st.e.serviceToastsThen(st.s, ih, n.sess, limit, n.clearRow) {
			n.toastFailures = 0
			if _, keyed, err := ih.PendingByte(0); keyed && err == nil {
				// A key took the toast down: the next one waits
				// toastRearm, as at a key wait.
				n.quietUntil = time.Now().Add(toastRearm)
			}
		} else if n.toastFailures++; n.toastFailures >= toastRetries {
			n.toastsOff = true
		}
	}
}

// afterShow ends every path through show. A key typed here is the caller's,
// so it starts the idle countdown again. Looking for one can take a sysop's
// chat request off its channel, as can the waits in show; that request is
// taken before the script runs again, as a key wait would take it, and chat
// time is not charged to the caller either.
func (n *menuNotices) afterShow(ih *editor.InputHandler) {
	n.noteTyped(ih)
	if ih.ServicePendingBreakIn() {
		n.idleFrom = time.Now()
	}
}

// noteTyped starts the idle countdown again when a key is waiting in ih.
// A closed connection is not a key.
func (n *menuNotices) noteTyped(ih *editor.InputHandler) {
	if _, typed, err := ih.PendingByte(0); typed && err == nil {
		n.idleFrom = time.Now()
	}
}

// clearRow blanks screen row row, leaving the cursor and colours as they
// were: what a notice drew there goes, whether or not the script's next run
// draws over it.
func (n *menuNotices) clearRow(row int) {
	out := "\x1b7" + ansi.MoveCursor(row, 1) + "\x1b[0m\x1b[2K\x1b8"
	_ = terminalio.WriteProcessedBytes(n.st.terminal, []byte(out), n.st.outputMode) // best-effort; the script redraws next
}
