# WFC Sysop Console (`wfc`)

`wfc` is a **Waiting-For-Caller** console: a live, read-only view of who's
online, what each node is doing, and a feed of recent system events. You run it
on your own machine (laptop, desktop, another server) and it connects to a
running ViSiON/3 daemon over the BBS's **existing SSH server** — so it works
the same whether the BBS is on localhost or hosted in the cloud.

This version is **monitor-only**: it does not disconnect nodes, send messages,
or start sysop chat. Those are planned for a later release.

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
| `--readonly` | View-only (always true in this version) |
| `--version` / `--help` | Print version / usage |

## Console functions

The console is a single full-screen view with four parts: a status header, the
node table, an optional event log, and a command bar. It refreshes on its own
(once a second by default; tune with `--refresh`).

### Status header

One line across the top with live system stats:

| Field | Meaning |
|-------|---------|
| System name | The BBS name from `config.json` (falls back to `ViSiON/3 WFC` before the first snapshot arrives) |
| `Nodes` | Number of currently active nodes (connections) |
| `Calls Today` | Calls answered since midnight; shows `—` if the counter is unavailable |
| `Uptime` | How long the daemon has been running, as `Xd Xh Xm` |
| Clock | Current local time on *your* machine (`HH:MM:SS`) |

### Node table

One row per active connection, including callers still at the login prompt:

| Column | Meaning |
|--------|---------|
| `Handle` | The caller's handle, or `(login)` if not yet logged in |
| `Status` | Coarse node status — see below |
| `Activity` | What the caller is doing right now (e.g. reading messages), if the current screen reports it |
| `Menu` | The menu the caller is currently in |
| `Address` | The caller's remote IP address and port |

Status values:

- **`login`** — connected but not yet authenticated
- **`online`** — logged in
- **`menu`** — logged in and sitting in a menu (no other activity reported)
- **`chat`** / **`idle`** — reserved for later releases

Use `↑`/`↓` to select a row and `Enter` to open the details view.

### Node details

`Enter` on a node shows everything the daemon knows about that session: node
number, status, handle, user ID, access level, remote address, current menu,
current activity, connect time, last-activity time, and **time left** in the
caller's session (minutes remaining against their time limit, `(unknown)` if
the account has no limit). `Esc` returns to the node table.

### Event log

Press `L` to split the screen and show a live feed of recent system events,
newest at the bottom, each stamped `HH:MM:SS` with the handle involved:

| Event | Fired when |
|-------|-----------|
| `caller.connected` | A new connection appears |
| `caller.disconnected` | A node drops or logs off |
| `menu.changed` | A caller moves to a different menu |
| `activity.changed` | A caller's reported activity changes |

The feed keeps the most recent events in memory (200 by default; adjust with
`--max-events`). Press `L` again to hide the log and give the node table the
full screen.

### Refresh and reconnect

The console polls the daemon for a fresh snapshot once a second (configurable
with `--refresh`) and receives events as they happen; `R` forces an immediate
refresh. If the SSH connection drops, a **Disconnected** banner replaces the
screen — press `R` to reconnect (the console re-subscribes to the event feed
automatically) or `Q` to quit.

## Navigating the console

| Key | Action |
|-----|--------|
| `↑` / `↓` | Select a node |
| `Enter` | Show node details |
| `Esc` | Back to the node list |
| `R` | Refresh now (also reconnect when disconnected) |
| `L` | Show/hide the event log panel |
| `Q` / `Ctrl+C` | Quit |

The screen refreshes about once a second on its own. If the connection drops,
the console shows a **Disconnected** banner; press `R` to reconnect or `Q` to
quit — it will not crash.

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
`helper users listkeys "Your Handle"` on the server. The server also logs
every rejection with a reason (`wfc access disabled` vs `insufficient access
level`), so the BBS log tells you exactly which rule fired.

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
- **Re-checked while connected.** An open WFC session re-verifies its
  authorization every 30 seconds. Turning **WFC Access** off, or banning or
  demoting the user from within the running BBS, disconnects their open
  console within that window. Key removals made with `ue` or `helper` edit
  `users.json` on disk, which the daemon only reads at startup — so **revoking
  a key still requires a BBS restart** to lock out new connections.
- **Everything is visible to every qualifying account.** The console shows all
  active sessions — including **invisible** ones — with each caller's handle,
  IP address, and activity, to *any* account at or above `coSysOpLevel`.
  Granting level 250 grants this visibility; set `coSysOpLevel` accordingly.
- **Sanitized display.** Caller-supplied text (handles) is stripped of
  terminal control characters before rendering, and control characters are
  rejected in new handles at registration.
- **Audited.** Every admin session open/close (and every command) is written to
  the BBS log via structured logging. Unknown public-key offers are logged at
  debug level with the key fingerprint.
- **Host-key verified.** The client checks the daemon's SSH host key against
  `known_hosts` unless you pass `--insecure`.

Because WFC rides the BBS's existing SSH server, you do **not** need to open any
additional port for it.
