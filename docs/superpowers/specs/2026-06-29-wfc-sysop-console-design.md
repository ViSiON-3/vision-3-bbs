# ViSiON/3 WFC Sysop Console — Design

**Date:** 2026-06-29
**Branch:** `feature/wfc`
**Status:** Approved design, pending implementation plan

## 1. Purpose

Add a new ViSiON/3 companion binary, `wfc`, that gives a sysop a traditional
"Waiting For Caller" console for a **running** ViSiON/3 daemon. The console
monitors live node/caller activity and recent system events. It is built to work
for **modern, remote/cloud deployments**: a sysop runs `wfc` on their own
desktop (any OS) and connects securely to a BBS hosted elsewhere (e.g. a cloud
VM at a public IP).

`wfc` is an admin console, **not** the BBS daemon. It observes and (in later
phases) controls a separate, already-running `vision3` process.

### Scope of this version (v1)

v1 is **read-only monitoring**. It renders a live WFC screen — system header,
node table, node-detail view, and a live event feed — and performs **no
mutations** to caller sessions. The command path (`Execute`) exists in the
interface and wire protocol from day one so that mutations (send message,
disconnect, sysop chat) can be added later without reshaping the architecture,
but **no mutating command is implemented in v1**.

## 2. Why the original spec needed reshaping (technical realities)

An earlier draft assumed the daemon already exposes an "admin control API",
an "event hub", and a "node/session state store" that `wfc` connects to, with a
transport abstraction over unix socket / named pipe / loopback TCP / SSH. The
codebase does not match that:

1. **The BBS is a single in-memory daemon.** `vision3` is one long-running
   process; every caller is a goroutine inside it. All live node state lives in
   one place: `SessionRegistry`, a `map[int]*BbsSession` guarded by a mutex
   (`internal/session/registry.go`, `internal/session/session.go`). `BbsSession`
   already holds `User`, `CurrentMenu`, `Activity`, `StartTime`, `LastActivity`,
   `Invisible`, `RemoteAddr`, and a `PendingPages` queue.

2. **There is no IPC, admin API, or general event hub.** Every companion binary
   (`ue`, `helper`, `v3mail`, `config`, `strings`, `menuedit`) coordinates with
   the daemon **only through shared JSON files on disk** — they never talk to the
   running process. A separate `wfc` process therefore **cannot read
   `SessionRegistry`**; live state exists only in the daemon's memory. The only
   event broadcaster that exists is chat-specific (`internal/chat`), not a
   system-wide hub.

3. **The daemon already runs an SSH server with a host key**
   (`cmd/vision3/ssh_server.go`, via `gliderlabs/ssh`). It currently accepts all
   connections and defers to the BBS login menu; it pre-authenticates known users
   by password. **There is no public-key user auth yet.**

4. **Sysop identity == access level.** `255` = SysOp, `250` = CoSysOp
   (`internal/menu/acs_checker.go`).

5. **The TUI stack is already chosen.** `ue`, `config`, `menuedit`, and `strings`
   all use **Bubble Tea + Lip Gloss + Bubbles** (charmbracelet, with vendored
   forks under `third_party/`). The SSH client lib (`golang.org/x/crypto/ssh`) is
   pure-Go.

The consequences that drive this design:

- Live state must be **served by the daemon**; a standalone process can only get
  it over the network.
- Because the daemon already speaks SSH, the admin RPC should **ride an SSH
  channel** rather than introduce a second exposed network listener.
- Because there is no event hub, v1 **synthesizes events by diffing snapshots**
  inside the admin server, instead of instrumenting the whole daemon.

## 3. Architecture

### 3.1 Layering (hard boundary)

WFC-specific logic does **not** live in the daemon. The TUI is a reusable
package that depends only on an interface.

