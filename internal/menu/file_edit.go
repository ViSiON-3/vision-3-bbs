package menu

import (
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	"github.com/gliderlabs/ssh"
	"golang.org/x/term"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/file"
	"github.com/ViSiON-3/vision-3-bbs/internal/perm"
	"github.com/ViSiON-3/vision-3-bbs/internal/terminalio"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// runEditFileRecord implements the EDITFILERECORD review queue for reviewing
// uploaded files one at a time with actions to change description, rename,
// delete, move, skip, or quit. It is open to whoever manages a file area
// (files.manage: a co-sysop, or a role granting it, #1044), and lists only
// the areas they manage. Each action checks again, against the stored
// record and the area as they are, just before it changes anything.
func runEditFileRecord(c *cmdCtx, args string) (*user.User, string, error) {
	e := c.e
	s := c.s
	terminal := c.terminal
	currentUser := c.currentUser
	nodeNumber := c.nodeNumber
	outputMode := c.outputMode

	if currentUser == nil || e.FileMgr == nil {
		return currentUser, "", nil
	}

	um := c.userManager
	managed := e.managedFileAreas(um, currentUser)
	if len(managed) == 0 && !e.isCoSysOpOrAbove(storedActor(um, currentUser)) {
		wv(terminal, "\r\n|04You don't manage any file areas.\r\n", outputMode)
		return currentUser, "", nil
	}
	// mayAct decides files.manage in areaID again just before a change.
	// The change itself is then made only while the file is still in that
	// area (the ...InArea / ...FromArea / conditional operations), so a file
	// moved elsewhere meanwhile isn't changed on the strength of this area.
	mayAct := func(areaID int) (perm.Decision, bool) {
		d := e.fileManagement(um, currentUser, areaID)
		if !d.Allowed {
			wv(terminal, "\r\n|12You can no longer manage files in this area. Nothing was changed.\r\n", outputMode)
		}
		return d, d.Allowed
	}

	// Ask whether to scan all areas or current only.
	e.writeString(terminal, outputMode, e.Strings().SysopReviewScanAll)
	scanInput, err := readLineFromSessionIH(s, terminal)
	if err != nil {
		return currentUser, "", err
	}

	scanAll := strings.EqualFold(strings.TrimSpace(scanInput), "Y")

	// Collect unreviewed files from chosen areas.
	var unreviewed []file.FileRecord
	if scanAll {
		for _, area := range managed {
			unreviewed = append(unreviewed, e.FileMgr.GetUnreviewedFiles(area.ID)...)
		}
	} else {
		currentAreaID := currentUser.CurrentFileAreaID
		for _, area := range managed {
			if area.ID == currentAreaID {
				unreviewed = e.FileMgr.GetUnreviewedFiles(currentAreaID)
			}
		}
	}

	if len(unreviewed) == 0 {
		e.writeString(terminal, outputMode, e.Strings().SysopReviewNoFiles)
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte("\r\n")), outputMode)
		uiPause(1 * time.Second)
		return currentUser, "", nil
	}

	for i := 0; i < len(unreviewed); i++ {
		rec := unreviewed[i]

		areaName := ""
		if area, found := e.FileMgr.GetAreaByID(rec.AreaID); found {
			areaName = area.Name
		}

		action, err := editFileShowAndPrompt(e, s, terminal, rec, areaName, outputMode)
		if err != nil {
			return currentUser, "", err
		}

		markReviewed := false

		switch strings.ToUpper(action) {
		case "C":
			if err := editFileChangeDescription(e, s, terminal, rec, currentUser.Handle, mayAct, nodeNumber, outputMode); err != nil {
				return currentUser, "", err
			}
			markReviewed = true

		case "R":
			renamed, err := editFileRename(e, s, terminal, rec, currentUser.Handle, mayAct, nodeNumber, outputMode)
			if err != nil {
				return currentUser, "", err
			}
			markReviewed = renamed // Only offer review after a rename that happened.

		case "D":
			deleted, err := editFileDelete(e, s, terminal, rec, currentUser.Handle, mayAct, nodeNumber, outputMode)
			if err != nil {
				return currentUser, "", err
			}
			if deleted {
				continue // Skip review prompt, file is gone.
			}

		case "M":
			moved, err := editFileMove(e, s, terminal, rec, currentUser.Handle, managed, mayAct, nodeNumber, outputMode)
			if err != nil {
				return currentUser, "", err
			}
			markReviewed = moved // Only offer review after a move that happened.

		case "S":
			continue

		case "Q":
			return currentUser, "", nil

		default:
			continue
		}

		if markReviewed {
			terminalio.WriteProcessedBytes(terminal, []byte("\r\n"), outputMode)
			reviewInput, err := editFilePromptYN(e, s, terminal, "|15Mark as reviewed? |07[|15Y|07/|15N|07]: ", outputMode)
			if err != nil {
				return currentUser, "", err
			}
			if strings.EqualFold(reviewInput, "Y") {
				// A moved file is reviewed in the area it is in now.
				areaID := rec.AreaID
				if cur, gErr := e.FileMgr.GetFileRecordByID(rec.ID); gErr == nil && cur != nil {
					areaID = cur.AreaID
				}
				d, ok := mayAct(areaID)
				if !ok {
					continue
				}
				updateErr := e.FileMgr.UpdateFileRecordInArea(rec.ID, areaID, func(r *file.FileRecord) {
					r.Reviewed = true
				})
				if errors.Is(updateErr, file.ErrRecordMoved) {
					wv(terminal, editFileMovedElsewhere, outputMode)
				} else if updateErr != nil {
					slog.Error("failed to mark file as reviewed", "node", nodeNumber, "file", rec.Filename, "error", updateErr)
				} else {
					logFileAction(d, "marked reviewed", nodeNumber, currentUser.Handle, rec)
					e.writeString(terminal, outputMode, e.Strings().SysopReviewMarked)
					terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte("\r\n")), outputMode)
				}
			}
		}
	}

	return currentUser, "", nil
}

