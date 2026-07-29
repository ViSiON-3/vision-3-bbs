package menu

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/terminalio"
	"github.com/ViSiON-3/vision-3-bbs/internal/transfer"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
	"github.com/gliderlabs/ssh"
	"golang.org/x/term"
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
			slog.Warn("skipping symlink", "name", entry.Name())
			continue
		}
		info, err := entry.Info()
		if err != nil {
			slog.Warn("failed to get file info", "name", entry.Name(), "error", err)
			continue
		}
		if !info.Mode().IsRegular() {
			slog.Warn("skipping non-regular file", "name", entry.Name())
			continue
		}
		files[entry.Name()] = info.Size()
	}
	return files, nil
}

// newFileInfo describes a single file discovered in the incoming-upload
// staging directory, pending duplicate-checking and registration.
type newFileInfo struct {
	name string
	size int64
}

// runUploadFile is the RunnableFunc wrapper for UPLOADFILE menu commands.
func runUploadFile(c *cmdCtx, args string) (*user.User, string, error) {
	e := c.e
	s := c.s
	terminal := c.terminal
	userManager := c.userManager
	currentUser := c.currentUser
	nodeNumber := c.nodeNumber
	sessionStartTime := c.sessionStartTime
	outputMode := c.outputMode

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
		slog.Error("upload failed", "node", nodeNumber, "error", err)
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
	slog.Info("user starting upload", "node", nodeNumber, "handle", currentUser.Handle, "area", currentAreaID, "tag", currentAreaTag)

	// 1. Check upload ACS
	area, areaExists := e.FileMgr.GetAreaByID(currentAreaID)
	if !areaExists {
		return fmt.Errorf("file area %d not found", currentAreaID)
	}

	if area.ACSUpload != "" && !checkACS(area.ACSUpload, currentUser, s, terminal, sessionStartTime) {
		slog.Warn("user denied upload access", "node", nodeNumber, "handle", currentUser.Handle, "tag", currentAreaTag, "acs", area.ACSUpload)
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
		slog.Error("protocol selection error", "node", nodeNumber, "error", protoErr)
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
		slog.Error("failed to create incoming directory", "node", nodeNumber, "error", err)
		return fmt.Errorf("failed to create incoming directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(incomingDir) }() // best-effort temp cleanup

	// 8-9. Receive files via the selected protocol and scan for new arrivals.
	newFiles, ok := e.receiveUploadBatch(s, terminal, outputMode, nodeNumber, proto, incomingDir)
	if !ok {
		return nil
	}

	// 9. Process each new file: dedupe, register, and credit the uploader.
	successCount, duplicateCount := e.registerUploadedFiles(s, terminal, outputMode, nodeNumber, newFiles, incomingDir, targetDir, currentAreaID, currentUser, userManager, existingNames)

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

// receiveUploadBatch executes the selected transfer protocol's receive into
// incomingDir and scans the result for newly arrived files. It returns
// ok=false when the caller should return nil immediately; a message has
// already been displayed to the user in that case.
func (e *MenuExecutor) receiveUploadBatch(
	s ssh.Session,
	terminal *term.Terminal,
	outputMode ansi.OutputMode,
	nodeNumber int,
	proto transfer.ProtocolConfig,
	incomingDir string,
) ([]newFileInfo, bool) {
	// 8. Execute protocol receive into temp directory
	msg := fmt.Sprintf("\r\n|15Starting %s receive...|07\r\n", proto.Name)
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
			slog.Error("transfer binary not found", "node", nodeNumber, "error", transferErr)
			terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte("\r\n|01File transfer program not found!|07\r\n|07The SysOp needs to install the transfer binary (sexyz).\r\n|07See docs/sysop/files/file-transfer.md for setup instructions.\r\n")), outputMode)
			return nil, false
		}
		slog.Warn("receive returned error, checking for partial receives", "node", nodeNumber, "protocol", proto.Name, "error", transferErr)
	}

	// 9. Scan received files from temp directory.
	// Always scan even if transferErr != nil: rz exits non-zero when it times out
	// waiting for ZFIN, but may have already received files successfully.
	receivedFiles, err := scanDirectoryFiles(incomingDir)
	if err != nil {
		slog.Error("failed to scan incoming directory", "node", nodeNumber, "error", err)
		return nil, false
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
		return nil, false
	}

	// Sort by name for consistent ordering
	sort.Slice(newFiles, func(i, j int) bool {
		return newFiles[i].name < newFiles[j].name
	})

	slog.Info("detected new files after upload", "node", nodeNumber, "count", len(newFiles))

	return newFiles, true
}

// checkUploadDuplicates validates a received filename and rejects it if it is
// unsafe or already present in the area's file metadata. It returns
// skip=true when the caller should skip the file (a message has already been
// shown and any partial file removed); duplicate indicates whether the skip
// should be counted as a rejected duplicate.
func checkUploadDuplicates(
	nf newFileInfo,
	incomingPath string,
	existingNames map[string]bool,
	terminal *term.Terminal,
	outputMode ansi.OutputMode,
	nodeNumber int,
) (skip bool, duplicate bool) {
	// Validate filename (defense in depth — rz -r should prevent this, but be safe)
	safeName := filepath.Base(nf.name)
	if safeName != nf.name || safeName == "." || safeName == ".." || strings.Contains(nf.name, "..") || filepath.IsAbs(nf.name) {
		slog.Error("rejected unsafe filename", "node", nodeNumber, "name", nf.name)
		_ = os.Remove(incomingPath) // best-effort cleanup of rejected upload
		errMsg := fmt.Sprintf("\r\n|01'%s' rejected: invalid filename.|07\r\n", nf.name)
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(errMsg)), outputMode)
		return true, false
	}

	// Check for duplicate in metadata
	if existingNames[strings.ToLower(nf.name)] {
		slog.Warn("duplicate file rejected", "node", nodeNumber, "name", nf.name)
		_ = os.Remove(incomingPath) // best-effort cleanup of rejected upload

		dupMsg := fmt.Sprintf("\r\n|09'%s' already exists in this area. Rejected.|07\r\n", nf.name)
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(dupMsg)), outputMode)
		return true, true
	}

	return false, false
}
