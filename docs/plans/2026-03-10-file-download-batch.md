# File Download, Batch Download, Clear Batch — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Implement DOWNLOADFILE, BATCHDOWNLOAD, and CLEAR_BATCH runnables for the file transfer menu, matching ViSiON/2's loop-based download workflow.

**Architecture:** Three thin runnables in a new file `internal/menu/file_download.go`, sharing a `downloadLoop` helper. They reuse the existing transfer infrastructure (`runTransferSend`, `selectTransferProtocol`) and string constants from `LoadedStrings`. Registration in `executor.go`.

**Tech Stack:** Go, SSH terminal I/O, ZMODEM file transfer via sexyz binary.

**Branch:** `feature/file-download-batch`

---

## Task 1: Create feature branch

**Step 1: Create and switch to feature branch**

```bash
git checkout -b feature/file-download-batch
```

**Step 2: Verify branch**

```bash
git branch --show-current
```

Expected: `feature/file-download-batch`

---

## Task 2: Implement CLEAR_BATCH runnable

Simplest of the three — no transfer logic, validates the pattern.

**Files:**
- Create: `internal/menu/file_download.go`
- Modify: `internal/menu/executor.go` (registration)

**Step 1: Create `internal/menu/file_download.go` with `runClearBatch`**

```go
package menu

import (
	"errors"
	"fmt"
	"io"
	"log"
	"time"

	"github.com/gliderlabs/ssh"
	"golang.org/x/term"

	"github.com/stlalpha/vision3/internal/ansi"
	"github.com/stlalpha/vision3/internal/terminalio"
	"github.com/stlalpha/vision3/internal/user"
)

// runClearBatch clears the user's tagged file batch queue.
func runClearBatch(e *MenuExecutor, s ssh.Session, terminal *term.Terminal,
	userManager *user.UserMgr, currentUser *user.User, nodeNumber int,
	sessionStartTime time.Time, args string, outputMode ansi.OutputMode,
	termWidth int, termHeight int) (*user.User, string, error) {

	if currentUser == nil {
		return nil, "", nil
	}

	if len(currentUser.TaggedFileIDs) == 0 {
		msg := "\r\n|07Batch queue is already empty.|07\r\n"
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
		time.Sleep(1 * time.Second)
		return currentUser, "", nil
	}

	count := len(currentUser.TaggedFileIDs)
	currentUser.TaggedFileIDs = nil

	if err := userManager.UpdateUser(currentUser); err != nil {
		log.Printf("ERROR: Node %d: Failed to save user after clearing batch: %v", nodeNumber, err)
		msg := "\r\n|01Error saving changes.|07\r\n"
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
		time.Sleep(1 * time.Second)
		return currentUser, "", nil
	}

	msg := fmt.Sprintf("\r\n|07Cleared |15%d|07 file(s) from batch queue.|07\r\n", count)
	terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
	log.Printf("INFO: Node %d: User %s cleared batch queue (%d files)", nodeNumber, currentUser.Handle, count)
	time.Sleep(1 * time.Second)
	return currentUser, "", nil
}
```

**Step 2: Register in `executor.go`**

Add after the `UPLOADFILE` registration line (569):

```go
registry["CLEAR_BATCH"] = runClearBatch
```

**Step 3: Build and verify**

```bash
go build ./...
go vet ./...
```

Expected: no errors.

**Step 4: Commit**

```bash
git add internal/menu/file_download.go internal/menu/executor.go
git commit -m "feat: add CLEAR_BATCH runnable for file transfer menu"
```

---

## Task 3: Implement the download loop helper and DOWNLOADFILE runnable

**Files:**
- Modify: `internal/menu/file_download.go`

**Step 1: Add imports and the `downloadLoop` helper**

Add these imports to the existing import block:

```go
"os"
"path/filepath"
"strings"

"github.com/google/uuid"
```

Add `downloadLoop` — the shared V2-style add/continue/exit loop:

