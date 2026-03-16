# V3Net: Native Message Networking Protocol

## Overview

V3Net is a REST+SSE message networking protocol for Vision/3. It is firewall-friendly, trivial to set up, and supports real-time events (logon/logoff, inter-BBS chat) that FTN echomail cannot provide. Designed for sysops who want a native Vision/3 network without mailer software and nodelist management.

**V3Net is additive.** Vision/3 continues to support FTN-based echomail networks (FidoNet, fsxNet, etc.) alongside V3Net. No existing FTN/echomail code is modified, removed, or deprecated. V3Net is a parallel, independent networking stack.

The first named network running on V3Net is **felonynet**.

## Architecture

Hub-and-leaf topology:
- **Hub**: HTTP server that stores messages, manages subscriptions, and fans out events via SSE
- **Leaf**: Client that polls the hub for new messages, maintains an SSE connection for real-time events, and writes received messages to local JAM message bases

### Node Identity

`node_id` = lowercase hex of the first 16 bytes of `SHA-256(raw_ed25519_public_key)` (16 hex chars = 8 bytes). Generated once on first run, persisted at `v3net.keystore_path`.

### Authentication

Every authenticated request includes two HTTP headers:

```
X-V3Net-Node-ID: a3f9e1b2c4d5e6f7
X-V3Net-Signature: <base64url(ed25519_sign(privkey, canonical_string))>
```

Canonical string to sign: `{METHOD}\n{PATH}\n{DATE_UTC}\n{SHA256_HEX(body)}`

The hub validates the node ID exists in its subscriber list, the `Date` header is within ±5 minutes, and the signature verifies against the stored public key.

## Wire Format — Message Object

```jsonc
{
  "v3net": "1.0",
  "network": "felonynet",
  "msg_uuid": "550e8400-e29b-41d4-a716-446655440000",
  "thread_uuid": "550e8400-e29b-41d4-a716-446655440000",
  "parent_uuid": null,
  "origin_node": "bbs.example.net",
  "origin_board": "General",
  "from": "Darkstar",
  "to": "All",
  "subject": "Hello from the underground",
  "date_utc": "2026-03-16T04:20:00Z",
  "body": "...",
  "tearline": "Vision/3 BBS * Somewhere, Earth",  // optional
  "attributes": 0,
  "kludges": {}
}
```

### Validation Rules

- `v3net` must be `"1.0"` (reject others with HTTP 422)
- `msg_uuid`, `thread_uuid` must be valid UUID v4
- `parent_uuid` is null or valid UUID v4
- `network` must match `[a-z0-9_-]{1,32}`
- `date_utc` must parse as RFC 3339
- `from` and `to`: 1–64 printable ASCII characters
- `subject`: 1–128 characters
- `body`: non-empty, ≤ 32,768 bytes (truncate with kludge `"v3net_truncated": true` if over limit)

## Hub REST API

Base path: `/v3net/v1`

| Method | Endpoint | Auth | Description |
|---|---|---|---|
| GET | `/v3net/v1/networks` | No | List networks this hub serves |
| GET | `/v3net/v1/{network}/info` | No | Full metadata for a single network |
| GET | `/v3net/v1/{network}/messages` | Yes | Fetch messages newer than cursor (`since`, `limit` params) |
| POST | `/v3net/v1/{network}/messages` | Yes | Submit a message (deduplicates by `msg_uuid`) |
| GET | `/v3net/v1/{network}/events` | Yes | SSE stream (ping, logon, logoff, new_message, chat) |
| POST | `/v3net/v1/{network}/chat` | Yes | Inter-BBS chat (1 msg/sec/node rate limit, not persisted) |
| POST | `/v3net/v1/subscribe` | No | Register a new leaf node |

### SSE Event Types

- `ping` — every 30s keepalive
- `logon` / `logoff` — user presence events with handle, node, timestamp
- `new_message` — notification with network, msg_uuid, from, subject
- `chat` — inter-BBS chat with from, node, text, timestamp

Leaf nodes reconnect with exponential backoff (base 5s, max 5min, jitter ±10%) on disconnect.

### Central Registry

Optional static JSON file for network discovery. Not required for operation — nodes can connect to a hub directly.

Canonical URL: `https://raw.githubusercontent.com/ViSiON-3/v3net-registry/main/registry.json`

Cached in memory for 1 hour. Startup does not fail if unreachable.

## Package Layout: `internal/v3net/`

```
internal/v3net/
  protocol/               # Shared wire types, constants, validation
  keystore/               # ed25519 keypair generation and persistence
  dedup/                  # SQLite-backed UUID deduplication index
  hub/                    # Hub HTTP server
  leaf/                   # Leaf client (polls hub, maintains SSE)
  registry/               # Central registry fetch + parse
  events/                 # SSE event types and broadcaster
cmd/
  v3net-hub/              # standalone hub binary (optional later)
```

Reuses existing `internal/jam/` for JAM message base I/O.

## New Files

