# WFC Sysop Console (`wfc`)

`wfc` is a **Waiting-For-Caller** console: a live, read-only view of who's
online, what each node is doing, and a feed of recent system events. You run it
on your own machine (laptop, desktop, another server) and it connects to a
running ViSiON/3 daemon over the BBS's **existing SSH server** — so it works
the same whether the BBS is on localhost or hosted in the cloud.

This version is **monitor-only**: it does not disconnect nodes, send messages,
or start sysop chat. Those are planned for a later release.

## Requirements to access WFC

A user account can open the WFC console only when **both** of these are true:

1. **Access level ≥ the CoSysOp level** (`coSysOpLevel` in `config.json`,
   default **250**). SysOp (255) and CoSysOp (≥250) qualify; regular users do
   not. WFC does **not** use the single-character ACS `flags` field — it is the
   numeric access level that matters.
2. **A registered SSH public key** on the account. WFC authenticates with your
   SSH key (no password), and the key must be listed on a qualifying account.

A key that isn't registered — or that belongs to a below-CoSysOp user — simply
falls through to the **normal caller login**. Adding WFC access never affects
regular logins.

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
| `--max-events <n>` | Events kept in the feed (default 200) |
| `--readonly` | View-only (always true in this version) |
| `--version` / `--help` | Print version / usage |

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

## Security model

- **Key-based auth only.** WFC presents your SSH public key; there is no
  password path for the console.
- **Authorization is re-checked server-side** when the admin channel opens — a
  valid key alone is not enough; the account must still be at/above CoSysOp
  level.
- **Additive, non-disruptive.** Unknown or under-privileged keys fall through
  to the normal caller login; existing logins are unchanged.
- **Audited.** Every admin session open/close (and every command) is written to
  the BBS log via structured logging.
- **Host-key verified.** The client checks the daemon's SSH host key against
  `known_hosts` unless you pass `--insecure`.

Because WFC rides the BBS's existing SSH server, you do **not** need to open any
additional port for it.
