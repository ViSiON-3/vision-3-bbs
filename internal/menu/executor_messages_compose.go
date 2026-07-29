package menu

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/editor"
	"github.com/ViSiON-3/vision-3-bbs/internal/message"
	"github.com/ViSiON-3/vision-3-bbs/internal/terminalio"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
	"github.com/gliderlabs/ssh"
	"golang.org/x/term"
)

// runComposeMessage handles the process of composing and saving a new message.
// It is a RunnableFunc-compatible wrapper; use runComposeMessageWithIH when a
// shared InputHandler is available to prevent the editor goroutine from consuming
// bytes after the editor exits.
func runComposeMessage(c *cmdCtx, args string) (*user.User, string, error) {
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

	return runComposeMessageWithIH(e, s, getSessionIH(s), terminal, userManager, currentUser, nodeNumber, sessionStartTime, args, outputMode, termWidth, termHeight)
}

// runComposeMessageWithIH is the internal implementation of runComposeMessage.
// ih is an optional pre-created *InputHandler shared with the caller's reader loop;
// pass nil to create a new one inside the editor.
func runComposeMessageWithIH(e *MenuExecutor, s ssh.Session, ih *editor.InputHandler, terminal *term.Terminal, userManager *user.UserMgr, currentUser *user.User, nodeNumber int, sessionStartTime time.Time, args string, outputMode ansi.OutputMode, termWidth int, termHeight int) (*user.User, string, error) {
	slog.Debug("running COMPOSEMSG", "node", nodeNumber, "args", args)

	// 1. Determine Target Area
	var areaTag string
	var area *message.MessageArea // Use pointer type
	var exists bool

	if args == "" {
		// No args provided, use current user's area
		if currentUser == nil {
			slog.Warn("COMPOSEMSG called without user and without args", "node", nodeNumber)
			msg := "\r\n|01Error: Not logged in and no area specified.|07\r\n"
			terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
			time.Sleep(1 * time.Second)
			return nil, "", nil // Return to menu
		}
		if currentUser.CurrentMessageAreaTag == "" || currentUser.CurrentMessageAreaID <= 0 {
			slog.Warn("COMPOSEMSG called but no current message area is set", "node", nodeNumber, "handle", currentUser.Handle)
			msg := "\r\n|01Error: No current message area selected.|07\r\n"
			terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
			time.Sleep(1 * time.Second)
			return nil, "", nil // Return to menu
		}
		areaTag = currentUser.CurrentMessageAreaTag
		slog.Info("COMPOSEMSG using current user area tag", "node", nodeNumber, "tag", areaTag)
		area, exists = e.MessageMgr.GetAreaByTag(areaTag)
	} else {
		// Args provided, use args as the area tag
		slog.Info("COMPOSEMSG using provided area tag in args", "node", nodeNumber, "tag", args)
		areaTag = args
		area, exists = e.MessageMgr.GetAreaByTag(areaTag)
	}

	// Common checks after determining areaTag/area
	if !exists {
		slog.Error("COMPOSEMSG called with invalid area tag", "node", nodeNumber, "tag", areaTag)
		msg := fmt.Sprintf("\r\n|01Invalid message area: %s|07\r\n", areaTag)
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
		time.Sleep(1 * time.Second)
		return nil, "", nil // Return to menu, not an error
	}

	// Check user logged in (required for ACS check and posting)
	if currentUser == nil {
		slog.Warn("COMPOSEMSG reached ACS check without logged in user", "node", nodeNumber, "tag", areaTag)
		msg := "\r\n|01Error: You must be logged in to post messages.|07\r\n"
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
		time.Sleep(1 * time.Second)
		return nil, "", nil // Return to menu
	}

	// Check ACSWrite permission for the area and currentUser
	if !checkACS(area.ACSWrite, currentUser, s, terminal, sessionStartTime) {
		slog.Warn("user denied post access to area", "node", nodeNumber, "handle", currentUser.Handle, "tag", area.Tag, "acs", area.ACSWrite)
		// TODO: Display user-friendly error message (e.g., Access Denied String)
		time.Sleep(1 * time.Second)
		return nil, "", nil // Return to menu, not an error
	}

	// === PASCAL-STYLE MESSAGE POSTING FLOW ===

	// 2. Prompt for Title (30 chars)
	titlePrompt := e.LoadedStrings.MsgTitleStr
	if titlePrompt == "" {
		titlePrompt = "|07Title: |15"
	}
	terminalio.WriteProcessedBytes(terminal, []byte("\r\n"), outputMode)

	var subject string
	for {
		val, aborted, ferr := e.promptComposeField(s, terminal, ih, titlePrompt, 30, "", "title", outputMode, nodeNumber, termWidth, termHeight)
		if ferr != nil {
			if errors.Is(ferr, io.EOF) {
				return nil, "LOGOFF", io.EOF
			}
			slog.Error("failed reading title input", "node", nodeNumber, "error", ferr)
			terminalio.WriteProcessedBytes(terminal, []byte("\r\nError reading title.\r\n"), outputMode)
			time.Sleep(1 * time.Second)
			return nil, "", nil // Return to menu
		}
		if aborted {
			return nil, "", nil
		}
		subject = strings.TrimSpace(val)
		if subject != "" {
			break
		}
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte("|01Subject is required.|07\r\n")), outputMode)
	}

	// 3. Prompt for To (24 chars, default "All")
	toPrompt := e.LoadedStrings.MsgToStr
	if toPrompt == "" {
		toPrompt = "|07To: |15"
	}
	var toUser string
	for {
		val, aborted, ferr := e.promptComposeField(s, terminal, ih, toPrompt, 24, "All", "'to'", outputMode, nodeNumber, termWidth, termHeight)
		if ferr != nil {
			if errors.Is(ferr, io.EOF) {
				return nil, "LOGOFF", io.EOF
			}
			slog.Error("failed reading 'to' input", "node", nodeNumber, "error", ferr)
			terminalio.WriteProcessedBytes(terminal, []byte("\r\nError reading recipient.\r\n"), outputMode)
			time.Sleep(1 * time.Second)
			return nil, "", nil
		}
		if aborted {
			return nil, "", nil
		}
		toUser = val
		break
	}
	toUser = strings.TrimSpace(toUser)
	if toUser == "" {
		toUser = "All"
	}

	// 4. Prompt for Anonymous (if user level >= AnonymousLevel)
	isAnonymous := false
	allowAnon := currentUser.AccessLevel >= e.ServerCfg.AnonymousLevel
	if allowAnon {
		areaAllowsAnon := true
		if area.AllowAnon != nil {
			areaAllowsAnon = *area.AllowAnon
		}
		confAllowsAnon := true
		if e.ConferenceMgr != nil && area.ConferenceID != 0 {
			if conf, ok := e.ConferenceMgr.GetByID(area.ConferenceID); ok {
				if conf.AllowAnon != nil {
					confAllowsAnon = *conf.AllowAnon
				}
			}
		}
		allowAnon = areaAllowsAnon && confAllowsAnon
	}
	if allowAnon {
		anonPrompt := e.LoadedStrings.MsgAnonStr
		if anonPrompt == "" {
			anonPrompt = "|07Anonymous? @"
		}
		isAnon, err := e.PromptYesNo(s, terminal, anonPrompt, outputMode, nodeNumber, termWidth, termHeight, false)
		if err != nil {
			if errors.Is(err, io.EOF) {
				slog.Info("user disconnected during anonymous input", "node", nodeNumber)
				return nil, "LOGOFF", io.EOF
			}
			slog.Error("failed reading anonymous input", "node", nodeNumber, "error", err)
			isAnon = false
		}
		isAnonymous = isAnon
		terminalio.WriteProcessedBytes(terminal, []byte("\r\n"), outputMode)
	}

	// 5. Determine the sender display name for the editor header (@F@ field).
	// Priority: anonymous string > real name (if area requires it) > handle.
	fromName := currentUser.Handle
	if area.RealNameOnly && strings.TrimSpace(currentUser.RealName) != "" {
		fromName = currentUser.RealName
	}
	if isAnonymous {
		fromName = strings.TrimSpace(e.LoadedStrings.AnonymousName)
		if fromName == "" {
			fromName = "Anonymous"
		}
	}

	// 6. Call the Editor
	slog.Debug("clearing screen before calling editor", "node", nodeNumber)
	terminalio.WriteProcessedBytes(terminal, []byte(ansi.ClearScreen()), outputMode) // Clear screen before editor

	// Build editor context for FSEDITOR.ANS header placeholders
	nextMsgNum := 0
	if msgCount, mcErr := e.MessageMgr.GetMessageCountForArea(area.ID); mcErr == nil {
		nextMsgNum = msgCount + 1
	}
	composeConfName := "Local"
	if area.ConferenceID != 0 && e.ConferenceMgr != nil {
		if conf, ok := e.ConferenceMgr.GetByID(area.ConferenceID); ok {
			composeConfName = conf.Name
		}
	}
	editorCtx := editor.EditorContext{
		NodeNumber: nodeNumber,
		NextMsgNum: nextMsgNum,
		ConfArea:   fmt.Sprintf("%s > %s", composeConfName, area.Name),
	}

	// No quote data for new messages
	body, saved, err := editor.RunEditorWithMetadata("", s, s, outputMode, subject, toUser, fromName, isAnonymous, "", "", "", "", false, nil, ih, editorCtx)
	slog.Debug("editor returned", "node", nodeNumber, "error", err, "saved", saved, "length", len(body))

	if err != nil {
		slog.Error("editor failed", "node", nodeNumber, "handle", currentUser.Handle, "error", err)
		return nil, "", fmt.Errorf("editor error: %w", err)
	}

	// Clear screen after editor exits
	terminalio.WriteProcessedBytes(terminal, []byte(ansi.ClearScreen()), outputMode)

	if !saved {
		slog.Info("user aborted message composition", "node", nodeNumber, "handle", currentUser.Handle, "tag", area.Tag)
		terminalio.WriteProcessedBytes(terminal, []byte("\r\nMessage aborted.\r\n"), outputMode)
		time.Sleep(1 * time.Second)
		return nil, "", nil // Return to current menu
	}

	if strings.TrimSpace(body) == "" {
		slog.Info("user saved empty message", "node", nodeNumber, "handle", currentUser.Handle, "tag", area.Tag)
		terminalio.WriteProcessedBytes(terminal, []byte("\r\nMessage body empty. Aborting post.\r\n"), outputMode)
		time.Sleep(1 * time.Second)
		return nil, "", nil // Return to current menu
	}

	// 7. Append auto-signature if user has one and message is not anonymous
	if !isAnonymous && currentUser.AutoSignature != "" {
		body = body + "\n\n" + currentUser.AutoSignature
	}

	// 8. Save the Message via JAM backend (fromName already computed above)

	msgNum, err := e.MessageMgr.AddMessage(area.ID, fromName, toUser, subject, body, "")
	if err != nil {
		slog.Error("failed to save message", "node", nodeNumber, "handle", currentUser.Handle, "tag", area.Tag, "error", err)
		errorMsg := ansi.ReplacePipeCodes([]byte("\r\n|01Error saving message!|07\r\n"))
		terminalio.WriteProcessedBytes(terminal, errorMsg, outputMode)
		time.Sleep(2 * time.Second)
		return nil, "", fmt.Errorf("failed saving message: %w", err)
	}

	// 8. Update user message counter
	currentUser.MessagesPosted++
	if err := userManager.UpdateUser(currentUser); err != nil {
		slog.Error("failed to update MessagesPosted", "node", nodeNumber, "handle", currentUser.Handle, "error", err)
	}

	// 9. Confirmation
	slog.Info("user posted message", "node", nodeNumber, "handle", currentUser.Handle, "num", msgNum, "tag", area.Tag)
	confirmMsg := ansi.ReplacePipeCodes([]byte("\r\n|02Message Posted!|07\r\n"))
	terminalio.WriteProcessedBytes(terminal, confirmMsg, outputMode)
	time.Sleep(1 * time.Second)

	return nil, "", nil
}
