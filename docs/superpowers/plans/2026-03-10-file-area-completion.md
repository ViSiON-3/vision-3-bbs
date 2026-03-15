# File Area Completion Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement all remaining file menu commands (search, info, newscan, column config, extended listing, sysop review queue, lightbar rename, want list) to reach V2 feature parity.

**Architecture:** Each feature is a standalone runnable registered in `executor.go`. New files follow the existing `file_*.go` pattern in `internal/menu/`. New FileManager methods go in `internal/file/manager.go`. User struct gets two new fields. FileRecord gets one new field. All user-facing strings go through `LoadedStrings`.

**Tech Stack:** Go, SSH terminal I/O, pipe-code ANSI rendering, JSON persistence

**Spec:** `docs/superpowers/specs/2026-03-10-file-area-completion-design.md`

**Linear Epic:** VIS-90

---

## Pre-Implementation: Branch Setup

- [ ] **Step 1: Create feature branch from main**

```bash
git checkout main && git pull
git checkout -b feature/file-area-completion
```

---

## Task 1: User & FileRecord Schema Changes (VIS-91, VIS-95, VIS-97)

All three features that need schema changes are done first so downstream tasks have the fields available.

**Files:**
- Modify: `internal/user/user.go` (add `TaggedFileAreaIDs`, `FileListColumns`)
- Modify: `internal/file/types.go` (add `Reviewed` to FileRecord)

- [ ] **Step 1: Add TaggedFileAreaIDs to User struct**

In `internal/user/user.go`, after the `TaggedMessageAreaTags` field (line 55), add:

```go
TaggedFileAreaIDs []int `json:"tagged_file_area_ids,omitempty"` // File area IDs tagged for file newscan
```

- [ ] **Step 2: Add FileListColumns to User struct**

After the `FileListingMode` field (line 68), add:

```go
FileListColumns struct {
    Name        bool `json:"name"`
    Size        bool `json:"size"`
    Date        bool `json:"date"`
    Downloads   bool `json:"downloads"`
    Uploader    bool `json:"uploader"`
    Description bool `json:"description"`
} `json:"file_list_columns,omitempty"`
```

- [ ] **Step 3: Add Reviewed to FileRecord**

In `internal/file/types.go`, after the `DownloadCount` field (line 31), add:

```go
Reviewed bool `json:"reviewed,omitempty"`
```

- [ ] **Step 4: Build and vet**

```bash
go build ./... && go vet ./...
```

- [ ] **Step 5: Commit**

```bash
git add internal/user/user.go internal/file/types.go
git commit -m "feat: add TaggedFileAreaIDs, FileListColumns, and Reviewed fields"
```

---

## Task 2: FileManager Methods (VIS-91, VIS-93)

Add the data access methods needed by newscan and search.

**Files:**
- Modify: `internal/file/manager.go`
- Create: `internal/file/manager_search_test.go`

- [ ] **Step 1: Write failing test for SearchFiles**

Create `internal/file/manager_search_test.go`:

```go
package file

import (
	"os"
	"testing"

	"github.com/google/uuid"
)

func TestSearchFiles(t *testing.T) {
	dir := t.TempDir()
	fm, err := NewFileManager(dir)
	if err != nil {
		t.Fatalf("NewFileManager: %v", err)
	}
	fm.AddArea(FileArea{ID: 1, Tag: "UTILS", Name: "Utilities", Path: "utils"})
	fm.AddArea(FileArea{ID: 2, Tag: "GAMES", Name: "Games", Path: "games"})

	// Create area dirs so AddFileRecord can save
	os.MkdirAll(dir+"/utils", 0o755)
	os.MkdirAll(dir+"/games", 0o755)

	fm.AddFileRecord(FileRecord{ID: uuid.New(), AreaID: 1, Filename: "COOLUTIL.ZIP", Description: "A cool utility"})
	fm.AddFileRecord(FileRecord{ID: uuid.New(), AreaID: 2, Filename: "TETRIS.ZIP", Description: "Cool tetris game"})
	fm.AddFileRecord(FileRecord{ID: uuid.New(), AreaID: 1, Filename: "BORING.ZIP", Description: "Nothing special"})

	// Search by filename
	results := fm.SearchFiles("cool")
	if len(results) != 2 {
		t.Errorf("expected 2 results for 'cool', got %d", len(results))
	}

	// Search by description
	results = fm.SearchFiles("tetris")
	if len(results) != 1 {
		t.Errorf("expected 1 result for 'tetris', got %d", len(results))
	}

	// No match
	results = fm.SearchFiles("nonexistent")
	if len(results) != 0 {
		t.Errorf("expected 0 results for 'nonexistent', got %d", len(results))
	}
}

func TestGetFilesNewerThan(t *testing.T) {
	dir := t.TempDir()
	fm, err := NewFileManager(dir)
	if err != nil {
		t.Fatalf("NewFileManager: %v", err)
	}
	fm.AddArea(FileArea{ID: 1, Tag: "UTILS", Name: "Utilities", Path: "utils"})
	os.MkdirAll(dir+"/utils", 0o755)

	now := time.Now()
	old := now.Add(-48 * time.Hour)
	recent := now.Add(-1 * time.Hour)

	fm.AddFileRecord(FileRecord{ID: uuid.New(), AreaID: 1, Filename: "OLD.ZIP", UploadedAt: old})
	fm.AddFileRecord(FileRecord{ID: uuid.New(), AreaID: 1, Filename: "NEW.ZIP", UploadedAt: recent})

	cutoff := now.Add(-24 * time.Hour)
	results := fm.GetFilesNewerThan(1, cutoff)
	if len(results) != 1 {
		t.Errorf("expected 1 file newer than cutoff, got %d", len(results))
	}
	if results[0].Filename != "NEW.ZIP" {
		t.Errorf("expected NEW.ZIP, got %s", results[0].Filename)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./internal/file/ -run "TestSearchFiles|TestGetFilesNewerThan" -v
```

