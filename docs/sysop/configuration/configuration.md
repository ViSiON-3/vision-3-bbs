# ViSiON/3 Configuration Guide

This guide covers the configuration files used by ViSiON/3 BBS. You can edit these files by hand or use the **TUI Configuration Editor** (see below).

## Configuration Editor (TUI)

ViSiON/3 includes an interactive TUI configuration editor modeled after ViSiON/2's `CONFIG.EXE`. It provides a menu-driven interface for managing all system configuration without editing JSON by hand.

### Running the Editor

```bash
# From the BBS directory (uses configs/ by default)
./config

# Or specify a custom config path
./config -config /path/to/configs
```

### Main Menu

The editor opens to a main menu. Keys **1**, **2**, **3**, and **4** open sub-menus; the rest go directly to record lists.

| Key | Section | What it covers |
|-----|---------|----------------|
| 1 | System Configuration | Opens 9 sub-screens: BBS identity, server setup, connection limits, access levels, default settings, IP lists, NUV, DOS emulation, logging |
| 2 | Areas and Conferences | Sub-menu: Message Areas, File Areas, Conferences |
| 3 | Echomail Networking | Sub-menu: Echomail Networks, Echomail Links, FTN Setup Wizard |
| 4 | ViSiON/3 Networking (V3Net) | Sub-menu: Node Identity, Subscriptions, Hosted Networks |
| 5 | Door Programs | External door program record list |
| 6 | Transfer Protocols | File transfer protocol record list |
| 7 | Archivers | Archive format record list |
| 8 | Event Scheduler | Automated event record list |
| 9 | Login Sequence | Login step record list |
| Q | Quit | Exit (prompts to save if there are unsaved changes) |

### System Configuration Sub-screens

Choosing **System Configuration** (key 1) opens an inner menu with nine numbered sub-screens, all writing to `configs/config.json`. Use Up/Down and Enter (or the sub-screen number) to navigate; Esc returns to the main menu.

| Sub-screen | Name | Fields |
|------------|------|--------|
| 0 | BBS Registration | Board Name, SysOp Name, BBS Location, Timezone |
| 1 | Server Setup | SSH enabled/host/port/legacy-algorithms, Telnet enabled/host/port, V3Net enabled + hub settings |
| 2 | Connection Limits | Max Nodes, Max Per IP, Failed Logins (0=off), Lockout Minutes, Idle Timeout, Transfer Timeout |
| 3 | Access Levels | SysOp Level, CoSysOp Level, Invisible Level, New User Level, Regular Level, Logon Level, Anonymous Level |
| 4 | Default Settings | Allow New Users (Y/N), File List Mode (lightbar/classic), Deleted User Retention Days |
| 5 | IP Blocklist/Allowlist | Blocklist Path, Allowlist Path |
| 6 | New User Voting (NUV) | Use NUV, Auto Add NUV, NUV Use Level, Yes/No vote thresholds, Validate/Kill on threshold, NUV Level, NUV Form |
| 7 | DOS Emulation | DOSemu Path |
| 8 | Logging | Log Directory, Min Level, Rolling Type, Cache Writes, Max Files, Max Size KB |

**Server Setup (sub-screen 1)** also writes to `configs/v3net.json` for the V3Net fields (keystore path, dedup DB path, registry URL, hub enabled/host/port/data dir/auto-approve).

### Areas and Conferences Sub-menu

| Item | What it edits | File written |
|------|---------------|--------------|
| Message Areas | Area tag, name, type (local/echomail/v3net/netmail), ACS, paths, conference, networking fields | `configs/message_areas.json` |
| File Areas | Area tag, name, description, path, ACS, conference | `configs/file_areas.json` |
| Conferences | Tag, name, description, ACS, display order | `configs/conferences.json` |

### Echomail Networking Sub-menu

| Item | What it edits |
|------|---------------|
| Echomail Networks | Global FTN paths (inbound, outbound, temp, bad/dupe tags) and per-network settings (own address, poll interval, tearline) |
| Echomail Links | Per-hub link settings (address, packet/session/AreaFix passwords, flavour) |
| FTN Setup Wizard | Guided flow: downloads a network's echolist, lets you browse and subscribe to areas, then writes `ftn.json`, `message_areas.json`, and `conferences.json` automatically |

### ViSiON/3 Networking (V3Net) Sub-menu

| Item | What it edits |
|------|---------------|
| Node Identity | View public key / mnemonic phrase, generate a new keypair, or recover from a seed phrase |
| Subscriptions | Add/edit hub subscriptions (hub URL, network name, poll interval, origin); includes a V3Net Area Browser and a Registry Browser for discovering networks |
| Hosted Networks | Manage networks your hub hosts, including their areas (add/edit/delete/rename with optional JAM base operations) |

