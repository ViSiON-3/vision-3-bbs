# File Download, Batch Download, and Clear Batch

## Overview

Implement three missing menu-level runnables for the file transfer menu (FILEM): DOWNLOADFILE, BATCHDOWNLOAD, and CLEAR_BATCH. These provide the classic prompt-driven download workflow matching ViSiON/2 behavior.

## V2 Download Workflow

ViSiON/2 used a loop-based download flow where `D` prompted users to build a batch queue, then initiate transfer. The strings.json entries confirm this:

- `addBatchPrompt`: prompt for filename to add
- `downloadStr`: `"File(s) Download - e(X)it, (A)dd, (Cr) Continue"` — loop control
- `untaggingBatchFile`: feedback when removing from queue

## Design

### DOWNLOADFILE (D)

The primary download command. Enters a loop where the user builds a download queue, then initiates transfer.

1. Check user logged in and file area selected (`FileNoAreaSelected`)
2. Check download ACS on current area (`YouCantDownloadHere` / `NoDownloadsHere`)
3. Prompt for filename using `AddBatchPrompt`
4. Case-insensitive exact match via `findFileInArea(fm, areaID, name)` (exists in file_viewer.go)
5. No match → `FileNotFoundFormat` → re-prompt
6. Match → add file ID to `TaggedFileIDs` (skip if already tagged, using `FileAlreadyMarked`)
7. Show `DownloadStr` prompt — options:
   - **A** — add more files (loop back to step 3)
   - **X** — exit without downloading
   - **CR** (Enter) — proceed to transfer
8. On CR: resolve all tagged file paths → `selectTransferProtocol` → `runTransferSend`
9. Clear `TaggedFileIDs`, update `NumDownloads` on user, save

### BATCHDOWNLOAD (B)

Downloads files already in the batch queue (tagged via lightbar or previous `D` session).

1. Check user logged in
2. `TaggedFileIDs` empty → `"No files tagged for download."` → return
3. Show tagged file count, then `DownloadStr` prompt:
   - **A** — add more files (prompt for filename, same as DOWNLOADFILE step 3-6)
   - **X** — exit without downloading
   - **CR** — proceed to transfer
4. On CR: resolve paths → `selectTransferProtocol` → `runTransferSend`
5. Clear `TaggedFileIDs`, update `NumDownloads`, save

### CLEAR_BATCH (-)

Clears the batch download queue.

1. Check user logged in
2. `TaggedFileIDs` empty → `"Batch queue is already empty."` → return
3. Show count: `"Cleared N file(s) from batch queue."`
4. Set `TaggedFileIDs = nil`, save user

## Existing Strings Used

| String Key | Usage |
|---|---|
| `AddBatchPrompt` | Filename prompt when adding to queue |
| `DownloadStr` | Loop control prompt (X/A/CR) |
| `FileNoAreaSelected` | No area selected error |
| `FileNotFoundFormat` | File not found in area |
| `FilePromptFormat` | Generic file prompt (fallback) |
| `YouCantDownloadHere` | ACS denied |
| `NoDownloadsHere` | Area has no download permission |
| `FileAlreadyMarked` | File already in batch |
| `UntaggingBatchFile` | Feedback when clearing batch |
| `InvalidFilename` | Bad filename input |
| `SuccessfulDownload` | Transfer success message |
| `FiftyFilesMaximum` | Batch size limit |

## Implementation

### New file: `internal/menu/file_download.go`

Three runnables:
- `runDownloadFile` — DOWNLOADFILE command
- `runBatchDownload` — BATCHDOWNLOAD command
- `runClearBatch` — CLEAR_BATCH command

Shared helper:
- `downloadLoop` — the add/continue/exit loop used by both DOWNLOADFILE and BATCHDOWNLOAD

### Existing helper reused: `internal/menu/file_viewer.go`

- `findFileInArea(fm, areaID, filename)` — case-insensitive filename lookup (already exists)

### Registration: `internal/menu/executor.go`

```go
registry["DOWNLOADFILE"] = runDownloadFile
registry["BATCHDOWNLOAD"] = runBatchDownload
registry["CLEAR_BATCH"] = runClearBatch
```

### No changes needed

- User struct — `TaggedFileIDs` already exists
- Transfer package — `ExecuteSend`, `runTransferSend` already work
- FileManager — `GetFilePath`, `IncrementDownloadCount` already exist
- StringsConfig — all needed strings already defined

## Testing

- Unit test `findFileInArea` (already has coverage via file_viewer tests)
- Unit tests for `runClearBatch` edge cases
- Build verification: `go build ./...` and `go vet ./...`
- Manual test on pms.vision3bbs.com: upload file, D to download, B for batch, - to clear
