# WFC Sysop Console (`wfc`)

`wfc` is a **Waiting-For-Caller** console: a live view of who's online, what
each node is doing, the event scheduler's status, and logs of caller and bot
activity, with a **kick** command to drop a caller. You run it on your
own machine (laptop, desktop, another server) and it connects to a running
ViSiON/3 daemon over the BBS's **existing SSH server** — so it works the same
whether the BBS is on localhost or hosted in the cloud.

If the link to the BBS drops, the console **reconnects on its own** and keeps
going; you never have to restart it. Sysop chat and paging a caller are not in
this release.

## Requirements to access WFC

A user account can open the WFC console only when **all** of these are true:

1. **Access level ≥ the CoSysOp level** (`coSysOpLevel` in `config.json`,
   default **250**). SysOp (255) and CoSysOp (≥250) qualify; regular users do
   not. WFC does **not** use the single-character ACS `flags` field — it is the
   numeric access level that matters.
2. **A registered SSH public key** on the account. WFC authenticates with your
   SSH key (no password), and the key must be listed on a qualifying account.
3. **WFC access enabled** — the `wfcEnabled` config flag (default **true**).
   Toggle it in the config TUI under **System Configuration → Access Levels**
   as **WFC Access**. This is hot-reloaded: the change takes effect on the
   *next* connection attempt, no restart needed.

A key that isn't registered, belongs to a below-CoSysOp user, or arrives while
WFC Access is disabled simply falls through to the **normal caller login**.
Adding WFC access never affects regular logins.

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

### 3. Register it and restart (on the server)

```bash
helper users addkey "J0hnny A1pha" /tmp/my.pub   # quote handles with spaces
helper users listkeys "J0hnny A1pha"             # confirm it landed
```

Or pipe the pasted line via stdin: `helper users addkey "J0hnny A1pha" -`.
Then **restart the BBS** — the daemon reads `users.json` at startup only.

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

You can still edit `data/users/users.json` by hand if you prefer — add a
`publicKeys` array of OpenSSH public-key lines:

```json
{
  "handle": "Felonius",
  "accessLevel": 255,
  "publicKeys": [
    "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAA... sysop@laptop"
  ]
}
```

> Keep your **private** key on your own machine only. Only the **public** key
> (`.pub`) goes into `users.json`.

> **Restart note:** `ue` and `helper` are separate programs that edit
> `users.json`; the running BBS loads users at startup and does **not**
> hot-reload that file. After adding or removing a key while the BBS is running,
> **restart the BBS** for the change to take effect.

## Building `wfc`

`wfc` is built alongside the other binaries:

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

### CLI flags

| Flag | Purpose |
|------|---------|
| `--connect ssh://user@host:port` | Admin endpoint (required) |
| `--identity <path>` | SSH private key (default `~/.ssh/id_ed25519`) |
| `--known-hosts <path>` | known_hosts file (default `~/.ssh/known_hosts`) |
| `--insecure` | Skip SSH host-key verification (dev/first-run only) |
| `--ascii` | ASCII borders instead of box-drawing characters |
| `--no-color` | Disable color |
| `--refresh <ms>` | Snapshot poll interval in milliseconds (default 1000) |
| `--max-events <n>` | Events kept in the feed (default 200) |
| `--readonly` | View-only: hides and disables the kick command |
| `--version` / `--help` | Print version / usage |

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
queued structural config reload is flagged here as `RELOAD PENDING`.

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
| `Connection lost: …` / `Reconnected` | The console's own link to the BBS dropped or came back |

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
left** in the caller's session (`(unknown)` if the account has no limit).
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
audited in the BBS log with the admin's handle. `--readonly` hides the command
entirely.

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
`OFFLINE - retry in Ns`, the Online Now caption turns red, the last known rows are
dimmed, and the Callers log records `Connection lost:` with the reason. The
console then redials on its own, backing off from 1 second up to 30 seconds
between attempts, and logs `Reconnected` when the BBS is back. Press `R` to
retry immediately instead of waiting, or `Q` to quit. If the snapshot feed
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
| `PgUp` / `PgDn` | Scroll the log back / forward (page the cursor on Events) |
| `R` | Refresh now (not shown on the bar); retry the connection now when offline |
| `Q` / `Ctrl+C` | Quit |

## Troubleshooting

**`ssh: handshake failed: … unable to authenticate, attempted methods [none
publickey], no supported methods remain`** — the server saw your key and
declined it. `wfc` has no password fallback, so the connection ends. In order
of likelihood:

1. The public key isn't registered on the account — or was registered but the
   **BBS wasn't restarted** afterward.
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
  qualifying account. Turning **WFC Access** off, or banning, demoting, or
  deleting the user in the running BBS, disconnects their console within that
  window. Key edits made with `ue` or `helper` change `users.json` on disk,
  which the daemon only reads at startup — so revoking a key that way still
  requires a **BBS restart** to take effect.
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
- **Kick is the only mutation.** Any account that can open the console can
  disconnect any node; there is no separate permission level. Run remote
  consoles with `--readonly` if a co-sysop should only watch.
- **Host-key verified.** The client checks the daemon's SSH host key against
  `known_hosts` unless you pass `--insecure`.

Because WFC rides the BBS's existing SSH server, you do **not** need to open any
additional port for it.
