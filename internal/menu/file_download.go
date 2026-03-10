package menu

import (
	"errors"
	"fmt"
	"io"
	"log"
	"strings"
	"time"

	"github.com/gliderlabs/ssh"
	"github.com/google/uuid"
	"golang.org/x/term"

	"github.com/stlalpha/vision3/internal/ansi"
	"github.com/stlalpha/vision3/internal/terminalio"
	"github.com/stlalpha/vision3/internal/user"
)

// runClearBatch empties the user's tagged-file batch queue.
func runClearBatch(e *MenuExecutor, s ssh.Session, terminal *term.Terminal, userManager *user.UserMgr, currentUser *user.User, nodeNumber int, sessionStartTime time.Time, args string, outputMode ansi.OutputMode, termWidth int, termHeight int) (*user.User, string, error) {
	if currentUser == nil {
		return nil, "", nil
	}

	if len(currentUser.TaggedFileIDs) == 0 {
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(e.LoadedStrings.BatchQueueEmpty)), outputMode)
		time.Sleep(1 * time.Second)
		return currentUser, "", nil
	}

	count := len(currentUser.TaggedFileIDs)
	currentUser.TaggedFileIDs = nil

	if err := userManager.UpdateUser(currentUser); err != nil {
		log.Printf("ERROR: Node %d: Failed to update user after clearing batch queue: %v", nodeNumber, err)
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(e.LoadedStrings.SaveUserError)), outputMode)
		time.Sleep(1 * time.Second)
		return currentUser, "", nil
	}

	terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(fmt.Sprintf(e.LoadedStrings.BatchClearedFormat, count))), outputMode)
	log.Printf("INFO: Node %d: User %s cleared %d file(s) from batch queue", nodeNumber, currentUser.Handle, count)
	time.Sleep(1 * time.Second)

	return currentUser, "", nil
}

// downloadLoop implements the V2-style download workflow shared by DOWNLOADFILE
// and BATCHDOWNLOAD. When startByAdding is true the user is prompted for a
// filename before entering the main loop (DOWNLOADFILE behaviour).
func (e *MenuExecutor) downloadLoop(
	s ssh.Session,
	terminal *term.Terminal,
	userManager *user.UserMgr,
	currentUser *user.User,
	nodeNumber int,
	outputMode ansi.OutputMode,
	startByAdding bool,
) (*user.User, error) {

	// promptFilename shows the add-batch prompt and reads user input.
	// Returns the trimmed filename or "" if the user pressed Enter (cancel).
	promptFilename := func() (string, error) {
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(e.LoadedStrings.AddBatchPrompt)), outputMode)
		input, err := readLineFromSessionIH(s, terminal)
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(input), nil
	}

	// tryAddFile looks up filename in the current area and adds it to the
	// batch. Returns true if the file was added. On failure (not found,
	// duplicate, limit) it displays the appropriate message and returns false.
	tryAddFile := func(filename string) bool {
		rec, err := findFileInArea(e.FileMgr, currentUser.CurrentFileAreaID, filename)
		if err != nil {
			terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(fmt.Sprintf(e.LoadedStrings.FileNotFoundFormat, filename))), outputMode)
			time.Sleep(1 * time.Second)
			return false
		}
		for _, id := range currentUser.TaggedFileIDs {
			if id == rec.ID {
				terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(e.LoadedStrings.FileAlreadyMarked)), outputMode)
				time.Sleep(1 * time.Second)
				return false
			}
		}
		if len(currentUser.TaggedFileIDs) >= 50 {
			terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(e.LoadedStrings.FiftyFilesMaximum)), outputMode)
			time.Sleep(1 * time.Second)
			return false
		}
		currentUser.TaggedFileIDs = append(currentUser.TaggedFileIDs, rec.ID)
		if err := userManager.UpdateUser(currentUser); err != nil {
			log.Printf("ERROR: Node %d: Failed to persist tagged file: %v", nodeNumber, err)
		}
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(fmt.Sprintf(e.LoadedStrings.AddedToBatchFormat, rec.Filename))), outputMode)
		log.Printf("INFO: Node %d: User %s tagged file %s (%s)", nodeNumber, currentUser.Handle, rec.ID, rec.Filename)
		return true
	}

	// If starting from DOWNLOADFILE, prompt until a file is added or the
	// user cancels with empty input. Non-fatal failures re-prompt.
	if startByAdding {
		for {
			filename, err := promptFilename()
			if err != nil {
				return currentUser, err
			}
			if filename == "" {
				break // user cancelled
			}
			if tryAddFile(filename) {
				break // file added, proceed to main loop
			}
			// Not found / duplicate / limit — re-prompt
		}
		if len(currentUser.TaggedFileIDs) == 0 {
			return currentUser, nil
		}
	}

	// Main loop.
	for {
		if len(currentUser.TaggedFileIDs) == 0 {
			terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(e.LoadedStrings.NoFilesTagged)), outputMode)
			time.Sleep(1 * time.Second)
			return currentUser, nil
		}

		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(fmt.Sprintf(e.LoadedStrings.BatchCountFormat, len(currentUser.TaggedFileIDs)))), outputMode)

		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(e.LoadedStrings.DownloadStr)), outputMode)

		input, err := readLineFromSessionIH(s, terminal)
		if err != nil {
			return currentUser, err
		}
		input = strings.ToUpper(strings.TrimSpace(input))

		switch input {
		case "X":
			return currentUser, nil
		case "A":
			filename, err := promptFilename()
			if err != nil {
				return currentUser, err
			}
			if filename != "" {
				tryAddFile(filename)
			}
			continue
		case "", "C":
			// Proceed to transfer.
		default:
			continue
		}

		// Resolve tagged file IDs to paths.
		var paths []string
		var fileIDs []uuid.UUID
		for _, id := range currentUser.TaggedFileIDs {
			p, err := e.FileMgr.GetFilePath(id)
			if err != nil {
				log.Printf("WARN: Node %d: Could not resolve path for file %s: %v", nodeNumber, id, err)
				continue
			}
			paths = append(paths, p)
			fileIDs = append(fileIDs, id)
		}

		if len(paths) == 0 {
			terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(e.LoadedStrings.FilesResolveError)), outputMode)
			currentUser.TaggedFileIDs = nil
			if err := userManager.UpdateUser(currentUser); err != nil {
				log.Printf("ERROR: Node %d: Failed to persist cleared batch: %v", nodeNumber, err)
			}
			time.Sleep(2 * time.Second)
			return currentUser, nil
		}

		// Select transfer protocol.
		proto, ok, err := e.selectTransferProtocol(s, terminal, outputMode)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return currentUser, err
			}
			log.Printf("ERROR: Node %d: Protocol selection error: %v", nodeNumber, err)
			return currentUser, nil
		}
		if !ok {
			// Cancelled — back to loop.
			continue
		}

		// Execute transfer.
		successCount, failCount := e.runTransferSend(s, terminal, proto, paths, fileIDs, outputMode, nodeNumber)

		// Clear batch and update stats.
		currentUser.TaggedFileIDs = nil
		currentUser.NumDownloads += successCount

		if err := userManager.UpdateUser(currentUser); err != nil {
			log.Printf("ERROR: Node %d: Failed to update user after download: %v", nodeNumber, err)
		}

		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(fmt.Sprintf(e.LoadedStrings.DownloadFinishedFormat, successCount, failCount))), outputMode)
		log.Printf("INFO: Node %d: User %s download complete — success=%d fail=%d", nodeNumber, currentUser.Handle, successCount, failCount)
		time.Sleep(2 * time.Second)

		return currentUser, nil
	}
}

