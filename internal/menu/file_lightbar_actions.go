package menu

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"time"

	"github.com/google/uuid"

	"github.com/ViSiON-3/vision-3-bbs/internal/file"
	"github.com/ViSiON-3/vision-3-bbs/internal/terminalio"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
	"github.com/ViSiON-3/vision-3-bbs/internal/ziplab"
)

// fileLightbar user-facing actions: mark toggle, view, download, upload.

// toggleMark toggles whether the selected file is tagged for download,
// saves the change to the user record, and redraws just the toggled row.
func (lb *fileLightbar) toggleMark() {
	if len(lb.allFiles) > 0 {
		fileID := lb.allFiles[lb.selectedIndex].ID
		found := false
		newTaggedIDs := make([]uuid.UUID, 0, len(lb.currentUser.TaggedFileIDs))
		for _, taggedID := range lb.currentUser.TaggedFileIDs {
			if taggedID == fileID {
				found = true
			} else {
				newTaggedIDs = append(newTaggedIDs, taggedID)
			}
		}
		if !found {
			newTaggedIDs = append(newTaggedIDs, fileID)
		}
		lb.currentUser.TaggedFileIDs = newTaggedIDs
		if err := lb.userManager.UpdateUser(lb.currentUser); err != nil {
			slog.Error("failed to save user after tag toggle", "node", lb.nodeNumber, "error", err)
		}
		// Redraw just the toggled row to show/hide the mark.
		if row, h := lb.screenRowForFile(lb.selectedIndex); row >= 0 {
			_ = lb.writeFileRow(row, lb.selectedIndex, true, h)
		}
	}
}

// viewFile displays the selected file — the ziplab viewer for supported
// archives, or the plain file viewer otherwise — and reports whether run()
// should do a full redraw afterward. refresh is false only when the file's
// on-disk path couldn't be resolved (matching the original inline case,
// which skipped the redraw in that case).
func (lb *fileLightbar) viewFile() (refresh bool) {
	if len(lb.allFiles) > 0 {
		sel := &lb.allFiles[lb.selectedIndex]
		filePath, pathErr := lb.e.FileMgr.GetFilePath(sel.ID)
		if pathErr != nil {
			slog.Error("failed to get path for file", "node", lb.nodeNumber, "id", sel.ID, "error", pathErr)
			return false
		}
		// Show cursor for the viewer.
		_ = terminalio.WriteProcessedBytes(lb.terminal, []byte("\x1b[?25h"), lb.outputMode)
		if lb.e.FileMgr.IsSupportedArchive(sel.Filename) {
			ctx, cancel := lb.e.transferContext(lb.s.Context())
			ziplab.RunZipLabView(ctx, lb.s, lb.terminal, filePath, sel.Filename, lb.outputMode, sessionReadLine(lb.s, lb.terminal), sessionReadKey(lb.s))
			cancel()
		} else {
			tw, th := getTerminalSize(lb.s)
			viewFileByRecord(lb.e, lb.s, lb.terminal, sel, lb.outputMode, tw, th)
		}
		// Hide cursor again.
		lb.endFooterPrompt()
		return true
	}
	return false
}

// confirmDownload prompts the user to confirm downloading the currently
// tagged files. proceed is only meaningful when exit is false: it is true
// if — and only if — the user confirmed.
func (lb *fileLightbar) confirmDownload() (proceed bool, exit bool, action string, err error) {
	confirmPrompt := fmt.Sprintf("Download %d marked file(s)?", len(lb.currentUser.TaggedFileIDs))
	// Replace the footer lightbar with the confirm prompt instead of printing over the file list.
	lb.beginFooterPrompt()

	tw, th := getTerminalSize(lb.s)
	proceed, promptErr := lb.e.PromptYesNo(lb.s, lb.terminal, confirmPrompt, lb.outputMode, lb.nodeNumber, tw, th, false)
	if promptErr != nil {
		if logoffIfDisconnected(promptErr) {
			return false, true, "LOGOFF", io.EOF
		}
		slog.Error("error getting download confirmation", "node", lb.nodeNumber, "error", promptErr)
		lb.endFooterPrompt()
		return false, false, "", nil
	}

	if !proceed {
		lb.endFooterPrompt()
		return false, false, "", nil
	}

	return true, false, "", nil
}

// collectTaggedPaths resolves each tagged file ID to a path on disk via fm,
// skipping (and counting as a failure) any ID whose record can't be
// resolved or whose backing file is missing or unreadable. paths and ids
// are parallel slices: paths[i] is the resolved path for ids[i].
func collectTaggedPaths(fm *file.FileManager, nodeNumber int, taggedIDs []uuid.UUID) (paths []string, ids []uuid.UUID, failCount int) {
	paths = make([]string, 0, len(taggedIDs))
	ids = make([]uuid.UUID, 0, len(taggedIDs))
	for _, fileID := range taggedIDs {
		fp, pathErr := fm.GetFilePath(fileID)
		if pathErr != nil {
			slog.Error("failed to get path for file ID", "node", nodeNumber, "id", fileID, "error", pathErr)
			failCount++
			continue
		}
		if _, statErr := os.Stat(fp); os.IsNotExist(statErr) {
			slog.Error("file path for ID does not exist", "node", nodeNumber, "path", fp, "id", fileID)
			failCount++
			continue
		} else if statErr != nil {
			slog.Error("error stating file path for ID", "node", nodeNumber, "path", fp, "id", fileID, "error", statErr)
			failCount++
			continue
		}
		paths = append(paths, fp)
		ids = append(ids, fileID)
	}
	return paths, ids, failCount
}

