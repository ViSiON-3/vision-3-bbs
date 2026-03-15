# Synchronet JavaScript Door Games

ViSiON/3 includes a built-in JavaScript engine that can run Synchronet BBS door games natively. Games run inside the BBS process with no external programs and no PTY bridging. The required Synchronet JS libraries and two example doors (LORD and LORD II) are included in the release bundle under `doors/sbbs/` — no separate download or installation is needed to get started.

## Overview

Synchronet BBS has a large library of JavaScript-based door games (LORD, LORD II, Chicken Delivery, Synkroban, and many more). These games use Synchronet-specific JS APIs (`console.*`, `bbs.*`, `user.*`, `system.*`, `File`, `load()`) and a framework called **DORKit** for terminal I/O.

ViSiON/3 embeds [goja](https://github.com/dop251/goja), a pure-Go ES5.1+ JavaScript engine, and implements enough of Synchronet's API surface to run these games. DORKit, frame.js, recordfile.js, and other Synchronet JS libraries run unmodified.

> **Note:** LORD and LORD II have been tested. Other Synchronet JS door games may work but are untested — please report any issues you encounter.

### How It Works

1. User selects a Synchronet JS door from a menu
2. ViSiON/3 creates an isolated JavaScript runtime for the session
3. The game's JS files are loaded and executed directly in the BBS process
4. Game I/O flows through the user's SSH/telnet session with no intermediary
5. When the game exits (or the user disconnects), the BBS session resumes

Each connected user gets their own isolated JS runtime. There is no shared state between sessions except through the game's own data files, which use file locking for multi-node safety.

## Included Files

The release bundle includes everything needed to run Synchronet JS doors under `doors/sbbs/`:

```
doors/sbbs/
├── exec/
│   └── load/          JS utility libraries (dorkit.js, recordfile.js, sbbsdefs.js, and more)
└── xtrn/
    ├── lord/          Legend of the Red Dragon
    └── lord2/         Legend of the Red Dragon II
```

To run other Synchronet JS doors, add the game's directory from the [Synchronet Git repository](https://gitlab.synchro.net/main/sbbs) (or an existing Synchronet installation) under `doors/sbbs/xtrn/` and add a configuration entry as described below.

## Configuration

### doors.json

Add a door entry with `"type": "synchronet_js"`:

```json
{
  "name": "LORDJS",
  "type": "synchronet_js",
  "script": "lord.js",
  "working_directory": "doors/sbbs/xtrn/lord",
  "exec_dir": "doors/sbbs/exec/",
  "library_paths": [
    "doors/sbbs/exec/load"
  ],
  "single_instance": true,
  "min_access_level": 10
}
```

### Configuration Fields

| Field               | Type     | Required | Description                                                              |
| ------------------- | -------- | -------- | ------------------------------------------------------------------------ |
| `name`              | string   | Yes      | Door name (used in `DOOR:NAME` menu commands)                            |
| `type`              | string   | Yes      | Must be `"synchronet_js"`                                                |
| `script`            | string   | Yes      | Main JS file to execute, relative to `working_directory`                 |
| `working_directory` | string   | Yes      | Path to the game's data directory (relative to BBS root or absolute)     |
| `exec_dir`          | string   | Yes      | Path to Synchronet's `exec/` directory (mapped to `system.exec_dir` in JS) |
| `library_paths`     | []string | Yes      | Search paths for `load()` and `require()` — `exec/load` contains all JS libraries including DORKit |
| `single_instance`   | bool     | No       | Only allow one node at a time (default: false)                           |
| `min_access_level`  | int      | No       | Minimum access level required (default: 0)                               |
| `args`              | []string | No       | Arguments passed to the script as `argv`                                 |

### Config Editor

You can also configure Synchronet JS doors through the interactive [Configuration Editor](configuration/configuration.md#configuration-editor-tui) (`./config`, section 5 — Door Programs). Set the "Type" field to `synchronet_js` and fill in the Script, Exec Dir, and Library Paths fields.

## Menu Integration

Launch Synchronet JS doors the same way as any other door:

```
DOOR:LORDJS
```

See [Menus & ACS](menus/menu-system.md) for details on adding door entries to menus.

## Module Resolution

When a game calls `load()` or `require()`, ViSiON/3 searches for the requested file in this order:

1. **Current script's directory** — the directory of the script that called `load()`
2. **`js.load_path_list`** — initialized from `library_paths`, but scripts can modify it at runtime
3. **Working directory** — the game's `working_directory`

This matches Synchronet's module resolution behavior. Games that dynamically modify `js.load_path_list` (which DORKit does) work correctly.

## Implemented API Surface

The following Synchronet API objects and functions are available to JS door games:

### Global Objects

| Object    | Description                                                                                 |
| --------- | ------------------------------------------------------------------------------------------- |
| `console` | Terminal I/O: write, read, getkey, inkey, getstr, cursor movement, attributes, clear screen |
| `bbs`     | BBS state: node_num, sys_status, online, get_time_left()                                    |
| `user`    | Current user: alias, name, number, security.level, settings, stats                          |
| `system`  | System info: name, operator, exec_dir, data_dir, node_dir, timer                            |
| `server`  | Server identification (stub for DORKit mode detection)                                      |
| `client`  | Client connection info (stub for DORKit mode detection)                                     |
| `js`      | Runtime: exec_dir, load_path_list, terminated, on_exit()                                    |

### Classes

| Class   | Description                                                                                                                                          |
| ------- | ---------------------------------------------------------------------------------------------------------------------------------------------------- |
| `File`  | File I/O: open, close, read, write, readln, writeln, readBin, writeBin, seek (via position property), lock/unlock, flush, truncate, INI file methods |
| `Queue` | Inter-thread message queue with session I/O fallback (used by DORKit for input buffering)                                                            |

### Global Functions

| Function                         | Description                              |
| -------------------------------- | ---------------------------------------- |
| `load(filename)`                 | Load and execute a JS module             |
| `require(filename, symbol)`      | Load a module and verify a symbol exists |
| `exit(code)`                     | Terminate the script                     |
| `ascii(str)` / `ascii_str(code)` | Character code conversion                |
| `random(max)`                    | Random integer 0 to max-1                |
| `time()`                         | Current Unix timestamp                   |
| `sleep(ms)`                      | Sleep for milliseconds                   |
| `mswait(ms)`                     | Alias for sleep                          |
| `format(fmt, ...)`               | sprintf-style formatting                 |
| `strftime(fmt, time)`            | Date formatting                          |
| `file_exists(path)`              | Check if a file exists                   |
| `file_remove(path)`              | Delete a file                            |
| `file_rename(old, new)`          | Rename a file                            |
| `file_mutex(path)`               | Atomic lock file creation                |
| `backslash(path)`                | Ensure path ends with `/`                |
| `truncsp(str)`                   | Trim trailing whitespace                 |
| `log(msg)`                       | Log a message                            |

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
- Wrong `library_paths` — should point to `exec/load` (all JS libraries including DORKit are in this directory)
- Wrong `exec_dir` — should point to the `exec/` directory, not `exec/load/`
- Missing game files in `working_directory`

### "module not found" errors

The requested JS file could not be found in any search path. Verify that:
- The file exists in one of the `library_paths` directories
- The `exec_dir` is set correctly (DORKit constructs paths like `system.exec_dir + "load/"`)

### Stale lock file errors

If a game crashes or is forcefully terminated, lock files (`.lock`) may be left behind. ViSiON/3 automatically cleans up lock files created via `file_mutex()`, but some games create their own lock files independently. Delete any stale `.lock` files in the game's working directory.

### ANSI art displays incorrectly

Verify that your terminal supports CP437 encoding. Most modern SSH clients (SyncTERM, NetRunner, mTelnet) handle this correctly. If using a UTF-8 terminal, some block characters may not render as expected.

## Example: Setting Up LORD and LORD II

LORD and LORD II are included in the bundle under `doors/sbbs/xtrn/lord/` and `doors/sbbs/xtrn/lord2/`. To enable them:

1. **Add to doors.json**:
   ```json
   {
     "name": "LORDJS",
     "type": "synchronet_js",
     "script": "lord.js",
     "working_directory": "doors/sbbs/xtrn/lord",
     "exec_dir": "doors/sbbs/exec/",
     "library_paths": [
       "doors/sbbs/exec/load"
     ],
     "single_instance": true
   },
   {
     "name": "LORD2JS",
     "type": "synchronet_js",
     "script": "lord2.js",
     "working_directory": "doors/sbbs/xtrn/lord2",
     "exec_dir": "doors/sbbs/exec/",
     "library_paths": [
       "doors/sbbs/exec/load"
     ],
     "single_instance": true
   }
   ```

2. **Add menu entries** in your `.CFG` file:
   ```
   L,DOOR:LORDJS,*
   2,DOOR:LORD2JS,*
   ```

3. **Connect and play** — select the door from your menu.

## Adding Other Synchronet JS Doors

To add a door beyond the included examples:

1. Obtain the game's `xtrn/{game}/` directory from the [Synchronet Git repository](https://gitlab.synchro.net/main/sbbs) or an existing Synchronet installation
2. Place it under `doors/sbbs/xtrn/{game}/`
3. Add a `synchronet_js` entry to `configs/doors.json` pointing to it
