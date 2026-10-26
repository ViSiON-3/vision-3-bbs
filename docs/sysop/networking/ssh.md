# SSH Server

ViSiON/3 includes a built-in SSH (Secure Shell) server implemented in pure Go —
no CGO or native libraries required. It is the recommended way for callers to
reach your board: passwords and screens travel encrypted, a caller's SSH
username is their BBS handle so they skip the login screen, and the same server
carries the [WFC sysop console](how-to-guides/wfc-console.md) so you never open
a second port for administration. Retro terminals such as SyncTERM and
NetRunner connect over it too, with the legacy algorithms they need switched on
by default.

Key features:

- **Pure Go** — no CGO, no system library dependencies
- **Legacy algorithm support** — optional older SSH algorithms for retro BBS clients (SyncTERM, NetRunner)
- **Read interrupt support** — `BBSSession` wraps `ssh.Session` to allow clean door program I/O cancellation
- **Password and keyboard-interactive authentication**
- **Public-key authentication for the WFC console** (callers still log in with a password)
- **Configurable server banner** (`Version` field)

## Setup

SSH settings are in the config editor: `./config` → **System Setup → Server
Setup**.

| Field | Shipped | Description |
|-------|---------|-------------|
| SSH Enabled | Y | Enable/disable the SSH server |
| SSH Host | `0.0.0.0` | Address to listen on; blank means all interfaces |
| SSH Port | `2222` | TCP port to listen on |
| Legacy SSH | Y | Offer older SSH algorithms for retro client compatibility (see below) |

The listener is created at startup, so restart the BBS after changing these.

### Listening on IPv6

**SSH Host** accepts an IPv4 address, a hostname, or an IPv6 address. Write an
IPv6 address plainly (`::1`, `::`, `2001:db8::5`) or in brackets (`[::1]`);
both are accepted, and a scoped address such as `fe80::1%eth0` works too.
Leave the field blank to listen on every interface.

### Using port 22

