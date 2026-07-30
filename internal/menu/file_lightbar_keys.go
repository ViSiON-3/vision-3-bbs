package menu

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/ViSiON-3/vision-3-bbs/internal/editor"
	"github.com/ViSiON-3/vision-3-bbs/internal/file"
	"github.com/ViSiON-3/vision-3-bbs/internal/terminalio"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
	"github.com/ViSiON-3/vision-3-bbs/internal/ziplab"
)

// fileLightbar key handling: reading input, navigation, the sysop-bar
// toggle, and the command dispatch switch.

// readKeyOrLogoff reads the next key from lb.ih, mapping a disconnect or
// idle timeout to the "LOGOFF" result convention documented on
// runListFilesLightbar. exit is true whenever run() should return
// immediately (either a real read error or the LOGOFF mapping); keyInt is
// only meaningful when exit is false.
func (lb *fileLightbar) readKeyOrLogoff() (keyInt int, exit bool, action string, err error) {
	keyInt, readErr := lb.ih.ReadKey()
	if readErr != nil {
		if logoffIfDisconnected(readErr) {
			return 0, true, "LOGOFF", io.EOF
		}
		return 0, true, "", readErr
	}
	return keyInt, false, "", nil
}

// handleNavKey processes one key code read by run()'s main loop. It reports
// whether run() should exit immediately (exit, with result/action/err as the
// values run() should return — used for bare Esc); otherwise dispatch tells
// run() whether key was set to a command that the command switch
// (dispatchCommand) should handle. Pure navigation keys (arrows, paging,
// Home/End) mutate lb's selection/viewport/cmdIndex fields directly and
// report dispatch=false so run() simply loops around, matching the
// `continue` behavior of the original inline switch.
func (lb *fileLightbar) handleNavKey(keyInt int) (key string, dispatch bool, exit bool, result *user.User, action string, err error) {
	// Navigation keys — matched directly on integer key codes so that
	// multi-byte escape sequences (PageUp/PageDown etc.) are handled
	// atomically by the InputHandler and can never be split by the
	// 100 ms inter-byte ESC timeout, which previously caused bare ESC
	// to be returned and the lightbar to exit unexpectedly.
	switch keyInt {
	case editor.KeyArrowUp: // Up
		lb.selectedIndex--
		return "", false, false, nil, "", nil
	case editor.KeyArrowDown: // Down
		lb.selectedIndex++
		return "", false, false, nil, "", nil
	case editor.KeyArrowRight: // Right — command bar
		lb.cmdIndex++
		if lb.cmdIndex >= len(lb.cmdEntries) {
			lb.cmdIndex = 0
		}
		return "", false, false, nil, "", nil
	case editor.KeyArrowLeft: // Left — command bar
		lb.cmdIndex--
		if lb.cmdIndex < 0 {
			lb.cmdIndex = len(lb.cmdEntries) - 1
		}
		return "", false, false, nil, "", nil
	case editor.KeyPageUp, editor.KeyCtrlR: // Page Up
		newTop := lb.topIndexForPrevPage()
		lb.topIndex = newTop
		lb.selectedIndex = newTop
		return "", false, false, nil, "", nil
	case editor.KeyPageDown: // Page Down
		count := lb.filesVisibleFrom(lb.topIndex)
		nextTop := lb.topIndex + count
		if nextTop >= len(lb.allFiles) {
			if len(lb.allFiles) > 0 {
				lb.selectedIndex = len(lb.allFiles) - 1
			}
		} else {
			lb.topIndex = nextTop
			lb.selectedIndex = nextTop
		}
		return "", false, false, nil, "", nil
	case editor.KeyHome: // Home
		lb.selectedIndex = 0
		return "", false, false, nil, "", nil
	case editor.KeyEnd: // End
		if len(lb.allFiles) > 0 {
			lb.selectedIndex = len(lb.allFiles) - 1
		}
		return "", false, false, nil, "", nil
	case editor.KeyEsc: // Bare Esc
		return "", false, true, nil, "", nil
	case editor.KeyEnter: // Enter: execute selected command bar item
		return lb.cmdEntries[lb.cmdIndex].hotkey, true, false, nil, "", nil
	default:
		if keyInt >= 32 && keyInt < 127 {
			return strings.ToLower(string(rune(keyInt))), true, false, nil, "", nil
		}
		return "", false, false, nil, "", nil // Ignore non-printable, non-navigation keys
	}
}