```go
// downloadLoop runs the V2-style download workflow: show prompt, let user
// (A)dd more files, e(X)it, or (CR) continue to transfer.
// If startByAdding is true, it prompts for a filename first before showing
// the download prompt (used by DOWNLOADFILE).
func (e *MenuExecutor) downloadLoop(
	s ssh.Session,
	terminal *term.Terminal,
	userManager *user.UserMgr,
	currentUser *user.User,
	nodeNumber int,
	outputMode ansi.OutputMode,
	startByAdding bool,
) (*user.User, error) {

	currentAreaID := currentUser.CurrentFileAreaID

	addFile := func() {
		prompt := e.LoadedStrings.AddBatchPrompt
		if prompt == "" {
			prompt = "\r\n|15Filename: |07"
		}
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(prompt)), outputMode)

		input, err := readLineFromSessionIH(s, terminal)
		if err != nil {
			return
		}
		filename := strings.TrimSpace(input)
		if filename == "" {
			return
		}

		// Check batch size limit
		if len(currentUser.TaggedFileIDs) >= 50 {
			fiftyMax := e.LoadedStrings.FiftyFilesMaximum
			if fiftyMax == "" {
				fiftyMax = "\r\n|01You can only tag up to 50 files!|07\r\n"
			}
			terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte("\r\n"+fiftyMax+"\r\n")), outputMode)
			return
		}

		record, err := findFileInArea(e.FileMgr, currentAreaID, filename)
		if err != nil {
			msg := e.LoadedStrings.FileNotFoundFormat
			if msg == "" {
				msg = "\r\n|01File '%s' not found in current area.|07\r\n"
			}
			terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(fmt.Sprintf(msg, filename))), outputMode)
			return
		}

		// Check if already tagged
		for _, id := range currentUser.TaggedFileIDs {
			if id == record.ID {
				alreadyMarked := e.LoadedStrings.FileAlreadyMarked
				if alreadyMarked == "" {
					alreadyMarked = "\r\n|09File is already marked!|07\r\n"
				}
				terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte("\r\n"+alreadyMarked+"\r\n")), outputMode)
				return
			}
		}

		currentUser.TaggedFileIDs = append(currentUser.TaggedFileIDs, record.ID)
		msg := fmt.Sprintf("\r\n|07Tagged |15%s|07 (%s)|07\r\n", record.Filename, formatSize(record.Size))
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
	}

	// If starting by adding (DOWNLOADFILE), prompt for a file first
	if startByAdding {
		addFile()
		// If nothing was tagged after the first prompt, just return
		if len(currentUser.TaggedFileIDs) == 0 {
			return currentUser, nil
		}
	}

	// Main download loop
	for {
		if len(currentUser.TaggedFileIDs) == 0 {
			msg := "\r\n|07No files tagged for download.|07\r\n"
			terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
			time.Sleep(1 * time.Second)
			return currentUser, nil
		}

		// Show tagged count
		countMsg := fmt.Sprintf("\r\n|07Batch: |15%d|07 file(s) tagged.|07\r\n", len(currentUser.TaggedFileIDs))
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(countMsg)), outputMode)

		// Show download prompt
		dlPrompt := e.LoadedStrings.DownloadStr
		if dlPrompt == "" {
			dlPrompt = "\r\n|07e(X)it, (A)dd, (Cr) Continue : "
		}
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(dlPrompt)), outputMode)

		input, err := readLineFromSessionIH(s, terminal)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil, io.EOF
			}
			return currentUser, nil
		}

		choice := strings.ToUpper(strings.TrimSpace(input))
		switch choice {
		case "X":
			return currentUser, nil

		case "A":
			addFile()
			continue

		case "", "C":
			// Continue to download
			goto doTransfer

		default:
			continue
		}
	}

doTransfer:
	// Resolve file paths
	filePaths := make([]string, 0, len(currentUser.TaggedFileIDs))
	fileIDs := make([]uuid.UUID, 0, len(currentUser.TaggedFileIDs))
	failCount := 0

	for _, fileID := range currentUser.TaggedFileIDs {
		fp, err := e.FileMgr.GetFilePath(fileID)
		if err != nil {
			log.Printf("ERROR: Node %d: Failed to get path for file ID %s: %v", nodeNumber, fileID, err)
			failCount++
			continue
		}
		if _, statErr := os.Stat(fp); statErr != nil {
			log.Printf("ERROR: Node %d: File %s not found on disk: %v", nodeNumber, fp, statErr)
			failCount++
			continue
		}
		filePaths = append(filePaths, fp)
		fileIDs = append(fileIDs, fileID)
	}

	if len(filePaths) == 0 {
		msg := "\r\n|01Could not find any of the marked files on the server.|07\r\n"
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
		time.Sleep(2 * time.Second)
		currentUser.TaggedFileIDs = nil
		_ = userManager.UpdateUser(currentUser)
		return currentUser, nil
	}

	// Select protocol
	proto, protoOK, protoErr := e.selectTransferProtocol(s, terminal, outputMode)
	if protoErr != nil {
		if errors.Is(protoErr, io.EOF) {
			return nil, io.EOF
		}
		log.Printf("ERROR: Node %d: Protocol selection error: %v", nodeNumber, protoErr)
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte("\r\n|01Error: No transfer protocols configured.|07\r\n")), outputMode)
		time.Sleep(2 * time.Second)
		return currentUser, nil
	}
	if !protoOK {
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte("\r\n|07Download cancelled.|07\r\n")), outputMode)
		return currentUser, nil
	}

	// Execute transfer
	log.Printf("INFO: Node %d: User %s starting download of %d files.", nodeNumber, currentUser.Handle, len(filePaths))
	terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte("\r\n|07Preparing download...\r\n")), outputMode)
	time.Sleep(500 * time.Millisecond)

	successCount, dlFailCount := e.runTransferSend(s, terminal, proto, filePaths, fileIDs, outputMode, nodeNumber)
	failCount += dlFailCount

	// Clear tags and update stats
	currentUser.TaggedFileIDs = nil
	currentUser.NumDownloads += successCount
	if err := userManager.UpdateUser(currentUser); err != nil {
		log.Printf("ERROR: Node %d: Failed to save user after download: %v", nodeNumber, err)
	}

	statusMsg := fmt.Sprintf("\r\n|07Download finished. Success: |15%d|07, Failed: |15%d|07.|07\r\n", successCount, failCount)
	terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(statusMsg)), outputMode)
	time.Sleep(2 * time.Second)

	return currentUser, nil
}
```