Expected: compilation errors (methods don't exist)

- [ ] **Step 3: Add missing import to test file**

Add `"time"` to the import block in the test file.

- [ ] **Step 4: Implement SearchFiles in manager.go**

Add after `GetFileRecordByID`:

```go
// SearchFiles returns file records whose filename or description contains
// query (case-insensitive) across all areas.
func (fm *FileManager) SearchFiles(query string) []FileRecord {
	fm.muFiles.RLock()
	defer fm.muFiles.RUnlock()

	lowerQuery := strings.ToLower(query)
	var results []FileRecord
	for _, records := range fm.fileRecords {
		for _, rec := range records {
			if strings.Contains(strings.ToLower(rec.Filename), lowerQuery) ||
				strings.Contains(strings.ToLower(rec.Description), lowerQuery) {
				results = append(results, rec)
			}
		}
	}
	return results
}
```

- [ ] **Step 5: Implement GetFilesNewerThan in manager.go**

Add after `SearchFiles`:

```go
// GetFilesNewerThan returns file records in the given area uploaded after since.
func (fm *FileManager) GetFilesNewerThan(areaID int, since time.Time) []FileRecord {
	fm.muFiles.RLock()
	defer fm.muFiles.RUnlock()

	var results []FileRecord
	for _, rec := range fm.fileRecords[areaID] {
		if rec.UploadedAt.After(since) {
			results = append(results, rec)
		}
	}
	return results
}
```

- [ ] **Step 6: Run tests**

```bash
go test ./internal/file/ -run "TestSearchFiles|TestGetFilesNewerThan" -v
```

Expected: PASS

- [ ] **Step 7: Build and vet entire project**

```bash
go build ./... && go vet ./...
```

- [ ] **Step 8: Commit**

```bash
git add internal/file/manager.go internal/file/manager_search_test.go
git commit -m "feat: add SearchFiles and GetFilesNewerThan to FileManager"
```

---

## Task 3: File Search — SEARCH_FILES (VIS-93)

**Files:**
- Create: `internal/menu/file_search.go`
- Modify: `internal/menu/executor.go` (register)
- Modify: `internal/config/config.go` (new strings)
- Modify: `internal/stringeditor/metadata.go` (new string editor entries)
- Modify production `strings.json` on pms

- [ ] **Step 1: Add string keys to config.go**

In `internal/config/config.go`, add to the LoadedStrings struct:

```go
SearchFilesPrompt   string `json:"searchFilesPrompt"`
SearchFilesMinChars string `json:"searchFilesMinChars"`
SearchNoResults     string `json:"searchNoResults"`
SearchResultsHeader string `json:"searchResultsHeader"`
```

- [ ] **Step 2: Add string editor metadata entries**

In `internal/stringeditor/metadata.go`, add entries for the 4 new keys.

- [ ] **Step 3: Create file_search.go**

Create `internal/menu/file_search.go`:

```go
package menu

import (
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/gliderlabs/ssh"
	"golang.org/x/term"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/terminalio"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

func runSearchFiles(e *MenuExecutor, s ssh.Session, terminal *term.Terminal,
	userManager *user.UserMgr, currentUser *user.User, nodeNumber int,
	sessionStartTime time.Time, args string, outputMode ansi.OutputMode,
	termWidth int, termHeight int) (*user.User, string, error) {

	if currentUser == nil {
		return nil, "", nil
	}

	// Prompt for search string
	terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(e.LoadedStrings.SearchFilesPrompt)), outputMode)
	input, err := readLineFromSessionIH(s, terminal)
	if err != nil {
		return currentUser, "", err
	}
	query := strings.TrimSpace(input)
	if len(query) < 3 {
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(e.LoadedStrings.SearchFilesMinChars)), outputMode)
		time.Sleep(1 * time.Second)
		return currentUser, "", nil
	}

	// Search across all areas
	allResults := e.FileMgr.SearchFiles(query)
	if len(allResults) == 0 {
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(e.LoadedStrings.SearchNoResults)), outputMode)
		time.Sleep(1 * time.Second)
		return currentUser, "", nil
	}

	// Filter by ACS and display
	terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(
		fmt.Sprintf(e.LoadedStrings.SearchResultsHeader, query),
	)), outputMode)

	lineCount := 0
	pageSize := 20
	if termHeight > 4 {
		pageSize = termHeight - 4
	}

	for _, rec := range allResults {
		area, ok := e.FileMgr.GetAreaByID(rec.AreaID)
		if !ok {
			continue
		}
		if area.ACSList != "" && !checkACS(area.ACSList, currentUser, s, terminal, sessionStartTime) {
			continue
		}

		desc := rec.Description
		if len(desc) > 40 {
			desc = desc[:40]
		}
		line := fmt.Sprintf("|15%-12s |07%-10s |03%5dk |07%s\r\n",
			rec.Filename, area.Tag, rec.Size/1024, desc)
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(line)), outputMode)

		lineCount++
		if lineCount >= pageSize {
			if !writeCenteredPausePrompt(terminal, s, outputMode, termWidth) {
				break
			}
			lineCount = 0
		}
	}

	writeCenteredPausePrompt(terminal, s, outputMode, termWidth)
	return currentUser, "", nil
}
```

- [ ] **Step 4: Register in executor.go**

In `registerAppRunnables`, add:

```go
registry["SEARCH_FILES"] = runSearchFiles
```

- [ ] **Step 5: Build and test**

```bash
go build ./... && go vet ./...
```

- [ ] **Step 6: Add strings to production strings.json**

SSH to pms and add the 4 new keys via Python script.

- [ ] **Step 7: Commit**

```bash
git add internal/menu/file_search.go internal/menu/executor.go internal/config/config.go internal/stringeditor/metadata.go
git commit -m "feat: implement SEARCH_FILES command"
```

---

## Task 4: File Info — SHOWFILEINFO (VIS-94)

**Files:**
- Create: `internal/menu/file_info.go`
- Modify: `internal/menu/executor.go` (register)
- Modify: `internal/config/config.go` (new strings)
- Modify: `internal/stringeditor/metadata.go`

- [ ] **Step 1: Add string keys to config.go**

```go
FileInfoPrompt string `json:"fileInfoPrompt"`
FileInfoHeader string `json:"fileInfoHeader"`
FileInfoFormat string `json:"fileInfoFormat"`
```

- [ ] **Step 2: Add string editor metadata entries**

- [ ] **Step 3: Create file_info.go**

Create `internal/menu/file_info.go`:

```go
package menu

import (
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/gliderlabs/ssh"
	"golang.org/x/term"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/terminalio"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

func runShowFileInfo(e *MenuExecutor, s ssh.Session, terminal *term.Terminal,
	userManager *user.UserMgr, currentUser *user.User, nodeNumber int,
	sessionStartTime time.Time, args string, outputMode ansi.OutputMode,
	termWidth int, termHeight int) (*user.User, string, error) {

	if currentUser == nil {
		return nil, "", nil
	}
	if currentUser.CurrentFileAreaID <= 0 {
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(e.LoadedStrings.FileNoAreaSelected)), outputMode)
		time.Sleep(1 * time.Second)
		return currentUser, "", nil
	}

	// Prompt for filename
	terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(e.LoadedStrings.FileInfoPrompt)), outputMode)
	input, err := readLineFromSessionIH(s, terminal)
	if err != nil {
		return currentUser, "", err
	}
	filename := strings.TrimSpace(input)
	if filename == "" {
		return currentUser, "", nil
	}

	rec, err := findFileInArea(e.FileMgr, currentUser.CurrentFileAreaID, filename)
	if err != nil {
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(
			fmt.Sprintf(e.LoadedStrings.FileNotFoundFormat, filename),
		)), outputMode)
		time.Sleep(1 * time.Second)
		return currentUser, "", nil
	}

	area, _ := e.FileMgr.GetAreaByID(currentUser.CurrentFileAreaID)
	areaName := ""
	if area != nil {
		areaName = area.Name
	}

	sizeStr := formatFileSize(rec.Size)
	dateStr := rec.UploadedAt.Format("01/02/2006")

	info := fmt.Sprintf(
		"\r\n|15Filename    |07: |11%s\r\n"+
			"|15File Size   |07: |11%s\r\n"+
			"|15Upload Date |07: |11%s\r\n"+
			"|15Uploaded By |07: |11%s\r\n"+
			"|15Downloads   |07: |11%d\r\n"+
			"|15File Area   |07: |11%s\r\n"+
			"|15Description |07: |03%s\r\n",
		rec.Filename, sizeStr, dateStr,
		rec.UploadedBy, rec.DownloadCount, areaName,
		rec.Description,
	)

	terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(info)), outputMode)
	log.Printf("INFO: Node %d: User %s viewed file info for %s", nodeNumber, currentUser.Handle, rec.Filename)

	writeCenteredPausePrompt(terminal, s, outputMode, termWidth)
	return currentUser, "", nil
}
```

- [ ] **Step 4: Register in executor.go**

```go
registry["SHOWFILEINFO"] = runShowFileInfo
```

- [ ] **Step 5: Build and test**

```bash
go build ./... && go vet ./...
```

- [ ] **Step 6: Add strings to production strings.json**

- [ ] **Step 7: Commit**

```bash
git add internal/menu/file_info.go internal/menu/executor.go internal/config/config.go internal/stringeditor/metadata.go
git commit -m "feat: implement SHOWFILEINFO command"
```

---

## Task 5: File Newscan — FILE_NEWSCAN (VIS-91)

**Files:**
- Create: `internal/menu/file_newscan.go`
- Modify: `internal/menu/executor.go` (register)
- Modify: `internal/config/config.go` (new strings)
- Modify: `internal/stringeditor/metadata.go`

- [ ] **Step 1: Add string keys to config.go**

```go
FileNewscanHeader    string `json:"fileNewscanHeader"`
FileNewscanAreaHdr   string `json:"fileNewscanAreaHdr"`
FileNewscanNoNew     string `json:"fileNewscanNoNew"`
FileNewscanComplete  string `json:"fileNewscanComplete"`
```

- [ ] **Step 2: Add string editor metadata entries**

- [ ] **Step 3: Create file_newscan.go**

Create `internal/menu/file_newscan.go`. The function scans file areas for files uploaded after `currentUser.LastLogin`. If `args == "CURRENT"`, scan current area only. Otherwise scan all areas in `TaggedFileAreaIDs` (or all accessible areas if that slice is empty).

For each area with new files: display area header, then list files using the same format as classic file listing (filename, size, date, description). Paginated with More prompt.

```go
package menu

import (
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/gliderlabs/ssh"
	"golang.org/x/term"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/terminalio"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

func runFileNewscan(e *MenuExecutor, s ssh.Session, terminal *term.Terminal,
	userManager *user.UserMgr, currentUser *user.User, nodeNumber int,
	sessionStartTime time.Time, args string, outputMode ansi.OutputMode,
	termWidth int, termHeight int) (*user.User, string, error) {

	if currentUser == nil {
		return nil, "", nil
	}

	currentOnly := strings.EqualFold(strings.TrimSpace(args), "CURRENT")
	since := currentUser.LastLogin

	terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(e.LoadedStrings.FileNewscanHeader)), outputMode)

	// Determine which areas to scan
	var areaIDs []int
	if currentOnly {
		if currentUser.CurrentFileAreaID > 0 {
			areaIDs = []int{currentUser.CurrentFileAreaID}
		}
	} else if len(currentUser.TaggedFileAreaIDs) > 0 {
		areaIDs = currentUser.TaggedFileAreaIDs
	} else {
		// Scan all accessible areas
		for _, area := range e.FileMgr.ListAreas() {
			if area.ACSList != "" && !checkACS(area.ACSList, currentUser, s, terminal, sessionStartTime) {
				continue
			}
			areaIDs = append(areaIDs, area.ID)
		}
	}

	pageSize := 20
	if termHeight > 4 {
		pageSize = termHeight - 4
	}
	lineCount := 0
	totalNew := 0

	for _, areaID := range areaIDs {
		area, ok := e.FileMgr.GetAreaByID(areaID)
		if !ok {
			continue
		}
		if area.ACSList != "" && !checkACS(area.ACSList, currentUser, s, terminal, sessionStartTime) {
			continue
		}

		newFiles := e.FileMgr.GetFilesNewerThan(areaID, since)
		if len(newFiles) == 0 {
			continue
		}

		// Area header
		hdr := fmt.Sprintf(e.LoadedStrings.FileNewscanAreaHdr, area.Name, len(newFiles))
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(hdr)), outputMode)
		lineCount++

		for _, rec := range newFiles {
			desc := rec.Description
			if len(desc) > 40 {
				desc = desc[:40]
			}
			line := fmt.Sprintf("|15%-12s |03%5dk |07%s |07%s\r\n",
				rec.Filename, rec.Size/1024, rec.UploadedAt.Format("01/02/06"), desc)
			terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(line)), outputMode)

			lineCount++
			totalNew++
			if lineCount >= pageSize {
				if !writeCenteredPausePrompt(terminal, s, outputMode, termWidth) {
					return currentUser, "", nil
				}
				lineCount = 0
			}
		}
	}

	if totalNew == 0 {
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(e.LoadedStrings.FileNewscanNoNew)), outputMode)
	} else {
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(
			fmt.Sprintf(e.LoadedStrings.FileNewscanComplete, totalNew),
		)), outputMode)
	}

	writeCenteredPausePrompt(terminal, s, outputMode, termWidth)
	return currentUser, "", nil
}
```

- [ ] **Step 4: Register in executor.go**

```go
registry["FILE_NEWSCAN"] = runFileNewscan
```

- [ ] **Step 5: Build and test**

```bash
go build ./... && go vet ./...
```

- [ ] **Step 6: Add strings to production strings.json**

- [ ] **Step 7: Commit**

```bash
git add internal/menu/file_newscan.go internal/menu/executor.go internal/config/config.go internal/stringeditor/metadata.go
git commit -m "feat: implement FILE_NEWSCAN command"
```

---

## Task 6: File Newscan Config (VIS-92)

**Files:**
- Modify: `internal/menu/file_newscan.go` (add config function)
- Modify: `internal/menu/executor.go` (register)
- Modify: `internal/config/config.go` (new strings)
- Modify: `internal/stringeditor/metadata.go`
- Modify: `menus/v3/cfg/FILEM.CFG` (wire key C)

- [ ] **Step 1: Add string keys to config.go**

```go
FileNewscanConfigHeader  string `json:"fileNewscanConfigHeader"`
FileNewscanConfigPrompt  string `json:"fileNewscanConfigPrompt"`
FileNewscanConfigSaved   string `json:"fileNewscanConfigSaved"`
```

- [ ] **Step 2: Implement runFileNewscanConfig**

Add to `internal/menu/file_newscan.go`. Pattern mirrors `runNewscanConfig` in `message_scan.go`:
- List all accessible file areas with `[X]`/`[ ]` markers based on `currentUser.TaggedFileAreaIDs`
- User enters area number to toggle, `*` for all, `-` for none, `Q` to quit
- Persist via `userManager.UpdateUser`

- [ ] **Step 3: Register in executor.go**

```go
registry["FILENEWSCANCONFIG"] = runFileNewscanConfig
```

- [ ] **Step 4: Update FILEM.CFG**

Change key C from `RUN:PLACEHOLDER ConfigFileNewscan` to `RUN:FILENEWSCANCONFIG`.

- [ ] **Step 5: Build and test**

```bash
go build ./... && go vet ./...
```

- [ ] **Step 6: Add strings to production strings.json**

- [ ] **Step 7: Commit**

```bash
git add internal/menu/file_newscan.go internal/menu/executor.go internal/config/config.go internal/stringeditor/metadata.go menus/v3/cfg/FILEM.CFG
git commit -m "feat: implement file newscan config"
```

---

## Task 7: Configurable File Listing Columns (VIS-95)

**Files:**
- Modify: `internal/menu/user_config.go` (add column config runnable)
- Modify: `internal/menu/executor.go` (register + modify classic listing renderer)
- Modify: `internal/config/config.go` (new strings)
- Modify: `internal/stringeditor/metadata.go`
- Modify: `menus/v3/cfg/FILEM.CFG` (wire key K)

- [ ] **Step 1: Add string keys to config.go**

```go
CfgFileColumnsHeader string `json:"cfgFileColumnsHeader"`
CfgFileColumnsToggle string `json:"cfgFileColumnsToggle"`
CfgFileColumnsSaved  string `json:"cfgFileColumnsSaved"`
```

- [ ] **Step 2: Add helper to check if columns are default (all false = all on)**

In `internal/menu/user_config.go`, add a helper:

```go
func fileColumnEnabled(u *user.User, col string) bool {
	c := u.FileListColumns
	// Zero-value struct (all false) means all columns on
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
```

- [ ] **Step 3: Implement runCfgFileColumns**

Display current column toggles, let user enter letter (N/S/D/L/U/E) to flip each. Q to save and quit. Persist via `UpdateUser`.

- [ ] **Step 4: Update classic file listing in executor.go**

In the classic display loop (~line 8964), wrap each column's rendering in `fileColumnEnabled` checks. The `^NAME`, `^SIZE`, `^DATE`, `^DESC` placeholders are already in the MID template — conditionally blank them when disabled.

- [ ] **Step 5: Register in executor.go**

```go
registry["CFG_FILECOLUMNS"] = runCfgFileColumns
```

- [ ] **Step 6: Update FILEM.CFG**

Change key K from `RUN:PLACEHOLDER ConfigFileListing` to `RUN:CFG_FILECOLUMNS`.

- [ ] **Step 7: Build and test**

```bash
go build ./... && go vet ./...
```

- [ ] **Step 8: Add strings to production strings.json**

- [ ] **Step 9: Commit**

```bash
git add internal/menu/user_config.go internal/menu/executor.go internal/config/config.go internal/stringeditor/metadata.go menus/v3/cfg/FILEM.CFG
git commit -m "feat: implement configurable file listing columns"
```

---

## Task 8: Extended File Listing — LISTFILES_EXTENDED (VIS-96)

**Files:**
- Modify: `internal/menu/executor.go` (add runnable, pass extended flag)

- [ ] **Step 1: Add extended parameter to classic listing logic**

Refactor the classic file listing in `runListFiles` to accept an `extended bool` parameter. When `extended == true`, skip `fileColumnEnabled` checks and show all columns. The simplest approach: extract the rendering into a helper or pass a flag through args.

Since `runListFiles` already checks `args`, use args to pass the flag:

```go
func runListFilesExtended(e *MenuExecutor, s ssh.Session, terminal *term.Terminal,
	userManager *user.UserMgr, currentUser *user.User, nodeNumber int,
	sessionStartTime time.Time, args string, outputMode ansi.OutputMode,
	termWidth int, termHeight int) (*user.User, string, error) {
	return runListFiles(e, s, terminal, userManager, currentUser, nodeNumber,
		sessionStartTime, "EXTENDED", outputMode, termWidth, termHeight)
}
```

Then in `runListFiles`, check for `strings.Contains(strings.ToUpper(args), "EXTENDED")` and skip column filtering when true.

- [ ] **Step 2: Register in executor.go**

```go
registry["LISTFILES_EXTENDED"] = runListFilesExtended
```

- [ ] **Step 3: Build and test**

```bash
go build ./... && go vet ./...
```

- [ ] **Step 4: Commit**

```bash
git add internal/menu/executor.go
git commit -m "feat: implement LISTFILES_EXTENDED command"
```

---

## Task 9: Sysop File Review Queue (VIS-97)

**Files:**
- Create: `internal/menu/file_edit.go`
- Modify: `internal/file/manager.go` (add GetUnreviewedFiles, MarkFileReviewed)
- Create: `internal/file/manager_review_test.go`
- Modify: `internal/menu/executor.go` (register)
- Modify: `internal/config/config.go` (new strings)
- Modify: `internal/stringeditor/metadata.go`
- Modify: `menus/v3/cfg/FILEM.CFG` (wire key E)

- [ ] **Step 1: Write failing test for GetUnreviewedFiles**

Create `internal/file/manager_review_test.go`:

```go
package file

import (
	"os"
	"testing"

	"github.com/google/uuid"
)

func TestGetUnreviewedFiles(t *testing.T) {
	dir := t.TempDir()
	fm, err := NewFileManager(dir)
	if err != nil {
		t.Fatalf("NewFileManager: %v", err)
	}
	fm.AddArea(FileArea{ID: 1, Tag: "UTILS", Name: "Utilities", Path: "utils"})
	os.MkdirAll(dir+"/utils", 0o755)

	fm.AddFileRecord(FileRecord{ID: uuid.New(), AreaID: 1, Filename: "REVIEWED.ZIP", Reviewed: true})
	fm.AddFileRecord(FileRecord{ID: uuid.New(), AreaID: 1, Filename: "UNREVIEWED.ZIP", Reviewed: false})

	results := fm.GetUnreviewedFiles(1)
	if len(results) != 1 {
		t.Errorf("expected 1 unreviewed file, got %d", len(results))
	}
	if results[0].Filename != "UNREVIEWED.ZIP" {
		t.Errorf("expected UNREVIEWED.ZIP, got %s", results[0].Filename)
	}
}
```

- [ ] **Step 2: Implement GetUnreviewedFiles in manager.go**

```go
// GetUnreviewedFiles returns file records in the given area where Reviewed is false.
func (fm *FileManager) GetUnreviewedFiles(areaID int) []FileRecord {
	fm.muFiles.RLock()
	defer fm.muFiles.RUnlock()

	var results []FileRecord
	for _, rec := range fm.fileRecords[areaID] {
		if !rec.Reviewed {
			results = append(results, rec)
		}
	}
	return results
}
```

- [ ] **Step 3: Run test**

```bash
go test ./internal/file/ -run "TestGetUnreviewedFiles" -v
```

- [ ] **Step 4: Add string keys to config.go**

```go
SysopReviewHeader    string `json:"sysopReviewHeader"`
SysopReviewFileInfo  string `json:"sysopReviewFileInfo"`
SysopReviewPrompt    string `json:"sysopReviewPrompt"`
SysopReviewMarked    string `json:"sysopReviewMarked"`
SysopReviewNoFiles   string `json:"sysopReviewNoFiles"`
SysopReviewScanAll   string `json:"sysopReviewScanAll"`
```

- [ ] **Step 5: Create file_edit.go**

Create `internal/menu/file_edit.go` with `runEditFileRecord`. The function:
1. Asks sysop: scan current area or all areas
2. Collects unreviewed files from chosen areas
3. Steps through one at a time: clear screen, display file metadata, show action menu
4. Actions: [C] change description, [R] rename, [D] delete, [M] move, [S] skip, [Q] quit
5. After C/R/D/M, asks "Mark as reviewed? [Y/N]"
6. Uses existing `UpdateFileRecord`, `DeleteFileRecord`, `MoveFileRecord` from FileManager
7. For rename: `os.Rename` on disk + `UpdateFileRecord` to change `Filename`

- [ ] **Step 6: Register in executor.go**

```go
registry["EDITFILERECORD"] = runEditFileRecord
```

- [ ] **Step 7: Update FILEM.CFG**

Change key E from `RUN:PLACEHOLDER EditFileRecord` to `RUN:EDITFILERECORD`.

- [ ] **Step 8: Build and test**

```bash
go build ./... && go vet ./... && go test ./internal/file/ -v
```

- [ ] **Step 9: Add strings to production strings.json**

- [ ] **Step 10: Commit**

```bash
git add internal/menu/file_edit.go internal/file/manager.go internal/file/manager_review_test.go internal/menu/executor.go internal/config/config.go internal/stringeditor/metadata.go menus/v3/cfg/FILEM.CFG
git commit -m "feat: implement sysop file review queue"
```

---

## Task 10: Lightbar Rename (VIS-98)

**Files:**
- Modify: `internal/menu/file_lightbar.go` (add rename action)

- [ ] **Step 1: Add R to sysop commands list**

In `file_lightbar.go` around line 161, add to the `sysopCmds` slice:

```go
{key: "r", label: "Rename"},
```

- [ ] **Step 2: Add case "r" handler**

After the `case "m"` block (~line 1114), add a `case "r"` block:
1. Prompt for new filename
2. Validate: no path traversal, no empty, no duplicate in area
3. Build old and new full paths using area's base path
4. `os.Rename(oldPath, newPath)`
5. `e.FileMgr.UpdateFileRecord(rec.ID, func(r *FileRecord) { r.Filename = newName })`
6. Refresh file list
7. Log the rename

- [ ] **Step 3: Build and test**

```bash
go build ./... && go vet ./...
```

- [ ] **Step 4: Commit**

```bash
git add internal/menu/file_lightbar.go
git commit -m "feat: add rename action to lightbar sysop commands"
```

---

## Task 11: Sysop Want List (VIS-99)

**Files:**
- Create: `internal/menu/file_wantlist.go`
- Modify: `internal/menu/executor.go` (register)
- Modify: `internal/config/config.go` (new strings)
- Modify: `internal/stringeditor/metadata.go`
- Modify: `menus/v3/cfg/FILEM.CFG` (wire key X)

- [ ] **Step 1: Add string keys to config.go**

```go
WantListPrompt    string `json:"wantListPrompt"`
WantListSubmitted string `json:"wantListSubmitted"`
WantListEmpty     string `json:"wantListEmpty"`
WantListHeader    string `json:"wantListHeader"`
WantListCleared   string `json:"wantListCleared"`
```

- [ ] **Step 2: Create file_wantlist.go**

Implement `runWantList`:
- Define `WantListEntry` struct: Handle, Filename, Reason, Date
- Load/save from `data/wantlist.json` (create data/ dir if needed)
- If user is sysop: show list + option to clear individual or all
- If regular user: prompt for filename + reason, append to list, save

- [ ] **Step 3: Register in executor.go**

```go
registry["WANTLIST"] = runWantList
```

- [ ] **Step 4: Update FILEM.CFG**

Change key X from `RUN:PLACEHOLDER SysopWantList` to `RUN:WANTLIST`.

- [ ] **Step 5: Build and test**

```bash
go build ./... && go vet ./...
```

- [ ] **Step 6: Add strings to production strings.json**

- [ ] **Step 7: Commit**

```bash
git add internal/menu/file_wantlist.go internal/menu/executor.go internal/config/config.go internal/stringeditor/metadata.go menus/v3/cfg/FILEM.CFG
git commit -m "feat: implement sysop want list"
```

---

## Post-Implementation

- [ ] **Deploy to pms**

```bash
GOOS=linux GOARCH=amd64 go build -o /tmp/vision3-deploy ./cmd/vision3/
scp /tmp/vision3-deploy felonius@pms.vision3bbs.com:/tmp/vision3
ssh felonius@pms.vision3bbs.com "sudo systemctl stop vision3 && sudo cp /tmp/vision3 /opt/PiRATEMiNDSTATiON/vision3 && sudo systemctl start vision3"
```

- [ ] **Test each command on live BBS**

- [ ] **Run CodeRabbit review**

- [ ] **Create PR to main**

- [ ] **Update Linear issues to Done**