// toggleSysopBar flips whether the sysop-only command bar entries are
// shown, swapping cmdEntries between the sysop and user sets and resetting
// cmdIndex to the first entry.
func (lb *fileLightbar) toggleSysopBar() {
	lb.showSysopBar = !lb.showSysopBar
	if lb.showSysopBar {
		lb.cmdEntries = make([]cmdEntry, len(lb.sysopEntries))
		copy(lb.cmdEntries, lb.sysopEntries)
	} else {
		lb.cmdEntries = make([]cmdEntry, len(lb.userEntries))
		copy(lb.cmdEntries, lb.userEntries)
	}
	lb.cmdIndex = 0
}

// dispatchCommand executes the hotkey or Enter-selected command bar item in
// key: mark toggle, quit, info overlay, view, download, upload, and the
// sysop-only edit/kill/move/rename commands. It follows the (exit, result,
// action, err) signal convention documented on handleNavKey: false/nil/""/nil
// means "handled, run() should loop around". Every bare `continue` in the
// original inline switch becomes `return false, nil, "", nil` here, because
// the switch was the last statement in run()'s for loop — continuing was
// already equivalent to falling out of the switch. continue/break statements
// inside a case's own nested for/switch are untouched: they still target
// that inner loop, not run()'s.
func (lb *fileLightbar) dispatchCommand(key string, frame *lbFrame) (exit bool, result *user.User, action string, err error) {
	switch key {
	case " ": // Space: toggle mark
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

	case "q":
		return true, nil, "", nil

	case "i": // Info: show file detail overlay
		refresh, infoExit, infoResult, infoAction, infoErr := lb.showFileInfo()
		if infoExit {
			return true, infoResult, infoAction, infoErr
		}
		if refresh {
			frame.needFullRedraw = true
		}

	case "v":
		if len(lb.allFiles) > 0 {
			sel := &lb.allFiles[lb.selectedIndex]
			filePath, pathErr := lb.e.FileMgr.GetFilePath(sel.ID)
			if pathErr != nil {
				slog.Error("failed to get path for file", "node", lb.nodeNumber, "id", sel.ID, "error", pathErr)
				return false, nil, "", nil
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
			frame.needFullRedraw = true
		}

	case "d":
		if len(lb.currentUser.TaggedFileIDs) == 0 {
			msg := "\r\n|07No files marked for download. Use |15Space|07 to mark files.|07\r\n"
			_ = lb.writePipe(msg)
			time.Sleep(1 * time.Second)
			frame.needFullRedraw = true
			return false, nil, "", nil
		}

		confirmPrompt := fmt.Sprintf("Download %d marked file(s)?", len(lb.currentUser.TaggedFileIDs))
		// Replace the footer lightbar with the confirm prompt instead of printing over the file list.
		lb.beginFooterPrompt()

		tw, th := getTerminalSize(lb.s)
		proceed, promptErr := lb.e.PromptYesNo(lb.s, lb.terminal, confirmPrompt, lb.outputMode, lb.nodeNumber, tw, th, false)
		if promptErr != nil {
			if logoffIfDisconnected(promptErr) {
				return true, nil, "LOGOFF", io.EOF
			}
			slog.Error("error getting download confirmation", "node", lb.nodeNumber, "error", promptErr)
			lb.endFooterPrompt()
			frame.needFullRedraw = true
			return false, nil, "", nil
		}

		if !proceed {
			lb.endFooterPrompt()
			frame.needFullRedraw = true
			return false, nil, "", nil
		}

		slog.Info("starting download", "node", lb.nodeNumber, "handle", lb.currentUser.Handle, "count", len(lb.currentUser.TaggedFileIDs))
		// Clear the screen before the download process begins.
		_ = terminalio.WriteProcessedBytes(lb.terminal, []byte("\x1b[2J\x1b[H"), lb.outputMode)
		_ = lb.writePipe("|07Preparing download...\r\n")
		time.Sleep(500 * time.Millisecond)

		successCount := 0
		failCount := 0
		filesToDownload := make([]string, 0, len(lb.currentUser.TaggedFileIDs))
		fileIDsToDownload := make([]uuid.UUID, 0, len(lb.currentUser.TaggedFileIDs))

		for _, fileID := range lb.currentUser.TaggedFileIDs {
			fp, pathErr := lb.e.FileMgr.GetFilePath(fileID)
			if pathErr != nil {
				slog.Error("failed to get path for file ID", "node", lb.nodeNumber, "id", fileID, "error", pathErr)
				failCount++
				continue
			}
			if _, statErr := os.Stat(fp); os.IsNotExist(statErr) {
				slog.Error("file path for ID does not exist", "node", lb.nodeNumber, "path", fp, "id", fileID)
				failCount++
				continue
			} else if statErr != nil {
				slog.Error("error stating file path for ID", "node", lb.nodeNumber, "path", fp, "id", fileID, "error", statErr)
				failCount++
				continue
			}
			filesToDownload = append(filesToDownload, fp)
			fileIDsToDownload = append(fileIDsToDownload, fileID)
		}

		if len(filesToDownload) > 0 {
			slog.Info("initiating transfer", "node", lb.nodeNumber, "count", len(filesToDownload))

			// Use protocol selection (respects connection type - SSH vs Telnet)
			proto, protoOK, protoErr := lb.e.selectTransferProtocol(lb.s, lb.terminal, lb.outputMode)
			if protoErr != nil {
				if logoffIfDisconnected(protoErr) {
					return true, nil, "LOGOFF", io.EOF
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
		} else {
			slog.Warn("no valid file paths found for tagged files", "node", lb.nodeNumber)
			_ = lb.writePipe("\r\n|01Could not find any of the marked files on the server.|07\r\n")
			// failCount already equals the number of missing files (every
			// tagged ID incremented it in the collection loop above).
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

	case "u":
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

	case "e": // Edit description (sysop only)
		if !lb.isSysop || len(lb.allFiles) == 0 {
			return false, nil, "", nil
		}
		rec := lb.allFiles[lb.selectedIndex]
		lb.beginFooterPrompt()
		_ = lb.writePipe("|15New description: |07")
		newDesc, readErr := readLineFromSessionIHAllowAbort(lb.s, lb.terminal)
		lb.endFooterPrompt()
		if readErr == nil && newDesc != "" {
			if updErr := lb.e.FileMgr.UpdateFileDescription(rec.ID, newDesc); updErr != nil {
				slog.Error("failed to update description", "node", lb.nodeNumber, "file", rec.Filename, "error", updErr)
			} else {
				lb.allFiles = lb.e.FileMgr.GetFilesForArea(lb.currentAreaID)
			}
		}
		frame.needFullRedraw = true

	case "k": // Kill/delete file (sysop only)
		if !lb.isSysop || len(lb.allFiles) == 0 {
			return false, nil, "", nil
		}
		rec := lb.allFiles[lb.selectedIndex]
		confirmPrompt := fmt.Sprintf("Delete %s from disk?", rec.Filename)
		lb.beginFooterPrompt()
		tw, th := getTerminalSize(lb.s)
		proceed, promptErr := lb.e.PromptYesNo(lb.s, lb.terminal, confirmPrompt, lb.outputMode, lb.nodeNumber, tw, th, false)
		lb.endFooterPrompt()
		if promptErr != nil {
			if logoffIfDisconnected(promptErr) {
				return true, nil, "LOGOFF", io.EOF
			}
			frame.needFullRedraw = true
			return false, nil, "", nil
		}
		if proceed {
			if delErr := lb.e.FileMgr.DeleteFileRecord(rec.ID, true); delErr != nil {
				slog.Error("failed to delete file", "node", lb.nodeNumber, "file", rec.Filename, "error", delErr)
			} else {
				slog.Info("sysop deleted file from area", "node", lb.nodeNumber, "file", rec.Filename, "area", lb.currentAreaID)
				// Remove from user's tag list so stale IDs don't reach batch download.
				filtered := lb.currentUser.TaggedFileIDs[:0]
				for _, tid := range lb.currentUser.TaggedFileIDs {
					if tid != rec.ID {
						filtered = append(filtered, tid)
					}
				}
				lb.currentUser.TaggedFileIDs = filtered
				lb.refreshFileList()
			}
		}
		frame.needFullRedraw = true

	case "m": // Move file to another area (sysop only)
		if !lb.isSysop || len(lb.allFiles) == 0 {
			return false, nil, "", nil
		}
		rec := lb.allFiles[lb.selectedIndex]
		lb.beginFooterPrompt()
		_ = lb.writePipe("|15Move to area (# or tag): |07")
		areaInput, readErr := readLineFromSessionIHAllowAbort(lb.s, lb.terminal)
		lb.endFooterPrompt()
		if readErr != nil || strings.TrimSpace(areaInput) == "" {
			frame.needFullRedraw = true
			return false, nil, "", nil
		}
		// Resolve area by ID or tag.
		var targetAreaID int
		var targetArea *file.FileArea
		if n, parseErr := fmt.Sscanf(strings.TrimSpace(areaInput), "%d", &targetAreaID); n == 1 && parseErr == nil {
			if a, ok := lb.e.FileMgr.GetAreaByID(targetAreaID); ok {
				targetArea = a
			}
		} else {
			if a, ok := lb.e.FileMgr.GetAreaByTag(strings.TrimSpace(areaInput)); ok {
				targetArea = a
				targetAreaID = a.ID
			}
		}
		if targetArea == nil {
			lb.errMsgPause("\r\n|01Area not found.|07\r\n")
			frame.needFullRedraw = true
			return false, nil, "", nil
		}
		confirmPrompt := fmt.Sprintf("Move %s to %s?", rec.Filename, targetArea.Name)
		_ = terminalio.WriteProcessedBytes(lb.terminal, []byte("\x1b[?25h"), lb.outputMode)
		tw, th := getTerminalSize(lb.s)
		proceed, promptErr := lb.e.PromptYesNo(lb.s, lb.terminal, confirmPrompt, lb.outputMode, lb.nodeNumber, tw, th, false)
		lb.endFooterPrompt()
		if promptErr != nil {
			if logoffIfDisconnected(promptErr) {
				return true, nil, "LOGOFF", io.EOF
			}
			frame.needFullRedraw = true
			return false, nil, "", nil
		}
		if proceed {
			if mvErr := lb.e.FileMgr.MoveFileRecord(rec.ID, targetAreaID); mvErr != nil {
				slog.Error("failed to move file to area", "node", lb.nodeNumber, "file", rec.Filename, "area", targetAreaID, "error", mvErr)
			} else {
				slog.Info("sysop moved file to area", "node", lb.nodeNumber, "file", rec.Filename, "area", targetAreaID, "tag", targetArea.Tag)
				lb.refreshFileList()
			}
		}
		frame.needFullRedraw = true

	case "r": // Rename file on disk (sysop only)
		if !lb.isSysop || len(lb.allFiles) == 0 {
			return false, nil, "", nil
		}
		rec := lb.allFiles[lb.selectedIndex]
		lb.beginFooterPrompt()
		_ = lb.writePipe("|15New filename: |07")
		newName, readErr := readLineFromSessionIHAllowAbort(lb.s, lb.terminal)
		lb.endFooterPrompt()
		if readErr != nil || strings.TrimSpace(newName) == "" {
			frame.needFullRedraw = true
			return false, nil, "", nil
		}
		newName = filepath.Base(strings.TrimSpace(newName))
		if newName == "." || newName == ".." {
			lb.errMsgPause("\r\n|01Invalid filename.|07\r\n")
			frame.needFullRedraw = true
			return false, nil, "", nil
		}
		// Check for duplicate filename in the current area.
		duplicate := false
		for _, f := range lb.allFiles {
			if strings.EqualFold(f.Filename, newName) && f.ID != rec.ID {
				duplicate = true
				break
			}
		}
		if duplicate {
			lb.errMsgPause("\r\n|01Filename already exists in this area.|07\r\n")
			frame.needFullRedraw = true
			return false, nil, "", nil
		}
		oldPath, pathErr := lb.e.FileMgr.GetFilePath(rec.ID)
		if pathErr != nil {
			slog.Error("failed to resolve path", "node", lb.nodeNumber, "file", rec.Filename, "error", pathErr)
			frame.needFullRedraw = true
			return false, nil, "", nil
		}
		newPath := filepath.Join(filepath.Dir(oldPath), newName)
		// Guard against clobbering an untracked file already on disk at the
		// target path (the duplicate check above only consults DB records).
		// os.SameFile permits a case-only rename of the same file on a
		// case-insensitive filesystem. A stat error other than "not exist"
		// (e.g. a permission/IO fault) means we cannot prove the target is
		// safe to overwrite, so refuse rather than risk a clobber.
		newInfo, statErr := os.Stat(newPath)
		switch {
		case statErr == nil:
			oldInfo, oldStatErr := os.Stat(oldPath)
			if oldStatErr != nil || !os.SameFile(oldInfo, newInfo) {
				slog.Error("rename target already exists on disk", "node", lb.nodeNumber, "path", newPath)
				lb.errMsgPause("\r\n|01A file with that name already exists.|07\r\n")
				frame.needFullRedraw = true
				return false, nil, "", nil
			}
		case !errors.Is(statErr, os.ErrNotExist):
			slog.Error("cannot stat rename target", "node", lb.nodeNumber, "path", newPath, "error", statErr)
			lb.errMsgPause("\r\n|01Rename failed.|07\r\n")
			frame.needFullRedraw = true
			return false, nil, "", nil
		}
		if renErr := os.Rename(oldPath, newPath); renErr != nil {
			slog.Error("failed to rename file", "node", lb.nodeNumber, "from", rec.Filename, "to", newName, "error", renErr)
			lb.errMsgPause("\r\n|01Rename failed.|07\r\n")
			frame.needFullRedraw = true
			return false, nil, "", nil
		}
		if updErr := lb.e.FileMgr.UpdateFileRecord(rec.ID, func(r *file.FileRecord) {
			r.Filename = newName
		}); updErr != nil {
			slog.Error("failed to update record", "node", lb.nodeNumber, "file", newName, "error", updErr)
			if rollbackErr := os.Rename(newPath, oldPath); rollbackErr != nil {
				slog.Error("rollback rename failed (disk/DB inconsistent)", "node", lb.nodeNumber, "file", newName, "error", rollbackErr)
			}
		} else {
			slog.Info("sysop renamed file in area", "node", lb.nodeNumber, "from", rec.Filename, "to", newName, "area", lb.currentAreaID)
			lb.allFiles = lb.e.FileMgr.GetFilesForArea(lb.currentAreaID)
		}
		frame.needFullRedraw = true
	}
	return false, nil, "", nil
}