**Step 2: Add the `runDownloadFile` runnable**

```go
// runDownloadFile handles the DOWNLOADFILE menu command (D key).
// Matches ViSiON/2 workflow: prompt for filename, add to batch, then download loop.
func runDownloadFile(e *MenuExecutor, s ssh.Session, terminal *term.Terminal,
	userManager *user.UserMgr, currentUser *user.User, nodeNumber int,
	sessionStartTime time.Time, args string, outputMode ansi.OutputMode,
	termWidth int, termHeight int) (*user.User, string, error) {

	if currentUser == nil {
		return nil, "", nil
	}

	currentAreaID := currentUser.CurrentFileAreaID
	if currentAreaID <= 0 {
		msg := e.LoadedStrings.FileNoAreaSelected
		if msg == "" {
			msg = "\r\n|01No file area selected.|07\r\n"
		}
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
		time.Sleep(1 * time.Second)
		return currentUser, "", nil
	}

	// Check download ACS
	area, areaExists := e.FileMgr.GetAreaByID(currentAreaID)
	if !areaExists {
		return currentUser, "", nil
	}
	if area.ACSDownload != "" && !checkACS(area.ACSDownload, currentUser, s, terminal, sessionStartTime) {
		msg := e.LoadedStrings.YouCantDownloadHere
		if msg == "" {
			msg = "\r\n|01You can't download here!|07\r\n"
		}
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte("\r\n"+msg+"\r\n")), outputMode)
		time.Sleep(1 * time.Second)
		return currentUser, "", nil
	}

	updatedUser, err := e.downloadLoop(s, terminal, userManager, currentUser, nodeNumber, outputMode, true)
	if err != nil {
		if errors.Is(err, io.EOF) {
			return nil, "LOGOFF", io.EOF
		}
		return currentUser, "", err
	}
	if updatedUser == nil {
		return nil, "LOGOFF", io.EOF
	}

	return updatedUser, "", nil
}
```

**Step 3: Build and verify**

```bash
go build ./...
go vet ./...
```

**Step 4: Commit**

```bash
git add internal/menu/file_download.go
git commit -m "feat: add DOWNLOADFILE runnable with V2-style download loop"
```

---

## Task 4: Implement BATCHDOWNLOAD runnable

**Files:**
- Modify: `internal/menu/file_download.go`
- Modify: `internal/menu/executor.go` (registration)

