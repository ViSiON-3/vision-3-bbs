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
	"github.com/ViSiON-3/vision-3-bbs/internal/conference"
	"github.com/ViSiON-3/vision-3-bbs/internal/editor"
	"github.com/ViSiON-3/vision-3-bbs/internal/message"
	"github.com/ViSiON-3/vision-3-bbs/internal/terminalio"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
	"github.com/gliderlabs/ssh"
	"golang.org/x/term"
)

// runListMessageAreas displays a list of message areas using templates, then pauses.
func runListMessageAreas(c *cmdCtx, args string) (*user.User, string, error) {
	e := c.e
	s := c.s
	terminal := c.terminal
	currentUser := c.currentUser
	nodeNumber := c.nodeNumber
	sessionStartTime := c.sessionStartTime
	outputMode := c.outputMode

	slog.Debug("running LISTMSGAR", "node", nodeNumber)

	// Filter to current conference if user is logged in, otherwise show all
	filterConfID := -1
	if currentUser != nil {
		filterConfID = currentUser.CurrentMsgConferenceID
	}
	if _, err := displayMessageAreaListFiltered(e, s, terminal, currentUser, outputMode, nodeNumber, sessionStartTime, filterConfID); err != nil {
		return nil, "", err
	}

	// Wait for Enter using configured PauseString
	pausePrompt := e.LoadedStrings.PauseString
	if pausePrompt == "" {
		pausePrompt = "\r\n|07Press |15[ENTER]|07 to continue... "
	}
	terminalio.WriteStringCP437(terminal, ansi.ReplacePipeCodes([]byte(pausePrompt)), outputMode)

	ih := getSessionIH(s)
	for {
		key, err := ih.ReadKey()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil, "LOGOFF", io.EOF
			}
			return nil, "", err
		}
		if key == int('\r') || key == int('\n') {
			break
		}
	}

	return nil, "", nil
}

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
			wErr := terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
			if wErr != nil { /* Log? */
			}
			time.Sleep(1 * time.Second)
			return nil, "", nil // Return to menu
		}
		if currentUser.CurrentMessageAreaTag == "" || currentUser.CurrentMessageAreaID <= 0 {
			slog.Warn("COMPOSEMSG called but no current message area is set", "node", nodeNumber, "handle", currentUser.Handle)
			msg := "\r\n|01Error: No current message area selected.|07\r\n"
			wErr := terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
			if wErr != nil { /* Log? */
			}
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
		wErr := terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
		if wErr != nil { /* Log? */
		}
		time.Sleep(1 * time.Second)
		return nil, "", nil // Return to menu, not an error
	}

	// Check user logged in (required for ACS check and posting)
	if currentUser == nil {
		slog.Warn("COMPOSEMSG reached ACS check without logged in user", "node", nodeNumber, "tag", areaTag)
		msg := "\r\n|01Error: You must be logged in to post messages.|07\r\n"
		wErr := terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
		if wErr != nil { /* Log? */
		}
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
	wErr := terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(titlePrompt)), outputMode)
	if wErr != nil {
		slog.Warn("failed to write title prompt", "node", nodeNumber, "error", wErr)
	}

	var subject string
	var err error
	for {
		subject, err = styledInput(terminal, s, outputMode, 30, "")
		if err != nil {
			if errors.Is(err, io.EOF) {
				slog.Info("user disconnected during title input", "node", nodeNumber)
				return nil, "LOGOFF", io.EOF
			}
			if errors.Is(err, errInputAborted) {
				abort, confirmErr := e.confirmAbortPost(s, terminal, outputMode, nodeNumber, termWidth, termHeight)
				if confirmErr != nil {
					if errors.Is(confirmErr, io.EOF) {
						return nil, "LOGOFF", io.EOF
					}
					return nil, "", nil
				}
				if abort {
					return nil, "", nil
				}
				// No — re-show prompt and retry
				wErr = terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(titlePrompt)), outputMode)
				if wErr != nil {
					slog.Warn("failed to rewrite title prompt", "node", nodeNumber, "error", wErr)
				}
				continue
			}
			slog.Error("failed reading title input", "node", nodeNumber, "error", err)
			terminalio.WriteProcessedBytes(terminal, []byte("\r\nError reading title.\r\n"), outputMode)
			time.Sleep(1 * time.Second)
			return nil, "", nil // Return to menu
		}
		subject = strings.TrimSpace(subject)
		if subject != "" {
			break
		}
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte("|01Subject is required.|07\r\n")), outputMode)
		wErr = terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(titlePrompt)), outputMode)
		if wErr != nil {
			slog.Warn("failed to rewrite title prompt", "node", nodeNumber, "error", wErr)
		}
	}

	// 3. Prompt for To (24 chars, default "All")
	toPrompt := e.LoadedStrings.MsgToStr
	if toPrompt == "" {
		toPrompt = "|07To: |15"
	}
	var toUser string
	for {
		wErr = terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(toPrompt)), outputMode)
		if wErr != nil {
			slog.Warn("failed to write 'to' prompt", "node", nodeNumber, "error", wErr)
		}
		toUser, err = styledInput(terminal, s, outputMode, 24, "All")
		if err != nil {
			if errors.Is(err, io.EOF) {
				slog.Info("user disconnected during 'to' input", "node", nodeNumber)
				return nil, "LOGOFF", io.EOF
			}
			if errors.Is(err, errInputAborted) {
				abort, confirmErr := e.confirmAbortPost(s, terminal, outputMode, nodeNumber, termWidth, termHeight)
				if confirmErr != nil {
					if errors.Is(confirmErr, io.EOF) {
						return nil, "LOGOFF", io.EOF
					}
					return nil, "", nil
				}
				if abort {
					return nil, "", nil
				}
				continue // No — re-show prompt and retry
			}
			slog.Error("failed reading 'to' input", "node", nodeNumber, "error", err)
			terminalio.WriteProcessedBytes(terminal, []byte("\r\nError reading recipient.\r\n"), outputMode)
			time.Sleep(1 * time.Second)
			return nil, "", nil
		}
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

