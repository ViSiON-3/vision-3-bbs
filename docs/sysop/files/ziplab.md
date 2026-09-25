# ZipLab Upload Processing

ZipLab checks and tidies archives as users upload them. It tests each archive, extracts it, optionally virus-scans it, uses its `FILE_ID.DIZ` as the file description, strips other boards' ads, and stamps your own comment and ad file into it. While it works, the caller sees a status screen that ticks off each step.

ZipLab runs on any upload whose extension matches an **enabled** archiver in `archivers.json` (see [Archivers](configuration/configuration.md#archiversjson)). ZIP is handled natively; other formats use the external tools configured there.

## Configuring ZipLab

Use the [Configuration Editor](configuration/configuration.md#configuration-editor-tui), item **B — ZipLab Upload Processing**. It has three screens:

| Screen | Settings |
| --- | --- |
| **General** | Enabled, Run on Upload, Scan Failure (delete / quarantine), Quarantine Path |
| **Pipeline Steps** | An on/off switch for each step, plus the patterns, comment and include files |
| **Virus Scan** | Enabled, Command, Args, Timeout |

Each screen has a **Note** line that flags settings that will not work as they stand. Examples: quarantine with no path, the virus scan on with no scanner installed, or Extract off while steps that need it are on.

Settings are stored in `configs/ziplab.json`. The BBS re-reads the file each time a user uploads, so changes apply without a restart.

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

**Any non-zero exit counts as a failed scan.** That includes a scanner that is missing or not executable, or one that runs past the timeout. If the scan is on and the scanner is not installed, every upload fails. Test with a clean upload after turning it on.

To use the ClamAV daemon instead, which is much faster for repeated scans, set Command to `clamdscan` and Args to `["--fdpass", "--no-summary", "{WORKDIR}"]`.

### Failed scans

**Scan Failure** decides what happens to an upload that fails the scan:

- **delete** (default) — the upload is removed.
- **quarantine** — it is moved to **Quarantine Path**, which is created if needed. A relative path is relative to the BBS root. If Quarantine Path is empty, the upload is deleted instead.

## External archive formats

For formats other than ZIP, ZipLab runs the commands from `archivers.json`, with these placeholders filled in:

| Step | archivers.json command | Placeholders |
| --- | --- | --- |
| Test Integrity | `test` | `{ARCHIVE}` |
| Extract | `unpack` | `{ARCHIVE}`, `{OUTDIR}` |
| Add Comment | `comment` | `{ARCHIVE}`, `{FILE}` = the comment file |
| Include File | `addFile` | `{ARCHIVE}`, `{FILE}` = the include file |

A format with no command configured for a step fails that step. For Test Integrity or Extract, that rejects the upload.

## Status screen

While ZipLab runs, the caller sees `menus/v3/ansi/ZIPLAB.ANS`, with each step's progress drawn over it at positions defined in `menus/v3/ansi/ZIPLAB.NFO`. Each step has three entries: shown while **D**oing it, after it **P**asses, and if it **F**ails.

```
; step+state = column,row,normal color,highlight color,text
1D = 45,10,112,116,███
1P = 58,10,112,114,███
1F = 64,10,112,126,███
```

Colors are DOS attributes: background × 16 + foreground. Remove a step's lines to leave it off the screen; the step itself still runs. If `ZIPLAB.ANS` is missing, ZipLab runs without a status screen.

## ziplab.json

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

- `enabled` — master switch.
- `runOnUpload` — process archives as they are uploaded. Both this and `enabled` must be on.
- `scanFailBehavior` — `delete` or `quarantine`. Anything else deletes.
- `quarantinePath` — where quarantined uploads go.
- `steps` — one entry per step, as described above.

Keys that are missing keep their defaults. Older files may contain `archiveTypes` or `command`/`args` on steps other than the virus scan; these are ignored, and the editor drops them when it saves.