// editFileShowAndPrompt clears the screen, displays file metadata, and prompts
// the sysop for an action. Returns the single-character action string.
func editFileShowAndPrompt(e *MenuExecutor, s ssh.Session, terminal *term.Terminal, rec file.FileRecord, areaName string, outputMode ansi.OutputMode) (string, error) {
	terminalio.WriteProcessedBytes(terminal, []byte(ansi.ClearScreen()), outputMode)

	header := e.Strings().SysopReviewHeader
	e.writeString(terminal, outputMode, header)
	terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte("\r\n")), outputMode)

	sizeStr := formatReviewSize(rec.Size)
	dateStr := boardTime(rec.UploadedAt).Format("01/02/2006 15:04")

	info := fmt.Sprintf(
		"|15Filename  : |07%s\r\n"+
			"|15Size      : |07%s\r\n"+
			"|15Uploader  : |07%s\r\n"+
			"|15Date      : |07%s\r\n"+
			"|15Downloads : |07%d\r\n"+
			"|15Area      : |07%s\r\n"+
			"|15Desc      : |07%s\r\n",
		callerText(rec.Filename), sizeStr, callerText(rec.UploadedBy), dateStr, rec.DownloadCount, areaName, callerText(rec.Description),
	)
	terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(info+"\r\n")), outputMode)

	e.writeString(terminal, outputMode, e.Strings().SysopReviewPrompt)

	input, err := readLineFromSessionIH(s, terminal)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(input), nil
}

