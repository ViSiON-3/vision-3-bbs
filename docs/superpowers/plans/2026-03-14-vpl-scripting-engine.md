# VPL (Vision Programming Language) — Goja Scripting Engine Plan

**Goal:** Extend the existing goja JavaScript engine (`internal/syncjs/`) into a full Vision/3 scripting system ("VPL") that gives sysops a native API for building doors, utilities, and BBS features — independent of the Synchronet compatibility layer.

**Rationale:** Vision/3 already has ~3,500 LOC of goja infrastructure for Synchronet JS door compatibility. Rather than adding a second runtime, we extend the existing engine with a Vision/3-native API surface (`v3.*` namespace). This gives sysops access to user management, messaging, file areas, configuration, and session I/O through clean, documented JavaScript bindings.

**Architecture:** A new `internal/scripting/` package provides the V3 scripting engine, built on goja. It reuses the I/O and lifecycle patterns from `internal/syncjs/` but exposes Vision/3's actual Go packages instead of Synchronet-shaped stubs. The existing `syncjs` package remains untouched for backward compatibility with Synchronet JS doors.

**Tech Stack:** Go, goja (pure-Go ES5.1+ JavaScript engine), existing BBS managers (user, message, file, session, config)

---

## Directory Convention

```
scripts/                   # root for all VPL scripts
├── data/                  # persistent data written by scripts (v3.data storage, v3.fs sandbox)
└── examples/              # shipped example/reference scripts
    └── hello.js
```

- `scripts/` — Sysops organize their VPL scripts freely within this directory.
- `scripts/data/` — Default storage location for `v3.data` persistent key-value stores and `v3.fs` file operations. Each script gets a JSON file named after the script (e.g., `scripts/data/voting.json`). Scripts cannot write outside this sandbox without elevated access.
- `scripts/examples/` — Shipped example scripts for reference. Not intended to be modified in place.

Scripts are referenced in `doors.json` with `working_directory` pointing to their location under `scripts/`.

---

## Phase 1: Foundation — Engine & Console I/O [COMPLETED]

Create the core V3 scripting engine with terminal I/O. This is the minimum viable scripting environment — a sysop can write a script that displays text, reads input, and interacts with the terminal.

- [x] **Step 1.1: Create `internal/scripting/` package structure**

  Files created:
  - `engine.go` — Engine struct wrapping goja.Runtime, context management, I/O pipe, input parsing, script lifecycle
  - `config.go` — ScriptConfig (script path, working dir, args) and SessionContext
  - `errors.go` — Sentinel errors (ErrTerminated, ErrDisconnect, ErrTimeout)

  Pattern: Mirrors the isolation model from `syncjs/engine.go` — each user gets their own Engine instance with context cancellation and cleanup. Reuses the interposed-pipe I/O pattern for session read/write.

- [x] **Step 1.2: Implement `v3.console` — Terminal I/O bindings**

  File: `console.go`

  API surface:
  ```javascript
  v3.console.write(text)          // raw output
  v3.console.writeln(text)        // output + CRLF
  v3.console.print(text)          // output with pipe-code parsing (|07, |09, etc.)
  v3.console.println(text)        // print + CRLF
  v3.console.clear()              // clear screen
  v3.console.cls()                // alias for clear
  v3.console.gotoxy(x, y)        // cursor positioning
  v3.console.color(fg, bg)        // set color by number
  v3.console.reset()              // reset attributes
  v3.console.center(text)         // center text on screen
  v3.console.getkey()             // blocking single key read
  v3.console.getkey(timeout_ms)   // key read with timeout (returns "" on timeout)
  v3.console.getstr(maxlen)       // line input with editing
  v3.console.getstr(maxlen, opts) // line input with options {echo:false, upper:true, number:true}
  v3.console.getnum(max)          // numeric input
  v3.console.yesno(prompt)        // Y/n prompt, returns boolean
  v3.console.noyes(prompt)        // N/y prompt, returns boolean
  v3.console.pause()              // "press any key" prompt

  // Properties (read-only)
  v3.console.width                // terminal columns
  v3.console.height               // terminal rows
  ```

  Pipe codes use Vision/3 native format (`|00`-`|15`, `|B0`-`|B7`) via `internal/ansi`, NOT Synchronet Ctrl-A codes.