// runPromptAndComposeMessage lists areas, prompts for selection, checks permissions, and calls runComposeMessage.
func runPromptAndComposeMessage(c *cmdCtx, args string) (*user.User, string, error) {
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

	slog.Debug("running runPromptAndComposeMessage", "node", nodeNumber)

	if currentUser == nil {
		slog.Warn("runPromptAndComposeMessage called without logged in user", "node", nodeNumber)
		// Display user-friendly error
		msg := "\r\n|01Error: You must be logged in to post messages.|07\r\n"
		wErr := terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
		if wErr != nil {
			slog.Error("failed writing login required message", "node", nodeNumber, "error", wErr)
		}
		time.Sleep(1 * time.Second)
		return nil, "", nil // Return to menu
	}

	// 1. Display available message areas (adapted from runListMessageAreas, without pause)
	topTemplateFilename := "MSGAREA.TOP"
	midTemplateFilename := "MSGAREA.MID"
	botTemplateFilename := "MSGAREA.BOT" // We'll use BOT template differently here
	templateDir := filepath.Join(e.MenuSetPath, "templates")
	topTemplatePath := filepath.Join(templateDir, topTemplateFilename)
	midTemplatePath := filepath.Join(templateDir, midTemplateFilename)
	botTemplatePath := filepath.Join(templateDir, botTemplateFilename) // Load BOT template

	topTemplateBytes, errTop := readTemplateFile(topTemplatePath)
	midTemplateBytes, errMid := readTemplateFile(midTemplatePath)
	botTemplateBytes, errBot := readTemplateFile(botTemplatePath) // Load BOT template

	if errTop != nil || errMid != nil || errBot != nil { // Check BOT error too
		slog.Error("failed to load one or more MSGAREA template files for prompt", "node", nodeNumber, "top", errTop, "mid", errMid, "bot", errBot)
		msg := "\r\n|01Error loading Message Area screen templates.|07\r\n"
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
		time.Sleep(1 * time.Second)
		return nil, "", fmt.Errorf("failed loading MSGAREA templates for prompt")
	}

	processedTopTemplate := ansi.ReplacePipeCodes(topTemplateBytes)
	processedMidTemplate := string(ansi.ReplacePipeCodes(midTemplateBytes))
	processedBotTemplate := ansi.ReplacePipeCodes(botTemplateBytes) // Process BOT template

	areas := e.MessageMgr.ListAreas() // Get all areas, sorted by Position

	terminalio.WriteProcessedBytes(terminal, []byte(ansi.ClearScreen()), outputMode) // Clear before displaying list
	terminalio.WriteProcessedBytes(terminal, processedTopTemplate, outputMode)       // Write TOP

	if len(areas) == 0 {
		slog.Debug("no message areas available to post in", "node", nodeNumber)
		noAreasMsg := ansi.ReplacePipeCodes([]byte("\r\n|07No message areas available.|07\r\n"))
		terminalio.WriteProcessedBytes(terminal, noAreasMsg, outputMode)
		time.Sleep(1 * time.Second)
		return nil, "", nil // Return to menu
	}

	// Display areas with 1-based sequential numbering
	var displayedAreas []*message.MessageArea
	for _, area := range areas {
		displayedAreas = append(displayedAreas, area)
		line := processedMidTemplate
		name := string(ansi.ReplacePipeCodes([]byte(area.Name)))
		desc := string(ansi.ReplacePipeCodes([]byte(area.Description)))
		idStr := strconv.Itoa(len(displayedAreas))
		tag := string(ansi.ReplacePipeCodes([]byte(area.Tag)))

		line = strings.ReplaceAll(line, "^ID", idStr)
		line = strings.ReplaceAll(line, "^TAG", tag)
		line = strings.ReplaceAll(line, "^NA", name)
		line = strings.ReplaceAll(line, "^DS", desc)

		terminalio.WriteProcessedBytes(terminal, []byte(line), outputMode) // Write MID for each area
	}

	terminalio.WriteProcessedBytes(terminal, processedBotTemplate, outputMode) // Write BOT

	// 2. Prompt for Area Selection
	prompt := "\r\n|07Enter Area # or Tag to Post In (or Enter to cancel): |15"
	slog.Debug("writing prompt for message area selection", "node", nodeNumber, "bytes", fmt.Sprintf("%x", ansi.ReplacePipeCodes([]byte(prompt))))
	wErr := terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(prompt)), outputMode)
	if wErr != nil {
		slog.Warn("failed to write area selection prompt", "node", nodeNumber, "error", wErr)
	}

	input, err := readLineFromSessionIH(s, terminal)
	if err != nil {
		if errors.Is(err, io.EOF) {
			slog.Info("user disconnected during area selection", "node", nodeNumber)
			return nil, "LOGOFF", io.EOF
		}
		slog.Error("failed reading area selection input", "node", nodeNumber, "error", err)
		return nil, "", fmt.Errorf("failed reading area selection: %w", err)
	}

	selectedAreaStr := strings.TrimSpace(input)
	if selectedAreaStr == "" {
		slog.Info("user cancelled message posting", "node", nodeNumber)
		terminalio.WriteProcessedBytes(terminal, []byte("\r\nPost cancelled.\r\n"), outputMode)
		time.Sleep(500 * time.Millisecond)
		return nil, "", nil // Return to current menu
	}

	// 3. Find Selected Area and Check Permissions
	var selectedArea *message.MessageArea
	var foundArea bool

	// Try parsing as list number (1-based) first
	if num, err := strconv.Atoi(selectedAreaStr); err == nil {
		if num >= 1 && num <= len(displayedAreas) {
			selectedArea = displayedAreas[num-1]
			foundArea = true
		}
	}

	// If not found by number, try by Tag (case-insensitive)
	if !foundArea {
		selectedArea, foundArea = e.MessageMgr.GetAreaByTag(strings.ToUpper(selectedAreaStr))
	}

	if !foundArea {
		slog.Warn("invalid area selection", "node", nodeNumber, "selection", selectedAreaStr, "handle", currentUser.Handle)
		// TODO: Use configurable string
		msg := fmt.Sprintf("\r\n|01Invalid area: %s|07\r\n", selectedAreaStr)
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
		time.Sleep(1 * time.Second)
		// TODO: Need to redraw menu
		return nil, "", nil // Return to menu
	}

	// Check write permission
	if !checkACS(selectedArea.ACSWrite, currentUser, s, terminal, sessionStartTime) {
		slog.Warn("user denied post access to selected area", "node", nodeNumber, "handle", currentUser.Handle, "tag", selectedArea.Tag, "acs", selectedArea.ACSWrite)
		// TODO: Use configurable string for access denied
		msg := fmt.Sprintf("\r\n|01Access denied to post in area: %s|07\r\n", selectedArea.Name)
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
		time.Sleep(1 * time.Second)
		// TODO: Need to redraw menu
		return nil, "", nil // Return to menu
	}

	slog.Info("user selected area to post in", "node", nodeNumber, "handle", currentUser.Handle, "name", selectedArea.Name, "tag", selectedArea.Tag)

	// 4. Call runComposeMessage with the selected Area Tag
	// Pass the area tag as the argument string
	return runComposeMessage(&cmdCtx{e: e, s: s, terminal: terminal, userManager: userManager, currentUser: currentUser, nodeNumber: nodeNumber, sessionStartTime: sessionStartTime, outputMode: outputMode, termWidth: termWidth, termHeight: termHeight}, selectedArea.Tag)
}

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
			runGetHeaderType(&cmdCtx{e: e, s: s, terminal: terminal, userManager: userManager, currentUser: currentUser, nodeNumber: nodeNumber, sessionStartTime: sessionStartTime, outputMode: outputMode, termWidth: termWidth, termHeight: termHeight}, "")
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

