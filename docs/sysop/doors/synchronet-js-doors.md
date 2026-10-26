# Synchronet JavaScript Door Games

ViSiON/3 includes a built-in JavaScript engine that can run Synchronet BBS door games natively. Games run inside the BBS process with no external programs and no PTY bridging, so they work on every platform the BBS does, ARM boards and macOS included. The required Synchronet JS libraries and two example doors (LORD and LORD II) are included in the release bundle under `doors/sbbs/` — no separate download or installation is needed to get started.

The step-by-step setup is [Set up a Synchronet JS door](how-to-guides/door-synchronet-js.md). The `doors.json` keys, module resolution rules and the implemented API surface are in the [Technical Reference](#technical-reference) at the bottom.

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
│   ├── load/          JS utility libraries (recordfile.js, sbbsdefs.js, and more)
│   └── dorkit/        DORKit terminal I/O framework (sbbs_console.js, screen.js, etc.)
└── xtrn/
    ├── lord/          Legend of the Red Dragon
    └── lord2/         Legend of the Red Dragon II
```

To run other Synchronet JS doors, add the game's directory from the [Synchronet Git repository](https://gitlab.synchro.net/main/sbbs) (or an existing Synchronet installation) under `doors/sbbs/xtrn/` and add a door record as described below.

## Configuration

Define the door in the [Configuration Editor](configuration/configuration.md#configuration-editor-tui): run `./config`, press **7** for Door Programs, then **I** to insert a record. Set **Type** to `Synchronet JS` and fill in:

| Field | Value for the bundled LORD |
| --- | --- |
| Code | `LORDJS` |
| Name | `Legend of the Red Dragon (JS)` |
| Working Dir | `doors/sbbs/xtrn/lord` — the game's data directory |
| Script | `lord.js` — the main JS file, relative to Working Dir |
| Exec Dir | `doors/sbbs/exec/` — Synchronet's `exec/` directory |
| Library Paths | `doors/sbbs/exec/load, doors/sbbs/exec/dorkit` — comma-separated search paths for `load()` and `require()` |
| Script Args | blank unless the game reads `argv` |
| Single Instance | `Y` for games that keep flat-file save data, such as LORD |
| Min Access Level | as you like |

Both library paths are needed: the games load utility libraries from `exec/load` and the terminal framework from `exec/dorkit`. Press **Esc**, then **Q** and **Y** to save. The same record as JSON is under [doors.json](#doorsjson) in the Technical Reference.

## Menu Integration

Launch Synchronet JS doors the same way as any other door, with a `DOOR:CODE` command on a menu:

```
DOOR:LORDJS
```

Run `./menuedit`, open `DOORSM`, press **F5**, and set **Keys** to a free letter, **Command** to `DOOR:LORDJS` and **ACS** to `*`. See [Menus & ACS](menus/menu-system.md) for the menu editor and the `menus.d/v3/` overlay it saves into, and [Generated door menus](doors/generated-menu.md) for a menu that lists every door automatically.

## Example: Setting Up LORD and LORD II

LORD and LORD II are included in the bundle under `doors/sbbs/xtrn/lord/` and `doors/sbbs/xtrn/lord2/`. To enable them:

1. **Define the doors** in the config editor as in [Configuration](#configuration): one record with code `LORDJS`, script `lord.js` and working directory `doors/sbbs/xtrn/lord`, and one with code `LORD2JS`, script `lord2.js` and working directory `doors/sbbs/xtrn/lord2`. Both take the same **Exec Dir** and **Library Paths**, and both want **Single Instance**. The JSON for both is under [doors.json](#doorsjson).

2. **Add menu entries** with `./menuedit`, which saves them into the `menus.d/v3/` overlay, as in [Menu Integration](#menu-integration). The equivalent hand-edited CFG entries are under [Menu CFG entries](#menu-cfg-entries).

3. **Connect and play** — select the door from your menu. The first run creates the game's data files inside the game's directory.

## Adding Other Synchronet JS Doors

To add a door beyond the included examples:

1. Obtain the game's `xtrn/{game}/` directory from the [Synchronet Git repository](https://gitlab.synchro.net/main/sbbs) or an existing Synchronet installation
2. Place it under `doors/sbbs/xtrn/{game}/`
3. Add a `Synchronet JS` door record pointing to it, with **Working Dir** set to the new directory and **Script** set to the game's main file

Not every Synchronet game runs. The engine implements the parts of the Synchronet API that DORKit-based games use; see [Implemented API Surface](#implemented-api-surface) and [Limitations](#limitations).

## Multi-Node Support

Games that support multiple simultaneous players (like LORD) use file locking for data safety. ViSiON/3 implements:

- **Byte-range file locking** via `fcntl` for `File.lock()` / `File.unlock()`
- **Atomic lock files** via `file_mutex()` using `O_CREATE|O_EXCL`
- **Automatic lock file cleanup** when the JS engine exits (prevents stale locks from crashes)

Each node gets its own temporary directory (`system.node_dir`) for per-node state files.

A game is ended the same way as any other door when the caller hangs up, idles past their idle timeout or runs out of time; see [Ending a Door](doors/doors.md#ending-a-door). A caller with no time limit, and CoSysOps and above, are reported to the game as having an hour left that never counts down, so no game mistakes them for being out of time.

## Limitations

- **ES5 only**: The goja engine supports ES5.1 with some ES6 features. Most Synchronet games are ES5-era and work fine. Games using modern ES6+ syntax may not work.
- **No `json-client.js`**: Games that require Synchronet's JSON database server are not yet supported.
- **No `conio` (local console)**: The local console module (for sysop-side display) is not implemented. Games detect this and skip local console initialization.
- **Mozilla extensions**: Some Mozilla/SpiderMonkey-specific syntax (like `for each...in`) is not supported by goja. When a module using this syntax fails to load, ViSiON/3 provides a stub object and logs a warning. This is sufficient for games like LORD where the failing module (cnflib.js) is not critical to gameplay.

## Troubleshooting

### Game exits immediately with no output

Check the ViSiON/3 log file for JS errors. Common causes:
- Wrong `library_paths` — must include both `exec/load` and `exec/dorkit`
- Wrong `exec_dir` — should point to the `exec/` directory, not `exec/load/` or `exec/dorkit/`
- Missing game files in `working_directory`

### "module not found" errors

The requested JS file could not be found in any search path. Verify that:
- The file exists in one of the `library_paths` directories
- The `exec_dir` is set correctly (DORKit constructs paths like `system.exec_dir + "load/"`)

### Stale lock file errors

If a game crashes or is forcefully terminated, lock files (`.lock`) may be left behind. ViSiON/3 automatically cleans up lock files created via `file_mutex()`, but some games create their own lock files independently. Delete any stale `.lock` files in the game's working directory.

### ANSI art displays incorrectly

Verify that your terminal supports CP437 encoding. Most modern SSH clients (SyncTERM, NetRunner, mTelnet) handle this correctly. If using a UTF-8 terminal, some block characters may not render as expected.

## Technical Reference

### doors.json

A Synchronet JS door is a record in `configs/doors.json` with `"type": "synchronet_js"`. The keys shared by every door type are in the [Door Programs reference](doors/doors.md#doorsjson); `commands`, the dropfile keys and `environment_variables` are not used.

| Key | Type | Default | Meaning |
| --- | --- | --- | --- |
| `code` | string | required | Unique internal code (used in `DOOR:CODE` menu commands). Uppercase; A-Z, 0-9, `_`, `-`; max 16 chars |
| `name` | string | required | Display name shown to users (case preserved) |
| `type` | string | required | Must be `"synchronet_js"` |
| `script` | string | required | Main JS file to execute, relative to `working_directory` |
| `working_directory` | string | required | Path to the game's data directory (relative to BBS root or absolute) |
| `exec_dir` | string | required | Path to Synchronet's `exec/` directory (mapped to `system.exec_dir` in JS) |
| `library_paths` | []string | required | Search paths for `load()` and `require()` — include both `exec/load` (JS libraries) and `exec/dorkit` (DORKit terminal framework) |
| `args` | []string | none | Arguments passed to the script as `argv`. Placeholders are not substituted |
| `single_instance` | bool | `false` | Only allow one node at a time |
| `min_access_level` | int | `0` | Minimum access level required |

The bundled games, as they appear inside the top-level array of
`configs/doors.json`:

```json
[
{
  "code": "LORDJS",
  "name": "Legend of the Red Dragon (JS)",
  "type": "synchronet_js",
  "script": "lord.js",
  "working_directory": "doors/sbbs/xtrn/lord",
  "exec_dir": "doors/sbbs/exec/",
  "library_paths": [
    "doors/sbbs/exec/load",
    "doors/sbbs/exec/dorkit"
  ],
  "single_instance": true,
  "min_access_level": 10
},
{
  "code": "LORD2JS",
  "name": "LORD II: New World (JS)",
  "type": "synchronet_js",
  "script": "lord2.js",
  "working_directory": "doors/sbbs/xtrn/lord2",
  "exec_dir": "doors/sbbs/exec/",
  "library_paths": [
    "doors/sbbs/exec/load",
    "doors/sbbs/exec/dorkit"
  ],
  "single_instance": true
}
]
```

### Menu CFG entries

To add the menu entries by hand instead of with `menuedit`, copy the shipped file into the overlay first (`mkdir -p menus.d/v3/cfg && cp menus/v3/cfg/DOORSM.CFG menus.d/v3/cfg/`), then add these objects inside its top-level array, after a trailing comma on the entry before them:

```json
{ "KEYS": "L", "CMD": "DOOR:LORDJS", "ACS": "*", "HIDDEN": false },
{ "KEYS": "2", "CMD": "DOOR:LORD2JS", "ACS": "*", "HIDDEN": false }
```

### Module Resolution

When a game calls `load()` or `require()`, ViSiON/3 searches for the requested file in this order:

1. **Current script's directory** — the directory of the script that called `load()`
2. **`js.load_path_list`** — initialized from `library_paths`, but scripts can modify it at runtime
3. **Working directory** — the game's `working_directory`

This matches Synchronet's module resolution behavior. Games that dynamically modify `js.load_path_list` (which DORKit does) work correctly.

### Implemented API Surface

The following Synchronet API objects and functions are available to JS door games:

#### Global Objects

| Object    | Description                                                                                 |
| --------- | ------------------------------------------------------------------------------------------- |
| `console` | Terminal I/O: write, read, getkey, inkey, getstr, cursor movement, attributes, clear screen |
| `bbs`     | BBS state: node_num, sys_status, online, get_time_left()                                    |
| `user`    | Current user: alias, name, number, security.level, settings, stats                          |
| `system`  | System info: name, operator, exec_dir, data_dir, node_dir, timer                            |
| `server`  | Server identification (stub for DORKit mode detection)                                      |
| `client`  | Client connection info (stub for DORKit mode detection)                                     |
| `js`      | Runtime: exec_dir, load_path_list, terminated, on_exit()                                    |

#### Classes

| Class   | Description                                                                                                                                          |
| ------- | ---------------------------------------------------------------------------------------------------------------------------------------------------- |
| `File`  | File I/O: open, close, read, write, readln, writeln, readBin, writeBin, seek (via position property), lock/unlock, flush, truncate, INI file methods |
| `Queue` | Inter-thread message queue with session I/O fallback (used by DORKit for input buffering)                                                            |

#### Global Functions

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

`js.on_exit()` handlers run once, before the session closes, within a five-second budget, so a game's save or cleanup on exit is not lost to a late interrupt.

### Encoding and ANSI Art

Synchronet JS games use CP437 encoding for ANSI art. ViSiON/3 preserves CP437 byte values through the JS string round-trip using Latin-1 mapping:

- **File reads**: Raw bytes are mapped to Unicode codepoints with the same numeric value (byte 0xDB = rune U+00DB)
- **Terminal output**: Runes are converted back to raw bytes and written directly to the session

This means ANSI art files display correctly without any encoding configuration.
