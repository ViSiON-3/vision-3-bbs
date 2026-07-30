package menu

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/ViSiON-3/vision-3-bbs/internal/editor"
	"github.com/ViSiON-3/vision-3-bbs/internal/file"
	"github.com/ViSiON-3/vision-3-bbs/internal/terminalio"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
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
		lb.toggleMark()

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
		if lb.viewFile() {
			frame.needFullRedraw = true
		}

	case "d":
		return lb.downloadFiles(frame)

	case "u":
		lb.uploadFiles(frame)

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
