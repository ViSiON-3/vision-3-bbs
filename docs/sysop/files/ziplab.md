# ZipLab Upload Processing

ZipLab checks and tidies archives as users upload them, the way the ZipLab
add-ons did for DOS boards in the 1990s. It tests each archive, extracts it,
optionally virus-scans it, uses its `FILE_ID.DIZ` as the file description,
strips other boards' ads, and stamps your own comment and ad file into it.
While it works, the caller sees a status screen that ticks off each step, if
your menu set has `ZIPLAB.ANS` (see [Status screen](#status-screen)). ZipLab
also provides the archive contents viewer that the file menu's **V** command
opens (see [Viewing archives](#viewing-archives)).

ZipLab runs on any upload whose extension matches an **enabled** archiver in
`archivers.json` (see [Archivers](configuration/configuration.md#archiversjson)).
ZIP is handled natively; other formats use the external tools configured there.

## Configuring ZipLab

Use the [Configuration Editor](configuration/configuration.md#configuration-editor-tui)
(`./config`), item **B — ZipLab Upload Processing**. It has three screens:

| Screen | Settings |
| --- | --- |
| **General** | Enabled, Run on Upload, Scan Failure (delete / quarantine), Quarantine Path |
| **Pipeline Steps** | Test Integrity, Extract, Virus Scan, DIZ / Remove Ads and its Patterns File, Add Comment and its Comment File, Include File and the File to Include |
| **Virus Scan** | Enabled, Command, Args, Timeout Secs |

Each screen has a **Note** line that flags settings that will not work as they
stand. Examples: quarantine with no path, the virus scan on with no scanner
installed, or Extract off while steps that need it are on.

Settings are stored in `configs/ziplab.json` (see
[ziplab.json](#ziplabjson)). The BBS re-reads the file each time a user
uploads, so changes apply without a restart.

## The pipeline

Steps run in this order. The numbers match `ZIPLAB.NFO` (see [Status screen](#status-screen)).

| # | Step | On failure |
| --- | --- | --- |
| 1 | **Test Integrity** — ZIP entries are read in full; other formats run the archiver's `test` command | Upload rejected |
| 2 | **Extract** — to a temporary directory, removed afterwards | Upload rejected |
| 3 | **Virus Scan** — runs your scanner over the extracted files | Upload rejected, then deleted or quarantined |
| 5 | **FILE_ID.DIZ / Remove Ads** — reads the DIZ, strips ad files | Logged, upload continues |
| 6 | **Add Comment** — sets the archive comment | Logged, upload continues |
| 7 | **Include File** — adds your ad file to the archive | Logged, upload continues |

Step 4 ("Strip AV") is a DOS-era step and is not implemented.

Steps 3 and 5 work on the extracted files, so **they are skipped when Extract is off**.

When a rejected upload fails, the caller sees `ZipLab processing failed` and the file is not added to the area. Details go to the BBS log.

When the archive carries a `FILE_ID.DIZ`, its text becomes the file description and the caller is not asked for one. If there is no DIZ, or step 5 is off, the caller types a description as usual.

## Support files

The patterns, comment and include files live in the `ziplab/` directory under the BBS root. Relative paths in the settings resolve against that directory; absolute paths are used as given.

Under Docker the directory is the `ziplab/` volume. The image carries the shipped files and copies any that are missing into it at startup, so edit them on the host through the `./ziplab` mount (see [Docker](getting-started/docker.md#persistent-data)).

| File | Default | Purpose |
| --- | --- | --- |
| Patterns file | `REMOVE.TXT` | Filenames to strip, one per line. Exact names, case-insensitive; lines starting with `;` are comments. |
| Comment file | `ZCOMMENT.TXT` | Text set as the archive comment. |
| Include file | `BBS.AD` | Added to every archive under its own filename. |

Ad files are removed from the extracted copy of every format, but from the uploaded archive itself only for ZIP.

## Virus scanning

The scan is off by default because it needs an external scanner. The default command is ClamAV:

| Setting | Default |
| --- | --- |
| Command | `clamscan` |
| Args | `["--stdout", "--no-summary", "{WORKDIR}"]` |
| Timeout | 120 seconds (0 means 60) |

In Args, `{WORKDIR}` is the directory of extracted files and `{FILE}` is the uploaded archive. The scanner runs with the extracted files as its working directory.

**Any non-zero exit counts as a failed scan.** That includes a scanner that is missing or not executable, or one that runs past the timeout. If the scan is on and the scanner is not installed, every upload fails, unless Extract is off: the scan needs the extracted files, so it is skipped then. Test with a clean upload after turning it on.

To use the ClamAV daemon instead, which is much faster for repeated scans, set Command to `clamdscan` and Args to `["--fdpass", "--no-summary", "{WORKDIR}"]`.

### Failed scans

**Scan Failure** decides what happens to an upload that fails the scan:

- **delete** (default) — the upload is removed.
- **quarantine** — it is moved to **Quarantine Path**, which is created if needed. A relative path is relative to the BBS root. If Quarantine Path is empty, the upload is deleted instead.

## External archive formats

For formats other than ZIP, ZipLab runs the commands from `archivers.json`.
Enable the format there (Configuration Editor, **9 — Archivers**) and make
sure its tool is installed; the placeholders each step fills in are listed
under [archivers.json commands](#archiversjson-commands-used-by-ziplab) below.
A format with no command configured for a step fails that step. For Test
Integrity or Extract, that rejects the upload.

## Viewing archives

ZipLab's contents viewer opens when a caller views an archive — **V** in the
file listings or the `VIEW_FILE` command. The file is recognised by its
content signature rather than its name (see
[Viewing files and archives](files/file-areas.md#viewing-files-and-archives)),
and the viewer lists ZIP archives: each member's number, size, date and name,
then a `ZipLab [#/Q]` prompt. Enter redraws the listing, **Q** returns to the
file menu, and a number downloads that one member.

A member download goes through the same path as any other download: the
area's download ACS is checked again and the protocol menu is shown before
anything is unpacked, so a refused or cancelled download extracts nothing,
and a transfer the caller aborts returns them to the viewer rather than
logging them off. The member is unpacked to a temporary file that is removed
after the transfer, members over 256 MB are refused, and a successful
transfer is credited to the caller's download byte total like a whole-file
download.

## Status screen

While ZipLab runs, the caller sees `menus/v3/ansi/ZIPLAB.ANS`, with each
step's progress drawn over it at positions defined in
`menus/v3/ansi/ZIPLAB.NFO`. Each step has three entries: shown while
**D**oing it, after it **P**asses, and if it **F**ails. Remove a step's lines
to leave it off the screen; the step itself still runs. If `ZIPLAB.ANS` is
missing, ZipLab runs without a status screen. The file format is under
[ZIPLAB.NFO](#ziplabnfo) below.

## Troubleshooting

**Every upload fails** — the virus scan is on and the scanner is missing or
not executable, or `archivers.json` has a format enabled whose tool is not
installed. The **Note** line on each editor screen names the problem; the
BBS log has the step and the command's output.

**RAR, 7z, ARJ or LHA uploads are rejected** — the format is enabled but its
`test` or `unpack` command is not installed, or the archiver has no command
configured for the step.

**The DIZ is not used** — Extract or DIZ / Remove Ads is off, or the archive
is not ZIP and its `unpack` command failed; a failed step 5 is logged and the
caller is asked for a description instead.

**Nothing is drawn while ZipLab runs** — `ZIPLAB.ANS` is missing from the
menu set, or `ZIPLAB.NFO` has no entries for the steps.

## Technical Reference

### ziplab.json

The Configuration Editor writes this file for you. For reference, the defaults are:

```json
{
  "enabled": true,
  "runOnUpload": true,
  "scanFailBehavior": "delete",
  "steps": {
    "testIntegrity": { "enabled": true },
    "extractToTemp": { "enabled": true },
    "virusScan": {
      "enabled": false,
      "command": "clamscan",
      "args": ["--stdout", "--no-summary", "{WORKDIR}"],
      "timeoutSeconds": 120
    },
    "removeAds":   { "enabled": true, "patternsFile": "REMOVE.TXT" },
    "addComment":  { "enabled": true, "commentFile": "ZCOMMENT.TXT" },
    "includeFile": { "enabled": true, "filePath": "BBS.AD" }
  }
}
```

| Key | Type | Default | Meaning |
| --- | --- | --- | --- |
| `enabled` | bool | `true` | Master switch |
| `runOnUpload` | bool | `true` | Process archives as they are uploaded; both this and `enabled` must be on |
| `scanFailBehavior` | string | `"delete"` | `delete` or `quarantine`; anything else deletes |
| `quarantinePath` | string | `""` | Where quarantined uploads go; relative to the BBS root. Empty means delete |
| `steps.testIntegrity.enabled` | bool | `true` | Step 1 |
| `steps.extractToTemp.enabled` | bool | `true` | Step 2; steps 3 and 5 need it |
| `steps.virusScan.enabled` | bool | `false` | Step 3 |
| `steps.virusScan.command` | string | `"clamscan"` | Scanner program |
| `steps.virusScan.args` | string[] | `["--stdout", "--no-summary", "{WORKDIR}"]` | Scanner arguments; `{WORKDIR}` = extracted files, `{FILE}` = the archive |
| `steps.virusScan.timeoutSeconds` | integer | `120` | Seconds before the scan counts as failed; `0` means 60 |
| `steps.removeAds.enabled` | bool | `true` | Step 5 (DIZ and ad removal) |
| `steps.removeAds.patternsFile` | string | `"REMOVE.TXT"` | Ad filenames to strip, relative to `ziplab/` |
| `steps.addComment.enabled` | bool | `true` | Step 6 |
| `steps.addComment.commentFile` | string | `"ZCOMMENT.TXT"` | Archive comment text, relative to `ziplab/` |
| `steps.includeFile.enabled` | bool | `true` | Step 7 |
| `steps.includeFile.filePath` | string | `"BBS.AD"` | File added to each archive, relative to `ziplab/` |

Keys that are missing keep their defaults. Older files may contain `archiveTypes` or `command`/`args` on steps other than the virus scan; these are ignored, and the editor drops them when it saves.

### archivers.json commands used by ZipLab

For formats other than ZIP, ZipLab runs these commands from `archivers.json`,
with the placeholders filled in:

| Step | archivers.json command | Placeholders |
| --- | --- | --- |
| Test Integrity | `test` | `{ARCHIVE}` |
| Extract | `unpack` | `{ARCHIVE}`, `{OUTDIR}` |
| Add Comment | `comment` | `{ARCHIVE}`, `{FILE}` = the comment file |
| Include File | `addFile` | `{ARCHIVE}`, `{FILE}` = the include file |

`{ARCHIVE}` is the uploaded archive's path and `{OUTDIR}` the temporary
extraction directory. Upload processing picks the format by the file's
extension against the enabled archivers; the contents viewer uses the
`magic` signatures.

### ZIPLAB.NFO

`menus/v3/ansi/ZIPLAB.NFO` places each step's status text on `ZIPLAB.ANS`.
Lines starting with `;` are comments. Each entry is the step number, a state
letter (`D` doing, `P` passed, `F` failed), then
`column,row,normal color,highlight color,text`:

```
; step+state = column,row,normal color,highlight color,text
1D = 45,10,112,116,███
1P = 58,10,112,114,███
1F = 64,10,112,126,███
```

Colors are DOS attributes: background × 16 + foreground. Step numbers are
1–3 and 5–7 as in [The pipeline](#the-pipeline).
