# SAUCE Metadata Handling

Much of the ANSI art a board collects carries a SAUCE record — SAUCE
(Standard Architecture for Universal Comment Extensions) is the small block
of metadata art groups have appended to their releases since the early 1990s,
naming the title, artist, group and font. It is invisible in an art viewer
but, sent raw to a terminal, it shows up as a trail of garbage after the
picture. ViSiON/3 strips SAUCE records automatically wherever it shows art
or text, so you can drop files straight from an art pack into your menu set
without editing them. There is nothing to configure.

## What is SAUCE?

SAUCE is a metadata format commonly embedded in ANSI art files. It contains information about the artwork such as:

- Title (35 characters)
- Author (20 characters)
- Group/Organization (20 characters)
- Creation Date (YYYYMMDD format)
- Font information (e.g., "IBM VGA")
- File type and data type information

## Where SAUCE Stripping is Applied

The SAUCE record is removed before display from:

- **Menu and login art** — every `.ANS` loaded through the menu system,
  including `PRELOGON.ANS` and its numbered variants, the pre-login matrix,
  `LOGIN.ANS`, and the art drawn by `DISPLAYFILE` and scripts
- **Templates** — the `.TOP`/`.MID`/`.BOT` templates for oneliners, last
  callers, who's online and system stats
- **Files callers view** — a text file paged by `VIEW_FILE` or
  `TYPE_TEXT_FILE` (see [File Areas](files/file-areas.md#viewing-files-and-archives))
- **FILE_ID.DIZ** — a DIZ with a SAUCE record (ad-pack style) gives a clean
  description when ZipLab or `helper files import` reads it
- **WFC backdrops** — art shown by the Waiting For Caller console

You can leave the SAUCE record in place in your own files; keeping it
preserves the artist credit for anyone who opens the file in an art viewer.

## Troubleshooting

**Garbage after an art file** on screen is almost never SAUCE. Check that the
file is CP437-encoded and that the terminal's encoding matches the caller's
setting; a file saved as UTF-8 by a modern editor shows the same symptom.

## Technical Reference

### Format Structure

A SAUCE record consists of:

1. **EOF Marker** (1 byte): `0x1A` (Ctrl-Z) - marks the end of the actual content
2. **Optional Comment Block**: May contain multiple 64-character comment lines
3. **SAUCE Record** (128 bytes): Starts with the signature "SAUCE00"

### Example

Before stripping:

```text
[ANSI art content]^Z SAUCE00 [metadata bytes...]
```

After stripping:

```text
[ANSI art content]
```

The EOF marker (^Z / 0x1A) and everything after it is removed.

### Implementation

The SAUCE stripping is implemented in `internal/ansi/ansi.go`:

- The `GetAnsiFileContent()` function automatically calls `stripSAUCE()` after reading the file
- Detection: Checks if the last 128 bytes start with "SAUCE"
- Removal: Searches backwards for the EOF marker (0x1A) and truncates the content before it
- Fallback: If no EOF marker is found, removes just the 128-byte SAUCE record

`ansi.StripSAUCE()` exposes the same routine for art bytes that do not come
from a file. The file viewer, templates and `FILE_ID.DIZ` reader use a
variant that also drops a preceding `COMNT` comment block.

### Testing

Tests are located in `internal/ansi/sauce_test.go` and cover:

- Files without SAUCE metadata (pass through unchanged)
- SAUCE with EOF marker (proper removal)
- SAUCE without EOF marker (fallback removal)
- Malformed SAUCE signatures (ignored)
- Files too small to contain SAUCE (ignored)

Run tests with:

```bash
go test ./internal/ansi -v -run TestStripSAUCE
```

### References

- [SAUCE Specification (Revision 5)](https://www.acid.org/info/sauce/sauce.htm)
- [ANSI Art Format Documentation](http://fileformats.archiveteam.org/wiki/ANSI_Art)