- [x] **Step 1.3: Implement `v3.session` — Session context bindings**

  File: `session.go`

  ```javascript
  v3.session.node          // current node number
  v3.session.timeLeft      // seconds remaining (dynamic)
  v3.session.online        // true if connected (dynamic)
  v3.session.startTime     // session start (Unix timestamp)

  v3.session.bbs.name      // board name
  v3.session.bbs.sysop     // sysop name
  v3.session.bbs.version   // vision/3 version string

  v3.session.user.id       // user ID
  v3.session.user.handle   // handle/alias
  v3.session.user.realName // real name
  v3.session.user.accessLevel
  v3.session.user.timesCalled
  v3.session.user.location
  v3.session.user.screenWidth
  v3.session.user.screenHeight

  v3.args                  // script arguments array
  ```

- [x] **Step 1.4: Implement script execution via menu system**

  Files created:
  - `internal/menu/v3script_door.go` — Unix door executor bridging DoorCtx to V3Engine
  - `internal/menu/v3script_door_windows.go` — Windows door executor

  Modified:
  - `internal/menu/door_handler.go` — `v3_script` dispatch + door list/info type labels
  - `internal/menu/door_handler_windows.go` — Same dispatch + labels
  - `internal/config/config.go` — Updated DoorConfig.Type comment
  - `internal/configeditor/fields_doors.go` — Config editor: new type option, `isV3Script()`, VPL-specific fields (Script, Args)

  Door config example (entry in `doors.json` array):
  ```json
  {
    "name": "HELLO",
    "type": "v3_script",
    "script": "hello.js",
    "working_directory": "scripts/examples"
  }
  ```

- [x] **Step 1.5: Add `scripts/` directory and example script**

  Created `scripts/examples/hello.js` demonstrating `v3.console` and `v3.session` API.
  Created `scripts/data/.gitkeep` for persistent script data storage.

---

## Phase 2: User & Data Access [COMPLETED]

Expose read/write access to user accounts and BBS data, enabling scripts that modify user state, display stats, and manage accounts.

- [x] **Step 2.1: Implement `v3.user` — Current user bindings (read/write)**

  New file: `user.go`

  ```javascript
  // Read-only properties
  v3.user.id               // user ID
  v3.user.handle           // handle/alias
  v3.user.realName         // real name
  v3.user.accessLevel      // numeric access level
  v3.user.timesCalled      // login count
  v3.user.location         // group/location
  v3.user.screenWidth      // terminal width preference
  v3.user.screenHeight     // terminal height preference

  // Writable (persisted on script exit or explicit save)
  v3.user.set(field, value) // update user field
  v3.user.save()            // persist changes immediately
  ```

  Implementation: Backed by `internal/user.Manager`. Changes are buffered and written on `save()` or engine close. Only the current user's own record is writable.

- [x] **Step 2.2: Implement `v3.users` — User database access (read-only)**

  New file: `users.go`

  ```javascript
  v3.users.get(handle)        // get user by handle, returns user object or null
  v3.users.getByID(id)        // get user by ID
  v3.users.count()            // total user count
  v3.users.list()             // returns array of {id, handle, accessLevel, lastOn}
  v3.users.find(opts)         // search: {minLevel: 50, location: "NYC"}
  ```

  Security: Read-only access. No password fields exposed. Sysop-level scripts (accessLevel >= configured threshold) could get write access in a future phase.

- [x] **Step 2.3: Implement `v3.data` — Script-local persistent storage**

  New file: `data.go`

  ```javascript
  v3.data.get(key)             // read from script's data store
  v3.data.set(key, value)      // write (JSON-serializable values)
  v3.data.delete(key)          // remove key
  v3.data.keys()               // list all keys
  v3.data.getAll()             // return entire store as object
  ```

  Storage: JSON file per script in `scripts/data/<script-name>.json`. This gives each script its own persistent key-value store without polluting user records or requiring database setup. Thread-safe via file locking.

---

## Phase 3: Message & File Area Integration [COMPLETED]

Enable scripts to interact with the BBS's core content — reading/posting messages and browsing file areas.

