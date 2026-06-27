package menu

import (
	"bytes"
	"errors"
	"fmt"
	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/file"
	"github.com/ViSiON-3/vision-3-bbs/internal/telnetserver"
	"github.com/ViSiON-3/vision-3-bbs/internal/terminalio" // <-- Added import
	"github.com/ViSiON-3/vision-3-bbs/internal/transfer"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
	"github.com/ViSiON-3/vision-3-bbs/internal/ziplab"
	"github.com/gliderlabs/ssh"
	"github.com/google/uuid"
	"golang.org/x/term"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// scanDirectoryFiles returns a map of filename -> file size for all files in a directory,
// excluding metadata.json.
func scanDirectoryFiles(dir string) (map[string]int64, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("failed to read directory: %w", err)
	}
	files := make(map[string]int64)
	for _, entry := range entries {
		if entry.IsDir() || entry.Name() == "metadata.json" {
			continue
		}
		if entry.Type()&os.ModeSymlink != 0 {
			log.Printf("WARN: Skipping symlink %s", entry.Name())
			continue
		}
		info, err := entry.Info()
		if err != nil {
			log.Printf("WARN: Failed to get info for file %s: %v", entry.Name(), err)
			continue
		}
		if !info.Mode().IsRegular() {
			log.Printf("WARN: Skipping non-regular file %s", entry.Name())
			continue
		}
		files[entry.Name()] = info.Size()
	}
	return files, nil
}

// isTelnetSession returns true when s was established over a raw telnet connection.
func isTelnetSession(s ssh.Session) bool {
	_, ok := s.(*telnetserver.TelnetSessionAdapter)
	return ok
}

// selectTransferProtocol displays the available transfer protocols filtered for the
// current connection type, then prompts the user to choose one by key.
//
// Rules:
//   - Protocols with connection_type "" are shown on all connections.
//   - Protocols with connection_type "ssh" are shown on SSH sessions only.
//   - Protocols with connection_type "telnet" are shown on telnet sessions only.
//   - Pressing Enter selects the default protocol.
//   - Typing Q cancels. An unrecognised key re-prompts — no silent fallback.
//
// Returns (selected, true, nil) on selection, (zero, false, nil) on cancel,
// or (zero, false, err) on I/O error.
func (e *MenuExecutor) selectTransferProtocol(s ssh.Session, terminal *term.Terminal, outputMode ansi.OutputMode) (transfer.ProtocolConfig, bool, error) {
	// Filter protocols for this connection type.
	connType := transfer.ConnTypeSSH
	if isTelnetSession(s) {
		connType = transfer.ConnTypeTelnet
	}
	var available []transfer.ProtocolConfig
	for _, p := range e.Protocols {
		if p.ConnectionType == transfer.ConnTypeAny || p.ConnectionType == connType {
			available = append(available, p)
		}
	}
	if len(available) == 0 {
		return transfer.ProtocolConfig{}, false, fmt.Errorf("no transfer protocols configured for this connection type")
	}

	defaultProto, _ := transfer.DefaultProtocol(available)

	// Build the menu string once — reused on re-prompt.
	var menu strings.Builder
	menu.WriteString("\r\n|15Transfer Protocols:|07\r\n\r\n")
	for _, p := range available {
		if p.Default {
			menu.WriteString(fmt.Sprintf("  |15[|14%-3s|15]|07 %-22s |08(default)|07\r\n", p.Key, p.Name))
		} else {
			menu.WriteString(fmt.Sprintf("  |15[|14%-3s|15]|07 %s\r\n", p.Key, p.Name))
		}
	}
	menuBytes := ansi.ReplacePipeCodes([]byte(menu.String()))

	for {
		terminalio.WriteProcessedBytes(terminal, menuBytes, outputMode)
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(fmt.Sprintf("|07Select protocol |15[%s]|07, or |15Q|07 to cancel: ", defaultProto.Key))), outputMode)

		input, err := readLineFromSessionIH(s, terminal)
		if err != nil {
			return transfer.ProtocolConfig{}, false, err
		}
		input = strings.TrimSpace(input)

		if strings.ToUpper(input) == "Q" {
			return transfer.ProtocolConfig{}, false, nil
		}
		if input == "" {
			return defaultProto, true, nil
		}

		p, found := transfer.FindProtocol(available, input)
		if found {
			return p, true, nil
		}
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(fmt.Sprintf("\r\n|01Unknown protocol %q — please choose from the list above.|07\r\n", strings.ToUpper(input)))), outputMode)
	}
}

