# Bulk File Import

A file library is built faster from the command line than one upload at a
time. The `helper files` commands import whole directories into a file area
— a CD-ROM dump, a staging folder, a collection rescued from an old board —
and give every archive its `FILE_ID.DIZ` as the description, exactly as an
upload through ZipLab would. They work on the area's file list on disk under
the same lock the BBS uses, so you can run them while callers are online.

## Prerequisites

`helper` is built with the rest of the board by `build.sh` and sits at the
BBS root. To build it on its own:

```bash
go build -o helper ./cmd/helper/
```

Run it from the ViSiON/3 root directory (where `configs/` and `data/` are
located), or use the `--config` and `--data` flags to specify paths.

## Importing a Directory

`helper files import` imports all files from a source directory into one
file area, extracting `FILE_ID.DIZ` descriptions from supported archives:

```bash
./helper files import --dir /path/to/files --area GENERAL
```

The area is named by its tag, case-insensitively. Add `--dry-run` first to
see what would happen, `--preserve-dates` to keep each file's modification
time as its upload date (otherwise every file is dated now), `--move` to move
rather than copy, and `--uploader` to credit a handle other than `Sysop`.
The full flag list is in the [Technical Reference](#helper-files-import).

### Examples

Preview what would be imported:

```bash
./helper files import --dir /mnt/cdrom/files --area GENERAL --dry-run
```

Import files from a CD-ROM dump with original dates preserved:

```bash
./helper files import --dir /mnt/cdrom/utils --area UTILS --preserve-dates --uploader "Sysop"
```

Import and move files from a staging directory:

```bash
./helper files import --dir /tmp/incoming --area UPLOADS --move
```

### What the import does

**File scanning:**
- Scans the source directory (non-recursive, top level only)
- Skips directories, hidden files (dotfiles), and system files (`files.bbs`, `metadata.json`, `Thumbs.db`, `.DS_Store`, `desktop.ini`)

**Duplicate detection:**
- Compares filenames (case-insensitive) against the target area's existing `metadata.json`
- Duplicate files are skipped with a warning

**DIZ extraction:**
- For supported archive types (determined by `configs/archivers.json`), FILE_ID.DIZ is extracted and used as the file description
- ZIP files are read natively; other formats use their configured external extract commands
- DIZ search is case-insensitive (`FILE_ID.DIZ`, `file_id.diz`, etc.)
- Use `--no-diz` to skip extraction (descriptions will be empty)

**File transfer:**
- Default: files are **copied** to the area directory
- With `--move`: files are moved (renamed; falls back to copy+delete across filesystems)
- Source files are preserved unless `--move` is specified

**Metadata:**
- Each imported file gets a new UUID and `FileRecord` entry
- All new records are appended to the area's `metadata.json` in a single atomic write
- In `--dry-run` mode, no files are copied and no metadata is written

## Refreshing Descriptions

`helper files reextractdiz` re-scans the files already in one or all areas
and updates descriptions from the `FILE_ID.DIZ` found in archives. Use it
after an import with `--no-diz`, or after installing an archiver tool (say
`unrar`) so that formats you could not read before get their descriptions.

Re-extract DIZ for a specific area:

```bash
./helper files reextractdiz --area GENERAL
```

Preview changes across all areas:

```bash
./helper files reextractdiz --dry-run
```

It only processes files whose archiver is enabled in `configs/archivers.json`,
reports files missing from disk as `MISS`, updates a record only when the
extracted DIZ differs from the current description, and leaves non-archive
files unchanged.

## Typical Workflows

### Importing a CD-ROM or Archive Collection

```bash
# 1. Preview what will be imported
./helper files import --dir /mnt/cdrom/files --area GENERAL --preserve-dates --dry-run

# 2. Run the actual import
./helper files import --dir /mnt/cdrom/files --area GENERAL --preserve-dates

# 3. Verify — check the BBS file listing or metadata directly
cat data/files/general/metadata.json | python3 -m json.tool | head -20
```

### Setting Up a New File Area with Files

```bash
# 1. Create the area in ./config → 3 Areas and Conferences → File Areas,
#    then import into it (the area directory is created if needed)
./helper files import --dir ~/bbs-files/utils --area UTILS --uploader "Sysop"
```

A running BBS applies the new area once no callers are online, and shows the
imported files the next time the area is listed.

### Fixing Missing Descriptions

```bash
# Re-extract DIZ from all archives in all areas
./helper files reextractdiz

# Or target a specific area
./helper files reextractdiz --area GENERAL
```

## Supported Archive Formats

DIZ extraction supports all formats defined in `configs/archivers.json`:

| Format | Native | Notes |
|--------|--------|-------|
| ZIP    | Yes    | Built-in Go support, always available |
| 7Z     | No     | Requires `7z` binary |
| RAR    | No     | Requires `unrar` binary |
| ARJ    | No     | Requires `arj` binary |
| LHA/LZH | No   | Requires `lha` binary |

Native ZIP extraction reads FILE_ID.DIZ directly from the archive without
extracting to disk. Non-native formats extract to a temporary directory,
search for the DIZ file, then clean up. Only ZIP is enabled in the shipped
`archivers.json`; enable the others in the Configuration Editor (section
**9 — Archivers**) once their tools are installed.

## Troubleshooting

### "file area not found"

The `--area` tag must match a tag in `configs/file_areas.json` (case-insensitive). The error output lists the available areas.

### DIZ not extracted from non-ZIP archives

Ensure the archiver is enabled in `configs/archivers.json` and the required external binary (e.g., `unrar`, `7z`) is installed and in PATH.

### Files imported but not visible in BBS

The BBS re-reads an area's `metadata.json` when the file changes, so imported
files appear on the next listing. If they do not, check that the import wrote
to the same `--data` directory the BBS uses, and that the area's `path` in
`configs/file_areas.json` is the directory you expected.

### Permission errors during import

Ensure the helper command line utility has read access to the source directory and write access to the target area directory (`data/files/{area-path}/`).

## Technical Reference

### `helper files import`

```text
Usage: helper files import [options]

Options:
  --dir DIR          Source directory containing files to import (required)
  --area TAG         Target file area tag, e.g. GENERAL (required)
  --uploader NAME    Uploader handle (default: "Sysop")
  --data DIR         Data directory (default: "data")
  --config DIR       Config directory (default: "configs")
  --move             Move files instead of copying (default: copy)
  --dry-run          Show what would be imported without making changes
  --no-diz           Skip FILE_ID.DIZ extraction from archives
  --preserve-dates   Use file modification time as upload date (default: current time)
```

#### Output Format

```text
Import to: General Files (GENERAL)
Area path: data/files/general
Source:    /mnt/cdrom/files
Uploader:  Sysop
Files:     42 candidates
Mode:      COPY

  OK    COOLAPP.ZIP                              510.2 KB +DIZ
  OK    README.TXT                                 2.1 KB
  SKIP  EXISTING.ZIP                             (duplicate)
  ERR   BROKEN.ZIP                               permission denied

Summary: 40 imported, 1 skipped, 1 errors
```

Status codes:
- `OK` — file imported successfully (`+DIZ` if description was extracted)
- `SKIP` — file already exists in the area
- `ERR` — import failed (error shown)
- `WARN` — non-fatal issue (e.g., DIZ extraction failed but file still imported)
- `ADD` — would be imported (dry run only)

### `helper files reextractdiz`

```text
Usage: helper files reextractdiz [options]

Options:
  --area TAG     Specific file area tag (omit for all areas)
  --data DIR     Data directory (default: "data")
  --config DIR   Config directory (default: "configs")
  --dry-run      Show what would be updated without making changes
```

### Exit codes

Both commands exit `0` when they finish, including a dry run, and `1` when
the arguments are missing or invalid, the area or source directory cannot be
found, or the area's file list cannot be read or saved. Per-file problems are
reported as `ERR` or `WARN` lines and do not change the exit code.

### Files written

Imported files land in `data/files/{area-path}/` under their original names,
and each gets a record in that directory's `metadata.json` — see
[File Areas](files/file-areas.md#file-storage-and-metadatajson) for the
record format. Nothing else is touched.