The shipped port, 2222, needs no special privileges and stays clear of the
machine's own `sshd`. To answer on port 22 while running as an unprivileged
user, give the service `CAP_NET_BIND_SERVICE`; see
[Running as a systemd Service](how-to-guides/systemd-service.md#using-privileged-ports-22--23).

## SSH Host Keys

Host keys are stored in `configs/`:

- `ssh_host_rsa_key` — RSA private key (auto-generated on first run)
- `ssh_host_rsa_key.pub` — RSA public key

Host keys are generated automatically by `setup.sh`, `dev-setup.sh`, the Docker
entrypoint or the BBS startup if they don't exist. Keep the private key
readable by the BBS user only; `vision3 doctor` warns when it is readable by
group or others. Back it up with the rest of `configs/`: a new host key makes
every caller's client complain that the server's identity changed.

## Legacy Algorithm Support

When **Legacy SSH** is on (`legacySSHAlgorithms: true`, the shipped and
fallback default), the server advertises a broader set of SSH algorithms needed
by retro BBS terminal software:

| Category | Additional algorithms |
| -------- | --------------------- |
| Key exchange | `diffie-hellman-group14-sha1`, `diffie-hellman-group1-sha1` |
| Ciphers | `aes128-cbc`, `aes256-cbc`, `3des-cbc` |
| MACs | `hmac-sha1` |

When `false`, only modern secure algorithms are offered. The modern set is
always offered as well, so turning Legacy SSH on costs nothing for OpenSSH or
PuTTY callers; it only widens the choice for older clients.

> **Tip:** Leave Legacy SSH on if SyncTERM or other retro clients fail to connect.

## How a caller logs in

An SSH client sends a username as part of the connection, so an SSH caller
whose username matches a handle authenticates at the protocol level (password
or keyboard-interactive) and goes straight into the login sequence, skipping
the pre-login matrix and the `LOGIN` screen that Telnet callers see.

Before the first screen the board detects the terminal's encoding: it draws
one box-drawing character, asks the terminal where its cursor ended up, and
waits up to 750 ms for the answer. A UTF-8 terminal and a CP437 terminal
report different columns, and the answer overrides the guess made from the
terminal type. A caller who has saved `utf8` or `cp437` in their User Konfig
(`K`) overrides detection from login on. See [Encoding](advanced/encoding.md).

## Supported Clients

Tested and working:

- **SyncTERM** — BBS-specific terminal with CP437/ANSI graphics support
- **OpenSSH** (`ssh` command line)
- **PuTTY**
- **mRemoteNG**

## Remote Sysop Console (WFC)

The `wfc` Waiting-For-Caller console connects over **this same SSH server** to
monitor live node/caller activity from your own machine — no extra port to open.
Sysops are authenticated by SSH public key (CoSysOp level or above). The
console and its node-watching channel are SSH subsystems (`wfc-admin` and
`wfc-snoop`) rather than shell sessions, so they never collide with a caller
login. See the [WFC Sysop Console](how-to-guides/wfc-console.md) guide for
building `wfc`, registering a key, and connecting.

## Troubleshooting

### Connection Refused

- Check that SSH Enabled is **Y** in `./config` → System Setup → Server Setup
- Verify the port is not in use: `ss -ltn | grep 2222` (or `netstat -ln`)
- Check that `configs/ssh_host_rsa_key` exists and is readable
- If **SSH Host** is an IPv6 address, make sure the machine has that address;
  a bind failure is logged at startup

### SyncTERM or Retro Client Fails to Connect

- Make sure Legacy SSH is **Y** in `./config` → System Setup → Server Setup, then restart the BBS

### Authentication Fails

- Verify the username and password are correct in the user database
- Check BBS logs for authentication error messages
- A caller below `logonLevel`, or a soft-deleted account, is refused after the
  password check; see [User Management](users/user-management.md)

### Garbled art on a UTF-8 terminal

The session chose the wrong encoding, usually because the caller saved `cp437`
in `K` on a UTF-8 terminal. Have them set **Encoding** back to `Auto`. See
[Encoding](advanced/encoding.md).

## Technical Reference

### `config.json` keys

| Key | Type | Shipped | Fallback | Meaning |
|-----|------|---------|----------|---------|
| `sshEnabled` | bool | `true` | `true` | Start the SSH listener |
| `sshHost` | string | `"0.0.0.0"` | `0.0.0.0` | Listen address. IPv4, hostname, or IPv6 with or without brackets; blank means all interfaces |
| `sshPort` | int | `2222` | `2222` | Listen port |
| `legacySSHAlgorithms` | bool | `true` | `true` | Offer the legacy key exchanges, ciphers and MACs listed above in addition to the modern set |

"Shipped" is the value in `templates/configs/config.json`, which setup copies
into `configs/`; "Fallback" is what the loader uses when the key is missing.
The host and port are joined with `net.JoinHostPort` (one enclosing bracket
pair is stripped first), which is what lets an IPv6 literal work.

### Algorithms offered with Legacy SSH on

| Category | Algorithms, in preference order |
|----------|--------------------------------|
| Key exchange | `curve25519-sha256`, `curve25519-sha256@libssh.org`, `ecdh-sha2-nistp256`, `ecdh-sha2-nistp384`, `ecdh-sha2-nistp521`, `diffie-hellman-group14-sha256`, `diffie-hellman-group16-sha512`, `diffie-hellman-group14-sha1`, `diffie-hellman-group1-sha1` |
| Ciphers | `chacha20-poly1305@openssh.com`, `aes128-gcm@openssh.com`, `aes256-gcm@openssh.com`, `aes128-ctr`, `aes192-ctr`, `aes256-ctr`, `aes128-cbc`, `aes256-cbc`, `3des-cbc` |
| MACs | `hmac-sha2-256-etm@openssh.com`, `hmac-sha2-512-etm@openssh.com`, `hmac-sha2-256`, `hmac-sha2-512`, `hmac-sha1` |

With Legacy SSH off the server uses the `golang.org/x/crypto/ssh` defaults.

### Files

| Path | Purpose |
|------|---------|
| `configs/ssh_host_rsa_key`, `.pub` | Host key pair, created on first start if missing |
| `internal/sshserver/server.go` | `Server` struct wrapping `gliderlabs/ssh.Server`; `BBSSession` wrapping `ssh.Session` with read interrupt support |
| `cmd/vision3/ssh_server.go` | Registers the `wfc-admin` and `wfc-snoop` subsystems |
| `internal/util/listen_address.go` | Host and port formatting shared with the Telnet and V3Net listeners |

### Package: `internal/sshserver`

The SSH server wraps [`gliderlabs/ssh`](https://github.com/gliderlabs/ssh),
which itself wraps `golang.org/x/crypto/ssh`. All sessions are handled via the
`gliderlabs/ssh.Session` interface, which the BBS session handler uses for both
SSH and telnet connections.

- **`Config`** — Server configuration (host key path, address, port, legacy algorithms, session/password/keyboard-interactive handlers, subsystem handlers, verified public-key callback)
- **`BBSSession.SetReadInterrupt(ch)`** — Registers a channel that cancels a blocked `Read()` with `ErrReadInterrupted`, used by door programs for clean I/O teardown
- **`VerifiedPublicKeyCallback`** — Called by `x/crypto` only after a key's signature verifies; the WFC console's identity comes from here and from nowhere else, so a public key offered without a signature is never an identity

### Session Interface

Both SSH and telnet sessions implement `gliderlabs/ssh.Session`, allowing the BBS session handler to be protocol-agnostic. SSH sessions are created directly by `gliderlabs/ssh`; telnet sessions use a compatible adapter.
