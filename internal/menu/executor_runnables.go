package menu

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/terminalio"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
	"github.com/ViSiON-3/vision-3-bbs/internal/version"
)

// registerPlaceholderRunnables adds dummy functions for testing
func registerPlaceholderRunnables(registry map[string]RunnableFunc) { // Use local RunnableFunc
	// Keep READMAIL as a placeholder for now
	registry["READMAIL"] = func(c *cmdCtx, args string) (*user.User, string, error) {
		e := c.e
		terminal := c.terminal
		currentUser := c.currentUser
		nodeNumber := c.nodeNumber
		outputMode := c.outputMode

		if currentUser == nil {
			slog.Warn("readmail called without logged in user", "node", nodeNumber)
			msg := e.LoadedStrings.ExecReadmailLogin
			wErr := terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
			if wErr != nil {
				slog.Error("failed writing readmail error message", "error", wErr)
			}
			time.Sleep(1 * time.Second)
			return nil, "", nil // No user change, no next action, no error
		}
		msg := fmt.Sprintf(e.LoadedStrings.ExecReadmailPlaceholder, currentUser.Handle)
		wErr := terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
		if wErr != nil {
			slog.Error("failed writing readmail placeholder message", "error", wErr)
		}
		time.Sleep(500 * time.Millisecond)
		return nil, "", nil // No user change, no next action, no error
	}

	// Register DOOR handler — delegates to door_handler.go
	registry["DOOR:"] = func(c *cmdCtx, doorName string) (*user.User, string, error) {
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

		if currentUser == nil {
			slog.Warn("door called without logged in user", "node", nodeNumber, "door", doorName)
			msg := e.LoadedStrings.ExecDoorLogin
			wErr := terminalio.WriteProcessedBytes(s.Stderr(), ansi.ReplacePipeCodes([]byte(msg)), outputMode)
			if wErr != nil {
				slog.Error("failed writing door error message (not logged in)", "error", wErr)
			}
			return nil, "", nil
		}
		slog.Info("user attempting to run door", "node", nodeNumber, "handle", currentUser.Handle, "door", doorName)

		// Look up door configuration
		doorConfig, exists := e.GetDoorConfig(strings.ToUpper(doorName))
		if !exists {
			slog.Warn("door configuration not found", "door", doorName)
			errMsg := fmt.Sprintf(e.LoadedStrings.ExecDoorNotConfigured, doorName)
			wErr := terminalio.WriteProcessedBytes(s.Stderr(), ansi.ReplacePipeCodes([]byte(errMsg)), outputMode)
			if wErr != nil {
				slog.Error("failed writing door error message (not configured) to stderr", "error", wErr)
			}
			return nil, "", nil
		}

		// Check per-door access level
		if doorConfig.MinAccessLevel > 0 && currentUser.AccessLevel < doorConfig.MinAccessLevel {
			slog.Warn("user denied access to door", "node", nodeNumber, "handle", currentUser.Handle, "level", currentUser.AccessLevel, "door", doorName, "requires", doorConfig.MinAccessLevel)
			errFmt := e.LoadedStrings.DoorAccessDenied
			if strings.TrimSpace(errFmt) == "" {
				errFmt = "\r\n|14Access denied to door: |11%s|07\r\n"
			}
			errMsg := fmt.Sprintf(errFmt, doorName)
			wErr := terminalio.WriteProcessedBytes(s.Stderr(), ansi.ReplacePipeCodes([]byte(errMsg)), outputMode)
			if wErr != nil {
				slog.Error("failed writing door access denied message", "error", wErr)
			}
			time.Sleep(1 * time.Second)
			return nil, "", nil
		}

		// Build door context and execute
		// Use passed termWidth/termHeight (from user preferences) instead of reading from User struct
		ctx := buildDoorCtx(e, s, terminal,
			currentUser.ID, currentUser.Handle, currentUser.RealName,
			currentUser.AccessLevel, currentUser.TimeLimit, currentUser.TimesCalled,
			currentUser.GroupLocation,
			termWidth, termHeight,
			nodeNumber, sessionStartTime, outputMode,
			doorConfig, doorName)
		ctx.UserManager = userManager
		ctx.CurrentUser = currentUser

		// Doors read directly from ssh.Session; reset shared InputHandler first
		// so it does not race and steal door/menu keystrokes.
		resetSessionIH(s)
		cmdErr := executeDoor(ctx)
		_ = getSessionIH(s)

		if cmdErr != nil {
			if errors.Is(cmdErr, ErrDoorBusy) {
				slog.Info("door is busy", "node", nodeNumber, "door", doorName, "handle", currentUser.Handle)
				busyFmt := e.LoadedStrings.DoorBusyFormat
				if strings.TrimSpace(busyFmt) == "" {
					busyFmt = "\r\n|14Door is currently in use: |11%s|07\r\n"
				}
				busyMsg := fmt.Sprintf(busyFmt, doorName)
				terminalio.WriteProcessedBytes(s.Stderr(), ansi.ReplacePipeCodes([]byte(busyMsg)), outputMode)
				time.Sleep(1 * time.Second)
			} else {
				slog.Error("door execution failed", "node", nodeNumber, "handle", currentUser.Handle, "door", doorName, "error", cmdErr)
				doorErrorMessage(ctx, fmt.Sprintf("Error running external program '%s': %v", doorName, cmdErr))
				time.Sleep(2 * time.Second)
			}
		} else {
			slog.Info("door completed", "node", nodeNumber, "handle", currentUser.Handle, "door", doorName)
		}

		return nil, "", nil
	}
}

