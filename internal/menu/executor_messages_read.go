package menu

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/terminalio"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// runReadMsgs handles reading messages from the user's current area.
// Delegates to runMessageReader which uses Pascal-style MSGHDR templates and lightbar navigation.
func runReadMsgs(c *cmdCtx, args string) (*user.User, string, error) {
	e := c.e
	s := c.s
	terminal := c.terminal
	userManager := c.userManager
	currentUser := c.currentUser
	nodeNumber := c.nodeNumber
	sessionStartTime := c.sessionStartTime
	outputMode := c.outputMode
	termWidth := c.termWidth
	termHeight := c.termHeight

	slog.Debug("running READMSGS", "node", nodeNumber)

	if currentUser == nil {
		msg := "\r\n|01Error: You must be logged in to read messages.|07\r\n"
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
		time.Sleep(1 * time.Second)
		return nil, "", nil
	}

	currentAreaID := currentUser.CurrentMessageAreaID
	currentAreaTag := currentUser.CurrentMessageAreaTag

	if currentAreaID <= 0 || currentAreaTag == "" {
		msg := "\r\n|01Error: No message area selected.|07\r\n"
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
		time.Sleep(1 * time.Second)
		return nil, "", nil
	}

	// Prompt for header selection if not yet set
	if currentUser.MsgHdr < 1 || currentUser.MsgHdr > 14 {
		// Check if MSGHDR.ANS exists for selection screen
		selPath := filepath.Join(e.MenuSetPath, "templates", "message_headers", "MSGHDR.ANS")
		if _, statErr := os.Stat(selPath); statErr == nil {
			terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte("\r\n|07Please select a message header style.|07\r\n")), outputMode)
			time.Sleep(500 * time.Millisecond)
			_, _, _ = runGetHeaderType(&cmdCtx{e: e, s: s, terminal: terminal, userManager: userManager, currentUser: currentUser, nodeNumber: nodeNumber, sessionStartTime: sessionStartTime, outputMode: outputMode, termWidth: termWidth, termHeight: termHeight}, "")
		}
	}

	totalMessageCount, err := e.MessageMgr.GetMessageCountForArea(currentAreaID)
	if err != nil {
		msg := fmt.Sprintf("\r\n|01Error loading message info for area %s.|07\r\n", currentAreaTag)
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
		time.Sleep(1 * time.Second)
		return nil, "", err
	}

	if totalMessageCount == 0 {
		msg := fmt.Sprintf("\r\n|07No messages in area |15%s|07.\r\n", currentAreaTag)
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
		time.Sleep(1 * time.Second)
		return nil, "", nil
	}

	// Determine starting message
	newCount, err := e.MessageMgr.GetNewMessageCount(currentAreaID, currentUser.Handle)
	if err != nil {
		newCount = 0
	}

	var currentMsgNum int
	if newCount > 0 {
		currentMsgNum = totalMessageCount - newCount + 1
	} else {
		// No new messages - prompt for specific message number
		noNewMsg := fmt.Sprintf("\r\n|07No new messages in area |15%s|07.", currentAreaTag)
		totalMsg := fmt.Sprintf(" |07Total messages: |15%d|07.", totalMessageCount)
		promptMsg := fmt.Sprintf("\r\n|07Read message # (|151-%d|07, |15Enter|07=Cancel): |15", totalMessageCount)
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(noNewMsg+totalMsg)), outputMode)
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(promptMsg)), outputMode)

		input, readErr := readLineFromSessionIH(s, terminal)
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				return nil, "LOGOFF", io.EOF
			}
			return nil, "", readErr
		}
		selectedNumStr := strings.TrimSpace(input)
		if selectedNumStr == "" {
			return nil, "", nil
		}
		selectedNum, parseErr := strconv.Atoi(selectedNumStr)
		if parseErr != nil || selectedNum < 1 || selectedNum > totalMessageCount {
			msg := fmt.Sprintf("\r\n|01Invalid message number: %s|07\r\n", selectedNumStr)
			terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
			time.Sleep(1 * time.Second)
			return nil, "", nil
		}
		currentMsgNum = selectedNum
	}

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

	// Delegate to the new message reader with MSGHDR templates and lightbar
	return runMessageReader(e, s, terminal, userManager, currentUser, nodeNumber,
		sessionStartTime, outputMode, currentMsgNum, totalMessageCount, false, tw, th, nil)
}

// runNewscan handles the message newscan with Pascal-style GetScanType setup and multi-area flow.
func runNewscan(c *cmdCtx, args string) (*user.User, string, error) {
	e := c.e
	s := c.s
	terminal := c.terminal
	userManager := c.userManager
	currentUser := c.currentUser
	nodeNumber := c.nodeNumber
	sessionStartTime := c.sessionStartTime
	outputMode := c.outputMode
	termWidth := c.termWidth
	termHeight := c.termHeight

	slog.Debug("running NEWSCAN", "node", nodeNumber, "handle", currentUser.Handle)

	// Refresh user from the in-process manager so we pick up any newscan
	// setting changes saved during this session (e.g. tagged areas modified
	// via newscan config or by another goroutine on the same node).
	if reloaded, exists := userManager.GetUserByID(currentUser.ID); exists {
		currentUser = reloaded
	}

	// Determine if this is a "current area only" scan based on args
	currentOnly := strings.ToUpper(strings.TrimSpace(args)) == "CURRENT"

	return runNewScanAll(e, s, terminal, userManager, currentUser, nodeNumber,
		sessionStartTime, outputMode, currentOnly, termWidth, termHeight)
}