```
cmd/wfc/main.go            CLI: flags, config load, build an AdminClient, run wfcui
internal/wfcui/            transport-agnostic Bubble Tea TUI; depends ONLY on admin.AdminClient
  model.go view.go update.go keys.go styles.go details.go
internal/admin/            the contract + server + client implementations
  client.go      AdminClient interface + shared data types (SystemSnapshot, NodeState, Event, AdminCommand, Result)
  server.go      runs inside the daemon: owns SessionRegistry access, authz, audit, snapshot + diff engine
  client_inproc.go  InProcessClient: direct calls to server (for embedded SSH admin / --wfc classic)
  client_ssh.go     SSHChannelClient: dials BBS SSH, opens "wfc-admin" subsystem, speaks the wire protocol
  wire.go        frame encoding (length-prefixed JSON)
```

**Dependency rule:** `internal/wfcui` imports `internal/admin` for the interface
and data types **only**. It never imports `internal/session`, `internal/menu`,
or daemon internals. This keeps the TUI testable in isolation and reusable by
both `cmd/wfc` and the daemon's embedded admin path.

`internal/admin/server.go` is the **only** code that touches `SessionRegistry`,
and it does so read-only, under the existing `BbsSession.Mutex` RLock, copying
into serialization-safe projection structs.

### 3.2 The `AdminClient` contract

```go
type AdminClient interface {
    Snapshot(ctx context.Context) (*SystemSnapshot, error)
    Subscribe(ctx context.Context) (<-chan Event, error) // server-push event feed
    Execute(ctx context.Context, cmd AdminCommand) (*Result, error) // v1: only CommandRefresh
    Close() error
}
```

Two implementations satisfy it:

- **`InProcessClient`** — calls the server directly in the daemon process. Used
  by the daemon's embedded SSH admin path and the `--wfc` classic local console.
  No serialization.
- **`SSHChannelClient`** — used by `cmd/wfc`. Dials the BBS SSH server, opens the
  `wfc-admin` subsystem channel, and exchanges length-prefixed JSON frames.

Because both satisfy the same interface, `internal/wfcui` is identical in all
deployment modes.

### 3.3 Data types (serialization-safe projections)

`SystemSnapshot`, `NodeState`, and `Event` are plain JSON structs — **no**
`net.Conn`, mutex, or channel fields. The server builds them by reading
`BbsSession` under RLock and copying scalar fields. v1 `NodeState` carries at
least: `NodeID`, `Status`, `Handle`, `UserID`, `AccessLevel`, `RemoteAddr`,
`CurrentMenu`, `Activity`, `Invisible`, `ConnectedAt`, `LastActivity`. `TimeLeft`
is included **best-effort**: it is derived (user time limit minus elapsed) only
where a time-limit source exists, and omitted otherwise — it is not a stored
`BbsSession` field. `SystemSnapshot` carries `Time`, `SystemName`, `Uptime`,
`Nodes []NodeState`, and a `Counters` struct populated only from sources that
exist (see §6). `Event` carries `Time`, `Type`, `NodeID`, `Handle`, `Message`.

Status values are derived by the server from session fields (e.g. `online`,
`login` when `User == nil`, `menu`, plus `idle` for empty nodes) — the session
struct has no explicit status enum, so the server maps it.

## 4. Transport & authentication

### 4.1 SSH subsystem channel

`cmd/wfc` is an SSH client to the **existing** BBS SSH server. It requests an
SSH **subsystem** named `wfc-admin` (gliderlabs/ssh exposes
`SubsystemHandlers`) instead of a shell/PTY. Normal callers request a shell/PTY
— a different channel type — so the admin path and the caller path are fully
isolated. The admin RPC frames flow over this single encrypted channel.

The embedded-SSH admin experience (`ssh sysop@bbs` landing directly in the WFC
TUI) is the **same** subsystem path, with the daemon running `wfcui` server-side
against an `InProcessClient`.

### 4.2 Public-key auth (the user-system change)

This is the only change to the auth system, and it is **additive**:

- Add a `PublicKeys []string` field to the `User` struct
  (`internal/user/user.go`), stored in `data/users/users.json`. Because Go JSON
  unmarshaling tolerates missing fields, existing user records load fine with an
  empty list — **no hard migration**. (A later convenience: let `ue`/`config`
  register a key; not required for v1.)
