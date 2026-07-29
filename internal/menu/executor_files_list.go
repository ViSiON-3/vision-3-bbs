package menu

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/gliderlabs/ssh"
	"golang.org/x/term"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/file"
	"github.com/ViSiON-3/vision-3-bbs/internal/terminalio"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// fileColumnEnabled returns whether a column should be shown in the classic file listing.
// When extended is true, all columns are shown. When all user toggles are false (zero value),
// all columns are shown (backwards compatible default).
func fileColumnEnabled(u *user.User, col string, extended bool) bool {
	if extended {
		return true
	}
	c := u.FileListColumns
	allDefault := !c.Name && !c.Size && !c.Date && !c.Downloads && !c.Uploader && !c.Description
	if allDefault {
		return true
	}
	switch col {
	case "name":
		return c.Name
	case "size":
		return c.Size
	case "date":
		return c.Date
	case "downloads":
		return c.Downloads
	case "uploader":
		return c.Uploader
	case "description":
		return c.Description
	}
	return true
}

// fileListState bundles the per-invocation state of the file listing UI,
// shared between the classic pager and the lightbar pager.
type fileListState struct {
	e                *MenuExecutor
	s                ssh.Session
	terminal         *term.Terminal
	userManager      *user.UserMgr
	currentUser      *user.User
	nodeNumber       int
	sessionStartTime time.Time
	outputMode       ansi.OutputMode
	termWidth        int
	termHeight       int

	currentAreaID  int
	currentAreaTag string
	area           *file.FileArea

	topTemplateBytes     []byte
	botTemplateBytes     []byte
	processedMidTemplate string
	processedBotTemplate []byte
	fconfpath            string

	filesPerPage int
	totalFiles   int
	totalPages   int
	currentPage  int
	filesOnPage  []file.FileRecord

	cmdBarOptions []LightbarOption
	hiBarOptions  []LightbarOption
	extendedMode  bool
}

// runListFilesExtended displays a file listing with all columns visible regardless of user config.
func runListFilesExtended(c *cmdCtx, args string) (*user.User, string, error) {
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

	return runListFiles(&cmdCtx{e: e, s: s, terminal: terminal, userManager: userManager, currentUser: currentUser, nodeNumber: nodeNumber, sessionStartTime: sessionStartTime, outputMode: outputMode, termWidth: termWidth, termHeight: termHeight}, "EXTENDED")
}

