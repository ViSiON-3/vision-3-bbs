package menu

import (
	"log/slog"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/terminalio"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// runListMsgs is the main entry point for the message list command.
func runListMsgs(c *cmdCtx, args string) (*user.User, string, error) {
	return runListMsgsFiltered(c, args, nil)
}

// runListMsgsFiltered lists messages in the current area. Other users' private
// messages are never listed; when msgFilter is non-nil (e.g. PRIVMAIL), only
// messages it accepts are listed too. The filter is propagated to the reader
// when a message is opened.
func runListMsgsFiltered(c *cmdCtx, args string, msgFilter msgOwnershipFilter) (*user.User, string, error) {
	u, action, _, err := runListMsgsWith(c, args, msgFilter, listOpts{})
	return u, action, err
}

// listOpts is how the list was opened.
type listOpts struct {
	// fromReader is set when the reader's L opened the list. The list then
	// never nests a reader: Enter returns "READ" with the highlighted
	// message, and Q and Esc return "READ" with startMsg, for the reader to
	// show.
	fromReader bool
	startMsg   int // the message to put the cursor on; 0 = the first
}

// runListMsgsWith is runListMsgsFiltered with listOpts. The message number
// is meaningful with the "READ" action (fromReader only).
func runListMsgsWith(c *cmdCtx, args string, msgFilter msgOwnershipFilter, opts listOpts) (*user.User, string, int, error) {
	baseFilter := withPrivacy(privateMailReaderFor(c.e, c.userManager, c.currentUser), msgFilter)
	matcher := twitMatcher(c.e, c.userManager, c.currentUser)
	msgFilter = withTwit(func() *user.TwitMatcher { return matcher }, baseFilter, nil)
	e := c.e
	s := c.s
	terminal := c.terminal
	userManager := c.userManager
	currentUser := c.currentUser
	nodeNumber := c.nodeNumber
	sessionStartTime := c.sessionStartTime
	outputMode := c.outputMode
	termHeight := c.termHeight

	// Validate user is logged in
	if currentUser == nil {
		slog.Warn("LISTMSGS called without logged in user", "node", nodeNumber)
		e.writeString(terminal, outputMode, e.Strings().MsgListLoginRequired)
		uiPause(1 * time.Second)
		return nil, "", 0, nil
	}

	// Check if user has selected a message area
	currentAreaID := currentUser.CurrentMessageAreaID
	if currentAreaID == 0 {
		e.writeString(terminal, outputMode, e.Strings().MsgListNoAreaSelected)
		uiPause(1 * time.Second)
		return currentUser, "", 0, nil
	}

	// Get area information
	area, found := e.MessageMgr.GetAreaByID(currentAreaID)
	if !found {
		e.writeString(terminal, outputMode, e.Strings().MsgListAreaNotFound)
		uiPause(1 * time.Second)
		return currentUser, "", 0, nil
	}

	// Get conference name
	confName := "Local"
	if currentUser.CurrentMsgConferenceID != 0 && e.ConferenceMgr != nil {
		if conf, found := e.ConferenceMgr.GetByID(currentUser.CurrentMsgConferenceID); found {
			confName = conf.Name
		}
	}

	// Flat or threaded: as the caller last had it.
	threaded := sessionListThreaded(s)
	build := func(threaded bool) ([]MessageListEntry, int, error) {
		if threaded {
			return buildThreadedMessageList(e.MessageMgr, currentAreaID, currentUser.Handle, msgFilter)
		}
		return buildMessageList(e.MessageMgr, currentAreaID, currentUser.Handle, msgFilter)
	}

	// Build message list
	entries, lastRead, err := build(threaded)
	if err != nil {
		slog.Error("failed to build message list", "node", nodeNumber, "error", err)
		e.writeString(terminal, outputMode, e.Strings().MsgListLoadError)
		uiPause(1 * time.Second)
		return currentUser, "", 0, nil
	}

	// Check if area is empty
	if len(entries) == 0 {
		e.writeString(terminal, outputMode, e.Strings().MsgListNoMessages)
		uiPause(2 * time.Second)
		return currentUser, "", 0, nil
	}

	// Derive an effective terminal height.
	// Prefer live PTY height, then user profile height, then passed fallback.
	effectiveHeight := termHeight
	if ptyReq, _, ok := s.Pty(); ok && ptyReq.Window.Height > 0 {
		effectiveHeight = ptyReq.Window.Height
	} else if currentUser.ScreenHeight > 0 {
		effectiveHeight = currentUser.ScreenHeight
	}
	if effectiveHeight <= 0 {
		effectiveHeight = 24
	}

	// Calculate items per page based on terminal height
	// Header: 5 lines (top border, title, separator, column headers, separator)
	// Footer: 5 lines (separator, page info, separator, help, bottom border)
	headerHeight := 5
	footerHeight := 5
	itemsPerPage := effectiveHeight - headerHeight - footerHeight
	if itemsPerPage < 3 {
		itemsPerPage = 3 // Minimum
	}

	// Initialize state
	state := &MessageListState{
		AreaID:        currentAreaID,
		TotalMessages: len(entries),
		Entries:       entries,
		CurrentPage:   1,
		ItemsPerPage:  itemsPerPage,
		SelectedIndex: 0,
		LastRead:      lastRead,
		Threaded:      threaded,
	}
	state.selectMessage(opts.startMsg)

	// rebuild reloads the list in mode threaded (new posts, deletions, read
	// markers) and puts the cursor back on message n, or near where it was.
	rebuild := func(threaded bool, n int) {
		newEntries, newLastRead, rebuildErr := build(threaded)
		if rebuildErr != nil {
			slog.Error("failed to rebuild message list", "node", nodeNumber, "error", rebuildErr)
			return
		}
		state.Threaded = threaded
		state.Entries = newEntries
		state.TotalMessages = len(newEntries)
		state.LastRead = newLastRead
		if state.CurrentPage > state.totalPages() {
			state.CurrentPage = state.totalPages()
		}
		state.selectMessage(n)
	}

	// Main navigation loop
	sessionIH := getSessionIH(s)

	// Ensure cursor is restored when exiting
	defer func() {
		terminalio.WriteProcessedBytes(terminal, []byte("\x1b[?25h"), outputMode) // Show cursor
	}()

	// Draw initial screen
	if err := drawMessageListScreen(terminal, state, area.Name, confName, outputMode, e.Strings()); err != nil {
		slog.Error("failed to draw message list", "node", nodeNumber, "error", err)
		return currentUser, "", 0, err
	}

	previousIndex := state.SelectedIndex // Track previous selection for optimized refresh

	for {
		// Handle navigation
		action, selectedMsg, err := runMessageListNavigation(sessionIH, state)
		if err != nil {
			slog.Error("navigation error", "node", nodeNumber, "error", err)
			return currentUser, "LOGOFF", 0, err
		}

		switch action {
		case "QUIT":
			if opts.fromReader {
				return currentUser, "READ", opts.startMsg, nil
			}
			return currentUser, "", 0, nil

		case "TOGGLE":
			cur := 0
			if i := state.selectedAbsolute(); state.selectable(i) {
				cur = state.Entries[i].MsgNum
			}
			setSessionListThreaded(s, !state.Threaded)
			rebuild(!state.Threaded, cur)
			if err := drawMessageListScreen(terminal, state, area.Name, confName, outputMode, e.Strings()); err != nil {
				slog.Error("failed to redraw message list", "node", nodeNumber, "error", err)
				return currentUser, "", 0, err
			}
			previousIndex = state.SelectedIndex

		case "READ":
			if opts.fromReader {
				// The reader that opened the list shows the message itself.
				return currentUser, "READ", selectedMsg, nil
			}
			// Get terminal dimensions from user preferences
			tw := currentUser.ScreenWidth
			if tw == 0 {
				tw = 80
			}
			th := currentUser.ScreenHeight
			if th == 0 {
				th = 24
			}

			// The reader navigates by real area message number, so it needs the
			// area's actual message count as its upper bound — not len(entries),
			// which is smaller when the list is filtered (e.g. PRIVMAIL) or has
			// gaps, and would make the reader exit immediately for high msgNums.
			areaMsgCount, countErr := e.MessageMgr.GetMessageCountForArea(currentAreaID)
			if countErr != nil || areaMsgCount < selectedMsg {
				areaMsgCount = selectedMsg
			}

			// Call message reader. It knows it was opened from here, so its
			// L comes back rather than opening a second list.
			_, nextAction, lastMsg, err := runMessageReaderOpts(e, s, terminal, userManager,
				currentUser, nodeNumber, sessionStartTime, outputMode,
				selectedMsg, areaMsgCount, false, tw, th, baseFilter, readerOpts{fromList: true})

			if err != nil {
				slog.Error("message reader error", "node", nodeNumber, "error", err)
				return currentUser, "", 0, err
			}

			// Handle return action
			if nextAction == "LOGOFF" {
				return currentUser, "LOGOFF", 0, nil
			}

			matcher = twitMatcher(e, userManager, currentUser)
			// Rebuild list to pick up posted/deleted messages and updated
			// lastread markers, in the mode the reader's V or L asked for,
			// with the cursor on the message that was being read.
			rebuild(sessionListThreaded(s), lastMsg)

			// Handle empty area after deletions
			if state.TotalMessages == 0 {
				e.writeString(terminal, outputMode, e.Strings().MsgListNoMessages)
				uiPause(2 * time.Second)
				return currentUser, "", 0, nil
			}

			// Redraw full screen after returning from reader
			if err := drawMessageListScreen(terminal, state, area.Name, confName, outputMode, e.Strings()); err != nil {
				slog.Error("failed to redraw message list", "node", nodeNumber, "error", err)
				return currentUser, "", 0, err
			}
			previousIndex = state.SelectedIndex // Track highlighted line after redraw

		case "REFRESH_FULL":
			// Optimized page refresh (only redraw message lines and page info, no screen clear)
			if err := refreshPageContent(terminal, state, outputMode, e.Strings()); err != nil {
				slog.Error("failed to refresh page content", "node", nodeNumber, "error", err)
				return currentUser, "", 0, err
			}
			previousIndex = state.SelectedIndex // Track highlighted line after redraw

		case "REFRESH_LINE":
			// Optimized refresh: only redraw changed lines
			// Message lines start at row 6 (1: top border, 2: title, 3: sep, 4: headers, 5: sep, 6+: messages)
			start, _ := calculatePagination(len(state.Entries), state.ItemsPerPage, state.CurrentPage)

			// Unhighlight previous line
			if previousIndex >= 0 && previousIndex < state.ItemsPerPage {
				actualIndex := start + previousIndex
				if actualIndex < len(state.Entries) {
					row := 6 + previousIndex
					_ = drawMessageLineAtRow(terminal, state.Entries[actualIndex], row, false, outputMode, e.Strings()) // best-effort redraw
				}
			}

			// Highlight current line
			if state.SelectedIndex >= 0 && state.SelectedIndex < state.ItemsPerPage {
				actualIndex := start + state.SelectedIndex
				if actualIndex < len(state.Entries) {
					row := 6 + state.SelectedIndex
					_ = drawMessageLineAtRow(terminal, state.Entries[actualIndex], row, true, outputMode, e.Strings()) // best-effort redraw
				}
			}

			previousIndex = state.SelectedIndex

		case "ERROR":
			return currentUser, "LOGOFF", 0, err
		}
	}
}