| File | Purpose |
|---|---|
| `internal/v3net/protocol/message.go` | `Message` struct with JSON tags, `Validate() error` |
| `internal/v3net/protocol/event.go` | `Event` struct and typed payloads (Logon, Logoff, NewMessage, Chat, Ping) |
| `internal/v3net/protocol/network.go` | `NetworkInfo`, `NetworkPolicy`, `RegistryNetwork` structs |
| `internal/v3net/protocol/message_test.go` | Table-driven validation tests |
| `internal/v3net/keystore/keystore.go` | `Keystore` — Load/create keypair, NodeID, Sign, PubKeyBase64 |
| `internal/v3net/keystore/keystore_test.go` | Round-trip, node ID stability, signature verification |
| `internal/v3net/dedup/dedup.go` | `Index` — SQLite-backed UUID dedup (Seen, MarkSeen, LastSeen) |
| `internal/v3net/dedup/dedup_test.go` | Dedup index tests |
| `internal/v3net/hub/hub.go` | `Hub` struct and constructor |
| `internal/v3net/hub/config.go` | `Config` and `NetworkConfig` structs |
| `internal/v3net/hub/server.go` | HTTP router setup, stdlib `net/http` only |
| `internal/v3net/hub/handlers.go` | One handler per endpoint |
| `internal/v3net/hub/auth.go` | Auth middleware — validate node ID + signature |
| `internal/v3net/hub/subscribers.go` | SQLite-persisted subscriber registry |
| `internal/v3net/hub/messages.go` | SQLite message storage, cursor-based pagination |
| `internal/v3net/hub/events.go` | SSE broadcaster with ping goroutine |
| `internal/v3net/hub/hub_test.go` | Integration tests via `httptest.NewServer` |
| `internal/v3net/leaf/leaf.go` | `Leaf` struct and constructor |
| `internal/v3net/leaf/config.go` | `Config` struct |
| `internal/v3net/leaf/jam.go` | `JAMWriter` interface (implementation injected from BBS) |
| `internal/v3net/leaf/poller.go` | Polling goroutine — fetch, dedup, write to JAM |
| `internal/v3net/leaf/sse.go` | SSE connection with exponential backoff reconnect |
| `internal/v3net/leaf/sender.go` | SendMessage, SendChat, SendLogon, SendLogoff |
| `internal/v3net/leaf/leaf_test.go` | Mock hub integration tests |
| `internal/v3net/registry/registry.go` | `Fetch()` with 1-hour cache via `sync.Map` |

## Modified Files

| File | Change |
|---|---|
| Config file | Add `[v3net]`, `[v3net.hub]`, `[[v3net.leaf]]` sections |
| BBS startup | Load keystore, open dedup index, start leaf/hub if enabled |
| Logon/logoff hooks | Fire `leaf.SendLogon` / `leaf.SendLogoff` |
| Message post path | Call `leaf.SendMessage` after local JAM write succeeds |
| Sysop menus | Add "V3Net Status" — node ID, hub status, leaf subscription status |

## Database Schemas

### Dedup Index (`internal/dedup/`)

```sql
CREATE TABLE IF NOT EXISTS seen_messages (
  msg_uuid       TEXT PRIMARY KEY,
  network        TEXT NOT NULL,
  local_jam_msgnum INTEGER,
  seen_at        DATETIME DEFAULT (datetime('now'))
);
```

### Hub Subscribers (`hub/subscribers.go`)

```sql
CREATE TABLE IF NOT EXISTS subscribers (
  node_id    TEXT NOT NULL,
  network    TEXT NOT NULL,
  pubkey_b64 TEXT NOT NULL,
  bbs_name   TEXT,
  bbs_host   TEXT,
  status     TEXT NOT NULL DEFAULT 'pending',
  created_at DATETIME DEFAULT (datetime('now')),
  PRIMARY KEY (node_id, network)
);
```

### Hub Messages (`hub/messages.go`)

```sql
CREATE TABLE IF NOT EXISTS messages (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  msg_uuid    TEXT UNIQUE NOT NULL,
  network     TEXT NOT NULL,
  data        TEXT NOT NULL,
  received_at DATETIME DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_messages_network ON messages(network, id);
```

## JAM Integration

Map `protocol.Message` fields to JAM headers:
- `from` → JAM `From`
- `to` → JAM `To`
- `subject` → JAM `Subject`
- `date_utc` → JAM `DateWritten`
- `body` → JAM `Body`; prepend kludge line `\x01V3NETUUID: {msg_uuid}` for UUID recovery
- `attributes` → JAM `Attributes` (pass through)

## Vision/3 Config

```toml
[v3net]
enabled        = false
keystore_path  = "data/v3net.key"
dedup_db_path  = "data/v3net_dedup.sqlite"
registry_url   = "https://raw.githubusercontent.com/ViSiON-3/v3net-registry/main/registry.json"

[v3net.hub]
enabled      = false
listen_addr  = ":8765"
tls_cert     = ""
tls_key      = ""
data_dir     = "data/v3net_hub"
auto_approve = true

[[v3net.leaf]]
hub_url       = "https://bbs.felonynet.org"
network       = "felonynet"
board         = "FelonyNet General"
poll_interval = "5m"
```