// runTransferSend executes a protocol send for the given file paths. It handles
// resetSessionIH/getSessionIH, batch vs one-at-a-time logic, ExecuteSend, error
// handling (including ErrBinaryNotFound), and IncrementDownloadCount.
// fileIDs must match paths in order (paths[i] corresponds to fileIDs[i]).
// Returns successCount and failCount.
func (e *MenuExecutor) runTransferSend(s ssh.Session, terminal *term.Terminal, proto transfer.ProtocolConfig, paths []string, fileIDs []uuid.UUID, outputMode ansi.OutputMode, nodeNumber int) (successCount, failCount int) {
	if len(paths) == 0 {
		return 0, 0
	}

	names := make([]string, len(paths))
	for i, p := range paths {
		names[i] = filepath.Base(p)
	}

	resetSessionIH(s)
	defer func() {
		time.Sleep(250 * time.Millisecond)
		getSessionIH(s)
	}()

	if proto.BatchSend && len(paths) > 1 {
		// Batch: single transfer session
		log.Printf("INFO: Node %d: Batch sending %d file(s) via %q: %v", nodeNumber, len(paths), proto.Name, names)
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(fmt.Sprintf("\r\n|15Initiating %s batch transfer (%d files)...|07\r\n", proto.Name, len(paths)))), outputMode)
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte("|07Please start the receive function in your terminal.\r\n")), outputMode)

		ctx, cancel := e.transferContext(s.Context())
		defer cancel()
		transferErr := proto.ExecuteSend(ctx, s, paths...)
		if transferErr != nil {
			log.Printf("ERROR: Node %d: %q batch send failed: %v", nodeNumber, proto.Name, transferErr)
			terminalio.WriteProcessedBytes(terminal, []byte(ansi.ClearScreen()), outputMode)
			if errors.Is(transferErr, transfer.ErrBinaryNotFound) {
				terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte("\r\n|01File transfer program not found!|07\r\n|07The SysOp needs to install the transfer binary (sexyz).\r\n|07See docs/sysop/files/file-transfer.md for setup instructions.\r\n")), outputMode)
			} else {
				terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte("|01Transfer failed or was cancelled.\r\n")), outputMode)
			}
			return 0, len(paths)
		}
		log.Printf("INFO: Node %d: %q batch send completed successfully.", nodeNumber, proto.Name)
		terminalio.WriteProcessedBytes(terminal, []byte(ansi.ClearScreen()), outputMode)
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte("|07Transfer complete.\r\n")), outputMode)
		for _, id := range fileIDs {
			if id != uuid.Nil {
				if err := e.FileMgr.IncrementDownloadCount(id); err != nil {
					log.Printf("WARN: Node %d: Failed to increment download count for %s: %v", nodeNumber, id, err)
				}
			}
		}
		return len(paths), 0
	}

	// One-at-a-time
	log.Printf("INFO: Node %d: Sending %d file(s) one-at-a-time via %q", nodeNumber, len(paths), proto.Name)
	terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(fmt.Sprintf("\r\n|15Initiating %s transfer (%d file(s), one at a time)...|07\r\n", proto.Name, len(paths)))), outputMode)
	terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte("|07Prepare your terminal to receive each file.\r\n")), outputMode)

	for i, p := range paths {
		ctx, cancel := e.transferContext(s.Context())
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(fmt.Sprintf("|15[%d/%d]|07 Sending: |14%s|07...", i+1, len(paths), names[i]))), outputMode)
		sendErr := proto.ExecuteSend(ctx, s, p)
		cancel()
		if sendErr != nil {
			log.Printf("ERROR: Node %d: %q send failed for %s: %v", nodeNumber, proto.Name, names[i], sendErr)
			if errors.Is(sendErr, transfer.ErrBinaryNotFound) {
				terminalio.WriteProcessedBytes(terminal, []byte(ansi.ClearScreen()), outputMode)
				terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte("\r\n|01File transfer program not found!|07\r\n|07The SysOp needs to install the transfer binary (sexyz).\r\n|07See docs/sysop/files/file-transfer.md for setup instructions.\r\n")), outputMode)
				return successCount, failCount + (len(paths) - i)
			}
			terminalio.WriteProcessedBytes(terminal, []byte(ansi.ClearScreen()), outputMode)
			terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(fmt.Sprintf("|15[%d/%d]|07 |14%s|07: |01FAILED|07\r\n", i+1, len(paths), names[i]))), outputMode)
			failCount++
			continue
		}
		log.Printf("INFO: Node %d: %q sent %s OK", nodeNumber, proto.Name, names[i])
		terminalio.WriteProcessedBytes(terminal, []byte(ansi.ClearScreen()), outputMode)
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(fmt.Sprintf("|15[%d/%d]|07 |14%s|07: |02OK|07\r\n", i+1, len(paths), names[i]))), outputMode)
		successCount++
		if i < len(fileIDs) && fileIDs[i] != uuid.Nil {
			if err := e.FileMgr.IncrementDownloadCount(fileIDs[i]); err != nil {
				log.Printf("WARN: Node %d: Failed to increment download count for %s: %v", nodeNumber, fileIDs[i], err)
			}
		}
	}
	return successCount, failCount
}

// runUploadFile is the RunnableFunc wrapper for UPLOADFILE menu commands.
func runUploadFile(e *MenuExecutor, s ssh.Session, terminal *term.Terminal, userManager *user.UserMgr, currentUser *user.User, nodeNumber int, sessionStartTime time.Time, args string, outputMode ansi.OutputMode, termWidth int, termHeight int) (*user.User, string, error) {
	if currentUser == nil {
		msg := "\r\n|01Error: You must be logged in to upload files.|07\r\n"
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
		time.Sleep(1 * time.Second)
		return nil, "", nil
	}

	currentAreaID := currentUser.CurrentFileAreaID
	currentAreaTag := currentUser.CurrentFileAreaTag
	if currentAreaID <= 0 {
		msg := "\r\n|01Error: No file area selected.|07\r\n"
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
		time.Sleep(1 * time.Second)
		return currentUser, "", nil
	}

	if err := e.runUploadFiles(s, terminal, currentUser, userManager, currentAreaID, currentAreaTag, outputMode, nodeNumber, sessionStartTime); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, "LOGOFF", err
		}
		log.Printf("ERROR: Node %d: Upload failed: %v", nodeNumber, err)
	}

	// Reload user to get updated NumUploads
	if reloaded, exists := userManager.GetUserByID(currentUser.ID); exists {
		currentUser = reloaded
	}
	return currentUser, "", nil
}

