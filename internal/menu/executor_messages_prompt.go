package menu

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"strconv"
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

// promptComposeField performs one shared "compose field" input cycle: write the
// prompt, read via styledInput, and handle EOF/abort. On ESC (errInputAborted) it
// asks confirmAbortPost; a "No" answer re-shows the prompt and retries in place.
// It returns aborted=true if the user confirmed abandoning the post (caller should
// return to the menu), err=io.EOF if the session disconnected, or the raw error
// from styledInput for any other read failure (caller decides how to report it).
// Callers own any additional validation (e.g. required-field retry, default
// substitution) since that differs between the title and to prompts.
func (e *MenuExecutor) promptComposeField(s ssh.Session, terminal *term.Terminal, ih *editor.InputHandler, prompt string, maxLen int, defaultValue string, outputMode ansi.OutputMode, nodeNumber, termWidth, termHeight int) (value string, aborted bool, err error) {
	for {
		wErr := terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(prompt)), outputMode)
		if wErr != nil {
			slog.Warn("failed to write compose field prompt", "node", nodeNumber, "error", wErr)
		}
		value, err = styledInput(terminal, s, outputMode, maxLen, defaultValue)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return "", false, io.EOF
			}
			if errors.Is(err, errInputAborted) {
				abort, confirmErr := e.confirmAbortPost(s, terminal, outputMode, nodeNumber, termWidth, termHeight)
				if confirmErr != nil {
					if errors.Is(confirmErr, io.EOF) {
						return "", false, io.EOF
					}
					return "", true, nil
				}
				if abort {
					return "", true, nil
				}
				continue // No — re-show prompt and retry
			}
			return "", false, err
		}
		return value, false, nil
	}
}
