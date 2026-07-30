package menu

import (
	"log/slog"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/terminalio"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// runListMsgs is the main entry point for the message list command.
func runListMsgs(c *cmdCtx, args string) (*user.User, string, error) {
	return runListMsgsFiltered(c, args, nil)
}

// runListMsgsFiltered lists messages in the current area. When msgFilter is
// non-nil (e.g. PRIVMAIL), only messages it accepts are listed, and the filter
// is propagated to the reader when a message is opened.
func runListMsgsFiltered(c *cmdCtx, args string, msgFilter msgOwnershipFilter) (*user.User, string, error) {
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
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(e.LoadedStrings.MsgListLoginRequired)), outputMode)
		time.Sleep(1 * time.Second)
		return nil, "", nil
	}

	// Check if user has selected a message area
	currentAreaID := currentUser.CurrentMessageAreaID
	if currentAreaID == 0 {
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(e.LoadedStrings.MsgListNoAreaSelected)), outputMode)
		time.Sleep(1 * time.Second)
		return currentUser, "", nil
	}

	// Get area information
	area, found := e.MessageMgr.GetAreaByID(currentAreaID)
	if !found {
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(e.LoadedStrings.MsgListAreaNotFound)), outputMode)
		time.Sleep(1 * time.Second)
		return currentUser, "", nil
	}

	// Get conference name
	confName := "Local"
	if currentUser.CurrentMsgConferenceID != 0 && e.ConferenceMgr != nil {
		if conf, found := e.ConferenceMgr.GetByID(currentUser.CurrentMsgConferenceID); found {
			confName = conf.Name
		}
	}

	// Build message list
	entries, lastRead, err := buildMessageList(e.MessageMgr, currentAreaID, currentUser.Handle, msgFilter)
	if err != nil {
		slog.Error("failed to build message list", "node", nodeNumber, "error", err)
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(e.LoadedStrings.MsgListLoadError)), outputMode)
		time.Sleep(1 * time.Second)
		return currentUser, "", nil
	}

	// Check if area is empty
	if len(entries) == 0 {
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(e.LoadedStrings.MsgListNoMessages)), outputMode)
		time.Sleep(2 * time.Second)
		return currentUser, "", nil
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
	}

	// Main navigation loop
	sessionIH := getSessionIH(s)

	// Ensure cursor is restored when exiting
	defer func() {
		terminalio.WriteProcessedBytes(terminal, []byte("\x1b[?25h"), outputMode) // Show cursor
	}()

	// Draw initial screen
	if err := drawMessageListScreen(terminal, state, area.Name, confName, outputMode); err != nil {
		slog.Error("failed to draw message list", "node", nodeNumber, "error", err)
		return currentUser, "", err
	}

	previousIndex := state.SelectedIndex // Track previous selection for optimized refresh

	for {
		// Handle navigation
		action, selectedMsg, err := runMessageListNavigation(sessionIH, state)
		if err != nil {
			slog.Error("navigation error", "node", nodeNumber, "error", err)
			return currentUser, "LOGOFF", err
		}

		switch action {
		case "QUIT":
			return currentUser, "", nil

		case "READ":
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

			// Call message reader
			_, nextAction, err := runMessageReader(e, s, terminal, userManager,
				currentUser, nodeNumber, sessionStartTime, outputMode,
				selectedMsg, areaMsgCount, false, tw, th, msgFilter)

			if err != nil {
				slog.Error("message reader error", "node", nodeNumber, "error", err)
				return currentUser, "", err
			}

			// Handle return action
			if nextAction == "LOGOFF" {
				return currentUser, "LOGOFF", nil
			}

			// Rebuild list to pick up posted/deleted messages and updated lastread markers
			newEntries, newLastRead, rebuildErr := buildMessageList(e.MessageMgr, currentAreaID, currentUser.Handle, msgFilter)
			if rebuildErr != nil {
				slog.Error("failed to rebuild message list", "node", nodeNumber, "error", rebuildErr)
			} else {
				state.Entries = newEntries
				state.TotalMessages = len(newEntries)
				state.LastRead = newLastRead

				// Clamp page/selection if messages were deleted
				totalPages := (state.TotalMessages + state.ItemsPerPage - 1) / state.ItemsPerPage
				if totalPages < 1 {
					totalPages = 1
				}
				if state.CurrentPage > totalPages {
					state.CurrentPage = totalPages
				}
				_, end := calculatePagination(len(state.Entries), state.ItemsPerPage, state.CurrentPage)
				start, _ := calculatePagination(len(state.Entries), state.ItemsPerPage, state.CurrentPage)
				itemsOnPage := end - start
				if state.SelectedIndex >= itemsOnPage {
					state.SelectedIndex = itemsOnPage - 1
				}
				if state.SelectedIndex < 0 {
					state.SelectedIndex = 0
				}
			}

			// Handle empty area after deletions
			if state.TotalMessages == 0 {
				terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(e.LoadedStrings.MsgListNoMessages)), outputMode)
				time.Sleep(2 * time.Second)
				return currentUser, "", nil
			}

			// Redraw full screen after returning from reader
			if err := drawMessageListScreen(terminal, state, area.Name, confName, outputMode); err != nil {
				slog.Error("failed to redraw message list", "node", nodeNumber, "error", err)
				return currentUser, "", err
			}
			previousIndex = state.SelectedIndex // Track highlighted line after redraw

		case "REFRESH_FULL":
			// Optimized page refresh (only redraw message lines and page info, no screen clear)
			if err := refreshPageContent(terminal, state, outputMode); err != nil {
				slog.Error("failed to refresh page content", "node", nodeNumber, "error", err)
				return currentUser, "", err
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
					_ = drawMessageLineAtRow(terminal, state.Entries[actualIndex], row, false, outputMode) // best-effort redraw
				}
			}

			// Highlight current line
			if state.SelectedIndex >= 0 && state.SelectedIndex < state.ItemsPerPage {
				actualIndex := start + state.SelectedIndex
				if actualIndex < len(state.Entries) {
					row := 6 + state.SelectedIndex
					_ = drawMessageLineAtRow(terminal, state.Entries[actualIndex], row, true, outputMode) // best-effort redraw
				}
			}

			previousIndex = state.SelectedIndex

		case "ERROR":
			return currentUser, "LOGOFF", err
		}
	}
}