- [x] **Step 3.1: Implement `v3.message` — Message area bindings**

  New file: `message.go`

  ```javascript
  // Area listing
  v3.message.areas()                      // list accessible areas [{id, name, tag, echoTag, type}]
  v3.message.area(tag)                    // get area by tag

  // Reading
  v3.message.count(areaID)               // message count in area
  v3.message.get(areaID, msgNum)         // get message {from, to, subject, body, date, msgid}
  v3.message.getNew(areaID)              // messages since last read
  v3.message.newCount(areaID)            // new message count

  // Posting
  v3.message.post(areaID, {to, subject, body})       // post to area
  v3.message.postPrivate({to, subject, body})         // private message
  ```

  Backed by `internal/message.Manager`. Respects ACS — only areas the current user can access are returned. Posting respects write-access ACS.

- [x] **Step 3.2: Implement `v3.file` — File area bindings**

  New file: `file.go`

  ```javascript
  // Area listing
  v3.file.areas()                         // list accessible areas
  v3.file.area(tag)                       // get area by tag

  // Browsing
  v3.file.list(areaID)                    // files in area
  v3.file.count(areaID)                   // file count
  v3.file.search(query)                   // keyword search across areas
  v3.file.newFiles(areaID, sinceDate)     // new file scan

  // Metadata
  v3.file.info(areaID, fileID)            // full file record
  ```

  Backed by `internal/file.Manager`. Read-only for now. Upload/download operations are complex (transfer protocols) and belong in a later phase.

---

## Phase 4: ANSI Art & Display [COMPLETED]

Enable scripts to leverage Vision/3's ANSI art display capabilities.

- [x] **Step 4.1: Implement `v3.ansi` — ANSI art display**

  New file: `ansi_display.go`

  ```javascript
  v3.ansi.display(filename)              // display .ANS file (respects terminal height)
  v3.ansi.displayRaw(filename)           // display without pipe-code processing
  v3.ansi.sauce(filename)                // get SAUCE metadata {title, author, group, width, height}
  ```

  Leverages existing `internal/ansi` package. File paths are sandboxed to the script's working directory and `menus/v3/ansi/`.

- [x] **Step 4.2: Implement `v3.util` — Common utility functions**

  New file: `util.go`

  ```javascript
  v3.util.sleep(ms)                      // pause execution
  v3.util.random(max)                    // random int 0 to max-1
  v3.util.time()                         // current Unix timestamp
  v3.util.date()                         // formatted date string
  v3.util.padRight(str, len)             // string padding
  v3.util.padLeft(str, len)
  v3.util.center(str, width)             // center text in width
  v3.util.stripAnsi(str)                 // remove ANSI codes
  v3.util.stripPipe(str)                 // remove pipe codes
  v3.util.displayLen(str)                // visible length (no ANSI/pipe)
  ```

---

## Phase 5: File I/O & Sandboxing [COMPLETED]

Allow scripts to read/write files within controlled boundaries.

- [x] **Step 5.1: Implement `v3.fs` — Sandboxed file operations**

  New file: `filesystem.go`

  ```javascript
  v3.fs.read(path)                       // read text file
  v3.fs.write(path, content)             // write text file
  v3.fs.append(path, content)            // append to file
  v3.fs.exists(path)                     // check existence
  v3.fs.delete(path)                     // delete file
  v3.fs.list(dir)                        // list directory contents
  v3.fs.mkdir(path)                      // create directory
  ```

  **Sandboxing:** All paths are resolved relative to `scripts/data/`. Path traversal (`../`) is blocked. Access outside the sandbox is denied. Sysop scripts (high access level) may get broader access via config.