// runUploadFiles handles the ZMODEM upload workflow for the current file area.
func (e *MenuExecutor) runUploadFiles(
	s ssh.Session,
	terminal *term.Terminal,
	currentUser *user.User,
	userManager *user.UserMgr,
	currentAreaID int,
	currentAreaTag string,
	outputMode ansi.OutputMode,
	nodeNumber int,
	sessionStartTime time.Time,
) error {
	log.Printf("INFO: Node %d: User %s starting upload to area %d (%s)", nodeNumber, currentUser.Handle, currentAreaID, currentAreaTag)

	// 1. Check upload ACS
	area, areaExists := e.FileMgr.GetAreaByID(currentAreaID)
	if !areaExists {
		return fmt.Errorf("file area %d not found", currentAreaID)
	}

	if area.ACSUpload != "" && !checkACS(area.ACSUpload, currentUser, s, terminal, sessionStartTime) {
		log.Printf("WARN: Node %d: User %s denied upload access to area %s (ACS: %s)", nodeNumber, currentUser.Handle, currentAreaTag, area.ACSUpload)
		msg := "\r\n|01You do not have permission to upload to this area.|07\r\n"
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
		time.Sleep(2 * time.Second)
		return nil
	}

	// 2. Determine target directory
	targetDir, err := e.FileMgr.GetAreaUploadPath(currentAreaID)
	if err != nil {
		return fmt.Errorf("failed to resolve upload directory: %w", err)
	}

	// 3. Build set of existing filenames in metadata for duplicate checking
	existingFiles := e.FileMgr.GetFilesForArea(currentAreaID)
	existingNames := make(map[string]bool)
	for _, f := range existingFiles {
		existingNames[strings.ToLower(f.Filename)] = true
	}

	// 5. Protocol selection
	terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(fmt.Sprintf("\r\n|15Uploading to: |14%s|07\r\n", area.Name))), outputMode)
	proto, ok, protoErr := e.selectTransferProtocol(s, terminal, outputMode)
	if protoErr != nil {
		if errors.Is(protoErr, io.EOF) {
			return protoErr
		}
		log.Printf("ERROR: Node %d: Protocol selection error: %v", nodeNumber, protoErr)
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte("\r\n|01Error: No transfer protocols configured on this system.|07\r\n")), outputMode)
		time.Sleep(2 * time.Second)
		return nil
	}
	if !ok {
		return nil // user cancelled
	}

	// 6. Display instructions
	msg := fmt.Sprintf("\r\n|11Start the %s send in your terminal.|07\r\n|07After transfer, you will be prompted for file descriptions.\r\n\r\n|07Press |15ENTER|07 to begin or |15Q|07 to cancel: ", proto.Name)
	terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)

	input, err := readLineFromSessionIH(s, terminal)
	if err != nil {
		if errors.Is(err, io.EOF) {
			return err
		}
		return nil
	}
	if strings.ToUpper(strings.TrimSpace(input)) == "Q" {
		return nil
	}

	// 7. Create temp directory for receiving uploads
	incomingDir, err := os.MkdirTemp(targetDir, ".incoming-*")
	if err != nil {
		log.Printf("ERROR: Node %d: Failed to create incoming directory: %v", nodeNumber, err)
		return fmt.Errorf("failed to create incoming directory: %w", err)
	}
	defer os.RemoveAll(incomingDir)

	// 8. Execute protocol receive into temp directory
	msg = fmt.Sprintf("\r\n|15Starting %s receive...|07\r\n", proto.Name)
	terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)

	resetSessionIH(s)
	ctx, cancel := e.transferContext(s.Context())
	defer cancel()
	transferErr := proto.ExecuteReceive(ctx, s, incomingDir)
	time.Sleep(250 * time.Millisecond)
	getSessionIH(s)
	terminalio.WriteProcessedBytes(terminal, []byte(ansi.ClearScreen()), outputMode)
	if transferErr != nil {
		if errors.Is(transferErr, transfer.ErrBinaryNotFound) {
			log.Printf("ERROR: Node %d: Transfer binary not found: %v", nodeNumber, transferErr)
			terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte("\r\n|01File transfer program not found!|07\r\n|07The SysOp needs to install the transfer binary (sexyz).\r\n|07See docs/sysop/files/file-transfer.md for setup instructions.\r\n")), outputMode)
			return nil
		}
		log.Printf("WARN: Node %d: %q receive returned error: %v (checking for partial receives)", nodeNumber, proto.Name, transferErr)
	}

	// 9. Scan received files from temp directory.
	// Always scan even if transferErr != nil: rz exits non-zero when it times out
	// waiting for ZFIN, but may have already received files successfully.
	receivedFiles, err := scanDirectoryFiles(incomingDir)
	if err != nil {
		log.Printf("ERROR: Node %d: Failed to scan incoming directory: %v", nodeNumber, err)
		return nil
	}

	type newFileInfo struct {
		name string
		size int64
	}
	var newFiles []newFileInfo
	for filename, size := range receivedFiles {
		newFiles = append(newFiles, newFileInfo{name: filename, size: size})
	}

	if len(newFiles) == 0 {
		if transferErr != nil {
			errMsg := "\r\n|01Transfer receive failed.|07\r\n"
			terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(errMsg)), outputMode)
		} else {
			msg = "\r\n|07No new files detected.|07\r\n"
			terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
		}
		time.Sleep(2 * time.Second)
		return nil
	}

	// Sort by name for consistent ordering
	sort.Slice(newFiles, func(i, j int) bool {
		return newFiles[i].name < newFiles[j].name
	})

	log.Printf("INFO: Node %d: Detected %d new file(s) after upload", nodeNumber, len(newFiles))

	// 9. Process each new file
	successCount := 0
	duplicateCount := 0

	// Load ZipLab config once for all files
	zlCfg, zlErr := ziplab.LoadConfig(e.RootConfigPath)
	if zlErr != nil {
		log.Printf("WARN: Node %d: Failed to load ZipLab config: %v", nodeNumber, zlErr)
	}

	for _, nf := range newFiles {
		incomingPath := filepath.Join(incomingDir, nf.name)

		// Validate filename (defense in depth — rz -r should prevent this, but be safe)
		safeName := filepath.Base(nf.name)
		if safeName != nf.name || safeName == "." || safeName == ".." || strings.Contains(nf.name, "..") || filepath.IsAbs(nf.name) {
			log.Printf("ERROR: Node %d: Rejected unsafe filename: %s", nodeNumber, nf.name)
			os.Remove(incomingPath)
			errMsg := fmt.Sprintf("\r\n|01'%s' rejected: invalid filename.|07\r\n", nf.name)
			terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(errMsg)), outputMode)
			continue
		}

		// Check for duplicate in metadata
		if existingNames[strings.ToLower(nf.name)] {
			log.Printf("WARN: Node %d: Duplicate file rejected: %s", nodeNumber, nf.name)
			duplicateCount++
			os.Remove(incomingPath)

			dupMsg := fmt.Sprintf("\r\n|09'%s' already exists in this area. Rejected.|07\r\n", nf.name)
			terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(dupMsg)), outputMode)
			continue
		}

		// ZipLab processing for supported archive types (runs on file in incoming dir)
		var description string
		filePath := incomingPath

		if zlErr == nil && zlCfg.Enabled && zlCfg.RunOnUpload && zlCfg.IsArchiveSupported(nf.name) {
			log.Printf("INFO: Node %d: Running ZipLab pipeline on %s", nodeNumber, nf.name)

			zlBaseDir := filepath.Join(filepath.Dir(e.RootConfigPath), "ziplab")
			proc := ziplab.NewProcessor(zlCfg, zlBaseDir)

			// Load ZIPLAB.ANS and ZIPLAB.NFO for visual display
			ansiPath := filepath.Join(e.MenuSetPath, "ansi", "ZIPLAB.ANS")
			nfoPath := filepath.Join(e.MenuSetPath, "ansi", "ZIPLAB.NFO")

			ansiContent, _ := ansi.GetAnsiFileContent(ansiPath)
			nfo, _ := ziplab.ParseNFO(nfoPath)

			var result ziplab.PipelineResult
			if ansiContent != nil {
				result = proc.DisplayPipeline(terminal, nfo, ansiContent, filePath)
			} else {
				result = proc.RunPipeline(filePath, nil)
			}

			if !result.Success {
				log.Printf("ERROR: Node %d: ZipLab pipeline failed for %s: %v", nodeNumber, nf.name, result.Error)
				errMsg := fmt.Sprintf("\r\n|01ZipLab processing failed for '%s'.|07\r\n", nf.name)
				terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(errMsg)), outputMode)
				time.Sleep(2 * time.Second)
				continue
			}

			if result.Description != "" {
				description = sanitizeControlChars(strings.TrimRight(result.Description, " \t\r\n"))
				log.Printf("INFO: Node %d: Using FILE_ID.DIZ description for %s: %q", nodeNumber, nf.name, description)
			}
		}

		// Prompt for description if ZipLab didn't extract one
		if description == "" {
			pauseEnter(s, terminal, outputMode, e.LoadedStrings.FilePausePrompt)
			terminalio.WriteProcessedBytes(terminal, []byte(ansi.ClearScreen()), outputMode)
			descPrompt := fmt.Sprintf("\r\n|15%s|07 (%d bytes)\r\n|11Desc:|07 ", nf.name, nf.size)
			terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(descPrompt)), outputMode)

			descInput, err := readLineFromSessionIH(s, terminal)
			if err != nil {
				// If the session died (e.g. SSH client disconnected during the
				// transfer wait), preserve the upload with a default description
				// rather than LOGOFFing the user mid-file-processing.
				log.Printf("WARN: Node %d: Session lost during description prompt for %s (%v); using default description", nodeNumber, nf.name, err)
				description = "No description"
			} else {
				description = sanitizeControlChars(strings.TrimSpace(descInput))
			}
			if len([]rune(description)) > 60 {
				description = string([]rune(description)[:60])
			}
		}
		if description == "" {
			description = "No description"
		}

		// Re-stat file to get post-pipeline size (ZipLab may have modified it)
		if fi, statErr := os.Stat(incomingPath); statErr != nil {
			log.Printf("WARN: Node %d: Failed to stat %s after pipeline: %v (using original size)", nodeNumber, nf.name, statErr)
		} else {
			nf.size = fi.Size()
		}

		// Create and add FileRecord
		record := file.FileRecord{
			ID:            uuid.New(),
			AreaID:        currentAreaID,
			Filename:      nf.name,
			Description:   description,
			Size:          nf.size,
			UploadedAt:    time.Now(),
			UploadedBy:    currentUser.Handle,
			DownloadCount: 0,
		}

		// Move file from incoming to target directory
		finalPath := filepath.Join(targetDir, nf.name)
		if moveErr := os.Rename(incomingPath, finalPath); moveErr != nil {
			log.Printf("ERROR: Node %d: Failed to move %s to area: %v", nodeNumber, nf.name, moveErr)
			errMsg := fmt.Sprintf("\r\n|01Failed to accept '%s'.|07\r\n", nf.name)
			terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(errMsg)), outputMode)
			continue
		}

		if addErr := e.FileMgr.AddFileRecord(record); addErr != nil {
			log.Printf("ERROR: Node %d: Failed to add file record for %s: %v", nodeNumber, nf.name, addErr)
			errMsg := fmt.Sprintf("\r\n|01Failed to register '%s'.|07\r\n", nf.name)
			terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(errMsg)), outputMode)
			if removeErr := os.Remove(finalPath); removeErr != nil {
				log.Printf("ERROR: Node %d: Failed to clean up orphaned file %s: %v", nodeNumber, nf.name, removeErr)
			}
			continue
		}

		log.Printf("INFO: Node %d: Added file record for %s (ID: %s)", nodeNumber, nf.name, record.ID)
		successCount++
		existingNames[strings.ToLower(nf.name)] = true
	}

	// 9. Update user upload count
	if successCount > 0 {
		currentUser.NumUploads += successCount
		if updateErr := userManager.UpdateUser(currentUser); updateErr != nil {
			log.Printf("ERROR: Node %d: Failed to update user upload count: %v", nodeNumber, updateErr)
		}
	}

	// 10. Display summary
	summary := fmt.Sprintf("\r\n|15Upload complete.|07 Added: |15%d|07", successCount)
	if duplicateCount > 0 {
		summary += fmt.Sprintf("  Rejected (duplicate): |09%d|07", duplicateCount)
	}
	summary += "\r\n"
	terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(summary)), outputMode)
	time.Sleep(2 * time.Second)

	return nil
}

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
func runListFilesExtended(e *MenuExecutor, s ssh.Session, terminal *term.Terminal, userManager *user.UserMgr, currentUser *user.User, nodeNumber int, sessionStartTime time.Time, args string, outputMode ansi.OutputMode, termWidth int, termHeight int) (*user.User, string, error) {
	return runListFiles(e, s, terminal, userManager, currentUser, nodeNumber, sessionStartTime, "EXTENDED", outputMode, termWidth, termHeight)
}

