package menu

import (
	"bytes"
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
	"github.com/ViSiON-3/vision-3-bbs/internal/file"
	"github.com/ViSiON-3/vision-3-bbs/internal/terminalio"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
	"github.com/ViSiON-3/vision-3-bbs/internal/ziplab"
	"github.com/google/uuid"
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
	topTemplatePath := filepath.Join(e.MenuSetPath, "templates", "FILELIST.TOP")
	midTemplatePath := filepath.Join(e.MenuSetPath, "templates", "FILELIST.MID")
	botTemplatePath := filepath.Join(e.MenuSetPath, "templates", "FILELIST.BOT")

	topTemplateBytes, errTop := readTemplateFile(topTemplatePath)
	midTemplateBytes, errMid := readTemplateFile(midTemplatePath)
	botTemplateBytes, errBot := readTemplateFile(botTemplatePath)
	if errBot != nil {
		if os.IsNotExist(errBot) {
			botTemplateBytes = nil
		} else {
			slog.Error("failed to load FILELIST.BOT template", "node", nodeNumber, "error", errBot)
			msg := "\r\n|01Error loading File List screen templates.|07\r\n"
			terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
			time.Sleep(1 * time.Second)
			return nil, "", fmt.Errorf("failed loading FILELIST templates")
		}
	}

	if errTop != nil || errMid != nil {
		slog.Error("failed to load FILELIST template files", "node", nodeNumber, "topError", errTop, "midError", errMid)
		msg := "\r\n|01Error loading File List screen templates.|07\r\n"
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
		time.Sleep(1 * time.Second)
		return nil, "", fmt.Errorf("failed loading FILELIST templates")
	}

	// Apply common pipe tokens (|CFAN, |UH, etc.) before colour-code processing.
	topTemplateBytes = e.applyCommonTemplateTokens(topTemplateBytes, currentUser, nodeNumber)
	midTemplateBytes = e.applyCommonTemplateTokens(midTemplateBytes, currentUser, nodeNumber)
	botTemplateBytes = e.applyCommonTemplateTokens(botTemplateBytes, currentUser, nodeNumber)

	processedTopTemplate := ansi.ReplacePipeCodes(topTemplateBytes)
	processedMidTemplate := string(ansi.ReplacePipeCodes(midTemplateBytes))
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

	// Estimate lines used by header, footer, prompt
	headerLines := bytes.Count(processedTopTemplate, []byte("\n")) + 1
	footerLines := bytes.Count(processedBotTemplate, []byte("\n")) + 1
	// TODO: Make prompt configurable and count its lines accurately
	promptLines := 2 // Estimate 2 lines for prompt + input line
	fixedLines := headerLines + footerLines + promptLines
	filesPerPage := termHeight - fixedLines
	if filesPerPage < 1 {
		filesPerPage = 1 // Ensure at least 1 file can be shown
	}
	slog.Debug("file list pagination", "node", nodeNumber, "termHeight", termHeight, "fixedLines", fixedLines, "filesPerPage", filesPerPage)

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

	// 4. Dispatch based on file listing mode (user pref overrides server default)
	fileListMode := currentUser.FileListingMode
	if fileListMode == "" {
		fileListMode = e.ServerCfg.FileListingMode
	}
	if !strings.EqualFold(fileListMode, "classic") {
		return runListFilesLightbar(e, s, terminal, userManager, currentUser, nodeNumber, sessionStartTime,
			currentAreaID, currentAreaTag, area,
			topTemplateBytes, processedMidTemplate, processedBotTemplate,
			filesPerPage, totalFiles, totalPages,
			cmdBarOptions, hiBarOptions, outputMode)
	}

	// Classic display loop
	fconfpath := e.resolveFileConferencePath(currentUser)
	for {
		// 4.1 Clear Screen
		writeErr := terminalio.WriteProcessedBytes(terminal, []byte(ansi.ClearScreen()), outputMode)
		if writeErr != nil {
			slog.Error("failed clearing screen for LISTFILES", "node", nodeNumber, "error", writeErr)
		}

		// 4.2 Display Top Template (process @FCONFPATH@, @FTOTAL@, @FPAGE@ placeholders per page)
		topRendered := ansi.ReplacePipeCodes(processFileListPlaceholders(topTemplateBytes, currentPage, totalPages, totalFiles, fconfpath))
		wErr := terminalio.WriteProcessedBytes(terminal, topRendered, outputMode)
		if wErr != nil {
			slog.Error("failed writing LISTFILES top template", "node", nodeNumber, "error", wErr)
		}
		wErr = terminalio.WriteProcessedBytes(terminal, []byte("\r\n"), outputMode)
		if wErr != nil {
			slog.Error("failed writing CRLF after LISTFILES top template", "node", nodeNumber, "error", wErr)
		}

		// 4.3 Display Files on Current Page (using MID template)
		if len(filesOnPage) == 0 {
			// Display "No files in this area" message
			// TODO: Use a configurable string?
			noFilesMsg := "\r\n|07   No files in this area.   \r\n"
			terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(noFilesMsg)), outputMode)
		} else {
			for i, fileRec := range filesOnPage {
				line := processedMidTemplate
				fileNumOnPage := (currentPage-1)*filesPerPage + i + 1

				fileNumStr := strconv.Itoa(fileNumOnPage)
				fileNameStr := ""
				if fileColumnEnabled(currentUser, "name", extendedMode) {
					fileNameStr = fileRec.Filename
					if len(fileNameStr) > 12 {
						fileNameStr = fileNameStr[:12]
					}
					fileNameStr = fmt.Sprintf("%-12s", fileNameStr)
				} else {
					fileNameStr = strings.Repeat(" ", 12)
				}
				dateStr := ""
				if fileColumnEnabled(currentUser, "date", extendedMode) {
					dateStr = fileRec.UploadedAt.Format("01/02/06")
				} else {
					dateStr = strings.Repeat(" ", 8)
				}
				sizeStr := ""
				if fileColumnEnabled(currentUser, "size", extendedMode) {
					sizeStr = fmt.Sprintf("%5s", fmt.Sprintf("%dk", fileRec.Size/1024))
				} else {
					sizeStr = strings.Repeat(" ", 5)
				}

				markStr := " "
				if currentUser.TaggedFileIDs != nil {
					for _, taggedID := range currentUser.TaggedFileIDs {
						if taggedID == fileRec.ID {
							markStr = "*"
							break
						}
					}
				}

				var dizLines []string
				firstDesc := ""
				if fileColumnEnabled(currentUser, "description", extendedMode) {
					dizLines = formatDIZLines(fileRec.Description, dizMaxWidth, dizMaxLines)
					if len(dizLines) > 0 {
						firstDesc = dizLines[0]
					}
				}

				line = strings.ReplaceAll(line, "^MARK", markStr)
				line = strings.ReplaceAll(line, "^NUM", fileNumStr)
				line = strings.ReplaceAll(line, "^NAME", fileNameStr)
				line = strings.ReplaceAll(line, "^DATE", dateStr)
				line = strings.ReplaceAll(line, "^SIZE", sizeStr)
				line = strings.ReplaceAll(line, "^DESC", firstDesc)

				wErr = writeProcessedStringWithManualEncoding(terminal, []byte(line), outputMode)
				if wErr != nil {
					slog.Error("failed writing file list line", "node", nodeNumber, "line", i, "error", wErr)
				}
				wErr = terminalio.WriteProcessedBytes(terminal, []byte("\r\n"), outputMode)
				if wErr != nil {
					slog.Error("failed writing CRLF after file list line", "node", nodeNumber, "line", i, "error", wErr)
				}

				prefixLine := processedMidTemplate
				prefixLine = strings.ReplaceAll(prefixLine, "^MARK", " ")
				prefixLine = strings.ReplaceAll(prefixLine, "^NUM", "   ")
				prefixLine = strings.ReplaceAll(prefixLine, "^NAME", strings.Repeat(" ", 12))
				prefixLine = strings.ReplaceAll(prefixLine, "^DATE", strings.Repeat(" ", 8))
				prefixLine = strings.ReplaceAll(prefixLine, "^SIZE", strings.Repeat(" ", 5))
				prefixLine = strings.ReplaceAll(prefixLine, "^DESC", "")
				processedPrefix := string(ansi.ReplacePipeCodes([]byte(prefixLine)))
				prefixLen := ansi.VisibleLength(processedPrefix)
				descIndent := strings.Repeat(" ", prefixLen)
				for j := 1; j < len(dizLines); j++ {
					contLine := "|07" + descIndent + dizLines[j]
					wErr = writeProcessedStringWithManualEncoding(terminal, ansi.ReplacePipeCodes([]byte(contLine)), outputMode)
					if wErr != nil {
						break
					}
					_ = terminalio.WriteProcessedBytes(terminal, []byte("\r\n"), outputMode)
				}

			}
		}

		// 4.4 Display Bottom Template (with pagination info)
		botRendered := processFileListPlaceholders(botTemplateBytes, currentPage, totalPages, totalFiles, fconfpath)
		bottomLine := string(ansi.ReplacePipeCodes(botRendered))
		bottomLine = strings.ReplaceAll(bottomLine, "^PAGE", strconv.Itoa(currentPage))
		bottomLine = strings.ReplaceAll(bottomLine, "^TOTALPAGES", strconv.Itoa(totalPages))
		wErr = terminalio.WriteProcessedBytes(terminal, []byte(bottomLine), outputMode)
		if wErr != nil {
			slog.Error("failed writing LISTFILES bottom template", "node", nodeNumber, "error", wErr)
			// Handle error
		}

		// 4.5 Display Prompt (Use a standard file list prompt or configure one)
		// TODO: Use configurable prompt string
		prompt := "\r\n|07File Cmd (|15N|07=Next, |15P|07=Prev, |15#|07=Mark, |15V|07=View, |15D|07=Download, |15U|07=Upload, |15Q|07=Quit): |15"
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(prompt)), outputMode)

		// 4.6 Read User Input
		input, err := readLineFromSessionIH(s, terminal)
		if err != nil {
			if errors.Is(err, io.EOF) {
				slog.Info("user disconnected during LISTFILES", "node", nodeNumber)
				return nil, "LOGOFF", io.EOF
			}
			slog.Error("failed reading LISTFILES input", "node", nodeNumber, "error", err)
			// Consider retry or exit
			return nil, "", err
		}

		upperInput := strings.ToUpper(strings.TrimSpace(input))

		// 4.7 Process Input
		switch upperInput {
		case "N", " ", "": // Next Page (Space/Enter default to Next)
			if currentPage < totalPages {
				currentPage++
				// Fetch files for the new page
				filesOnPage, err = e.FileMgr.GetFilesForAreaPaginated(currentAreaID, currentPage, filesPerPage)
				if err != nil {
					// Log error and potentially return or break the loop
					slog.Error("failed to get files for page", "node", nodeNumber, "page", currentPage, "error", err)
					// Display error message to user?
					time.Sleep(1 * time.Second)
				}
			} else {
				// Indicate last page (optional feedback)
				terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte("\r\n|07Already on last page.|07")), outputMode)
				time.Sleep(500 * time.Millisecond)
			}
			continue // Redraw loop
		case "P": // Previous Page
			if currentPage > 1 {
				currentPage--
				// Fetch files for the new page
				filesOnPage, err = e.FileMgr.GetFilesForAreaPaginated(currentAreaID, currentPage, filesPerPage)
				if err != nil {
					// Log error and potentially return or break the loop
					slog.Error("failed to get files for page", "node", nodeNumber, "page", currentPage, "error", err)
					// Display error message to user?
					time.Sleep(1 * time.Second)
				}
			} else {
				// Indicate first page (optional feedback)
				terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte("\r\n|07Already on first page.|07")), outputMode)
				time.Sleep(500 * time.Millisecond)
			}
			continue // Redraw loop
		case "Q": // Quit
			slog.Debug("user quit LISTFILES", "node", nodeNumber)
			return nil, "", nil // Return to FILEM menu
		case "D": // Download marked files
			slog.Debug("user initiated download command", "node", nodeNumber, "handle", currentUser.Handle, "area", currentAreaID)

			// 1. Check if any files are marked
			if len(currentUser.TaggedFileIDs) == 0 {
				msg := "\r\n|07No files marked for download. Use |15#|07 to mark files.|07\r\n"
				terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
				time.Sleep(1 * time.Second)
				continue // Go back to file list display
			}

			// 2. Confirm download
			confirmPrompt := fmt.Sprintf("Download %d marked file(s)?", len(currentUser.TaggedFileIDs))
			// Use WriteProcessedBytes for SaveCursor, positioning, and clear line
			// Need to position this prompt carefully, perhaps near the bottom prompt line.
			// For now, just display it after the main prompt. TODO: Improve positioning.
			terminalio.WriteProcessedBytes(terminal, []byte(ansi.SaveCursor()), outputMode)
			terminalio.WriteProcessedBytes(terminal, []byte("\r\n\x1b[K"), outputMode) // Newline, clear line

			proceed, err := e.PromptYesNo(s, terminal, confirmPrompt, outputMode, nodeNumber, termWidth, termHeight, false)
			terminalio.WriteProcessedBytes(terminal, []byte(ansi.RestoreCursor()), outputMode) // Restore cursor after prompt

			if err != nil {
				if errors.Is(err, io.EOF) {
					slog.Info("user disconnected during download confirmation", "node", nodeNumber)
					return nil, "LOGOFF", io.EOF
				}
				slog.Error("error getting download confirmation", "node", nodeNumber, "error", err)
				msg := "\r\n|01Error during confirmation.|07\r\n"
				terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
				time.Sleep(1 * time.Second)
				continue // Back to file list
			}

			if !proceed {
				slog.Debug("user cancelled download", "node", nodeNumber)
				terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte("\r\n|07Download cancelled.|07")), outputMode)
				time.Sleep(500 * time.Millisecond)
				continue // Back to file list
			}

			// 3. Protocol selection
			proto, protoOK, protoErr := e.selectTransferProtocol(s, terminal, outputMode)
			if protoErr != nil {
				if errors.Is(protoErr, io.EOF) {
					return nil, "LOGOFF", protoErr
				}
				slog.Error("protocol selection error", "node", nodeNumber, "error", protoErr)
				terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte("\r\n|01Error: No transfer protocols configured on this system.|07\r\n")), outputMode)
				time.Sleep(2 * time.Second)
				continue
			}
			if !protoOK {
				terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte("\r\n|07Download cancelled.|07\r\n")), outputMode)
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
			for _, fileID := range currentUser.TaggedFileIDs {
				filePath, pathErr := e.FileMgr.GetFilePath(fileID)
				if pathErr != nil {
					slog.Error("failed to get path for file", "node", nodeNumber, "fileID", fileID, "error", pathErr)
					failCount++
					continue
				}
				if _, statErr := os.Stat(filePath); statErr != nil {
					slog.Error("file not on disk", "node", nodeNumber, "path", filePath, "fileID", fileID, "error", statErr)
					failCount++
					continue
				}
				resolved = append(resolved, dlEntry{id: fileID, path: filePath, name: filepath.Base(filePath)})
			}

			if len(resolved) == 0 {
				slog.Warn("no valid file paths found for tagged files", "node", nodeNumber)
				terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte("\r\n|01Could not find any of the marked files on the server.|07\r\n")), outputMode)
				failCount = len(currentUser.TaggedFileIDs)
			} else {
				paths := make([]string, len(resolved))
				fileIDs := make([]uuid.UUID, len(resolved))
				for i, fe := range resolved {
					paths[i] = fe.path
					fileIDs[i] = fe.id
				}
				transferSuccess, transferFail := e.runTransferSend(s, terminal, proto, paths, fileIDs, outputMode, nodeNumber)
				successCount += transferSuccess
				failCount += transferFail
				time.Sleep(1 * time.Second)
			}

			// 4. Clear tags, update download count, and save user state
			slog.Debug("clearing tagged file IDs", "node", nodeNumber, "count", len(currentUser.TaggedFileIDs), "handle", currentUser.Handle)
			currentUser.TaggedFileIDs = nil // Clear the list
			currentUser.NumDownloads += successCount
			if err := userManager.UpdateUser(currentUser); err != nil {
				slog.Error("failed to save user data after download attempt", "node", nodeNumber, "error", err)
				// Inform user? State might be inconsistent.
				terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte("\r\n|01Error saving user state after download.|07")), outputMode)
			}

			// 5. Final status message
			statusMsg := fmt.Sprintf("|07Download attempt finished. Success: %d, Failed: %d.|07\r\n", successCount, failCount)
			terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(statusMsg)), outputMode)
			time.Sleep(2 * time.Second)

			// Go back to the file list (will redraw with cleared marks)
			continue
		case "U": // Upload Files
			slog.Debug("upload command entered", "node", nodeNumber, "area", currentAreaID, "tag", currentAreaTag)
			uploadErr := e.runUploadFiles(s, terminal, currentUser, userManager, currentAreaID, currentAreaTag, outputMode, nodeNumber, sessionStartTime)
			if uploadErr != nil {
				if errors.Is(uploadErr, io.EOF) {
					return nil, "LOGOFF", uploadErr
				}
				slog.Error("upload failed", "node", nodeNumber, "error", uploadErr)
			}
			// Reload user to get updated NumUploads
			if reloaded, exists := userManager.GetUser(currentUser.Handle); exists {
				currentUser = reloaded
			}
			// Refresh file count and page data
			totalFiles, _ = e.FileMgr.GetFileCountForArea(currentAreaID)
			if filesPerPage > 0 {
				totalPages = (totalFiles + filesPerPage - 1) / filesPerPage
			}
			if totalPages == 0 {
				totalPages = 1
			}
			if currentPage > totalPages {
				currentPage = totalPages
			}
			filesOnPage, _ = e.FileMgr.GetFilesForAreaPaginated(currentAreaID, currentPage, filesPerPage)
			continue
		case "V": // View file
			slog.Debug("view command entered in file list", "node", nodeNumber)
			viewPrompt := "\r\n|07Enter file # to view (or |15ENTER|07 to cancel): |15"
			terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(viewPrompt)), outputMode)
			viewInput, viewErr := readLineFromSessionIH(s, terminal)
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
				terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte("\r\n|01Invalid file number.|07\r\n")), outputMode)
				time.Sleep(500 * time.Millisecond)
				continue
			}
			viewIndex := fileNumToView - 1 - (currentPage-1)*filesPerPage
			if viewIndex < 0 || viewIndex >= len(filesOnPage) {
				terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte("\r\n|01File number not on current page.|07\r\n")), outputMode)
				time.Sleep(500 * time.Millisecond)
				continue
			}
			fileToView := filesOnPage[viewIndex]
			if e.FileMgr.IsSupportedArchive(fileToView.Filename) {
				viewFilePath, pathErr := e.FileMgr.GetFilePath(fileToView.ID)
				if pathErr != nil {
					slog.Error("failed to get path for file", "node", nodeNumber, "fileID", fileToView.ID, "error", pathErr)
					terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte("\r\n|01Error locating file.|07\r\n")), outputMode)
					time.Sleep(1 * time.Second)
				} else {
					ctx, cancel := e.transferContext(s.Context())
					ziplab.RunZipLabView(ctx, s, terminal, viewFilePath, fileToView.Filename, outputMode, sessionReadLine(s, terminal), sessionReadKey(s))
					cancel()
				}
			} else {
				viewFileByRecord(e, s, terminal, &fileToView, outputMode, termWidth, termHeight)
			}
			continue
		case "A": // Area Change (Placeholder/Not implemented here, handled by menu?)
			slog.Debug("area change command entered (handled by menu)", "node", nodeNumber)
			msg := "\r\n|01Use menu options to change area.|07\r\n"
			terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
			time.Sleep(1 * time.Second)
		default: // Includes 'T' (Tagging) and potential numeric input
			// Try to parse as a number for tagging
			fileNumToTag, err := strconv.Atoi(upperInput)
			if err == nil && fileNumToTag > 0 {
				// Valid number entered, attempt to tag/untag
				fileIndex := fileNumToTag - 1 - (currentPage-1)*filesPerPage
				if fileIndex >= 0 && fileIndex < len(filesOnPage) {
					fileToToggle := filesOnPage[fileIndex]
					found := false
					newTaggedIDs := []uuid.UUID{}
					if currentUser.TaggedFileIDs != nil {
						for _, taggedID := range currentUser.TaggedFileIDs {
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
						slog.Debug("user tagged file", "node", nodeNumber, "handle", currentUser.Handle, "fileNum", fileNumToTag, "fileID", fileToToggle.ID)
					} else {
						// File was tagged, so we removed it (untagged)
						slog.Debug("user untagged file", "node", nodeNumber, "handle", currentUser.Handle, "file_num", fileNumToTag, "id", fileToToggle.ID)
					}
					currentUser.TaggedFileIDs = newTaggedIDs
					// No page change needed, loop will redraw with updated marks
				} else {
					// Invalid file number for current page
					slog.Debug("invalid file number entered", "node", nodeNumber, "file_num", fileNumToTag)
					// Optional: Add user feedback message
				}
			} else {
				// Input was not N, P, Q, D, U, V, A, or a valid number - Invalid command
				slog.Debug("invalid command entered in LISTFILES", "node", nodeNumber, "input", upperInput)
				// Optional: Add user feedback message
			}
		} // end switch
	} // end for loop

	// Should not be reached normally
	// return nil, "", nil
}