// runDownloadFile prompts the user for a filename and enters the download loop.
func runDownloadFile(e *MenuExecutor, s ssh.Session, terminal *term.Terminal, userManager *user.UserMgr, currentUser *user.User, nodeNumber int, sessionStartTime time.Time, args string, outputMode ansi.OutputMode, termWidth int, termHeight int) (*user.User, string, error) {
	if currentUser == nil {
		return nil, "", nil
	}

	if currentUser.CurrentFileAreaID <= 0 {
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(e.LoadedStrings.FileNoAreaSelected)), outputMode)
		time.Sleep(1 * time.Second)
		return currentUser, "", nil
	}

	area, ok := e.FileMgr.GetAreaByID(currentUser.CurrentFileAreaID)
	if !ok {
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(e.LoadedStrings.FileAreaNotFound)), outputMode)
		time.Sleep(1 * time.Second)
		return currentUser, "", nil
	}

	if area.ACSDownload != "" && !checkACS(area.ACSDownload, currentUser, s, terminal, sessionStartTime) {
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(e.LoadedStrings.YouCantDownloadHere)), outputMode)
		time.Sleep(1 * time.Second)
		return currentUser, "", nil
	}

	updatedUser, err := e.downloadLoop(s, terminal, userManager, currentUser, nodeNumber, outputMode, true)
	if err != nil {
		if errors.Is(err, io.EOF) {
			return nil, "LOGOFF", io.EOF
		}
		log.Printf("ERROR: Node %d: downloadLoop error: %v", nodeNumber, err)
		if updatedUser != nil {
			return updatedUser, "", nil
		}
		return currentUser, "", nil
	}

	return updatedUser, "", nil
}

// runBatchDownload transfers the user's already-tagged batch files without
// re-checking file area or download ACS (validated at tag time).
func runBatchDownload(e *MenuExecutor, s ssh.Session, terminal *term.Terminal, userManager *user.UserMgr, currentUser *user.User, nodeNumber int, sessionStartTime time.Time, args string, outputMode ansi.OutputMode, termWidth int, termHeight int) (*user.User, string, error) {
	if currentUser == nil {
		return nil, "", nil
	}

	if len(currentUser.TaggedFileIDs) == 0 {
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(e.LoadedStrings.NoFilesTagged)), outputMode)
		time.Sleep(1 * time.Second)
		return currentUser, "", nil
	}

	updatedUser, err := e.downloadLoop(s, terminal, userManager, currentUser, nodeNumber, outputMode, false)
	if err != nil {
		if errors.Is(err, io.EOF) {
			return nil, "LOGOFF", io.EOF
		}
		log.Printf("ERROR: Node %d: downloadLoop error: %v", nodeNumber, err)
		if updatedUser != nil {
			return updatedUser, "", nil
		}
		return currentUser, "", nil
	}

	return updatedUser, "", nil
}