- [x] **Step 5.2: Implement resource limits and timeouts**

  - Maximum script execution time (configurable, default 30 minutes)
  - Maximum file I/O operations per script invocation
  - Maximum output bytes per script (prevent infinite loops flooding terminal)
  - Memory limits (goja doesn't expose this directly — monitor via Go runtime)
  - Log resource usage per script execution

---

## Phase 6: Inter-Node Communication & Events [COMPLETED]

Enable scripts to interact with other active sessions and respond to BBS events.

- [x] **Step 6.1: Implement `v3.nodes` — Node/who's-online access**

  New file: `nodes.go`

  ```javascript
  v3.nodes.list()                        // active nodes [{node, handle, activity, idle}]
  v3.nodes.count()                       // active node count
  v3.nodes.send(nodeNum, message)        // send inter-node message
  v3.nodes.broadcast(message)            // send to all nodes
  ```

  Backed by `internal/session` active sessions tracking. Inter-node messaging requires a simple pub/sub mechanism (Go channels) between session goroutines.

- [ ] **Step 6.2: Implement script-as-event-handler**

  Allow V3 scripts to be triggered by scheduler events (not just door invocations):
  - Add `"v3_script"` as an event type in `internal/scheduler/`
  - Event scripts run without a session (headless) — `v3.console` is unavailable
  - Useful for: maintenance tasks, data cleanup, report generation, external API calls

  Event config example:
  ```json
  {
    "name": "nightly_stats",
    "schedule": "0 3 * * *",
    "type": "v3_script",
    "command": "scripts/nightly_stats.js"
  }
  ```

---

## Phase 7: Configuration Editor & Documentation

- [x] **Step 7.1: Update config editor for v3_script door type**

  Done in Phase 1. Config editor recognizes `v3_script` type with VPL-specific fields (Script, Args). Synchronet-specific fields (Exec Dir, Library Paths) are hidden for VPL scripts.

- [x] **Step 7.2: Sysop documentation**

  Created `docs/sysop/scripting/vpl-scripting.md` — Combined guide + API reference covering:
  - What VPL is, comparison with Synchronet JS doors
  - Directory structure, adding scripts, menu integration
  - Complete Programming Reference for all v3.* APIs
  - Pipe codes quick reference
  - Added to `docs/sysop/_sidebar.md` navigation

- [x] **Step 7.3: Example scripts**

  Create practical example scripts in `scripts/examples/`:
  - `oneliners.js` — Classic BBS oneliners wall (uses `v3.data` for persistence)
  - `voting.js` — Voting booth with persistent results (uses `v3.data`)
  - `stats.js` — System statistics display
  - `automsg.js` — Auto-message of the day (uses `v3.data`)
  - `userstats.js` — Top callers / top posters display (uses `v3.users`)
  - `last10.js` — Last 10 callers display (uses `v3.users`)

---

## Implementation Notes

### Package Dependencies

```
internal/scripting/
  ├── imports: github.com/dop251/goja
  ├── imports: internal/user       (via interface, Phase 2+)
  ├── imports: internal/message    (via interface, Phase 3+)
  ├── imports: internal/file       (via interface, Phase 3+)
  ├── imports: internal/ansi       (for pipe-code rendering)
  ├── imports: internal/config     (for BBS config access)
  └── imports: internal/session    (for node/who's-online, Phase 6+)

internal/menu/
  └── imports: internal/scripting  (v3script_door.go)
```

### Interface-Based Design

To avoid circular dependencies, define manager interfaces in `internal/scripting/`:

```go
type UserProvider interface {
    GetUser(handle string) (*user.User, error)
    GetUserByID(id int) (*user.User, error)
    UpdateUser(u *user.User) error
    ListUsers() ([]*user.User, error)
}

type MessageProvider interface {
    ListAreas() []message.Area
    GetMessage(areaID int, msgNum int) (*message.Message, error)
    AddMessage(areaID int, msg *message.Message) error
    GetMessageCountForArea(areaID int) (int, error)
    // ...
}

type FileProvider interface {
    ListAreas() []file.Area
    GetFilesForArea(areaID string) ([]file.FileRecord, error)
    SearchFiles(query string) ([]file.FileRecord, error)
    // ...
}
```

### Synchronet Compatibility

The existing `internal/syncjs/` package is **not modified**. Synchronet JS doors continue to work exactly as before through the `synchronet_js` door type. The new `v3_script` type is a completely separate code path. Sysops choose which engine to use per-door in `doors.json`.

### Security Model

- Scripts run in the same process — no OS-level sandboxing
- File I/O is path-restricted to `scripts/data/` (Phase 5)
- `v3.data` storage is sandboxed to `scripts/data/<script-name>.json` (Phase 2)
- User data writes limited to current user (unless sysop-level)
- No network access in Phase 1-6 (could add `v3.http` in a future phase)
- Resource limits prevent runaway scripts
- Context cancellation ensures cleanup on disconnect

### Naming: Why "VPL"?

Following BBS tradition: PCBoard had PPL (PCBoard Programming Language), Mystic has MPL (Mystic Programming Language). VPL = Vision Programming Language. The underlying runtime is JavaScript/goja, but the API namespace and documentation brand it as VPL — sysops interact with `v3.*` objects, not raw goja.
