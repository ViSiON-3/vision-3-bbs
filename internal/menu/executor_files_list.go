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

	"github.com/gliderlabs/ssh"
	"github.com/google/uuid"
	"golang.org/x/term"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/file"
	"github.com/ViSiON-3/vision-3-bbs/internal/terminalio"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
	"github.com/ViSiON-3/vision-3-bbs/internal/ziplab"
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
		fconfpath:            e.resolveFileConferencePath(currentUser),
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
				st.filesOnPage, err = st.e.FileMgr.GetFilesForAreaPaginated(st.currentAreaID, st.currentPage, st.filesPerPage)
				if err != nil {
					// Log error and potentially return or break the loop
					slog.Error("failed to get files for page", "node", st.nodeNumber, "page", st.currentPage, "error", err)
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
				st.filesOnPage, err = st.e.FileMgr.GetFilesForAreaPaginated(st.currentAreaID, st.currentPage, st.filesPerPage)
				if err != nil {
					// Log error and potentially return or break the loop
					slog.Error("failed to get files for page", "node", st.nodeNumber, "page", st.currentPage, "error", err)
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
			slog.Debug("user initiated download command", "node", st.nodeNumber, "handle", st.currentUser.Handle, "area", st.currentAreaID)

			// 1. Check if any files are marked
			if len(st.currentUser.TaggedFileIDs) == 0 {
				msg := "\r\n|07No files marked for download. Use |15#|07 to mark files.|07\r\n"
				terminalio.WriteProcessedBytes(st.terminal, ansi.ReplacePipeCodes([]byte(msg)), st.outputMode)
				time.Sleep(1 * time.Second)
				continue // Go back to file list display
			}

			// 2. Confirm download
			confirmPrompt := fmt.Sprintf("Download %d marked file(s)?", len(st.currentUser.TaggedFileIDs))
			// Use WriteProcessedBytes for SaveCursor, positioning, and clear line
			// Need to position this prompt carefully, perhaps near the bottom prompt line.
			// For now, just display it after the main prompt. TODO: Improve positioning.
			terminalio.WriteProcessedBytes(st.terminal, []byte(ansi.SaveCursor()), st.outputMode)
			terminalio.WriteProcessedBytes(st.terminal, []byte("\r\n\x1b[K"), st.outputMode) // Newline, clear line

			proceed, err := st.e.PromptYesNo(st.s, st.terminal, confirmPrompt, st.outputMode, st.nodeNumber, st.termWidth, st.termHeight, false)
			terminalio.WriteProcessedBytes(st.terminal, []byte(ansi.RestoreCursor()), st.outputMode) // Restore cursor after prompt

			if err != nil {
				if errors.Is(err, io.EOF) {
					slog.Info("user disconnected during download confirmation", "node", st.nodeNumber)
					return nil, "LOGOFF", io.EOF
				}
				slog.Error("error getting download confirmation", "node", st.nodeNumber, "error", err)
				msg := "\r\n|01Error during confirmation.|07\r\n"
				terminalio.WriteProcessedBytes(st.terminal, ansi.ReplacePipeCodes([]byte(msg)), st.outputMode)
				time.Sleep(1 * time.Second)
				continue // Back to file list
			}

			if !proceed {
				slog.Debug("user cancelled download", "node", st.nodeNumber)
				terminalio.WriteProcessedBytes(st.terminal, ansi.ReplacePipeCodes([]byte("\r\n|07Download cancelled.|07")), st.outputMode)
				time.Sleep(500 * time.Millisecond)
				continue // Back to file list
			}

			// 3. Protocol selection
			proto, protoOK, protoErr := st.e.selectTransferProtocol(st.s, st.terminal, st.outputMode)
			if protoErr != nil {
				if errors.Is(protoErr, io.EOF) {
					return nil, "LOGOFF", protoErr
				}
				slog.Error("protocol selection error", "node", st.nodeNumber, "error", protoErr)
				terminalio.WriteProcessedBytes(st.terminal, ansi.ReplacePipeCodes([]byte("\r\n|01Error: No transfer protocols configured on this system.|07\r\n")), st.outputMode)
				time.Sleep(2 * time.Second)
				continue
			}
			if !protoOK {
				terminalio.WriteProcessedBytes(st.terminal, ansi.ReplacePipeCodes([]byte("\r\n|07Download cancelled.|07\r\n")), st.outputMode)
				time.Sleep(500 * time.Millisecond)
				continue
			}

			// 4. Resolve tagged files to paths; pre-count lookup failures.
			type dlEntry struct {
				id   uuid.UUID
				path string
				name string
			}
			var resolved []dlEntry
			var successCount, failCount int
			for _, fileID := range st.currentUser.TaggedFileIDs {
				filePath, pathErr := st.e.FileMgr.GetFilePath(fileID)
				if pathErr != nil {
					slog.Error("failed to get path for file", "node", st.nodeNumber, "fileID", fileID, "error", pathErr)
					failCount++
					continue
				}
				if _, statErr := os.Stat(filePath); statErr != nil {
					slog.Error("file not on disk", "node", st.nodeNumber, "path", filePath, "fileID", fileID, "error", statErr)
					failCount++
					continue
				}
				resolved = append(resolved, dlEntry{id: fileID, path: filePath, name: filepath.Base(filePath)})
			}

			if len(resolved) == 0 {
				slog.Warn("no valid file paths found for tagged files", "node", st.nodeNumber)
				terminalio.WriteProcessedBytes(st.terminal, ansi.ReplacePipeCodes([]byte("\r\n|01Could not find any of the marked files on the server.|07\r\n")), st.outputMode)
				failCount = len(st.currentUser.TaggedFileIDs)
			} else {
				paths := make([]string, len(resolved))
				fileIDs := make([]uuid.UUID, len(resolved))
				for i, fe := range resolved {
					paths[i] = fe.path
					fileIDs[i] = fe.id
				}
				transferSuccess, transferFail := st.e.runTransferSend(st.s, st.terminal, proto, paths, fileIDs, st.outputMode, st.nodeNumber)
				successCount += transferSuccess
				failCount += transferFail
				time.Sleep(1 * time.Second)
			}

			// 4. Clear tags, update download count, and save user state
			slog.Debug("clearing tagged file IDs", "node", st.nodeNumber, "count", len(st.currentUser.TaggedFileIDs), "handle", st.currentUser.Handle)
			st.currentUser.TaggedFileIDs = nil // Clear the list
			st.currentUser.NumDownloads += successCount
			if err := st.userManager.UpdateUser(st.currentUser); err != nil {
				slog.Error("failed to save user data after download attempt", "node", st.nodeNumber, "error", err)
				// Inform user? State might be inconsistent.
				terminalio.WriteProcessedBytes(st.terminal, ansi.ReplacePipeCodes([]byte("\r\n|01Error saving user state after download.|07")), st.outputMode)
			}

			// 5. Final status message
			statusMsg := fmt.Sprintf("|07Download attempt finished. Success: %d, Failed: %d.|07\r\n", successCount, failCount)
			terminalio.WriteProcessedBytes(st.terminal, ansi.ReplacePipeCodes([]byte(statusMsg)), st.outputMode)
			time.Sleep(2 * time.Second)

			// Go back to the file list (will redraw with cleared marks)
			continue
		case "U": // Upload Files
			slog.Debug("upload command entered", "node", st.nodeNumber, "area", st.currentAreaID, "tag", st.currentAreaTag)
			uploadErr := st.e.runUploadFiles(st.s, st.terminal, st.currentUser, st.userManager, st.currentAreaID, st.currentAreaTag, st.outputMode, st.nodeNumber, st.sessionStartTime)
			if uploadErr != nil {
				if errors.Is(uploadErr, io.EOF) {
					return nil, "LOGOFF", uploadErr
				}
				slog.Error("upload failed", "node", st.nodeNumber, "error", uploadErr)
			}
			// Reload user to get updated NumUploads
			if reloaded, exists := st.userManager.GetUser(st.currentUser.Handle); exists {
				st.currentUser = reloaded
			}
			// Refresh file count and page data
			st.totalFiles, _ = st.e.FileMgr.GetFileCountForArea(st.currentAreaID)
			if st.filesPerPage > 0 {
				st.totalPages = (st.totalFiles + st.filesPerPage - 1) / st.filesPerPage
			}
			if st.totalPages == 0 {
				st.totalPages = 1
			}
			if st.currentPage > st.totalPages {
				st.currentPage = st.totalPages
			}
			st.filesOnPage, _ = st.e.FileMgr.GetFilesForAreaPaginated(st.currentAreaID, st.currentPage, st.filesPerPage)
			continue
		case "V": // View file
			slog.Debug("view command entered in file list", "node", st.nodeNumber)
			viewPrompt := "\r\n|07Enter file # to view (or |15ENTER|07 to cancel): |15"
			terminalio.WriteProcessedBytes(st.terminal, ansi.ReplacePipeCodes([]byte(viewPrompt)), st.outputMode)
			viewInput, viewErr := readLineFromSessionIH(st.s, st.terminal)
			if viewErr != nil {
				if errors.Is(viewErr, io.EOF) {
					return nil, "LOGOFF", io.EOF
				}
				continue
			}
			viewNum := strings.TrimSpace(viewInput)
			if viewNum == "" {
				continue
			}
			fileNumToView, parseErr := strconv.Atoi(viewNum)
			if parseErr != nil || fileNumToView <= 0 {
				terminalio.WriteProcessedBytes(st.terminal, ansi.ReplacePipeCodes([]byte("\r\n|01Invalid file number.|07\r\n")), st.outputMode)
				time.Sleep(500 * time.Millisecond)
				continue
			}
			viewIndex := fileNumToView - 1 - (st.currentPage-1)*st.filesPerPage
			if viewIndex < 0 || viewIndex >= len(st.filesOnPage) {
				terminalio.WriteProcessedBytes(st.terminal, ansi.ReplacePipeCodes([]byte("\r\n|01File number not on current page.|07\r\n")), st.outputMode)
				time.Sleep(500 * time.Millisecond)
				continue
			}
			fileToView := st.filesOnPage[viewIndex]
			if st.e.FileMgr.IsSupportedArchive(fileToView.Filename) {
				viewFilePath, pathErr := st.e.FileMgr.GetFilePath(fileToView.ID)
				if pathErr != nil {
					slog.Error("failed to get path for file", "node", st.nodeNumber, "fileID", fileToView.ID, "error", pathErr)
					terminalio.WriteProcessedBytes(st.terminal, ansi.ReplacePipeCodes([]byte("\r\n|01Error locating file.|07\r\n")), st.outputMode)
					time.Sleep(1 * time.Second)
				} else {
					ctx, cancel := st.e.transferContext(st.s.Context())
					ziplab.RunZipLabView(ctx, st.s, st.terminal, viewFilePath, fileToView.Filename, st.outputMode, sessionReadLine(st.s, st.terminal), sessionReadKey(st.s))
					cancel()
				}
			} else {
				viewFileByRecord(st.e, st.s, st.terminal, &fileToView, st.outputMode, st.termWidth, st.termHeight)
			}
			continue
		case "A": // Area Change (Placeholder/Not implemented here, handled by menu?)
			slog.Debug("area change command entered (handled by menu)", "node", st.nodeNumber)
			msg := "\r\n|01Use menu options to change area.|07\r\n"
			terminalio.WriteProcessedBytes(st.terminal, ansi.ReplacePipeCodes([]byte(msg)), st.outputMode)
			time.Sleep(1 * time.Second)
		default: // Includes 'T' (Tagging) and potential numeric input
			// Try to parse as a number for tagging
			fileNumToTag, err := strconv.Atoi(upperInput)
			if err == nil && fileNumToTag > 0 {
				// Valid number entered, attempt to tag/untag
				fileIndex := fileNumToTag - 1 - (st.currentPage-1)*st.filesPerPage
				if fileIndex >= 0 && fileIndex < len(st.filesOnPage) {
					fileToToggle := st.filesOnPage[fileIndex]
					found := false
					newTaggedIDs := []uuid.UUID{}
					if st.currentUser.TaggedFileIDs != nil {
						for _, taggedID := range st.currentUser.TaggedFileIDs {
							if taggedID == fileToToggle.ID {
								found = true // Mark as found to skip adding it back
							} else {
								newTaggedIDs = append(newTaggedIDs, taggedID)
							}
						}
					}
					if !found {
						// File was not tagged, so add it
						newTaggedIDs = append(newTaggedIDs, fileToToggle.ID)
						slog.Debug("user tagged file", "node", st.nodeNumber, "handle", st.currentUser.Handle, "fileNum", fileNumToTag, "fileID", fileToToggle.ID)
					} else {
						// File was tagged, so we removed it (untagged)
						slog.Debug("user untagged file", "node", st.nodeNumber, "handle", st.currentUser.Handle, "file_num", fileNumToTag, "id", fileToToggle.ID)
					}
					st.currentUser.TaggedFileIDs = newTaggedIDs
					// No page change needed, loop will redraw with updated marks
				} else {
					// Invalid file number for current page
					slog.Debug("invalid file number entered", "node", st.nodeNumber, "file_num", fileNumToTag)
					// Optional: Add user feedback message
				}
			} else {
				// Input was not N, P, Q, D, U, V, A, or a valid number - Invalid command
				slog.Debug("invalid command entered in LISTFILES", "node", st.nodeNumber, "input", upperInput)
				// Optional: Add user feedback message
			}
		} // end switch
	} // end for loop

	// Should not be reached normally
	// return nil, "", nil
}
