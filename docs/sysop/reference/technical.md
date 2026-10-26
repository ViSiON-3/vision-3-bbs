# Technical Reference

This document covers implementation patterns and subsystem details beyond what's in [Architecture](reference/architecture.md).

## File Area System

### Menu Commands

All file menu commands are implemented (no PLACEHOLDERs remain). The names below are the `RUN:` targets as the executor registers them; the [Menu Commands Reference](reference/menu-commands.md#file-areas-and-transfers) has the data each one accepts and the access it checks.

| Command | Description |
|---------|-------------|
| `RUN:LISTFILES` | Paginated file listing with marking and lightbar navigation |
| `RUN:LISTFILEAR` | List available file areas |
| `RUN:SELECTFILEAREA` | Change current file area |
| `RUN:SEARCH_FILES` | Keyword search across file names and descriptions |
| `RUN:SHOWFILEINFO` | Detailed file record display |
| `RUN:FILE_NEWSCAN` | Scan tagged areas for files newer than the newscan cutoff |
| `RUN:FILENEWSCANCONFIG` | Per-user file area tagging for newscans |
| `RUN:SETFILESCANDATE` | Set the file newscan cutoff for this session |
| `RUN:EDITFILERECORD` | Sysop review queue for unreviewed uploads |
| `RUN:BATCHDOWNLOAD` | Download marked files via ZMODEM |
| `RUN:UPLOADFILE` | Upload file to current area |
| `RUN:WANTLIST` | User-maintained list of wanted files |

### String Externalization

All user-facing strings are fields of `config.StringsConfig` (`internal/config/config_strings_types.go`), loaded from `configs/strings.json` by `LoadStrings`. Missing fields are filled from `StringFallbacks` by `applyStringDefaults()`, so the BBS runs without a `strings.json` file. Sysops override strings by editing `configs/strings.json` or with the String Editor. A string whose value is `!CODE [args]` runs the V3 script door `CODE` in place of the text, and `%file.ext [text]` shows a display file first (`internal/stringhook`); `!|text` prints a literal leading `!`.

### FileManager Methods

Key methods on `FileManager` (`internal/file/manager_query.go`):

- `SearchFiles(query string) []FileRecord` — case-insensitive search across names and descriptions in all areas.
- `GetFilesNewerThan(areaID int, since time.Time) []FileRecord` — returns files uploaded after the given timestamp.
- `GetUnreviewedFiles(areaID int) []FileRecord` — returns files in an area where `Reviewed == false`.
- `GetFileRecordByID(fileID uuid.UUID) (*FileRecord, error)` — fetch a single file record by its UUID.

### User Fields for File Areas

Fields on the `User` struct (`internal/user/user.go`):

- `TaggedFileAreaIDs []int` — file areas the user has tagged for newscans.
- `TaggedFileAreaTags []string` — corresponding area tags for tagged file areas.
- `FileNewscanSince *time.Time` — the newscan cutoff set by `SETFILESCANDATE`; nil means the previous logon.
- `FileListColumns` — the columns shown in file listings (`name`, `size`, `date`, `downloads`, `uploader`, `description`).

### FileRecord.Reviewed

The `Reviewed` field on `FileRecord` tracks whether a sysop has approved an uploaded file. The `RUN:EDITFILERECORD` command lists unreviewed files and lets sysops approve, move or delete them. Files that arrive through a TIC file echo are stored reviewed.

### Lightbar Sysop Bar Toggle

In lightbar file listings, pressing `*` toggles a sysop action bar (rename, move, delete, edit description). This is gated by ACS — only users with sysop-level access see the bar.

### Batch File Downloads

Files are marked during listing and queued in the user's batch. At download time (`RUN:BATCHDOWNLOAD`), ACS is re-validated per-area to prevent privilege escalation if area permissions changed between marking and downloading. On persist failure, the batch clear is rolled back.

### FTN File Echoes (TIC)

A file area with both `network` and `file_echo` set is fed by an FTN file echo. `v3mail toss` and `v3mail poll` handle `.TIC` files in each network's inbound before tossing packets (`internal/tosser/tic.go`, parser in `internal/ftn/tic.go`). A TIC is accepted when `From` is one of the network's links, `Pw` matches that link's `tic_password` (or, with no password set, the TIC arrived in the secure inbound), an area is linked to the echo for that network, the named file is present, and `Crc` matches. The file moves into the area with a record (description from `Ldesc`/`Desc`, uploader = the hatching address, reviewed, `crc32`). Same name with the same CRC is dropped as a dupe; a different CRC replaces the file and keeps its record and download count. A TIC that fails goes to `badtic` under the network's `temp_path` together with its file. The `Replaces` keyword is honoured. See [FTN File Echoes (TIC)](files/file-echoes.md).

## Shared Config Files

More than one program writes the JSON files in `configs/`: the BBS (allow-new-users toggle, sponsor area editor, V3Net subscriptions, auto-created areas), the config editor, `helper` (`ftnsetup`, `fileecho`, `areas set`) and `v3mail`. Three packages keep them from undoing each other.

| Package | Role |
|---------|------|
| `internal/filelock` | Advisory cross-process lock. The lock file is `<file>.lock` beside the data file (`configs/message_areas.json.lock` and so on); `/configs/*.lock` is git-ignored. Advisory means it only excludes programs that ask for it |
| `internal/configmerge` | `SaveFile` locks, re-reads the file, three-way merges the on-disk version with the caller's against the version the caller loaded, and writes only when something would change. Objects merge key by key; lists of items with an `id` merge item by item; an edit beats a delete; the same tag added twice becomes one item and an id collision is renumbered; a conflict keeps the saver's value and is reported |
| `internal/atomicfile` | `WriteFile` writes a temp file in the same directory and renames it over the target. On Windows the rename retries briefly while another process holds the file open |

The config editor saves `config.json`, `conferences.json`, `file_areas.json`, `ftn.json` and `v3net.json` through `configmerge.SaveFile` and shows conflicts on its status line. `message_areas.json` has its own per-area, per-field merge (`message.MergeAreas` in `internal/message/areas_file.go`) used by the BBS and the editor; `helper` holds the lock from read to write. The BBS's `SaveAreas` merges only when the file's bytes differ from what it last read or wrote, and recreates a file that vanished from memory rather than reading it as "every area deleted". The same `filelock` also guards `users.json`, the FTN dupe database and QWKnet imports.

## Board Diagnostics (doctor)

`vision3 doctor` (also `vision3 --doctor`) is a read-only health report that runs the startup preflight checks and exits before any listener starts. It lives in `cmd/vision3/doctor.go`.

| Flag | Effect |
|------|--------|
| `--json` | Write the report as JSON (checks, summary, fixes) |
| `--fix` | Create the standard data directories that are missing. Nothing else is changed |
| `--dry-run` | With `--fix`: list the directories it would create without creating them |

Exit codes: `0` no findings, `1` warnings only, `2` at least one failure, or the report could not be written. `--fix` without `--doctor`, or `--dry-run` without `--fix`, also exits `2`.

The checks cover `configs/*.json` parsing with line and column, unknown keys in `config.json` and `ftn.json`, data, log and message-base paths and permissions, area and conference references, duplicate tags and base paths, message and file echo routing, TIC authentication, FTN addresses, links, passwords and paths against `binkd.conf`, QWKnet and V3Net settings and the V3Net identity key, sysop and default credentials, ACS levels, signup settings, InfoForms, IP lists and secret-file permissions, the bad-name list, menu files and their `GOTO`/`RUN`/`DOOR` references, and configured door scripts and executables.

## Door Servers

Doors of `type: "rlogin"` and `type: "telnet"` are outbound connections rather than programs. The shared fields are `host`, `port` (default 513 for RLogin, 23 for Telnet), `connect_timeout` (default 10 seconds) and `disconnect_key` (default `^]`; `none` disables it). `relayRemoteSession` in `internal/menu/door_handler_remote.go` relays the session either way.

- **RLogin** (`internal/rlogin`, RFC 1282): only connection establishment is implemented. `client_username`, `server_username` and `terminal_type` carry the three handshake fields and accept placeholders.
- **Telnet** (`internal/telnetclient`, RFC 854): the mirror of `internal/telnetserver`. It answers the server's negotiation and starts none of its own, reports the caller's stored screen size and the door's `terminal_type` (default `ANSI`), escapes `0xFF`, and sends CR as CR NUL outside binary mode. `send_on_connect` is typed into the session as soon as it is up, for servers that want a login; `raw_tcp` turns the protocol off for a server that speaks plain TCP.

See [Door Servers (RLogin, Telnet)](doors/door-servers.md) for setup.

## WFC Snoop Channel

The `wfc` console connects over SSH and uses two subsystems registered in `cmd/vision3/ssh_server.go`: `wfc-admin` for the console and `wfc-snoop` (`cmd/vision3/wfc_snoop.go`) for watching a node. Access to both is the same: an SSH key on a CoSysOp-or-above account with WFC Access on, re-checked every 30 seconds. An account with `wfcReadOnly` set may watch but not kick, snoop, type in or chat.

`internal/snoop` provides the per-session `Tap` that the SSH and telnet transports write through. It copies everything written to the caller to any attached sysop, keeps the output since the last clear-screen (64 KiB) so a new watcher sees the current screen, and never blocks the caller's write. Binary transfers show as `[transfer in progress]`. While type-in or split-screen chat is on, the sysop's keystrokes are merged into the caller's input, so they work in menus and inside doors; only one sysop holds a node's keyboard at a time. Attach and detach are logged; the caller is not told. `PAGESYSOP` pages every connected console and waits `pageSysopTimeout` seconds for an answer, with one page per caller per `pageSysopCooldown` seconds. See [WFC Sysop Console](how-to-guides/wfc-console.md#watching-a-node).
