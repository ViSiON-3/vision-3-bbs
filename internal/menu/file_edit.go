package menu

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gliderlabs/ssh"
	"golang.org/x/term"

	"github.com/stlalpha/vision3/internal/ansi"
	"github.com/stlalpha/vision3/internal/file"
	"github.com/stlalpha/vision3/internal/terminalio"
	"github.com/stlalpha/vision3/internal/user"
)

// runEditFileRecord implements the EDITFILERECORD sysop command for reviewing
// uploaded files one at a time with actions to change description, rename,
// delete, move, skip, or quit.
func runEditFileRecord(e *MenuExecutor, s ssh.Session, terminal *term.Terminal, userManager *user.UserMgr, currentUser *user.User, nodeNumber int, sessionStartTime time.Time, args string, outputMode ansi.OutputMode, termWidth int, termHeight int) (*user.User, string, error) {
	if currentUser == nil || e.FileMgr == nil {
		return currentUser, "", nil
	}

	if !e.isCoSysOpOrAbove(currentUser) {
		return currentUser, "", nil
	}

	// Ask whether to scan all areas or current only.
	terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(e.LoadedStrings.SysopReviewScanAll)), outputMode)
	scanInput, err := readLineFromSessionIH(s, terminal)
	if err != nil {
		return currentUser, "", err
	}

	scanAll := strings.EqualFold(strings.TrimSpace(scanInput), "Y")

	// Collect unreviewed files from chosen areas.
	var unreviewed []file.FileRecord
	if scanAll {
		for _, area := range e.FileMgr.ListAreas() {
			unreviewed = append(unreviewed, e.FileMgr.GetUnreviewedFiles(area.ID)...)
		}
	} else {
		currentAreaID := currentUser.CurrentFileAreaID
		if currentAreaID > 0 {
			unreviewed = e.FileMgr.GetUnreviewedFiles(currentAreaID)
		}
	}

	if len(unreviewed) == 0 {
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(e.LoadedStrings.SysopReviewNoFiles+"\r\n")), outputMode)
		time.Sleep(1 * time.Second)
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
			if err := editFileChangeDescription(e, s, terminal, rec, nodeNumber, outputMode); err != nil {
				return currentUser, "", err
			}
			markReviewed = true

		case "R":
			if err := editFileRename(e, s, terminal, rec, nodeNumber, outputMode); err != nil {
				return currentUser, "", err
			}
			markReviewed = true

		case "D":
			deleted, err := editFileDelete(e, s, terminal, rec, nodeNumber, outputMode)
			if err != nil {
				return currentUser, "", err
			}
			if deleted {
				continue // Skip review prompt, file is gone.
			}

		case "M":
			if err := editFileMove(e, s, terminal, rec, nodeNumber, outputMode); err != nil {
				return currentUser, "", err
			}
			markReviewed = true

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
				updateErr := e.FileMgr.UpdateFileRecord(rec.ID, func(r *file.FileRecord) {
					r.Reviewed = true
				})
				if updateErr != nil {
					log.Printf("ERROR: Node %d: Failed to mark file %s as reviewed: %v", nodeNumber, rec.Filename, updateErr)
				} else {
					terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(e.LoadedStrings.SysopReviewMarked+"\r\n")), outputMode)
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

	header := e.LoadedStrings.SysopReviewHeader
	terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(header+"\r\n")), outputMode)

	sizeStr := formatReviewSize(rec.Size)
	dateStr := rec.UploadedAt.Format("01/02/2006 15:04")

	info := fmt.Sprintf(
		"|15Filename  : |07%s\r\n"+
			"|15Size      : |07%s\r\n"+
			"|15Uploader  : |07%s\r\n"+
			"|15Date      : |07%s\r\n"+
			"|15Downloads : |07%d\r\n"+
			"|15Area      : |07%s\r\n"+
			"|15Desc      : |07%s\r\n",
		rec.Filename, sizeStr, rec.UploadedBy, dateStr, rec.DownloadCount, areaName, rec.Description,
	)
	terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(info+"\r\n")), outputMode)

	terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(e.LoadedStrings.SysopReviewPrompt)), outputMode)

	input, err := readLineFromSessionIH(s, terminal)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(input), nil
}

// editFileChangeDescription prompts for a new description and updates the record.
func editFileChangeDescription(e *MenuExecutor, s ssh.Session, terminal *term.Terminal, rec file.FileRecord, nodeNumber int, outputMode ansi.OutputMode) error {
	terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte("\r\n|15New description: |07")), outputMode)
	newDesc, err := readLineFromSessionIH(s, terminal)
	if err != nil {
		return err
	}
	newDesc = strings.TrimSpace(newDesc)
	if newDesc == "" {
		return nil
	}
	updateErr := e.FileMgr.UpdateFileRecord(rec.ID, func(r *file.FileRecord) {
		r.Description = newDesc
	})
	if updateErr != nil {
		log.Printf("ERROR: Node %d: Failed to update description for %s: %v", nodeNumber, rec.Filename, updateErr)
	}
	return nil
}

