# File Areas

File areas are the BBS's download library: topic-based collections of files
that callers browse, mark, download and upload to, each with its own access
rules. ViSiON/3 keeps the classic shape of a 1990s file base (areas grouped
into conferences, a newscan for files uploaded since your last call, a sysop
review queue for uploads) and adds the modern conveniences: a full-screen
lightbar listing, `FILE_ID.DIZ` descriptions pulled out of archives
automatically, an archive viewer, and areas that fill themselves from an FTN
(FidoNet Technology Network) file echo.

## Setting Up File Areas

Open the [Configuration Editor](configuration/configuration.md#configuration-editor-tui)
(`./config`), choose **3 — Areas and Conferences**, then **File Areas**. Each
area has these fields:

| Field | Meaning |
| --- | --- |
| **Tag** | Short uppercase identifier callers can type to jump to the area, e.g. `UTILS` |
| **Name** / **Description** | What callers see in area lists |
| **Path** | Directory under `data/files/` where the files live; created for you when the BBS loads the area |
| **ACS List** | Access Control String needed to see the area and list its files (empty = everyone) |
| **ACS Upload** | ACS needed to upload into the area |
| **ACS Download** | ACS needed to download from it |
| **Conference** | Conference the area belongs to; conferences group areas for both the file and message menus |
| **Network** / **File Echo** | Leave empty for a local area. Set both to have the area fed by an FTN file echo — see [FTN File Echoes](files/file-echoes.md) |

Saving writes `configs/file_areas.json`. A running BBS validates the file at
once and applies it on the first poll with no callers online, so new areas
appear without a restart (see
[Applying Configuration Changes](configuration/configuration.md#applying-configuration-changes)).

The shipped configuration has two areas: **GENERAL** (list `s10`, download
`s25`, upload `s250`) and an **UPLOADS** queue that callers at `s10` can upload
to but only the sysop can list or download from. That pattern — a public
library plus a private drop box — is a good starting point: callers' uploads
land where you can check them before moving them to a public area.

### Creating a new area and filling it

1. Add the area in `./config` → **3 — Areas and Conferences** → **File Areas**.
2. Import files with `helper files import`; it copies the files, reads each
   archive's `FILE_ID.DIZ` for the description and writes the area's file list
   (see [Bulk File Import](files/bulk-import.md)):

```bash
./helper files import --dir /path/to/files --area MYAREA --preserve-dates
```

The import can run while the BBS is up: the BBS, `v3mail` and the helper all
share each area's file list under a lock, so none of them overwrites what
another added.

### Access control

- **Listing** — empty `acs_list` lets every caller see the area. Restrict with
  an ACS such as `s10` (level 10 and up) or `fD` (flag D).
- **Uploading** — set `acs_upload`. A validated-user level such as `s10` keeps
  new accounts out of the upload path.
- **Downloading** — set `acs_download`; every file in an area shares it.

All three accept the same ACS syntax as menus and message areas.

## The File Menu

Callers reach the file menu with the stock **T** key on the main menu. Its
commands are bound in `menus/v3/cfg/FILEM.CFG` and drawn by `FILEM.ANS`; the
[menu commands reference](reference/menu-commands.md#file-areas-and-transfers)
describes each command in full. The shipped bindings:

| Key | Command | Does |
| --- | --- | --- |
| **A** | `SELECTFILEAREA` | Pick an area by number or tag (lightbar or classic, per the caller's listing mode) |
| **C** | `CHANGEFILECONF` | Pick a conference; the choice applies to the message menu too |
| **]** / **[** | `NEXTFILEAREA` / `PREVFILEAREA` | Step through the areas in the current conference |
| **}** / **{** | `NEXTFILECONF` / `PREVFILECONF` | Step through conferences (also for the message menu) |
| **F** or **L** | `LISTFILES` | List the current area |
| **W** | `LISTFILES_EXTENDED` | List with every column shown |
| **S** | `SEARCH_FILES` | Search names and descriptions across every area (three characters or more) |
| **N** | `FILE_NEWSCAN` | Files uploaded since the newscan cutoff, in the tagged areas |
| **Z** | `FILENEWSCANCONFIG` | Tag and untag areas for the file newscan |
| **Y** | `SETFILESCANDATE` | Change the newscan cutoff for this session |
| **D** | `DOWNLOADFILE` | Ask for a filename, add it to the batch, start the transfer |
| **B** | `BATCHDOWNLOAD` | Send every marked file |
| **-** | `CLEAR_BATCH` | Empty the batch |
| **U** | `UPLOADFILE` | Upload into the current area |
| **V** | `VIEW_FILE` | View a file: archives open in the contents viewer, text is paged |
| **T** | `TYPE_TEXT_FILE` | Page a file as plain text |
| **I** | `SHOWFILEINFO` | Print a file's full record |
| **K** | `CFG_FILECOLUMNS` | Choose the columns shown in file listings |
| **E** | `EDITFILERECORD` | Sysop review queue (`ACS: SYSOP`) |
| **X** | `WANTLIST` | File want list |
| **M** / **Q** / **G** | — | Message menu / main menu / log off |

### Listing files

Each caller has a **file listing mode**, chosen in the user Konfig editor
(`USERCONFIG`, the **K** key on the main menu) and defaulted by
`fileListingMode` in `config.json` (`lightbar` unless you change it):

- **Lightbar** — a full-screen list. Arrow keys move, **Space** marks a file
  for the batch, **Enter** runs the highlighted command-bar item, **I** shows
  the file's record, **V** views it, **D** downloads, **U** uploads, **Q**
  leaves. Sysops also get **E** (edit description), **K** (delete), **M**
  (move to another area) and **R** (rename on disk).
- **Classic** — a paged list drawn from the `FILELIST` templates, with as many
  files per page as the terminal height allows. **N** (or Enter) pages
  forward, **P** back, a number marks or unmarks that file, **D** downloads
  the marked files, **U** uploads, **V** views and **Q** quits.

The columns shown — size, date, download count, uploader, description — are
the caller's choice under **K** (`CFG_FILECOLUMNS`); `LISTFILES EXTENDED`
shows them all regardless.

### Downloading

`DOWNLOADFILE` and `BATCHDOWNLOAD` check the area's `acs_download`, let the
caller pick a protocol (or use the default when only one fits their
connection), and hand the session to the transfer program. ZModem 8k through
`sexyz` is the shipped protocol for both SSH and telnet; see
[File Transfer](files/file-transfer.md). Each successful download bumps the
file's download count and the caller's download count and byte total.

### Uploading

`UPLOADFILE` checks `acs_upload`, receives the files into a hidden
`.incoming-*` staging directory inside the area, then registers each one:

1. **Duplicate check** — a filename already in the area (compared ignoring
   case) is rejected and reported in the summary.
2. **ZipLab** — when the file's extension matches an enabled archiver and
   ZipLab is on, the archive is tested, extracted, optionally virus-scanned
   and tidied; its `FILE_ID.DIZ` becomes the description. A failed test or
   scan rejects the upload. See [ZipLab Upload Processing](files/ziplab.md).
3. **Description** — if ZipLab did not supply one, the caller is asked
   (cut to 60 characters; blank becomes `No description`).
4. **Record** — the file moves into the area, unreviewed, and the caller's
   upload count and byte total are credited.

### Viewing files and archives

`VIEW_FILE` (and **V** in either listing) decides what a file is by looking
inside it: the first bytes are compared with the `magic` signatures of the
**enabled** archivers in `archivers.json`, and only when nothing matches does
the extension decide. A ZIP named `NODELIST.Z75` therefore opens as an archive
rather than as a page of binary noise, while a text file called `README.ZIP`
pages as text. Disabling an archiver in the Configuration Editor (section
**9 — Archivers**) removes its format from detection.

The contents viewer is ZipLab's and lists ZIP archives only; other formats
page as text, so enable only the archivers you have tools for. In the viewer
the caller types an entry number to download that one member — the download
runs through the normal flow, re-checking the area's `acs_download` and
offering the protocol menu, so a cancelled transfer returns to the viewer
cleanly — or **Q** to quit. Text files have any trailing SAUCE record
stripped before display (see [SAUCE Metadata](files/sauce-metadata.md)) and
are refused over 4 MB.

### File newscan

`FILE_NEWSCAN` (**N**) lists files uploaded since the caller's previous
logon, grouped by area, in the areas tagged under `FILENEWSCANCONFIG` (**Z**)
or in every listable area when nothing is tagged. `FILE_NEWSCAN CURRENT`
scans only the current area. It draws from the `FILESCAN` templates
(`FILESCAN.TOP`, `.MID`, `.BOT` and `.AREA`); the shipped menu set has none,
so built-in defaults are used until you add your own, and a `FILESCAN.ANS`
header is shown when present.

`SETFILESCANDATE` (**Y**) lets the caller move the cutoff:

- a date (`MM/DD/YY`) — show files uploaded on or after it
- `A` — show all files regardless of date
- `R` — reset to the default ("since your previous logon")

The choice lasts only for the current session: it is stored on the user
record as `fileNewscanSince` and cleared at the next logon, so a caller who
once chose **A** is not shown every file forever. The prompt text is the
`fileScanDatePrompt` string. The message menu keeps its scan-date command on
**U**, which the file menu uses for Upload.

## Day-to-Day Operation

### Reviewing uploads

New uploads are recorded as unreviewed. `EDITFILERECORD` (**E**, sysop only)
walks the queue — all areas or the current one — and lets you edit the
description, move the file to another area (out of an UPLOADS drop box into
the public library, say) or delete it, marking each file reviewed as you go.
The lightbar listing's sysop keys do the same jobs one file at a time.

### Removing a file

In the lightbar listing, highlight the file and press **K**. The record and
the file on disk go together. To do it by hand instead, delete the file and
remove its entry from the area's `metadata.json`.

### Troubleshooting

**Files not showing** — check the caller's access against `acs_list`; make
sure the area's `metadata.json` is valid JSON (an array) and that the files
it names exist on disk. A file present on disk but absent from
`metadata.json` is invisible: add it with `helper files import`.

**Can't change areas** — the area must exist in `configs/file_areas.json`
and the caller must pass its `acs_list`. Both numbers and tags are accepted,
tags case-insensitively.

**Downloads fail** — `bin/sexyz` must be present and executable, the caller's
terminal must speak ZModem, and for a batch download at least one file must
be marked. See [File Transfer](files/file-transfer.md).

**An upload is rejected** — the summary says why: a duplicate name, or
`ZipLab processing failed` with the reason in the BBS log.

**A ZIP opens as text** — the `zip` archiver is disabled in `archivers.json`,
or the file's first bytes are not a ZIP signature (a corrupt archive still
opens in the viewer and reports an error there).

### Housekeeping

- Keep `metadata.json` and the files on disk in step; `helper files
  reextractdiz` refreshes descriptions from archives after you add tools.
- Set `acs_upload` so only validated callers can upload, and keep ZipLab's
  integrity test on.
- Back up `data/files/` (files and metadata together) and
  `configs/file_areas.json`.

## Technical Reference

### file_areas.json

File areas are defined in `configs/file_areas.json`, an array of objects:

```json
[
  {
    "id": 1,
    "tag": "GENERAL",
    "name": "General Files",
    "description": "General purpose file area",
    "path": "general",
    "acs_list": "",
    "acs_upload": "",
    "acs_download": ""
  }
]
```

| Key | Type | Default | Meaning |
| --- | --- | --- | --- |
| `id` | integer | required | Unique numeric identifier; file records refer to it |
| `tag` | string | required | Short tag, uppercase, unique |
| `name` | string | required | Display name |
| `description` | string | `""` | Area description |
| `path` | string | required | Directory relative to `data/files/`; absolute paths and `..` are rejected and the area skipped |
| `acs_list` | string | `""` | ACS required to list files (empty = public) |
| `acs_upload` | string | `""` | ACS required to upload |
| `acs_download` | string | `""` | ACS required to download |
| `conference_id` | integer | `0` | Conference the area belongs to (0 = ungrouped) |
| `network` | string | `""` | FTN network name in `ftn.json` feeding the area by TIC |
| `file_echo` | string | `""` | File echo tag whose TICs land here; with `network`, marks a file echo area |

### File storage and metadata.json

- Base directory: `data/files/` (under the configured data directory)
- Area directory: `data/files/{path}/`, created when the area is loaded
- Each file keeps its original name
- Each area's file list is `metadata.json` in its directory; a missing file
  means an empty area

Every writer (the BBS, `v3mail toss`, `helper files`) changes the list on
disk under a cross-process lock and the BBS re-reads an area's list when the
file has changed, so the processes can run side by side.

```json
[
  {
    "id": "11111111-1111-1111-1111-111111111111",
    "area_id": 1,
    "filename": "TEST1.TXT",
    "description": "A simple test text file.",
    "size": 21,
    "uploaded_at": "2024-01-01T10:00:00Z",
    "uploaded_by": "System",
    "download_count": 1
  }
]
```

| Key | Type | Default | Meaning |
| --- | --- | --- | --- |
| `id` | UUID string | required | Record identifier |
| `area_id` | integer | required | Must match the area's `id` |
| `filename` | string | required | Filename on disk (basename only) |
| `description` | string | `""` | Description shown in listings |
| `size` | integer | `0` | Size in bytes |
| `uploaded_at` | RFC 3339 time | required | Upload timestamp; the newscan compares against it |
| `uploaded_by` | string | `""` | Uploader's handle (an FTN address for files that arrived by TIC) |
| `download_count` | integer | `0` | Number of downloads |
| `reviewed` | bool | `false` | Set when a sysop has reviewed the file; unreviewed files make up the `EDITFILERECORD` queue |
| `crc32` | string | omitted | CRC-32 as 8 uppercase hex digits, recorded for files that arrived by TIC |

### Related settings

| File | Key | Meaning |
| --- | --- | --- |
| `config.json` | `fileListingMode` | Default listing mode for callers who have not chosen one: `lightbar` (default) or `classic` |
| `strings.json` | `fileScanDatePrompt` | Prompt text for `SETFILESCANDATE` |
| `archivers.json` | `magic`, `enabled` | Content signatures used to identify archives for viewing; see [archivers.json](configuration/configuration.md#archiversjson) |

### File display templates

File listings use templates in `menus/v3/templates/` (override them in
`menus.d/templates/`). A template named `X.TOP` may also be shipped as
`X.TOP.ANS`; both are found.

#### Area list templates

**FILEAREA.TOP** - Header before area list

```text
|07--- File Area List ---
```

**FILEAREA.MID** - Template for each area

```text
 |07[^ID] |15^TAG - ^NA |07(^NF files)
```

**FILEAREA.BOT** - Footer after area list

```text
|07--- End of List ---
```

#### File list templates

**FILELIST.TOP** - Header before file list

```text
|07--- File List Top ---
```

**FILELIST.MID** - Template for each file

```text
|15^MARK|07^NUM |11^NAME |07^DATE ^SIZE ^DESC
```

**FILELIST.BOT** - Footer with pagination (optional; the shipped set has none)

```text
|07Page ^PAGE of ^TOTALPAGES
```

**File List Pipe Codes & Placeholders** (usable in FILELIST.TOP and FILELIST.BOT):

| Code | Description |
| ---- | ----------- |
| `\|FCONFPATH` | Plain-text path "Conference > File Area Name" (e.g. `Local > General Files`). Colors should be applied in the template around the placeholder. Also available in menu ANSI files. |
| `\|FPAGE` | Current page text, e.g. `Page 1 of 3` |
| `\|FTOTAL` | Total file count in the current area |

**@-placeholder formats** (visual-width preserves ANSI art layout):

| Placeholder | Description |
| ----------- | ----------- |
| `@FCONFPATH@` | Conference path, value as-is (no width constraint) |
| `@FCONFPATH################################@` | Conference path padded/truncated to placeholder length (34 cols in this example) |
| `@FPAGE@` | Page text, value as-is |
| `@FPAGE###########@` | Page text in fixed-width field (17 cols in this example) |
| `@FTOTAL@` | File count, value as-is |
| `@FTOTAL######@` | File count in fixed-width field (12 cols in this example) |

**Alignment modifiers** (`|L` left, `|R` right, `|C` center) — same syntax as [message header placeholders](messages/placeholders.md):

| Placeholder | Description |
| ----------- | ----------- |
| `@FPAGE\|R#####@` | Right-justify page text (entire placeholder length = field width) |
| `@FTOTAL\|R#@` | Right-justify file count |
| `@FTOTAL\|C:5@` | Center file count in 5-char field (explicit `:N` width) |
| `@FCONFPATH\|C################################@` | Center conference path |

**Important:** `@-placeholders` are processed before `|pipe` codes, so `@FPAGE|R#####@` works correctly — the `|R` modifier is not consumed as a pipe code.

**Example FILELIST.TOP:**

```text
■ |04@FCONFPATH################################@|07 Files: |15@FTOTAL|R#@|07 @FPAGE|R#####@
```

This produces: `■ Local > General Files          Files:  1    Page 1 of 1` with colors applied by the surrounding pipe codes (`|04`, `|07`, `|15`).

#### Template variables

**Area Templates:**

- `^ID` - Area ID number
- `^TAG` - Area tag
- `^NA` - Area name
- `^DS` - Area description
- `^NF` - Number of files

**File Templates:**

- `^MARK` - Selection marker (*) or space
- `^NUM` - File number on page
- `^NAME` - Filename
- `^SIZE` - File size (shown as XXXk)
- `^DATE` - Upload date (MM/DD/YY format)
- `^DESC` - File description

#### File newscan templates

`FILESCAN.TOP`, `FILESCAN.MID`, `FILESCAN.BOT` and `FILESCAN.AREA` shape the
newscan output. None ship, so these built-in defaults apply:

```text
FILESCAN.TOP   |15File Newscan |07- new files since |11@DATE@|07  (then a rule)
FILESCAN.AREA  |11@AREA@ |07(@COUNT@ new)
FILESCAN.MID   |15^NAME |07^DATE ^SIZE |03^DESC
FILESCAN.BOT   (a rule)
```

`@DATE@` is the cutoff, `@AREA@` and `@COUNT@` the area name and its number
of new files; the `.MID` line takes the file template variables above.