// runListFiles displays a paginated list of files in the current file area.
func runListFiles(e *MenuExecutor, s ssh.Session, terminal *term.Terminal, userManager *user.UserMgr, currentUser *user.User, nodeNumber int, sessionStartTime time.Time, args string, outputMode ansi.OutputMode, termWidth int, termHeight int) (*user.User, string, error) {
	extendedMode := false
	for _, tok := range strings.Fields(args) {
		if strings.EqualFold(tok, "EXTENDED") {
			extendedMode = true
			break
		}
	}
	log.Printf("DEBUG: Node %d: Running LISTFILES (extended=%v)", nodeNumber, extendedMode)

	// 1. Check User and Current File Area
	if currentUser == nil {
		log.Printf("WARN: Node %d: LISTFILES called without logged in user.", nodeNumber)
		msg := "\r\n|01Error: You must be logged in to list files.|07\r\n"
		wErr := terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
		if wErr != nil { /* Log? */
		}
		time.Sleep(1 * time.Second)
		return nil, "", nil // Return to menu
	}

	// Get current file area from user session
	currentAreaID := currentUser.CurrentFileAreaID
	currentAreaTag := currentUser.CurrentFileAreaTag

	if currentAreaID <= 0 {
		log.Printf("WARN: Node %d: User %s has no current file area selected.", nodeNumber, currentUser.Handle)
		msg := "\r\n|01Error: No file area selected.|07\r\n"
		wErr := terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
		if wErr != nil { /* Log? */
		}
		time.Sleep(1 * time.Second)
		return nil, "", nil // Return to menu
	}

	log.Printf("INFO: Node %d: User %s listing files for Area ID %d (%s)", nodeNumber, currentUser.Handle, currentAreaID, currentAreaTag)

	// Check Read ACS for the file area
	area, exists := e.FileMgr.GetAreaByID(currentAreaID)
	if !exists || !checkACS(area.ACSList, currentUser, s, terminal, sessionStartTime) {
		log.Printf("WARN: Node %d: User %s denied read access to file area %d (%s) due to ACS '%s'", nodeNumber, currentUser.Handle, currentAreaID, currentAreaTag, area.ACSList)
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
			log.Printf("ERROR: Node %d: Failed to load FILELIST.BOT template: %v", nodeNumber, errBot)
			msg := "\r\n|01Error loading File List screen templates.|07\r\n"
			wErr := terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
			if wErr != nil { /* Log? */
			}
			time.Sleep(1 * time.Second)
			return nil, "", fmt.Errorf("failed loading FILELIST templates")
		}
	}

	if errTop != nil || errMid != nil {
		log.Printf("ERROR: Node %d: Failed to load FILELIST template files: TOP(%v), MID(%v)", nodeNumber, errTop, errMid)
		msg := "\r\n|01Error loading File List screen templates.|07\r\n"
		wErr := terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
		if wErr != nil { /* Log? */
		}
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
	log.Printf("DEBUG: Node %d: TermHeight=%d, FixedLines=%d, FilesPerPage=%d", nodeNumber, termHeight, fixedLines, filesPerPage)

	// --- Get Total File Count ---
	// TODO: Implement GetFileCountForArea in FileManager
	totalFiles, err := e.FileMgr.GetFileCountForArea(currentAreaID)
	if err != nil {
		log.Printf("ERROR: Node %d: Failed to get file count for area %d: %v", nodeNumber, currentAreaID, err)
		msg := fmt.Sprintf("\r\n|01Error retrieving file list for area '%s'.|07\r\n", currentAreaTag)
		wErr := terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
		if wErr != nil { /* Log? */
		}
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
		// TODO: Implement GetFilesForAreaPaginated in FileManager
		filesOnPage, err = e.FileMgr.GetFilesForAreaPaginated(currentAreaID, currentPage, filesPerPage)
		if err != nil {
			log.Printf("ERROR: Node %d: Failed to get files for area %d, page %d: %v", nodeNumber, currentAreaID, currentPage, err)
			msg := fmt.Sprintf("\r\n|01Error retrieving file list page for area '%s'.|07\r\n", currentAreaTag)
			wErr := terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
			if wErr != nil { /* Log? */
			}
			time.Sleep(1 * time.Second)
			return nil, "", fmt.Errorf("failed getting file page: %w", err)
		}
	} else {
		filesOnPage = []file.FileRecord{} // Ensure empty slice if no files
	}

	// Load optional BAR files for file listing lightbar.
	cmdBarOptions, cmdBarErr := loadBarFile("FILELISTCMD", e)
	if cmdBarErr != nil {
		log.Printf("WARN: Node %d: Failed to load FILELISTCMD.BAR: %v", nodeNumber, cmdBarErr)
	}
	hiBarOptions, hiBarErr := loadBarFile("FILELISTHI", e)
	if hiBarErr != nil {
		log.Printf("WARN: Node %d: Failed to load FILELISTHI.BAR: %v", nodeNumber, hiBarErr)
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
			log.Printf("ERROR: Node %d: Failed clearing screen for LISTFILES: %v", nodeNumber, writeErr)
		}

		// 4.2 Display Top Template (process @FCONFPATH@, @FTOTAL@, @FPAGE@ placeholders per page)
		topRendered := ansi.ReplacePipeCodes(processFileListPlaceholders(topTemplateBytes, currentPage, totalPages, totalFiles, fconfpath))
		wErr := terminalio.WriteProcessedBytes(terminal, topRendered, outputMode)
		if wErr != nil {
			log.Printf("ERROR: Node %d: Failed writing LISTFILES top template: %v", nodeNumber, wErr)
		}
		wErr = terminalio.WriteProcessedBytes(terminal, []byte("\r\n"), outputMode)
		if wErr != nil {
			log.Printf("ERROR: Node %d: Failed writing CRLF after LISTFILES top template: %v", nodeNumber, wErr)
		}

		// 4.3 Display Files on Current Page (using MID template)
		if len(filesOnPage) == 0 {
			// Display "No files in this area" message
			// TODO: Use a configurable string?
			noFilesMsg := "\r\n|07   No files in this area.   \r\n"
			wErr = terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(noFilesMsg)), outputMode)
			if wErr != nil { /* Log? */
			}
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
					log.Printf("ERROR: Node %d: Failed writing file list line %d: %v", nodeNumber, i, wErr)
				}
				wErr = terminalio.WriteProcessedBytes(terminal, []byte("\r\n"), outputMode)
				if wErr != nil {
					log.Printf("ERROR: Node %d: Failed writing CRLF after file list line %d: %v", nodeNumber, i, wErr)
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
			log.Printf("ERROR: Node %d: Failed writing LISTFILES bottom template: %v", nodeNumber, wErr)
			// Handle error
		}

		// 4.5 Display Prompt (Use a standard file list prompt or configure one)
		// TODO: Use configurable prompt string
		prompt := "\r\n|07File Cmd (|15N|07=Next, |15P|07=Prev, |15#|07=Mark, |15V|07=View, |15D|07=Download, |15U|07=Upload, |15Q|07=Quit): |15"
		wErr = terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(prompt)), outputMode)
		if wErr != nil {
			// Handle error
		}

		// 4.6 Read User Input
		input, err := readLineFromSessionIH(s, terminal)
		if err != nil {
			if errors.Is(err, io.EOF) {
				log.Printf("INFO: Node %d: User disconnected during LISTFILES.", nodeNumber)
				return nil, "LOGOFF", io.EOF
			}
			log.Printf("ERROR: Node %d: Failed reading LISTFILES input: %v", nodeNumber, err)
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
					log.Printf("ERROR: Node %d: Failed to get files for page %d: %v", nodeNumber, currentPage, err)
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
					log.Printf("ERROR: Node %d: Failed to get files for page %d: %v", nodeNumber, currentPage, err)
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
			log.Printf("DEBUG: Node %d: User quit LISTFILES.", nodeNumber)
			return nil, "", nil // Return to FILEM menu
		case "D": // Download marked files
			log.Printf("DEBUG: Node %d: User %s initiated Download command in area %d.", nodeNumber, currentUser.Handle, currentAreaID)

			// 1. Check if any files are marked
			if len(currentUser.TaggedFileIDs) == 0 {
				msg := "\r\n|07No files marked for download. Use |15#|07 to mark files.|07\r\n"
				wErr := terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
				if wErr != nil { /* Log? */
				}
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
					log.Printf("INFO: Node %d: User disconnected during download confirmation.", nodeNumber)
					return nil, "LOGOFF", io.EOF
				}
				log.Printf("ERROR: Node %d: Error getting download confirmation: %v", nodeNumber, err)
				msg := "\r\n|01Error during confirmation.|07\r\n"
				terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
				time.Sleep(1 * time.Second)
				continue // Back to file list
			}

			if !proceed {
				log.Printf("DEBUG: Node %d: User cancelled download.", nodeNumber)
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
				log.Printf("ERROR: Node %d: Protocol selection error: %v", nodeNumber, protoErr)
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
					log.Printf("ERROR: Node %d: Failed to get path for file ID %s: %v", nodeNumber, fileID, pathErr)
					failCount++
					continue
				}
				if _, statErr := os.Stat(filePath); statErr != nil {
					log.Printf("ERROR: Node %d: File %s (ID %s) not on disk: %v", nodeNumber, filePath, fileID, statErr)
					failCount++
					continue
				}
				resolved = append(resolved, dlEntry{id: fileID, path: filePath, name: filepath.Base(filePath)})
			}

			if len(resolved) == 0 {
				log.Printf("WARN: Node %d: No valid file paths found for tagged files.", nodeNumber)
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
			log.Printf("DEBUG: Node %d: Clearing %d tagged file IDs for user %s.", nodeNumber, len(currentUser.TaggedFileIDs), currentUser.Handle)
			currentUser.TaggedFileIDs = nil // Clear the list
			currentUser.NumDownloads += successCount
			if err := userManager.UpdateUser(currentUser); err != nil {
				log.Printf("ERROR: Node %d: Failed to save user data after download attempt: %v", nodeNumber, err)
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
			log.Printf("DEBUG: Node %d: Upload command entered for area %d (%s)", nodeNumber, currentAreaID, currentAreaTag)
			uploadErr := e.runUploadFiles(s, terminal, currentUser, userManager, currentAreaID, currentAreaTag, outputMode, nodeNumber, sessionStartTime)
			if uploadErr != nil {
				if errors.Is(uploadErr, io.EOF) {
					return nil, "LOGOFF", uploadErr
				}
				log.Printf("ERROR: Node %d: Upload failed: %v", nodeNumber, uploadErr)
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
			log.Printf("DEBUG: Node %d: View command entered in file list", nodeNumber)
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
					log.Printf("ERROR: Node %d: Failed to get path for file %s: %v", nodeNumber, fileToView.ID, pathErr)
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
			log.Printf("DEBUG: Node %d: Area Change command entered (Handled by menu)", nodeNumber)
			msg := "\r\n|01Use menu options to change area.|07\r\n"
			wErr := terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
			if wErr != nil { /* Log? */
			}
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
						log.Printf("DEBUG: Node %d: User %s tagged file #%d (ID: %s)", nodeNumber, currentUser.Handle, fileNumToTag, fileToToggle.ID)
					} else {
						// File was tagged, so we removed it (untagged)
						log.Printf("DEBUG: Node %d: User %s untagged file #%d (ID: %s)", nodeNumber, currentUser.Handle, fileNumToTag, fileToToggle.ID)
					}
					currentUser.TaggedFileIDs = newTaggedIDs
					// No page change needed, loop will redraw with updated marks
				} else {
					// Invalid file number for current page
					log.Printf("DEBUG: Node %d: Invalid file number entered: %d", nodeNumber, fileNumToTag)
					// Optional: Add user feedback message
				}
			} else {
				// Input was not N, P, Q, D, U, V, A, or a valid number - Invalid command
				log.Printf("DEBUG: Node %d: Invalid command entered in LISTFILES: %s", nodeNumber, upperInput)
				// Optional: Add user feedback message
			}
		} // end switch
	} // end for loop

	// Should not be reached normally
	// return nil, "", nil
}

