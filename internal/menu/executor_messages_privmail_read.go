package menu

import (
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/message"
	"github.com/ViSiON-3/vision-3-bbs/internal/terminalio"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// ownPrivateMailFilter returns a msgOwnershipFilter that accepts only private
// messages addressed to handle (case-insensitive). It is the single source of
// truth for "may this user see this PRIVMAIL message" used by both the reader
// and the list, so navigation can never surface another user's mail.
func ownPrivateMailFilter(handle string) msgOwnershipFilter {
	return func(m *message.DisplayMessage) bool {
		return m.IsPrivate && strings.EqualFold(m.To, handle)
	}
}

// runReadPrivateMail handles reading private mail for the current user.
// It filters messages to only show those addressed to the current user with MSG_PRIVATE flag.
func runReadPrivateMail(c *cmdCtx, args string) (*user.User, string, error) {
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

	slog.Debug("running READPRIVMAIL", "node", nodeNumber)

	if currentUser == nil {
		msg := "\r\n|01Error: You must be logged in to read private mail.|07\r\n"
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
		time.Sleep(1 * time.Second)
		return nil, "", nil
	}

	// Get PRIVMAIL area
	privmailArea, exists := e.MessageMgr.GetAreaByTag("PRIVMAIL")
	if !exists {
		slog.Error("PRIVMAIL area not found", "node", nodeNumber)
		msg := "\r\n|01Error: Private mail area not configured.|07\r\n"
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
		time.Sleep(1 * time.Second)
		return nil, "", nil
	}

	// Get JAM base for PRIVMAIL area
	base, err := e.MessageMgr.GetBase(privmailArea.ID)
	if err != nil {
		slog.Error("JAM base not open for PRIVMAIL area", "node", nodeNumber, "error", err)
		msg := "\r\n|01Error: Private mail base not available.|07\r\n"
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
		time.Sleep(1 * time.Second)
		return nil, "", nil
	}
	defer func() {
		if cerr := base.Close(); cerr != nil {
			slog.Warn("closing JAM base", "error", cerr)
		}
	}()

	// Get total message count
	totalMessages, err := e.MessageMgr.GetMessageCountForArea(privmailArea.ID)
	if err != nil {
		slog.Error("failed to get message count for PRIVMAIL", "node", nodeNumber, "error", err)
		msg := "\r\n|01Error loading private mail.|07\r\n"
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
		time.Sleep(1 * time.Second)
		return nil, "", err
	}

	if totalMessages == 0 {
		msg := "\r\n|07No private mail found.|07\r\n"
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
		time.Sleep(1 * time.Second)
		return nil, "", nil
	}

	// Scan all messages and filter for private messages addressed to current user
	// CRITICAL SECURITY: Must check BOTH IsPrivate() AND To field matches current user
	privateMessages := []int{}
	for msgNum := 1; msgNum <= totalMessages; msgNum++ {
		msg, err := base.ReadMessage(msgNum)
		if err != nil {
			slog.Warn("failed to read message in PRIVMAIL", "node", nodeNumber, "msg", msgNum, "error", err)
			continue
		}

		// Skip deleted messages
		if msg.IsDeleted() {
			continue
		}

		// Check if message is private AND addressed to current user (case-insensitive)
		if msg.IsPrivate() && strings.EqualFold(msg.To, currentUser.Handle) {
			privateMessages = append(privateMessages, msgNum)
		}
	}

	if len(privateMessages) == 0 {
		msg := "\r\n|07No private mail found for you.|07\r\n"
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
		time.Sleep(1 * time.Second)
		return nil, "", nil
	}

	// Display count and read messages using the message reader
	confirmMsg := fmt.Sprintf("\r\n|02Found %d private message(s) for you.|07\r\n", len(privateMessages))
	terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(confirmMsg)), outputMode)
	time.Sleep(500 * time.Millisecond)

	// Temporarily set current area to PRIVMAIL for the message reader
	originalAreaID := currentUser.CurrentMessageAreaID
	originalAreaTag := currentUser.CurrentMessageAreaTag
	currentUser.CurrentMessageAreaID = privmailArea.ID
	currentUser.CurrentMessageAreaTag = privmailArea.Tag

	// Start reading from the first private message
	startMsgNum := privateMessages[0]

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

	// Call message reader with the filtered list
	// Constrain the reader to the current user's own private messages so that
	// next/prev/jump navigation can never reveal another user's mail.
	updatedUser, nextMenu, err := runMessageReader(e, s, terminal, userManager, currentUser, nodeNumber,
		sessionStartTime, outputMode, startMsgNum, totalMessages, false, tw, th, ownPrivateMailFilter(currentUser.Handle))

	// Restore original area
	if updatedUser != nil {
		updatedUser.CurrentMessageAreaID = originalAreaID
		updatedUser.CurrentMessageAreaTag = originalAreaTag
		currentUser.CurrentMessageAreaID = originalAreaID
		currentUser.CurrentMessageAreaTag = originalAreaTag
	}

	return updatedUser, nextMenu, err
}

// runListPrivateMail handles listing private mail for the current user.
// It temporarily switches to the PRIVMAIL area and calls the standard list function.
func runListPrivateMail(c *cmdCtx, args string) (*user.User, string, error) {
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

	slog.Debug("running LISTPRIVMAIL", "node", nodeNumber)

	if currentUser == nil {
		msg := "\r\n|01Error: You must be logged in to list private mail.|07\r\n"
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
		time.Sleep(1 * time.Second)
		return nil, "", nil
	}

	// Get PRIVMAIL area
	privmailArea, exists := e.MessageMgr.GetAreaByTag("PRIVMAIL")
	if !exists {
		slog.Error("PRIVMAIL area not found", "node", nodeNumber)
		msg := "\r\n|01Error: Private mail area not configured.|07\r\n"
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
		time.Sleep(1 * time.Second)
		return nil, "", nil
	}

	// Temporarily set current area to PRIVMAIL
	originalAreaID := currentUser.CurrentMessageAreaID
	originalAreaTag := currentUser.CurrentMessageAreaTag
	currentUser.CurrentMessageAreaID = privmailArea.ID
	currentUser.CurrentMessageAreaTag = privmailArea.Tag

	// List only the current user's own private mail (filtered both in the list
	// and in the reader it opens).
	updatedUser, nextMenu, err := runListMsgsFiltered(&cmdCtx{e: e, s: s, terminal: terminal, userManager: userManager, currentUser: currentUser, nodeNumber: nodeNumber, sessionStartTime: sessionStartTime, outputMode: outputMode, termWidth: termWidth, termHeight: termHeight}, args, ownPrivateMailFilter(currentUser.Handle))

	// Restore original area
	if updatedUser != nil {
		updatedUser.CurrentMessageAreaID = originalAreaID
		updatedUser.CurrentMessageAreaTag = originalAreaTag
	}
	currentUser.CurrentMessageAreaID = originalAreaID
	currentUser.CurrentMessageAreaTag = originalAreaTag

	return updatedUser, nextMenu, err
}