**Step 1: Add `runBatchDownload` to `file_download.go`**

```go
// runBatchDownload handles the BATCHDOWNLOAD menu command (B key).
// Shows tagged files and enters the V2-style download loop.
func runBatchDownload(e *MenuExecutor, s ssh.Session, terminal *term.Terminal,
	userManager *user.UserMgr, currentUser *user.User, nodeNumber int,
	sessionStartTime time.Time, args string, outputMode ansi.OutputMode,
	termWidth int, termHeight int) (*user.User, string, error) {

	if currentUser == nil {
		return nil, "", nil
	}

	if len(currentUser.TaggedFileIDs) == 0 {
		msg := "\r\n|07No files tagged for download.|07\r\n"
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
		time.Sleep(1 * time.Second)
		return currentUser, "", nil
	}

	updatedUser, err := e.downloadLoop(s, terminal, userManager, currentUser, nodeNumber, outputMode, false)
	if err != nil {
		if errors.Is(err, io.EOF) {
			return nil, "LOGOFF", io.EOF
		}
		return currentUser, "", err
	}
	if updatedUser == nil {
		return nil, "LOGOFF", io.EOF
	}

	return updatedUser, "", nil
}
```

**Step 2: Register all three in `executor.go`**

Add after the `UPLOADFILE` registration line (569):

```go
registry["DOWNLOADFILE"] = runDownloadFile
registry["BATCHDOWNLOAD"] = runBatchDownload
registry["CLEAR_BATCH"] = runClearBatch
```

Note: `CLEAR_BATCH` was registered in Task 2. Move it here so all three are together.

**Step 3: Build and verify**

```bash
go build ./...
go vet ./...
```

**Step 4: Commit**

```bash
git add internal/menu/file_download.go internal/menu/executor.go
git commit -m "feat: add BATCHDOWNLOAD runnable, register all download commands"
```

---

## Task 5: Build, test, and deploy

**Step 1: Run full test suite**

```bash
go test ./internal/menu/... -v -count=1 2>&1 | tail -30
go test ./internal/file/... -v -count=1 2>&1 | tail -20
```

Expected: all tests pass.

**Step 2: Cross-compile for deployment target**

```bash
GOOS=linux GOARCH=amd64 go build -o /tmp/vision3-test ./cmd/vision3/
```

Expected: binary built successfully.

**Step 3: Deploy to test server**

```bash
scp /tmp/vision3-test felonius@pms.vision3bbs.com:/opt/PiRATEMiNDSTATiON/vision3.new
ssh felonius@pms.vision3bbs.com "cd /opt/PiRATEMiNDSTATiON && sudo systemctl stop vision3 && cp vision3 vision3.bak && mv vision3.new vision3 && chmod +x vision3 && sudo systemctl start vision3"
```

**Step 4: Manual testing on pms.vision3bbs.com:37337**

1. SSH in, go to File Menu
2. Press `-` — should show "Batch queue is already empty"
3. Go to file listing (L), tag some files with Space, press Q
4. Press `-` — should clear the batch and show count
5. Tag files again via listing
6. Press `B` — should show batch count and download prompt
7. Press `A` — should prompt for filename to add
8. Press Enter — should initiate transfer
9. Press `D` — should prompt for filename, then show download prompt

**Step 5: Commit any fixes, push branch**

```bash
git push -u origin feature/file-download-batch
```

---

## Task 6: Create pull request

```bash
gh pr create --title "feat: add file download, batch download, and clear batch" --body "$(cat <<'EOF'
## Summary
- Implements DOWNLOADFILE (D), BATCHDOWNLOAD (B), and CLEAR_BATCH (-) runnables
- Matches ViSiON/2 loop-based download workflow (Add/Continue/eXit)
- Uses existing transfer infrastructure and string constants
- New file: `internal/menu/file_download.go`

## Test plan
- [ ] `go build ./...` passes
- [ ] `go test ./internal/menu/... ./internal/file/...` passes
- [ ] Manual test: D key prompts for filename, adds to batch, download loop works
- [ ] Manual test: B key shows tagged files, offers add/continue/exit
- [ ] Manual test: - key clears batch queue
- [ ] Tested on pms.vision3bbs.com:37337
EOF
)"
```