// displayFileAreaList is an internal helper to display the list of accessible file areas.
// It does not include a pause prompt.
func displayFileAreaList(e *MenuExecutor, s ssh.Session, terminal *term.Terminal, currentUser *user.User, outputMode ansi.OutputMode, nodeNumber int, sessionStartTime time.Time) error {
	log.Printf("DEBUG: Node %d: Displaying file area list (helper)", nodeNumber)

	// 1. Define Template filenames and paths
	topTemplateFilename := "FILEAREA.TOP"
	midTemplateFilename := "FILEAREA.MID"
	botTemplateFilename := "FILEAREA.BOT"
	templateDir := filepath.Join(e.MenuSetPath, "templates")
	topTemplatePath := filepath.Join(templateDir, topTemplateFilename)
	midTemplatePath := filepath.Join(templateDir, midTemplateFilename)
	botTemplatePath := filepath.Join(templateDir, botTemplateFilename)

	// 2. Load Template Files
	topTemplateBytes, errTop := readTemplateFile(topTemplatePath)
	midTemplateBytes, errMid := readTemplateFile(midTemplatePath)
	botTemplateBytes, errBot := readTemplateFile(botTemplatePath)

	if errTop != nil || errMid != nil || errBot != nil {
		log.Printf("ERROR: Node %d: Failed to load one or more FILEAREA template files: TOP(%v), MID(%v), BOT(%v)", nodeNumber, errTop, errMid, errBot)
		// Display error message to terminal
		msg := "\r\n|01Error loading File Area screen templates.|07\r\n"
		wErr := terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
		if wErr != nil { /* Log? */
		}
		time.Sleep(1 * time.Second)
		return fmt.Errorf("failed loading FILEAREA templates")
	}

	// 3. Process Pipe Codes in Templates FIRST
	processedTopTemplate := ansi.ReplacePipeCodes(topTemplateBytes)
	processedMidTemplate := string(ansi.ReplacePipeCodes(midTemplateBytes))
	processedBotTemplate := ansi.ReplacePipeCodes(botTemplateBytes)

	// Conference header template (optional)
	confHdrBytes, errConf := readTemplateFile(filepath.Join(templateDir, "FILECONF.HDR"))
	confHdrTemplate := ""
	if errConf == nil {
		confHdrTemplate = string(ansi.ReplacePipeCodes(confHdrBytes))
	}

	// 4. Get file area list data and group by conference
	areas := e.FileMgr.ListAreas()

	// Build conference groups: conferenceID -> []file.FileArea (ACS-filtered)
	groups := make(map[int][]file.FileArea)
	confIDs := make(map[int]bool)
	for _, area := range areas {
		if !checkACS(area.ACSList, currentUser, s, terminal, sessionStartTime) {
			log.Printf("TRACE: Node %d: User %s denied list access to file area %d (%s) due to ACS '%s'", nodeNumber, currentUser.Handle, area.ID, area.Tag, area.ACSList)
			continue
		}
		groups[area.ConferenceID] = append(groups[area.ConferenceID], area)
		confIDs[area.ConferenceID] = true
	}

	// Sort conference IDs (0/ungrouped first)
	var sortedConfIDs []int
	if e.ConferenceMgr != nil {
		sortedConfIDs = e.ConferenceMgr.GetSortedConferenceIDs(confIDs)
	} else {
		for cid := range confIDs {
			sortedConfIDs = append(sortedConfIDs, cid)
		}
		sort.Ints(sortedConfIDs)
	}

	// 5. Build the output string using processed templates and data
	var outputBuffer bytes.Buffer
	outputBuffer.Write(processedTopTemplate)

	areasDisplayed := 0
	for _, cid := range sortedConfIDs {
		areasInConf := groups[cid]
		if len(areasInConf) == 0 {
			continue
		}

		// Check conference ACS and write header
		if cid != 0 && e.ConferenceMgr != nil {
			conf, found := e.ConferenceMgr.GetByID(cid)
			if found && !checkACS(conf.ACS, currentUser, s, terminal, sessionStartTime) {
				continue
			}
			if found && confHdrTemplate != "" {
				hdr := confHdrTemplate
				hdr = strings.ReplaceAll(hdr, "^CN", conf.Name)
				hdr = strings.ReplaceAll(hdr, "^CT", conf.Tag)
				hdr = strings.ReplaceAll(hdr, "^CD", conf.Description)
				hdr = strings.ReplaceAll(hdr, "^CI", strconv.Itoa(conf.ID))
				outputBuffer.WriteString(hdr)
			}
		}

		for _, area := range areasInConf {
			line := processedMidTemplate
			name := string(ansi.ReplacePipeCodes([]byte(area.Name)))
			desc := string(ansi.ReplacePipeCodes([]byte(area.Description)))
			idStr := strconv.Itoa(area.ID)
			tag := string(ansi.ReplacePipeCodes([]byte(area.Tag)))
			fileCount, countErr := e.FileMgr.GetFileCountForArea(area.ID)
			if countErr != nil {
				log.Printf("WARN: Node %d: Failed getting file count for area %d (%s): %v", nodeNumber, area.ID, area.Tag, countErr)
				fileCount = 0
			}
			fileCountStr := strconv.Itoa(fileCount)

			line = strings.ReplaceAll(line, "^ID", idStr)
			line = strings.ReplaceAll(line, "^TAG", tag)
			line = strings.ReplaceAll(line, "^NA", name)
			line = strings.ReplaceAll(line, "^DS", desc)
			line = strings.ReplaceAll(line, "^NF", fileCountStr)

			outputBuffer.WriteString(line)
			areasDisplayed++
		}
	}

	if areasDisplayed == 0 {
		log.Printf("DEBUG: Node %d: No accessible file areas to display for user %s.", nodeNumber, currentUser.Handle)
		outputBuffer.WriteString("\r\n|07   No accessible file areas found.   \r\n")
	}

	outputBuffer.Write(processedBotTemplate)

	// 6. Clear screen and display the assembled content
	writeErr := terminalio.WriteProcessedBytes(terminal, []byte(ansi.ClearScreen()), outputMode)
	if writeErr != nil {
		log.Printf("ERROR: Node %d: Failed clearing screen for file area list: %v", nodeNumber, writeErr)
		// Try to continue anyway?
	}

	processedContent := outputBuffer.Bytes()
	// For CP437 mode, write raw bytes directly to avoid UTF-8 false positives
	var wErr error
	if outputMode == ansi.OutputModeCP437 {
		_, wErr = terminal.Write(processedContent)
	} else {
		wErr = terminalio.WriteProcessedBytes(terminal, processedContent, outputMode)
	}
	if wErr != nil {
		log.Printf("ERROR: Node %d: Failed writing file area list output: %v", nodeNumber, wErr)
		return wErr // Return the error from writing
	}

	return nil // Success
}

