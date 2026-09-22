# Door Programs

Doors are external programs launched from the BBS. ViSiON/3 generates a dropfile, hands off the user's terminal to the door process, and resumes the BBS session when the door exits.

This page is the reference for every door setting. **Setting up your first door?** Start with [Setting Up Doors](how-to-guides/doors.md), which walks through a DOS door, a native door, a Synchronet JS door and a VPL script step by step.

## Configuration

Use the [Configuration Editor](configuration/configuration.md#configuration-editor-tui) (`./config`, section 6 — Door Programs) to add, edit, and remove door definitions interactively. The JSON below is what the editor writes to `configs/doors.json`.

### Required Fields by Door Type

| Type | Minimum fields to set |
| --- | --- |
| Native external door | `Code`, `Name`, `Type=native`, `Commands`, `Dropfile Type` if the door reads one (see [Supported Dropfile Types](#supported-dropfile-types)) |
| DOS door (dosemu2) | `Code`, `Name`, `Type=dos`, `Commands`, `Dropfile Type` (usually `DOOR.SYS`), `Drive C Path`, optional `FOSSIL Driver` |
| Synchronet JS door | `Code`, `Name`, `Type=synchronet_js`, `Script`, `Working Dir`, `Exec Dir`, `Library Paths` |
| VPL script door | `Code`, `Name`, `Type=v3_script`, `Script`, `Working Dir` |

### Supported Dropfile Types

A dropfile is a small text file the BBS writes just before the door starts. It tells the door who is connected, how much time they have, and which node they are on. Pick the format your door's documentation asks for in the **Dropfile Type** field (`dropfile_type` in JSON).

| Dropfile Type | Origin | Lines | Filename written | Typically used by |
| --- | --- | --- | --- | --- |
| `DOOR.SYS` | PCBoard / GAP | 52 | `DOOR.SYS` | Most DOS door games (LORD, TradeWars, Usurper), many modern doors |
| `DOOR32.SYS` | Mystic / Synchronet | 11 | `DOOR32.SYS` | 32-bit and modern doors, Synchronet-style doors |
| `DORINFO1.DEF` | RBBS-PC / QuickBBS | 13 | `DORINFO1.DEF` | Older RBBS, QuickBBS, and RemoteAccess doors |
| `CHAIN.TXT` | WWIV | 30 | `CHAIN.TXT` | WWIV chain programs |
| `(none)` | — | — | nothing | Doors that only need environment variables or command-line placeholders |

All four formats carry the same core session data:

| Value | Source |
| --- | --- |
| Node number | Current node |
| Baud rate | Always `38400` (`115200` in `DORINFO1.DEF`, `9600` in `CHAIN.TXT`) |
| Real name and handle | User record |
| Security level | User access level |
| Time remaining | Session time left, in minutes or seconds depending on the format |
| User record number | User ID |
| BBS name | `boardName` from `config.json` |
| Screen size | User's saved screen height (and width in `CHAIN.TXT`), default 25 rows / 80 columns |
| Graphics | Always ANSI |

Fields the BBS does not track are filled with safe placeholders: phone numbers are `00-0000-0000`, the password is `SECRET`, dates are `01-01-1971`, and the sysop name in `DORINFO1.DEF` and `CHAIN.TXT` is `Sysop`. Doors that key their save data on the user record number, real name, or handle work as expected.

**How each door type handles dropfiles:**

- **Native doors** (Linux, macOS, Windows) write only the selected format. With `(none)` no file is written and `{DROPFILE}` expands to an empty string. The file is deleted when the door exits.
- **DOS doors** always write all four formats to the per-node directory (`C:\NODES\TEMPn\`). `Dropfile Type` only decides which file the `{DROPFILE}` and `{DOSDROPFILE}` placeholders point at; it defaults to `DOOR.SYS`. `Dropfile Location` and `Dropfile Case` are ignored for DOS doors.
- **Synchronet JS and VPL script doors** do not use dropfiles. The script runtime gets the session data directly. See [Synchronet JS Doors](doors/synchronet-js-doors.md).

**Filename case.** Native doors on case-sensitive filesystems sometimes look for `door32.sys` rather than `DOOR32.SYS`. Set **Dropfile Case** (`dropfile_case`) to `lower` for those doors. The default, `upper`, writes the conventional uppercase name.

**Where the file goes.** See [Dropfile Location](#dropfile-location) below. The short version: `startup` (the default) writes into the door's working directory, `node` writes into a fresh per-node temporary directory so several nodes can run the same door at once.

### Door Command Line

The **Commands** field (`commands` in JSON) is the command line the BBS runs when a user opens the door. It is entered differently depending on the door type:

| Door type | How to enter Commands in the config editor | Stored in JSON as |
| --- | --- | --- |
| Native | Executable followed by comma-separated arguments: `/opt/doors/tw2002/tw2002 -n, {NODE}, -d, {DROPFILE}` | `["/opt/doors/tw2002/tw2002", "-n", "{NODE}", "-d", "{DROPFILE}"]` |
| DOS | Comma-separated DOS batch lines: `START.BAT {NODE}, EXIT` | `["START.BAT {NODE}", "EXIT"]` |
| Synchronet JS / VPL | Not used. Set **Script** and **Script Args** instead | `script`, `args` |

For native doors the first token is the executable and every following comma-separated entry becomes exactly one argument, so a flag and its value are two entries (`-n, {NODE}`) unless the door wants them joined (`-n{NODE}`). Set **Use Shell** to `Yes` for a `.sh` / `.bat` script or a program that must start through the shell; the field does not interpret pipes, redirects or globs.

For DOS doors each entry becomes a line in the generated `EXTERNAL.BAT`, run after the FOSSIL driver loads, the screen clears, and the BBS changes into **Working Dir**.

#### Placeholders

These placeholders are substituted at runtime wherever they appear in **Commands**, the arguments of **Cleanup Command**, and **Env Vars** (`commands`, `cleanup_args`, `environment_variables`). The cleanup program path itself is not substituted. They are not substituted in **Script Args** for JS or VPL doors.

| Placeholder | Value | Available for |
| --- | --- | --- |
| `{NODE}` | Node number | All |
| `{PORT}` | Port number (same as node number) | All |
| `{TIMELEFT}` | Minutes remaining in the session | All |
| `{BAUD}` | Baud rate (simulated, always `38400`) | All |
| `{USERHANDLE}` | User's handle | All |
| `{USERID}` | User record number | All |
| `{REALNAME}` | User's real name | All |
| `{LEVEL}` | User's access level | All |
| `{STARTUPDIR}` | Resolved working directory (`.` when **Working Dir** is blank) | All |
| `{DROPFILE}` | Host path to the generated dropfile (empty when **Dropfile Type** is `(none)`) | Native, DOS |
| `{NODEDIR}` | Host directory containing the dropfile | Native, DOS |
| `{DOSDROPFILE}` | DOS path to the dropfile, e.g. `C:\NODES\TEMP1\DOOR.SYS` | DOS only |
| `{DOSNODEDIR}` | DOS path to the node directory, e.g. `C:\NODES\TEMP1` | DOS only |

Placeholders are plain text replacement, so add any separator the door needs yourself. A door that wants a trailing slash on a directory takes `{NODEDIR}/`.

Examples:

```json
"commands": ["/opt/doors/lord/lord", "-n{NODE}", "-p{NODEDIR}"]
```

```json
"commands": ["START.BAT {NODE} {DOSNODEDIR}"]
```

```json
"cleanup_command": "/opt/bbs/scripts/lord-cleanup.sh",
"cleanup_args": ["{NODEDIR}", "{NODE}"]
```

#### Menu commands

The menu commands that launch doors take no flags of their own. `DOOR:CODE` runs the door with that code, and `LISTDOORS`, `OPENDOOR`, and `DOORINFO` ignore anything after the command name. Anything the door needs on its command line goes in **Commands**. See [Menu Integration](#menu-integration).

### JSON Reference

Door programs are stored in `configs/doors.json` as an array.

> **Note:** The template `doors.json` ships with seven VPL script doors from `scripts/examples/`. The Synchronet JS runtime and the LORD and LORD II JavaScript games are included in the release bundle under `doors/sbbs/` but are not defined in the template; see [Set up a Synchronet JS door](how-to-guides/door-synchronet-js.md). DOS door games must be obtained separately from BBS archives; the LORD entry below is an example.

```json
[
  {
    "code": "LORD",
    "name": "Legend of the Red Dragon",
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

## Configuration Fields

### Common Fields

These fields apply to both native and DOS doors:

| Field | Type | Description |
| --- | --- | --- |
| `code` | string | Unique internal code (used in `DOOR:CODE` menu commands). Uppercase; A-Z, 0-9, `_`, `-`; max 16 chars |
| `name` | string | Display name shown to users (free-form, case preserved) |
| `commands` | []string | Native: `[0]`=executable, `[1:]`=args. DOS: each entry is a batch command line |
| `working_directory` | string | Native: Linux directory to run the command in. DOS: DOS path to `cd` into before running commands (e.g., `C:\DOORS\LORD`) |
| `type` | string | `synchronet_js`, `v3_script`, or blank for a native/DOS door (see `is_dos`) |
| `dropfile_type` | string | Dropfile format: `DOOR.SYS`, `DOOR32.SYS`, `CHAIN.TXT`, `DORINFO1.DEF`, or blank for none. See [Supported Dropfile Types](#supported-dropfile-types) |
| `dropfile_location` | string | Where to write dropfile: `startup` (working dir, default) or `node` (per-node temp dir). Native doors only |
| `dropfile_case` | string | Dropfile filename case: `upper` (default, `DOOR32.SYS`) or `lower` (`door32.sys`). Native doors only |
| `min_access_level` | int | Minimum user access level required (0 = no restriction) |
| `single_instance` | bool | Only allow one node to run this door at a time |
| `cleanup_command` | string | Command to run after the door exits (optional) |
| `cleanup_args` | []string | Arguments for cleanup command (supports placeholders) |
| `environment_variables` | map | Additional environment variables to set (supports placeholders). Format: `{"KEY": "VALUE"}`. Native doors only |

### Native Door Fields

These fields are used when `is_dos` is `false` (the default):

| Field | Type | Description |
| --- | --- | --- |
| `io_mode` | string | I/O handling: `STDIO` (default) or `SOCKET` |
| `requires_raw_terminal` | bool | Allocate a PTY for raw terminal I/O |
| `use_shell` | bool | Wrap command in `/bin/sh -c` (Linux) or `cmd /c` (Windows) |

### DOS Door Fields

Set `is_dos: true` to run a 16-bit DOS door game via dosemu2.

| Field | Type | Description |
| --- | --- | --- |
| `is_dos` | bool | `true` = DOS door launched via dosemu2 |
| `drive_c_path` | string | Host path mounted as DOS C: drive. Relative paths are resolved against the BBS root directory. Default: `doors/drive_c` (blank falls back to `~/.dosemu/drive_c`) |
| `dos_emulator` | string | Emulator selection: `""` or `"auto"` (default), `"dosemu"` |
| `fossil_driver` | string | DOS FOSSIL driver command to load before the door (e.g., `C:\UTILS\X00.EXE eliminate`). Loaded in EXTERNAL.BAT before `cls` and the door commands |
| `dosemu_config` | string | Path to a custom `.dosemurc` config file. If blank, the user's `~/.dosemu/.dosemurc` is used as a base with per-node overrides appended |

### Global DOS Settings

The dosemu2 binary path is configured globally in `config.json` (System Setup > DOS Emulation in the config editor), not per-door:

| Field | Type | Description |
| --- | --- | --- |
| `dosemuPath` | string | Path to the dosemu2 binary. Default: `/usr/libexec/dosemu2/dosemu2.bin`. ViSiON/3 calls the binary directly (bypassing the bash wrapper at `/usr/bin/dosemu` which mangles backslash arguments) |

## Placeholders

See [Door Command Line](#door-command-line) for the full placeholder table and examples.

## I/O Modes

### STDIO (default)

Standard I/O redirection — the door's stdin/stdout/stderr are connected directly to the user's session. When `requires_raw_terminal` is `true`, a PTY is allocated for full raw terminal passthrough (recommended for most interactive doors).

### SOCKET

Creates a Unix socketpair and passes one end to the door process as file descriptor 3. The environment variable `DOOR_SOCKET_FD=3` is set automatically. The BBS bridges the other end bidirectionally to the user's session. Use this for doors that expect a raw socket handle rather than STDIO.

## Dropfile Location

By default (`dropfile_location: "startup"` or blank), the dropfile is written to the door's `working_directory`. Set `dropfile_location: "node"` to write it to a fresh per-node temporary directory (named like `vision3_node1_XXXXXX` under the system temp directory) instead. The directory is removed when the door exits. This is useful for multi-instance doors where multiple nodes may run simultaneously and need isolated dropfiles.

For DOS doors, `dropfile_location` is ignored: all four dropfile formats are always written to the per-node directory inside `drive_c` (at `C:\NODES\TEMPn\`). Point the door game at that directory using the `{DOSNODEDIR}` or `{DOSDROPFILE}` placeholder on its command line.

## Access Control

Set `min_access_level` to restrict a door to users with a minimum access level. Users below the required level will see an "access denied" message. A value of `0` (default) means no restriction.

Doors with access restrictions are hidden from the door list for unauthorized users.

## Single Instance Locking

Set `single_instance: true` for doors that should not run concurrently on multiple nodes (e.g., single-player games with shared save files). When a second node tries to launch the same door, they will see a "door is in use" error message.

## Cleanup Command

The `cleanup_command` and `cleanup_args` fields specify an optional command to run after the door exits. This is useful for:

- Removing temporary files or lock files left by the door
- Processing score files or game results
- Resetting door state between sessions

Placeholders are replaced in `cleanup_args`; `cleanup_command` itself is used as written. Cleanup failures are logged but do not affect the user's session.

Example:

```json
{
  "cleanup_command": "/opt/bbs/scripts/lord-cleanup.sh",
  "cleanup_args": ["{NODEDIR}", "{NODE}"]
}
```

## Use Shell

Set `use_shell: true` to start the door through a shell (`/bin/sh` on Linux and macOS, `cmd` on Windows). The command and its arguments are passed through unchanged, so pipes, redirects and globs in the command line are not interpreted; put those in a wrapper script. Required for launching shell scripts (`.sh`, `.bat`, `.cmd` files) directly.

## Menu Integration

Doors are launched via menu commands:

- `DOOR:CODE` — Launch a specific door by its internal code
- `LISTDOORS` — Display the list of available doors
- `OPENDOOR` — Prompt the user to enter a door code (supports `?` to list)
- `DOORINFO` — Show configuration details for a specific door

See [Menus & ACS](menus/menu-system.md) for details on adding door entries to menus.

## Running Synchronet JavaScript Doors

ViSiON/3 can run Synchronet BBS JavaScript door games natively using a built-in JS engine. The required JS libraries and LORD/LORD II example doors are included in the bundle under `doors/sbbs/`. Set `"type": "synchronet_js"` in the door configuration.

See [Synchronet JS Doors](doors/synchronet-js-doors.md) for full setup instructions.

## Running DOS Doors

DOS doors are supported on Linux x86/x86-64 via dosemu2. ViSiON/3 uses dosemu2's terminal translator (`$_term_color`, `$_term_esc_char`) to convert DOS INT 10h screen output to ANSI escape sequences, which are bridged to the user's SSH session via a PTY.

### How It Works

1. ViSiON/3 generates a per-node `dosemurc` config that maps `drive_c_path` as the DOS C: drive
2. An `EXTERNAL.BAT` batch file is generated containing: FOSSIL driver loading (if configured), `cls`, `cd` to working directory (if set), and the user's `commands`
3. dosemu2 boots DOS, executes EXTERNAL.BAT, and exits via `exitemu`
4. The PTY output bridge gates on the `cls` clear screen sequence (`ESC[2J`), suppressing all DOS boot text from reaching the user
5. After the door exits, the BBS session resumes

### FOSSIL Driver

Most DOS BBS door games communicate via COM1 serial I/O using the FOSSIL (INT 14h) interface. Configure the `fossil_driver` field to load a FOSSIL driver (such as X00.EXE) before the door runs:

```json
"fossil_driver": "C:\\UTILS\\X00.EXE eliminate"
```

The FOSSIL driver is loaded in EXTERNAL.BAT before `cls` and the door commands. The `eliminate` parameter tells X00.EXE to remove any previous instance before installing.

dosemu2's `$_com1 = "virtual"` maps COM1 to the controlling PTY, enabling FOSSIL-based door games to communicate with the SSH session.

### dosemu2 Configuration

ViSiON/3 generates a per-node `dosemurc` file with these settings:

| Setting | Value | Purpose |
| --- | --- | --- |
| `$_hdimage` | `"<drive_c_path> +0 +1"` | Maps the door's drive_c as C:, includes system paths |
| `$_lredir_paths` | `"<node_path>"` | Allows access to the per-node directory |
| `$_video` | `"none"` | Disables graphical video output |
| `$_vga` | `"off"` | Disables VGA emulation |
| `$_term_color` | `(on)` | Enables ANSI terminal color translation |
| `$_term_esc_char` | `(27)` | Sets ESC as the ANSI escape character |

If a base `~/.dosemu/.dosemurc` exists, it is loaded first and these settings are appended. Key settings like `$_com1 = "virtual"` and CP437 character set configuration should be in the base dosemurc.

### Boot Text Suppression

dosemu2 outputs boot text (FreeDOS/comcom64 startup messages) through the terminal translator before EXTERNAL.BAT starts. ViSiON/3 suppresses this by gating PTY output: all output is buffered and discarded until the `cls` command in EXTERNAL.BAT produces an `ESC[2J` clear screen sequence. From that point on, all output is forwarded to the user. This gives the user a clean screen when the door starts.

### Platform Support

| Platform | DOS Door Support | Native Door Support |
| --- | --- | --- |
| Linux x86 / x86-64 | Yes (dosemu2) | Yes (STDIO/PTY/Socket) |
| Linux ARM / ARM64 | No | Yes (STDIO/PTY/Socket) |
| macOS | No | Yes (STDIO/PTY/Socket) |
| Windows 32-bit | Not yet (NTVDM planned) | Yes (STDIO only) |
| Windows 64-bit | No | Yes (STDIO only) |

### DOS Door Example

A complete LORD (Legend of the Red Dragon) configuration:

```json
{
  "code": "LORD",
  "name": "Legend of the Red Dragon",
  "working_directory": "C:\\DOORS\\LORD",
  "dropfile_type": "DOOR.SYS",
  "dropfile_location": "node",
  "single_instance": true,
  "is_dos": true,
  "commands": [
    "START.BAT {NODE}"
  ],
  "drive_c_path": "drive_c",
  "fossil_driver": "C:\\UTILS\\X00.EXE eliminate",
  "dos_emulator": "dosemu"
}
```

This configuration:

- Sets the DOS working directory to `C:\DOORS\LORD` (auto `cd` before commands)
- Generates a `DOOR.SYS` dropfile in the per-node directory (`C:\NODES\TEMPn\`)
- Loads the X00.EXE FOSSIL driver for COM1 serial I/O
- Runs `START.BAT` with the node number
- Prevents multiple nodes from running LORD simultaneously (`single_instance`)

### dosemurc Template

A recommended base `~/.dosemu/.dosemurc`:

```dosemu
$_cpu = "80486"
$_cpu_vm = "auto"
$_xms = (1024)
$_ems = (1024)
$_ems_frame = (0xe000)
$_external_char_set = "cp437"
$_internal_char_set = "cp437"
$_term_updfreq = (8)
$_layout = "us"
$_rawkeyboard = (auto)
$_mouse_internal = (on)
$_com1 = "virtual"
$_sound = (off)
```

The `$_external_char_set` and `$_internal_char_set` must be `"cp437"` for DOS door ANSI art to render correctly. The `$_com1 = "virtual"` setting maps COM1 to the controlling terminal for FOSSIL driver communication.

## Environment Variables

The following environment variables are automatically set for all door processes:

| Variable | Value |
| --- | --- |
| `BBS_USERHANDLE` | User's handle |
| `BBS_USERID` | User ID number |
| `BBS_NODE` | Node number |
| `BBS_TIMELEFT` | Minutes remaining |
| `LINES` | Terminal height |
| `COLUMNS` | Terminal width |
| `DOOR_SOCKET_FD` | Socket FD (SOCKET mode only) |
| `DOSEMU_QUIET` | `1` (DOS doors only, suppresses dosemu startup messages) |

Additional variables can be configured per-door via the `environment_variables` field (**Env Vars** in the config editor, entered as `KEY=VALUE, KEY2=VALUE2`). These are set in the OS environment before a native door process is launched. Placeholders are substituted at runtime. DOS doors receive only `DOSEMU_QUIET`; pass session data to a DOS door through the dropfile or the batch command line instead.

Example:

```json
{
  "environment_variables": {
    "TERM": "ansi",
    "DSZLOG": "/tmp/node{NODE}_dsz.log",
    "GAME_DATA": "/opt/bbs/doors/lord/data"
  }
}
```
