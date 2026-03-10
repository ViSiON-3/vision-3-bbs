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
		msg := "|07Batch queue is already empty.\r\n"
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
		time.Sleep(1 * time.Second)
		return currentUser, "", nil
	}

	count := len(currentUser.TaggedFileIDs)
	currentUser.TaggedFileIDs = nil

	if err := userManager.UpdateUser(currentUser); err != nil {
		log.Printf("ERROR: Node %d: Failed to update user after clearing batch queue: %v", nodeNumber, err)
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte("|01Error saving user data.\r\n")), outputMode)
		time.Sleep(1 * time.Second)
		return currentUser, "", nil
	}

	msg := fmt.Sprintf("|15Cleared %d file(s) from batch queue.\r\n", count)
	terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
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

	// addFile prompts the user for a filename and appends the file to the
	// tagged batch. Returns true if a file was added.
	addFile := func() (bool, error) {
		prompt := e.LoadedStrings.AddBatchPrompt
		if prompt == "" {
			prompt = "|07Filename to add: "
		}
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(prompt)), outputMode)

		input, err := readLineFromSessionIH(s, terminal)
		if err != nil {
			return false, err
		}
		input = strings.TrimSpace(input)
		if input == "" {
			return false, nil
		}

		rec, err := findFileInArea(e.FileMgr, currentUser.CurrentFileAreaID, input)
		if err != nil {
			msg := e.LoadedStrings.FileNotFoundFormat
			if msg == "" {
				msg = "|01File not found: %s\r\n"
			}
			terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(fmt.Sprintf(msg, input))), outputMode)
			time.Sleep(1 * time.Second)
			return false, nil
		}

		// Check for duplicates.
		for _, id := range currentUser.TaggedFileIDs {
			if id == rec.ID {
				msg := e.LoadedStrings.FileAlreadyMarked
				if msg == "" {
					msg = "|01File already marked.\r\n"
				}
				terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
				time.Sleep(1 * time.Second)
				return false, nil
			}
		}

		// Check batch limit.
		if len(currentUser.TaggedFileIDs) >= 50 {
			msg := e.LoadedStrings.FiftyFilesMaximum
			if msg == "" {
				msg = "|0150 files maximum.\r\n"
			}
			terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
			time.Sleep(1 * time.Second)
			return false, nil
		}

		currentUser.TaggedFileIDs = append(currentUser.TaggedFileIDs, rec.ID)
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(fmt.Sprintf("|15%s|07 added to batch.\r\n", rec.Filename))), outputMode)
		log.Printf("INFO: Node %d: User %s tagged file %s (%s)", nodeNumber, currentUser.Handle, rec.ID, rec.Filename)
		return true, nil
	}

	// If starting from DOWNLOADFILE, prompt for the first file.
	if startByAdding {
		added, err := addFile()
		if err != nil {
			return currentUser, err
		}
		if !added && len(currentUser.TaggedFileIDs) == 0 {
			return currentUser, nil
		}
	}

	// Main loop.
	for {
		if len(currentUser.TaggedFileIDs) == 0 {
			terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte("|07No files tagged for download.\r\n")), outputMode)
			time.Sleep(1 * time.Second)
			return currentUser, nil
		}

		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(fmt.Sprintf("|14Batch: %d file(s) tagged.|07\r\n", len(currentUser.TaggedFileIDs)))), outputMode)

		prompt := e.LoadedStrings.DownloadStr
		if prompt == "" {
			prompt = "|07e(|15X|07)it, (|15A|07)dd, (|15CR|07) Continue: "
		}
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(prompt)), outputMode)

		input, err := readLineFromSessionIH(s, terminal)
		if err != nil {
			return currentUser, err
		}
		input = strings.ToUpper(strings.TrimSpace(input))

		switch input {
		case "X":
			return currentUser, nil
		case "A":
			if _, err := addFile(); err != nil {
				return currentUser, err
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
			terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte("|01No valid files could be resolved for download.\r\n")), outputMode)
			currentUser.TaggedFileIDs = nil
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

		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(fmt.Sprintf("|15Download finished. Success: %d, Failed: %d.|07\r\n", successCount, failCount))), outputMode)
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
		msg := e.LoadedStrings.FileNoAreaSelected
		if msg == "" {
			msg = "|01No file area selected.\r\n"
		}
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
		time.Sleep(1 * time.Second)
		return currentUser, "", nil
	}

	area, ok := e.FileMgr.GetAreaByID(currentUser.CurrentFileAreaID)
	if !ok {
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte("|01File area not found.\r\n")), outputMode)
		time.Sleep(1 * time.Second)
		return currentUser, "", nil
	}

	if area.ACSDownload != "" && !checkACS(area.ACSDownload, currentUser, s, terminal, sessionStartTime) {
		msg := e.LoadedStrings.YouCantDownloadHere
		if msg == "" {
			msg = "|01You can't download here.\r\n"
		}
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
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