// runListFileAreas displays a list of file areas using templates.
func runListFileAreas(e *MenuExecutor, s ssh.Session, terminal *term.Terminal, userManager *user.UserMgr, currentUser *user.User, nodeNumber int, sessionStartTime time.Time, args string, outputMode ansi.OutputMode, termWidth int, termHeight int) (*user.User, string, error) {
	log.Printf("DEBUG: Node %d: Running LISTFILEAR", nodeNumber)

	if currentUser == nil {
		log.Printf("WARN: Node %d: LISTFILEAR called without logged in user.", nodeNumber)
		msg := "\r\n|01Error: You must be logged in to list file areas.|07\r\n"
		wErr := terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
		if wErr != nil { /* Log? */
		}
		time.Sleep(1 * time.Second)
		return nil, "", nil
	}

	// Call the helper to display the list
	if err := displayFileAreaList(e, s, terminal, currentUser, outputMode, nodeNumber, sessionStartTime); err != nil {
		// Error already logged by helper, maybe add context?
		log.Printf("ERROR: Node %d: Error occurred during displayFileAreaList from runListFileAreas: %v", nodeNumber, err)
		// Need to decide if we still pause or just return.
		// For now, return the error to prevent pause on failed display.
		return nil, "", err
	}

	// Wait for Enter using configured PauseString (centered)
	pausePrompt := e.LoadedStrings.PauseString
	if pausePrompt == "" {
		pausePrompt = "\r\n|07Press |15[ENTER]|07 to continue... " // Fallback
	}

	log.Printf("DEBUG: Node %d: Displaying LISTFILEAR pause prompt (centered)", nodeNumber)
	err := writeCenteredPausePrompt(s, terminal, pausePrompt, outputMode, termWidth, termHeight)
	if err != nil {
		if errors.Is(err, io.EOF) {
			log.Printf("INFO: Node %d: User disconnected during LISTFILEAR pause.", nodeNumber)
			return nil, "LOGOFF", io.EOF
		}
		log.Printf("ERROR: Node %d: Failed during LISTFILEAR pause: %v", nodeNumber, err)
		return nil, "", err
	}

	return nil, "", nil // Success, return to current menu (FILEM)
}

