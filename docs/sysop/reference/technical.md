# Technical Reference

This document covers implementation patterns and subsystem details beyond what's in [Architecture](architecture.md).

## File Area System

### Menu Commands

All file menu commands are implemented (no PLACEHOLDERs remain):

| Command | Description |
|---------|-------------|
| `RUN:LISTFILES` | Paginated file listing with marking and lightbar navigation |
| `RUN:LISTFILEAR` | List available file areas |
| `RUN:SELECTFILEAREA` | Change current file area |
| `RUN:SEARCHFILES` | Keyword search across file descriptions |
| `RUN:FILEINFO` | Detailed file record display |
| `RUN:FILENEWSCAN` | Scan tagged areas for files newer than last scan |
| `RUN:FILENEWSCANCONFIG` | Per-user file area tagging for newscans |
| `RUN:REVIEWFILES` | Sysop review queue for unreviewed uploads |
| `RUN:DOWNLOADBATCH` | Download marked files via ZMODEM |
| `RUN:UPLOADFILE` | Upload file to current area |
| `RUN:WANTLIST` | User-maintained list of wanted files |

### String Externalization

All user-facing strings use the `LoadedStrings` map in `internal/config/`. Default values are registered in `applyStringDefaults()` so the BBS runs without a `strings.json` file. Sysops override strings by adding keys to `configs/strings.json`.

### FileManager Methods

Key methods on `FileManager` (`internal/file/manager.go`):

- `SearchFiles(keyword string) []FileRecord` — case-insensitive search across descriptions in all areas.
- `GetFilesNewerThan(areaID int, since time.Time) []FileRecord` — returns files uploaded after the given timestamp.
- `GetUnreviewedFiles() []FileRecord` — returns files where `Reviewed == false` across all areas.
- `GetFileRecordByID(areaID int, fileID int) (*FileRecord, error)` — fetch a single file record by area and ID.

### User Fields for File Areas

Fields on the `User` struct (`internal/user/user.go`):

- `TaggedFileAreaIDs []int` — file areas the user has tagged for newscans.
- `TaggedFileAreaTags []string` — corresponding area tags for tagged file areas.
- `FileListColumns string` — user's preferred column layout for file listings.

### FileRecord.Reviewed

The `Reviewed` field on `FileRecord` tracks whether a sysop has approved an uploaded file. The `RUN:REVIEWFILES` command lists unreviewed files and lets sysops approve or reject them.

### Lightbar Sysop Bar Toggle

In lightbar file listings, pressing `*` toggles a sysop action bar (rename, move, delete, edit description). This is gated by ACS — only users with sysop-level access see the bar.

### Batch File Downloads

Files are marked during listing and queued in the user's batch. At download time (`RUN:DOWNLOADBATCH`), ACS is re-validated per-area to prevent privilege escalation if area permissions changed between marking and downloading. On persist failure, the batch clear is rolled back.
