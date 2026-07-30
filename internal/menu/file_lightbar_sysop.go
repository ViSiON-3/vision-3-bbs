package menu

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"

	"github.com/ViSiON-3/vision-3-bbs/internal/file"
	"github.com/ViSiON-3/vision-3-bbs/internal/terminalio"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// fileLightbar sysop-only commands: edit description, kill, move, rename —
// plus the pure decision helpers they share.

// resolveTargetArea resolves a user-typed "move to area" input to a target
// area: numeric input is looked up by area ID, anything else by area tag.
// targetArea is nil if nothing matched.
func resolveTargetArea(fm *file.FileManager, input string) (targetAreaID int, targetArea *file.FileArea) {
	trimmed := strings.TrimSpace(input)
	if n, parseErr := fmt.Sscanf(trimmed, "%d", &targetAreaID); n == 1 && parseErr == nil {
		if a, ok := fm.GetAreaByID(targetAreaID); ok {
			targetArea = a
		}
	} else {
		if a, ok := fm.GetAreaByTag(trimmed); ok {
			targetArea = a
			targetAreaID = a.ID
		}
	}
	return targetAreaID, targetArea
}

// validateNewName cleans a user-provided rename target (basename only,
// trimmed) and checks it against the reserved "." / ".." names and the
// other files already in existingFiles (matched case-insensitively, since
// the filesystem this runs on may itself be case-insensitive). errMsg is
// empty when cleaned is safe to use as a new filename.
func validateNewName(newName string, existingFiles []file.FileRecord, currentID uuid.UUID) (cleaned string, errMsg string) {
	cleaned = filepath.Base(strings.TrimSpace(newName))
	if cleaned == "." || cleaned == ".." {
		return cleaned, "\r\n|01Invalid filename.|07\r\n"
	}
	for _, f := range existingFiles {
		if strings.EqualFold(f.Filename, cleaned) && f.ID != currentID {
			return cleaned, "\r\n|01Filename already exists in this area.|07\r\n"
		}
	}
	return cleaned, ""
}

// renameConflict classifies why safeRenameOnDisk refused to rename, so the
// caller can log and message the user exactly as the original inline case
// did for each distinct failure.
type renameConflict int

const (
	renameOK renameConflict = iota
	renameTargetExists
	renameStatFailed
	renameFailed
)

// safeRenameOnDisk renames oldPath to newPath, refusing to clobber an
// untracked file already on disk at the target path (the duplicate check in
// validateNewName only consults DB records). os.SameFile permits a
// case-only rename of the same file on a case-insensitive filesystem. A
// stat error other than "not exist" means we cannot prove the target is
// safe to overwrite, so it also refuses. renErr is the underlying error the
// caller should log alongside its own node/file context; it is nil on
// renameOK and renameTargetExists (which isn't itself an error).
func safeRenameOnDisk(oldPath, newPath string) (conflict renameConflict, renErr error) {
	newInfo, statErr := os.Stat(newPath)
	switch {
	case statErr == nil:
		oldInfo, oldStatErr := os.Stat(oldPath)
		if oldStatErr != nil || !os.SameFile(oldInfo, newInfo) {
			return renameTargetExists, nil
		}
	case !errors.Is(statErr, os.ErrNotExist):
		return renameStatFailed, statErr
	}
	if err := os.Rename(oldPath, newPath); err != nil {
		return renameFailed, err
	}
	return renameOK, nil
}

// editDescription runs the "e" sysop command: prompt for a new description
// and save it. There's no disconnect-mid-prompt LOGOFF path here (an abort
// or read error just means "don't save"), so this is a plain void method.
func (lb *fileLightbar) editDescription(frame *lbFrame) {
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
}

// killFile runs the "k" sysop command: confirm and delete the selected file
// from disk, dropping any stale tag reference to it.
func (lb *fileLightbar) killFile(frame *lbFrame) (exit bool, result *user.User, action string, err error) {
	rec := lb.allFiles[lb.selectedIndex]
	confirmPrompt := fmt.Sprintf("Delete %s from disk?", rec.Filename)
	lb.beginFooterPrompt()
	tw, th := getTerminalSize(lb.s)
	proceed, promptErr := lb.e.PromptYesNo(lb.s, lb.terminal, confirmPrompt, lb.outputMode, lb.nodeNumber, tw, th, false)
	lb.endFooterPrompt()
	if promptErr != nil {
		if errors.Is(promptErr, io.EOF) {
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
	return false, nil, "", nil
}

// moveFile runs the "m" sysop command: prompt for a target area (by ID or
// tag), confirm, and move the selected file's record there.
func (lb *fileLightbar) moveFile(frame *lbFrame) (exit bool, result *user.User, action string, err error) {
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
	targetAreaID, targetArea := resolveTargetArea(lb.e.FileMgr, areaInput)
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
		if errors.Is(promptErr, io.EOF) {
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
	return false, nil, "", nil
}

// renameFile runs the "r" sysop command: prompt for a new filename,
// validate and rename it on disk, and update the file record.
func (lb *fileLightbar) renameFile(frame *lbFrame) (exit bool, result *user.User, action string, err error) {
	rec := lb.allFiles[lb.selectedIndex]
	lb.beginFooterPrompt()
	_ = lb.writePipe("|15New filename: |07")
	newNameInput, readErr := readLineFromSessionIHAllowAbort(lb.s, lb.terminal)
	lb.endFooterPrompt()
	if readErr != nil || strings.TrimSpace(newNameInput) == "" {
		frame.needFullRedraw = true
		return false, nil, "", nil
	}
	newName, validateErrMsg := validateNewName(newNameInput, lb.allFiles, rec.ID)
	if validateErrMsg != "" {
		lb.errMsgPause(validateErrMsg)
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
	conflict, renErr := safeRenameOnDisk(oldPath, newPath)
	switch conflict {
	case renameTargetExists:
		slog.Error("rename target already exists on disk", "node", lb.nodeNumber, "path", newPath)
		lb.errMsgPause("\r\n|01A file with that name already exists.|07\r\n")
		frame.needFullRedraw = true
		return false, nil, "", nil
	case renameStatFailed:
		slog.Error("cannot stat rename target", "node", lb.nodeNumber, "path", newPath, "error", renErr)
		lb.errMsgPause("\r\n|01Rename failed.|07\r\n")
		frame.needFullRedraw = true
		return false, nil, "", nil
	case renameFailed:
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
	return false, nil, "", nil
}