// registerAppRunnables registers the actual application command functions.
func registerAppRunnables(registry map[string]RunnableFunc) { // Use local RunnableFunc
	registry["PLACEHOLDER"] = runPlaceholderCommand // Canonical handler for undefined/not-yet-implemented options
	registry["MAINLOGOFF"] = runMainLogoffCommand   // MAIN menu logoff with confirmation + GOODBYE.ANS
	registry["IMMEDIATELOGOFF"] = runImmediateLogoffCommand
	registry["SHOWSTATS"] = runShowStats
	registry["SYSTEMSTATS"] = runSystemStats
	registry["LASTCALLERS"] = runLastCallers
	registry["AUTHENTICATE"] = runAuthenticate
	registry["ONELINER"] = runOneliners                              // Register new placeholder
	registry["FULL_LOGIN_SEQUENCE"] = runFullLoginSequence           // Register the new sequence
	registry["SHOWVERSION"] = runShowVersion                         // Register the version display runnable
	registry["LISTUSERS"] = runListUsers                             // Register the user list runnable
	registry["PENDINGVALIDATIONNOTICE"] = runPendingValidationNotice // SysOp notice for new users awaiting validation
	registry["VALIDATEUSER"] = runValidateUser                       // Validate user accounts from admin menu
	registry["NEWUSERVAL"] = runNewUserValidation                    // Prompt to validate new users if any pending
	registry["UNVALIDATEUSER"] = runUnvalidateUser                   // Remove validation from user accounts
	registry["BANUSER"] = runBanUser                                 // Quick-ban user accounts
	registry["DELETEUSER"] = runDeleteUser                           // Soft-delete user accounts (data preserved)
	registry["PURGEUSERS"] = runPurgeUsers                           // Permanently purge soft-deleted users past retention period
	registry["ADMINLISTUSERS"] = runAdminListUsers                   // Admin detailed user browser
	registry["TOGGLEALLOWNEWUSERS"] = runAdminToggleAllowNewUsers    // Toggle allowNewUsers config flag
	registry["LISTMSGAR"] = runListMessageAreas                      // <-- ADDED: Register message area list runnable
	registry["COMPOSEMSG"] = runComposeMessage                       // <-- ADDED: Register compose message runnable
	registry["PROMPTANDCOMPOSEMESSAGE"] = runPromptAndComposeMessage // <-- ADDED: Register prompt/compose runnable (Corrected key to uppercase)
	registry["READMSGS"] = runReadMsgs                               // <-- ADDED: Register message reading runnable
	registry["NEWSCAN"] = runNewscan                                 // <-- ADDED: Register newscan runnable
	registry["LISTFILES"] = runListFiles                             // <-- ADDED: Register file list runnable
	registry["VIEW_FILE"] = runViewFile                              // View file (archives show listing, text gets paged)
	registry["TYPE_TEXT_FILE"] = runTypeTextFile                     // Type text file with paging
	registry["LISTFILEAR"] = runListFileAreas                        // <-- ADDED: Register file area list runnable
	registry["SELECTFILEAREA"] = runSelectFileAreaDispatch           // File area selection (dispatches by fileListingMode)
	registry["SELECTMSGAREA"] = runSelectMessageAreaLightbar         // Register message area selection runnable (lightbar)
	registry["CHANGEMSGCONF"] = runChangeMsgConferenceLightbar       // Change message conference (lightbar)
	registry["NEXTMSGAREA"] = runNextMsgArea                         // Navigate to next message area
	registry["PREVMSGAREA"] = runPrevMsgArea                         // Navigate to previous message area
	registry["NEXTMSGCONF"] = runNextMsgConf                         // Navigate to next message conference
	registry["PREVMSGCONF"] = runPrevMsgConf                         // Navigate to previous message conference
	registry["NEWUSER"] = runNewUser                                 // Register new user application runnable
	registry["GETHEADERTYPE"] = runGetHeaderType                     // Message header style selection
	registry["LISTMSGS"] = runListMsgs                               // List messages in current area
	registry["SENDPRIVMAIL"] = runSendPrivateMail                    // Send private mail to user
	registry["READPRIVMAIL"] = runReadPrivateMail                    // Read private mail
	registry["LISTPRIVMAIL"] = runListPrivateMail                    // List private mail
	registry["NEWSCANCONFIG"] = runNewscanConfig                     // Configure newscan tagged areas
	registry["UPDATENEWSCAN"] = runUpdateNewscanPointers             // Update newscan pointers to a specific date
	registry["NMAILSCAN"] = runNewMailScan                           // New mail scan
	registry["DISPLAYFILE"] = runLoginDisplayFile                    // Display ANSI file
	registry["RUNDOOR"] = runLoginDoor                               // Run external script/door
	registry["FASTLOGIN"] = runFastLogin                             // Inline fast login menu
	registry["LISTDOORS"] = runListDoors                             // List available doors
	registry["OPENDOOR"] = runOpenDoor                               // Prompt and open a door
	registry["DOORINFO"] = runDoorInfo                               // Show door information
	registry["UPLOADFILE"] = runUploadFile                           // ZMODEM file upload
	registry["DOWNLOADFILE"] = runDownloadFile                       // V2-style download: prompt, add to batch, transfer
	registry["BATCHDOWNLOAD"] = runBatchDownload                     // Download tagged batch files
	registry["CLEAR_BATCH"] = runClearBatch                          // Clear tagged file batch queue
	registry["SEARCH_FILES"] = runSearchFiles                        // Search files across all areas
	registry["SHOWFILEINFO"] = runShowFileInfo                       // Show file metadata
	registry["FILE_NEWSCAN"] = runFileNewscan                        // Scan file areas for new uploads
	registry["EDITFILERECORD"] = runEditFileRecord                   // Sysop file review queue
	registry["WANTLIST"] = runWantList                               // File want list
	registry["FILENEWSCANCONFIG"] = runFileNewscanConfig             // File newscan area config
	registry["CFG_FILECOLUMNS"] = runCfgFileColumns                  // Configure file listing columns
	registry["LISTFILES_EXTENDED"] = runListFilesExtended            // Extended file listing (all columns)
	registry["QWKDOWNLOAD"] = runQWKDownload                         // QWK mail packet download
	registry["QWKUPLOAD"] = runQWKUpload                             // QWK REP packet upload
	registry["WHOISONLINE"] = runWhoIsOnline                         // Who's online display
	registry["CFG_HOTKEYS"] = runCfgHotKeys
	registry["CFG_MOREPROMPTS"] = runCfgMorePrompts
	registry["CFG_SCREENWIDTH"] = runCfgScreenWidth
	registry["CFG_SCREENHEIGHT"] = runCfgScreenHeight
	registry["CFG_TERMTYPE"] = runCfgTermType
	registry["CFG_REALNAME"] = runCfgRealName
	registry["CFG_NOTE"] = runCfgNote
	registry["CFG_CUSTOMPROMPT"] = runCfgCustomPrompt
	registry["CFG_COLOR"] = runCfgColor
	registry["CFG_PASSWORD"] = runCfgPassword
	registry["CFG_FILELISTMODE"] = runCfgFileListMode
	registry["CFG_AUTOSIG"] = runCfgAutoSig
	registry["CFG_VIEWCONFIG"] = runCfgViewConfig
	registry["CHAT"] = runChat
	registry["PAGE"] = runPage
	registry["SPONSORMENU"] = runSponsorMenu                 // Sponsor menu (% key in Messages Menu)
	registry["SPONSOREDITAREA"] = runSponsorEditArea         // Edit current message area fields
	registry["PRINTNEWS"] = runPrintNews                     // Display news new since last login (login sequence)
	registry["LISTNEWS"] = runListNews                       // List/read all news items
	registry["EDITNEWS"] = runEditNews                       // SysOp: news management (Add/Delete/Edit/List/View)
	registry["VOTE"] = runVote                               // Voting booths system
	registry["VOTEMANDATORY"] = runVoteOnMandatory           // Mandatory voting check (login sequence)
	registry["LISTNUV"] = runNUVList                         // List NUV candidates and vote tallies
	registry["SCANNUV"] = runNUVScan                         // Vote on pending NUV candidates
	registry["BBSLIST"] = runBBSList                         // List BBS directory entries
	registry["BBSLISTADD"] = runBBSListAdd                   // Add new BBS listing
	registry["BBSLISTEDIT"] = runBBSListEdit                 // Edit BBS listing (owner or sysop)
	registry["BBSLISTDELETE"] = runBBSListDelete             // Delete BBS listing (owner or sysop)
	registry["BBSLISTVERIFY"] = runBBSListVerify             // SysOp: toggle verified flag
	registry["RUMORSLIST"] = runRumorsList                   // List all rumors
	registry["RUMORSADD"] = runRumorsAdd                     // Add a new rumor
	registry["RUMORSDELETE"] = runRumorsDelete               // Delete a rumor
	registry["RUMORSSEARCH"] = runRumorsSearch               // Search rumors
	registry["RUMORSNEWSCAN"] = runRumorsNewscan             // Rumors newscan (since last login)
	registry["RANDOMRUMOR"] = runRandomRumor                 // Display random rumor (login sequence)
	registry["INFOFORMS"] = runInfoForms                     // InfoForms menu (list/fill/view forms)
	registry["INFOFORMVIEW"] = runInfoFormView               // View own completed infoform
	registry["INFOFORMHUNT"] = runInfoFormHunt               // SysOp: browse all users' completed forms
	registry["INFOFORMREQUIRED"] = runInfoFormRequired       // Login sequence: force required forms
	registry["INFOFORMNUKE"] = runInfoFormNuke               // SysOp: delete all forms for a user
	registry["V3NETSTATUS"] = runV3NetStatus                 // V3Net networking status display
	registry["V3NETAREAS"] = runV3NetAreas                   // V3Net area subscriptions
	registry["V3NETPROPOSE"] = runV3NetPropose               // V3Net propose new area
	registry["V3NETACCESSREQUESTS"] = runV3NetAccessRequests // V3Net area access requests (manager)
	registry["V3NETCOORDINATOR"] = runV3NetCoordinator       // V3Net coordinator panel
	registry["V3NETREGISTRY"] = runV3NetRegistry             // V3Net network registry browser
}

