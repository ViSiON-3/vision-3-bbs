# WFC Sysop Console (`wfc`)

`wfc` is a **Waiting-For-Caller** console: the modern stand-in for the screen a
sysop used to leave glowing on the BBS machine overnight. It is a live view of
who's online, what each node is doing, the event scheduler's status, and logs of
caller and bot activity. From it you can **kick** a caller, **watch** a node's
screen as the caller sees it, **type** for them, break in with the classic
**split-screen chat**, and **answer pages** from callers who want the SysOp.
You run it on your own machine (laptop, desktop, another server) and it
connects to a running ViSiON/3 daemon over the BBS's **existing SSH server** —
so it works the same whether the BBS is on localhost or hosted in the cloud.

If the link to the BBS drops, the console **reconnects on its own** and keeps
going; you never have to restart it.

## Requirements to access WFC

A user account can open the WFC console only when **all** of these are true:

1. **Access level ≥ the CoSysOp level** (`coSysOpLevel` in `config.json`,
   default **250**). SysOp (255) and CoSysOp (≥250) qualify; regular users do
   not. WFC does **not** use the single-character ACS `flags` field — it is the
   numeric access level that matters.
2. **A registered SSH public key** on the account. WFC authenticates with your
   SSH key (no password), and the key must be listed on a qualifying account.
   Add it from the **WFC Keys** field in `ue` or with `helper users addkey`
   (see below).
