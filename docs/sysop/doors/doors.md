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
    "args": ["/N{NODE}", "/P{PORT}", "/T{TIMELEFT}"],
    "working_directory": "/opt/bbs/doors/lord",
    "dropfile_type": "DOOR.SYS",
    "io_mode": "STDIO",
    "requires_raw_terminal": true,
    "environment_variables": {
      "TERM": "ansi"
    }
  }
}
```

## Configuration Fields

### Native Doors

| Field                   | Description                                         |
| ----------------------- | --------------------------------------------------- |
| `name`                  | Display name for the door                           |
| `command`               | Path to the executable                              |
| `args`                  | Command-line arguments (supports placeholders)      |
| `working_directory`     | Directory to run the command in                     |
| `dropfile_type`         | Dropfile format: `DOOR.SYS`, `CHAIN.TXT`, or `NONE` |
| `io_mode`               | I/O handling: `STDIO`                               |
| `requires_raw_terminal` | Whether raw terminal mode is needed                 |
| `environment_variables` | Additional environment variables to set             |

### DOS Doors

Set `is_dos: true` to run a 16-bit DOS door game via dosemu2.

| Field           | Description                                                      |
| --------------- | ---------------------------------------------------------------- |
| `is_dos`        | `true` = DOS door                                                |
| `dos_commands`  | Array of DOS commands to run (supports placeholders)             |
| `drive_c_path`  | Host path mounted as DOS C: drive (default: `~/.dosemu/drive_c`) |
| `dos_emulator`  | Emulator selection: `""` or `"auto"` (default), `"dosemu"`       |
| `dosemu_path`   | Path to dosemu2 binary (default: `/usr/bin/dosemu`)              |
| `dosemu_config` | Custom `.dosemurc` config file path (optional)                   |

#### Running DOS Doors

There are currently two supported ways to run DOS doors with ViSiON/3:

1. **dosemu2 on Linux x86/x86-64** — The recommended approach for running 16-bit DOS doors. dosemu2 connects the door's COM1 serial port back to the user's SSH session via a PTY pair (`serial { virtual com 1 }`).

2. **32-bit Windows build** — A 32-bit Windows build of ViSiON/3 can natively launch 16-bit DOS door executables without any emulator. This relies on Windows' built-in NTVDM (NT Virtual DOS Machine) subsystem, which is only available on 32-bit editions of Windows.

> **Note:** 64-bit Windows does not support DOS doors, as NTVDM is not available on 64-bit editions.

#### Platform Support

| Platform           | DOS Door Support   |
| ------------------ | ------------------ |
| Linux x86 / x86-64 | Yes (dosemu2)      |
| Linux ARM / ARM64  | No                 |
| macOS              | No                 |
| Windows 32-bit     | Yes (native NTVDM) |
| Windows 64-bit     | No                 |

#### DOS Door Example

```json
{
  "name": "LORD",
  "is_dos": true,
  "dos_commands": [
    "cd C:\\DOORS\\LORD",
    "LORD.EXE /N{NODE} /T{TIMELEFT}"
  ],
  "drive_c_path": "/opt/bbs/drive_c",
  "dropfile_type": "DOOR.SYS"
}
```

## Placeholders

The following placeholders can be used in `args` (native doors) and `dos_commands` (DOS doors) and are substituted at runtime:

| Placeholder    | Value                        |
| -------------- | ---------------------------- |
| `{NODE}`       | Node number                  |
| `{PORT}`       | Port number                  |
| `{TIMELEFT}`   | Minutes remaining in session |
| `{BAUD}`       | Baud rate (simulated)        |
| `{USERHANDLE}` | User's handle                |
| `{USERID}`     | User ID number               |
| `{REALNAME}`   | User's real name             |
| `{LEVEL}`      | Access level                 |

## Menu Integration

Doors are launched via menu commands. Add a menu entry with the `DOOR` command type and specify the door key as the data parameter. See [Menus & ACS](../menus/menu-system.md) for details.