// editFileRename prompts for a new filename, validates it, renames on disk, and updates the record.
func editFileRename(e *MenuExecutor, s ssh.Session, terminal *term.Terminal, rec file.FileRecord, nodeNumber int, outputMode ansi.OutputMode) error {
	terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte("\r\n|15New filename: |07")), outputMode)
	newName, err := readLineFromSessionIH(s, terminal)
	if err != nil {
		return err
	}
	newName = strings.TrimSpace(newName)
	if newName == "" {
		return nil
	}

	// Prevent path traversal: only use the base name.
	safeName := filepath.Base(newName)
	if safeName != newName || safeName == "." || safeName == ".." {
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte("\r\n|12Invalid filename.\r\n")), outputMode)
		return nil
	}

	oldPath, pathErr := e.FileMgr.GetFilePath(rec.ID)
	if pathErr != nil {
		log.Printf("ERROR: Node %d: Failed to get file path for %s: %v", nodeNumber, rec.Filename, pathErr)
		return nil
	}

	dir := filepath.Dir(oldPath)
	newPath := filepath.Join(dir, safeName)

	if renameErr := os.Rename(oldPath, newPath); renameErr != nil {
		log.Printf("ERROR: Node %d: Failed to rename %s to %s: %v", nodeNumber, oldPath, newPath, renameErr)
		return nil
	}

	updateErr := e.FileMgr.UpdateFileRecord(rec.ID, func(r *file.FileRecord) {
		r.Filename = safeName
	})
	if updateErr != nil {
		log.Printf("ERROR: Node %d: Failed to update filename record for %s: %v", nodeNumber, rec.Filename, updateErr)
		if rollbackErr := os.Rename(newPath, oldPath); rollbackErr != nil {
			log.Printf("ERROR: Node %d: Rollback rename failed %s -> %s: %v (disk/DB inconsistent)", nodeNumber, newPath, oldPath, rollbackErr)
		}
		return nil
	}

	terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(e.LoadedStrings.SysopReviewRenamed+"\r\n")), outputMode)
	return nil
}

// editFileDelete confirms deletion and removes the file record and disk file.
// Returns true if the file was deleted.
func editFileDelete(e *MenuExecutor, s ssh.Session, terminal *term.Terminal, rec file.FileRecord, nodeNumber int, outputMode ansi.OutputMode) (bool, error) {
	confirm, err := editFilePromptYN(e, s, terminal, "\r\n|12Delete this file? |07[|15Y|07/|15N|07]: ", outputMode)
	if err != nil {
		return false, err
	}
	if !strings.EqualFold(confirm, "Y") {
		return false, nil
	}

	delErr := e.FileMgr.DeleteFileRecord(rec.ID, true)
	if delErr != nil {
		log.Printf("ERROR: Node %d: Failed to delete file %s: %v", nodeNumber, rec.Filename, delErr)
		return false, nil
	}
	return true, nil
}

// editFileMove shows a list of areas, prompts for a target, confirms, and moves the file.
func editFileMove(e *MenuExecutor, s ssh.Session, terminal *term.Terminal, rec file.FileRecord, nodeNumber int, outputMode ansi.OutputMode) error {
	areas := e.FileMgr.ListAreas()

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
		return err
	}
	areaInput = strings.TrimSpace(areaInput)
	if areaInput == "" {
		return nil
	}

	var targetID int
	if _, scanErr := fmt.Sscanf(areaInput, "%d", &targetID); scanErr != nil {
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte("\r\n|12Invalid area number.\r\n")), outputMode)
		return nil
	}

	targetArea, found := e.FileMgr.GetAreaByID(targetID)
	if !found {
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte("\r\n|12Area not found.\r\n")), outputMode)
		return nil
	}

	confirmMsg := fmt.Sprintf("\r\n|15Move to |11%s|15? |07[|15Y|07/|15N|07]: ", targetArea.Name)
	confirm, err := editFilePromptYN(e, s, terminal, confirmMsg, outputMode)
	if err != nil {
		return err
	}
	if !strings.EqualFold(confirm, "Y") {
		return nil
	}

	moveErr := e.FileMgr.MoveFileRecord(rec.ID, targetID)
	if moveErr != nil {
		log.Printf("ERROR: Node %d: Failed to move file %s to area %d: %v", nodeNumber, rec.Filename, targetID, moveErr)
	}
	return nil
}

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
