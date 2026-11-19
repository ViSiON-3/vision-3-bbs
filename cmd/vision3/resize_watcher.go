package main

import (
	"log/slog"
	"time"

	"github.com/gliderlabs/ssh"
)

// resizeWatcher applies a caller's window changes to their session. While a
// binary transfer runs it holds them back, since repainting would corrupt
// the transfer's stream, and applies the last one held back once the
// transfer ends.
type resizeWatcher struct {
	node int32
	// transferActive reports whether a binary transfer is running; nil for
	// a session that has none.
	transferActive func() bool
	// setSize resizes the terminal, which repaints it.
	setSize func(w, h int)
	// onSize records every window change, held back or not.
	onSize func(win ssh.Window)
	// replayPoll is how often, while a resize is held back, the watcher looks
	// for the end of the transfer.
	replayPoll time.Duration
}

// run applies the window changes from resizes until it closes. It alone
// holds the size held back, and looks for the end of the transfer on a
// ticker while there is one, so every transfer's held-back size is applied
// when it ends and a newer size always wins over an older one.
func (rw *resizeWatcher) run(resizes <-chan ssh.Window) {
	var pending *ssh.Window
	var ticker *time.Ticker
	var tick <-chan time.Time
	stopTicker := func() {
		if ticker != nil {
			ticker.Stop()
			ticker, tick = nil, nil
		}
	}
	defer stopTicker()
	for {
		select {
		case win, ok := <-resizes:
			if !ok {
				slog.Debug("window change channel closed", "node", rw.node)
				return
			}
			slog.Debug("window resize event", "node", rw.node, "width", win.Width, "height", win.Height)
			rw.onSize(win)
			if win.Width <= 0 || win.Height <= 0 {
				continue
			}
			// Skip terminal repaint during binary transfers — SetSize writes
			// ANSI sequences via session.Write() which does CRLF conversion,
			// corrupting the RawWrite binary data stream.
			if rw.transferActive != nil && rw.transferActive() {
				slog.Debug("suppressed terminal.SetSize during active transfer", "node", rw.node)
				pending = &win
				if ticker == nil {
					ticker = time.NewTicker(rw.replayPoll)
					tick = ticker.C
				}
				continue
			}
			// This size replaces any held back.
			pending = nil
			stopTicker()
			rw.setSize(win.Width, win.Height)
		case <-tick:
			if rw.transferActive() {
				continue
			}
			stopTicker()
			if pending != nil {
				slog.Debug("applying the size held back during the transfer", "node", rw.node, "width", pending.Width, "height", pending.Height)
				rw.setSize(pending.Width, pending.Height)
				pending = nil
			}
		}
	}
}
