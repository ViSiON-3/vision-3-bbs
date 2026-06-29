package menu

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/terminalio"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
	"github.com/gliderlabs/ssh"
	"golang.org/x/term"
)

// runNewScanAll implements the multi-area newscan flow matching Pascal's NewScanAll.
func runNewScanAll(e *MenuExecutor, s ssh.Session, terminal *term.Terminal,
	userManager *user.UserMgr, currentUser *user.User, nodeNumber int,
	sessionStartTime time.Time, outputMode ansi.OutputMode,
	currentOnly bool, termWidth int, termHeight int) (*user.User, string, error) {

	if currentUser == nil {
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(e.LoadedStrings.ScanLoginRequired)), outputMode)
		time.Sleep(1 * time.Second)
		return nil, "", nil
	}

	// Use the session-scoped InputHandler so this scan loop, any editor
	// invocations, and the lightbar menus all share a single goroutine reading
	// from the SSH session — no keystrokes are lost when control transfers.
	scanIH := getSessionIH(s)
	reader := bufio.NewReader(scanIH)

	// Get total message count for current area (for range display in setup)
	currentAreaID := currentUser.CurrentMessageAreaID
	numMsgs := 0
	if currentAreaID > 0 {
		cnt, _ := e.MessageMgr.GetMessageCountForArea(currentAreaID)
		numMsgs = cnt
	}

	hiColor := e.Theme.YesNoHighlightColor
	loColor := e.Theme.YesNoRegularColor

	// Show scan setup menu
	scanCfg, err := runGetScanType(reader, e, terminal, outputMode, numMsgs, currentOnly, hiColor, loColor)
	if err != nil {
		if errors.Is(err, io.EOF) {
			return nil, "LOGOFF", io.EOF
		}
		return nil, "", err
	}
	if scanCfg.Aborted {
		return nil, "", nil
	}

	// Display "Scanning Messages..."
	terminalio.WriteProcessedBytes(terminal, []byte(ansi.ClearScreen()), outputMode)
	terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(e.LoadedStrings.ScanHeader)), outputMode)

	nonStop := false
	quitNewScan := false

	// If current area only, scan just that area
	if scanCfg.WhichAreas == 3 {
		if currentAreaID <= 0 {
			terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(e.LoadedStrings.ScanNoAreaSelected)), outputMode)
			time.Sleep(1 * time.Second)
			return nil, "", nil
		}

		totalCount, _ := e.MessageMgr.GetMessageCountForArea(currentAreaID)
		if totalCount == 0 {
			terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(e.LoadedStrings.ScanNoMessages)), outputMode)
			time.Sleep(1 * time.Second)
			return nil, "", nil
		}

		startMsg := determineStartMessage(e, scanCfg, currentAreaID, currentUser.Handle, totalCount)

		// Get terminal dimensions: prefer passed params, then user preferences, then defaults
		tw := termWidth
		if tw <= 0 {
			tw = currentUser.ScreenWidth
		}
		if tw <= 0 {
			tw = 80
		}
		th := termHeight
		if th <= 0 {
			th = currentUser.ScreenHeight
		}
		if th <= 0 {
			th = 24
		}

		_, action, readErr := runMessageReader(e, s, terminal, userManager, currentUser,
			nodeNumber, sessionStartTime, outputMode, startMsg, totalCount, true, tw, th, nil)
		if readErr != nil || action == "LOGOFF" {
			return nil, "LOGOFF", readErr
		}
		return nil, "", nil
	}

	// Load scan area header template (ANSI art) if available
	scanHeaderTemplate, headerErr := readTemplateFile(filepath.Join(e.MenuSetPath, "templates", "system_header", "HEADER"))
	if headerErr != nil && !os.IsNotExist(headerErr) {
		slog.Warn("failed to load scan header template", "node", nodeNumber, "error", headerErr)
	}

	// Multi-area scan: iterate through accessible areas
	allAreas := e.MessageMgr.ListAreas()

	// If tagged areas mode, check if user has any tagged areas
	if scanCfg.WhichAreas == 1 && len(currentUser.TaggedMessageAreaTags) == 0 {
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(e.LoadedStrings.ScanNoTaggedAreas)), outputMode)
		time.Sleep(2 * time.Second)
		return nil, "", nil
	}

	// Snapshot the user's current area/conf so we can restore after the scan
	origAreaID := currentUser.CurrentMessageAreaID
	origAreaTag := currentUser.CurrentMessageAreaTag
	origConfID := currentUser.CurrentMsgConferenceID
	origConfTag := currentUser.CurrentMsgConferenceTag

	// Create tagged area map for quick lookup
	taggedMap := make(map[string]bool)
	if scanCfg.WhichAreas == 1 {
		for _, tag := range currentUser.TaggedMessageAreaTags {
			taggedMap[tag] = true
		}
	}

	for _, area := range allAreas {
		if quitNewScan {
			break
		}

		// Check ACS
		if !checkACS(area.ACSRead, currentUser, s, terminal, sessionStartTime) {
			continue
		}

		// Filter by tagged areas if WhichAreas == 1
		if scanCfg.WhichAreas == 1 && !taggedMap[area.Tag] {
			continue
		}

		// Filter by conference if WhichAreas == 2 (all in conference)
		if scanCfg.WhichAreas == 2 && area.ConferenceID != currentUser.CurrentMsgConferenceID {
			continue
		}

		totalCount, countErr := e.MessageMgr.GetMessageCountForArea(area.ID)
		if countErr != nil || totalCount == 0 {
			continue
		}

		startMsg := determineStartMessage(e, scanCfg, area.ID, currentUser.Handle, totalCount)
		if startMsg > totalCount {
			continue // No messages to show for this area
		}

		// Resolve terminal dimensions once per iteration: prefer passed params, then user prefs, then defaults
		tw := termWidth
		if tw <= 0 {
			tw = currentUser.ScreenWidth
		}
		if tw <= 0 {
			tw = 80
		}
		th := termHeight
		if th <= 0 {
			th = currentUser.ScreenHeight
		}
		if th <= 0 {
			th = 24
		}

		// Set current area
		currentUser.CurrentMessageAreaID = area.ID
		currentUser.CurrentMessageAreaTag = area.Tag
		e.setUserMsgConference(currentUser, area.ConferenceID)

		// Clear screen before showing area progress to avoid overwriting
		// the message reader's separator/lightbar left on screen.
		terminalio.WriteProcessedBytes(terminal, []byte(ansi.ClearScreen()), outputMode)

		// Display scan area header: ANSI art template or plain text fallback.
		// Both paths write exactly one trailing newline before the nonStop block.
		if len(scanHeaderTemplate) > 0 {
			scanInfo := fmt.Sprintf("|09Scanning |01(|13%s|01)... |07[|15%d|07/|15%d|07 msgs]",
				area.Tag, startMsg, totalCount)
			merged := bytes.ReplaceAll(scanHeaderTemplate, []byte("|@"), []byte(scanInfo))
			merged = bytes.TrimRight(merged, "\r\n")
			terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes(merged), outputMode)
			terminalio.WriteProcessedBytes(terminal, []byte("\r\n"), outputMode)
		} else {
			boardMsg := fmt.Sprintf(e.LoadedStrings.ScanAreaProgress,
				area.Tag, startMsg, totalCount)
			boardMsg = strings.TrimRight(boardMsg, "\r\n")
			terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(boardMsg)), outputMode)
			terminalio.WriteProcessedBytes(terminal, []byte("\r\n"), outputMode)
		}

		if !nonStop {
			terminalio.WriteProcessedBytes(terminal, []byte("\r\n"), outputMode)
			// Show per-area lightbar: Read/Post/Jump/Skip/Quit/NonStop
			selectedKey, lbErr := runMsgLightbar(reader, terminal, scanAreaOptions, outputMode,
				hiColor, loColor, "", 0, false, 0)
			if lbErr != nil {
				if errors.Is(lbErr, io.EOF) {
					return nil, "LOGOFF", io.EOF
				}
				break
			}

			switch selectedKey {
			case 'R': // Read this area
				// Fall through to call runMessageReader below
			case 'P': // Post
				_, _, _ = runComposeMessageWithIH(e, s, scanIH, terminal, userManager, currentUser, nodeNumber,
					sessionStartTime, "", outputMode, tw, th)
				continue
			case 'J': // Jump to message #
				prevStart := startMsg
				handleJump(reader, terminal, outputMode, &startMsg, totalCount, e.LoadedStrings.MsgJumpPrompt, e.LoadedStrings.MsgInvalidMsgNum)
				// Match Pascal: if the jump succeeded, mark everything before
				// the new start as read (NScan.LastRead[Cb] := Valu(Inpt)-1).
				if startMsg != prevStart && startMsg > 1 {
					_ = e.MessageMgr.SetLastRead(area.ID, currentUser.Handle, startMsg-1)
				}
			case 'S': // Skip this area
				continue
			case 'Q': // Quit scanning
				quitNewScan = true
				continue
			case 'N': // NonStop
				nonStop = true
			}
		}

		if quitNewScan {
			break
		}

		// Read messages in this area
		_, action, readErr := runMessageReader(e, s, terminal, userManager, currentUser,
			nodeNumber, sessionStartTime, outputMode, startMsg, totalCount, true, tw, th, nil)
		if readErr != nil || action == "LOGOFF" {
			return nil, "LOGOFF", readErr
		}
		if action == "QUIT_NEWSCAN" {
			quitNewScan = true
		}

		// Update pointers
		if scanCfg.UpdatePointers {
			if saveErr := userManager.UpdateUser(currentUser); saveErr != nil {
				slog.Error("failed to save user data during newscan", "node", nodeNumber, "error", saveErr)
			}
		}
	}

	// Newscan complete — restore original conf/area before returning.
	// The scan loop may have called UpdateUser with a scanned area, so we must
	// persist the restored state back to DB as well.
	currentUser.CurrentMessageAreaID = origAreaID
	currentUser.CurrentMessageAreaTag = origAreaTag
	currentUser.CurrentMsgConferenceID = origConfID
	currentUser.CurrentMsgConferenceTag = origConfTag
	if err := userManager.UpdateUser(currentUser); err != nil {
		slog.Error("failed to restore user area after newscan", "node", nodeNumber, "error", err)
	}

	terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(e.LoadedStrings.ScanComplete)), outputMode)
	time.Sleep(1 * time.Second)

	return currentUser, "", nil
}