func runPlaceholderCommand(c *cmdCtx, args string) (*user.User, string, error) {
	e := c.e
	terminal := c.terminal
	currentUser := c.currentUser
	nodeNumber := c.nodeNumber
	outputMode := c.outputMode

	e.showUndefinedMenuInput(terminal, outputMode, nodeNumber)
	return currentUser, "", nil
}

func runMainLogoffCommand(c *cmdCtx, args string) (*user.User, string, error) {
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

	prompt := e.LoadedStrings.LogOffStr
	if prompt == "" {
		prompt = "\r\n|07Log off now? @"
	}

	confirm, err := e.PromptYesNo(s, terminal, prompt, outputMode, nodeNumber, termWidth, termHeight, false)
	if err != nil {
		if errors.Is(err, io.EOF) {
			return nil, "LOGOFF", io.EOF
		}
		return currentUser, "", err
	}

	if !confirm {
		return currentUser, "", nil
	}

	return runImmediateLogoffCommand(&cmdCtx{e: e, s: s, terminal: terminal, userManager: userManager, currentUser: currentUser, nodeNumber: nodeNumber, sessionStartTime: sessionStartTime, outputMode: outputMode, termWidth: termWidth, termHeight: termHeight}, args)
}

func runImmediateLogoffCommand(c *cmdCtx, args string) (*user.User, string, error) {
	e := c.e
	terminal := c.terminal
	currentUser := c.currentUser
	nodeNumber := c.nodeNumber
	outputMode := c.outputMode

	if displayErr := e.displayFile(terminal, "GOODBYE.ANS", outputMode); displayErr != nil {
		slog.Warn("failed to display GOODBYE.ANS before logoff", "node", nodeNumber, "error", displayErr)
		_ = terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(e.LoadedStrings.ExecGoodbye)), outputMode)
	}

	time.Sleep(1 * time.Second)
	return currentUser, "LOGOFF", nil
}