// editFileChangeDescription prompts for a new description and updates the record.
func editFileChangeDescription(e *MenuExecutor, s ssh.Session, terminal *term.Terminal, rec file.FileRecord, handle string, mayAct fileActGuard, nodeNumber int, outputMode ansi.OutputMode) error {
	terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte("\r\n|15New description: |07")), outputMode)
	newDesc, err := readLineFromSessionIH(s, terminal)
	if err != nil {
		return err
	}
	newDesc = strings.TrimSpace(newDesc)
	if newDesc == "" {
		return nil
	}
	d, ok := mayAct(rec.AreaID)
	if !ok {
		return nil
	}
	updateErr := e.FileMgr.UpdateFileRecordInArea(rec.ID, rec.AreaID, func(r *file.FileRecord) {
		r.Description = newDesc
	})
	if errors.Is(updateErr, file.ErrRecordMoved) {
		wv(terminal, editFileMovedElsewhere, outputMode)
	} else if updateErr != nil {
		slog.Error("failed to update description", "node", nodeNumber, "file", rec.Filename, "error", updateErr)
	} else {
		logFileAction(d, "description changed", nodeNumber, handle, rec)
	}
	return nil
}

// editFileMovedElsewhere is the notice when the file left the area it was
// listed in while the caller was at a prompt.
const editFileMovedElsewhere = "\r\n|12That file has been moved or deleted elsewhere. Nothing was changed.\r\n"

// editFileRenameFailed tells the sysop a rename did not happen.
const editFileRenameFailed = "\r\n|12Rename failed.\r\n"

// editFileRename prompts for a new filename, validates it, renames on disk, and updates the record.
// It reports whether the file was renamed; a cancelled, invalid or failed
// rename returns false.
func editFileRename(e *MenuExecutor, s ssh.Session, terminal *term.Terminal, rec file.FileRecord, handle string, mayAct fileActGuard, nodeNumber int, outputMode ansi.OutputMode) (bool, error) {
	terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte("\r\n|15New filename: |07")), outputMode)
	newName, err := readLineFromSessionIH(s, terminal)
	if err != nil {
		return false, err
	}
	newName = strings.TrimSpace(newName)
	if newName == "" {
		return false, nil
	}

	// Prevent path traversal: only use the base name.
	safeName := filepath.Base(newName)
	if safeName != newName || safeName == "." || safeName == ".." {
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte("\r\n|12Invalid filename.\r\n")), outputMode)
		return false, nil
	}
	// The same name is not a rename (a case-only change still is): leave the
	// disk alone so the caller doesn't treat it as a change to review.
	if newName == rec.Filename {
		return false, nil
	}
	d, ok := mayAct(rec.AreaID)
	if !ok {
		return false, nil
	}
	// One step under the area's lock: the file must still be in the area it
	// was listed in, the name free, and the disk and the record change
	// together.
	renErr := e.FileMgr.RenameFileInArea(rec.ID, rec.AreaID, safeName)
	switch {
	case errors.Is(renErr, file.ErrRecordMoved):
		wv(terminal, editFileMovedElsewhere, outputMode)
		return false, nil
	case renErr != nil:
		slog.Error("failed to rename file", "node", nodeNumber, "from", rec.Filename, "to", safeName, "error", renErr)
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(editFileRenameFailed)), outputMode)
		return false, nil
	}

	logFileAction(d, "renamed", nodeNumber, handle, rec, "to", safeName)
	e.writeString(terminal, outputMode, e.Strings().SysopReviewRenamed)
	terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte("\r\n")), outputMode)
	return true, nil
}

// editFileDelete confirms deletion and removes the file record and disk file.
// Returns true if the file was deleted.
func editFileDelete(e *MenuExecutor, s ssh.Session, terminal *term.Terminal, rec file.FileRecord, handle string, mayAct fileActGuard, nodeNumber int, outputMode ansi.OutputMode) (bool, error) {
	confirm, err := editFilePromptYN(e, s, terminal, "\r\n|12Delete this file? |07[|15Y|07/|15N|07]: ", outputMode)
	if err != nil {
		return false, err
	}
	if !strings.EqualFold(confirm, "Y") {
		return false, nil
	}
	d, ok := mayAct(rec.AreaID)
	if !ok {
		return false, nil
	}

	deleted, delErr := e.FileMgr.DeleteFileRecordIf(rec.ID, true, func(r file.FileRecord) bool { return r.AreaID == rec.AreaID })
	if delErr == nil && !deleted || delErr != nil && e.fileRecordGone(rec.ID, rec.AreaID) {
		wv(terminal, editFileMovedElsewhere, outputMode)
		return false, nil
	}
	if delErr != nil {
		slog.Error("failed to delete file", "node", nodeNumber, "file", rec.Filename, "error", delErr)
		return false, nil
	}
	logFileAction(d, "deleted", nodeNumber, handle, rec)
	return true, nil
}

