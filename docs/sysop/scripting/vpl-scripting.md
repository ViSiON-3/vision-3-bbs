# VPL Scripting

VPL (Vision Programming Language) is ViSiON/3's built-in scripting system. It lets sysops write custom doors, utilities, and interactive features in JavaScript using a BBS-specific API — without external programs, compilers, or dependencies.

## What It Is

VPL scripts are JavaScript files that run inside the BBS process using the `v3.*` API. The engine is powered by [goja](https://github.com/dop251/goja), the same pure-Go JavaScript engine used for [Synchronet JS doors](doors/synchronet-js-doors.md), but with a clean Vision/3-native API instead of Synchronet's compatibility surface.

Each user who runs a VPL script gets their own isolated runtime. Scripts have access to terminal I/O, the user database, message areas, file areas, and persistent storage — all through the `v3.*` namespace.

### VPL vs. Synchronet JS Doors

| | VPL Scripts | Synchronet JS Doors |
|---|---|---|
| API namespace | `v3.*` | `console.*`, `bbs.*`, `user.*` |
| Purpose | Custom Vision/3 features | Run existing Synchronet games |
| Color codes | Vision/3 pipe codes (`\|07`, `\|15`) | Synchronet Ctrl-A codes |
| Door type | `v3_script` | `synchronet_js` |
| External files needed | None | Synchronet exec/load libraries |

Both use the same underlying JavaScript engine (goja) and the same session isolation model.

## Why It's Useful

- **No external tools** — scripts run inside the BBS process, no compilation or external runtime needed
- **Full BBS access** — read/write users, post messages, search files, persist data
- **Familiar language** — JavaScript (ES5.1+ with classes, arrow functions, template literals)
- **Per-user isolation** — each session gets its own runtime, no shared state leaks
- **Hot reload** — edit a script file, run it again immediately. No restart required
- **Persistent storage** — built-in key-value store for script data

### Common Use Cases

- Oneliners wall
- Voting booths
- Auto-message of the day
- System statistics displays
- Top callers / top posters
- Last 10 callers
- Custom user surveys
- BBS-specific mini-games
- Automated maintenance tasks

## Directory Structure

```
scripts/                       Root for all VPL scripts
├── data/                      Persistent data (v3.data storage)
│   ├── hello.json             Auto-created per-script JSON stores
│   └── voting.json
└── examples/                  Shipped example scripts
    └── hello.js
```

| Path | Purpose |
|------|---------|
| `scripts/` | Place your `.js` script files here. Organize freely with subdirectories. |
| `scripts/data/` | Automatic storage for `v3.data`. Each script gets a JSON file named after the script (e.g., `voting.json` for `voting.js`). Do not edit these manually while the BBS is running. |
| `scripts/examples/` | Shipped example scripts for reference. |

## Adding a VPL Script

### 1. Write the Script

Create a `.js` file in `scripts/` (or a subdirectory):

```javascript
// scripts/greeting.js
v3.console.clear();
v3.console.println("|11Welcome, |15" + v3.user.handle + "|11!");
v3.console.println("|07You have called |15" + v3.user.timesCalled + "|07 times.");
v3.console.println("|07There are |15" + v3.users.count() + "|07 users registered.");
v3.console.pause();
```

### 2. Add to doors.json

Add an entry to `configs/doors.json`:

```json
{
    "name": "GREETING",
    "type": "v3_script",
    "script": "greeting.js",
    "working_directory": "scripts"
}
```

Or use the **Configuration Editor** (`./config`, section 5 — Door Programs) and select "VPL Script" as the door type.

#### Door Config Fields

| Field | Required | Description |
|-------|----------|-------------|
| `name` | Yes | Door name used in `DOOR:NAME` menu commands (uppercase recommended) |
| `type` | Yes | Must be `"v3_script"` |
| `script` | Yes | Script filename, relative to `working_directory` |
| `working_directory` | Yes | Directory containing the script |
| `args` | No | Array of arguments available as `v3.args` in the script |
| `single_instance` | No | If `true`, only one node can run this door at a time |
| `min_access_level` | No | Minimum user access level required (0 = no restriction) |

### 3. Add to a Menu

Add a command entry to your menu's `.CFG` file (e.g., `menus/v3/cfg/DOORSM.CFG`):

```json
{
    "KEYS": "G",
    "CMD": "DOOR:GREETING",
    "ACS": "*",
    "HIDDEN": false,
    "NODE_ACTIVITY": "Running Greeting Script"
}
```

The user presses `G` from the doors menu, the script runs, and when it exits the menu redisplays automatically.

### 4. Test It

Connect to the BBS, navigate to the doors menu, and select the key you assigned. The script runs immediately — no restart needed. Edit the `.js` file and run it again to see changes.

## Script Arguments

Pass arguments to scripts via the `args` field in `doors.json`:

```json
{
    "name": "VOTING",
    "type": "v3_script",
    "script": "voting.js",
    "working_directory": "scripts",
    "args": ["topic1", "Best BBS Software"]
}
```

Access them in the script:

```javascript
var topic = v3.args[0];   // "topic1"
var title = v3.args[1];   // "Best BBS Software"
```

## Example: hello.js

The shipped example script demonstrates all major API areas:

```javascript
v3.console.clear();
v3.console.println("|09========================================");
v3.console.println("|11  Welcome to Vision/3 VPL Scripting!");
v3.console.println("|09========================================");

// Session info
v3.console.println("|07BBS Name:     |15" + v3.session.bbs.name);
v3.console.println("|07Node:         |15" + v3.session.node);

// Current user info
v3.console.println("|07Hello, |15" + v3.user.handle + "|07!");
v3.console.println("|07Access Level: |15" + v3.user.accessLevel);

// User database
v3.console.println("|07Total Users:  |15" + v3.users.count());

// Persistent storage
var visits = v3.data.get("visitCount") || 0;
visits++;
v3.data.set("visitCount", visits);
v3.console.println("|07Visit #|15" + visits);

// Interactive input
if (v3.console.yesno("|07Leave a message")) {
    v3.console.print("|07Message: |15");
    var msg = v3.console.getstr(60);
    v3.console.println("|10You said: |15" + msg);
}

v3.console.pause();
```

## Programming Reference

All VPL APIs are accessed through the `v3` global object. Functions that fail silently return `null`, `undefined`, `0`, or an empty string/array as noted. Functions that encounter serious errors (e.g., database write failures) throw a JavaScript error that can be caught with `try/catch`.

### v3.args

Array of string arguments passed via the `args` field in `doors.json`.

```javascript
v3.args[0]   // first argument
v3.args[1]   // second argument
v3.args.length
```

---

### v3.console

Terminal input/output. Output functions accept Vision/3 pipe codes (`|07`, `|15`, `|B0`, etc.) where noted.

#### Output

| Function | Description |
|----------|-------------|
| `write(text)` | Raw output, no pipe-code processing |
| `writeln(text)` | Raw output + newline, no pipe-code processing |
| `print(text)` | Output with pipe-code processing |
| `println(text)` | Output with pipe-code processing + newline |
| `clear()` | Clear screen and move cursor to top-left |
| `cls()` | Alias for `clear()` |
| `gotoxy(x, y)` | Move cursor to column `x`, row `y` (1-based) |
| `color(fg)` | Set foreground color by pipe-code number (0–15) |
| `color(fg, bg)` | Set foreground and background color |
| `reset()` | Reset all terminal color/style attributes |
| `center(text)` | Print text centered on screen (pipe-code aware) + newline |

#### Input

| Function | Returns | Description |
|----------|---------|-------------|
| `getkey()` | `string` | Wait for a single keypress, returns the character |
| `getkey(timeout_ms)` | `string` | Wait up to `timeout_ms` milliseconds. Returns `""` on timeout |
| `getstr(maxlen)` | `string` | Read a line of input up to `maxlen` characters |
| `getstr(maxlen, opts)` | `string` | Read with options object (see below) |
| `getnum(max)` | `number` | Read a number. Clamps to `max` if exceeded. Returns `0` on empty input |
| `yesno(prompt)` | `boolean` | Display prompt with `(Y/n)?` — returns `true` unless user presses N |
| `noyes(prompt)` | `boolean` | Display prompt with `(N/y)?` — returns `true` (No) unless user presses Y |
| `pause()` | — | Display `[Press any key]` and wait for keypress |

**getstr options object:**

```javascript
v3.console.getstr(40, { echo: false });    // hidden input (passwords)
v3.console.getstr(20, { upper: true });    // force uppercase
v3.console.getstr(5, { number: true });    // digits only
```

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `echo` | boolean | `true` | Set to `false` to hide input (password entry) |
| `upper` | boolean | `false` | Force input to uppercase |
| `number` | boolean | `false` | Accept digits only |

#### Properties

| Property | Type | Description |
|----------|------|-------------|
| `width` | number | Terminal width in columns |
| `height` | number | Terminal height in rows |

---

### v3.session

Read-only session and BBS information.

| Property | Type | Description |
|----------|------|-------------|
| `node` | number | Current node number |
| `startTime` | number | Session start time (Unix timestamp) |
| `timeLeft` | number | Seconds remaining in session (dynamic, recalculated on each access) |
| `online` | boolean | `true` if user is still connected (dynamic) |

#### v3.session.bbs

| Property | Type | Description |
|----------|------|-------------|
| `name` | string | BBS name |
| `sysop` | string | SysOp name |
| `version` | string | Vision/3 version string |

#### v3.session.user

Read-only snapshot of the current user taken at session start.

| Property | Type | Description |
|----------|------|-------------|
| `id` | number | User ID |
| `handle` | string | User handle |
| `realName` | string | Real name |
| `accessLevel` | number | Access level |
| `timesCalled` | number | Total login count |
| `location` | string | User location |
| `screenWidth` | number | Terminal width |
| `screenHeight` | number | Terminal height |

> **Note:** `v3.session.user` is a static snapshot. For live values, use `v3.user` instead.

---

### v3.user

Current user with live accessor properties and write support. All properties reflect the current in-memory state.

#### Properties (read-only)

| Property | Type | Description |
|----------|------|-------------|
| `id` | number | User ID |
| `handle` | string | User handle |
| `realName` | string | Real name |
| `accessLevel` | number | Access level |
| `timesCalled` | number | Total login count |
| `location` | string | User location |
| `messagesPosted` | number | Total messages posted |
| `uploads` | number | Number of uploads |
| `downloads` | number | Number of downloads |
| `filePoints` | number | File points balance |
| `validated` | boolean | Whether user is validated |

#### Methods

| Function | Description |
|----------|-------------|
| `set(field, value)` | Update a writable field. Changes are held in memory until `save()` |
| `save()` | Persist changes to the user database. Throws on error |

**Writable fields for `set()`:**

| Field | Type | Description |
|-------|------|-------------|
| `"realName"` | string | User's real name |
| `"location"` | string | User's location |
| `"screenWidth"` | number | Terminal width preference |
| `"screenHeight"` | number | Terminal height preference |

```javascript
v3.user.set("location", "New York, NY");
v3.user.save();
```

---

### v3.users

Read-only access to the user database. Returns safe fields only — no passwords or private data.

| Function | Returns | Description |
|----------|---------|-------------|
| `get(handle)` | object \| `null` | Look up user by handle |
| `getByID(id)` | object \| `null` | Look up user by numeric ID |
| `count()` | number | Total registered user count |
| `list()` | array | All users (safe fields only) |

**User object fields** (returned by `get`, `getByID`, `list`):

| Field | Type |
|-------|------|
| `id` | number |
| `handle` | string |
| `realName` | string |
| `accessLevel` | number |
| `timesCalled` | number |
| `location` | string |
| `messagesPosted` | number |
| `uploads` | number |
| `downloads` | number |
| `filePoints` | number |
| `validated` | boolean |
| `lastLogin` | number (Unix timestamp) |
| `createdAt` | number (Unix timestamp) |

---

### v3.data

Per-script persistent key-value storage. Data is stored as JSON in `scripts/data/<script-name>.json`. Values must be JSON-serializable (strings, numbers, booleans, arrays, plain objects).

| Function | Returns | Description |
|----------|---------|-------------|
| `get(key)` | any \| `undefined` | Read a value by key |
| `set(key, value)` | — | Write a value (creates the file if needed) |
| `delete(key)` | — | Remove a key |
| `keys()` | array | Array of all keys in the store |
| `getAll()` | object | Entire store as a key-value object |

```javascript
// Track visit count
var count = v3.data.get("visits") || 0;
count++;
v3.data.set("visits", count);

// Store complex data
v3.data.set("lastVisitor", {
    handle: v3.user.handle,
    time: Date.now()
});

// Clean up
v3.data.delete("oldKey");
```

---

### v3.message

Message area access — read, post, and search messages.

| Function | Returns | Description |
|----------|---------|-------------|
| `areas()` | array | List all message areas |
| `area(tag)` | object \| `null` | Get a message area by tag |
| `count(areaID)` | number | Message count in an area |
| `get(areaID, msgNum)` | object \| `null` | Get a specific message |
| `newCount(areaID)` | number | Unread message count for the current user |
| `post(areaID, opts)` | number | Post a message, returns message number. Throws on error |
| `postPrivate(areaID, opts)` | number | Post a private message, returns message number. Throws on error |
| `totalCount()` | number | Total messages across all areas |

**Message area object fields** (returned by `areas`, `area`):

| Field | Type | Description |
|-------|------|-------------|
| `id` | number | Area ID |
| `tag` | string | Area tag |
| `name` | string | Area display name |
| `description` | string | Area description |
| `type` | string | Area type (e.g., `"local"`, `"echo"`) |
| `echoTag` | string | FidoNet echo tag (if applicable) |
| `conferenceID` | number | Conference ID |

**Message object fields** (returned by `get`):

| Field | Type | Description |
|-------|------|-------------|
| `msgNum` | number | Message number within the area |
| `from` | string | Sender handle |
| `to` | string | Recipient handle |
| `subject` | string | Subject line |
| `body` | string | Message body text |
| `date` | number | Timestamp (Unix) |
| `msgID` | string | Unique message ID |
| `replyID` | string | ID of the message being replied to |
| `replyToNum` | number | Message number being replied to |
| `isPrivate` | boolean | Whether this is a private message |
| `areaID` | number | Area ID the message belongs to |

**Post options object** (for `post` and `postPrivate`):

| Field | Required | Default | Description |
|-------|----------|---------|-------------|
| `to` | No | `"All"` | Recipient handle |
| `subject` | No | `""` | Subject line |
| `body` | No | `""` | Message body |
| `replyTo` | No | `""` | Message ID being replied to |

```javascript
// Post a public message
v3.message.post(1, {
    to: "All",
    subject: "Hello from VPL",
    body: "This message was posted by a script!"
});

// Post a private message
v3.message.postPrivate(1, {
    to: "SysOp",
    subject: "Script Notification",
    body: "Something happened."
});
```

---

### v3.file

Read-only access to file areas and file listings.

| Function | Returns | Description |
|----------|---------|-------------|
| `areas()` | array | List all file areas |
| `area(tag)` | object \| `null` | Get a file area by tag |
| `list(areaID)` | array | List files in an area |
| `count(areaID)` | number | File count in an area |
| `search(query)` | array | Keyword search across all areas |
| `totalCount()` | number | Total files across all areas |

**File area object fields** (returned by `areas`, `area`):

| Field | Type | Description |
|-------|------|-------------|
| `id` | number | Area ID |
| `tag` | string | Area tag |
| `name` | string | Area display name |
| `description` | string | Area description |
| `conferenceID` | number | Conference ID |

**File record fields** (returned by `list`, `search`):

| Field | Type | Description |
|-------|------|-------------|
| `id` | string | File UUID |
| `areaID` | number | Area ID |
| `filename` | string | File name |
| `description` | string | File description |
| `size` | number | File size in bytes |
| `uploadedAt` | number | Upload timestamp (Unix) |
| `uploadedBy` | string | Uploader handle |
| `downloadCount` | number | Number of times downloaded |

---

### v3.fs

Sandboxed file operations. All paths are resolved relative to `scripts/data/`. Path traversal (`../`) outside the sandbox is blocked. Functions throw on error unless noted.

| Function | Returns | Description |
|----------|---------|-------------|
| `read(path)` | string | Read a text file. Throws if not found |
| `write(path, content)` | — | Write a text file (overwrites if exists, creates parent dirs) |
| `append(path, content)` | — | Append to a file (creates if not exists) |
| `exists(path)` | boolean | Check if file or directory exists |
| `delete(path)` | boolean | Delete a file. Returns `true` if deleted |
| `list(dir)` | array | List directory contents |
| `mkdir(path)` | — | Create a directory (and parents) |

**Directory listing entry fields** (returned by `list`):

| Field | Type | Description |
|-------|------|-------------|
| `name` | string | File or directory name |
| `isDir` | boolean | `true` if entry is a directory |
| `size` | number | File size in bytes (0 for directories) |

```javascript
// Write and read a log file
v3.fs.append("logs/activity.txt", v3.user.handle + " visited\n");
var log = v3.fs.read("logs/activity.txt");

// Check before reading
if (v3.fs.exists("config.txt")) {
    var cfg = v3.fs.read("config.txt");
}

// List files
var files = v3.fs.list("logs");
for (var i = 0; i < files.length; i++) {
    v3.console.println(files[i].name + " (" + files[i].size + " bytes)");
}
```

> **Security:** Scripts cannot access files outside `scripts/data/`. Attempting to use `../` to escape the sandbox will throw an error.

---

### v3.ansi

Display ANSI art files (.ANS) from scripts. Files are resolved by searching the script's working directory first, then `menus/v3/ansi/`, then `menus/v3/templates/`.

| Function | Description |
|----------|-------------|
| `display(filename)` | Read and display an .ANS file with pipe-code processing |
| `displayRaw(filename)` | Display an .ANS file without pipe-code processing |

SAUCE metadata is automatically stripped. Raw bytes are sent as-is to the terminal (no CP437-to-UTF-8 conversion).

```javascript
v3.ansi.display("welcome.ans");     // with pipe codes
v3.ansi.displayRaw("artwork.ans");  // raw ANSI only
```

---

### v3.util

Common utility functions for scripting.

| Function | Returns | Description |
|----------|---------|-------------|
| `sleep(ms)` | — | Pause execution for `ms` milliseconds. Respects disconnect |
| `random(max)` | number | Random integer from 0 to `max - 1` |
| `time()` | number | Current Unix timestamp (seconds) |
| `date()` | string | Current date/time as `"2006-01-02 15:04:05"` |
| `date(format)` | string | Current date/time in Go format (see below) |
| `padRight(str, width)` | string | Pad string on the right with spaces to `width` |
| `padRight(str, width, char)` | string | Pad with specified character |
| `padLeft(str, width)` | string | Pad string on the left with spaces to `width` |
| `padLeft(str, width, char)` | string | Pad with specified character |
| `center(str, width)` | string | Center string within `width` characters |
| `stripAnsi(str)` | string | Remove ANSI escape sequences |
| `stripPipe(str)` | string | Remove Vision/3 pipe codes |
| `displayLen(str)` | number | Visible display length (ignoring ANSI and pipe codes) |

**Date format** uses Go's reference time layout. Common patterns:

| Format String | Example Output |
|---------------|----------------|
| `"2006-01-02"` | `2026-03-14` |
| `"01/02/2006"` | `03/14/2026` |
| `"15:04:05"` | `14:30:00` |
| `"Mon Jan 2, 2006"` | `Sat Mar 14, 2026` |

```javascript
// Pause for 2 seconds
v3.util.sleep(2000);

// Random number 1-6 (dice roll)
var roll = v3.util.random(6) + 1;

// Formatted columns
v3.console.println(v3.util.padRight("Name", 20) + v3.util.padLeft("Score", 10));
```

---

### v3.nodes

Access to active BBS nodes (who's online) and inter-node messaging.

| Function | Returns | Description |
|----------|---------|-------------|
| `list()` | array | List active nodes. Invisible sessions hidden from non-sysops |
| `count()` | number | Active node count (respects invisible flag) |
| `send(nodeNum, message)` | boolean | Send a page message to a specific node. Returns `false` if node not found |
| `broadcast(message)` | — | Send a page message to all active nodes (except self) |

**Node entry fields** (returned by `list`):

| Field | Type | Description |
|-------|------|-------------|
| `node` | number | Node number |
| `handle` | string | User handle (empty if not logged in) |
| `activity` | string | Current activity description |
| `idle` | number | Seconds since last input |
| `invisible` | boolean | Whether session is invisible (sysop only) |

```javascript
// Show who's online
var nodes = v3.nodes.list();
for (var i = 0; i < nodes.length; i++) {
    var n = nodes[i];
    v3.console.println("|15Node " + n.node + "|07: " + n.handle + " - " + n.activity);
}

// Page another node
v3.nodes.send(2, "Hey, check out the new files!");

// Broadcast to everyone
v3.nodes.broadcast("System maintenance in 5 minutes");
```

> **Note:** Page messages are delivered at the recipient's next menu prompt, not immediately.

---

### Pipe Codes Quick Reference

Use pipe codes in `print()`, `println()`, `center()`, `yesno()`, `noyes()`, and `color()`.

| Code | Color |
|------|-------|
| `\|00` | Black |
| `\|01` | Dark Blue |
| `\|02` | Dark Green |
| `\|03` | Dark Cyan |
| `\|04` | Dark Red |
| `\|05` | Dark Magenta |
| `\|06` | Brown |
| `\|07` | Light Gray (default) |
| `\|08` | Dark Gray |
| `\|09` | Light Blue |
| `\|10` | Light Green |
| `\|11` | Light Cyan |
| `\|12` | Light Red |
| `\|13` | Light Magenta |
| `\|14` | Yellow |
| `\|15` | White (bright) |

Background colors use `|B0` through `|B7`.
