# File Area Completion Epic

Complete the remaining unimplemented file menu commands to reach V2 feature parity.

## Context

FILEM.CFG defines 19 menu commands. 11 are implemented (area select, listing, upload, download, batch download, clear batch, view file, type text, logoff, goto main, file list mode). 8 remain as stubs, placeholders, or unregistered commands. The lightbar file browser already has sysop actions (edit description, kill, move) but is missing rename.

## Stories

### Story 1: File Search (`SEARCH_FILES`)

**Menu key:** S
**File:** `internal/menu/file_search.go`

Prompt user for a search string (minimum 3 chars). Scan filenames and descriptions across all file areas where the user passes `ACSList`. Display matches using FILELIST.TOP/MID/BOT templates with the area name prepended. Paginated output with More prompt.

**New FileManager method:**
```go
func (fm *FileManager) SearchFiles(query string) []FileRecord
```
Returns records across all areas where filename or description contains query (case-insensitive). Caller filters by ACS.

**Registration:** `registry["SEARCH_FILES"] = runSearchFiles`

### Story 2: File Info (`SHOWFILEINFO`)

**Menu key:** I
**File:** `internal/menu/file_info.go`

Prompt for filename, look up in current area via `findFileInArea`. Display full-screen:
- Filename, file size (formatted), upload date, uploader handle
- Download count, area name
- Full multi-line description

Use a FILEINFO.ANS template if present (with placeholders), otherwise render with pipe codes.

**Registration:** `registry["SHOWFILEINFO"] = runShowFileInfo`

### Story 3: File Newscan (`FILE_NEWSCAN`)

**Menu key:** N, Z
**File:** `internal/menu/file_newscan.go`

Scan file areas for files uploaded since `user.LastLogin`. If args contains "CURRENT", scan current area only. Otherwise scan all areas in `user.TaggedFileAreaIDs` (or all accessible areas if empty/unset).

Display new files per area using file listing templates. Show area header before each area's files. Paginated with More prompt.

**New user field:**
```go
TaggedFileAreaIDs []int `json:"tagged_file_area_ids,omitempty"`
```

**New FileManager method:**
```go
func (fm *FileManager) GetFilesNewerThan(areaID int, since time.Time) []FileRecord
```

**Registration:** `registry["FILE_NEWSCAN"] = runFileNewscan`

### Story 4: File Newscan Config (`ConfigFileNewscan`)

**Menu key:** C (replaces PLACEHOLDER)
**File:** `internal/menu/file_newscan.go` (same file as Story 3)

List all file areas with `[X]` or `[ ]` toggle markers. User enters area number to toggle, `*` to select all, `-` to clear all, `Q` to quit. Mirrors `runNewscanConfig` pattern for message areas.

Persists to `user.TaggedFileAreaIDs` via `userManager.UpdateUser`.

**Registration:** `registry["FILENEWSCANCONFIG"] = runFileNewscanConfig`

**FILEM.CFG update:** Change key C from `RUN:PLACEHOLDER ConfigFileNewscan` to `RUN:FILENEWSCANCONFIG`

### Story 5: Configurable File Listing Columns (`ConfigFileListing`)

**Menu key:** K (replaces PLACEHOLDER)
**File:** `internal/menu/user_config.go` (add to existing)

V2 had `FileList[1..8]` boolean toggles for which columns appear in the classic file listing. V3 equivalent:

**New user field:**
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

Defaults: all true (when zero-value/omitted, treat as all-on). Go bools default to `false`, so `fileColumnEnabled()` explicitly checks if all fields are false (zero-value struct) and returns `true` for all columns in that case. First toggle from default state sets all to `true` then flips the selected column off.

Display current column config with toggles. User enters letter to flip each column.

Update the classic file listing renderer in `executor.go` (`runListFiles`) to respect these column settings. Only render columns the user has enabled.

**Registration:** `registry["CFG_FILECOLUMNS"] = runCfgFileColumns`

