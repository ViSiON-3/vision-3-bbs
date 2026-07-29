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

	"github.com/google/uuid"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/terminalio"
	"github.com/ViSiON-3/vision-3-bbs/internal/ziplab"
)

// fetchPage re-fetches the current page's files from the file manager into
// st.filesOnPage, logging (but not returning) any error, matching the
// original inline pattern repeated across the classic LISTFILES loop.
func (st *fileListState) fetchPage() error {
	var err error
	st.filesOnPage, err = st.e.FileMgr.GetFilesForAreaPaginated(st.currentAreaID, st.currentPage, st.filesPerPage)
	if err != nil {
		slog.Error("failed to get files for page", "node", st.nodeNumber, "page", st.currentPage, "error", err)
	}
	return err
}

// handleFileDownload implements the "D" (download marked files) command of
// the classic LISTFILES loop.
func (st *fileListState) handleFileDownload() (logoff bool, err error) {
	slog.Debug("user initiated download command", "node", st.nodeNumber, "handle", st.currentUser.Handle, "area", st.currentAreaID)

	// 1. Check if any files are marked
	if len(st.currentUser.TaggedFileIDs) == 0 {
		msg := "\r\n|07No files marked for download. Use |15#|07 to mark files.|07\r\n"
		terminalio.WriteProcessedBytes(st.terminal, ansi.ReplacePipeCodes([]byte(msg)), st.outputMode)
		time.Sleep(1 * time.Second)
		return false, nil // Go back to file list display
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
			return true, io.EOF
		}
		slog.Error("error getting download confirmation", "node", st.nodeNumber, "error", err)
		msg := "\r\n|01Error during confirmation.|07\r\n"
		terminalio.WriteProcessedBytes(st.terminal, ansi.ReplacePipeCodes([]byte(msg)), st.outputMode)
		time.Sleep(1 * time.Second)
		return false, nil // Back to file list
	}

	if !proceed {
		slog.Debug("user cancelled download", "node", st.nodeNumber)
		terminalio.WriteProcessedBytes(st.terminal, ansi.ReplacePipeCodes([]byte("\r\n|07Download cancelled.|07")), st.outputMode)
		time.Sleep(500 * time.Millisecond)
		return false, nil // Back to file list
	}

	// 3. Protocol selection
	proto, protoOK, protoErr := st.e.selectTransferProtocol(st.s, st.terminal, st.outputMode)
	if protoErr != nil {
		if errors.Is(protoErr, io.EOF) {
			return true, protoErr
		}
		slog.Error("protocol selection error", "node", st.nodeNumber, "error", protoErr)
		terminalio.WriteProcessedBytes(st.terminal, ansi.ReplacePipeCodes([]byte("\r\n|01Error: No transfer protocols configured on this system.|07\r\n")), st.outputMode)
		time.Sleep(2 * time.Second)
		return false, nil
	}
	if !protoOK {
		terminalio.WriteProcessedBytes(st.terminal, ansi.ReplacePipeCodes([]byte("\r\n|07Download cancelled.|07\r\n")), st.outputMode)
		time.Sleep(500 * time.Millisecond)
		return false, nil
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
	return false, nil
}

// handleFileUpload implements the "U" (upload files) command of the classic
// LISTFILES loop.
func (st *fileListState) handleFileUpload() (logoff bool, err error) {
	slog.Debug("upload command entered", "node", st.nodeNumber, "area", st.currentAreaID, "tag", st.currentAreaTag)
	uploadErr := st.e.runUploadFiles(st.s, st.terminal, st.currentUser, st.userManager, st.currentAreaID, st.currentAreaTag, st.outputMode, st.nodeNumber, st.sessionStartTime)
	if uploadErr != nil {
		if errors.Is(uploadErr, io.EOF) {
			return true, uploadErr
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
	_ = st.fetchPage()
	return false, nil
}

// handleFileView implements the "V" (view file) command of the classic
// LISTFILES loop.
func (st *fileListState) handleFileView() (logoff bool, err error) {
	slog.Debug("view command entered in file list", "node", st.nodeNumber)
	viewPrompt := "\r\n|07Enter file # to view (or |15ENTER|07 to cancel): |15"
	terminalio.WriteProcessedBytes(st.terminal, ansi.ReplacePipeCodes([]byte(viewPrompt)), st.outputMode)
	viewInput, viewErr := readLineFromSessionIH(st.s, st.terminal)
	if viewErr != nil {
		if errors.Is(viewErr, io.EOF) {
			return true, io.EOF
		}
		return false, nil
	}
	viewNum := strings.TrimSpace(viewInput)
	if viewNum == "" {
		return false, nil
	}
	fileNumToView, parseErr := strconv.Atoi(viewNum)
	if parseErr != nil || fileNumToView <= 0 {
		terminalio.WriteProcessedBytes(st.terminal, ansi.ReplacePipeCodes([]byte("\r\n|01Invalid file number.|07\r\n")), st.outputMode)
		time.Sleep(500 * time.Millisecond)
		return false, nil
	}
	viewIndex := fileNumToView - 1 - (st.currentPage-1)*st.filesPerPage
	if viewIndex < 0 || viewIndex >= len(st.filesOnPage) {
		terminalio.WriteProcessedBytes(st.terminal, ansi.ReplacePipeCodes([]byte("\r\n|01File number not on current page.|07\r\n")), st.outputMode)
		time.Sleep(500 * time.Millisecond)
		return false, nil
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
	return false, nil
}

// toggleFileTag implements the default case of the classic LISTFILES loop:
// parsing a numeric file number and toggling its tagged (marked for
// download) state on the current user.
func (st *fileListState) toggleFileTag(upperInput string) {
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
}