- Add a `PublicKeyHandler` to the SSH server. A presented key is matched against
  registered `User.PublicKeys`. If it matches a user **and** that user's access
  level ≥ CoSysOp, the connection is flagged admin-eligible. A non-matching key
  returns `false`, falling through to today's password / keyboard-interactive
  caller login. **The caller experience is unchanged.**

### 4.3 Authorization & audit

- Authorization is **re-checked server-side** when the `wfc-admin` subsystem is
  opened — not only at key-auth time: identity → access level → allow/deny, with
  a `--readonly` cap that forbids any mutating command.
- Host-key verification on the client (known-hosts style) protects against MITM.
- Every accepted admin session and every command executed is `slog`-audited:
  `sysop`, `command`, `node`, `remote_addr`. (v1 audits session
  open/close and the no-op `CommandRefresh`; mutations audit when added.)

### 4.4 Why SSH and not a second listener

The daemon already exposes SSH with a managed host key, connection tracking, and
IP allow/block lists. Riding an SSH channel reuses all of that, adds nothing new
to the public attack surface, requires no second TLS cert or token file, and
gives a turnkey cross-platform client (every OS has an SSH stack via
`x/crypto/ssh`). Unix-socket / named-pipe transports are intentionally **not**
implemented: they only help when `wfc` runs on the same host, which contradicts
the cloud requirement, and SSH-to-loopback already covers the same-host case.

## 5. Live-state production — snapshot + diff engine (v1 decision)

**Decision:** v1 does **not** build a daemon-wide event hub. A real hub would
require instrumenting dozens of session call sites (logon/logoff/menu-change/
activity-change), which contradicts the "don't fork BBS logic" boundary.

Instead, `internal/admin/server.go`:

1. Polls `SessionRegistry.ListActive()` on the refresh interval (default 1s),
   builds a `SystemSnapshot` under RLock.
2. Diffs against the previous snapshot to synthesize events:
   `caller.connected`, `caller.disconnected`, `menu.changed`, `activity.changed`.
3. Serves current state via `Snapshot` and the synthesized events via
   `Subscribe`. Events are held in a bounded ring buffer (`--max-events`,
   default 200).

This keeps daemon/session changes at essentially zero while the screen still
feels live at a 1s refresh. A push-based event hub can later replace the diff
engine **behind the same `AdminClient` interface and wire protocol**, with no
change to `wfcui` or `cmd/wfc`.

## 6. Header counters (no invented data)

Counters are populated only from existing sources:

- `Uptime`, `ActiveNodes` — from the registry / daemon start time.
- `CallsToday` / recent-call info — from `data/users/callhistory.json` if present.

Counters with no backing store in the current codebase (e.g. uploads/downloads/
messages today) are **omitted** in v1 rather than shown as zero or fabricated.
They can be added when a data source exists.

## 7. TUI (`internal/wfcui`)

- **Stack:** Bubble Tea + Lip Gloss + Bubbles, matching the other companion
  TUIs.
- **Layout:** 80×25 minimum — header (system name / uptime / calls) / node table
  (node, status, user, activity, idle, time-left) / event feed / command bar.
- **Keys (v1):** `↑/↓` select node, `Enter` node details, `R` refresh, `L`
  toggle log panel, `?` help, `Q` quit, `Esc` back. Mutating keys (`C` chat,
  `M` message, `D` disconnect) are **rendered disabled** so the layout is already
  shaped for the next phase.
- **Node details view:** node, status, handle, user ID, remote address, current
  menu, activity, connected-at, last-activity, and time-left (best-effort, per
  §3.3). Sensitive fields respect the viewer's role.
- **Flags affecting rendering:** `--ascii` swaps the Lip Gloss border set to
  ASCII; `--no-color` strips styling.
- **Guards:** "Terminal too small for WFC display" screen below 80×25;
  **Disconnected** banner with retry/quit on transport loss — never panic.

## 8. `cmd/wfc` CLI & configuration

```
wfc --connect ssh://sysop@bbs.example.com:6023   # remote (primary)
wfc --readonly        # explicit; v1 is read-only regardless
wfc --ascii
wfc --no-color
wfc --refresh <ms>    # default 1000
wfc --max-events <n>  # default 200
wfc --identity <path> # SSH private key for auth
wfc --version | --help
```

