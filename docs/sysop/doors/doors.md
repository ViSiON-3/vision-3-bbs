# Door Programs

Doors are external programs launched from the BBS: games, utilities, anything that talks to a terminal. When a caller picks one, ViSiON/3 writes a dropfile that tells the program who is calling, hands the caller's terminal to it, and takes the session back when the program exits. Doors are how a board gets Legend of the Red Dragon, TradeWars and the rest of the classics, along with modern games written for today's systems.

This page explains every door setting and how doors behave while they run. **Setting up your first door?** Start with [Setting Up Doors](how-to-guides/doors.md), which walks through a DOS door, a native door, a Synchronet JS door and a VPL script step by step. The hand-edited `configs/doors.json` file, the dropfile formats, placeholders and environment variables are in the [Technical Reference](#technical-reference) at the bottom.

Doors that live on another machine are set up differently: see [Door Servers](doors/door-servers.md) for connecting out to a shared door server over RLogin or Telnet.

## Door Types

ViSiON/3 runs six kinds of door. The **Type** field in the config editor picks one, and decides which other fields the editor shows.

| Type (config editor) | What it runs | How it is stored in `doors.json` | Walkthrough |
| --- | --- | --- | --- |
| Native | A program built for the machine the BBS runs on: a Linux or macOS binary, a Windows program on a Windows BBS, or a shell script | `type` blank, `is_dos` false | [Set up a native door](how-to-guides/door-native.md) |
| DOS | A 16-bit DOS program under dosemu2 (Linux x86 only) | `type` blank, `"is_dos": true` | [Set up a DOS door](how-to-guides/door-dos.md) |
| Synchronet JS | A Synchronet BBS JavaScript door game, run by the built-in JS engine | `"type": "synchronet_js"` | [Set up a Synchronet JS door](how-to-guides/door-synchronet-js.md) |
| VPL Script | A ViSiON/3 script with access to the BBS itself | `"type": "v3_script"` | [Set up a VPL script door](how-to-guides/door-vpl.md) |
| RLogin | An outbound connection to a door server that speaks RLogin | `"type": "rlogin"` | [Set up a door server connection](how-to-guides/door-rlogin.md) |
| Telnet | An outbound connection to a door server, or another BBS, that answers Telnet | `"type": "telnet"` | [Door Servers — Telnet](doors/door-servers.md#telnet) |

## Configuration

Use the [Configuration Editor](configuration/configuration.md#configuration-editor-tui) (`./config`, section **7 — Door Programs → 1 — Doors**) to add, edit, and remove door definitions interactively. Press **I** to insert a record, fill in the fields, press **Esc** to leave the record, then **Q** and **Y** to save. The editor writes `configs/doors.json`; the running board notices the change within a couple of seconds, so no restart is needed.

**Door Programs → 2 — Door Categories** groups doors for the [generated door menu](doors/generated-menu.md). Doors without a category still work; they are listed under **Other**.

### Required Fields by Door Type

| Type | Minimum fields to set |
| --- | --- |
| Native external door | `Code`, `Name`, `Type=Native`, `Commands`, `Dropfile Type` if the door reads one (see [Supported Dropfile Types](#supported-dropfile-types)) |
| DOS door (dosemu2) | `Code`, `Name`, `Type=DOS`, `Commands`, `Dropfile Type` (usually `DOOR.SYS`), `Drive C Path`, optional `FOSSIL Driver` |
| Synchronet JS door | `Code`, `Name`, `Type=Synchronet JS`, `Script`, `Working Dir`, `Exec Dir`, `Library Paths` |
| VPL script door | `Code`, `Name`, `Type=VPL Script`, `Script`, `Working Dir` |
| RLogin door server | `Code`, `Name`, `Type=RLogin`, `Host`, usually `Terminal Type` (see [Door Servers](doors/door-servers.md)) |
| Telnet door server | `Code`, `Name`, `Type=Telnet`, `Host` (see [Door Servers — Telnet](doors/door-servers.md#telnet)) |

**Code** is the name you use in menus (`DOOR:CODE`). It is stored in upper case and may use A-Z, 0-9, `_` and `-`, up to 16 characters. **Name** is what callers see and can be anything.

### Supported Dropfile Types

A dropfile is a small text file the BBS writes just before the door starts. It tells the door who is connected, how much time they have, and which node they are on. Pick the format your door's documentation asks for in the **Dropfile Type** field (`dropfile_type` in JSON).

| Dropfile Type | Origin | Lines | Filename written | Typically used by |
| --- | --- | --- | --- | --- |
| `DOOR.SYS` | PCBoard / GAP | 52 | `DOOR.SYS` | Most DOS door games (LORD, TradeWars, Usurper), many modern doors |
| `DOOR32.SYS` | Mystic / Synchronet | 11 | `DOOR32.SYS` | 32-bit and modern doors, Synchronet-style doors |
| `DORINFO1.DEF` | RBBS-PC / QuickBBS | 13 | `DORINFO1.DEF` | Older RBBS, QuickBBS, and RemoteAccess doors |
| `CHAIN.TXT` | WWIV | 30 | `CHAIN.TXT` | WWIV chain programs |
| `DROPFILE.INI` | Synchronet (draft) | named keys | `DROPFILE.INI` | Doors written for the named-value format. See [DROPFILE.INI](#dropfileini) |
| `(none)` | — | — | nothing | Doors that only need environment variables or command-line placeholders |

All five formats carry the same core session data: node number, the caller's handle and real name, their access level and user record number, the time they have left and the board's name. Fields the BBS does not track, such as phone numbers and passwords, are filled with safe placeholders, so doors that key their save data on the user record number, real name, or handle work as expected. Users with no time limit, and CoSysOps and above, are reported as having 540 minutes left. The exact contents of each format are in [Dropfile Formats](#dropfile-formats).

**How each door type handles dropfiles:**

- **Native doors** (Linux, macOS, Windows) write only the selected format. With `(none)` no file is written and `{DROPFILE}` expands to an empty string. The file is deleted when the door exits.
- **DOS doors** always write every format to the per-node directory (`C:\NODES\TEMPn\`). `Dropfile Type` decides which file the `{DROPFILE}` and `{DOSDROPFILE}` placeholders point at; it defaults to `DOOR.SYS`. Selecting `DROPFILE.INI` also sets `DROPFILE_INI` inside DOS. `Dropfile Location` and `Dropfile Case` do not apply to DOS doors and the editor does not show them.
- **Synchronet JS and VPL script doors** do not use dropfiles. The script runtime gets the session data directly. See [Synchronet JS Doors](doors/synchronet-js-doors.md).
- **RLogin and Telnet doors** do not use dropfiles. There is no local process to read one; over RLogin the user is identified to the door server through the handshake instead, and over Telnet by whatever **Send On Connect** types for them. See [Door Servers](doors/door-servers.md).

**DROPFILE.INI** is the named-value format [drafted by Synchronet](https://github.com/SynchronetBBS/sbbs/blob/master/docs/dropfile_ini.md). Instead of giving a value its meaning by line number, it writes `KEY=value` lines under INI sections, so a door reads only the keys it needs. It also carries things the positional formats cannot, such as the caller's IP address, terminal size and encoding, and the BBS's idle timeout. The door finds the file through the `DROPFILE_INI` environment variable or the `{DROPFILE}` placeholder. Every key ViSiON/3 writes is listed under [DROPFILE.INI](#dropfileini) in the Technical Reference.

**Personal details.** Some doors are closed source, or send what they read to other systems. Set **Hide Personal** (`dropfile_hide_personal`) on such a door to leave the user's real name, location and IP address out of its `DROPFILE.INI`. The other dropfile formats are not affected.

**Filename case.** Native doors on case-sensitive filesystems sometimes look for `door32.sys` rather than `DOOR32.SYS`. Set **Dropfile Case** (`dropfile_case`) to `lower` for those doors. The default, `upper`, writes the conventional uppercase name.

**Where the file goes.** See [Dropfile Location](#dropfile-location) below. The short version: `startup` (the default) writes into the door's working directory, or into a per-node temporary directory if the door has none; `node` always writes into a fresh per-node temporary directory so several nodes can run the same door at once.

### Door Command Line

The **Commands** field (`commands` in JSON) is the command line the BBS runs when a user opens the door. It is entered differently depending on the door type:

| Door type | How to enter Commands in the config editor | Stored in JSON as |
| --- | --- | --- |
| Native | Executable followed by its arguments, separated by spaces: `/opt/doors/tw2002/tw2002 -n {NODE} -d {DROPFILE}` | `["/opt/doors/tw2002/tw2002", "-n", "{NODE}", "-d", "{DROPFILE}"]` |
| Native, an argument containing spaces | Separate the arguments with commas instead: `/opt/doors/game/game -title, My Game` | `["/opt/doors/game/game", "-title", "My Game"]` |
| DOS | Comma-separated DOS batch lines: `START.BAT {NODE}, EXIT` | `["START.BAT {NODE}", "EXIT"]` |
| Synchronet JS / VPL | Not used. Set **Script** and **Script Args** instead | `script`, `args` |
| RLogin | Not used. Set **Host** and the handshake fields instead | `host`, `terminal_type` |
| Telnet | Not used. Set **Host** and, if the server wants a login, **Send On Connect** | `host`, `send_on_connect` |

For native doors the first word is the executable and the rest are its arguments. When the text after the executable contains a comma, it is split on commas and each entry becomes exactly one argument, so an argument may contain spaces; otherwise it is split on whitespace, as a shell would. A flag and its value are two arguments (`-n {NODE}`) unless the door wants them joined (`-n{NODE}`). A single argument that contains spaces is shown with a trailing comma so it reads back the same way. **Cleanup Command** follows the same rule. Set **Use Shell** to `Yes` for a `.sh` / `.bat` script or a program that must start through the shell; the field does not interpret pipes, redirects or globs.

For DOS doors each entry becomes a line in the generated `EXTERNAL.BAT`, run after the FOSSIL driver loads, the screen clears, and the BBS changes into **Working Dir**. A line that starts a `.BAT` file is prefixed with `CALL` so control returns to the BBS afterwards.

Placeholders such as `{NODE}`, `{DROPFILE}` and `{NODEDIR}` are replaced when the door starts, in **Commands**, **Cleanup Command** arguments and **Env Vars**, and in the handshake and login fields of RLogin and Telnet doors. The full list is under [Placeholders](#placeholders) in the Technical Reference.

#### Menu commands

The menu commands that launch doors take no flags of their own. `DOOR:CODE` runs the door with that code, and `LISTDOORS`, `OPENDOOR`, and `DOORINFO` ignore anything after the command name. Anything the door needs on its command line goes in **Commands**. See [Menu Integration](#menu-integration).

## Dropfile Location

By default (**Dropfile Location** `startup`, or `dropfile_location` blank), the dropfile is written to the door's **Working Dir**. A door with no working directory is treated as if it had `node`, so nodes running it at once never share a dropfile. Set `node` to write it to a fresh per-node temporary directory (named like `vision3_node1_XXXXXX` under the system temp directory) instead. The directory is removed when the door exits. This is useful for multi-instance doors where multiple nodes may run simultaneously and need isolated dropfiles. A `DROPFILE.INI` door that is not **Single Instance** always uses a per-node directory, whatever this setting says.

For DOS doors the setting does not exist: every dropfile format is always written to the per-node directory inside `drive_c` (at `C:\NODES\TEMPn\`, which is `doors/drive_c/nodes/tempn/` on the host). The config editor does not show **Dropfile Location** for a DOS door, and `DOORINFO` shows a DOS door's dropfile type with no location. Point the door game at that directory using the `{DOSNODEDIR}` or `{DOSDROPFILE}` placeholder on its command line, or in the game's own configuration. A `dropfile_location` left in an older DOS door's record is ignored.

## Access Control

Set **Min Access Level** (`min_access_level`) to restrict a door to users with a minimum access level. Users below the required level will see an "access denied" message. A value of `0` (default) means no restriction.

Doors with access restrictions are hidden from the door list for unauthorized users. The menu command that launches the door can carry an ACS (Access Control String) as well; see [Menus & ACS](menus/menu-system.md).

## Single Instance Locking

Set **Single Instance** (`single_instance`) to `Yes` for doors that should not run concurrently on multiple nodes (e.g., single-player games with shared save files). When a second node tries to launch the same door, they will see a "door is in use" error message. The lock is freed the moment the door ends, including when the BBS ends it because the caller hung up.

## Cleanup Command

**Cleanup Command** (`cleanup_command` and `cleanup_args`) names an optional program to run after the door exits. This is useful for:

- Removing temporary files or lock files left by the door
- Processing score files or game results
- Resetting door state between sessions

In the config editor it is entered like **Commands**: the program, then its arguments separated by spaces, or by commas when an argument contains spaces. Placeholders are replaced in the arguments; the program path itself is used as written. Cleanup failures are logged but do not affect the user's session.

## Use Shell

Set **Use Shell** (`use_shell`) to `Yes` to start the door through a shell (`/bin/sh` on Linux and macOS, `cmd` on Windows). The command and its arguments are passed through unchanged, so pipes, redirects and globs in the command line are not interpreted; put those in a wrapper script. Required for launching shell scripts (`.sh`, `.bat`, `.cmd` files) directly.

## I/O Modes

Native doors only. **I/O Mode** (`io_mode`) picks how the door's input and output are wired to the caller.

### STDIO (default)

Standard I/O redirection: the door's stdin/stdout/stderr are connected directly to the user's session. When **Raw Terminal** (`requires_raw_terminal`) is `Yes`, a PTY (pseudo-terminal) is allocated for full raw terminal passthrough, which is what most interactive, full-screen doors want.

### SOCKET

Creates a Unix socketpair and passes one end to the door process as file descriptor 3. The environment variable `DOOR_SOCKET_FD=3` is set automatically. The BBS bridges the other end bidirectionally to the user's session. Use this for doors that expect a raw socket handle rather than STDIO. Socket mode is not available on Windows.

## Menu Integration

Doors are launched via menu commands:

- `DOOR:CODE` — Launch a specific door by its internal code
- `DOORMENU` — Show the [generated door menu](doors/generated-menu.md) built from `doors.json`, with categories, paging and lightbar or numbered input; `RUN:DOORMENU CODE` opens one category directly
- `LISTDOORS` — Display the list of available doors
- `OPENDOOR` — Prompt the user to enter a door code (supports `?` to list)
- `DOORINFO` — Show configuration details for a specific door

The stock doors menu (`DOORSM`) is hand-drawn ANSI art with one `DOOR:CODE` entry per key, and that still works. The generated menu is the alternative for a board with many doors: it draws itself from `doors.json`, so adding a door needs no art edit. See [Menus & ACS](menus/menu-system.md) for the menu editor and [Menu Commands Reference](reference/menu-commands.md) for the commands.

## Running Synchronet JavaScript Doors

ViSiON/3 can run Synchronet BBS JavaScript door games natively using a built-in JS engine. The required JS libraries and LORD/LORD II example doors are included in the bundle under `doors/sbbs/`. Set **Type** to `Synchronet JS` (`"type": "synchronet_js"`) in the door configuration.

See [Synchronet JS Doors](doors/synchronet-js-doors.md) for full setup instructions.

## Running DOS Doors

DOS doors are supported on Linux x86/x86-64 via dosemu2. ViSiON/3 uses dosemu2's terminal translator (`$_term_color`, `$_term_esc_char`) to convert DOS INT 10h screen output to ANSI escape sequences, which are bridged to the user's SSH session via a PTY.

### How It Works

1. ViSiON/3 generates a per-node `dosemurc` config that maps `drive_c_path` as the DOS C: drive
2. An `EXTERNAL.BAT` batch file is generated containing: FOSSIL driver loading (if configured), `cls` (plus a screen clear echoed to COM1 in serial mode), `cd` to working directory (if set), and the user's `commands`
3. dosemu2 boots DOS, executes EXTERNAL.BAT, and exits via `exitemu`
4. The PTY output bridge suppresses DOS boot text: in terminal mode it waits for the `cls` clear screen sequence (`ESC[2J`), and in serial mode for the same screen clear echoed to COM1 (see [Boot Text Suppression](#boot-text-suppression))
5. After the door exits, the BBS session resumes

The generated files are described under [Generated dosemu2 Configuration](#generated-dosemu2-configuration) in the Technical Reference.

### FOSSIL Driver

Most DOS BBS door games communicate via COM1 serial I/O using the FOSSIL (Fido/Opus/SEAdog Standard Interface Layer, INT 14h) interface. Configure the **FOSSIL Driver** field (`fossil_driver`) to load a FOSSIL driver (such as X00.EXE) before the door runs:

```json
"fossil_driver": "C:\\UTILS\\X00.EXE eliminate"
```

The FOSSIL driver is loaded in EXTERNAL.BAT before `cls` and the door commands. The `eliminate` parameter tells X00.EXE to remove any previous instance before installing.

dosemu2's `$_com1 = "virtual"` maps COM1 to the controlling PTY, enabling FOSSIL-based door games to communicate with the SSH session. ViSiON/3 adds it to the per-node `dosemurc`, along with `$_video = "none"`, and runs dosemu2 without `-t`, so the door's output reaches the caller only through COM1.

A door that loads its own FOSSIL driver from **Commands**, or drives the serial port itself, can leave **FOSSIL Driver** blank and set `$_com1 = "virtual"` in its own **DOSemu Config** file (`dosemu_config`) instead. That puts the door in the same serial mode. Any other `$_com1` line in the base config is dropped, and the door runs in terminal mode. This includes the `$_com1 = "virtual"` in the shared `~/.dosemu/.dosemurc`, which the installed template sets for all doors: only a door's own config file counts.

### Boot Text Suppression

dosemu2 outputs boot text (FreeDOS/comcom64 startup messages) before EXTERNAL.BAT starts. ViSiON/3 suppresses this by buffering PTY output until the door's output begins, then forwarding everything from that point on, so the user gets a clean screen when the door starts. How it finds that point depends on the mode:

- **Terminal mode** (no COM1): output is held until the `cls` command in EXTERNAL.BAT produces an `ESC[2J` clear screen sequence.
- **Serial mode** (**FOSSIL Driver** set, or a virtual COM1 in the door's `dosemu_config`): `$_video = "none"` keeps the DOS screen off the PTY, but dosemu2 still writes plain-ASCII boot text there. EXTERNAL.BAT echoes a screen clear (`ESC[2J ESC[H`) to COM1 right before the door commands, and output is held until it arrives.

If 32 KB of output builds up without the marker, the BBS gives up waiting and forwards it all, so a door whose batch file never clears the screen still appears, boot text and all. If dosemu2 exits before the marker, the held output is dropped.

### Platform Support

| Platform | DOS Door Support | Native Door Support |
| --- | --- | --- |
| Linux x86 / x86-64 | Yes (dosemu2) | Yes (STDIO/PTY/Socket) |
| Linux ARM / ARM64 | No | Yes (STDIO/PTY/Socket) |
| macOS | No | Yes (STDIO/PTY/Socket) |
| Windows 32-bit | Not yet (NTVDM planned) | Yes (STDIO only) |
| Windows 64-bit | No | Yes (STDIO only) |

A complete LORD configuration is under [doors.json](#doorsjson) in the Technical Reference, and the step-by-step install is [Set up a DOS door](how-to-guides/door-dos.md).

## Ending a Door

A door reads the caller's connection directly, so the BBS watches it on the door's behalf and ends the door, then logs the caller off, when:

- the caller sends nothing for their [idle timeout](#idle-timeout);
- the caller's [time limit](#time-limit) runs out;
- the caller [hangs up](#hang-up).

### Idle Timeout

The session **Idle Timeout** (`sessionIdleTimeoutMinutes` in `config.json`) applies inside doors as it does in the menus. When a caller sends nothing for that long, the BBS ends the door, shows the idle timeout message and logs the caller off. Every key the caller presses restarts the countdown, as does anything else their terminal sends, such as a reply to a door's terminal query. Output from the door doesn't count. CoSysOps and above are exempt here too.

Many doors have their own inactivity timer. If a door's timer is shorter, it ends the session first, as it always has. A door that reads `DROPFILE.INI` can use `IDLE_LIMIT` to match the BBS's timer or to warn the user.

### Time Limit

A caller's per-call **time limit** (`timeLimit`; see [Time Limits](users/user-management.md#time-limits)) runs out inside a door as it does in the menus: the BBS ends the door, shows the time limit message and logs the caller off. A caller with no time left isn't let into a door at all. Doors are still told the time left through their dropfile (`TIME_LEFT` and the equivalents), so a well-behaved door can wrap up first.

CoSysOps and above have no time limit in doors, whatever their account says, so a sysop is never cut off mid-game. A caller whose `timeLimit` is `0` (unlimited) is reported to doors as having 540 minutes rather than none, and VPL and Synchronet JS scripts see a steady hour, so no door mistakes an unlimited caller for one who is out of time.

### Hang-up

When a caller drops their connection inside a door, the door is ended at once, rather than left running until it notices on its own or its own timer ends it. The node and, for a **Single Instance** door, the door lock are freed straight away.

### How a door is ended

- **Native and DOS doors** are hung up on: the BBS sends `SIGHUP` to the door and any programs it started, as a modem dropping carrier would, and `SIGKILL` to anything still running 5 seconds later. On Windows the door process is killed.
- **RLogin and Telnet doors** have their connection to the door server closed, including while it is still being made.
- **Synchronet JavaScript and V3 script doors** are stopped as if the caller had disconnected.

## When a Door Fails

A native door that exits with a nonzero status, or is ended by a signal, has still run: whatever it had to say it has already shown the caller, and plenty of doors exit nonzero as a matter of course. The caller is returned to the menu and the exit is logged as a warning in `data/logs/vision3.log`. A door that is misconfigured and quits immediately is therefore only visible in the log, so check there first when a door "does nothing".

A door that cannot be started at all, such as a missing executable or a DOS door whose dosemu2 did not start, shows the caller an error followed by `Press Enter to continue...`. The BBS waits for that Enter before returning to the menu, so the key never reaches the menu and relaunches the door. A caller who idles at that prompt for their idle timeout, or hangs up, is logged off as they would be anywhere else.

Other messages a caller can see: **access denied** when their level is below the door's **Min Access Level**, **door is in use** when a **Single Instance** door is running on another node, and **not found** when a `DOOR:CODE` command names a code that is not in `doors.json`. The wording of each is in `strings.json` (`doorAccessDenied`, `doorBusyFormat`, `doorErrorFormat`, `doorNotFoundFormat` and their neighbours).

The door setup guide has a [troubleshooting list](how-to-guides/doors.md#troubleshooting) for doors that start but misbehave.

## Technical Reference

Everything below is for hand-editing `configs/doors.json` and for door authors who need the exact file formats. The config editor writes all of it for you.

### doors.json

Door programs are stored in `configs/doors.json` as an array. The running BBS reloads the file within a couple of seconds of a change. A malformed file, or a door with an invalid or duplicate `code`, is logged and the previous set of doors stays in use until it is fixed; at startup the same mistake stops the BBS, so keep a copy before editing by hand.

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

This configuration:

- Sets the DOS working directory to `C:\DOORS\LORD` (auto `cd` before commands)
- Generates a `DOOR.SYS` dropfile in the per-node directory (`C:\NODES\TEMPn\`)
- Loads the X00.EXE FOSSIL driver for COM1 serial I/O
- Runs `START.BAT` with the node number
- Prevents multiple nodes from running LORD simultaneously (`single_instance`)

A native door with a cleanup step:

```json
{
  "code": "TW2002",
  "name": "TradeWars 2002",
  "working_directory": "doors/tw2002",
  "commands": ["./tw2002", "-n", "{NODE}", "-d", "{DROPFILE}"],
  "dropfile_type": "DOOR.SYS",
  "dropfile_location": "node",
  "requires_raw_terminal": true,
  "cleanup_command": "/opt/bbs/scripts/tw-cleanup.sh",
  "cleanup_args": ["{NODEDIR}", "{NODE}"]
}
```

Every key is optional except `code` and `name`; a key left out takes the default in the tables below.

#### Common Fields

These fields apply to every door type unless the table says otherwise.

| Key | Type | Default | Meaning |
| --- | --- | --- | --- |
| `code` | string | required | Unique internal code (used in `DOOR:CODE` menu commands). Uppercase; A-Z, 0-9, `_`, `-`; max 16 chars |
| `name` | string | required | Display name shown to users (free-form, case preserved) |
| `type` | string | blank | `synchronet_js`, `v3_script`, `rlogin`, `telnet`, or blank for a native or DOS door (DOS is `is_dos`) |
| `working_directory` | string | blank | Native: directory to run the command in, absolute or relative to the directory the BBS was started from (normally the BBS root). DOS: DOS path to `cd` into before running commands (e.g., `C:\DOORS\LORD`). Script doors: the game's directory |
| `commands` | []string | none | Native: `[0]`=executable, `[1:]`=args. DOS: each entry is a batch command line. Not used by script or remote doors |
| `dropfile_type` | string | blank (none) | `DOOR.SYS`, `DOOR32.SYS`, `CHAIN.TXT`, `DORINFO1.DEF`, `DROPFILE.INI`, or blank for none. See [Supported Dropfile Types](#supported-dropfile-types). Native and DOS doors |
| `dropfile_location` | string | `startup` | `startup` (working dir; per-node temp dir if there is no working dir) or `node` (per-node temp dir). Native doors only; ignored for DOS |
| `dropfile_case` | string | `upper` | `upper` (`DOOR32.SYS`) or `lower` (`door32.sys`). Native doors only |
| `dropfile_hide_personal` | bool | `false` | Leave the user's real name, location and IP address out of `DROPFILE.INI`. See [DROPFILE.INI](#dropfileini) |
| `min_access_level` | int | `0` | Minimum user access level required (0 = no restriction) |
| `single_instance` | bool | `false` | Only allow one node to run this door at a time |
| `cleanup_command` | string | blank | Program to run after the door exits. Native and DOS doors |
| `cleanup_args` | []string | none | Arguments for the cleanup program (supports placeholders) |
| `environment_variables` | map | none | Additional environment variables, `{"KEY": "VALUE"}` (supports placeholders). Native doors only |

#### Native Door Fields

Used when `type` is blank and `is_dos` is `false`.

| Key | Type | Default | Meaning |
| --- | --- | --- | --- |
| `io_mode` | string | `STDIO` | `STDIO` or `SOCKET`. See [I/O Modes](#io-modes) |
| `requires_raw_terminal` | bool | `false` | Allocate a PTY for raw terminal I/O |
| `use_shell` | bool | `false` | Wrap command in `/bin/sh -c` (Linux, macOS) or `cmd /c` (Windows) |

#### DOS Door Fields

Set `is_dos: true` to run a 16-bit DOS door game via dosemu2.

| Key | Type | Default | Meaning |
| --- | --- | --- | --- |
| `is_dos` | bool | `false` | `true` = DOS door launched via dosemu2 |
| `drive_c_path` | string | `~/.dosemu/drive_c` | Host path mounted as DOS C: drive. Relative paths are resolved against the BBS root directory; the installer creates `doors/drive_c` |
| `dos_emulator` | string | `auto` | `auto` or `dosemu`. Only dosemu2 is supported today |
| `fossil_driver` | string | blank | DOS FOSSIL driver command to load before the door (e.g., `C:\UTILS\X00.EXE eliminate`). Setting it puts the door in serial mode |
| `dosemu_config` | string | blank | Host path to a custom `.dosemurc` used as the base config. If blank, `~/.dosemu/.dosemurc` is the base. A custom config that sets `$_com1 = "virtual"` runs the door in serial mode even with `fossil_driver` blank |

#### Script Door Fields

Used when `type` is `synchronet_js` or `v3_script`. Only `script` and `args` apply to a VPL script; see [Synchronet JS Doors](doors/synchronet-js-doors.md) and [VPL Scripting](scripting/vpl-scripting.md).

| Key | Type | Default | Meaning |
| --- | --- | --- | --- |
| `script` | string | required | Main JS file to execute, relative to `working_directory` |
| `args` | []string | none | Arguments passed to the script (`argv` in Synchronet JS, `v3.args` in VPL). Placeholders are not substituted |
| `exec_dir` | string | required (JS) | Synchronet's `exec/` directory (`system.exec_dir`) |
| `library_paths` | []string | required (JS) | Search paths for `load()` and `require()` |

#### Remote Door Fields

Used when `type` is `rlogin` or `telnet`. See [Door Servers](doors/door-servers.md) for what to put in them.

| Key | Type | Default | Meaning |
| --- | --- | --- | --- |
| `host` | string | required | Door server hostname or IP address |
| `port` | int | `513` (rlogin), `23` (telnet) | TCP port; `0` means the default |
| `client_username` | string | `{USERHANDLE}` | RLogin client-user-name field; carries the password on servers that authenticate. Placeholders supported |
| `server_username` | string | `{USERHANDLE}` | RLogin server-user-name field, e.g. `[TAG]{USERHANDLE}`. Placeholders supported |
| `terminal_type` | string | `ANSI/38400` (rlogin), `ANSI` (telnet) | RLogin: terminal-type field, where door servers read the door code (`xtrn=LORD`). Telnet: the terminal type reported to the server. Placeholders supported |
| `send_on_connect` | string | blank | Telnet only. Typed into the session as soon as it connects; `\r` is Enter. Placeholders supported |
| `raw_tcp` | bool | `false` | Telnet only. `true` turns the Telnet protocol off for a server that speaks plain TCP |
| `connect_timeout` | int | `10` | Seconds to wait for the server; `0` means the default |
| `disconnect_key` | string | `^]` | Key that hangs up the session, in `^X` notation; `none` passes every key through |

#### Generated Menu Fields

Used by the [generated door menu](doors/generated-menu.md) only; a door's launch configuration is unaffected.

| Key | Type | Default | Meaning |
| --- | --- | --- | --- |
| `category` | string | blank (Other) | Code of a category defined in `doorCategories` in `config.json`. An unknown code lists the door under Other |
| `description` | string | blank | Shown by the `^DS` row placeholder |
| `hidden` | bool | `false` | Hide the door from the generated menu; `DOOR:CODE` still works |
| `sort_order` | int | `0` | Position when the menu sort is `manual` |

### Global DOS Settings

The dosemu2 binary path is configured globally in `config.json` (**System Setup → DOS Emulation** in the config editor), not per-door:

| Key | Type | Default | Meaning |
| --- | --- | --- | --- |
| `dosemuPath` | string | `/usr/libexec/dosemu2/dosemu2.bin` | Path to the dosemu2 binary. ViSiON/3 calls the binary directly, bypassing the bash wrapper at `/usr/bin/dosemu`, which mangles backslash arguments |

### Placeholders

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
| `{USERIP}` | Caller's IP address (IPv4 or IPv6 without brackets; empty when the session has no remote address) | All |
| `{STARTUPDIR}` | Resolved working directory (`.` when **Working Dir** is blank) | All |
| `{DROPFILE}` | Host path to the generated dropfile (empty when **Dropfile Type** is `(none)`) | Native, DOS |
| `{NODEDIR}` | Host directory containing the dropfile | Native, DOS |
| `{DOSDROPFILE}` | DOS path to the dropfile, e.g. `C:\NODES\TEMP1\DOOR.SYS` | DOS only |
| `{DOSNODEDIR}` | DOS path to the node directory, e.g. `C:\NODES\TEMP1` | DOS only |

For RLogin doors the placeholders above marked *All* are also substituted in **Client User**, **Server User**, and **Terminal Type**; for Telnet doors in **Terminal Type** and **Send On Connect**. Dropfile and DOS placeholders do not apply, since no local process runs.

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

### Environment Variables

The following environment variables are automatically set for native and Windows door processes. DOS doors receive only `DOSEMU_QUIET`, plus `DROPFILE_INI` inside DOS when **Dropfile Type** is `DROPFILE.INI`:

| Variable | Value |
| --- | --- |
| `BBS_USERHANDLE` | User's handle |
| `BBS_USERID` | User ID number |
| `BBS_NODE` | Node number |
| `BBS_TIMELEFT` | Minutes remaining |
| `BBS_USERIP` | Caller's IP address |
| `LINES` | Terminal height (Linux and macOS) |
| `COLUMNS` | Terminal width (Linux and macOS) |
| `DOOR_SOCKET_FD` | Socket FD (SOCKET mode only) |
| `DROPFILE_INI` | Absolute path to the dropfile (**Dropfile Type** `DROPFILE.INI` only) |
| `DOSEMU_QUIET` | `1` (DOS doors only, suppresses dosemu startup messages) |

A `BBS_*` or `DROPFILE_INI` variable you set yourself in **Env Vars** wins over the automatic value. Additional variables can be configured per-door via the `environment_variables` field (**Env Vars** in the config editor, entered as `KEY=VALUE, KEY2=VALUE2`). These are set in the OS environment before a native door process is launched. Placeholders are substituted at runtime. DOS doors receive only `DOSEMU_QUIET`; pass session data to a DOS door through the dropfile or the batch command line instead.

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

### Dropfile Formats

The four positional formats carry the same core session data:

| Value | Source |
| --- | --- |
| Node number | Current node |
| Baud rate | Always `38400` (`115200` in `DORINFO1.DEF`, `9600` in `CHAIN.TXT`) |
| Real name and handle | User record |
| Security level | User access level |
| Time remaining | Session time left, in minutes or seconds depending on the format. Users with no time limit, and CoSysOps and above, are reported as 540 minutes. |
| User record number | User ID |
| BBS name | `boardName` from `config.json` |
| Screen size | User's saved screen height (and width in `CHAIN.TXT`), default 25 rows / 80 columns |
| Graphics | Always ANSI |

Fields the BBS does not track are filled with safe placeholders: phone numbers are `00-0000-0000`, the password is `SECRET`, dates are `01-01-1971`, and the sysop name in `DORINFO1.DEF` and `CHAIN.TXT` is `Sysop`. Doors that key their save data on the user record number, real name, or handle work as expected.

#### DROPFILE.INI

`DROPFILE.INI` is the named-value drop file [drafted by Synchronet](https://github.com/SynchronetBBS/sbbs/blob/master/docs/dropfile_ini.md) (draft 0.6). Instead of giving a value its meaning by line number, it writes `KEY=value` lines under INI sections, so a door reads only the keys it needs. The format is still a draft and its keys may change.

ViSiON/3 writes these keys:

| Section | Keys |
| --- | --- |
| `[file]` | `FILE_TIME` |
| `[system]` | `SYS_SOFTWARE`, `SYS_VENDOR` (`VISION3`), `SYS_VERSION`, `SYS_NAME`, `SYS_OP`, `SYS_NODE_NUM`, `SYS_NODE_COUNT`, `SYS_QWKID`, `SYS_LOCATION` |
| `[comm]` | `COMM_TYPE`, `COMM_CHARSET`, plus `COMM_HANDLE` or `COMM_PORT` where the type needs one |
| `[user]` | `USER_ALIAS`, `USER_NUMBER`, `USER_ROLE`, `USER_REALNAME`, `USER_LOCATION`, `USER_IP`, `USER_PROTOCOL` |
| `[terminal]` | `TERM_COLS`, `TERM_ROWS`, `TERM_TYPE` (`ansi`), `TERM_CHARSET`, `TERM_TERMINFO` |
| `[session]` | `TIME_LEFT` (seconds), `TEMP_DIR`, `LOCAL_DISPLAY` (`0`), `IDLE_LIMIT` (seconds) |
| `[door]` | `DOOR_CODE`, `DOOR_NAME` (the door's **Code** and **Name**) |
| `[x-vision3]` | `X_VISION3_LEVEL` (the user's access level) |

Optional keys with nothing to report are left out: `SYS_QWKID` needs an explicit `qwkID` in `config.json`, `TERM_TERMINFO` is written only when the caller's client sent a terminal type, and `TEMP_DIR` is written only when the file is in a per-node directory (any DOS door, or a native door as described below).

`IDLE_LIMIT` is the [idle timeout](#idle-timeout) in seconds, so a door can warn the user before it runs out; it is left out for CoSysOps and above, who are exempt. One key the draft defines is never written: `SYS_FTN_ADDR` lists the board's FTN addresses with the primary one first, and FTN networks aren't configured with a primary.

- **`COMM_TYPE`** is `stdio` for a native door, `socket` with `COMM_HANDLE=3` in [SOCKET](#socket) I/O mode, and `fossil` with `COMM_PORT=1` for a DOS door with a **FOSSIL Driver** or a `dosemu_config` that sets `$_com1 = "virtual"`. A DOS door without one reads and writes the DOS console, which is reported as `stdio`.
- **`COMM_CHARSET`** is the caller's terminal encoding, `CP437` or `UTF-8`, because the BBS relays a door's bytes untranslated. A DOS door in terminal mode (no **FOSSIL Driver** and no virtual COM1 in its `dosemu_config`) always gets `CP437`, since dosemu2 translates its screen.
- **`USER_ROLE`** is `sysop` or `cosysop` when the user's access level reaches `sysOpLevel` or `coSysOpLevel`, otherwise `user`.
- **`TERM_TERMINFO`** is the terminal type the caller's client sent, through Telnet TERMINAL-TYPE or the SSH pty request, such as `xterm-256color`, for doors that use curses or terminfo.
- **Text values** are written in CP437; `FILE_UTF8` is never set. Control characters in a value become `?`, and values are cut to the 255-byte line limit.
- **`TEMP_DIR`** is a path, written exactly as the file system gives it. A path that would need changing to fit the file, because it is too long or contains a control character, is left out rather than cut short.
- **Hide Personal** (`dropfile_hide_personal`) leaves out `USER_REALNAME`, `USER_LOCATION` and `USER_IP`.

The door finds the file through the `DROPFILE_INI` environment variable, which holds its absolute path, or through `{DROPFILE}` on the command line. For a DOS door the BBS adds `SET DROPFILE_INI=C:\NODES\TEMPn\DROPFILE.INI` to the generated batch file when **Dropfile Type** is `DROPFILE.INI`; `{DOSDROPFILE}` gives the same path for the command line. No two nodes' files may share a path, so a native door always gets a per-node directory for `DROPFILE.INI`, as with **Dropfile Location** `node`, unless it is marked **Single Instance**. Only a single-instance door can have the file written to its working directory with `startup`.

### Generated dosemu2 Configuration

For every DOS door launch, ViSiON/3 writes two files into the node directory (`doors/drive_c/nodes/tempn/`, `C:\NODES\TEMPn` in DOS): a `dosemurc` and `EXTERNAL.BAT`. dosemu2's own boot log for the launch is `dosemu_boot.log` in the same directory.

The per-node `dosemurc` starts from a base config: the door's `dosemu_config` if set, otherwise `~/.dosemu/.dosemurc` if it exists. Its `$_com1`, `$_hdimage`, `$_video` and `$_vga` lines are removed, and these settings are appended:

| Setting | Value | Purpose |
| --- | --- | --- |
| `$_lredir_paths` | `"<node_path>"` | Allows access to the per-node directory |
| `$_com1` | `"virtual"` | Serial mode only: maps COM1 to the caller's PTY |
| `$_video` | `"none"` | Serial mode only: keeps the DOS screen off the PTY |
| `$_term_color` | `(on)` | Terminal mode only: enables ANSI terminal color translation |
| `$_term_esc_char` | `(27)` | Terminal mode only: sets ESC as the ANSI escape character |

The C: drive is passed to dosemu2 on its command line rather than through `$_hdimage`, and terminal mode adds dosemu2's `-t` flag. Settings the BBS does not manage, such as the CP437 character set configuration, belong in the base dosemurc.

**Serial mode** is on when **FOSSIL Driver** is set, or when the door's own `dosemu_config` sets `$_com1 = "virtual"`. The check reads the config file line by line: the setting name must be exactly `$_com1` (so `$_com10` does not count), the value is compared without regard to case after stripping surrounding single or double quotes, a trailing `;` and any `#` comment, and the last `$_com1` line wins, as it does in dosemu2. A virtual COM1 in the shared `~/.dosemu/.dosemurc` is not counted, because the installed template sets one for every door.

`EXTERNAL.BAT` is generated in this order:

1. `@echo off`, `SET PATH=%PATH%;D:\dosemu` and `c:`
2. The **FOSSIL Driver** line, if set
3. `SET DROPFILE_INI=C:\NODES\TEMPn\DROPFILE.INI`, when **Dropfile Type** is `DROPFILE.INI`
4. `cls`, and in serial mode `echo ESC[2J ESC[H >COM1` (the marker the output gate waits for)
5. The drive letter and `cd` into **Working Dir**, if set
6. Each line of **Commands**, with placeholders replaced and `CALL` added in front of a `.BAT`
7. `exitemu`

### dosemurc Template

The installer writes this base `~/.dosemu/.dosemurc` from `templates/configs/dosemurc`:

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
$_sound = (off)
```

The `$_external_char_set` and `$_internal_char_set` must be `"cp437"` for DOS door ANSI art to render correctly. A `$_com1` line here has no effect, because the BBS sets COM1 for each door: set **FOSSIL Driver** on the door, or give it a `dosemu_config` with `$_com1 = "virtual"`.