// runSelectFileAreaDispatch checks the user/server fileListingMode setting and
// dispatches to either the lightbar or classic text-mode file area selector.
func runSelectFileAreaDispatch(e *MenuExecutor, s ssh.Session, terminal *term.Terminal, userManager *user.UserMgr, currentUser *user.User, nodeNumber int, sessionStartTime time.Time, args string, outputMode ansi.OutputMode, termWidth int, termHeight int) (*user.User, string, error) {
	mode := ""
	if currentUser != nil {
		mode = currentUser.FileListingMode
	}
	if mode == "" {
		mode = e.ServerCfg.FileListingMode
	}
	if strings.EqualFold(mode, "classic") {
		return runSelectFileArea(e, s, terminal, userManager, currentUser, nodeNumber, sessionStartTime, args, outputMode, termWidth, termHeight)
	}
	return runSelectFileAreaLightbar(e, s, terminal, userManager, currentUser, nodeNumber, sessionStartTime, args, outputMode, termWidth, termHeight)
}

// runSelectFileArea prompts the user for a file area tag and changes the current user's
// active file area if valid and accessible (classic text-mode).
func runSelectFileArea(e *MenuExecutor, s ssh.Session, terminal *term.Terminal, userManager *user.UserMgr, currentUser *user.User, nodeNumber int, sessionStartTime time.Time, args string, outputMode ansi.OutputMode, termWidth int, termHeight int) (*user.User, string, error) {
	log.Printf("DEBUG: Node %d: Running SELECTFILEAREA", nodeNumber)

	if currentUser == nil {
		log.Printf("WARN: Node %d: SELECTFILEAREA called without logged in user.", nodeNumber)
		msg := "\r\n|01Error: You must be logged in to select a file area.|07\r\n"
		wErr := terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
		if wErr != nil { /* Log? */
		}
		time.Sleep(1 * time.Second)
		return nil, "", nil
	}

	// --- Display the list first --- <--- MODIFIED
	if err := displayFileAreaList(e, s, terminal, currentUser, outputMode, nodeNumber, sessionStartTime); err != nil {
		log.Printf("ERROR: Node %d: Failed displaying file area list in SELECTFILEAREA: %v", nodeNumber, err)
		// Don't proceed if the list couldn't be displayed
		return currentUser, "", err // Return error
	}
	// Add a newline between list and prompt
	terminalio.WriteProcessedBytes(terminal, []byte("\r\n"), outputMode)

	// Prompt for area tag
	prompt := e.LoadedStrings.ChangeFileAreaStr
	if prompt == "" {
		prompt = "|07File Area Tag (?=List, Q=Quit): |15"
	}
	renderedPrompt := ansi.ReplacePipeCodes([]byte(prompt))
	curUpClear := "\x1b[A\r\x1b[2K"

	// Show initial prompt
	terminalio.WriteProcessedBytes(terminal, renderedPrompt, outputMode)

	for {
		inputTag, err := readLineFromSessionIH(s, terminal)
		if err != nil {
			if errors.Is(err, io.EOF) {
				log.Printf("INFO: Node %d: User disconnected during SELECTFILEAREA prompt.", nodeNumber)
				return nil, "LOGOFF", io.EOF
			}
			log.Printf("ERROR: Node %d: Error reading input for SELECTFILEAREA: %v", nodeNumber, err)
			return currentUser, "", err
		}

		inputClean := strings.TrimSpace(inputTag)
		upperInput := strings.ToUpper(inputClean)

		if upperInput == "Q" {
			log.Printf("DEBUG: Node %d: SELECTFILEAREA aborted by user.", nodeNumber)
			terminalio.WriteProcessedBytes(terminal, []byte("\r\n"), outputMode)
			return currentUser, "", nil
		}
		if upperInput == "" {
			terminalio.WriteProcessedBytes(terminal, renderedPrompt, outputMode)
			continue
		}

		if upperInput == "?" {
			log.Printf("DEBUG: Node %d: User requested file area list again from SELECTFILEAREA.", nodeNumber)
			if listErr := displayFileAreaList(e, s, terminal, currentUser, outputMode, nodeNumber, sessionStartTime); listErr != nil {
				log.Printf("ERROR: Node %d: Failed redisplaying file area list: %v", nodeNumber, listErr)
			}
			terminalio.WriteProcessedBytes(terminal, []byte("\r\n"), outputMode)
			terminalio.WriteProcessedBytes(terminal, renderedPrompt, outputMode)
			continue
		}

		// Try parsing as ID first, then fallback to Tag
		var area *file.FileArea
		var exists bool
		matched := false

		if inputID, parseErr := strconv.Atoi(inputClean); parseErr == nil {
			log.Printf("DEBUG: Node %d: User input '%s' parsed as ID %d. Looking up by ID.", nodeNumber, inputClean, inputID)
			area, exists = e.FileMgr.GetAreaByID(inputID)
			if exists {
				matched = true
			}
		}
		if !matched {
			log.Printf("DEBUG: Node %d: User input '%s' not an ID. Looking up by Tag '%s'.", nodeNumber, inputClean, upperInput)
			area, exists = e.FileMgr.GetAreaByTag(upperInput)
			if exists {
				matched = true
			}
		}

		if !matched {
			terminalio.WriteProcessedBytes(terminal, []byte(curUpClear), outputMode)
			msg := fmt.Sprintf("|01Invalid file area '%s'!|07", inputClean)
			terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
			time.Sleep(1 * time.Second)
			terminalio.WriteProcessedBytes(terminal, []byte("\r\x1b[2K"), outputMode)
			terminalio.WriteProcessedBytes(terminal, renderedPrompt, outputMode)
			continue
		}

		// Check ACSList permission
		if !checkACS(area.ACSList, currentUser, s, terminal, sessionStartTime) {
			log.Printf("WARN: Node %d: User %s denied access to file area %d ('%s') due to ACS '%s'", nodeNumber, currentUser.Handle, area.ID, area.Tag, area.ACSList)
			terminalio.WriteProcessedBytes(terminal, []byte(curUpClear), outputMode)
			msg := fmt.Sprintf("|01Access denied to file area '%s'!|07", area.Tag)
			terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
			time.Sleep(1 * time.Second)
			terminalio.WriteProcessedBytes(terminal, []byte("\r\x1b[2K"), outputMode)
			terminalio.WriteProcessedBytes(terminal, renderedPrompt, outputMode)
			continue
		}

		// Found a valid, accessible area — update user state
		currentUser.CurrentFileAreaID = area.ID
		currentUser.CurrentFileAreaTag = area.Tag
		e.setUserFileConference(currentUser, area.ConferenceID)

		// Save the user state
		if saveErr := userManager.UpdateUser(currentUser); saveErr != nil {
			log.Printf("ERROR: Node %d: Failed to save user data after updating file area: %v", nodeNumber, saveErr)
			currentUser.CurrentFileAreaID = area.ID // revert not needed, just don't show success
			msg := "\r\n|01Error: Could not save area selection.|07\r\n"
			terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
			time.Sleep(1 * time.Second)
			terminalio.WriteProcessedBytes(terminal, renderedPrompt, outputMode)
			continue
		}

		log.Printf("INFO: Node %d: User %s changed file area to ID %d ('%s')", nodeNumber, currentUser.Handle, area.ID, area.Tag)
		msg := fmt.Sprintf("\r\n|07Current file area set to: |15%s|07\r\n", area.Name)
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
		time.Sleep(1 * time.Second)

		return currentUser, "", nil
	}
}