Connection target and identity may also come from the sysop's existing SSH
config / `vision3` config where practical; explicit flags win. Host-key
verification uses a known-hosts file.

## 9. Cross-platform / Windows 386

Pure-Go throughout (`x/crypto/ssh`, charmbracelet, `gliderlabs/ssh`). No CGO and
**no** platform-specific IPC code (no unix-socket or named-pipe paths) because
SSH is the single transport — so the platform matrix reduces to "does Go build
it," which holds for all targets:

```
windows/386  windows/amd64  linux/amd64  linux/arm64  darwin/amd64  darwin/arm64
```

The event feed uses a bounded ring buffer to keep memory predictable on small
targets.

## 10. Build order (validate the TUI before the network)

1. **In-process core.** `internal/admin` types + `server` (snapshot + diff
   engine) + `InProcessClient`, wired into a daemon `--wfc` classic local
   console. Proves `internal/wfcui` end-to-end with zero protocol risk.
   *(Decision: `--wfc` classic mode is treated as an internal dev/test scaffold
   in v1, not a marketed feature. It can be promoted later.)*
2. **Remote client.** `wire.go` + the `wfc-admin` subsystem handler +
   `SSHChannelClient` + `PublicKeyHandler` + `User.PublicKeys`. Delivers
   `cmd/wfc` remote — the primary deliverable.
3. **Embedded SSH admin.** `ssh sysop@bbs` routed straight into server-side
   `wfcui` over the same subsystem, reusing everything from steps 1–2.

## 11. Error handling

- No panics on connection loss: `wfcui` shows a **Disconnected** banner with
  retry/quit.
- Distinct messages for: cannot connect, auth failed (key not recognized or user
  not a sysop), and terminal too small.

## 12. Testing

- **`internal/admin`:** snapshot-projection tests; table-driven **diff-engine**
  tests (connect / disconnect / menu-change / activity-change → correct events);
  authz matrix (key → access level → allow/deny, `--readonly`).
- **`wire`:** round-trip encode/decode.
- **`client_inproc`:** against a fake/stub registry.
- **`internal/wfcui`:** Bubble Tea `Update` tests driven by synthetic
  snapshots/events — no real SSH required.
- **SSH auth:** extend the existing `cmd/vision3/ssh_auth_test.go` pattern to
  cover the new public-key path (recognized sysop key, recognized non-sysop key,
  unknown key falls through to caller login).

Run `go test ./...`, plus `gofmt`/`go vet` on changed files, before considering
any task done. Run cross-compile build checks for the six targets in §9.

## 13. Non-goals (v1)

- No mutating commands (send message, disconnect, change node status) — interface
  and wire protocol reserve space; implementations come later.
- No real-time sysop⇄caller chat bridge (the hardest piece: it requires invasive
  changes to the caller session's live I/O loop). Deferred to a dedicated phase.
- No web/browser dashboard, no unix-socket/named-pipe transports, no second
  network listener, no silent caller spying, no persistent chat transcripts.
- No daemon-wide push event hub (diff engine stands in; see §5).

## 14. Acceptance criteria (v1)

- Builds succeed for all six targets in §9, including `windows/386`, with no CGO.
- `wfc --help` and `wfc --version` work.
- `wfc --connect ssh://…` authenticates with a sysop public key, opens the
  `wfc-admin` subsystem, and renders system header, node table, and live event
  feed.
- A connecting key that maps to a non-sysop, or an unknown key, is denied admin
  and (for an unknown key) falls through to the normal caller login unchanged.
- Node selection and node-details view work; the screen updates as node state
  changes (≈1s).
- `--readonly` and `--ascii` work; mutating keys appear disabled.
- Transport loss shows a Disconnected banner and allows retry/quit without
  panicking.
- Admin session open/close is `slog`-audited.

## 15. Open decisions recorded (default-accepted; revisit on review)

1. **Diff-engine vs. event hub (§5):** v1 uses polling + snapshot-diff. Accepted
   as the default to keep daemon changes near-zero.
2. **`--wfc` classic local mode (§10):** treated as an internal dev/test scaffold
   in v1, not a marketed feature.