// runShowStats displays the user statistics screen (YOURSTAT.ANS).
func runShowStats(c *cmdCtx, args string) (*user.User, string, error) {
	e := c.e
	s := c.s
	terminal := c.terminal
	currentUser := c.currentUser
	nodeNumber := c.nodeNumber
	sessionStartTime := c.sessionStartTime
	outputMode := c.outputMode
	termWidth := c.termWidth
	termHeight := c.termHeight

	if currentUser == nil {
		slog.Warn("showstats called without logged in user", "node", nodeNumber)
		msg := e.LoadedStrings.ExecStatsLogin
		wErr := terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
		if wErr != nil {
			slog.Error("failed writing showstats error message", "error", wErr)
		}
		time.Sleep(1 * time.Second)
		return nil, "", nil // Updated return
	}

	ansFilename := "YOURSTAT.ANS"
	// Use MenuSetPath for ANSI file
	fullAnsPath := filepath.Join(e.MenuSetPath, "ansi", ansFilename)
	rawAnsiContent, readErr := ansi.GetAnsiFileContent(fullAnsPath)
	if readErr != nil {
		slog.Error("failed to read file for showstats", "node", nodeNumber, "path", fullAnsPath, "error", readErr)
		msg := fmt.Sprintf(e.LoadedStrings.ExecStatsError, ansFilename)
		wErr := terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
		if wErr != nil {
			slog.Error("failed writing showstats file read error message", "error", wErr)
		}
		time.Sleep(1 * time.Second)
		return nil, "", fmt.Errorf("failed to read %s: %w", ansFilename, readErr) // Updated return
	}

	placeholders := map[string]string{
		"|UH": currentUser.Handle,
		"|UN": currentUser.PrivateNote,
		"|UL": strconv.Itoa(currentUser.AccessLevel),
		"|FL": strconv.Itoa(currentUser.AccessLevel),
		"|UK": strconv.Itoa(currentUser.NumUploads),
		"|NU": strconv.Itoa(currentUser.NumUploads),
		"|DK": "0", "|ND": "0", "|TP": "0", "|NM": "0", "|LC": "N/A",
	}
	if currentUser.TimeLimit <= 0 {
		placeholders["|TL"] = "Unlimited"
	} else {
		elapsedSeconds := time.Since(sessionStartTime).Seconds()
		totalSeconds := float64(currentUser.TimeLimit * 60)
		remainingSeconds := totalSeconds - elapsedSeconds
		if remainingSeconds < 0 {
			remainingSeconds = 0
		}
		placeholders["|TL"] = strconv.Itoa(int(remainingSeconds / 60))
	}

	// Branch based on output mode to preserve encoding correctness
	slog.Debug("showstats output mode", "node", nodeNumber, "outputMode", outputMode)
	var statsDisplayBytes []byte
	if outputMode == ansi.OutputModeUTF8 {
		// UTF-8 mode: Convert CP437→UTF-8 first, then substitute placeholders
		utf8Content := string(ansi.CP437BytesToUTF8(rawAnsiContent))
		for key, val := range placeholders {
			utf8Content = strings.ReplaceAll(utf8Content, key, val)
		}
		statsDisplayBytes = []byte(utf8Content)
	} else {
		// CP437 mode: Substitute placeholders directly on raw bytes
		// (WriteProcessedBytes will pass them through unchanged)
		statsDisplayBytes = rawAnsiContent
		for key, val := range placeholders {
			statsDisplayBytes = bytes.ReplaceAll(statsDisplayBytes, []byte(key), []byte(val))
		}
	}

	// Use WriteProcessedBytes for ClearScreen
	wErr := terminalio.WriteProcessedBytes(terminal, []byte(ansi.ClearScreen()), outputMode)
	if wErr != nil {
		// Log error but continue if possible
		slog.Error("failed clearing screen for showstats", "node", nodeNumber, "error", wErr)
	}
	// For CP437 mode with raw ANSI content, write bytes directly to avoid UTF-8 decode artifacts
	if outputMode == ansi.OutputModeCP437 {
		_, wErr = terminal.Write(statsDisplayBytes)
	} else {
		wErr = terminalio.WriteProcessedBytes(terminal, statsDisplayBytes, outputMode)
	}
	if wErr != nil {
		slog.Error("failed writing processed YOURSTAT.ANS", "node", nodeNumber, "error", wErr)
		return nil, "", wErr // Updated return
	}

	// 5. Wait for Enter key press
	pausePrompt := e.LoadedStrings.PauseString // Use configured pause string
	if pausePrompt == "" {
		pausePrompt = "\r\n|07Press |15[ENTER]|07 to continue... " // Fallback
	}

	slog.Debug("displaying showstats pause prompt (centered)", "node", nodeNumber)
	err := writeCenteredPausePrompt(s, terminal, pausePrompt, outputMode, termWidth, termHeight)
	if err != nil {
		if errors.Is(err, io.EOF) {
			slog.Info("user disconnected during showstats pause", "node", nodeNumber)
			return nil, "LOGOFF", io.EOF
		}
		slog.Error("failed during showstats pause", "error", err)
		return nil, "", err
	}
	return nil, "", nil // Updated return (Success)
}