// generateReplySubject creates a suitable subject line for a reply.
// It prepends "Re: " unless the original subject already starts with it (case-insensitive).
func generateReplySubject(originalSubject string) string {
	upperSubject := strings.ToUpper(strings.TrimSpace(originalSubject))
	if strings.HasPrefix(upperSubject, "RE:") {
		return originalSubject // Already a reply
	}
	return "Re: " + originalSubject
}

// sanitizeControlChars strips control characters from user input to prevent
// ANSI/terminal injection. Preserves tabs and newlines.
func sanitizeControlChars(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '\t' || r == '\n' || r == '\r' {
			return r
		}
		if r < 0x20 || r == 0x7f {
			return -1 // strip
		}
		return r
	}, s)
}

// runSelectMessageArea displays message areas and prompts the user to select one.
func runSelectMessageArea(c *cmdCtx, args string) (*user.User, string, error) {
	e := c.e
	s := c.s
	terminal := c.terminal
	userManager := c.userManager
	currentUser := c.currentUser
	nodeNumber := c.nodeNumber
	sessionStartTime := c.sessionStartTime
	outputMode := c.outputMode

	slog.Debug("running SELECTMSGAREA", "node", nodeNumber)

	if currentUser == nil {
		msg := "\r\n|01Error: You must be logged in to select a message area.|07\r\n"
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
		time.Sleep(1 * time.Second)
		return nil, "", nil
	}

	filterConfID := currentUser.CurrentMsgConferenceID

	// Display initial area list
	displayedAreas, err := displayMessageAreaListFiltered(e, s, terminal, currentUser, outputMode, nodeNumber, sessionStartTime, filterConfID)
	if err != nil {
		return currentUser, "", err
	}

	// Build accessible conference list for [/] navigation
	var accessibleConfs []*conference.Conference
	if e.ConferenceMgr != nil {
		for _, conf := range e.ConferenceMgr.ListConferences() {
			if checkACS(conf.ACS, currentUser, s, terminal, sessionStartTime) {
				accessibleConfs = append(accessibleConfs, conf)
			}
		}
	}

	// Prompt for area #/tag
	prompt := e.LoadedStrings.ChangeBoardStr
	if prompt == "" {
		prompt = "|03Select Area |05[|13#|05/|13Tag|08, |13?|05=|13List|08, |13[|05=|13Prev |13]|05=|13Next|08, |13Q|05=|13Quit|05] : |11"
	}
	renderedPrompt := ansi.ReplacePipeCodes([]byte(prompt))
	curUpClear := "\x1b[A\r\x1b[2K" // move up one line, then clear it

	// Show initial prompt
	terminalio.WriteProcessedBytes(terminal, []byte("\r\n"), outputMode)
	terminalio.WriteProcessedBytes(terminal, renderedPrompt, outputMode)

	for {
		inputTag, err := readLineFromSessionIH(s, terminal)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil, "LOGOFF", io.EOF
			}
			return currentUser, "", err
		}

		inputClean := strings.TrimSpace(inputTag)
		upperInput := strings.ToUpper(inputClean)

		if upperInput == "Q" {
			return currentUser, "", nil
		}
		if upperInput == "" {
			terminalio.WriteProcessedBytes(terminal, renderedPrompt, outputMode)
			continue
		}

		// [ / ] = previous / next conference
		if (inputClean == "[" || inputClean == "]") && len(accessibleConfs) > 0 {
			// Find current conference index
			curIdx := -1
			for i, c := range accessibleConfs {
				if c.ID == filterConfID {
					curIdx = i
					break
				}
			}

			var newIdx int
			if inputClean == "]" {
				if curIdx < 0 || curIdx >= len(accessibleConfs)-1 {
					newIdx = 0 // wrap to first
				} else {
					newIdx = curIdx + 1
				}
			} else {
				if curIdx <= 0 {
					newIdx = len(accessibleConfs) - 1 // wrap to last
				} else {
					newIdx = curIdx - 1
				}
			}

			filterConfID = accessibleConfs[newIdx].ID
			displayedAreas, err = displayMessageAreaListFiltered(e, s, terminal, currentUser, outputMode, nodeNumber, sessionStartTime, filterConfID)
			if err != nil {
				return currentUser, "", err
			}
			terminalio.WriteProcessedBytes(terminal, []byte("\r\n"), outputMode)
			terminalio.WriteProcessedBytes(terminal, renderedPrompt, outputMode)
			continue
		}

		// ? = redisplay list and reprompt
		if upperInput == "?" {
			displayedAreas, err = displayMessageAreaListFiltered(e, s, terminal, currentUser, outputMode, nodeNumber, sessionStartTime, filterConfID)
			if err != nil {
				return currentUser, "", err
			}
			terminalio.WriteProcessedBytes(terminal, []byte("\r\n"), outputMode)
			terminalio.WriteProcessedBytes(terminal, renderedPrompt, outputMode)
			continue
		}

		// Try parsing as list number (1-based) first, then fallback to Tag
		var area *message.MessageArea
		var exists bool
		var errMsg string

		if num, parseErr := strconv.Atoi(inputClean); parseErr == nil {
			if num >= 1 && num <= len(displayedAreas) {
				area = displayedAreas[num-1]
				exists = true
			}
			if !exists {
				errMsg = fmt.Sprintf("|01Area #%d not found.|07", num)
			}
		} else {
			area, exists = e.MessageMgr.GetAreaByTag(upperInput)
			if !exists {
				errMsg = fmt.Sprintf("|01Area '%s' not found.|07", upperInput)
			}
		}

		if !exists {
			// Move up to overwrite prompt+input line, show error, then restore prompt
			terminalio.WriteProcessedBytes(terminal, []byte(curUpClear), outputMode)
			terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(errMsg)), outputMode)
			time.Sleep(1 * time.Second)
			terminalio.WriteProcessedBytes(terminal, []byte("\r\x1b[2K"), outputMode)
			terminalio.WriteProcessedBytes(terminal, renderedPrompt, outputMode)
			continue
		}

		// Check read ACS
		if !checkACS(area.ACSRead, currentUser, s, terminal, sessionStartTime) {
			terminalio.WriteProcessedBytes(terminal, []byte(curUpClear), outputMode)
			terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(fmt.Sprintf("|01Access denied to '%s'.|07", area.Tag))), outputMode)
			time.Sleep(1 * time.Second)
			terminalio.WriteProcessedBytes(terminal, []byte("\r\x1b[2K"), outputMode)
			terminalio.WriteProcessedBytes(terminal, renderedPrompt, outputMode)
			continue
		}

		// Update user state
		currentUser.CurrentMessageAreaID = area.ID
		currentUser.CurrentMessageAreaTag = area.Tag
		e.setUserMsgConference(currentUser, area.ConferenceID)

		if err := userManager.UpdateUser(currentUser); err != nil {
			slog.Error("failed to save user data after updating message area", "node", nodeNumber, "error", err)
			msg := "\r\n|01Error: Could not save area selection.|07\r\n"
			terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
			time.Sleep(1 * time.Second)
			terminalio.WriteProcessedBytes(terminal, renderedPrompt, outputMode)
			continue
		}

		slog.Info("user changed message area", "node", nodeNumber, "handle", currentUser.Handle, "id", area.ID, "tag", area.Tag)
		msg := fmt.Sprintf("\r\n|07Current message area set to: |15%s|07\r\n", area.Name)
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
		time.Sleep(1 * time.Second)

		return currentUser, "", nil
	}
}

