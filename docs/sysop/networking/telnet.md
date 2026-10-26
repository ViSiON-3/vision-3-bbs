# Telnet Server

ViSiON/3 includes a native Telnet server alongside the SSH server. Telnet is
the protocol most classic BBS terminals (SyncTERM, NetRunner, MagiTerm) speak
first, and it is the easiest way for a caller with nothing but a stock `telnet`
command to reach your board. Both protocols share the same session handler and
BBS logic, so a caller gets the same board whichever way they arrive. The
trade-off is that Telnet sends everything, including passwords, in cleartext;
keep SSH as the recommended door and treat Telnet as the welcome mat for retro
clients.

## Setup

Telnet settings are in the config editor: `./config` → **System Setup →
Server Setup**.

| Field | Shipped | Description |
|-------|---------|-------------|
| Telnet Enabled | Y | Enable/disable the Telnet server. A `config.json` without the key leaves Telnet **off** |
| Telnet Host | `0.0.0.0` | Address to listen on; blank means all interfaces |
| Telnet Port | `2323` | TCP port for Telnet connections |

The listener is created at startup, so restart the BBS after changing these.

### Listening on IPv6

**Telnet Host** accepts an IPv4 address, a hostname, or an IPv6 address.
Write an IPv6 address plainly (`::1`, `::`, `2001:db8::5`) or in brackets
(`[::1]`); both are accepted, and a scoped address such as `fe80::1%eth0` works
too. Leave the field blank to listen on every interface.

### Using port 23