3. **WFC access enabled** — **WFC Access** in the config editor (`./config` →
   **System Setup → Access & Security → Access Levels**), on by default. This is
   hot-reloaded: the change takes effect on the *next* connection attempt, no
   restart needed. (The key behind it is `wfcEnabled`; see the
   [Technical Reference](#technical-reference).)

A key that isn't registered, belongs to a below-CoSysOp user, or arrives while
WFC Access is disabled simply falls through to the **normal caller login**.
Adding WFC access never affects regular logins.

An account can also be marked **WFC Read Only**. Its console shows the
dashboard, logs, events and page list, but the daemon refuses its kick, snoop,
type-in and chat. See [Read-only accounts](#read-only-accounts).

## Getting an SSH key onto the server

WFC authenticates with an SSH keypair on the machine you run `wfc` from. The
**private** key stays on that machine; only the **public** half is registered
on the BBS. The whole flow is:

### 1. Obtain a keypair (on your own machine)

Check whether you already have one:

```bash
ls ~/.ssh/id_ed25519.pub
```

If not, generate one (accept the defaults; a passphrase is optional):

```bash
ssh-keygen -t ed25519
```

### 2. Transfer the public key to the server

Send the **`.pub` file only**. Any channel works — it's public material:

```bash
cat ~/.ssh/id_ed25519.pub        # copy this one line and paste it server-side
scp ~/.ssh/id_ed25519.pub sysop@your-bbs-host:/tmp/my.pub   # or copy the file
```

A public key is a single line starting `ssh-ed25519 AAAA…` ending in a
comment. If you're onboarding a remote co-sysop, this is the line they email
or DM you.

### 3. Register it (on the server)

```bash
helper users addkey "J0hnny A1pha" /tmp/my.pub   # quote handles with spaces
helper users listkeys "J0hnny A1pha"             # confirm it landed
```

Or pipe the pasted line via stdin: `helper users addkey "J0hnny A1pha" -`.
The running BBS picks the key up on the next WFC connection; no restart is
needed (see [When edits take effect](#when-edits-take-effect)).

## Enabling access for a sysop

A user can use WFC once their `accessLevel` is ≥ your `coSysOpLevel` (the
default sysop account is 255) and they have an SSH public key registered. You
can register keys with the built-in tools — no JSON editing required:

- **In `ue`** — open the user, activate the **WFC Keys** field, then `[A]dd` /
  `[D]elete` keys. Keys are shown by SHA256 fingerprint + comment.
- **From the CLI** — `helper users addkey <handle> <keyfile|->`,
  `helper users listkeys <handle>`, and
  `helper users delkey <handle> <fingerprint|index>`. For example, to onboard a
  co-sysop who sent you their `co.pub`:
  ```bash
  helper users addkey TheirHandle co.pub
  ```

Keys are validated with the same SSH library WFC auth uses, so an added key is
guaranteed usable; duplicates and malformed keys are rejected.

You can still edit `data/users/users.json` by hand if you prefer; the
`publicKeys` field is described in the [Technical Reference](#usersjson-fields).

> Keep your **private** key on your own machine only. Only the **public** key
> (`.pub`) goes into `users.json`.

### When edits take effect

`ue` and `helper` are separate programs that write `users.json`. The running
BBS reads the file again, at most once a second, when it saves users, when a
caller changes menu, and on every WFC check: a new connection, each console
command and snapshot, and the 30-second re-check of an open session. A key
added, removed or moved, a level change, a deletion, or the **WFC Read Only**
flag therefore applies without a restart:

- A new connection sees the change at once (allow a second if the file was
  read just before).
- An open console's commands follow the change at once.
- An open console or snoop that is no longer authorized is closed within 30
  seconds.

### Read-only accounts

Set **WFC Read Only** to `Y` in `ue` (right column of the user's edit screen);
the field is `wfcReadOnly` in `users.json`. A read-only
console can watch the dashboard, the logs, the events and the page list.
The daemon refuses everything that acts on a caller: kick, snoop, type-in and
chat. The console shows `READ-ONLY` in its title bar and hides those keys.

Setting the flag on a connected console stops its commands at once, and an
open snoop is closed within 30 seconds. Clearing it restores the commands
without a reconnect. Both apply to edits made with `ue` while the BBS is
running; see [When edits take effect](#when-edits-take-effect).

## Getting `wfc`

The release bundle includes `wfc` from v0.9.4, next to `vision3`, for every
platform. A remote sysop can take it from the bundle for their own machine's
platform; it does not have to match the BBS.

From source, `wfc` is built alongside the other binaries:

```bash
./build.sh                       # builds vision3 … menuedit … wfc (in place)
./dev-setup.sh ~/my-bbs          # or installs all binaries into a target dir
```

For a remote sysop on a different OS, cross-compile a copy for that machine
(pure Go, no CGO):

```bash
GOOS=windows GOARCH=amd64 go build -o wfc.exe ./cmd/wfc   # Windows x64
GOOS=darwin  GOARCH=arm64 go build -o wfc     ./cmd/wfc   # Apple Silicon
GOOS=linux   GOARCH=amd64 go build -o wfc     ./cmd/wfc   # Linux x64
```

## Connecting

```bash
wfc --connect ssh://Felonius@your-bbs-host:2222 --identity ~/.ssh/id_ed25519
```

- The user in the URL is your BBS **handle**; the port is the BBS's SSH port
  (`sshPort` in `config.json`, default 2222).
- Handles with **spaces** must be percent-encoded — a raw space is not valid in
  a URL. For example, `J0hnny A1pha` connects as
  `--connect 'ssh://J0hnny%20A1pha@brokenbit.us:2222'`. (Authorization is
  matched on your SSH key, not the username, so an imperfectly-typed handle
  won't lock you out as long as the key is registered.)
- `--identity` defaults to `~/.ssh/id_ed25519` if omitted.
- On first connect the server's host key is verified against your
  `~/.ssh/known_hosts`. If the host isn't known yet, add it (e.g. with
  `ssh-keyscan`) or use `--insecure` for a one-off (skips host-key verification
  — development/first-run only).

The full list of flags is in the [Technical Reference](#cli-flags).

## Console functions

The console is a single full-screen view: a title bar, a row of counters, an
**Online Now** box listing live callers, a **tabbed lower box** showing the
callers' log, the bot log, or scheduled events, and a command bar. The caller box is
only as tall as it needs to be and the lower box takes the rest. It
needs at least an 80×25 terminal and uses any extra size it is given. It
refreshes on its own (once a second by default; tune with `--refresh`).

### Title bar and counters

The title bar shows the BBS name (from `config.json`; `ViSiON/3 WFC` until the
first snapshot arrives) and, at the right, the console version — or the link
state when something is wrong (see [Reconnecting](#reconnecting) below). A
queued structural config reload is flagged here as `RELOAD PENDING`, and a
console started with `--readonly` or signed in with a read-only account shows
`READ-ONLY`.

The counter row underneath:

| Counter | Meaning |
|---------|---------|
| `Total Users` | Registered accounts (soft-deleted ones excluded) |
| `New Users` | Accounts waiting for sysop validation (not validated, not banned, not deleted) |
| `Mail Waiting` | Unread private mail addressed to the SysOp account (user #1) |
| `Calls Today` | Logins since local midnight: calls in the history plus callers still online |
| `Uptime` | How long the daemon has been running, as `HH:MM:SS` (with a day count past 24h) |

The row is centred; on an 80-column terminal the labels shorten to `Users`,
`New`, `Mail`, `Calls` and `Uptime` so all five fit. A `-` means the daemon could not supply that figure (no PRIVMAIL area, no
user #1). An **older daemon** that predates a counter also shows `-` there,
and its Events tab says so, until the BBS is updated. The counters that scan
data are refreshed every few seconds rather than every tick.

### Online Now

The upper box lists **live callers only** — one row per logged-in user:

| Column | Meaning |
|--------|---------|
| `Handle` | The caller's handle |
| `Activity` | What the caller is doing, or `Menu: NAME` when the screen reports nothing more specific |
| `On` | Time online (`4m`, `1h10m`, `2d3h`) |
| `Address` | Remote IP address (the port is left out; the details view has it) |
| `N#` | Node number |

With nobody on, the box shows a single `...waiting...` row.

### Lower box: Callers, Bots, Events

`TAB` (and `Shift+TAB`) cycles the lower box through three views; the active
tab is highlighted.

**Callers** — the activity log for real callers, newest at the bottom, each
line stamped `HH:MM:SS` with the handle involved:

| Line | Fired when |
|------|-----------|
| `Logged on` | A connection finishes logging in and becomes a caller |
| `Menu: NAME` | A caller moves to a different menu |
| *activity text* | A caller's reported activity changes |
| `Disconnected` | A caller drops or logs off |
| `Kicked by sysop` | A console disconnected that caller |

The console's own link changes are deliberately **not** logged here (see
[Reconnecting](#reconnecting)); the log is about the board, not the console.

**Bots** — the same kind of log for **anonymous connections**: port scanners,
probes, and callers who have not logged in yet, named by IP address with
`Connected` / `Disconnected` lines and the node they used. The count in the
tab label is how many such connections are open right now; on a public board
they usually come and go within a second or two, which is why the count is
normally 0 while the log keeps filling. Keeping this traffic out of the
Callers log is what makes that log readable.

**Events** — the event scheduler's entries from `events.json`:

| Column | Meaning |
|--------|---------|
| `Event` | The event's name (or ID) |
| `Schedule` | Its cron spec, `at startup` for run-at-startup events, or both |
| `Next Run` / `Last Run` | Times shown as `15:04` today, `Tue 15:04` this week, `Jan 02 15:04` otherwise |
| `Status` | `running`, `ok 1.2s`, `failed`, `timed out`, `never run`, or `disabled` |

The caller box is exactly as tall as the callers online, so with two callers
the lower box gets almost the whole screen; when a list is longer than the
space available it scrolls to keep the selected row visible, and the lower box
always keeps at least three rows.

**Where the cursor is.** On the Callers and Bots tabs, `↑`/`↓`, `Enter` and
`K` act on the callers in Online Now. Open the Events tab and the cursor moves
to that list instead (the caller rows lose their highlight); switch away from
Events to return it to the callers.

### Details

`Enter` on a caller or bot shows everything the daemon knows about that
session: node, status, handle, user ID, access level, remote address, current
menu, activity, connect time, time online, last-activity time, and **time
left** in the caller's session. It is the limit the BBS enforces: a CoSysOp or
SysOp, or an account with `timeLimit` `0`, shows `Unlimited`, and chat time is
credited back (see [Chat](#chat)). An older daemon that does not report the
figure shows `(unknown)`.
`Enter` on a scheduled event shows its ID, schedule, next and last run, last
result and duration, and run/failure counts. `Esc` (or `Enter`) closes the
overlay; `↑`/`↓` move to the next row without closing it.

### Kicking a caller

`K` on a selected caller asks for confirmation in the command bar
(`[Y] yes [N] no`), then disconnects that node. The command names the
caller's session, not just the node number, so if that caller has already
left and someone else has taken the slot, the kick is refused with a note to
select again. The caller sees a short
"disconnected by the SysOp" notice, their session ends through the normal
hang-up path (so the disconnect is logged and the node is freed), and every
connected console gets a `Kicked by sysop` line in its Callers log. Kicks are
audited in the BBS log with the admin's handle. `--readonly` and read-only
accounts hide the command, and the daemon refuses it from a read-only account.

### Watching a node

`S` on a caller in the Callers list or the details view opens a snoop on that
node. It uses a second SSH channel (the `wfc-snoop` subsystem) on the
console's existing connection, under the same access rule as the console:
CoSysOp level or above, a registered SSH key, WFC Access on. A
[read-only account](#read-only-accounts) is refused. The rule is re-checked
every 30 seconds, and a snoop whose account has become read-only is closed.

You see the caller's current screen straight away (up to 64 KiB of output since
their last clear-screen), then everything they see from then on. CP437 callers
display correctly on a UTF-8 terminal. During a binary file transfer the screen
shows `[transfer in progress]` instead of the transfer data.

When your terminal is taller than the caller's screen, a status bar on your
bottom row shows the node, handle, caller screen size, mode (`WATCH`, `TYPE` or
`CHAT`), the hotkeys and the last error. Otherwise it is hidden. `Alt-H`
toggles it.

| Key | Action |
|-----|--------|
| `Alt-X` | Leave the snoop and return to the dashboard |
| `Alt-H` | Show or hide the status bar |
| `Alt-T` | Type for the caller (see below) |
| `Alt-C` | Start or end chat (see below) |
| `Alt-S` | Change the chat screen, for the open chat or the next one (see [Chat screens](#chat-screens)) |

If the caller disconnects, `wfc` returns to the dashboard with
`node N disconnected`. The status bar follows your terminal size: it shows when
the terminal is taller than the caller's screen and moves to the new last row
when you resize. `Alt-H` overrides the size rule until you leave the snoop.
The bar waits to redraw while the caller's cursor position is unknown (after an
escape sequence `wfc` does not track) until the caller addresses the cursor
again.
Resize tracking needs a Unix terminal; on Windows the size is read once when the
snoop opens.

The caller is not told they are being watched. Attach and detach are written
to the BBS log with your handle.

### Typing for the caller

`Alt-T` toggles type-in. While it is on, your keystrokes reach the caller's
session as if they had typed them, including inside doors. You must be watching
the node first, and closing your snoop channel releases the keyboard.

Only one sysop can type on a node at a time. A second sysop who tries gets an
error naming who has it.

In type-in a plain `Esc` is sent after 300 ms, `Esc Esc` sends a single `Esc`,
and `Alt-T`, `Alt-C`, `Alt-X` and `Alt-H` stay hotkeys. The log records
type-in on and off with how long it lasted and how many bytes were sent. It
never records the keystrokes.

### Chat

`Alt-C` toggles chat. It opens a split screen on the caller's terminal with
your text in one area and the caller's in another. Each area word-wraps and
scrolls on its own. The shipped menu set opens the `CHAT1` screen: a framed
box for each side under a title row with the time and date, and the minutes
in chat at the bottom. With no screen configured the built-in layout is used:
your text on top, the caller's below, and a bar between them with both
handles and the time (`HH:MM`). Text colors come from the screen, or from
`chatSysopColor` and `chatUserColor` in
[`theme.json`](menus/menu-system.md#theme-themejson) where the screen leaves
them out. See [Chat screens](#chat-screens) for designing your own.

Chat works only while the caller is in the BBS: menus, prompts, the message
reader, the editor. It is refused while they are in a door, a file transfer or
the teleconference (`CHAT`), and the reason shows in your status bar. Watching and type-in still work there.
Chat opens as soon as the caller's session is waiting for a key. If that takes
more than 3 seconds the request is refused.

Chat ends when you press `Alt-C` again, when the caller presses `Esc` twice (it
closes about half a second after the second `Esc`), or when your snoop channel
closes. The caller's screen is put back as it was, including a half-typed line.
If more than 64 KiB was drawn since their last clear-screen it cannot be
restored, and they see `[back from chat, press Enter]`.

Chat time is not charged to the caller's time limit, and the idle timeout is
paused. If you end chat with `Alt-C` and were typing for the caller before it,
you return to type-in; otherwise to watching.

When the caller ends chat with `Esc` `Esc`, you lose the keyboard, including a
type-in you had before chat. Anything you type after that is thrown away
rather than landing on the caller's prompt. The snoop screen still shows
`CHAT` until you press `Alt-C`, which returns you to watching with "chat ended
by caller" in the status bar. The event log shows "Chat with <sysop> started"
and "Chat ended" for each chat.

### Chat screens

A chat screen is ANSI art for the background plus a layout saying where each
side types and where the names, time, date and elapsed minutes go, as
ViSiON/2's `CHAT.n` and `CHATNFO.n` did. They live in the menu set, so a board
can have several; the format is in
[Chat screens](menus/menu-system.md#chat-screens).

`sysopChatScreen` in `theme.json` names the screen `Alt-C` opens; the shipped
set's is `CHAT1`, and the config editor's Theme page changes it. It can be
`random`, a different screen each chat. `Alt-S` on the snoop screen cycles
through the others: the board's default, then the built-in layout, then
`random` when the board has two screens or more, then every other screen in
the set by name, such as `V2CHAT1` and `V2CHAT2`, the two screens ViSiON/2
shipped. The status bar shows `screen NAME` while a choice other than
the default is selected (and `screen random` when that is the default), and
the choice goes with the next `Alt-C` or answered page.

Pressing `Alt-S` during a chat switches that chat: the caller's screen is
redrawn on the new one and both sides' text moves onto its typing lines, the
line being typed still open. A screen the chat cannot use (missing, broken, or
bigger than the caller's terminal) is refused with the reason on the status
bar, and the chat keeps its screen. A daemon older than the console reports
no screens, and `Alt-S` says so; one that predates switching refuses it.

A screen is drawn for a terminal size (80x25 unless its layout says
otherwise). A caller whose terminal is smaller gets the built-in layout
instead, as does one whose screen fails to load; the daemon log says why,
and `vision3 doctor` reports a screen that cannot load.

### Pages

A caller pages you with the [`PAGESYSOP`](reference/menu-commands.md) menu
command and gives a one-line reason. With no `wfc` console connected they see
"The SysOp is not available right now." immediately.

Otherwise every connected console shows a `PAGE` badge with the number of
waiting pages and rings the terminal bell once for each new page. The caller
sees "Paging SysOp..." with a countdown and a bell each second. They can cancel
by pressing a key; a caller who cancels this way gets no message. After `pageSysopTimeout` seconds with no answer they see the
not-available message. A caller can send one page per `pageSysopCooldown`
seconds.

`P` opens the page list: node, handle, reason and age. `Enter` opens the snoop
on that node and starts chat; `Esc` goes back. A `--readonly` console, or one signed in with a read-only account, can open the list but cannot answer: `Enter` is refused. Pages that were answered, timed
out or cancelled stay in the list, marked, until the caller logs off.

The Alt keys arrive as `ESC` followed by a letter. In macOS Terminal and iTerm,
set the Option key to send Meta or Esc+, or press `Esc` and then the letter.

The wait and the cooldown are **Page Timeout** and **Page Cooldown** in the
config editor (`./config` → **System Setup → Access & Security → Access
Levels**); the keys are `pageSysopTimeout` (default 60) and `pageSysopCooldown`
(default 300) in `config.json`. Changes apply on config reload with no restart.
The caller's prompts are in the string editor; see
[String Editor](advanced/string-editor.md#strings-for-sysop-chat-and-paging).

`PAGESYSOP` is not bound in the shipped menu set. To offer it, add a command
such as `RUN:PAGESYSOP` on a key of your choice in `menus.d/v3/cfg/MAIN.CFG`
with `menuedit`; see [Menus & ACS](menus/menu-system.md#paging-the-sysop).

### Scrolling the logs

Both logs follow their newest entry. `PgUp` scrolls back a screenful at a
time and `PgDn` returns toward the tail; while scrolled back, the header row
shows how many newer lines lie below (`12 newer - PgDn`). On the Events tab
`PgUp`/`PgDn` move the cursor a page at a time. Each log keeps the most recent
events in memory (200 by default; adjust with `--max-events`), and switching
tabs jumps back to the tail.

### Reconnecting

The console watches its link to the BBS three ways: the event stream, the
once-a-second snapshot feed, and SSH keepalives (every 15 seconds, answered
within 10). A dropped connection — the BBS restarting, a laptop waking from
sleep, a NAT mapping expiring — is noticed within about 25 seconds at worst
and usually at once.

When the link drops the screen stays up: the title bar switches to
`OFFLINE - retry in Ns`, the Online Now caption turns red, the last known rows
are dimmed, and the command bar shows the reason for a few seconds. The
console then redials on its own, backing off from 1 second up to 30 seconds
between attempts, and shows `Reconnected` in the command bar when the BBS is
back. Press `R` to retry immediately instead of waiting, or `Q` to quit.

Link changes are not written to the logs and take no room in the event
history. Instead the title bar keeps a running tally after the first drop,
e.g. `WFC v1.0 - 12 drops, last 07:01` (shortened beside a long board name),
so a flapping link is visible at a glance. A laptop that sleeps overnight
typically shows one drop per wake cycle; that is the machine, not the BBS. If the snapshot feed
stalls for 30 seconds while the connection still looks alive, the console
treats that as a dead link and reconnects too.

While connected, `R` asks the daemon for an immediate refresh.

The very first connection is different: if it fails (wrong key, unknown host,
access denied) `wfc` prints the error and exits, so a misconfiguration is
reported plainly instead of turning into a retry loop.

## Navigating the console

| Key | Action |
|-----|--------|
| `↑` / `↓` / `Home` / `End` | Select a row |
| `TAB` / `Shift+TAB` | Switch the lower box between Callers, Bots and Events |
| `Enter` | Show details for the selected row |
| `Esc` | Close the details overlay |
| `K` | Kick the selected caller (asks `Y`/`N` first) |
| `S` | Watch the selected caller (snoop) |
| `P` | Open the page list |
| `PgUp` / `PgDn` | Scroll the log back / forward (page the cursor on Events) |
| `R` | Refresh now (not shown on the bar); retry the connection now when offline |
| `Q` / `Ctrl+C` | Quit |

On the snoop screen: `Alt-X` dashboard, `Alt-T` type-in, `Alt-C` chat, `Alt-S` chat screen, `Alt-H` status bar.

## Troubleshooting

**`ssh: handshake failed: … unable to authenticate, attempted methods [none
publickey], no supported methods remain`** — the server saw your key and
declined it. `wfc` has no password fallback, so the connection ends. In order
of likelihood:

1. The public key isn't registered on the account.
2. The key was added to a different account than you're thinking of, or the
   account's `accessLevel` is below `coSysOpLevel` (default 250).
3. `wfcEnabled` is toggled off in the server config.

Check which key your client is offering with
`ssh-keygen -lf ~/.ssh/id_ed25519.pub` and compare fingerprints against
`helper users listkeys "Your Handle"` on the server.

The BBS log narrows it down, but note the two levels:

- A **registered** key rejected for insufficient access level or disabled WFC
  access is logged at **info**, with the reason.
- An **unregistered** key is logged at **debug** with its fingerprint only, so
  at normal log levels a wrong or unknown key leaves no visible record. Raise
  the log level to debug if you see no rejection logged at all.

**`Host key verification failed`** — the server isn't in your
`~/.ssh/known_hosts` yet. Add it with
`ssh-keyscan -p 2222 your-bbs-host >> ~/.ssh/known_hosts`, or use
`--insecure` for a one-off first connection.

**Connection lands at the normal BBS login screen instead of the console** —
same causes as the handshake failure above: the key fell through to caller
login because it didn't match a qualifying account.

## Security model

- **Key-based auth only.** WFC presents your SSH public key; there is no
  password path for the console.
- **Authorization is re-checked server-side** when the admin channel opens — a
  valid key alone is not enough; the account must still be at/above CoSysOp
  level.
- **Additive, non-disruptive.** Unknown or under-privileged keys fall through
  to the normal caller login; existing logins are unchanged.
- **Re-checked while connected.** An open WFC session re-verifies every 30
  seconds that the *key* it authenticated with is still registered to a
  qualifying account. Turning **WFC Access** off, removing the key, or banning,
  demoting or deleting the user disconnects their console within that window,
  whether the change is made in the running BBS or with `ue` or `helper`.
- **Everything is visible to every qualifying account.** The console shows all
  active sessions — including **invisible** ones — with each caller's handle,
  IP address, and activity, to *any* account at or above `coSysOpLevel`.
  Granting level 250 grants this visibility; set `coSysOpLevel` accordingly.
- **Sanitized display.** Caller-supplied text (handles) is stripped of
  terminal control characters before rendering, and control characters are
  rejected in new handles at registration.
- **Audited.** Every admin session open/close and every command — including
  each kick, with the admin's handle and address — is written to the BBS log
  via structured logging. Unknown public-key offers are logged at debug level
  with the key fingerprint.
- **Kick, type-in and chat change things.** Any account that can open the console can
  disconnect any node, watch it, type for the caller and chat, unless the
  account is marked **WFC Read Only**. Watching is not shown to the caller.
  Mark a co-sysop's account read-only if they should only look at the
  dashboard. The daemon enforces it on the admin channel and the snoop
  channel. `--readonly` only changes the console it is passed to, so it does
  not restrict anyone else.
- **Host-key verified.** The client checks the daemon's SSH host key against
  `known_hosts` unless you pass `--insecure`.

Because WFC rides the BBS's existing SSH server, you do **not** need to open any
additional port for it.

## Technical Reference

Everything above can be done from `ue`, `helper` and the config editor. This
section is for hand-editing and scripting.

### CLI flags

| Flag | Purpose |
|------|---------|
| `--connect ssh://user@host:port` | Admin endpoint (required). The user is your BBS handle, percent-encoded if it has spaces; the port is the BBS's SSH port (`sshPort`, default 2222) |
| `--identity <path>` | SSH private key (default `~/.ssh/id_ed25519`) |
| `--known-hosts <path>` | known_hosts file (default `~/.ssh/known_hosts`) |
| `--insecure` | Skip SSH host-key verification (dev/first-run only) |
| `--ascii` | ASCII borders instead of box-drawing characters |
| `--no-color` | Disable color |
| `--refresh <ms>` | Snapshot poll interval in milliseconds (default 1000) |
| `--max-events <n>` | Events kept in the feed (default 200) |
| `--readonly` | View-only on this console: disables kick and snoop (and so type-in and chat). It is a client setting; use a [read-only account](#read-only-accounts) to restrict another sysop |
| `--version` / `--help` | Print version / usage |

`wfc` exits with status 2 when `--connect` is missing or malformed, 1 when the
key or home directory cannot be read, and prints the SSH error and exits when
the very first connection is refused. Once connected it reconnects on its own
(see [Reconnecting](#reconnecting)).

### `config.json` keys

| Key | Type | Default | Meaning |
|-----|------|---------|---------|
| `wfcEnabled` | bool | `true` | Allow WFC console connections. **WFC Access** in the config editor. Hot-reloaded; applies to the next connection and to the 30-second re-check of open consoles |
| `coSysOpLevel` | int | `250` | Minimum access level for a WFC account (and for snoop, type-in and chat) |
| `sshPort` / `sshHost` | int / string | `2222` / `0.0.0.0` | The SSH listener `wfc` connects to; see [SSH Server](networking/ssh.md) |
| `pageSysopTimeout` | int | `60` | Seconds a `PAGESYSOP` page waits for an answer. **Page Timeout** (5–600) |
| `pageSysopCooldown` | int | `300` | Minimum seconds between one caller's pages. **Page Cooldown** (0–86400) |

### `users.json` fields

| Field | Type | Meaning |
|-------|------|---------|
| `publicKeys` | array of strings | OpenSSH public-key lines (`authorized_keys` format) that may open the console. Omitted when empty |
| `wfcReadOnly` | bool | The account's console may watch but not kick, snoop, type in or chat. Omitted when false |
| `accessLevel` | int | Must be at or above `coSysOpLevel` |

```json
{
  "handle": "Felonius",
  "accessLevel": 255,
  "publicKeys": [
    "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAA... sysop@laptop"
  ],
  "wfcReadOnly": false
}
```

The running BBS re-reads `users.json` as described in
[When edits take effect](#when-edits-take-effect), so hand edits need no
restart either.

### `helper users` key commands

| Command | Purpose |
|---------|---------|
| `helper users addkey <handle> <keyfile\|->` | Register a public key (`-` reads the pasted line from stdin) |
| `helper users listkeys <handle>` | List the account's keys by SHA256 fingerprint and comment |
| `helper users delkey <handle> <fingerprint\|index>` | Remove a key |

### `theme.json` keys

| Key | Default | Meaning |
|-----|---------|---------|
| `chatSysopColor` | `11` | Sysop's pane in split-screen chat |
| `chatUserColor` | `10` | Caller's pane in split-screen chat |

See [Theme](menus/menu-system.md#theme-themejson) for the color numbering.

### Strings

The caller-side text for pages (`pageSysopReasonPrompt`, `pageSysopPaging`,
`pageSysopUnavailable`, `pageSysopCooldown`) lives in `strings.json` and is
edited with `./strings`; see
[Strings for sysop chat and paging](advanced/string-editor.md#strings-for-sysop-chat-and-paging).

### Protocol

The console and the snoop are two SSH **subsystems** on the BBS's SSH server:
`wfc-admin` carries the dashboard snapshot, event stream and commands, and
`wfc-snoop` carries a watched node's raw output and the sysop's keystrokes.
Both are opened on the one SSH connection `wfc` makes, so no extra port is
involved. Access to either is decided only after the client's key **signature**
verifies; a key offered without a signature is not an identity. The daemon
re-checks the account every 30 seconds for as long as either channel is open.