**FILEM.CFG update:** Change key K from `RUN:PLACEHOLDER ConfigFileListing` to `RUN:CFG_FILECOLUMNS`

### Story 6: Extended File Listing (`LISTFILES_EXTENDED`)

**Menu key:** W
**File:** `internal/menu/executor.go` (modify existing `runListFiles`)

Add an `extended` flag to the file listing logic. When true, all columns are rendered regardless of user's `FileListColumns` config. Mirrors V2's `ListFiles(Extended=true, ...)`.

`runListFilesExtended` calls the same listing logic with extended=true.

**Registration:** `registry["LISTFILES_EXTENDED"] = runListFilesExtended`

### Story 7: Sysop File Review Queue (`EditFileRecord`)

**Menu key:** E (replaces PLACEHOLDER, ACS: SYSOP)
**File:** `internal/menu/file_edit.go`

Sequential review of unreviewed uploads. Sysop walks through files where `Reviewed == false` one at a time, full-screen display:

```
SysOp File Review — [area name]
Filename    : COOLUTIL.ZIP
File Size   : 145,230
Uploaded By : SomeUser
Upload Date : 2026-03-08
Downloads   : 0
Description : A cool utility
              Second line of description

[C]hange Description  [R]ename  [D]elete  [M]ove  [S]kip  [Q]uit
Mark as reviewed? [Y/N]:
```

Actions: change description (inline prompt), rename on disk, delete (with confirmation), move to area, skip to next, quit. After any action except skip/quit, prompt to mark as reviewed.

**New FileRecord field:**
```go
Reviewed bool `json:"reviewed,omitempty"`
```

New uploads default to `Reviewed: false`. The review queue filters to `!Reviewed`. Option to scan current area or all areas.

**Registration:** `registry["EDITFILERECORD"] = runEditFileRecord`

**FILEM.CFG update:** Change key E from `RUN:PLACEHOLDER EditFileRecord` to `RUN:EDITFILERECORD`

### Story 8: Lightbar Rename

**File:** `internal/menu/file_lightbar.go`

Add rename-file-on-disk to the existing sysop actions in the lightbar (currently: E=edit description, K=kill, M=move).

New key **R**: prompt for new filename, validate (no path traversal, no duplicates in area), rename on disk via `os.Rename`, update `FileRecord.Filename` via `FileManager.UpdateFileRecord`.

### Story 9: Sysop Want List (`SysopWantList`)

**Menu key:** X (replaces PLACEHOLDER, ACS: SYSOP for view/clear, no ACS for submit)
**File:** `internal/menu/file_wantlist.go`

Users submit file requests stored in `data/wantlist.json`:
```json
[
  {"handle": "CoolDude", "filename": "GAME.ZIP", "reason": "Need this game", "date": "2026-03-10"}
]
```

Date format: ISO 8601 (`YYYY-MM-DD`). Concurrent access protected by `sync.Mutex` around all read-modify-write operations.

Two modes based on ACS:
- **User mode:** Submit a request (filename + optional reason)
- **Sysop mode:** List all requests, clear individual or all

**Registration:** `registry["WANTLIST"] = runWantList`

**FILEM.CFG update:** Change key X from `RUN:PLACEHOLDER SysopWantList` to `RUN:WANTLIST`

## Build Order

1. **Stories 3+4** (file newscan + config) — share `TaggedFileAreaIDs` user field
2. **Stories 1+2** (search + file info) — independent, can parallel
3. **Stories 5+6** (column config + extended listing) — 6 depends on 5
4. **Stories 7+8** (review queue + lightbar rename) — sysop features
5. **Story 9** (want list) — standalone, lowest priority

## String Externalization

All user-facing strings go in `LoadedStrings` / `strings.json`. No hardcoded pipe-code literals.

## Testing

Each story includes unit tests for new FileManager methods. Integration tested via BBS login on pms.

## Linear Tracking

Create one epic issue with sub-issues for each story.