The shipped port, 2323, needs no special privileges. To answer on the
standard port 23 while running as an unprivileged user, give the service
`CAP_NET_BIND_SERVICE`; see
[Running as a systemd Service](how-to-guides/systemd-service.md#using-privileged-ports-22--23).

### Testing the listener

```bash
# Connect with system telnet
telnet localhost 2323

# Connect with SyncTerm
# Set connection type to "Telnet" and port to 2323

# Connect with netcat (raw, no NAWS support)
nc localhost 2323
```

## What happens when a caller connects

The server spends its first couple of seconds learning about the caller's
terminal, so that the art it sends fits the screen and the character set the
terminal can draw.

1. **Terminal type.** The server asks the client for its terminal name
   (`syncterm`, `netrunner`, `xterm-256color` and so on). Each step of the
   exchange waits up to 2 seconds for its answer, so a reply that arrives in
   several packets on a slow link is not missed. A client that never answers,
   such as a raw `nc` connection, is treated as `ansi`.
2. **Encoding.** The terminal name is a guess at whether the terminal draws
   CP437 (the DOS character set BBS art is drawn in) or UTF-8. The board then
   asks the terminal directly: it draws one box-drawing character, asks where
   the cursor ended up, and waits up to 750 ms for the answer. A UTF-8 terminal
   reports the cursor one cell on, a CP437 terminal three. The answer overrides
   the guess; no answer keeps it. A caller who has saved `utf8` or `cp437` in
   their User Konfig (`K`) overrides both from login on. See
   [Encoding](advanced/encoding.md).
3. **Screen size.** The server asks the terminal to move its cursor to the far
   corner and report where it landed (waiting up to 3 seconds), which measures
   the rows that are actually usable: SyncTERM's status line takes one row away
   from the size the client advertises. That measurement wins over the size the
   client announced with NAWS (Negotiate About Window Size), even when the NAWS
   figure arrives later. If neither answers, the session runs at 80×25. The
   width is passed through as reported so wider terminals get art hard-wrapped
   correctly; the height is capped at 25 rows.
4. **Login.** Telnet has no protocol-level login, so the caller lands on the
   pre-login matrix and then the `LOGIN` screen, unlike an SSH caller who
   authenticates on the way in.

Clients may send a new NAWS size at any time, for example when the caller
resizes the window; the BBS follows the latest one.

## Troubleshooting

**Art comes out garbled on a Linux or macOS terminal.** The session chose the
wrong encoding. Check the log for `telnet terminal type` and `encoding probe`
lines for that node. If the caller once saved `cp437` in `K` on a UTF-8
terminal, the saved value overrides detection; have them set **Encoding** back
to `Auto` in `K`. See [Encoding](advanced/encoding.md).

**The screen scrolls by one row on a 24-row terminal.** Full-screen art that
ends in the bottom-right corner pushes the terminal down a row. Keep art to 24
rows with the last row short; see
[Full-screen art and short terminals](menus/menu-system.md#full-screen-art-and-short-terminals).

**`nc` or a raw socket shows nothing for a couple of seconds.** That is the
negotiation waiting for answers the client will never send: up to 2 seconds per
phase for the terminal type, then the size probe. A real Telnet client answers
at once.

**Connection refused.** Check **Telnet Enabled** is `Y`, that the port is not
in use (`ss -ltn | grep 2323`), and that the host address exists on this
machine; an IPv6 host on a machine without IPv6 fails at startup with a bind
error in the log.

**Callers from one address are refused.** See the connection limits and IP
filtering in [Security](configuration/security.md).

## Terminal Compatibility

### Tested Clients

- **SyncTerm**: Full compatibility (NAWS + CPR detection handles status bar)
- **PuTTY (Telnet mode)**: Full compatibility
- **Linux/macOS telnet**: Full compatibility
- **netcat**: Works but no NAWS support (defaults to 80x25)

## Technical Reference

### `config.json` keys

| Key | Type | Shipped | Fallback | Meaning |
|-----|------|---------|----------|---------|
| `telnetEnabled` | bool | `true` | `false` | Start the Telnet listener |
| `telnetHost` | string | `"0.0.0.0"` | `0.0.0.0` | Listen address. IPv4, hostname, or IPv6 with or without brackets; blank means all interfaces |
| `telnetPort` | int | `2323` | `2323` | Listen port |

"Shipped" is the value in `templates/configs/config.json`, which setup copies
into `configs/`; "Fallback" is what the loader uses when the key is missing.
The host and port are joined with `net.JoinHostPort`, which is what lets an
IPv6 literal work without hand-bracketing (one enclosing bracket pair is
stripped first, so `[::1]` keeps working).

### Architecture

```text
┌─────────────────────────────────────────────────────────┐
│                    ViSiON/3 BBS                         │
│                  (Go Application)                       │
└─────────────────────┬───────────────────────────────────┘
                      │
                      ▼
┌─────────────────────────────────────────────────────────┐
│          internal/telnetserver/adapter.go               │
│     (Adapts TelnetConn to gliderlabs/ssh.Session)       │
└─────────────────────┬───────────────────────────────────┘
                      │
                      ▼
┌─────────────────────────────────────────────────────────┐
│           internal/telnetserver/telnet.go               │
│     (IAC state machine, NAWS, terminal detection)       │
└─────────────────────┬───────────────────────────────────┘
                      │
                      ▼
┌─────────────────────────────────────────────────────────┐
│                  net.Conn (TCP)                         │
│               Raw TCP connection                        │
└─────────────────────────────────────────────────────────┘
```

The telnet server adapts raw TCP connections to the `gliderlabs/ssh.Session`
interface, allowing the entire BBS to work identically over both protocols.

### Core Components

#### 1. **telnet.go** — Telnet Protocol Handler

Handles all telnet protocol concerns: IAC command parsing, option negotiation, NAWS window size detection, and ANSI cursor position reporting.

Key types:

```go
TelnetConn  // Wraps net.Conn with IAC-transparent Read/Write
telnetState // IAC state machine state (9 states)
```

Key functions:

```go
NewTelnetConn(conn net.Conn) *TelnetConn
Negotiate() error                                   // Send option negotiations
DetectTerminalSize() (width, height int, method string)
Read(p []byte) (int, error)                         // IAC-filtered read
Write(p []byte) (int, error)                        // 0xFF-escaped write
```

#### 2. **adapter.go** — Interface Compatibility Layer

Adapts `TelnetConn` to the `gliderlabs/ssh.Session` interface so the existing `sessionHandler()` works unchanged for telnet connections.

Key types:

```go
TelnetSessionAdapter  // Implements ssh.Session interface
TelnetSessionContext  // Implements context.Context + ssh.Context
```

#### 3. **server.go** — Server Lifecycle

Manages TCP listener, connection acceptance, and per-connection goroutines.

Key types:

```go
Server         // TCP listener and connection dispatcher
Config         // Port, Host, SessionHandler
SessionHandler // func(*TelnetSessionAdapter)
```

### Telnet Protocol Handling

#### Option Negotiation

On connection, the server sends the following telnet option negotiations:

| Sequence            | Meaning                                 |
| ------------------- | --------------------------------------- |
| `IAC WILL ECHO`     | Server will echo input (character mode) |
| `IAC WILL SGA`      | Server suppresses go-ahead              |
| `IAC DO SGA`        | Client should suppress go-ahead         |
| `IAC DONT LINEMODE` | Disable line mode (character-at-a-time) |
| `IAC DO NAWS`       | Request window size from client         |
| `IAC DO TERM_TYPE`  | Request terminal type from client       |

Negotiation runs in two phases, each bounded by a 2-second timeout:

1. **Phase 1** — the six sequences above are sent together; the server reads
   until the client has answered `DO TERM_TYPE` with `WILL` or `WONT`. A
   `WILL TERM_TYPE` sets the `willTermType` flag.
2. **Phase 2** — if `willTermType` is set, the server sends
   `IAC SB TERM_TYPE SEND IAC SE` and reads until the `IS <string>`
   subnegotiation arrives.

Each phase keeps reading until it has the answer it is waiting for rather than
stopping at the first pause. The replies can arrive in several packets on a
real network, and stopping early lost the terminal type, so a UTF-8 terminal
was taken for a CP437 one and sent raw CP437 art.

#### IAC State Machine

All reads pass through a 9-state finite state machine that strips telnet commands transparently:

```text
stateData → stateIAC → stateWill/stateWont/stateDo/stateDont → stateData
          → stateIAC → stateSB → stateSBData → stateSBIAC → stateData
```

| State                    | Description                                              |
| ------------------------ | -------------------------------------------------------- |
| `stateData`              | Normal data — passes bytes through to caller             |
| `stateIAC`               | Received IAC (0xFF) — next byte determines command       |
| `stateWill/Wont/Do/Dont` | Option negotiation — consumes option byte                |
| `stateSB`                | Subnegotiation begin — captures option byte              |
| `stateSBData`            | Accumulating subnegotiation data                         |
| `stateSBIAC`             | IAC inside subnegotiation — SE ends it, IAC escapes 0xFF |

The state machine persists across `Read()` calls, correctly handling IAC sequences that span read boundaries.

#### 0xFF Byte Escaping

Since 0xFF is the IAC control byte in the telnet protocol:

- **Read**: `IAC IAC` (0xFF 0xFF) is unescaped to a single `0xFF` byte
- **Write**: Any `0xFF` in output data is escaped to `IAC IAC`
- Write uses a fast path (direct write) when no 0xFF bytes are present

#### NAWS (Negotiate About Window Size)

When the client supports NAWS (option 31), it sends terminal dimensions via subnegotiation:

```text
IAC SB NAWS [width_high] [width_low] [height_high] [height_low] IAC SE
```

- Width and height are big-endian 16-bit values
- Dimensions are validated (must be 1-255; anything else falls back to 80x25).
  The width is passed through so art can be hard-wrapped for terminals wider
  than 80 columns; the height is capped at 25
- Updates are forwarded to the session adapter through a one-slot channel that
  keeps the **latest** size: an unread older size is replaced rather than the
  new one dropped, so the cursor-report measurement is never overridden by a
  stale NAWS figure queued before it
- Clients may send NAWS updates at any time during the session (e.g., on terminal resize)

#### TERM_TYPE (Terminal Type, RFC 1091)

When the client accepts the `DO TERM_TYPE` request (responds `WILL TERM_TYPE`), the server follows up with a `SEND` subnegotiation and the client replies with its terminal type string:

```text
Server → IAC SB TERM_TYPE SEND IAC SE
Client → IAC SB TERM_TYPE IS <string> IAC SE
```

- The type string is lowercased and trimmed before storage
- Common values: `syncterm`, `netrunner`, `ansi-256color-rgb`, `ansi`, `xterm`, `xterm-256color`
- If the client does not support TERM_TYPE, the default `"ansi"` is used
- The detected type is exposed via `Pty().Term` and flows into the session handler's auto output-mode detection (CP437 vs UTF-8), which the encoding probe described below then confirms or overrides

#### Encoding probe

After the terminal type is known, the session handler (shared with SSH) draws
`│` as three UTF-8 bytes and sends `ESC[6n`. A terminal that decodes UTF-8
reports the cursor one or two cells on; a single-byte terminal reports three.
The probe waits 750 ms; with no answer the terminal-type guess stands. An
empty `preferredEncoding` on the user record means Auto, so this runs on every
call; a saved `utf8` or `cp437` overrides the result from login on.

### Terminal Size Detection

Terminal size detection uses a three-tier strategy:

#### 1. ANSI Cursor Position Report (CPR) — Primary

The most reliable method because it detects the actual usable terminal area, accounting for status bars (e.g., SyncTerm's status row steals one row from the NAWS-reported size).

```text
1. Save cursor position:     ESC[s
2. Move to far corner:       ESC[999;999H  (clamped by terminal)
3. Query cursor position:    ESC[6n
4. Parse response:           ESC[row;colR
5. Restore cursor position:  ESC[u
```

- Waits up to 3 seconds for CPR response
- Response parsed with regex: `\033\[(\d+);(\d+)R`
- Columns are capped at 255 and rows at 25; a reply under 20 columns or 10 rows
  is treated as bogus and replaced by 80 or 25 respectively

#### 2. NAWS — Fallback

Uses dimensions from the telnet NAWS negotiation (already populated during `Negotiate()`).

#### 3. Defaults — Final Fallback

Hard defaults of 80 columns by 25 rows if neither CPR nor NAWS provides valid dimensions.

### Connection Lifecycle

```text
1. TCP Accept          → net.Conn received
2. NewTelnetConn()     → Wrap with IAC-aware reader/writer
3. Negotiate()         → Send WILL/DO/DONT options, read replies (2s per phase)
4. DetectTerminalSize()→ CPR (3s timeout) → NAWS fallback → 80x25 defaults
5. NewTelnetSessionAdapter() → Create ssh.Session-compatible adapter
6. SessionHandler()    → Same handler as SSH sessions (encoding probe, login)
7. Close               → TCP connection closed on session end
```

### Session Adapter Interface

The `TelnetSessionAdapter` implements every method of `gliderlabs/ssh.Session`:

| Method                                   | Telnet Behavior                                                   |
| ---------------------------------------- | ----------------------------------------------------------------- |
| `Read()` / `Write()`                     | Delegates to `TelnetConn` (IAC-filtered/escaped)                  |
| `Close()`                                | Cancels context, closes TCP connection                            |
| `Pty()`                                  | Returns `Term=<negotiated type or "ansi">`, NAWS/CPR dimensions, window change channel |
| `User()`                                 | Returns `""` (forces manual login flow)                           |
| `RemoteAddr()` / `LocalAddr()`           | From underlying TCP connection                                    |
| `Context()`                              | Returns `TelnetSessionContext`                                    |
| `SessionID()`                            | Format: `telnet-{nanotime}-{counter}`                             |
| `Stderr()`                               | Returns self (same stream as stdout for BBS)                      |
| `Environ()` / `Command()`                | Empty (shell session)                                             |
| `PublicKey()` / `Permissions()`          | `nil` / empty (no SSH auth)                                       |
| `SendRequest()`                          | Returns error (not supported)                                     |
| `CloseWrite()` / `Signals()` / `Break()` | No-op                                                             |

### SSH vs Telnet Authentication

A key difference between the two protocols:

- **SSH**: User authenticates at the protocol level → `User()` returns the authenticated username → `sessionHandler` skips the LOGIN menu
- **Telnet**: No protocol-level authentication → `User()` returns `""` → `sessionHandler` presents the LOGIN menu for manual authentication

### Supported Telnet Options

| Option    | Code | Direction     | Purpose                              |
| --------- | ---- | ------------- | ------------------------------------ |
| ECHO      | 1    | `WILL`        | Server echoes input (character mode) |
| SGA       | 3    | `WILL` + `DO` | Suppress go-ahead (full-duplex)      |
| TERM_TYPE | 24   | `DO`          | Request terminal type string         |
| NAWS      | 31   | `DO`          | Request terminal dimensions          |
| LINEMODE  | 34   | `DONT`        | Disable line buffering               |

### Files

#### Telnet Server Package (`internal/telnetserver/`)

- `telnet.go` — TelnetConn, IAC state machine, NAWS, TERM_TYPE negotiation, CPR terminal detection
- `adapter.go` — TelnetSessionAdapter + TelnetSessionContext (ssh.Session interface)
- `server.go` — Server lifecycle, TCP listener, connection handling

#### Integration

- `cmd/vision3/main.go` — Session handler, telnet server startup, login flow
- `cmd/vision3/encoding.go` — Encoding probe timeout and the connect/login encoding decisions
- `internal/util/listen_address.go` — Host and port formatting shared by the Telnet, SSH and V3Net listeners

### Current Limitations

1. **No Encryption**: Telnet transmits data in cleartext (use SSH for secure access)
2. **25-row cap**: Terminal height is capped at 25 rows for BBS compatibility; width is not capped

### References

- Telnet Protocol: RFC 854 (Telnet Protocol Specification)
- NAWS: RFC 1073 (Telnet Window Size Option)
- TERM_TYPE: RFC 1091 (Telnet Terminal-Type Option)
- Linemode: RFC 1184 (Telnet Linemode Option)
- SyncTerm: <https://syncterm.bbsdev.net/>
- gliderlabs/ssh: <https://github.com/gliderlabs/ssh>