// sendTaggedFiles selects a transfer protocol and sends filesToDownload over
// it, adding to failCount on a protocol-selection failure. exit is set for
// a mid-selection disconnect (LOGOFF); failCount already reflects the files
// collectTaggedPaths could not resolve, per the comment preserved below.
func (lb *fileLightbar) sendTaggedFiles(filesToDownload []string, fileIDsToDownload []uuid.UUID, failCount int) (successCount int, newFailCount int, exit bool, action string, err error) {
	slog.Info("initiating transfer", "node", lb.nodeNumber, "count", len(filesToDownload))

	// Use protocol selection (respects connection type - SSH vs Telnet)
	proto, protoOK, protoErr := lb.e.selectTransferProtocol(lb.s, lb.terminal, lb.outputMode)
	if protoErr != nil {
		if logoffIfDisconnected(protoErr) {
			return 0, failCount, true, "LOGOFF", io.EOF
		}
		slog.Error("protocol selection error", "node", lb.nodeNumber, "error", protoErr)
		_ = lb.writePipe("\r\n|01Error: No transfer protocols configured on this system.|07\r\n")
		failCount += len(filesToDownload)
	} else if !protoOK {
		_ = lb.writePipe("\r\n|07Download cancelled.|07\r\n")
	} else {
		sentCount, sendFails := lb.e.runTransferSend(lb.s, lb.terminal, proto, filesToDownload, fileIDsToDownload, lb.outputMode, lb.nodeNumber)
		successCount = sentCount
		failCount += sendFails
		lb.ih = getSessionIH(lb.s)
	}
	time.Sleep(1 * time.Second)
	return successCount, failCount, false, "", nil
}

// downloadFiles drives the "d" command end to end: confirm, collect paths,
// send, and report the result. It returns the dispatchCommand exit signal.
func (lb *fileLightbar) downloadFiles(frame *lbFrame) (exit bool, result *user.User, action string, err error) {
	if len(lb.currentUser.TaggedFileIDs) == 0 {
		msg := "\r\n|07No files marked for download. Use |15Space|07 to mark files.|07\r\n"
		_ = lb.writePipe(msg)
		time.Sleep(1 * time.Second)
		frame.needFullRedraw = true
		return false, nil, "", nil
	}

	proceed, exit, action, err := lb.confirmDownload()
	if exit {
		return true, nil, action, err
	}
	if !proceed {
		frame.needFullRedraw = true
		return false, nil, "", nil
	}

	slog.Info("starting download", "node", lb.nodeNumber, "handle", lb.currentUser.Handle, "count", len(lb.currentUser.TaggedFileIDs))
	// Clear the screen before the download process begins.
	_ = terminalio.WriteProcessedBytes(lb.terminal, []byte("\x1b[2J\x1b[H"), lb.outputMode)
	_ = lb.writePipe("|07Preparing download...\r\n")
	time.Sleep(500 * time.Millisecond)

	filesToDownload, fileIDsToDownload, failCount := collectTaggedPaths(lb.e.FileMgr, lb.nodeNumber, lb.currentUser.TaggedFileIDs)

	successCount := 0
	if len(filesToDownload) > 0 {
		var sendExit bool
		successCount, failCount, sendExit, action, err = lb.sendTaggedFiles(filesToDownload, fileIDsToDownload, failCount)
		if sendExit {
			return true, nil, action, err
		}
	} else {
		slog.Warn("no valid file paths found for tagged files", "node", lb.nodeNumber)
		_ = lb.writePipe("\r\n|01Could not find any of the marked files on the server.|07\r\n")
		// failCount already equals the number of missing files (every
		// tagged ID incremented it in collectTaggedPaths above).
	}

	// Clear tags, update download count, and save.
	lb.currentUser.TaggedFileIDs = nil
	lb.currentUser.NumDownloads += successCount
	if saveErr := lb.userManager.UpdateUser(lb.currentUser); saveErr != nil {
		slog.Error("failed to save user data after download", "node", lb.nodeNumber, "error", saveErr)
	}

	statusMsg := fmt.Sprintf("|07Download finished. Success: %d, Failed: %d.|07\r\n", successCount, failCount)
	_ = lb.writePipe(statusMsg)
	time.Sleep(2 * time.Second)

	// Refresh file list.
	lb.refreshFileList()
	lb.endFooterPrompt()
	frame.needFullRedraw = true
	return false, nil, "", nil
}

// uploadFiles runs the upload prompt/transfer flow for the "u" command and
// refreshes the file list afterward.
func (lb *fileLightbar) uploadFiles(frame *lbFrame) {
	_ = terminalio.WriteProcessedBytes(lb.terminal, []byte("\x1b[?25h"), lb.outputMode)
	uploadErr := lb.e.runUploadFiles(lb.s, lb.terminal, lb.currentUser, lb.userManager, lb.currentAreaID, lb.currentAreaTag, lb.outputMode, lb.nodeNumber, lb.sessionStartTime)
	if uploadErr != nil {
		slog.Error("upload error", "node", lb.nodeNumber, "error", uploadErr)
	}
	// runUploadFiles calls resetSessionIH/getSessionIH internally,
	// so the local ih is now stale — refresh it.
	lb.ih = getSessionIH(lb.s)
	// Refresh file list after upload.
	lb.refreshFileList()
	lb.endFooterPrompt()
	frame.needFullRedraw = true
}
