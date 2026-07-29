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
	ansiProcessResult := res

	if st.currentUser != nil {
		slog.Warn("attempting to run LOGIN menu for already authenticated user, skipping login, going to MAIN", "handle", st.currentUser.Handle)

		// Seed default areas if not already set (e.g., SSH auto-login). Shares
		// setDefaultArea with the post-authentication path so both log the
		// selection and clear a stale tag when no area is accessible.
		st.setDefaultArea("message")
		st.setDefaultArea("file")

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

		// --- BEGIN Set Default Message/File Area (only if not already set from saved prefs) ---
		st.setDefaultArea("message")
		st.setDefaultArea("file")
		// --- END Set Default Message/File Area ---

		// Persist default area/conference selections to disk
		if userManager != nil {
			if saveErr := userManager.UpdateUser(st.currentUser); saveErr != nil {
				slog.Error("failed to save user default area selections", "error", saveErr)
			}
		}

		// --- BEGIN POST-AUTHENTICATION TRANSITION ---
		nextAction, ok, resolveErr := st.resolvePostAuthAction()
		if !ok {
			return loopReturn, "LOGOFF", resolveErr
		}
		// -- Return the next action AND the authenticated user --
		return loopReturn, nextAction, nil
	} else { // authenticatedUserResult == nil
		slog.Info("login failed, redisplaying LOGIN menu")
		return loopContinue, "", nil // Restart loop for LOGIN
	}
}

// setDefaultArea seeds st.currentUser's current message or file area with
// the first ACS-accessible area when no saved default already exists,
// mirroring the classic BBS behavior of dropping a freshly authenticated
// user into the first area they're allowed into. kind selects "message" or
// "file"; it picks the area manager, the ACS field checked (ACSRead vs
// ACSList), the CurrentXAreaID/Tag fields updated, and the conference
// setter invoked. This replaces what were two near-identical inline blocks
// in handleLoginMenu, differing only in those manager/field selections.
func (st *runLoopState) setDefaultArea(kind string) {
	e := st.e
	s := st.s
	terminal := st.terminal
	sessionStartTime := st.sessionStartTime
	u := st.currentUser

	type areaOption struct {
		id           int
		tag          string
		conferenceID int
		acs          string
	}

	var (
		currentID  int
		currentTag string
		confID     int
		confTag    string
		mgrPresent bool
		options    []areaOption
		accessVerb string
	)

	switch kind {
	case "message":
		currentID = u.CurrentMessageAreaID
		currentTag = u.CurrentMessageAreaTag
		confID = u.CurrentMsgConferenceID
		confTag = u.CurrentMsgConferenceTag
		accessVerb = "read"
		if e.MessageMgr != nil {
			mgrPresent = true
			for _, area := range e.MessageMgr.ListAreas() { // Already sorted by Position
				options = append(options, areaOption{id: area.ID, tag: area.Tag, conferenceID: area.ConferenceID, acs: area.ACSRead})
			}
		}
	case "file":
		currentID = u.CurrentFileAreaID
		currentTag = u.CurrentFileAreaTag
		confID = u.CurrentFileConferenceID
		confTag = u.CurrentFileConferenceTag
		accessVerb = "list"
		if e.FileMgr != nil {
			mgrPresent = true
			for _, area := range e.FileMgr.ListAreas() { // Assumes ListAreas is sorted by ID
				options = append(options, areaOption{id: area.ID, tag: area.Tag, conferenceID: area.ConferenceID, acs: area.ACSList})
			}
		}
	}

	if currentID == 0 && mgrPresent {
		slog.Debug(fmt.Sprintf("found %s areas for user", kind), "count", len(options), "handle", u.Handle)
		foundDefault := false
		for _, opt := range options {
			// Check if user has read/list access to this area
			if checkACS(opt.acs, u, s, terminal, sessionStartTime) {
				slog.Info(fmt.Sprintf("setting default %s area for user", kind), "handle", u.Handle, "id", opt.id, "tag", opt.tag)
				switch kind {
				case "message":
					u.CurrentMessageAreaID = opt.id
					u.CurrentMessageAreaTag = opt.tag
					e.setUserMsgConference(u, opt.conferenceID)
				case "file":
					u.CurrentFileAreaID = opt.id
					u.CurrentFileAreaTag = opt.tag
					e.setUserFileConference(u, opt.conferenceID)
				}
				foundDefault = true
				break // Found the first accessible area
			} else {
				slog.Debug(fmt.Sprintf("user denied %s access to %s area", accessVerb, kind), "handle", u.Handle, "id", opt.id, "tag", opt.tag, "acs", opt.acs)
			}
		}
		if !foundDefault {
			slog.Warn(fmt.Sprintf("user has no access to any %s areas", kind), "handle", u.Handle)
			switch kind {
			case "message":
				u.CurrentMessageAreaID = 0
				u.CurrentMessageAreaTag = ""
			case "file":
				u.CurrentFileAreaID = 0
				u.CurrentFileAreaTag = ""
			}
		}
	} else if currentID != 0 {
		slog.Info(fmt.Sprintf("user has saved %s area", kind), "handle", u.Handle, "id", currentID, "tag", currentTag, "conferenceID", confID, "conferenceTag", confTag)
	}
}

// resolvePostAuthAction loads LOGIN.CFG and finds the first ACS-accessible
// default command (Keys == "") to run immediately after successful
// authentication. ok is false when LOGIN.CFG failed to load or no
// accessible default command was found; err then carries the reason
// handleLoginMenu should log off with, preserving the original distinct
// error text for each failure mode.
func (st *runLoopState) resolvePostAuthAction() (action string, ok bool, err error) {
	e := st.e
	s := st.s
	terminal := st.terminal
	sessionStartTime := st.sessionStartTime

	// Load LOGIN.CFG to find the default action
	loginCfgPath := filepath.Join(e.MenuSetPath, "cfg") // Use correct path structure
	loginCommands, loadCmdErr := LoadCommands("LOGIN", loginCfgPath)
	if loadCmdErr != nil {
		slog.Error("failed to load LOGIN.CFG after successful authentication", "path", filepath.Join(loginCfgPath, "LOGIN.CFG"), "error", loadCmdErr)
		// Return an error? Or try to default to MAIN?
		return "", false, fmt.Errorf("failed loading LOGIN.CFG post-auth") // Logoff user on critical error
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
		return "", false, fmt.Errorf("no accessible default command found in LOGIN.CFG")
	}
	return nextAction, true, nil
}