// runListFiles displays a paginated list of files in the current file area.
func runListFiles(c *cmdCtx, args string) (*user.User, string, error) {
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

	extendedMode := false
	for _, tok := range strings.Fields(args) {
		if strings.EqualFold(tok, "EXTENDED") {
			extendedMode = true
			break
		}
	}
	slog.Debug("running LISTFILES", "node", nodeNumber, "extended", extendedMode)

	// 1. Check User and Current File Area
	if currentUser == nil {
		slog.Warn("LISTFILES called without logged in user", "node", nodeNumber)
		msg := "\r\n|01Error: You must be logged in to list files.|07\r\n"
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
		time.Sleep(1 * time.Second)
		return nil, "", nil // Return to menu
	}

	// Get current file area from user session
	currentAreaID := currentUser.CurrentFileAreaID
	currentAreaTag := currentUser.CurrentFileAreaTag

	if currentAreaID <= 0 {
		slog.Warn("user has no current file area selected", "node", nodeNumber, "handle", currentUser.Handle)
		msg := "\r\n|01Error: No file area selected.|07\r\n"
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
		time.Sleep(1 * time.Second)
		return nil, "", nil // Return to menu
	}

	slog.Info("user listing files", "node", nodeNumber, "handle", currentUser.Handle, "area", currentAreaID, "tag", currentAreaTag)

	// Check Read ACS for the file area
	area, exists := e.FileMgr.GetAreaByID(currentAreaID)
	if !exists {
		slog.Warn("current file area not found (stale/invalid area id)", "node", nodeNumber, "handle", currentUser.Handle, "area", currentAreaID, "tag", currentAreaTag)
		return nil, "", nil // Return to menu
	}
	if !checkACS(area.ACSList, currentUser, s, terminal, sessionStartTime) {
		slog.Warn("user denied read access to file area", "node", nodeNumber, "handle", currentUser.Handle, "area", currentAreaID, "tag", currentAreaTag, "acs", area.ACSList)
		// Display error message
		return nil, "", nil // Return to menu
	}

	// 2. Load Templates (FILELIST.TOP, FILELIST.MID, FILELIST.BOT)
	topTemplateBytes, processedMidTemplate, botTemplateBytes, err := e.loadFileListTemplates(currentUser, nodeNumber, terminal, outputMode)
	if err != nil {
		return nil, "", err
	}
	processedBotTemplate := ansi.ReplacePipeCodes(botTemplateBytes)

	// 3. Fetch Files and Pagination Logic
	// --- Determine lines available per page ---
	if termWidth <= 0 || termHeight <= 0 {
		ptyReq, _, isPty := s.Pty()
		if isPty {
			if termWidth <= 0 && ptyReq.Window.Width > 0 {
				termWidth = ptyReq.Window.Width
			}
			if termHeight <= 0 && ptyReq.Window.Height > 0 {
				termHeight = ptyReq.Window.Height
			}
		}
	}
	if termWidth <= 0 {
		termWidth = 80
	}
	if termHeight <= 0 {
		termHeight = 24
	}

	filesPerPage := computeFilePagination(termWidth, termHeight, topTemplateBytes, botTemplateBytes)
	slog.Debug("file list pagination", "node", nodeNumber, "termHeight", termHeight, "filesPerPage", filesPerPage)

	// --- Get Total File Count ---
	totalFiles, err := e.FileMgr.GetFileCountForArea(currentAreaID)
	if err != nil {
		slog.Error("failed to get file count for area", "node", nodeNumber, "area", currentAreaID, "error", err)
		msg := fmt.Sprintf("\r\n|01Error retrieving file list for area '%s'.|07\r\n", currentAreaTag)
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
		time.Sleep(1 * time.Second)
		return nil, "", fmt.Errorf("failed getting file count: %w", err)
	}

	totalPages := 0
	if totalFiles > 0 {
		totalPages = (totalFiles + filesPerPage - 1) / filesPerPage
	}
	if totalPages == 0 { // Ensure at least one page even if no files
		totalPages = 1
	}

	currentPage := 1                  // Start on page 1
	var filesOnPage []file.FileRecord // Use actual type from file package

	// --- Fetch Initial Page ---
	if totalFiles > 0 {
		filesOnPage, err = e.FileMgr.GetFilesForAreaPaginated(currentAreaID, currentPage, filesPerPage)
		if err != nil {
			slog.Error("failed to get files for area page", "node", nodeNumber, "area", currentAreaID, "page", currentPage, "error", err)
			msg := fmt.Sprintf("\r\n|01Error retrieving file list page for area '%s'.|07\r\n", currentAreaTag)
			terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
			time.Sleep(1 * time.Second)
			return nil, "", fmt.Errorf("failed getting file page: %w", err)
		}
	} else {
		filesOnPage = []file.FileRecord{} // Ensure empty slice if no files
	}

	// Load optional BAR files for file listing lightbar.
	cmdBarOptions, cmdBarErr := loadBarFile("FILELISTCMD", e)
	if cmdBarErr != nil {
		slog.Warn("failed to load FILELISTCMD.BAR", "node", nodeNumber, "error", cmdBarErr)
	}
	hiBarOptions, hiBarErr := loadBarFile("FILELISTHI", e)
	if hiBarErr != nil {
		slog.Warn("failed to load FILELISTHI.BAR", "node", nodeNumber, "error", hiBarErr)
	}

	st := &fileListState{
		e:                    e,
		s:                    s,
		terminal:             terminal,
		userManager:          userManager,
		currentUser:          currentUser,
		nodeNumber:           nodeNumber,
		sessionStartTime:     sessionStartTime,
		outputMode:           outputMode,
		termWidth:            termWidth,
		termHeight:           termHeight,
		currentAreaID:        currentAreaID,
		currentAreaTag:       currentAreaTag,
		area:                 area,
		topTemplateBytes:     topTemplateBytes,
		botTemplateBytes:     botTemplateBytes,
		processedMidTemplate: processedMidTemplate,
		processedBotTemplate: processedBotTemplate,
		filesPerPage:         filesPerPage,
		totalFiles:           totalFiles,
		totalPages:           totalPages,
		currentPage:          currentPage,
		filesOnPage:          filesOnPage,
		cmdBarOptions:        cmdBarOptions,
		hiBarOptions:         hiBarOptions,
		extendedMode:         extendedMode,
	}

	// 4. Dispatch based on file listing mode (user pref overrides server default)
	fileListMode := st.currentUser.FileListingMode
	if fileListMode == "" {
		fileListMode = st.e.ServerCfg.FileListingMode
	}
	if !strings.EqualFold(fileListMode, "classic") {
		return runListFilesLightbar(st)
	}

	// Classic display loop
	st.fconfpath = st.e.resolveFileConferencePath(st.currentUser)
	for {
		if err := st.renderFileListPage(); err != nil {
			slog.Error("failed rendering file list page", "node", st.nodeNumber, "error", err)
		}

		// 4.6 Read User Input
		input, err := readLineFromSessionIH(st.s, st.terminal)
		if err != nil {
			if errors.Is(err, io.EOF) {
				slog.Info("user disconnected during LISTFILES", "node", st.nodeNumber)
				return nil, "LOGOFF", io.EOF
			}
			slog.Error("failed reading LISTFILES input", "node", st.nodeNumber, "error", err)
			// Consider retry or exit
			return nil, "", err
		}

		upperInput := strings.ToUpper(strings.TrimSpace(input))

		// 4.7 Process Input
		switch upperInput {
		case "N", " ", "": // Next Page (Space/Enter default to Next)
			if st.currentPage < st.totalPages {
				st.currentPage++
				// Fetch files for the new page
				if fetchErr := st.fetchPage(); fetchErr != nil {
					// Display error message to user?
					time.Sleep(1 * time.Second)
				}
			} else {
				// Indicate last page (optional feedback)
				terminalio.WriteProcessedBytes(st.terminal, ansi.ReplacePipeCodes([]byte("\r\n|07Already on last page.|07")), st.outputMode)
				time.Sleep(500 * time.Millisecond)
			}
			continue // Redraw loop
		case "P": // Previous Page
			if st.currentPage > 1 {
				st.currentPage--
				// Fetch files for the new page
				if fetchErr := st.fetchPage(); fetchErr != nil {
					// Display error message to user?
					time.Sleep(1 * time.Second)
				}
			} else {
				// Indicate first page (optional feedback)
				terminalio.WriteProcessedBytes(st.terminal, ansi.ReplacePipeCodes([]byte("\r\n|07Already on first page.|07")), st.outputMode)
				time.Sleep(500 * time.Millisecond)
			}
			continue // Redraw loop
		case "Q": // Quit
			slog.Debug("user quit LISTFILES", "node", st.nodeNumber)
			return nil, "", nil // Return to FILEM menu
		case "D": // Download marked files
			logoff, dlErr := st.handleFileDownload()
			if logoff {
				return nil, "LOGOFF", dlErr
			}
			continue
		case "U": // Upload Files
			logoff, upErr := st.handleFileUpload()
			if logoff {
				return nil, "LOGOFF", upErr
			}
			continue
		case "V": // View file
			logoff, vErr := st.handleFileView()
			if logoff {
				return nil, "LOGOFF", vErr
			}
			continue
		case "A": // Area Change (Placeholder/Not implemented here, handled by menu?)
			slog.Debug("area change command entered (handled by menu)", "node", st.nodeNumber)
			msg := "\r\n|01Use menu options to change area.|07\r\n"
			terminalio.WriteProcessedBytes(st.terminal, ansi.ReplacePipeCodes([]byte(msg)), st.outputMode)
			time.Sleep(1 * time.Second)
		default: // Includes 'T' (Tagging) and potential numeric input
			st.toggleFileTag(upperInput)
		} // end switch
	} // end for loop

	// Should not be reached normally
	// return nil, "", nil
}
