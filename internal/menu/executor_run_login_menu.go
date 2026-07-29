package menu

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/terminalio"
)

// handleLoginMenu implements the LOGIN menu's special interactive handling:
// redirecting an already-authenticated user straight to MAIN, displaying the
// LOGIN screen and running the interactive login prompt, seeding default
// message/file areas for a freshly authenticated user, and resolving the
// LOGIN.CFG post-auth default action. The caller (Run) must only invoke this
// when st.currentMenuName == "LOGIN". It returns a loopAction telling Run
// whether to restart the loop (loopContinue) or leave Run (loopReturn) with
// retAction/retErr as the values Run should return (paired with
// st.currentUser, which this method mutates in place on successful auth).
func (st *runLoopState) handleLoginMenu(res ansi.ProcessAnsiResult) (act loopAction, retAction string, retErr error) {
	e := st.e
	s := st.s
	terminal := st.terminal
	outputMode := st.outputMode
	termWidth := st.termWidth
	termHeight := st.termHeight
	userManager := st.userManager
	nodeNumber := st.nodeNumber
	sessionStartTime := st.sessionStartTime
	ansiProcessResult := res

	if st.currentUser != nil {
		slog.Warn("attempting to run LOGIN menu for already authenticated user, skipping login, going to MAIN", "handle", st.currentUser.Handle)

		// Set default message area if not already set (e.g., SSH auto-login)
		if st.currentUser.CurrentMessageAreaID == 0 && e.MessageMgr != nil {
			for _, area := range e.MessageMgr.ListAreas() {
				if checkACS(area.ACSRead, st.currentUser, s, terminal, sessionStartTime) {
					st.currentUser.CurrentMessageAreaID = area.ID
					st.currentUser.CurrentMessageAreaTag = area.Tag
					e.setUserMsgConference(st.currentUser, area.ConferenceID)
					break
				}
			}
		}

		// Set default file area if not already set
		if st.currentUser.CurrentFileAreaID == 0 && e.FileMgr != nil {
			for _, area := range e.FileMgr.ListAreas() {
				if checkACS(area.ACSList, st.currentUser, s, terminal, sessionStartTime) {
					st.currentUser.CurrentFileAreaID = area.ID
					st.currentUser.CurrentFileAreaTag = area.Tag
					e.setUserFileConference(st.currentUser, area.ConferenceID)
					break
				}
			}
		}

		// Persist defaults
		if userManager != nil {
			if saveErr := userManager.UpdateUser(st.currentUser); saveErr != nil {
				slog.Error("failed to save user default area selections", "error", saveErr)
			}
		}

		st.currentMenuName = "MAIN"
		st.previousMenuName = "LOGIN" // Set previous explicitly here
		return loopContinue, "", nil
	}

	// Display the processed LOGIN screen, truncated to fit terminal height
	terminalio.WriteProcessedBytes(terminal, []byte(ansi.ClearScreen()), outputMode) // Clear first
	displayBytes := ansiProcessResult.DisplayBytes
	if termHeight > 0 {
		// Truncate ANSI output to terminal height to prevent scrolling
		// which would shift all Y coordinates
		lines := bytes.Split(displayBytes, []byte("\n"))
		if len(lines) > termHeight {
			displayBytes = bytes.Join(lines[:termHeight], []byte("\n"))
			slog.Debug("truncated LOGIN.ANS to fit terminal", "from", len(lines), "to", termHeight, "rows", termHeight)
		}
	}
	// For CP437 mode, write raw bytes directly to avoid UTF-8 false positives
	// (some CP437 byte pairs like 0xDF 0xB2 accidentally form valid UTF-8)
	// For UTF-8 mode, bytes are already converted to UTF-8, pass through
	var wErr error
	if outputMode == ansi.OutputModeCP437 {
		_, wErr = terminal.Write(displayBytes)
	} else {
		wErr = terminalio.WriteProcessedBytes(terminal, displayBytes, outputMode)
	}
	if wErr != nil {
		slog.Error("failed to write processed LOGIN.ANS bytes to terminal", "error", wErr)
		return loopReturn, "", fmt.Errorf("failed to display LOGIN.ANS: %w", wErr)
	}

	// Handle the interactive login prompt using extracted coordinates and colors
	authenticatedUserResult, loginErr := e.handleLoginPrompt(s, terminal, userManager, nodeNumber, ansiProcessResult.FieldCoords, ansiProcessResult.FieldColors, outputMode, termWidth, termHeight)

	// Process result of login attempt
	if loginErr != nil {
		if errors.Is(loginErr, io.EOF) {
			slog.Info("user disconnected during login prompt")
			return loopReturn, "LOGOFF", nil // Signal logoff
		}
		slog.Error("error during login prompt handling", "error", loginErr)
		return loopReturn, "", loginErr // Propagate critical error
	}

	if authenticatedUserResult != nil {
		slog.Info("login successful, proceeding based on LOGIN menu config", "handle", authenticatedUserResult.Handle)
		st.currentUser = authenticatedUserResult // Update the user for this Run context

		// --- Update user's terminal dimensions from detected size ---
		if termWidth > 0 && termHeight > 0 {
			st.currentUser.ScreenWidth = termWidth
			st.currentUser.ScreenHeight = termHeight
			slog.Info("updated user screen preferences", "handle", st.currentUser.Handle, "width", termWidth, "height", termHeight)
			if userManager != nil {
				if saveErr := userManager.UpdateUser(st.currentUser); saveErr != nil {
					slog.Error("failed to save user screen preferences", "error", saveErr)
				}
			}
		}

		// --- BEGIN Set Default Message Area (only if not already set from saved prefs) ---
		if st.currentUser.CurrentMessageAreaID == 0 && e.MessageMgr != nil {
			allAreas := e.MessageMgr.ListAreas() // Already sorted by Position
			slog.Debug("found message areas for user", "count", len(allAreas), "handle", st.currentUser.Handle)
			foundDefaultArea := false
			for _, area := range allAreas {
				// Check if user has read access to this area
				if checkACS(area.ACSRead, st.currentUser, s, terminal, sessionStartTime) {
					slog.Info("setting default message area for user", "handle", st.currentUser.Handle, "id", area.ID, "tag", area.Tag)
					st.currentUser.CurrentMessageAreaID = area.ID
					st.currentUser.CurrentMessageAreaTag = area.Tag
					e.setUserMsgConference(st.currentUser, area.ConferenceID)
					foundDefaultArea = true
					break // Found the first accessible area
				} else {
					slog.Debug("user denied read access to message area", "handle", st.currentUser.Handle, "id", area.ID, "tag", area.Tag, "acs", area.ACSRead)
				}
			}
			if !foundDefaultArea {
				slog.Warn("user has no access to any message areas", "handle", st.currentUser.Handle)
				st.currentUser.CurrentMessageAreaID = 0
				st.currentUser.CurrentMessageAreaTag = ""
			}
		} else if st.currentUser.CurrentMessageAreaID != 0 {
			slog.Info("user has saved message area", "handle", st.currentUser.Handle, "id", st.currentUser.CurrentMessageAreaID, "tag", st.currentUser.CurrentMessageAreaTag, "conferenceID", st.currentUser.CurrentMsgConferenceID, "conferenceTag", st.currentUser.CurrentMsgConferenceTag)
		}
		// --- END Set Default Message Area ---

		// --- BEGIN Set Default File Area (only if not already set from saved prefs) ---
		if st.currentUser.CurrentFileAreaID == 0 && e.FileMgr != nil {
			allFileAreas := e.FileMgr.ListAreas() // Assumes ListAreas is sorted by ID
			slog.Debug("found file areas for user", "count", len(allFileAreas), "handle", st.currentUser.Handle)
			foundDefaultFileArea := false
			for _, area := range allFileAreas {
				// Check if user has list access to this area
				if checkACS(area.ACSList, st.currentUser, s, terminal, sessionStartTime) { // Use ACSList
					slog.Info("setting default file area for user", "handle", st.currentUser.Handle, "id", area.ID, "tag", area.Tag)
					st.currentUser.CurrentFileAreaID = area.ID
					st.currentUser.CurrentFileAreaTag = area.Tag
					e.setUserFileConference(st.currentUser, area.ConferenceID)
					foundDefaultFileArea = true
					break // Found the first accessible area
				} else {
					slog.Debug("user denied list access to file area", "handle", st.currentUser.Handle, "id", area.ID, "tag", area.Tag, "acs", area.ACSList)
				}
			}
			if !foundDefaultFileArea {
				slog.Warn("user has no access to any file areas", "handle", st.currentUser.Handle)
				st.currentUser.CurrentFileAreaID = 0
				st.currentUser.CurrentFileAreaTag = ""
			}
		} else if st.currentUser.CurrentFileAreaID != 0 {
			slog.Info("user has saved file area", "handle", st.currentUser.Handle, "id", st.currentUser.CurrentFileAreaID, "tag", st.currentUser.CurrentFileAreaTag, "conferenceID", st.currentUser.CurrentFileConferenceID, "conferenceTag", st.currentUser.CurrentFileConferenceTag)
		}
		// --- END Set Default File Area ---

		// Persist default area/conference selections to disk
		if userManager != nil {
			if saveErr := userManager.UpdateUser(st.currentUser); saveErr != nil {
				slog.Error("failed to save user default area selections", "error", saveErr)
			}
		}

		// --- BEGIN POST-AUTHENTICATION TRANSITION ---
		// Load LOGIN.CFG to find the default action
		loginCfgPath := filepath.Join(e.MenuSetPath, "cfg") // Use correct path structure
		loginCommands, loadCmdErr := LoadCommands("LOGIN", loginCfgPath)
		if loadCmdErr != nil {
			slog.Error("failed to load LOGIN.CFG after successful authentication", "path", filepath.Join(loginCfgPath, "LOGIN.CFG"), "error", loadCmdErr)
			// Return an error? Or try to default to MAIN?
			return loopReturn, "LOGOFF", fmt.Errorf("failed loading LOGIN.CFG post-auth") // Logoff user on critical error
		}

		// Find the default command (Keys == "")
		nextAction := "" // Default action if not found?
		foundDefault := false
		for _, cmd := range loginCommands {
			if cmd.Keys == "" { // Check for empty string
				if cmd.Command == "RUN:AUTHENTICATE" {
					continue
				}
				if checkACS(cmd.ACS, st.currentUser, s, terminal, sessionStartTime) { // Use ssh.Session 's'
					nextAction = cmd.Command
					foundDefault = true
					slog.Debug("found default command in LOGIN.CFG after auth", "command", nextAction)
					break // Found the relevant default command (e.g., GOTO:MAIN)
				} else {
					slog.Warn("user denied default command in LOGIN.CFG", "handle", st.currentUser.Handle, "command", cmd.Command, "acs", cmd.ACS)
				}
			}
		}

		if !foundDefault {
			slog.Error("no accessible default command found in LOGIN.CFG, logging off", "handle", st.currentUser.Handle)
			return loopReturn, "LOGOFF", fmt.Errorf("no accessible default command found in LOGIN.CFG")
		}
		// -- Return the next action AND the authenticated user --
		return loopReturn, nextAction, nil
	} else { // authenticatedUserResult == nil
		slog.Info("login failed, redisplaying LOGIN menu")
		return loopContinue, "", nil // Restart loop for LOGIN
	}
}