## Implementation Phases

1. **Protocol types and validation** (`protocol/`) — DONE
2. **Keystore and auth** (`internal/keystore/`) — DONE
3. **Deduplication index** (`internal/dedup/`) — DONE
4. **Hub server** (`hub/`) — DONE
5. **Leaf client** (`leaf/`) — DONE
6. **Registry client** (`registry/`) — DONE
7. **Vision/3 integration** — DONE (config, startup wiring, message post hook, logon/logoff hooks)

42 test cases across 6 packages, all passing. Full project builds clean.

## What Was Built

### New packages (`internal/v3net/`)

| Package | Key Files |
|---|---|
| `protocol/` | message.go, event.go, network.go, message_test.go |
| `keystore/` | keystore.go, keystore_test.go |
| `dedup/` | dedup.go, dedup_test.go |
| `hub/` | hub.go, config.go, server.go, handlers.go, auth.go, subscribers.go, messages.go, events.go, hub_test.go |
| `leaf/` | leaf.go, config.go, jam.go, sender.go, poller.go, sse.go, leaf_test.go |
| `registry/` | registry.go, registry_test.go |

### Top-level service files (`internal/v3net/`)

| File | Purpose |
|---|---|
| `service.go` | Wires keystore, dedup, hub, leaves; exposes SendMessage/SendLogon/SendLogoff |
| `jam_adapter.go` | Bridges V3Net messages to JAM via MessageManager.AddMessage |
| `wire.go` | Builds wire messages from local posts, UUID v4 generation |

### Modified existing files

| File | Change |
|---|---|
| `internal/config/config.go` | Added `V3NetConfig`, `V3NetHubConfig`, `V3NetLeafConfig` structs to ServerConfig |
| `internal/message/manager.go` | Added `OnMessagePosted` callback field to MessageManager |
| `cmd/vision3/main.go` | V3Net service startup, leaf config, message post hook, logon/logoff hooks |
| `templates/configs/config.json` | Added `v3net` section with disabled defaults |
| `CLAUDE.md` | Added V3Net reference pointing to `internal/v3net/CLAUDE.md` |

### Dependencies added

- `modernc.org/sqlite` v1.46.2 (CGO-free SQLite) — bumped Go minimum to 1.25.0

## Completed Phases 8–12

### Phase 8 — Logon/Logoff Hub Endpoints (DONE)

- Added `POST /v3net/v1/{network}/presence` endpoint (`hub/handlers.go`, `hub/server.go`)
- `PresenceRequest` type in `protocol/network.go`
- `sendPresence` implemented in `leaf/sender.go`
- Tests: `TestPresence_LogonLogoff`, `TestPresence_InvalidType`

### Phase 9 — Sysop V3Net Status Menu (DONE)

- `internal/menu/v3net_status.go` with `runV3NetStatus` handler
- `V3NetStatusProvider` interface to avoid circular imports
- `V3NetStatus` field on `MenuExecutor`, wired from `main.go`
- Registered as `RUN:V3NETSTATUS`, key `3` in `ADMIN.CFG`

### Phase 10 — End-to-End Integration Test (DONE)

- `internal/v3net/integration_test.go` with 5 tests:
  - `TestIntegration_PostAndPoll` — full message round-trip
  - `TestIntegration_DedupPreventsDoubleWrite` — dedup verification
  - `TestIntegration_SSEReceivesEvents` — new_message SSE event
  - `TestIntegration_ChatEvent` — chat SSE event
  - `TestIntegration_PresenceEvents` — logon/logoff SSE events
- Exported `Hub.Mux()`, `Leaf.Poll()`, `Leaf.RunSSE()`, `Leaf.SetOnEvent()`

### Phase 11 — felonynet Documentation (DONE)

- `docs/felonynet.md` — joining as a leaf, hosting a hub, registry, troubleshooting

### Phase 12 — Hardening (DONE)

- **Chat rate limiting**: `hub/ratelimit.go` — per-node token bucket (1 msg/sec), returns 429
- **Request body limits**: `http.MaxBytesReader` in auth middleware (64KB) and subscribe handler (8KB)
- **Graceful shutdown**: `Hub.Close()` calls `server.Shutdown()` with 5s timeout before closing DB
- **Subscription approval**: Already correctly gated — `GetPubKey()` returns nil for non-active nodes
- Test: `TestChat_RateLimited`

## Testing Strategy

- Phases 1–3: pure unit tests, no external dependencies
- Phases 4–5: `httptest.NewServer` integration tests, no real network calls
- Phase 6: mock HTTP client
- Phase 7: integration with real BBS via config + hooks
- Phase 10: full end-to-end hub+leaf in same process

## Code Style

- No global mutable state (except registry cache via `sync.Map`)
- All exported I/O functions accept `context.Context` as first argument
- Use `slog` for structured logging (Debug for polls, Info for connections, Warn for retries, Error for failures)
- Wrap errors at every boundary with `fmt.Errorf("...: %w", err)`
- No `panic` outside `main` or test helpers
- Hub and leaf shut down cleanly on `context.Done()`