// runShowVersion displays configured version information.
func runShowVersion(c *cmdCtx, args string) (*user.User, string, error) {
	e := c.e
	s := c.s
	terminal := c.terminal
	nodeNumber := c.nodeNumber
	outputMode := c.outputMode
	termWidth := c.termWidth
	termHeight := c.termHeight

	slog.Debug("running showversion", "node", nodeNumber)

	versionTemplate := e.LoadedStrings.ExecVersionString
	versionString := versionTemplate
	if strings.Contains(versionTemplate, "%s") {
		versionString = fmt.Sprintf(versionTemplate, version.Number)
	}

	// Display the version
	terminalio.WriteProcessedBytes(terminal, []byte(ansi.ClearScreen()), outputMode) // Optional: Clear screen
	terminalio.WriteProcessedBytes(terminal, []byte("\r\n\r\n"), outputMode)         // Add some spacing
	wErr := terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(versionString)), outputMode)
	if wErr != nil {
		slog.Error("failed writing showversion output", "node", nodeNumber, "error", wErr)
		// Don't return error, just log it
	}

	// Wait for Enter
	pausePrompt := e.LoadedStrings.PauseString // Use configured pause string
	if pausePrompt == "" {
		slog.Warn("pausestring empty, no pause prompt will be shown for showversion", "node", nodeNumber)
		// Don't use a hardcoded fallback. If it's empty, it's empty.
	} else {
		terminalio.WriteProcessedBytes(terminal, []byte("\r\n"), outputMode) // Add newline before pause
		slog.Debug("displaying showversion pause prompt (centered)", "node", nodeNumber)
		err := writeCenteredPausePrompt(s, terminal, pausePrompt, outputMode, termWidth, termHeight)
		if err != nil {
			if errors.Is(err, io.EOF) {
				slog.Info("user disconnected during showversion pause", "node", nodeNumber)
				return nil, "LOGOFF", io.EOF
			}
			slog.Error("failed during showversion pause", "node", nodeNumber, "error", err)
			return nil, "", err
		}
	}

	return nil, "", nil // Return to the current menu
}
