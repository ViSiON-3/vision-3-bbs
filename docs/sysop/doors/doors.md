# Door Programs

Doors are external programs launched from the BBS. ViSiON/3 generates a dropfile, hands off the user's terminal to the door process, and resumes the BBS session when the door exits.

## Configuration

Use the [Configuration Editor](../configuration/configuration.md#configuration-editor-tui) (`./config`, section 5 — Door Programs) to add, edit, and remove door definitions interactively. This is the recommended approach.

### JSON Reference

Door programs are stored in `configs/doors.json`. Each entry is keyed by a unique name:

```json
{
  "lord": {
    "name": "Legend of the Red Dragon",
    "command": "lord.exe",
    "args": ["/N{NODE}", "/P{PORT}", "/T{TIMELEFT}", "/D{DROPFILE}"],
    "working_directory": "/opt/bbs/doors/lord",
    "dropfile_type": "DOOR.SYS",
    "dropfile_location": "startup",
    "io_mode": "STDIO",
    "requires_raw_terminal": true,
    "use_shell": false,
    "single_instance": false,
    "min_access_level": 0,
    "cleanup_command": "",
    "cleanup_args": [],
    "environment_variables": {
      "TERM": "ansi"
    }
  }
}
```

## Configuration Fields

### Common Fields

These fields apply to both native and DOS doors:

| Field | Type | Description |
| --- | --- | --- |
| `name` | string | Display name for the door |
| `dropfile_type` | string | Dropfile format: `DOOR.SYS`, `DOOR32.SYS`, `CHAIN.TXT`, `DORINFO1.DEF`, or `NONE` |
| `dropfile_location` | string | Where to write dropfile: `startup` (working dir, default) or `node` (per-node temp dir) |
| `min_access_level` | int | Minimum user access level required (0 = no restriction) |
| `single_instance` | bool | Only allow one node to run this door at a time |
| `cleanup_command` | string | Command to run after the door exits (optional) |
| `cleanup_args` | []string | Arguments for cleanup command (supports placeholders) |

### Native Door Fields

| Field | Type | Description |
| --- | --- | --- |
| `command` | string | Path to the executable |
| `args` | []string | Command-line arguments (supports placeholders) |
| `working_directory` | string | Directory to run the command in |
| `io_mode` | string | I/O handling: `STDIO` (default) or `SOCKET` |
| `requires_raw_terminal` | bool | Allocate a PTY for raw terminal I/O |
| `use_shell` | bool | Wrap command in `/bin/sh -c` (Linux) or `cmd /c` (Windows) |
| `environment_variables` | map[string]string | Additional environment variables to set |

### DOS Door Fields

Set `is_dos: true` to run a 16-bit DOS door game via dosemu2.

| Field | Type | Description |
| --- | --- | --- |
| `is_dos` | bool | `true` = DOS door |
| `dos_commands` | []string | Array of DOS commands to run (supports placeholders) |
| `drive_c_path` | string | Host path mounted as DOS C: drive (default: `~/.dosemu/drive_c`) |
| `dos_emulator` | string | Emulator selection: `""` or `"auto"` (default), `"dosemu"` |
| `dosemu_path` | string | Path to dosemu2 binary (default: `/usr/bin/dosemu`) |
| `dosemu_config` | string | Custom `.dosemurc` config file path (optional) |

## Placeholders

The following placeholders can be used in `args` (native doors), `dos_commands` (DOS doors), `cleanup_args`, and `environment_variables`. They are substituted at runtime:

| Placeholder | Value |
| --- | --- |
| `{NODE}` | Node number |
| `{PORT}` | Port number (same as node number) |
| `{TIMELEFT}` | Minutes remaining in session |
| `{BAUD}` | Baud rate (simulated, always 38400) |
| `{USERHANDLE}` | User's handle |
| `{USERID}` | User ID number |
| `{REALNAME}` | User's real name |
| `{LEVEL}` | User's access level |
| `{DROPFILE}` | Full path to the generated dropfile |
| `{NODEDIR}` | Directory containing the dropfile |
| `{STARTUPDIR}` | Resolved startup/working directory |
| `{DOSDROPFILE}` | DOS path to the dropfile (e.g., `C:\NODES\TEMP1\DOOR.SYS`) |
| `{DOSNODEDIR}` | DOS path to the node directory (e.g., `C:\NODES\TEMP1`) |

## I/O Modes

### STDIO (default)

Standard I/O redirection — the door's stdin/stdout/stderr are connected directly to the user's session. When `requires_raw_terminal` is `true`, a PTY is allocated for full raw terminal passthrough (recommended for most interactive doors).

### SOCKET

Creates a Unix socketpair and passes one end to the door process as file descriptor 3. The environment variable `DOOR_SOCKET_FD=3` is set automatically. The BBS bridges the other end bidirectionally to the user's session. Use this for doors that expect a raw socket handle rather than STDIO.

## Dropfile Location

By default (`dropfile_location: "startup"` or blank), the dropfile is written to the door's `working_directory`. Set `dropfile_location: "node"` to write it to a per-node temporary directory (`/tmp/vision3_nodeN/`) instead. This is useful for multi-instance doors where multiple nodes may run simultaneously and need isolated dropfiles.

For DOS doors, dropfiles are always written to the per-node temp directory inside `drive_c` regardless of this setting.

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

The cleanup command supports the same placeholders as door arguments. Cleanup failures are logged but do not affect the user's session.

Example:

```json
{
  "cleanup_command": "/opt/bbs/scripts/lord-cleanup.sh",
  "cleanup_args": ["{NODEDIR}", "{NODE}"]
}
```

## Use Shell

Set `use_shell: true` to wrap the door command in a shell (`/bin/sh -c` on Linux, `cmd /c` on Windows). This enables shell features like pipes, redirects, and globbing in the command line. Required for launching shell scripts (`.sh`, `.bat`, `.cmd` files) directly.

## Menu Integration

Doors are launched via menu commands:

- `DOOR:NAME` — Launch a specific door by name
- `LISTDOORS` — Display the list of available doors
- `OPENDOOR` — Prompt the user to enter a door name (supports `?` to list)
- `DOORINFO` — Show configuration details for a specific door

See [Menus & ACS](../menus/menu-system.md) for details on adding door entries to menus.

## Running DOS Doors

DOS doors are currently supported on Linux x86/x86-64 via dosemu2. dosemu2 connects the door's COM1 serial port back to the user's SSH session via a PTY pair (`serial { virtual com 1 }`).

32-bit editions of Windows include NTVDM (NT Virtual DOS Machine) which can natively run 16-bit DOS executables. ViSiON/3 does not yet implement NTVDM-based door launching, but it is planned for a future release.

> **Note:** 64-bit Windows does not include NTVDM and cannot run DOS doors.

### Platform Support

| Platform | DOS Door Support | Native Door Support |
| --- | --- | --- |
| Linux x86 / x86-64 | Yes (dosemu2) | Yes (STDIO/PTY/Socket) |
| Linux ARM / ARM64 | No | Yes (STDIO/PTY/Socket) |
| macOS | No | Yes (STDIO/PTY/Socket) |
| Windows 32-bit | Not yet (NTVDM planned) | Yes (STDIO only) |
| Windows 64-bit | No | Yes (STDIO only) |

### DOS Door Example

```json
{
  "name": "LORD",
  "is_dos": true,
  "dos_commands": [
    "cd C:\\DOORS\\LORD",
    "LORD.EXE /N{NODE} /T{TIMELEFT}"
  ],
  "drive_c_path": "/opt/bbs/drive_c",
  "dropfile_type": "DOOR.SYS",
  "single_instance": true,
  "cleanup_command": "/opt/bbs/scripts/reset-lord.sh",
  "cleanup_args": ["{NODE}"]
}
```

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

Additional variables can be configured per-door via the `environment_variables` field.
