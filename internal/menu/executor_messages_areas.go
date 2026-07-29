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
	"github.com/ViSiON-3/vision-3-bbs/internal/conference"
	"github.com/ViSiON-3/vision-3-bbs/internal/message"
	"github.com/ViSiON-3/vision-3-bbs/internal/terminalio"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
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
