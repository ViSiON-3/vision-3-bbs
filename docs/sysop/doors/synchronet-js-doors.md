# Synchronet JavaScript Door Games

ViSiON/3 includes a built-in JavaScript engine that can run Synchronet BBS door games natively. Games run inside the BBS process with no external programs, no PTY bridging, and no Synchronet installation required.

## Overview

Synchronet BBS has a large library of JavaScript-based door games (LORD, Chicken Delivery, Synkroban, and many more). These games use Synchronet-specific JS APIs (`console.*`, `bbs.*`, `user.*`, `system.*`, `File`, `load()`) and a framework called **DORKit** for terminal I/O.

ViSiON/3 embeds [goja](https://github.com/dop251/goja), a pure-Go ES5.1+ JavaScript engine, and implements enough of Synchronet's API surface to run these games. DORKit, frame.js, recordfile.js, and other Synchronet JS libraries run unmodified.

### How It Works

1. User selects a Synchronet JS door from a menu
2. ViSiON/3 creates an isolated JavaScript runtime for the session
3. The game's JS files are loaded and executed directly in the BBS process
4. Game I/O flows through the user's SSH/telnet session with no intermediary
5. When the game exits (or the user disconnects), the BBS session resumes

Each connected user gets their own isolated JS runtime. There is no shared state between sessions except through the game's own data files, which use file locking for multi-node safety.

## Required Files from Synchronet

You do **not** need to install Synchronet or clone the full repository. You need three directories:

```
/opt/sbbs/
├── exec/
│   ├── load/          JS utility libraries (~176 files, ~2.8 MB)
│   └── dorkit/        DORKit framework (~13 files, ~92 KB)
└── xtrn/
    └── lord/          Game-specific files (varies per game)
```

### Where to Get Them

These files can be obtained from:

- **Synchronet Git repository**: [gitlab.synchro.net/main/sbbs](https://gitlab.synchro.net/main/sbbs) — clone or download just the `exec/load/`, `exec/dorkit/`, and `xtrn/{game}/` directories
- **An existing Synchronet installation**: copy the same directories from any working Synchronet BBS

### What Each Directory Contains

| Directory | Contents |
| --- | --- |
| `exec/load/` | Core JS libraries: dorkit.js, recordfile.js, sbbsdefs.js, graphic.js, and other shared utilities used by most games |
| `exec/dorkit/` | DORKit display/console abstraction: sbbs_console.js, screen.js, ansi_input.js, and related files that handle terminal I/O |
| `xtrn/{game}/` | Game-specific files: main JS script, data files, ANSI art, configuration — varies per game |

### What You Do NOT Need

- Synchronet binaries or compiled programs
- Synchronet source code (C/C++)
- The Synchronet `ctrl/` or `data/` directories
- Other game directories (only the specific games you want to run)
- Any Synchronet configuration files

## Configuration

### doors.json

Add a door entry with `"type": "synchronet_js"`:

```json
{
  "name": "LORDJS",
  "type": "synchronet_js",
  "script": "lord.js",
  "working_directory": "/opt/sbbs/xtrn/lord",
  "exec_dir": "/opt/sbbs/exec/",
  "library_paths": [
    "/opt/sbbs/exec/load",
    "/opt/sbbs/exec/load/dorkit"
  ],
  "single_instance": true,
  "min_access_level": 10
}
```

### Configuration Fields

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `name` | string | Yes | Door name (used in `DOOR:NAME` menu commands) |
| `type` | string | Yes | Must be `"synchronet_js"` |
| `script` | string | Yes | Main JS file to execute, relative to `working_directory` |
| `working_directory` | string | Yes | Absolute path to the game's data directory |
| `exec_dir` | string | Yes | Path to Synchronet's `exec/` directory (mapped to `system.exec_dir` in JS) |
| `library_paths` | []string | Yes | Search paths for `load()` and `require()` — include both `exec/load` and `exec/load/dorkit` |
| `single_instance` | bool | No | Only allow one node at a time (default: false) |
| `min_access_level` | int | No | Minimum access level required (default: 0) |
| `args` | []string | No | Arguments passed to the script as `argv` |

### Config Editor

You can also configure Synchronet JS doors through the interactive [Configuration Editor](../configuration/configuration.md#configuration-editor-tui) (`./config`, section 5 — Door Programs). Set the "Type" field to `synchronet_js` and fill in the Script, Exec Dir, and Library Paths fields.

## Menu Integration

Launch Synchronet JS doors the same way as any other door:

```
DOOR:LORDJS
```

See [Menus & ACS](../menus/menu-system.md) for details on adding door entries to menus.

## Module Resolution

When a game calls `load()` or `require()`, ViSiON/3 searches for the requested file in this order:

1. **Current script's directory** — the directory of the script that called `load()`
2. **`js.load_path_list`** — initialized from `library_paths`, but scripts can modify it at runtime
3. **Working directory** — the game's `working_directory`

This matches Synchronet's module resolution behavior. Games that dynamically modify `js.load_path_list` (which DORKit does) work correctly.

## Implemented API Surface

The following Synchronet API objects and functions are available to JS door games:

### Global Objects

| Object | Description |
| --- | --- |
| `console` | Terminal I/O: write, read, getkey, inkey, getstr, cursor movement, attributes, clear screen |
| `bbs` | BBS state: node_num, sys_status, online, get_time_left() |
| `user` | Current user: alias, name, number, security.level, settings, stats |
| `system` | System info: name, operator, exec_dir, data_dir, node_dir, timer |
| `server` | Server identification (stub for DORKit mode detection) |
| `client` | Client connection info (stub for DORKit mode detection) |
| `js` | Runtime: exec_dir, load_path_list, terminated, on_exit() |

### Classes

| Class | Description |
| --- | --- |
| `File` | File I/O: open, close, read, write, readln, writeln, readBin, writeBin, seek (via position property), lock/unlock, flush, truncate, INI file methods |
| `Queue` | Inter-thread message queue with session I/O fallback (used by DORKit for input buffering) |

### Global Functions

| Function | Description |
| --- | --- |
| `load(filename)` | Load and execute a JS module |
| `require(filename, symbol)` | Load a module and verify a symbol exists |
| `exit(code)` | Terminate the script |
| `ascii(str)` / `ascii_str(code)` | Character code conversion |
| `random(max)` | Random integer 0 to max-1 |
| `time()` | Current Unix timestamp |
| `sleep(ms)` | Sleep for milliseconds |
| `mswait(ms)` | Alias for sleep |
| `format(fmt, ...)` | sprintf-style formatting |
| `strftime(fmt, time)` | Date formatting |
| `file_exists(path)` | Check if a file exists |
| `file_remove(path)` | Delete a file |
| `file_rename(old, new)` | Rename a file |
| `file_mutex(path)` | Atomic lock file creation |
| `backslash(path)` | Ensure path ends with `/` |
| `truncsp(str)` | Trim trailing whitespace |
| `log(msg)` | Log a message |

## Encoding and ANSI Art

Synchronet JS games use CP437 encoding for ANSI art. ViSiON/3 preserves CP437 byte values through the JS string round-trip using Latin-1 mapping:

- **File reads**: Raw bytes are mapped to Unicode codepoints with the same numeric value (byte 0xDB = rune U+00DB)
- **Terminal output**: Runes are converted back to raw bytes and written directly to the session

This means ANSI art files display correctly without any encoding configuration.

## Multi-Node Support

Games that support multiple simultaneous players (like LORD) use file locking for data safety. ViSiON/3 implements:

- **Byte-range file locking** via `fcntl` for `File.lock()` / `File.unlock()`
- **Atomic lock files** via `file_mutex()` using `O_CREATE|O_EXCL`
- **Automatic lock file cleanup** when the JS engine exits (prevents stale locks from crashes)

Each node gets its own temporary directory (`system.node_dir`) for per-node state files.

## Limitations

- **ES5 only**: The goja engine supports ES5.1 with some ES6 features. Most Synchronet games are ES5-era and work fine. Games using modern ES6+ syntax may not work.
- **No `json-client.js`**: Games that require Synchronet's JSON database server are not yet supported.
- **No `conio` (local console)**: The local console module (for sysop-side display) is not implemented. Games detect this and skip local console initialization.
- **Mozilla extensions**: Some Mozilla/SpiderMonkey-specific syntax (like `for each...in`) is not supported by goja. When a module using this syntax fails to load, ViSiON/3 provides a stub object and logs a warning. This is sufficient for games like LORD where the failing module (cnflib.js) is not critical to gameplay.

## Troubleshooting

### Game exits immediately with no output

Check the ViSiON/3 log file for JS errors. Common causes:
- Wrong `library_paths` — make sure both `exec/load` and `exec/load/dorkit` are listed
- Wrong `exec_dir` — should point to the `exec/` directory, not `exec/load/`
- Missing game files in `working_directory`

### "module not found" errors

The requested JS file could not be found in any search path. Verify that:
- The file exists in one of the `library_paths` directories
- The `exec_dir` is set correctly (DORKit constructs paths like `system.exec_dir + "dorkit/"`)

### Stale lock file errors

If a game crashes or is forcefully terminated, lock files (`.lock`) may be left behind. ViSiON/3 automatically cleans up lock files created via `file_mutex()`, but some games create their own lock files independently. Delete any stale `.lock` files in the game's working directory.

### ANSI art displays incorrectly

Verify that your terminal supports CP437 encoding. Most modern SSH clients (SyncTERM, NetRunner, mTelnet) handle this correctly. If using a UTF-8 terminal, some block characters may not render as expected.

## Example: Setting Up LORD

1. **Get the files**:
   ```bash
   # Clone just the needed directories from Synchronet
   git clone --depth 1 --filter=blob:none --sparse \
     https://gitlab.synchro.net/main/sbbs.git /opt/sbbs
   cd /opt/sbbs
   git sparse-checkout set exec/load exec/dorkit xtrn/lord
   ```

2. **Add to doors.json**:
   ```json
   {
     "name": "LORDJS",
     "type": "synchronet_js",
     "script": "lord.js",
     "working_directory": "/opt/sbbs/xtrn/lord",
     "exec_dir": "/opt/sbbs/exec/",
     "library_paths": [
       "/opt/sbbs/exec/load",
       "/opt/sbbs/exec/load/dorkit"
     ],
     "single_instance": true
   }
   ```

3. **Add a menu entry** in your `.CFG` file:
   ```
   L,DOOR:LORDJS,*
   ```

4. **Connect and play** — select the door from your menu and LORD should start.