### Navigation

| Key | Action |
|-----|--------|
| Up / Down | Move highlight |
| Enter | Select / activate field |
| I | Insert new record (record list screens) |
| D | Delete record (record list screens) |
| P | Reorder mode — Up/Down picks new position, Enter confirms |
| PgUp / PgDn | Previous/Next record from within an edit screen |
| Home / End | Jump to first/last item |
| Esc | Return to previous screen (or exit-confirm at top menu) |

### Saving

The editor tracks unsaved changes. When you quit with pending changes, you are prompted to save, and confirmed changes are then written to the appropriate configuration files.

## Configuration Files Overview

Configuration files are split between two directories:

**In `configs/` directory:**

- `strings.json` - Customizable text strings and prompts
- `doors.json` - External door program configurations
- `file_areas.json` - File area definitions
- `message_areas.json` - Message area definitions
- `conferences.json` - Conference grouping definitions
- `events.json` - Event scheduler configuration
- `config.json` - General BBS configuration
- `ftn.json` - FTN echomail configuration (networks, links, paths)
- `archivers.json` - Archive format definitions (ZIP, 7z, RAR, ARJ, LHA)
- SSH host keys (`ssh_host_rsa_key`, etc.)

**In `menus/v3/` directory (menu set):**

- `bar/PDMATRIX.BAR`, `cfg/PDMATRIX.CFG`, `mnu/PDMATRIX.MNU` - Pre-login matrix menu (see [Menu System Guide](menus/menu-system.md#pre-login-matrix-screen))
- `theme.json` - Theme color settings
- `ansi/PRELOGON.ANS` (or `PRELOGON.1`, `PRELOGON.2`, ...) - Pre-login ANSI screens shown before LOGIN (see [Menu System Guide](menus/menu-system.md#pre-login-ansi-files-prelogon))

**In `data/` directory:**

- `oneliners.json` - One-liner messages (JSON array)
- `oneliners.dat` - Legacy one-liner format (plain text, optional)

## strings.json

> *Use the [String Editor](advanced/string-editor.md) (`./strings`) to edit display strings interactively. It is a Go reimplementation of the original Vision/2 `STRINGS.EXE` utility. The JSON structure below is for reference.*

This file contains all the customizable text strings displayed by the BBS. You can modify these to personalize your system.

### Key String Categories

**Login/Authentication Strings:**

- `whatsYourAlias` - Login username prompt
- `whatsYourPw` - Login password prompt
- `systemPasswordStr` - System password prompt
- `wrongPassword` - Invalid password message

**User Interface Strings:**

- `pauseString` - Pause prompt (e.g., "Press Enter to continue")
- `defPrompt` - Default menu prompt
- `continueStr` - More prompt for paginated displays

**One-Liner Strings:**

- `askOneLiner` - Prompt asking whether to post a one-liner
- `oneLinerAnonymousPrompt` - Prompt asking whether to post anonymously
- `enterOneLiner` - Prompt for one-liner text entry
- `anonymousName` - Display label used when `anonymous=true` (for example, "Anonymous Coward")

**New User Strings:**

- `newUserNameStr` - New user alias prompt
- `createAPassword` - New user password creation
- `enterRealName` - Real name prompt
- `checkingUserBase` - Message shown while validating handle uniqueness

**Message System Strings:**

- `msgTitleStr` - Message title prompt
- `msgToStr` - Message recipient prompt
- `changeBoardStr` - Message area selection
- `postOnBoardStr` - Post confirmation

**File System Strings:**

- `changeFileAreaStr` - File area selection
- `downloadStr` - Download prompt
- `uploadFileStr` - Upload prompt

### Example Customization

```json
{
  "pauseString": "|15■|07■|08■ |B4|15SlAm eNtEr!|B0 |08■|07■|15■",
  "defPrompt": "|08•• |15|MN |08•• |13|TL |05Left: "
}
```

### Pipe Color Codes

The strings support pipe color codes:

- `|00-|15` - Standard 16 colors
- `|B0-|B7` - Background colors
- Special codes: `|CR` (carriage return), `|DE` (clear to end)

## doors.json

> *Use the [Configuration Editor](#configuration-editor-tui) (section 5 — Door Programs) to manage door settings interactively. The JSON structure below is for reference.*

Configures external door programs that can be launched from the BBS. The file contains an array of door configurations. See the [Door Programs Guide](doors/doors.md) for full documentation including DOS door setup, FOSSIL drivers, and dosemu2 configuration.

### Door Configuration Structure

```json
[
  {
    "name": "LORD",
    "commands": [
      "START.BAT {NODE}"
    ],
    "working_directory": "C:\\DOORS\\LORD",
    "dropfile_type": "DOOR.SYS",
    "dropfile_location": "node",
    "io_mode": "",
    "requires_raw_terminal": false,
    "use_shell": false,
    "single_instance": true,
    "min_access_level": 0,
    "cleanup_command": "",
    "cleanup_args": [],
    "environment_variables": {},
    "is_dos": true,
    "drive_c_path": "doors/drive_c",
    "dos_emulator": "dosemu",
    "fossil_driver": "C:\\UTILS\\X00.EXE eliminate",
    "dosemu_config": ""
  }
]
```

### Common Fields

- `name` - Unique identifier used in `DOOR:NAME` menu commands
- `commands` - Native: `[0]`=executable, `[1:]`=args. DOS: each entry is a batch command line
- `working_directory` - Native: Linux directory to run the command in. DOS: DOS path to cd into before running commands
- `dropfile_type` - Dropfile format: `DOOR.SYS`, `DOOR32.SYS`, `CHAIN.TXT`, `DORINFO1.DEF`, or blank
- `dropfile_location` - Where to write dropfile: `startup` (working dir) or `node` (per-node temp dir)
- `min_access_level` - Minimum user access level (0 = no restriction)
- `single_instance` - Only allow one node to run this door at a time
- `cleanup_command` / `cleanup_args` - Post-exit cleanup command (supports placeholders)

### Native Door Fields

- `io_mode` - I/O handling: `STDIO` (default) or `SOCKET`
- `requires_raw_terminal` - Allocate a PTY for raw terminal I/O
- `use_shell` - Wrap command in `/bin/sh -c`
- `environment_variables` - Additional environment variables

### DOS Door Fields

- `is_dos` - Set to `true` for DOS doors launched via dosemu2
- `drive_c_path` - Host path mounted as DOS C: drive (relative to BBS root, or absolute)
- `dos_emulator` - Emulator: `auto` (default) or `dosemu`
- `fossil_driver` - FOSSIL driver command (e.g., `C:\UTILS\X00.EXE eliminate`)
- `dosemu_config` - Custom `.dosemurc` config file path

### Available Placeholders

- `{NODE}` - Node number
- `{PORT}` - Port number
- `{TIMELEFT}` - Minutes remaining
- `{BAUD}` - Baud rate (simulated)
- `{USERHANDLE}` - User's handle
- `{USERID}` - User ID number
- `{REALNAME}` - User's real name
- `{LEVEL}` - Access level
- `{DROPFILE}` / `{NODEDIR}` - Linux paths to dropfile and its directory
- `{DOSDROPFILE}` / `{DOSNODEDIR}` - DOS paths (e.g., `C:\NODES\TEMP1\DOOR.SYS`)

## archivers.json

> *Use the [Configuration Editor](#configuration-editor-tui) (key 7 — Archivers) to manage archiver settings interactively. The JSON structure below is for reference.*

Defines archive formats and the external tools used to pack, unpack, test, and list them. This centralized configuration ensures all subsystems (ZipLab upload pipeline, file area management, archive viewing) use the same archiver definitions, and that different platforms can specify their preferred tool versions.

This follows the classic BBS pattern where archiver definitions are configured once system-wide rather than hardcoded.

### Default Configuration

If `archivers.json` is missing, built-in defaults are used (ZIP native via Go stdlib).

### Archiver Definition Structure

```json
{
    "archivers": [
        {
            "id": "zip",
            "name": "ZIP Archive",
            "extension": ".zip",
            "magic": "504B0304",
            "native": true,
            "enabled": true,
            "pack":    { "command": "zip",   "args": ["-j", "{ARCHIVE}", "{FILES}"] },
            "unpack":  { "command": "unzip", "args": ["-o", "{ARCHIVE}", "-d", "{OUTDIR}"] },
            "test":    { "command": "unzip", "args": ["-t", "{ARCHIVE}"] },
            "list":    { "command": "unzip", "args": ["-l", "{ARCHIVE}"] },
            "comment": { "command": "zip",   "args": ["-z", "{ARCHIVE}"] },
            "addFile": { "command": "zip",   "args": ["-j", "{ARCHIVE}", "{FILE}"] }
        }
    ]
}
```

### Archiver Field Descriptions

- `id` - Unique short identifier (e.g., "zip", "rar", "7z", "arj", "lha")
- `name` - Human-readable display name
- `extension` - Primary file extension including the dot (e.g., ".zip")
- `extensions` - Additional file extensions for this format (e.g., `[".lzh"]` for LHA)
- `magic` - Hex-encoded magic bytes at file offset 0 for format detection (e.g., "504B0304" for ZIP)
- `native` - When `true`, Go's built-in `archive/zip` stdlib is used for core operations and external commands are ignored for basic pack/unpack. Currently only ZIP supports native mode.
- `enabled` - Controls whether this archiver is active. Disabled archivers are skipped during detection and processing.
- `pack` - Command to create an archive. Placeholders: `{ARCHIVE}`, `{FILES}`, `{WORKDIR}`
- `unpack` - Command to extract an archive. Placeholders: `{ARCHIVE}`, `{OUTDIR}`
- `test` - Command to verify archive integrity. Placeholder: `{ARCHIVE}`
- `list` - Command to list archive contents. Placeholder: `{ARCHIVE}`
- `comment` - Command to add a comment to an archive. Placeholders: `{ARCHIVE}`, `{FILE}`
- `addFile` - Command to add a file to an existing archive. Placeholders: `{ARCHIVE}`, `{FILE}`

### Built-in Archiver Definitions

| ID    | Name            | Extension(s) | Magic Bytes    | Native | Default State |
| ----- | --------------- | ------------ | -------------- | ------ | ------------- |
| `zip` | ZIP Archive     | .zip         | `504B0304`     | Yes    | Enabled       |
| `7z`  | 7-Zip Archive   | .7z          | `377ABCAF271C` | No     | Disabled      |
| `rar` | RAR Archive     | .rar         | `526172211A07` | No     | Disabled      |
| `arj` | ARJ Archive     | .arj         | `60EA`         | No     | Disabled      |
| `lha` | LHA/LZH Archive | .lha, .lzh   | —              | No     | Disabled      |

To enable additional archive formats, set `"enabled": true` and ensure the corresponding external tool is installed on the system.

### FTN Bundle Note

FTN echomail bundles always use ZIP format (per FidoNet standard practice) and are handled natively by Go's `archive/zip` regardless of this configuration. This config applies to user-facing archive operations: file area uploads, archive viewing, ZipLab pipeline, etc.

## file_areas.json

> *Use the [Configuration Editor](#configuration-editor-tui) (key 2 → File Areas) to manage file area settings interactively. The JSON structure below is for reference.*

Defines file areas available on the BBS. The file contains an array of file area configurations.

### File Area Structure

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
    "acs_download": "",
    "conference_id": 1
  }
]
```

### File Area Field Descriptions

- `id` - Unique numeric identifier
- `tag` - Short tag for the area (uppercase)
- `name` - Display name
- `description` - Area description
- `path` - Subdirectory under `data/files/`
- `acs_list` - ACS string required to list files
- `acs_upload` - ACS string required to upload
- `acs_download` - ACS string required to download
- `conference_id` - Conference this area belongs to (0 or omitted = ungrouped)

### Access Control Strings (ACS)

- `s10` - Security level 10 or higher
- `fA` - Flag A must be set
- `!fB` - Flag B must NOT be set
- `s20&fC` - Level 20+ AND flag C
- `s10|fD` - Level 10+ OR flag D

## config.json

General BBS configuration. All settings in this file are managed through the **System Configuration** section of `./config` — you should not need to hand-edit it.

### TUI Paths

| Setting group | TUI path |
|---------------|----------|
| Board name, sysop name, location, timezone | System Configuration → BBS Registration (sub-screen 0) |
| SSH / Telnet ports and enabled flags | System Configuration → Server Setup (sub-screen 1) |
| Max nodes, per-IP limits, timeouts | System Configuration → Connection Limits (sub-screen 2) |
| Access levels (sysop, new user, logon, etc.) | System Configuration → Access Levels (sub-screen 3) |
| New user registration, file list mode, retention | System Configuration → Default Settings (sub-screen 4) |
| Blocklist / allowlist file paths | System Configuration → IP Blocklist/Allowlist (sub-screen 5) |
| NUV voting thresholds and behavior | System Configuration → New User Voting (sub-screen 6) |
| DOSemu binary path | System Configuration → DOS Emulation (sub-screen 7) |
| Log directory, level, rotation | System Configuration → Logging (sub-screen 8) |

### Field Reference

**BBS Registration:**

- `boardName` — BBS name displayed to users (default: `"ViSiON/3 BBS"`)
- `sysOpName` — Sysop's handle
- `bbsLocation` — Location string (city/region)
- `timezone` — IANA timezone for display formatting (e.g., `America/Los_Angeles`). If unset, falls back to `VISION3_TIMEZONE` env var, then `TZ`, then server local time.

**Server Setup:**

- `sshEnabled` — Enable/disable the SSH server (default: `true`)
- `sshHost` — Bind address (default: `"0.0.0.0"`)
- `sshPort` — TCP port (default: `2222`)
- `legacySSHAlgorithms` — Older SSH algorithms for retro clients like SyncTERM (default: `true`)
- `telnetEnabled` — Enable/disable the Telnet server (default: `false`)
- `telnetHost` — Bind address (default: `"0.0.0.0"`)
- `telnetPort` — TCP port (default: `2323`)

**Connection Limits:**

- `maxNodes` — Maximum simultaneous connections (default: `10`)
- `maxConnectionsPerIP` — Max concurrent connections from one IP (default: `3`)
- `maxFailedLogins` — Failed BBS logins from an IP before lockout (default: `5`, `0` = disabled)
- `lockoutMinutes` — Lockout duration in minutes (default: `30`)
- `sessionIdleTimeoutMinutes` — Idle session cutoff (default: `5`)
- `transferTimeoutMinutes` — File transfer timeout (default: `10`)

**Access Levels:**

- `sysOpLevel` — Security level for SysOp access (default: `255`)
- `coSysOpLevel` — Security level for Co-SysOp access (default: `250`)
- `invisibleLevel` — Level at which a user is invisible in the who's-online list (default: `0`, falls back to `coSysOpLevel`)
- `newUserLevel` — Level assigned to a brand-new account (default: `1`)
- `regularUserLevel` — Level for validated/regular users (default: `10`)
- `logonLevel` — Level granted on successful login (default: `10`)
- `anonymousLevel` — Level for guest/anonymous access (default: `5`, `0` = disabled)

**Default Settings:**

- `allowNewUsers` — Accept new user registrations (default: `true`)
- `fileListingMode` — `""` or `"lightbar"` (default) / `"classic"`
- `deletedUserRetentionDays` — Days to keep soft-deleted user records before `helper users purge` removes them (default: `30`, `-1` = keep forever)

**IP Blocklist/Allowlist:**

- `ipBlocklistPath` — Path to blocklist file (empty = disabled)
- `ipAllowlistPath` — Path to allowlist file (empty = disabled)

Both files are plain text: one IP or CIDR range per line, `#` for comments. They are watched for changes and reloaded automatically — no restart required. See [Security Guide](configuration/security.md) for format details.

**New User Voting (NUV):**

- `useNuv` — Enable the NUV system (default: `false`)
- `autoAddNuv` — Automatically queue new registrations for voting (default: `false`)
- `nuvUseLevel` — Minimum level to cast votes (default: `25`)
- `nuvYesVotes` — YES votes needed to trigger the yes threshold (default: `5`)
- `nuvNoVotes` — NO votes needed to trigger the no threshold (default: `5`)
- `nuvValidate` — Auto-validate when yes threshold is reached (default: `true`)
- `nuvKill` — Auto-soft-delete when no threshold is reached (default: `false`)
- `nuvLevel` — Level assigned to auto-validated users (default: `25`)
- `nuvForm` — Infoform number shown during NUV registration (default: `1`)

See [New User Voting](users/nuv.md) for full details.

**DOS Emulation:**

- `dosemuPath` — Path to the dosemu2 binary. ViSiON/3 calls it directly (bypassing the bash wrapper which mangles backslash arguments). Leave blank to use the default path.

See [Door Programs](doors/doors.md#running-dos-doors) for full DOS door setup documentation.

**Logging:**

- `logging.dir` — Directory for log files (default: `data/logs`)
- `logging.level` — Minimum log level: `DEBUG`, `INFO`, `WARN`, or `ERROR` (default: `INFO`)
- `logging.cache` — Buffer writes in an 8 KB cache; flushed on errors and clean exit (default: `true`)
- `logging.type` — Rotation mode: `0` = none, `1` = size-based, `2` = daily (default: `0`)
- `logging.maxFiles` — Numbered backups to keep (size mode) or days of files to retain (daily mode) (default: `5`)
- `logging.maxSizeKB` — Rotation threshold in KB; size mode only (default: `1024`)

See [Logging](#logging) for full details and examples.

### IP Blocklist/Allowlist Files

Both blocklist and allowlist files use the same format:

```text
# Comments start with #
# One IP or CIDR range per line

# Block specific IPs
192.168.1.100
10.0.0.50

# Block entire subnets
192.168.100.0/24
172.16.0.0/16

# IPv6 support
2001:db8::1
2001:db8::/32
```

**How it works:**

1. **Allowlist takes precedence**: If an IP is on the allowlist, it bypasses all other checks (blocklist, max nodes, per-IP limits)
2. **Blocklist checked next**: If an IP is on the blocklist, the connection is rejected
3. **Other limits apply**: If not on either list, normal connection limits apply

**Auto-Reload:**

- Files are **automatically monitored** for changes using file system watching
- When you edit and save either file, changes apply **within seconds** (no BBS restart needed)
- Debouncing (500ms) handles rapid successive edits
- All reloads are logged for debugging
- See [Security Guide](configuration/security.md#auto-reload-feature) for detailed usage

**Example setup:**

```json
{
  "ipBlocklistPath": "configs/blocklist.txt",
  "ipAllowlistPath": "configs/allowlist.txt"
}
```

Leave paths empty (`""`) to disable the feature.

## Logging

> *Use the [Configuration Editor](#configuration-editor-tui) (System Configuration → Logging) to manage logging settings interactively. The JSON structure below is for reference.*

ViSiON/3 writes structured JSON logs (one object per line) to a configurable directory. All settings live under the `"logging"` key in `configs/config.json` and are shared by every binary (`vision3`, `v3mail`). If the key is absent, defaults are applied automatically so existing installs continue to work unchanged.

### TUI: System Configuration → Logging

Open the configuration editor (`./config`), choose **System Configuration**, then navigate to **Logging** (sub-screen 8).

| Field | Description | Default |
|-------|-------------|---------|
| Log Directory | Directory for log files (absolute or relative to BBS root) | `data/logs` |
| Min Level | Minimum severity written: `DEBUG` / `INFO` / `WARN` / `ERROR` | `INFO` |
| Rolling Type | Rotation mode (see below) | `None` |
| Cache Writes | Buffer writes in an 8 KB cache; flushed on errors and clean exit | `Yes` |
| Max Files | Numbered backups (Size mode) or days of files to keep (Daily mode) | `5` |
| Max Size KB | File size in KB that triggers a rotation (Size mode only) | `1024` |

### Log Levels

| Level | What is written |
|-------|----------------|
| `DEBUG` | Everything — detailed trace output; use only for troubleshooting |
| `INFO` | Normal operation messages (recommended for production) |
| `WARN` | Warnings and errors only |
| `ERROR` | Errors only — quietest setting |

### Rolling Types

| Type | Behaviour |
|------|-----------|
| `None` (0) | Single file, no rotation — grows indefinitely. Simple; fine for low-traffic systems |
| `Size` (1) | Rotates when the file reaches **Max Size KB**. Old files shift: `vision3.log → vision3.log.1 → … → vision3.log.N`. Files beyond **Max Files** are deleted |
| `Daily` (2) | Opens a new dated file each calendar day (`vision3.YYYY-MM-DD.log`). Files older than **Max Files** days are pruned |

### Write Caching

When **Cache Writes** is enabled (the default), log lines are held in an 8 KB in-memory buffer before being flushed to disk. This reduces I/O on busy systems. The cache is flushed automatically:

- On every `ERROR`-level record (so critical messages are never lost on crash)
- On clean shutdown (`vision3` exit or SIGTERM)
- On a background ticker (approximately every 5 seconds)

Set to `No` to write every line through immediately. Useful during active debugging when you are `tail -f`-ing the log file.

### JSON Example

```json
{
  "logging": {
    "dir": "data/logs",
    "level": "INFO",
    "cache": true,
    "type": 1,
    "maxFiles": 7,
    "maxSizeKB": 2048
  }
}
```

This example uses Size rolling: the log rotates at 2 MB and keeps seven numbered backups.

### Log File Locations

Each binary writes to its own file within the configured directory:

| Binary | Log file |
|--------|----------|
| `vision3` | `<dir>/vision3.log` |
| `v3mail` | `<dir>/v3mail.log` |

The directory is created automatically if it does not exist.

### Log Format

Every line is a JSON object. Example:

```json
{"time":"2026-06-29T14:23:01Z","level":"INFO","msg":"user logged in","node":1,"handle":"AcidBurn"}
```

Fields present on every record:

- `time` — RFC 3339 UTC timestamp
- `level` — `DEBUG`, `INFO`, `WARN`, or `ERROR`
- `msg` — short lowercase description of the event

Additional structured attributes (node, user, error, etc.) vary by event.

## message_areas.json

> *Use the [Configuration Editor](#configuration-editor-tui) (key 2 → Message Areas) to manage message area settings interactively. The JSON structure below is for reference.*

Located in the `configs/` directory. Defines message areas available on the BBS.

See [Message Areas Guide](messages/message-areas.md) for detailed configuration.

## ftn.json

> *Use the [Configuration Editor](#configuration-editor-tui) (key 3 → Echomail Networks / Echomail Links) to manage FTN settings interactively. The JSON structure below is for reference.*

Located in the `configs/` directory. Configures the internal FTN tosser (v3mail) for echomail. Global fields include directory paths (`inbound_path`, `outbound_path`, `binkd_outbound_path`, `temp_path`) and routing tags (`bad_area_tag`, `dupe_area_tag`). Per-network fields include `own_address`, `internal_tosser_enabled`, `poll_interval_seconds`, and `tearline`. Per-link fields include `address`, `packet_password`, `areafix_password`, `name`, and `flavour`.

See [FTN Echomail Guide](messages/ftn-echomail.md) for setup and full field reference.

## conferences.json

> *Use the [Configuration Editor](#configuration-editor-tui) (key 2 → Conferences) to manage conference settings interactively. The JSON structure below is for reference.*

Located in the `configs/` directory. Defines conferences that group message areas and file areas together for organized display.

### Conference Structure

```json
[
  {
    "id": 1,
    "tag": "LOCAL",
    "name": "Local Areas",
    "description": "Local BBS discussion and file areas",
    "acs": ""
  }
]
```

### Conference Field Descriptions

- `id` - Unique numeric identifier (must be > 0; areas with `conference_id` of 0 or omitted are ungrouped)
- `tag` - Short tag name (uppercase)
- `name` - Display name shown in area listings
- `description` - Conference description
- `acs` - ACS string required to see this conference's areas (empty = visible to all)

### How Conference Grouping Works

Message areas and file areas each have an optional `conference_id` field that links them to a conference. When listing areas:

1. Areas with `conference_id` of 0 (or omitted) appear first as ungrouped
2. Areas belonging to conferences are grouped under a conference header
3. Conference ACS is checked — if a user doesn't meet the ACS requirement, the entire conference group is hidden
4. Individual area ACS still applies independently within each conference

### Conference Header Templates

Conference headers displayed in area listings use templates in `menus/v3/templates/`:

- `MSGCONF.HDR` - Header shown before each conference group in message area listings
- `FILECONF.HDR` - Header shown before each conference group in file area listings

Template placeholders:

- `^CN` - Conference name
- `^CT` - Conference tag

### ACS Codes for Conferences

Two ACS condition codes reference the user's current conference:

- `C` - Message conference (e.g., `C1` = user is in message conference 1)
- `X` - File conference (e.g., `X1` = user is in file conference 1)

The user's current conference is set automatically when they select an area.

### Graceful Degradation

If `conferences.json` is missing or empty, the system operates as before — area listings are flat with no conference headers.

## events.json

The event scheduler configuration file defines automated tasks that run on cron-style schedules.

See the complete [Event Scheduler Guide](advanced/event-scheduler.md) for detailed documentation.

### Basic Structure

```json
{
  "enabled": true,
  "max_concurrent_events": 3,
  "events": [
    {
      "id": "event_id",
      "name": "Event Name",
      "schedule": "*/15 * * * *",
      "command": "/path/to/command",
      "args": ["arg1", "arg2"],
      "working_directory": "/path/to/workdir",
      "timeout_seconds": 300,
      "enabled": true,
      "environment_vars": {
        "VAR_NAME": "value"
      }
    }
  ]
}
```

### Root Configuration

- **enabled** (boolean): Enable/disable the entire scheduler
- **max_concurrent_events** (integer): Maximum simultaneous event executions (default: 3)
- **events** (array): List of event configurations

### Event Configuration Fields

- **id** (string, required): Unique event identifier
- **name** (string, required): Human-readable name for logging
- **schedule** (string, required): Cron expression (e.g., `"*/15 * * * *"` or `"@hourly"`)
- **command** (string, required): Absolute path to executable
- **args** (array): Command-line arguments (each element is a separate argument)
- **working_directory** (string): Directory to run command in
- **timeout_seconds** (integer): Maximum execution time (0 = no timeout)
- **enabled** (boolean): Enable/disable this event
- **environment_vars** (object): Environment variables to set

### Cron Schedule Syntax

Standard 5-field cron format:

```text
┌─ minute (0-59)
│ ┌─ hour (0-23)
│ │ ┌─ day of month (1-31)
│ │ │ ┌─ month (1-12)
│ │ │ │ ┌─ day of week (0-6, Sunday=0)
│ │ │ │ │
* * * * *
```

**Examples:**
- `* * * * *` - Every minute
- `*/15 * * * *` - Every 15 minutes
- `0 3 * * *` - Daily at 3:00 AM
- `@hourly` - Every hour
- `@daily` - Once per day at midnight

### Available Placeholders

Use in command arguments or working_directory:

- `{TIMESTAMP}` - Unix timestamp
- `{EVENT_ID}` - Event identifier
- `{EVENT_NAME}` - Event name
- `{BBS_ROOT}` - BBS installation directory
- `{DATE}` - Current date (YYYY-MM-DD)
- `{TIME}` - Current time (HH:MM:SS)
- `{DATETIME}` - Date and time (YYYY-MM-DD HH:MM:SS)

### Common Use Cases

**FTN Mail Polling:**
```json
{
  "id": "ftn_poll",
  "schedule": "*/15 * * * *",
  "command": "/usr/local/bin/binkd",
  "args": ["-P", "21:4/158@fsxnet", "-D", "data/ftn/binkd.conf"],
  "timeout_seconds": 300,
  "enabled": true
}
```

**Daily Backup:**
```json
{
  "id": "backup",
  "schedule": "0 2 * * *",
  "command": "/usr/bin/tar",
  "args": ["-czf", "/backups/bbs-{DATE}.tar.gz", "{BBS_ROOT}/data"],
  "timeout_seconds": 7200,
  "enabled": true
}
```

See `templates/configs/events.json` for more examples.

## Menu Configuration

Menu files are located in `menus/v3/` with three components per menu:

### .MNU Files (Menu Definition)

Located in `menus/v3/mnu/`

Example `LOGIN.MNU`:

```text
RUN:FULL_LOGIN_SEQUENCE
COND:LI:GOTO:MAIN
HOTKEY:A:RUN:AUTHENTICATE
```

### .CFG Files (Menu Configuration)

Located in `menus/v3/cfg/`

Contains menu settings like:

- ACS requirements
- Password protection
- Display options

### .ANS Files (Menu Display)

Located in `menus/v3/ansi/`

ANSI art files displayed when the menu loads.

## Theme Configuration

The `menus/v3/theme.json` file controls color schemes:

```json
{
  "yesNoHighlightColor": 31,
  "yesNoRegularColor": 15
}
```

### Theme Fields

- `yesNoHighlightColor` - DOS color code for highlighted yes/no prompts
- `yesNoRegularColor` - DOS color code for regular yes/no prompts

Standard DOS color codes range from 0-255, where:

- 0-15: Standard 16 colors
- 16-231: Extended color palette
- 232-255: Grayscale

## oneliners.json

Located in the `data/` directory. Stores user-submitted one-liner messages displayed on the BBS.

### Structure

```json
[
  {
    "text": "first post from a hidden handle",
    "anonymous": true,
    "posted_by_username": "guest42",
    "posted_by_handle": "AcidBurn",
    "posted_at": "2026-02-13T17:30:00Z"
  },
  {
    "text": "long live the scene",
    "posted_by_username": "zerocool",
    "posted_by_handle": "ZeroCool",
    "posted_at": "2026-02-13T17:32:10Z"
  }
]
```

The file is a JSON array of one-liner objects. Each one-liner includes:

- `text` (displayed one-liner text, max 51 visible chars; pipe color codes are supported)
- `anonymous` (if true, on-screen display is anonymous)
- `posted_by_username` / `posted_by_handle` (actual poster identity for sysop traceability)
- `posted_at` (UTC RFC3339 timestamp)

Displayed name is derived automatically: `anonymousName` (from `strings.json`) when `anonymous=true`, otherwise `posted_by_handle` (fallback `posted_by_username`).

Legacy string-array entries are still read for backward compatibility and are normalized on write.

The system dynamically loads this file when displaying oneliners and saves new entries when users add them.

## SSH Host Keys

The `configs/` directory contains SSH host keys:

- `ssh_host_rsa_key` - RSA host key (required)
- `ssh_host_ed25519_key` - Ed25519 host key (optional)
- `ssh_host_dsa_key` - DSA host key (optional)

The RSA host key must be generated before starting the BBS:

```bash
cd configs
ssh-keygen -t rsa -f ssh_host_rsa_key -N ""
```

The BBS will fail to start if the host key is missing.

## Applying Configuration Changes

Most configuration changes take effect after a BBS restart. The TUI writes changes to disk when you save; restart `./vision3` to pick them up.

Exceptions that take effect without a restart:

- **IP blocklist/allowlist files** — watched by the BBS and reloaded automatically on save (no restart needed)
- **strings.json** — loaded fresh on each display

```bash
# Restart the BBS
# (Ctrl+C to stop, then:)
./vision3
```
