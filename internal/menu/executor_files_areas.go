package menu

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/file"
	"github.com/ViSiON-3/vision-3-bbs/internal/terminalio"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// runSelectFileAreaDispatch checks the user/server fileListingMode setting and
// dispatches to either the lightbar or classic text-mode file area selector.
func runSelectFileAreaDispatch(c *cmdCtx, args string) (*user.User, string, error) {
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

	mode := ""
	if currentUser != nil {
		mode = currentUser.FileListingMode
	}
	if mode == "" {
		mode = e.ServerCfg.FileListingMode
	}
	if strings.EqualFold(mode, "classic") {
		return runSelectFileArea(&cmdCtx{e: e, s: s, terminal: terminal, userManager: userManager, currentUser: currentUser, nodeNumber: nodeNumber, sessionStartTime: sessionStartTime, outputMode: outputMode, termWidth: termWidth, termHeight: termHeight}, args)
	}
	return runSelectFileAreaLightbar(&cmdCtx{e: e, s: s, terminal: terminal, userManager: userManager, currentUser: currentUser, nodeNumber: nodeNumber, sessionStartTime: sessionStartTime, outputMode: outputMode, termWidth: termWidth, termHeight: termHeight}, args)
}

// runSelectFileArea prompts the user for a file area tag and changes the current user's
// active file area if valid and accessible (classic text-mode).
func runSelectFileArea(c *cmdCtx, args string) (*user.User, string, error) {
	e := c.e
	s := c.s
	terminal := c.terminal
	userManager := c.userManager
	currentUser := c.currentUser
	nodeNumber := c.nodeNumber
	sessionStartTime := c.sessionStartTime
	outputMode := c.outputMode

	slog.Debug("running SELECTFILEAREA", "node", nodeNumber)

	if currentUser == nil {
		slog.Warn("SELECTFILEAREA called without logged in user", "node", nodeNumber)
		msg := "\r\n|01Error: You must be logged in to select a file area.|07\r\n"
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
		time.Sleep(1 * time.Second)
		return nil, "", nil
	}

	// --- Display the list first --- <--- MODIFIED
	if err := displayFileAreaList(e, s, terminal, currentUser, outputMode, nodeNumber, sessionStartTime); err != nil {
		slog.Error("failed displaying file area list in SELECTFILEAREA", "node", nodeNumber, "error", err)
		// Don't proceed if the list couldn't be displayed
		return currentUser, "", err // Return error
	}
	// Add a newline between list and prompt
	terminalio.WriteProcessedBytes(terminal, []byte("\r\n"), outputMode)

	// Prompt for area tag
	prompt := e.LoadedStrings.ChangeFileAreaStr
	if prompt == "" {
		prompt = "|07File Area Tag (?=List, Q=Quit): |15"
	}
	renderedPrompt := ansi.ReplacePipeCodes([]byte(prompt))
	curUpClear := "\x1b[A\r\x1b[2K"

	// Show initial prompt
	terminalio.WriteProcessedBytes(terminal, renderedPrompt, outputMode)

	for {
		inputTag, err := readLineFromSessionIH(s, terminal)
		if err != nil {
			if errors.Is(err, io.EOF) {
				slog.Info("user disconnected during SELECTFILEAREA prompt", "node", nodeNumber)
				return nil, "LOGOFF", io.EOF
			}
			slog.Error("error reading input for SELECTFILEAREA", "node", nodeNumber, "error", err)
			return currentUser, "", err
		}

		inputClean := strings.TrimSpace(inputTag)
		upperInput := strings.ToUpper(inputClean)

		if upperInput == "Q" {
			slog.Debug("SELECTFILEAREA aborted by user", "node", nodeNumber)
			terminalio.WriteProcessedBytes(terminal, []byte("\r\n"), outputMode)
			return currentUser, "", nil
		}
		if upperInput == "" {
			terminalio.WriteProcessedBytes(terminal, renderedPrompt, outputMode)
			continue
		}

		if upperInput == "?" {
			slog.Debug("user requested file area list again from SELECTFILEAREA", "node", nodeNumber)
			if listErr := displayFileAreaList(e, s, terminal, currentUser, outputMode, nodeNumber, sessionStartTime); listErr != nil {
				slog.Error("failed redisplaying file area list", "node", nodeNumber, "error", listErr)
			}
			terminalio.WriteProcessedBytes(terminal, []byte("\r\n"), outputMode)
			terminalio.WriteProcessedBytes(terminal, renderedPrompt, outputMode)
			continue
		}

		// Try parsing as ID first, then fallback to Tag
		var area *file.FileArea
		var exists bool
		matched := false

		if inputID, parseErr := strconv.Atoi(inputClean); parseErr == nil {
			slog.Debug("user input parsed as file area ID", "node", nodeNumber, "input", inputClean, "id", inputID)
			area, exists = e.FileMgr.GetAreaByID(inputID)
			if exists {
				matched = true
			}
		}
		if !matched {
			slog.Debug("user input not an ID, looking up by tag", "node", nodeNumber, "input", inputClean, "tag", upperInput)
			area, exists = e.FileMgr.GetAreaByTag(upperInput)
			if exists {
				matched = true
			}
		}

		if !matched {
			terminalio.WriteProcessedBytes(terminal, []byte(curUpClear), outputMode)
			msg := fmt.Sprintf("|01Invalid file area '%s'!|07", inputClean)
			terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
			time.Sleep(1 * time.Second)
			terminalio.WriteProcessedBytes(terminal, []byte("\r\x1b[2K"), outputMode)
			terminalio.WriteProcessedBytes(terminal, renderedPrompt, outputMode)
			continue
		}

		// Check ACSList permission
		if !checkACS(area.ACSList, currentUser, s, terminal, sessionStartTime) {
			slog.Warn("user denied access to file area due to ACS", "node", nodeNumber, "handle", currentUser.Handle, "area", area.Tag, "acs", area.ACSList)
			terminalio.WriteProcessedBytes(terminal, []byte(curUpClear), outputMode)
			msg := fmt.Sprintf("|01Access denied to file area '%s'!|07", area.Tag)
			terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
			time.Sleep(1 * time.Second)
			terminalio.WriteProcessedBytes(terminal, []byte("\r\x1b[2K"), outputMode)
			terminalio.WriteProcessedBytes(terminal, renderedPrompt, outputMode)
			continue
		}

		// Found a valid, accessible area — update user state
		currentUser.CurrentFileAreaID = area.ID
		currentUser.CurrentFileAreaTag = area.Tag
		e.setUserFileConference(currentUser, area.ConferenceID)

		// Save the user state
		if saveErr := userManager.UpdateUser(currentUser); saveErr != nil {
			slog.Error("failed to save user data after updating file area", "node", nodeNumber, "error", saveErr)
			currentUser.CurrentFileAreaID = area.ID // revert not needed, just don't show success
			msg := "\r\n|01Error: Could not save area selection.|07\r\n"
			terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
			time.Sleep(1 * time.Second)
			terminalio.WriteProcessedBytes(terminal, renderedPrompt, outputMode)
			continue
		}

		slog.Info("user changed file area", "node", nodeNumber, "handle", currentUser.Handle, "area", area.Tag, "id", area.ID)
		msg := fmt.Sprintf("\r\n|07Current file area set to: |15%s|07\r\n", area.Name)
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
		time.Sleep(1 * time.Second)

		return currentUser, "", nil
	}
}