// runSendPrivateMail handles sending private mail to another user.
// It validates the recipient exists and sets the MSG_PRIVATE flag.
func runSendPrivateMail(c *cmdCtx, args string) (*user.User, string, error) {
	e := c.e
	s := c.s
	terminal := c.terminal
	userManager := c.userManager
	currentUser := c.currentUser
	nodeNumber := c.nodeNumber
	outputMode := c.outputMode
	termWidth := c.termWidth
	termHeight := c.termHeight

	slog.Debug("running SENDPRIVMAIL", "node", nodeNumber)

	if currentUser == nil {
		msg := "\r\n|01Error: You must be logged in to send private mail.|07\r\n"
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

	// Prompt for recipient username
	terminalio.WriteProcessedBytes(terminal, []byte("\r\n"), outputMode)
	recipientPrompt := "|07Send private mail to: |15"
	var recipient string
	for {
		wErr := terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(recipientPrompt)), outputMode)
		if wErr != nil {
			slog.Warn("failed to write recipient prompt", "node", nodeNumber, "error", wErr)
		}
		var inputErr error
		recipient, inputErr = styledInput(terminal, s, outputMode, 24, "")
		if inputErr != nil {
			if errors.Is(inputErr, io.EOF) {
				slog.Info("user disconnected during recipient input", "node", nodeNumber)
				return nil, "LOGOFF", io.EOF
			}
			if errors.Is(inputErr, errInputAborted) {
				abort, confirmErr := e.confirmAbortPost(s, terminal, outputMode, nodeNumber, termWidth, termHeight)
				if confirmErr != nil {
					if errors.Is(confirmErr, io.EOF) {
						return nil, "LOGOFF", io.EOF
					}
					return nil, "", nil
				}
				if abort {
					return nil, "", nil
				}
				continue // No — re-show prompt and retry
			}
			slog.Error("failed reading recipient input", "node", nodeNumber, "error", inputErr)
			terminalio.WriteProcessedBytes(terminal, []byte("\r\nError reading recipient.\r\n"), outputMode)
			time.Sleep(1 * time.Second)
			return nil, "", nil
		}
		break
	}
	recipient = strings.TrimSpace(recipient)
	if recipient == "" {
		msg := "\r\n|01Recipient cannot be empty.|07\r\n"
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
		time.Sleep(1 * time.Second)
		return nil, "", nil
	}

	// Validate recipient user exists
	recipientUser, found := userManager.GetUser(recipient)
	if !found || recipientUser == nil {
		msg := fmt.Sprintf("\r\n|01Error: User '%s' not found.|07\r\n", recipient)
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
		time.Sleep(1 * time.Second)
		return nil, "", nil
	}

	// Prompt for subject
	titlePrompt := "|07Subject: |15"
	var subject string
	for {
		if wErr := terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(titlePrompt)), outputMode); wErr != nil {
			slog.Warn("failed to write subject prompt", "node", nodeNumber, "error", wErr)
		}
		var inputErr error
		subject, inputErr = styledInput(terminal, s, outputMode, 30, "")
		if inputErr != nil {
			if errors.Is(inputErr, io.EOF) {
				slog.Info("user disconnected during subject input", "node", nodeNumber)
				return nil, "LOGOFF", io.EOF
			}
			if errors.Is(inputErr, errInputAborted) {
				abort, confirmErr := e.confirmAbortPost(s, terminal, outputMode, nodeNumber, termWidth, termHeight)
				if confirmErr != nil {
					if errors.Is(confirmErr, io.EOF) {
						return nil, "LOGOFF", io.EOF
					}
					return nil, "", nil
				}
				if abort {
					return nil, "", nil
				}
				continue // No — re-show prompt and retry
			}
			slog.Error("failed reading subject input", "node", nodeNumber, "error", inputErr)
			terminalio.WriteProcessedBytes(terminal, []byte("\r\nError reading subject.\r\n"), outputMode)
			time.Sleep(1 * time.Second)
			return nil, "", nil
		}
		subject = strings.TrimSpace(subject)
		if subject != "" {
			break
		}
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte("|01Subject is required.|07\r\n")), outputMode)
	}

	// Launch editor
	slog.Debug("clearing screen before calling editor for private mail", "node", nodeNumber)
	terminalio.WriteProcessedBytes(terminal, []byte(ansi.ClearScreen()), outputMode)

	// Launch editor for private mail (no anonymous option for private mail)
	privNextMsg := 0
	if msgCount, mcErr := e.MessageMgr.GetMessageCountForArea(privmailArea.ID); mcErr == nil {
		privNextMsg = msgCount + 1
	}
	privEditorCtx := editor.EditorContext{
		NodeNumber: nodeNumber,
		NextMsgNum: privNextMsg,
		ConfArea:   "Private Mail",
	}
	body, saved, err := editor.RunEditorWithMetadata("", s, s, outputMode, subject, recipientUser.Handle, currentUser.Handle, false, "", "", "", "", false, nil, nil, privEditorCtx)
	slog.Debug("editor returned", "node", nodeNumber, "error", err, "saved", saved, "length", len(body))

	if err != nil {
		slog.Error("editor failed", "node", nodeNumber, "handle", currentUser.Handle, "error", err)
		return nil, "", fmt.Errorf("editor error: %w", err)
	}

	// Clear screen after editor exits
	terminalio.WriteProcessedBytes(terminal, []byte(ansi.ClearScreen()), outputMode)

	if !saved {
		slog.Info("user aborted private mail composition", "node", nodeNumber, "handle", currentUser.Handle)
		terminalio.WriteProcessedBytes(terminal, []byte("\r\nMessage aborted.\r\n"), outputMode)
		time.Sleep(1 * time.Second)
		return nil, "", nil
	}

	if strings.TrimSpace(body) == "" {
		slog.Info("user saved empty private mail", "node", nodeNumber, "handle", currentUser.Handle)
		terminalio.WriteProcessedBytes(terminal, []byte("\r\nMessage body empty. Aborting.\r\n"), outputMode)
		time.Sleep(1 * time.Second)
		return nil, "", nil
	}

	// Append auto-signature if user has one
	if currentUser.AutoSignature != "" {
		body = body + "\n\n" + currentUser.AutoSignature
	}

	// Save the private message with MSG_PRIVATE flag
	msgNum, err := e.MessageMgr.AddPrivateMessage(privmailArea.ID, currentUser.Handle, recipientUser.Handle, subject, body, "")
	if err != nil {
		slog.Error("failed to save private message", "node", nodeNumber, "handle", currentUser.Handle, "recipient", recipientUser.Handle, "error", err)
		errorMsg := ansi.ReplacePipeCodes([]byte("\r\n|01Error saving private message!|07\r\n"))
		terminalio.WriteProcessedBytes(terminal, errorMsg, outputMode)
		time.Sleep(2 * time.Second)
		return nil, "", fmt.Errorf("failed saving private message: %w", err)
	}

	// Update user message counter
	currentUser.MessagesPosted++
	if err := userManager.UpdateUser(currentUser); err != nil {
		slog.Error("failed to update MessagesPosted", "node", nodeNumber, "handle", currentUser.Handle, "error", err)
	}

	// Confirmation
	slog.Info("user sent private message", "node", nodeNumber, "handle", currentUser.Handle, "num", msgNum, "recipient", recipientUser.Handle)
	confirmMsg := ansi.ReplacePipeCodes([]byte(fmt.Sprintf("\r\n|02Private message sent to %s!|07\r\n", recipientUser.Handle)))
	terminalio.WriteProcessedBytes(terminal, confirmMsg, outputMode)
	time.Sleep(1 * time.Second)

	return nil, "", nil
}

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
	defer base.Close()

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