// editFileMove shows the areas the caller manages, prompts for a target,
// confirms, and moves the file: a move needs files.manage in both areas.
// It reports whether the file was moved; a cancelled, invalid or failed move
// returns false.
func editFileMove(e *MenuExecutor, s ssh.Session, terminal *term.Terminal, rec file.FileRecord, handle string, areas []file.FileArea, mayAct fileActGuard, nodeNumber int, outputMode ansi.OutputMode) (bool, error) {
	terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte("\r\n|15Available areas:\r\n")), outputMode)
	for _, a := range areas {
		if a.ID == rec.AreaID {
			continue // Skip current area.
		}
		line := fmt.Sprintf("|11%3d|07 - %s\r\n", a.ID, a.Name)
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(line)), outputMode)
	}

	terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte("|15Move to area #: |07")), outputMode)
	areaInput, err := readLineFromSessionIH(s, terminal)
	if err != nil {
		return false, err
	}
	areaInput = strings.TrimSpace(areaInput)
	if areaInput == "" {
		return false, nil
	}

	var targetID int
	if _, scanErr := fmt.Sscanf(areaInput, "%d", &targetID); scanErr != nil {
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte("\r\n|12Invalid area number.\r\n")), outputMode)
		return false, nil
	}

	targetArea, found := e.FileMgr.GetAreaByID(targetID)
	listed := false
	for _, a := range areas {
		listed = listed || (a.ID == targetID && a.ID != rec.AreaID)
	}
	if !found || !listed {
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte("\r\n|12Area not found.\r\n")), outputMode)
		return false, nil
	}

	confirmMsg := fmt.Sprintf("\r\n|15Move to |11%s|15? |07[|15Y|07/|15N|07]: ", targetArea.Name)
	confirm, err := editFilePromptYN(e, s, terminal, confirmMsg, outputMode)
	if err != nil {
		return false, err
	}
	if !strings.EqualFold(confirm, "Y") {
		return false, nil
	}
	d, ok := mayAct(rec.AreaID)
	if ok {
		_, ok = mayAct(targetID)
	}
	if !ok {
		return false, nil
	}

	moveErr := e.FileMgr.MoveFileRecordFromArea(rec.ID, rec.AreaID, targetID)
	if errors.Is(moveErr, file.ErrRecordMoved) {
		wv(terminal, editFileMovedElsewhere, outputMode)
		return false, nil
	}
	if moveErr != nil {
		slog.Error("failed to move file to area", "node", nodeNumber, "file", rec.Filename, "area", targetID, "error", moveErr)
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte("\r\n|12Move failed.\r\n")), outputMode)
		return false, nil
	}
	logFileAction(d, "moved", nodeNumber, handle, rec, "to_area", targetID)
	return true, nil
}

// fileActGuard decides files.manage in an area again just before a change,
// and tells the caller when it has been taken away.
type fileActGuard func(areaID int) (perm.Decision, bool)

// editFilePromptYN writes a prompt and reads a single-line response.
func editFilePromptYN(e *MenuExecutor, s ssh.Session, terminal *term.Terminal, prompt string, outputMode ansi.OutputMode) (string, error) {
	terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(prompt)), outputMode)
	input, err := readLineFromSessionIH(s, terminal)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(input), nil
}

// formatReviewSize formats a file size for display in the review screen.
func formatReviewSize(size int64) string {
	if size < 1024 {
		return fmt.Sprintf("%dB", size)
	}
	if size < 1024*1024 {
		return fmt.Sprintf("%dk", size/1024)
	}
	return fmt.Sprintf("%dM", size/(1024*1024))
}