// runUpdateNewscanPointers allows a user to set newscan pointers to a specific date
// for the current conference only, or all conferences.
func runUpdateNewscanPointers(c *cmdCtx, args string) (*user.User, string, error) {
	e := c.e
	s := c.s
	terminal := c.terminal
	currentUser := c.currentUser
	nodeNumber := c.nodeNumber
	sessionStartTime := c.sessionStartTime
	outputMode := c.outputMode

	if currentUser == nil {
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(e.LoadedStrings.UpdatePtrsLoginRequired)), outputMode)
		time.Sleep(1 * time.Second)
		return nil, "", nil
	}

	scanIH := getSessionIH(s)
	reader := bufio.NewReader(scanIH)

	cancel := func() (*user.User, string, error) {
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(e.LoadedStrings.UpdatePtrsCancelled)), outputMode)
		time.Sleep(1 * time.Second)
		return currentUser, "", nil
	}

	// Prompt for target date
	terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(e.LoadedStrings.UpdatePtrsDatePrompt)), outputMode)
	dateInput, err := readLineInput(reader, terminal, outputMode, 10)
	if err != nil {
		if errors.Is(err, io.EOF) {
			return nil, "LOGOFF", io.EOF
		}
		if errors.Is(err, errInputAborted) {
			return cancel()
		}
		return nil, "", err
	}

	// Parse the date input: A=all new, N=mark all read, or a date string
	// scanDate: -2 = mark all read (LastRead = totalCount), 0 = all new (LastRead = 0), >0 = unix timestamp
	var scanDate int64 = -1 // sentinel: will be rejected below if not set
	dateInput = strings.TrimSpace(dateInput)
	if dateInput == "" {
		return cancel()
	}
	switch unicode.ToUpper(rune(dateInput[0])) {
	case 'A':
		scanDate = 0 // all new — set LastRead to 0
	case 'N':
		scanDate = -2 // none new — mark all read
	default:
		t, tErr := time.Parse("01/02/06", dateInput)
		if tErr != nil {
			return cancel()
		}
		scanDate = t.Unix()
	}

	// Prompt for scope: current conference or all
	terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(e.LoadedStrings.UpdatePtrsScopePrompt)), outputMode)
	scopeKey, keyErr := readSingleKey(reader)
	if keyErr != nil {
		if errors.Is(keyErr, io.EOF) {
			return nil, "LOGOFF", io.EOF
		}
		return nil, "", keyErr
	}
	terminalio.WriteProcessedBytes(terminal, []byte("\r\n"), outputMode)

	if unicode.ToUpper(scopeKey) == rune(0x1B) { // ESC
		return cancel()
	}

	allConferences := unicode.ToUpper(scopeKey) == 'A'

	// Iterate areas and update pointers
	allAreas := e.MessageMgr.ListAreas()
	updatedCount := 0
	saveErr := false

	for _, area := range allAreas {
		// Check ACS
		if !checkACS(area.ACSRead, currentUser, s, terminal, sessionStartTime) {
			continue
		}

		// Filter by scope
		if !allConferences && area.ConferenceID != currentUser.CurrentMsgConferenceID {
			continue
		}

		totalCount, countErr := e.MessageMgr.GetMessageCountForArea(area.ID)
		if countErr != nil {
			continue
		}

		var newLastRead int
		switch {
		case scanDate == 0:
			// All new: reset pointer to 0 so all messages appear unread
			newLastRead = 0
		case scanDate == -2:
			// Mark all read: set pointer to totalCount
			newLastRead = totalCount
		default:
			// Date-based: find first message on/after target date, set pointer to one before it
			cfg := &ScanConfig{ScanDate: scanDate}
			startMsg := determineStartMessage(e, cfg, area.ID, currentUser.Handle, totalCount)
			newLastRead = startMsg - 1
			if newLastRead < 0 {
				newLastRead = 0
			}
		}

		if setErr := e.MessageMgr.SetLastRead(area.ID, currentUser.Handle, newLastRead); setErr != nil {
			slog.Error("failed to set lastread", "node", nodeNumber, "area", area.ID, "error", setErr)
			saveErr = true
			continue
		}
		updatedCount++
	}

	if saveErr {
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(e.LoadedStrings.UpdatePtrsError)), outputMode)
	} else {
		msg := fmt.Sprintf(e.LoadedStrings.UpdatePtrsSuccess, updatedCount)
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
	}
	time.Sleep(1 * time.Second)

	return currentUser, "", nil
}

// determineStartMessage calculates the starting message number based on scan config.
func determineStartMessage(e *MenuExecutor, cfg *ScanConfig, areaID int, username string, totalCount int) int {
	if cfg.RangeStart > 0 {
		return cfg.RangeStart
	}

	if cfg.ScanDate == 0 {
		// All messages
		return 1
	}

	if cfg.ScanDate == -1 {
		// New messages only
		newCount, err := e.MessageMgr.GetNewMessageCount(areaID, username)
		if err != nil || newCount == 0 {
			return totalCount + 1 // No new messages, skip area
		}
		return totalCount - newCount + 1
	}

	// Date-based scan: find the first message on or after the target date.
	// Messages in a JAM base are stored chronologically, so we scan forward
	// until we find one with DateTime >= start of the target day.
	targetDate := time.Unix(cfg.ScanDate, 0).Truncate(24 * time.Hour)
	for i := 1; i <= totalCount; i++ {
		msg, err := e.MessageMgr.GetMessage(areaID, i)
		if err != nil || msg.IsDeleted {
			continue
		}
		if !msg.DateTime.Truncate(24 * time.Hour).Before(targetDate) {
			return i
		}
	}
	return totalCount + 1 // No messages on or after the target date; skip area
}
