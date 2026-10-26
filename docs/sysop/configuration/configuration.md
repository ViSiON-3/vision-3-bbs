# ViSiON/3 Configuration Guide

ViSiON/3 keeps its settings in a handful of JSON files under `configs/`, one
per subject: the board itself in `config.json`, its text in `strings.json`,
and doors, areas, conferences, networks and scheduled events in files of their
own. You rarely need to open them. The **configuration editor** (`./config`),
a menu-driven TUI (text user interface) in the spirit of ViSiON/2's
`CONFIG.EXE`, edits all of them, and a running board picks up most saves
within a couple of seconds. This page covers the editor, the `vision3 doctor`
health check, how changes take effect, and, at the bottom, a technical
reference for every file.

**On this page**

- [Configuration Editor (TUI)](#configuration-editor-tui) — the menu-driven editor and what each screen writes
- [Doctor](#doctor) — `vision3 doctor`, the read-only board health check
- [Applying Configuration Changes](#applying-configuration-changes) — what reloads live, what waits for an idle board, what needs a restart
- [Bad user names](#bad-user-names) — handles refused at signup
- [Technical Reference](#technical-reference) — hand-editing the files:
  [config.json](#configjson), [strings.json](#stringsjson), [doors.json](#doorsjson),
  [archivers.json](#archiversjson), [ziplab.json](#ziplabjson), [file_areas.json](#file_areasjson),
  [message_areas.json](#message_areasjson), [ftn.json](#ftnjson), [conferences.json](#conferencesjson),
  [events.json](#eventsjson), [Menu Configuration](#menu-configuration),
  [Theme Configuration](#theme-configuration), [oneliners.json](#onelinersjson),
  [SSH Host Keys](#ssh-host-keys), [Logging](#logging), [Doctor command line](#doctor-command-line)
  and [Environment variables](#environment-variables)
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

The editor opens to a main menu. Keys **1** through **6** and **B** open sub-menus, keys **7** through **9**, **0** and **A** open record lists, and **Q** quits.

| Key | Section | What it covers |
|-----|---------|----------------|
| 1 | System Setup | Sub-menu: BBS Registration, Server Setup, Default Settings, DOS Emulation, Logging, QWK Mobile API |
| 2 | Access & Security | Sub-menu: Access Levels, Connection Limits, Bot Defense, IP Blocklist/Allowlist, New User Voting (NUV), Bad User Names |
| 3 | Areas and Conferences | Sub-menu: Message Areas, File Areas, Conferences |
| 4 | Echomail Networking | Sub-menu: Echomail Networks, Echomail Links, FTN Setup Wizard |
| 5 | QWK Networking | Sub-menu: QWK Networks, QWK Network Wizard |
| 6 | ViSiON/3 Networking (V3Net) | Sub-menu: Node Identity, Subscriptions, Hosted Networks |
| 7 | Door Programs | External door program record list |
| 8 | Transfer Protocols | File transfer protocol record list |
| 9 | Archivers | Archive format record list |
| 0 | Event Scheduler | Automated event record list |
| A | Login Sequence | Login step record list |
| B | ZipLab Upload Processing | Sub-menu: General, Pipeline Steps, Virus Scan (see [ZipLab](files/ziplab.md)) |
| Q | Quit | Exit (prompts to save if there are unsaved changes) |

### System Setup Sub-menu

Choosing **System Setup** (key 1) opens an inner menu of six numbered items, all writing to `configs/config.json`. Use Up/Down and Enter to open an item; Esc returns to the main menu.

| Item | Name | Fields |
|------|------|--------|
| 1 | BBS Registration | Board Name, SysOp Name, BBS Location, Timezone, QWK ID |
| 2 | Server Setup | SSH enabled/host/port/legacy-algorithms, Telnet enabled/host/port, V3Net enabled + keystore/dedup/registry paths, V3Net hub settings, Binkd mailer settings |
| 3 | Default Settings | Allow New Users (Y/N), Require Email (Y/N), File List Mode (lightbar/classic), Deleted User Retention Days, Twit Lists (Y/N) |
| 4 | DOS Emulation | DOSemu Path |
| 5 | Logging | Log Directory, Min Level, Rolling Type, Cache Writes, Max Files, Max Size KB |
| 6 | QWK Mobile API | Enabled, Host, Port, Cert File, Key File, Token TTL Hours |

**Server Setup (item 2)** also writes to `configs/v3net.json` for the V3Net fields (keystore path, dedup DB path, registry URL, hub enabled/host/port/data dir/auto-approve/auto-approve areas).

The SSH, Telnet and V3Net hub **Host** fields take an IPv4 address, a host
name, or an IPv6 address — `::1`, `::`, `fe80::1%eth0`, with or without
square brackets. The BBS joins the host and port correctly whichever form you
use. A blank host, or `0.0.0.0`, listens on every interface. Changing a host
or port takes effect at the next restart.

### Access & Security Sub-menu

Choosing **Access & Security** (key 2) opens an inner menu of six numbered items, also writing to `configs/config.json`.

| Item | Name | Fields |
|------|------|--------|
| 1 | Access Levels | SysOp Level, CoSysOp Level, WFC Access, Invisible Level, New User Level, Auto Validate, Notify SysOp, Regular Level, Logon Level, Anonymous Level, Page Timeout, Page Cooldown |
| 2 | Connection Limits | Max Nodes, Max Per IP, Failed Logins (0=off), Lockout Minutes, Idle Timeout, Chat Idle Timeout, Transfer Timeout |
| 3 | Bot Defense | Challenge gate (enable, art file, key, timeout, required presses, stray keys, live countdown) and connection-rate limiter (enable, hits, window, ban minutes). See [Security](configuration/security.md#bot-defense) |
| 4 | IP Blocklist/Allowlist | Blocklist Path, Allowlist Path |
| 5 | New User Voting (NUV) | Use NUV, Auto Add NUV, NUV Use Level, Yes/No vote thresholds, Validate/Kill on threshold, NUV Level, NUV Form |
| 6 | Bad User Names | Bad Names Path — the list of handles refused at signup; the rules themselves are edited in that file. See [Bad user names](#bad-user-names) |

### Areas and Conferences Sub-menu

| Item | What it edits | File written |
|------|---------------|--------------|
| Message Areas | Area tag, name, type (local/echomail/v3net/netmail), ACS, paths, conference, networking fields | `configs/message_areas.json` |
| File Areas | Area tag, name, description, path, ACS, conference | `configs/file_areas.json` |
| Conferences | Tag, name, description, ACS, display order | `configs/conferences.json` |

A message area's **Max Messages** and **Max Age** are only enforced by the
nightly `v3mail purge` event. When you set either and no enabled event runs
`v3mail purge`, the editor says so on the spot: *No enabled event runs 'v3mail
purge', so this limit is not enforced*. The BBS logs the same warning at
startup. Enable the nightly maintenance events under **Event Scheduler** (key
0); see [Nightly Message Base Maintenance](advanced/event-scheduler.md#nightly-message-base-maintenance).

### Echomail Networking Sub-menu

| Item | What it edits |
|------|---------------|
| Echomail Networks | Global FTN paths (inbound, outbound, temp, bad/dupe tags) and per-network settings (own address, tosser enable, origin line); hub polling is configured under Events |
| Echomail Links | Per-hub link settings (address, packet/session/AreaFix passwords, flavour) |
| FTN Setup Wizard | Guided flow: downloads a network's echolist, lets you browse and subscribe to areas, then writes `ftn.json`, `message_areas.json`, and `conferences.json` (and `file_areas.json` for any file echoes chosen) automatically |

Two things the editor does for you here:

- **A corrected own address follows into `binkd.conf`.** Change a network's
  **Own Address** (or re-run the wizard with the right one) and the save
  replaces the matching `address` line in `data/ftn/binkd.conf` as well, so
  the mailer stops announcing the old node number. Other AKAs and other
  networks are left alone; if the address is in a new zone, the `domain`
  line follows too.
- **Auto-Create changes can be applied to existing areas.** A network's
  Auto-Create settings (ACS, message and age limits, auto-join, real names)
  apply to areas created *after* the change. When you change one and the
  network already has echomail areas that differ, an **Existing Areas**
  prompt asks *Apply to N existing area(s) too?* — **Y** updates those areas,
  **N** or Esc keeps the Auto-Create change only. Netmail areas and other
  networks are never touched.

#### FTN Setup Wizard Flow

**Node Lookup.** After entering your FTN address, select the *Node Lookup* row and press Enter. The wizard downloads the network's nodelist, finds your node, and fills in your uplink hub's address, hostname, and BinkP port automatically. If your node isn't listed yet (new nodes appear in the next weekly nodelist), the hub is inferred from your net's Host/Hub entries. Networks without a published nodelist URL skip this feature; hub details can always be entered manually.

**Echo Areas.** Selecting the *Echo Areas* row downloads the network's echolist so you can tick the echoes you want. Echo areas are optional: not every network publishes its `.NA` file on the web (some hand it out through the network itself, via AreaFix), and web-hosted lists do go offline. If the download fails, press ESC — everything you entered is still on the form — and save. You get the network, its hub link, the netmail area and an updated `binkd.conf`; echoes can be added later under *Message Areas*, or by re-entering the wizard and choosing the network to edit.

**File Echoes.** For networks whose registry entry names a file echo list (`fileecho_list_url`), selecting the *File Echoes* row downloads it so you can tick the file echoes to carry. Saving creates one file area per ticked echo, linked to it so `v3mail toss` delivers inbound TIC files there. This step is optional; without it, use `helper fileecho` later (see [FTN File Echoes](files/file-echoes.md)).

### QWK Networking Sub-menu

| Item | What it edits |
|------|---------------|
| QWK Networks | Configured QWK network hubs (hub QWK ID, FTP host, login, password, tagline); **G** edits the shared paths and the bad-area tag; **W** re-runs the wizard on the highlighted network to add conferences |
| QWK Network Wizard | Guided flow (offers add-or-edit when a network already exists): pick a known network (DOVE-Net) or enter a custom hub, choose the hub conferences to carry, then writes `qwknet.json`, `message_areas.json`, `conferences.json` and the `qwknet_poll_<key>` event. See [QWK Networking](messages/qwk-networking.md) |

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

The editor tracks unsaved changes. When you quit with pending changes, you are prompted to save, and confirmed changes are then written to the appropriate configuration files. A successful save shows *All configurations saved successfully* on the status line and signals the running BBS to re-read what changed (see [Applying Configuration Changes](#applying-configuration-changes)).

**How saves work.** It is safe to keep the editor open while the BBS runs or
while you use `helper` (for example `helper ftnsetup`, `helper fileecho`, or
`helper areas set`). The files more than one program writes — `config.json`,
`conferences.json`, `message_areas.json`, `file_areas.json`, `ftn.json` and
`v3net.json` — are never simply overwritten:

- Every writer takes a cross-process lock on the file (a `<file>.lock`
  sidecar beside it) for the duration of its read-modify-write. The BBS,
  `helper` and the editor all honour the same lock.
- The editor remembers each file as it was loaded. On save it re-reads the
  file and merges three ways: a value changed only in the editor is kept, a
  value changed only on disk since loading — a conference added by
  `helper ftnsetup`, a V3Net subscription made on the BBS, the allow-new-users
  toggle — is kept too, and a file that would come out identical is not
  written at all.
- A value changed in both places takes the editor's value, except that an
  edit always beats a deletion. The status line then reads *Saved; N
  conflict(s) with changes made elsewhere (see log)* followed by the first
  one, and each conflict is written to the editor's log.
- Two records added under the same ID in both places are kept, with the
  editor's renumbered; two with the same tag become one.

The editor writes its log to `config.log` in the board's log directory
(`data/logs/config.log` by default, the `logging` settings in `config.json`),
resolved against the BBS root rather than wherever you launched it from.
That is where to look for the full text of a merge conflict or a
`binkd.conf` sync failure.

## Doctor

`vision3 doctor` is a read-only health check for the whole board. It reads
everything the BBS would read at startup — `configs/*.json`, the menu set,
`doors.json`, `binkd.conf`, the IP lists, the bad-names list, the message
bases — and reports what is missing, misspelled, unreadable or inconsistent,
with a suggested fix for each finding. It never starts the listeners and,
unless you ask for `--fix`, it changes nothing. Run it after an install, after
an upgrade, after hand-editing a config file, and whenever something has
"just stopped working": it is the quickest way to find the typo in
`config.json`, the echomail area with no network, the `binkd.conf` that still
carries template placeholders, or the `ftn.json` every local user can read.

### Running Doctor

Run it from the BBS directory. The board can be stopped or running; doctor
only reads:

```bash
cd /opt/vision3
./vision3 doctor          # or: ./vision3 --doctor
```

Both spellings do the same thing. Doctor takes the current directory as the
board root, so run it from the directory that holds `configs/` and `data/`.
On Docker: `docker exec -u vision3 vision3-bbs ./vision3 doctor`.

### Reading the Report

Findings are grouped into five sections, each line tagged with a status and,
where there is something to do, a `FIX →` line:

```text
  ViSiON/3 Doctor Utility v0.9.4
────────────────────────────────────────────────────────────────────────
BOARD  /opt/vision3

  BOARD FILES
  [PASS]  data/users/
  [INFO]  new user settings  signups are enabled at level 10; regular users are level 25; logon threshold is 10; auto-validation is false
         FIX → Confirm these levels and the validation policy match your signup workflow.
  [WARN]  default sysop credentials  Felonius still uses the published default password
         FIX → Log in and change the password, or update the account with ./ue.

  CONFIGURATION
  [PASS]  configs/config.json  parses

  NETWORK & MESSAGING
  [WARN]  message areas  echomail area "FN_GEN" has no FTN network
         FIX → Choose a network in the area's configuration, or change its area_type if it is local.
  [WARN]  binkd.conf  still contains template placeholders
         FIX → Run the FTN Setup Wizard and fill in the network's identity and link settings.

  SECURITY
  [WARN]  configs/ftn.json permissions  file is readable by all local users and may contain network credentials
         FIX → Restrict file permissions to the BBS operator, for example chmod 600 configs/ftn.json.

  MENUS & DOORS
  [PASS]  menus/v3/mnu/MAIN.MNU  parses
  [WARN]  doors  door "LORDJS" script does not exist: doors/sbbs/xtrn/lord/lord.js
         FIX → Correct the script path or restore the script file.

────────────────────────────────────────────────────────────────────────
CHECK WARNINGS  70 passed  ·  5 warning(s)  ·  0 failure(s)
```

| Tag | Meaning |
|-----|---------|
| `PASS` | The check is satisfied |
| `INFO` | Nothing is wrong; a setting worth a second look, such as the signup levels or a message base that has not been created yet |
| `WARN` | The board runs, but something is misconfigured, insecure, or will fail later |
| `FAIL` | The board will not start, or a file cannot be read or parsed |

The last line sums up as `ALL CHECKS PASSED`, `CHECK WARNINGS` or
`NEEDS ATTENTION`. Colour is used on a terminal and dropped when the output
is piped or `NO_COLOR` is set.

What each section covers:

- **Board files** — the directories and files the BBS needs (`configs/`,
  `data/users/`, `data/logs/`, `data/msgbases/`, `data/files/`, `menus/v3/`,
  `config.json`) and the recommended ones, the SSH host key when SSH is
  enabled, that the data paths are writable, access levels, signup settings,
  InfoForms, the default sysop password, the bad-names list, and every menu
  `GOTO`, `RUN`, `DOOR` and `MSGAREA` target.
- **Configuration** — every `configs/*.json` parses, with the line and column
  of a syntax error; unknown or misspelled keys in `config.json` and
  `ftn.json` (a `_comment` key is allowed); the experimental QWK API if it
  is switched on.
- **Network & messaging** — area and conference references, duplicate tags
  and base paths, echomail and file-echo routing, TIC passwords, the FTN own
  addresses, links, passwords and paths compared with `binkd.conf`, whether
  binkd is installed, and QWKnet networks.
- **V3Net** — `configs/v3net.json` parses; warns on a blank or duplicated
  hub network name, a leaf `hubUrl` that is not `http://` or `https://`, a
  bad `pollInterval`, a leaf with no network name or no boards, a board tag
  that matches no local message area, a `v3net` area whose `network` is not
  listed, and a hub that is enabled but publishes no networks. Checks that the
  identity keystore holds a valid Ed25519 keypair readable only by the BBS
  user. It never generates a replacement key.
- **Security** — the IP blocklist and allowlist parse, and files that may
  hold credentials (`ftn.json`, `doors.json`, `qwknet.json`, the V3Net key)
  are not readable by other local users.
- **Menus & doors** — every `.MNU` and `.CFG` file parses, and each door's
  script or executable exists.

### Safe Fixes (`--fix`)

Doctor repairs one class of problem on request: the standard data
directories. `--fix` creates whichever of `data/users`, `data/logs`,
`data/msgbases` and `data/files` are missing, then runs the normal report.
`--dry-run` shows what `--fix` would create without creating it:

```bash
./vision3 doctor --fix --dry-run    # preview
./vision3 doctor --fix              # create the missing directories
```

The report then opens with a `SAFE FIXES` block, one line per directory
(`WOULD`, `FIXED`, `SKIP` or `ERROR`). A path that exists but is not a
directory is skipped and left for you to look at. Nothing else is ever
changed: every other finding is a `FIX →` suggestion you carry out yourself,
usually in `./config`.

### Doctor in Scripts

`--json` writes the same report as a JSON document, and the exit code says
whether anything needs attention (0 clean, 1 warnings, 2 failures), so a
deploy script or a monitoring check can run doctor and act on the result. The
flags, exit codes and JSON shape are in
[Doctor command line](#doctor-command-line).
## Applying Configuration Changes

A running BBS checks its configuration files for changes every two seconds and
re-reads the ones that changed. Saving in `./config` is normally all you need —
the TUI signals the BBS once every file in the save has been written.

**Applied without a restart:**

| File | Notes |
|---|---|
| `configs/config.json` | Except the startup-only fields listed below |
| `configs/doors.json` | |
| `configs/login.json` | |
| `configs/strings.json` | |
| `menus/<set>/theme.json` | |
| Menu files (`.MNU`, `.CFG`) and ANSI art | Re-read every time a menu is displayed |
| `configs/protocols.json` | |
| `configs/events.json` | Reschedules cron events; see notes below |
| `configs/conferences.json` | A caller whose conference was removed sees "None" and re-selects |
| `configs/archivers.json` | Re-read on each archive operation |
| IP blocklist/allowlist files | Watched separately, reloaded on save |
| `configs/ftn.json` | Origin lines apply on save; the mailer's runtime settings (port, log level, links) apply on its next cycle. Enabling or disabling the binkd mailer itself still requires a restart |

Connection-security settings in `config.json` — `maxNodes`, `maxConnectionsPerIP`,
`maxFailedLogins`, `lockoutMinutes`, the connection rate limiter, and the
`ipBlocklistPath` / `ipAllowlistPath` settings — are applied live. Lowering a
limit below current usage never disconnects anyone: new connections are refused
until callers log off and usage falls back under the limit.

Reloading `events.json` replaces the cron schedule. Events already mid-run
finish undisturbed, and `run_at_startup` events belong to process startup —
saving the file does not re-fire them.

**Applied when the board is idle:**

Structural changes can pull state out from under a caller mid-session, so
they queue instead of applying immediately, and land within a couple of
seconds of the last caller logging off:

- `configs/file_areas.json` — areas added, removed, or re-pathed; each
  area's file records are re-read from its `metadata.json`
- `configs/message_areas.json` — areas added, removed, or re-pathed; the
  message bases on disk are untouched
- `configs/v3net.json` — leaf subscriptions are torn down and rebuilt
  (a caller mid-chat on a V3Net leaf loses that chat's stream). Subscribing
  from the in-BBS area browser applies immediately instead of waiting for
  idle. Hub settings, the `enabled` flag, and keystore/dedup paths still
  require a restart

A queued change is logged when it queues and again when it applies. The
change is validated the moment you save — a file that doesn't parse is
reported immediately and never queued, and the running config stays as it
was. To apply queued changes right now, without waiting for the board to
empty:

```bash
touch configs/reload.force
```

`reload.force` also re-reads everything else, like `reload.now` — the
difference is that nothing touches it automatically, so it is the explicit
"I know callers are online, do it anyway".

**Still requires a restart:**

- Listening ports and hosts (`sshPort`, `sshHost`, `telnetPort`, `telnetHost`)
- `boardName`'s startup copies: the WFC header, the V3Net identity sent in
  subscribe requests, and the QWK **API** service's packet headers.
  Everything else follows a rename immediately: message origin lines,
  V3Net wire messages and blank-origin tearlines, and interactive QWK
  packets (built fresh per operation)
- Enabling or disabling a protocol (`sshEnabled`, `telnetEnabled`)
- SSH host keys
- The QWK API listener
- Logging directory and rolling settings (the log *level* is applied live)

### Triggering a reload by hand

You do not normally need to, but a reload can be forced two ways:

```bash
# Touch the reload semaphore — the BBS re-reads everything within a couple of seconds
touch configs/reload.now

# Or send SIGHUP to the running process
kill -HUP $(pgrep vision3)
```

Both are useful after editing a config file by hand, or from a script. `SIGHUP`
reloads rather than terminating; it is not available on Windows.

### Restarting

For the changes that need it:

```bash
# Restart the BBS
# (Ctrl+C to stop, then:)
./vision3
```

## Bad user names

Every board has handles it would rather nobody registered: `root`, `admin`,
anything containing `sysop`, the names of your co-sysops. The bad-names list
is a plain text file of such rules, checked at signup. A caller who asks for
a matching handle gets the ordinary invalid-name message and tries again; the
attempt and the rule it hit are logged at INFO.

New installs get `configs/badusers.txt` from the shipped template, which
already refuses `root`, `admin*`, `*sysop*`, `guest` and `anonymous`. An
upgraded board will not have it — copy `templates/configs/badusers.txt` into
`configs/`, or create your own. Until the file exists, `vision3 doctor`
warns *bad user names ... is missing* and signup falls back to the built-in
checks.

```text
; badusers.txt - handles new users may not register
; One name or wildcard pattern per line. Matching is case-insensitive.
; * matches any run of characters. Blank lines and lines starting with ; or # are ignored.
root
admin*
*sysop*
guest
anonymous
```

Use one whole-handle rule per line, case-insensitive. Only `*` is a wildcard,
matching zero or more characters; other punctuation is literal. Blank lines and
trimmed lines beginning with `;` or `#` are ignored. Leading and trailing whitespace
is trimmed; no accent, punctuation or Unicode composition normalization is applied.
The file is re-read on every handle check, so edits take effect without restart.

The file's location is `badUsersPath` in `config.json`, shown as **Bad Names
Path** under **Access & Security → Bad User Names** (item 6) in `./config`.
It defaults to `configs/badusers.txt`; the default (or a blank value) uses
`badusers.txt` in the selected config directory. Custom relative paths are
relative to the working directory; absolute paths are supported.

Signup retains the existing reserved `new`, `q` and `sysop` names and format rules.
Missing, unreadable or invalid UTF-8/control
character rules produce a warning and preserve signup functionality. Empty or
comment-only files add no restrictions. `vision3 doctor` checks the selected file.

The local and online sysop editors warn on blocked handles but allow overrides.
There is currently no self-service handle rename flow. Existing accounts can still
log in; the list does not retroactively ban users.

The local user editor uses `./ue --config configs` by default, independently of
`--data` (the users directory). For an alternate installation, use
`./ue --data /path/to/accounts --config /path/to/board/configs` so handle warnings
and retention settings read that board's configuration. Existing `--data` usage
and the default `data/users` location remain supported.

## Technical Reference

Everything below is for hand-editing. The editor writes every one of these
files, so you only need this section to understand a setting it does not
expose, to script a change, or to read a file that `vision3 doctor` has
flagged. After editing a file by hand, run `./vision3 doctor` to catch a
syntax slip or a misspelled key, then let the running board pick the change up
(see [Applying Configuration Changes](#applying-configuration-changes)).

### Configuration Files Overview

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
- `ziplab.json` - ZipLab upload processing (see [ZipLab](files/ziplab.md#ziplabjson))
- `login.json` - Login sequence steps (see [Login Sequence](users/login-sequence.md))
- `protocols.json` - File transfer protocols (see [File Transfer](files/file-transfer.md))
- `qwknet.json` - QWK network hubs (see [QWK Networking](messages/qwk-networking.md))
- `v3net.json` - V3Net identity, subscriptions and hub (see [V3Net](v3net/configuration.md))
- `ftn_networks.json` - The registry of known FTN networks used by the setup wizard
- `badusers.txt`, `blocklist.txt`, `allowlist.txt` - Plain-text lists: handles refused at signup, IPs refused, IPs exempt from limits
- SSH host keys (`ssh_host_rsa_key`, etc.)
- `*.lock` - Lock sidecars created while a program writes a shared file; safe to ignore
- `reload.now`, `reload.force` - Touch to make a running BBS re-read its files (see [Applying Configuration Changes](#applying-configuration-changes))

**In `menus/v3/` directory (menu set):**

- `bar/PDMATRIX.BAR`, `cfg/PDMATRIX.CFG`, `mnu/PDMATRIX.MNU` - Pre-login matrix menu (see [Menu System Guide](menus/menu-system.md#pre-login-matrix-screen))
- `theme.json` - Theme color settings
- `ansi/PRELOGON.ANS` (or `PRELOGON.1`, `PRELOGON.2`, ...) - Pre-login ANSI screens shown before LOGIN (see [Menu System Guide](menus/menu-system.md#pre-login-ansi-files-prelogon))

**In `data/` directory:**

- `oneliners.json` - One-liner messages (JSON array)
- `oneliners.dat` - Legacy one-liner format (plain text, optional)

### config.json

General BBS configuration. All settings in this file are managed through the **System Setup** and **Access & Security** sections of `./config` — you should not need to hand-edit it.

#### TUI Paths

| Setting group | TUI path |
|---------------|----------|
| Board name, sysop name, location, timezone, QWK ID | System Setup → BBS Registration (item 1) |
| SSH / Telnet ports and enabled flags, V3Net, Binkd | System Setup → Server Setup (item 2) |
| New user registration, file list mode, retention, Twit Lists | System Setup → Default Settings (item 3) |
| DOSemu binary path | System Setup → DOS Emulation (item 4) |
| Log directory, level, rotation | System Setup → Logging (item 5) |
| QWK mobile API listener and TLS files | System Setup → QWK Mobile API (item 6) |
| Access levels (sysop, new user, logon, etc.) | Access & Security → Access Levels (item 1) |
| Max nodes, per-IP limits, timeouts | Access & Security → Connection Limits (item 2) |
| Bot challenge gate and connection-rate limiter | Access & Security → Bot Defense (item 3) |
| Blocklist / allowlist file paths | Access & Security → IP Blocklist/Allowlist (item 4) |
| NUV voting thresholds and behavior | Access & Security → New User Voting (NUV) (item 5) |

#### Field Reference

**BBS Registration:**

- `boardName` — BBS name displayed to users (default: `"ViSiON/3 BBS"`)
- `sysOpName` — Sysop's handle
- `bbsLocation` — Location string (city/region)
- `timezone` — IANA timezone for display formatting (e.g., `America/Los_Angeles`). If unset, falls back to `VISION3_TIMEZONE` env var, then `TZ`, then server local time.

**Server Setup:**

- `sshEnabled` — Enable/disable the SSH server (default: `true`)
- `sshHost` — Bind address: IPv4, host name, or IPv6 such as `"::1"` or `"[::]"` (default: `"0.0.0.0"`, every interface)
- `sshPort` — TCP port (default: `2222`)
- `legacySSHAlgorithms` — Older SSH algorithms for retro clients like SyncTERM (default: `true`)
- `telnetEnabled` — Enable/disable the Telnet server (shipped: `true`; fallback: `false`)
- `telnetHost` — Bind address, same forms as `sshHost` (default: `"0.0.0.0"`; blank also means every interface)
- `telnetPort` — TCP port (default: `2323`)

The V3Net hub listener's `host` in `v3net.json` takes the same forms. The QWK
API's `qwkAPI.host` takes an IPv6 address without brackets.

**Connection Limits:**

- `maxNodes` — Maximum simultaneous connections (default: `10`)
- `maxConnectionsPerIP` — Max concurrent connections from one IP (default: `3`)
- `maxFailedLogins` — Failed BBS logins from an IP before lockout (default: `5`, `0` = disabled)
- `lockoutMinutes` — Lockout duration in minutes (default: `30`)
- `sessionIdleTimeoutMinutes` — Idle session cutoff, in menus and in doors (default: `5`, `0` = disabled). CoSysOps and above are exempt
- `chatIdleTimeoutMinutes` — Idle cutoff while a caller is in teleconference chat (default: `0`, meaning `sessionIdleTimeoutMinutes` applies). In chat, messages, joins, leaves and topic changes in the caller's room count as activity, as do private messages to them, so someone reading a live conversation isn't dropped. CoSysOps and above are exempt
- `transferTimeoutMinutes` — File transfer timeout (default: `10`)

<a id="access-levels"></a>

**Access Levels:**

Two different sets of values are called "defaults" here, and they differ. The
**shipped** value is what a normal install gets, since setup copies
`templates/configs/config.json` into `configs/`. The **fallback** applies only
when a key is absent from your `config.json` — the loader starts from the
built-in defaults and overlays whatever the file provides.

| Setting | Shipped | Fallback | Meaning |
| ------- | ------- | -------- | ------- |
| `sysOpLevel` | `255` | `255` | SysOp access. `0` or lower falls back to `255`, so a blank value never makes every caller a sysop |
| `coSysOpLevel` | `250` | `250` | Co-SysOp access. `0` or lower falls back to `250` |
| `invisibleLevel` | `250` | `0` | Minimum level offered the Invisible Logon prompt, letting the caller keep the session off the Last Callers list (`0` falls back to `coSysOpLevel`) |
| `newUserLevel` | `10` | `1` | Assigned to a brand-new account |
| `regularUserLevel` | `25` | `10` | Assigned when a user is validated |
| `logonLevel` | `10` | `10` | Minimum level required to log in |
| `anonymousLevel` | `50` | `5` | Minimum level to post anonymously (`0` = disabled) |
| `notifySysopNewUser` | `true` | `true` | Page every co-sysop-or-above who is online when a signup completes |
| `pageSysopTimeout` | `60` | `60` | Seconds a caller waits for a sysop to answer a `PAGESYSOP` page before being told the SysOp is not available. Shown as **Page Timeout** in the config TUI, which accepts 5–600 |
| `pageSysopCooldown` | `300` | `300` | Seconds a caller must wait between pages; `0` disables the cooldown. Shown as **Page Cooldown** in the config TUI, which accepts 0–86400 |

See [User Management](users/user-management.md#system-access-levels) for how
these interact, and what happens at signup.

The two page settings are picked up on config reload without a restart. See
[Pages](how-to-guides/wfc-console.md#pages) for what the caller and the sysop
see.

`notifySysopNewUser` is deliberately independent of `autoValidateNewUsers`. An
auto-validated signup leaves nothing to review, but somebody joining is still
worth knowing at the time it happens, so the page fires either way. The notice
goes to anyone at `coSysOpLevel` or above who is online, and is delivered
through the same queue as a user page: it appears at their next menu prompt
rather than interrupting whatever they are doing. Nobody online means nobody is
paged — the login-sequence notice below is what covers that case. Edit the text
in `./strings` under **New User: SysOp Page**.

The fallback is `true`, so an existing system picks this up on upgrade without
editing `config.json`. That is deliberate for a notice that only ever adds a
line at a SysOp's own prompt; set it to `false`, or turn **Notify SysOp** off
under Security Levels in `./config`, to silence it.

**Default Settings:**

- `allowNewUsers` — Accept new user registrations (shipped and fallback: `true`)
- `requireNewUserEmail` — End signup by making the caller leave the SysOp (user #1) a private message: they are shown `NUEMAIL.ANS` in the menu set's `ansi` dir (or the `newUserEmailPrompt` string when the file is absent), paused, then dropped into the message editor addressed to the SysOp. The gate cannot be skipped — aborting or an empty message re-prompts; the obligation is stored on the account and resumed at next login (including logins that authenticate at the SSH layer), idling out of the editor counts the same as hanging up, and after three abandoned attempts (signup + two reconnects) the account is soft-deleted (shipped and fallback: `false`)
- `disableVgaPalette` — Stop loading the VGA color palette into UTF-8 callers' terminals. By default, UTF-8 sessions redefine the terminal's 16 ANSI colors to the exact VGA values and set its default background to black and foreground to light grey (xterm OSC 4/10/11), restoring the terminal's own palette at logoff. Bold colors (pipe codes `|08`-`|15`, `ESC[1;3x` in art) are also sent as explicit bright codes (`ESC[9xm`), since many modern terminals draw bold as a heavier font rather than the bright color. Without this, modern color themes render ANSI art with grey patches wherever explicit black (`ESC[40m`) meets reset cells. CP437 sessions are never affected; terminals without OSC support ignore it (shipped and fallback: `false`)
- `fileListingMode` — `""` or `"lightbar"` (default) / `"classic"`
- `deletedUserRetentionDays` — Days to keep soft-deleted user records before `helper users purge` removes them (shipped: `-1`, keep forever; fallback: `30`)
- `twitListEnabled` — Let callers keep personal [Twit Lists](messages/twit-list.md) and have them applied. Off hides the reader's Ignore command, refuses the list editor, and stops all Twit List filtering, including QWK downloads; saved lists are kept and apply again when it is turned back on (shipped and fallback: `true`, so a `config.json` without the key keeps Twit Lists on)
- `twitListMax` — Most names one caller may put on their Twit List (shipped: `50`; `0` or less also means 50)
- `twitExemptLevel` — Access level from which a user cannot be twit-listed (shipped: `0`, meaning only sysops and co-sysops are exempt). See the [Twit List Technical Reference](messages/twit-list.md#technical-reference)

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

**Door Menu** (the generated `DOORMENU`, see [Generated door menus](doors/generated-menu.md#configjson)):

- `doorMenuMode` — `lightbar` (default) or `list`
- `doorMenuSort` — Default door ordering: `name` (default), `code`, `config` (order in `doors.json`) or `manual` (ascending `sort_order`, then code)
- `doorMenuColumns` — Columns in a door list and the category picker, 1 to 4 (default: `1`)
- `doorCategories` — Array of door categories (`code`, `name`, `description`, `min_access_level`, `acs`, `sort_order`, `sort`, `columns`); with none defined the menu is flat

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

#### IP Blocklist/Allowlist Files

Both blocklist and allowlist files use the same format:

```text
# Comments start with #, on their own line or after an entry
# One IP or CIDR range per line

# Block specific IPs
192.168.1.100
10.0.0.50    # comment after an entry

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

### strings.json

> *Use the [String Editor](advanced/string-editor.md) (`./strings`) to edit display strings interactively. It is a Go reimplementation of the original Vision/2 `STRINGS.EXE` utility. The JSON structure below is for reference.*

This file contains all the customizable text strings displayed by the BBS. You can modify these to personalize your system.

A value does not have to be text. Borrowing from PCBoard's text tables, a value
that starts with `!CODE` runs the `v3_script` door `CODE` from `doors.json` in
place of the text, and one that starts with `%file.ext` shows that file from
the menu set's `ansi/` directory first (`!|` prints a literal `!`). The
`./strings` editor previews such values as `[door CODE]` or `[file NAME]`.
The full rules, including which strings cannot take a hook, are under
[Scripts and files in place of text](advanced/string-editor.md#scripts-and-files-in-place-of-text).

The user Konfig screen (**K** on the main menu) takes all of its text from the
`konfig*` strings, listed under **Konfig:** in `./strings`, so it can be
reworded or translated like any other prompt. A `strings.json` from before
those keys existed still draws the form from built-in fallbacks.

Other groups documented with their features: the quote banners
`quoteTopNet` (fallback `^N wrote:`) and `quoteBottomNet` (blank means no
bottom banner, and there is no fallback), the auto-signature prompts
`msgAutoSigPrompt`, `konfigAutoSigChoice` and `konfigAutoSigAskOn`/`Off`/`Suffix`
([Auto-Signature](messages/autosig.md#technical-reference)), and the `twit*`
strings ([Twit List](messages/twit-list.md#technical-reference)).

#### Key String Categories

**Login/Authentication Strings:**

- `whatsYourAlias` - Legacy Vision/2 alias prompt; not used (the login prompt is `execUsernamePrompt`)
- `whatsYourPw` - Legacy Vision/2 password prompt; not used (the login prompt is `execPasswordPrompt`)
- `systemPasswordStr` - Legacy Vision/2 system password prompt; not used (ViSiON/3 has no system password)
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

#### Example Customization

```json
{
  "pauseString": "|15■|07■|08■ |B4|15SlAm eNtEr!|B0 |08■|07■|15■",
  "defPrompt": "|08•• |15|MN |08•• |13|TL |05Left: "
}
```

#### Pipe Color Codes

The strings support pipe color codes:

- `|00-|15` - Standard 16 colors
- `|B0-|B7` - Background colors
- Special codes: `|CR` (carriage return), `|DE` (clear to end)

### doors.json

> *Use the [Configuration Editor](#configuration-editor-tui) (section 7 — Door Programs) to manage door settings interactively.*

Configures external door programs launched from the BBS: a JSON array of door
records, written by the Configuration Editor. Every key, its type and default
is in the [Door Programs Technical Reference](doors/doors.md#technical-reference),
with the dropfile formats, placeholders and environment variables. The `type`
key is `synchronet_js`, `v3_script`, `rlogin`, `telnet`, or blank for a native
door (`"is_dos": true` for DOS). The generated door menu adds `category`,
`description`, `hidden` and `sort_order`; see
[Generated door menus](doors/generated-menu.md). The `code` of a `v3_script`
door is also what a `strings.json` value names after `!` to run it in place of
its text (see [strings.json](#stringsjson)).

### archivers.json

> *Use the [Configuration Editor](#configuration-editor-tui) (section 9 — Archivers) to manage archiver settings interactively. The JSON structure below is for reference.*

Defines archive formats and the external tools used to pack, unpack, test, and list them. This centralized configuration ensures all subsystems (ZipLab upload pipeline, file area management, archive viewing) use the same archiver definitions, and that different platforms can specify their preferred tool versions.

This follows the classic BBS pattern where archiver definitions are configured once system-wide rather than hardcoded.

#### Default Configuration

If `archivers.json` is missing, built-in defaults are used (ZIP native via Go stdlib).

#### Archiver Definition Structure

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

#### Archiver Field Descriptions

- `id` - Unique short identifier (e.g., "zip", "rar", "7z", "arj", "lha")
- `name` - Human-readable display name
- `extension` - Primary file extension including the dot (e.g., ".zip")
- `extensions` - Additional file extensions for this format (e.g., `[".lzh"]` for LHA)
- `magic` - Hex-encoded magic bytes at file offset 0 for format detection (e.g., "504B0304" for ZIP). The file viewer (**V** in file listings) identifies an archive by the signatures of the enabled archivers first and by extension only when none matches, so a ZIP named `NODELIST.Z75` opens as an archive. Invalid hex is ignored. Upload processing and `helper files` still choose the format by extension
- `native` - When `true`, Go's built-in `archive/zip` stdlib is used for core operations and external commands are ignored for basic pack/unpack. Currently only ZIP supports native mode.
- `enabled` - Controls whether this archiver is active. Disabled archivers are skipped during detection and processing.
- `pack` - Command to create an archive. Placeholders: `{ARCHIVE}`, `{FILES}`, `{WORKDIR}`
- `unpack` - Command to extract an archive. Placeholders: `{ARCHIVE}`, `{OUTDIR}`
- `test` - Command to verify archive integrity. Placeholder: `{ARCHIVE}`
- `list` - Command to list archive contents. Placeholder: `{ARCHIVE}`
- `comment` - Command to add a comment to an archive. Placeholders: `{ARCHIVE}`, `{FILE}`
- `addFile` - Command to add a file to an existing archive. Placeholders: `{ARCHIVE}`, `{FILE}`

#### Built-in Archiver Definitions

| ID    | Name            | Extension(s) | Magic Bytes    | Native | Default State |
| ----- | --------------- | ------------ | -------------- | ------ | ------------- |
| `zip` | ZIP Archive     | .zip         | `504B0304`     | Yes    | Enabled       |
| `7z`  | 7-Zip Archive   | .7z          | `377ABCAF271C` | No     | Disabled      |
| `rar` | RAR Archive     | .rar         | `526172211A07` | No     | Disabled      |
| `arj` | ARJ Archive     | .arj         | `60EA`         | No     | Disabled      |
| `lha` | LHA/LZH Archive | .lha, .lzh   | —              | No     | Disabled      |

To enable additional archive formats, set `"enabled": true` and ensure the corresponding external tool is installed on the system.

#### FTN Bundle Note

ViSiON/3's tosser packs outbound echomail bundles as ZIP and unpacks only ZIP bundles from the inbound, using Go's built-in `archive/zip` and ignoring this file. That is a ViSiON/3 choice, not a FidoNet rule: the standards allow other packers, so a hub that sends ARC, ZOO or LZH bundles must be asked to send ZIP. This config applies to the user-facing archive operations: file area uploads, archive viewing and the ZipLab pipeline.

### ziplab.json

> *Use the [Configuration Editor](#configuration-editor-tui) (key B — ZipLab Upload Processing) to manage these settings interactively.*

Controls ZipLab, which tests, virus-scans and tidies archives as they are uploaded. Archive formats are not set here: ZipLab uses the enabled archivers from [archivers.json](#archiversjson). See [ZipLab Upload Processing](files/ziplab.md) for the pipeline, the settings, and the file format.

### file_areas.json

> *Use the [Configuration Editor](#configuration-editor-tui) (section 3 — Areas and Conferences → File Areas) to manage file area settings interactively. The JSON structure below is for reference.*

Defines file areas available on the BBS. The file contains an array of file area configurations.

#### File Area Structure

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

#### File Area Field Descriptions

- `id` - Unique numeric identifier
- `tag` - Short tag for the area (uppercase)
- `name` - Display name
- `description` - Area description
- `path` - Subdirectory under `data/files/`
- `acs_list` - ACS string required to list files
- `acs_upload` - ACS string required to upload
- `acs_download` - ACS string required to download
- `conference_id` - Conference this area belongs to (0 or omitted = ungrouped)
- `network` - FTN network whose file echo feeds this area (file echoes only)
- `file_echo` - Tag of the file echo delivered into this area, e.g. `TQW_LINUXFILES`; see [FTN File Echoes](files/file-echoes.md#file_areasjson-keys)

#### Access Control Strings (ACS)

- `s10` - Security level 10 or higher
- `fA` - Flag A must be set
- `!fB` - Flag B must NOT be set
- `s20&fC` - Level 20+ AND flag C
- `s10|fD` - Level 10+ OR flag D

### message_areas.json

> *Use the [Configuration Editor](#configuration-editor-tui) (key 3 → Areas and Conferences → Message Areas) to manage message area settings interactively. The JSON structure below is for reference.*

Located in the `configs/` directory. Defines message areas available on the BBS.
Keys: see [Area Properties](messages/message-areas.md#area-properties). Every
writer (the BBS, the config editor and `helper`) takes
`configs/message_areas.json.lock` and merges its change with the file on disk;
the BBS applies changes when no callers are online.

See [Message Areas Guide](messages/message-areas.md) for detailed configuration.

### ftn.json

> *Use the [Configuration Editor](#configuration-editor-tui) (key 4 → Echomail Networking → Echomail Networks / Echomail Links) to manage FTN settings interactively. The JSON structure below is for reference.*

Located in the `configs/` directory. Configures the internal FTN tosser (v3mail) for echomail. Global fields include directory paths (`inbound_path`, `outbound_path`, `binkd_outbound_path`, `temp_path`) and routing tags (`bad_area_tag`, `dupe_area_tag`). Per-network fields include `own_address`, `internal_tosser_enabled`, `origin`, an optional `binkd_outbound_path` (a BSO outbound of the network's own, needed when carrying more than one network), and an optional `nodelist` (the file echo and files its nodelist arrives as, see [FTN Nodelists](messages/nodelists.md)). Per-link fields include `address`, `packet_password`, `areafix_password`, `tic_password` (for inbound file echoes, see [File Echoes](files/file-echoes.md)), `filefix_password` and `filefix_name` (for `helper filefix`), `name`, `flavour`,
and `ip_family` (`ipv4`, `ipv6`, or blank for whichever the hub's name
resolves to). Each network also carries an `auto_create` block, the defaults
given to areas the tosser creates on its own. All of these are tabled in the
[FTN Echomail Technical Reference](messages/ftn-echomail.md#technical-reference).

When a network's `own_address` changes — in the wizard or under **Echomail
Networks** — the save also rewrites the matching `address` line in
`data/ftn/binkd.conf`, so the mailer follows the new node number without a
hand edit. Only the exact old address is replaced; other AKAs are kept.

Hub polling is configured under **Events** in `configs/events.json`; edit the
`echomail_poll_<network>` event's cron schedule. The former per-network
`poll_interval_seconds` field is obsolete and ignored when loading older configs.

See [FTN Echomail Guide](messages/ftn-echomail.md) for setup and full field reference.

### conferences.json

> *Use the [Configuration Editor](#configuration-editor-tui) (key 3 → Areas and Conferences → Conferences) to manage conference settings interactively. The JSON structure below is for reference.*

Located in the `configs/` directory. Defines conferences that group message areas and file areas together for organized display.

#### Conference Structure

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

#### Conference Field Descriptions

- `id` - Unique numeric identifier (must be > 0; areas with `conference_id` of 0 or omitted are ungrouped)
- `tag` - Short tag name (uppercase)
- `name` - Display name shown in area listings
- `description` - Conference description
- `acs` - ACS string required to see this conference's areas (empty = visible to all)

#### How Conference Grouping Works

Message areas and file areas each have an optional `conference_id` field that links them to a conference. When listing areas:

1. Areas with `conference_id` of 0 (or omitted) appear first as ungrouped
2. Areas belonging to conferences are grouped under a conference header
3. Conference ACS is checked — if a user doesn't meet the ACS requirement, the entire conference group is hidden
4. Individual area ACS still applies independently within each conference

#### Conference Header Templates

Conference headers displayed in area listings use templates in `menus/v3/templates/`:

- `MSGCONF.HDR` - Header shown before each conference group in message area listings
- `FILECONF.HDR` - Header shown before each conference group in file area listings

Template placeholders:

- `^CN` - Conference name
- `^CT` - Conference tag

#### ACS Codes for Conferences

Two ACS condition codes reference the user's current conference:

- `C` - Message conference (e.g., `C1` = user is in message conference 1)
- `X` - File conference (e.g., `X1` = user is in file conference 1)

The user's current conference is set automatically when they select an area.

#### Graceful Degradation

If `conferences.json` is missing or empty, the system operates as before — area listings are flat with no conference headers.

### events.json

The event scheduler configuration file defines automated tasks that run on cron-style schedules.

See the complete [Event Scheduler Guide](advanced/event-scheduler.md) for detailed documentation.

#### Basic Structure

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

#### Root Configuration

- **enabled** (boolean): Deprecated and ignored — the scheduler always runs; enable or disable individual events instead
- **max_concurrent_events** (integer): Maximum simultaneous event executions (default: 3). An event that fires while every slot is taken waits for one, for up to its `timeout_seconds`
- **events** (array): List of event configurations

#### Event Configuration Fields

- **id** (string, required): Unique event identifier
- **name** (string, required): Human-readable name for logging
- **schedule** (string): Cron expression (e.g., `"*/15 * * * *"` or `"@hourly"`). Required unless the event has `run_after` or `run_at_startup`
- **command** (string, required): Absolute path to executable
- **args** (array): Command-line arguments (each element is a separate argument)
- **working_directory** (string): Directory to run command in
- **timeout_seconds** (integer): Maximum execution time (0 = no timeout)
- **enabled** (boolean): Enable/disable this event
- **run_at_startup** (boolean): Also run once when the BBS starts
- **run_after** (string): ID of the event this one follows; it runs when that event finishes
- **delay_after_seconds** (integer): Seconds to wait after the `run_after` event finishes
- **environment_vars** (object): Environment variables to set
- **comment** (string): Free-text note, ignored by the BBS

Message areas with `max_messages` or `max_age` set are only purged by an
enabled event running `v3mail purge --all`. Without one, the BBS logs a
warning at startup and the config editor notes it when you set a limit.

#### Cron Schedule Syntax

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

#### Available Placeholders

Use in command arguments or working_directory:

- `{TIMESTAMP}` - Unix timestamp
- `{EVENT_ID}` - Event identifier
- `{EVENT_NAME}` - Event name
- `{BBS_ROOT}` - BBS installation directory
- `{DATE}` - Current date (YYYY-MM-DD)
- `{TIME}` - Current time (HH:MM:SS)
- `{DATETIME}` - Date and time (YYYY-MM-DD HH:MM:SS)

#### Common Use Cases

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

### Menu Configuration

Menu files are located in `menus/v3/` with three components per menu:

#### .MNU Files (Menu Definition)

Located in `menus/v3/mnu/`

Example `LOGIN.MNU`:

```text
RUN:FULL_LOGIN_SEQUENCE
COND:LI:GOTO:MAIN
HOTKEY:A:RUN:AUTHENTICATE
```

#### .CFG Files (Menu Configuration)

Located in `menus/v3/cfg/`

Contains menu settings like:

- ACS requirements
- Password protection
- Display options

#### .ANS Files (Menu Display)

Located in `menus/v3/ansi/`

ANSI art files displayed when the menu loads.

### Theme Configuration

The `menus/v3/theme.json` file controls color schemes:

```json
{
  "yesNoHighlightColor": 31,
  "yesNoRegularColor": 15,
  "chatSysopColor": 11,
  "chatUserColor": 10
}
```

#### Theme Fields

- `yesNoHighlightColor` - DOS color code for highlighted yes/no prompts
- `yesNoRegularColor` - DOS color code for regular yes/no prompts
- `chatSysopColor` - color of the sysop's pane in split-screen chat (default `11`)
- `chatUserColor` - color of the caller's pane in split-screen chat (default `10`)

A copy at `menus.d/<set>/theme.json` overrides the one in `menus/<set>/`.

Standard DOS color codes range from 0-255, where:

- 0-15: Standard 16 colors
- 16-231: Extended color palette
- 232-255: Grayscale

### oneliners.json

Located in the `data/` directory. Stores user-submitted one-liner messages displayed on the BBS.

#### Structure

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

### SSH Host Keys

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

### Logging

> *Use the [Configuration Editor](#configuration-editor-tui) (System Setup → Logging) to manage logging settings interactively. The JSON structure below is for reference.*

ViSiON/3 writes structured JSON logs (one object per line) to a configurable directory. All settings live under the `"logging"` key in `configs/config.json` and are shared by every binary (`vision3`, `v3mail`). If the key is absent, defaults are applied automatically so existing installs continue to work unchanged.

#### TUI: System Setup → Logging

Open the configuration editor (`./config`), choose **System Setup**, then navigate to **Logging** (item 5).

| Field | Description | Default |
|-------|-------------|---------|
| Log Directory | Directory for log files (absolute or relative to BBS root) | `data/logs` |
| Min Level | Minimum severity written: `DEBUG` / `INFO` / `WARN` / `ERROR` | `INFO` |
| Rolling Type | Rotation mode (see below) | `None` |
| Cache Writes | Buffer writes in an 8 KB cache; flushed on errors and clean exit | `Yes` |
| Max Files | Numbered backups (Size mode) or days of files to keep (Daily mode) | `5` |
| Max Size KB | File size in KB that triggers a rotation (Size mode only) | `1024` |

#### Log Levels

| Level | What is written |
|-------|----------------|
| `DEBUG` | Everything — detailed trace output; use only for troubleshooting |
| `INFO` | Normal operation messages (recommended for production) |
| `WARN` | Warnings and errors only |
| `ERROR` | Errors only — quietest setting |

#### Rolling Types

| Type | Behaviour |
|------|-----------|
| `None` (0) | Single file, no rotation — grows indefinitely. Simple; fine for low-traffic systems |
| `Size` (1) | Rotates when the file reaches **Max Size KB**. Old files shift: `vision3.log → vision3.log.1 → … → vision3.log.N`. Files beyond **Max Files** are deleted |
| `Daily` (2) | Opens a new dated file each calendar day (`vision3.YYYY-MM-DD.log`). Files older than **Max Files** days are pruned |

#### Write Caching

When **Cache Writes** is enabled (the default), log lines are held in an 8 KB in-memory buffer before being flushed to disk. This reduces I/O on busy systems. The cache is flushed automatically:

- On every `ERROR`-level record (so critical messages are never lost on crash)
- On clean shutdown (`vision3` exit or SIGTERM)
- On a background ticker (approximately every 5 seconds)

Set to `No` to write every line through immediately. Useful during active debugging when you are `tail -f`-ing the log file.

#### JSON Example

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

#### Log File Locations

Each binary writes to its own file within the configured directory:

| Binary | Log file |
|--------|----------|
| `vision3` | `<dir>/vision3.log` |
| `v3mail` | `<dir>/v3mail.log` |

The directory is created automatically if it does not exist.

#### Log Format

Every line is a JSON object. Example:

```json
{"time":"2026-06-29T14:23:01Z","level":"INFO","msg":"user logged in","node":1,"handle":"AcidBurn"}
```

Fields present on every record:

- `time` — RFC 3339 UTC timestamp
- `level` — `DEBUG`, `INFO`, `WARN`, or `ERROR`
- `msg` — short lowercase description of the event

Additional structured attributes (node, user, error, etc.) vary by event.

### Doctor command line

```bash
./vision3 doctor [--json] [--fix [--dry-run]]
./vision3 --doctor [--json] [--fix [--dry-run]]
```

| Flag | Meaning |
|------|---------|
| `doctor` / `--doctor` | Run the diagnostics and exit instead of starting the BBS. The board root is the current directory |
| `--json` | Write the report as indented JSON on standard output instead of the text report |
| `--fix` | Create the standard data directories (`data/users`, `data/logs`, `data/msgbases`, `data/files`) that are missing, mode `0750`. Requires `--doctor` |
| `--dry-run` | List what `--fix` would create without creating it. Requires `--fix` |

Exit codes: `0` clean, `1` at least one `WARN`, `2` at least one `FAIL` (or
invalid flags: `--fix` without `--doctor`, `--dry-run` without `--fix`). The
exit-code table and the shape of the `--json` document are in the
[Technical Reference](reference/technical.md#board-diagnostics-doctor).

### Environment variables

| Variable | Read by | Effect |
|----------|---------|--------|
| `VISION3_TIMEZONE` | `vision3`, `v3mail` | Timezone used when `timezone` is unset in `config.json`; `TZ` is tried next, then the server's local time |
| `TZ` | all programs | The usual system timezone; the fallback after `VISION3_TIMEZONE` |
| `NO_COLOR` | `vision3 doctor` | Set to anything to print the report without colour, as it is when the output is not a terminal or `TERM` is `dumb` |
| `V3MAIL_NO_CONSOLE_LOG` | `v3mail` | Set to anything to keep log records in `v3mail.log` only. The BBS sets it when it runs `v3mail` for a sysop so the records do not bury the command's own output |
| `VISION3_CONTAINER` | `vision3` | Set by the Docker image; changes the startup pre-flight advice to Docker terms (there is no `setup.sh` in the container) |
