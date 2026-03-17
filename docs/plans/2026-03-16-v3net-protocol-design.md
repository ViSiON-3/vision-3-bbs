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

`node_id` = lowercase hex of the first 8 bytes of `SHA-256(raw_ed25519_public_key)` (16 hex chars = 8 bytes). Generated once on first run, persisted at `v3net.keystore_path`.

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

### Core Endpoints (Phases 1–12)

| Method | Endpoint                       | Auth | Description                                                |
| ------ | ------------------------------ | ---- | ---------------------------------------------------------- |
| GET    | `/v3net/v1/networks`           | No   | List networks this hub serves                              |
| GET    | `/v3net/v1/{network}/info`     | No   | Full metadata for a single network                         |
| GET    | `/v3net/v1/{network}/messages` | Yes  | Fetch messages newer than cursor (`since`, `limit` params) |
| POST   | `/v3net/v1/{network}/messages` | Yes  | Submit a message (deduplicates by `msg_uuid`)              |
| GET    | `/v3net/v1/{network}/events`   | Yes  | SSE stream (ping, logon, logoff, new_message, chat)        |
| POST   | `/v3net/v1/{network}/chat`     | Yes  | Inter-BBS chat (1 msg/sec/node rate limit, not persisted)  |
| POST   | `/v3net/v1/{network}/presence` | Yes  | Logon/logoff presence events                               |
| POST   | `/v3net/v1/subscribe`          | No   | Register a new leaf node                                   |

### NAL Endpoints (Phase 13)

| Method | Endpoint                                           | Auth                 | Description                         |
| ------ | -------------------------------------------------- | -------------------- | ----------------------------------- |
| GET    | `/v3net/v1/{network}/nal`                          | No                   | Current signed NAL for this network |
| POST   | `/v3net/v1/{network}/nal`                          | Yes (coordinator)    | Publish a new signed NAL            |
| POST   | `/v3net/v1/{network}/areas/propose`                | Yes                  | Propose a new area                  |
| GET    | `/v3net/v1/{network}/areas/proposals`              | Yes (coordinator)    | List pending proposals              |
| POST   | `/v3net/v1/{network}/areas/proposals/{id}/approve` | Yes (coordinator)    | Approve a proposal                  |
| POST   | `/v3net/v1/{network}/areas/proposals/{id}/reject`  | Yes (coordinator)    | Reject a proposal                   |
| GET    | `/v3net/v1/{network}/areas/{tag}/access`           | Yes (manager)        | Current access config for an area   |
| POST   | `/v3net/v1/{network}/areas/{tag}/access/mode`      | Yes (manager)        | Set access mode                     |
| GET    | `/v3net/v1/{network}/areas/{tag}/access/requests`  | Yes (manager)        | List pending subscription requests  |
| POST   | `/v3net/v1/{network}/areas/{tag}/access/approve`   | Yes (manager)        | Approve subscription requests       |
| POST   | `/v3net/v1/{network}/areas/{tag}/access/deny`      | Yes (manager)        | Deny requests / add to deny list    |
| POST   | `/v3net/v1/{network}/areas/{tag}/access/remove`    | Yes (manager)        | Remove node from deny list          |
| POST   | `/v3net/v1/{network}/coordinator/transfer`         | Yes (coordinator)    | Initiate coordinator key transfer   |
| POST   | `/v3net/v1/{network}/coordinator/accept`           | Yes (incoming coord) | Accept coordinator key transfer     |

### Updated Subscribe Endpoint (Phase 13)

`POST /v3net/v1/subscribe` gains `area_tags`. Response includes per-area status:

```jsonc
// Request
{
  "network":    "felonynet",
  "node_id":    "a3f9e1b2c4d5e6f7",
  "pubkey_b64": "...",
  "bbs_name":   "The Underground BBS",
  "bbs_host":   "bbs.example.net",
  "area_tags":  ["fel.general", "fel.phreaking"]
}

// Response
{
  "ok": true,
  "areas": [
    { "tag": "fel.general",   "status": "active"  },
    { "tag": "fel.phreaking", "status": "pending" }
  ]
}
```

### SSE Event Types

Core (Phases 1–12):
- `ping` — every 30s keepalive
- `logon` / `logoff` — user presence events with handle, node, timestamp
- `new_message` — notification with network, msg_uuid, from, subject
- `chat` — inter-BBS chat with from, node, text, timestamp

NAL (Phase 13):
- `nal_updated` — signals leaf nodes to re-fetch the NAL within 60s ±10% jitter
- `area_proposed` — notifies coordinator of a new pending area proposal
- `area_access_requested` — notifies area manager of a pending subscription request
- `proposal_rejected` — notifies proposing node that their proposal was rejected
- `subscription_denied` — notifies a leaf node that area access was denied
- `coordinator_transfer_pending` — notifies incoming coordinator of a pending transfer

Leaf nodes reconnect with exponential backoff (base 5s, max 5min, jitter ±10%) on disconnect.

### Central Registry

Optional static JSON file for network discovery. Not required for operation — nodes can connect to a hub directly.

Canonical URL: `https://raw.githubusercontent.com/ViSiON-3/v3net-registry/main/registry.json`

Cached in memory for 1 hour. Startup does not fail if unreachable. The git registry is an **archival mirror only** — all area management happens in-BBS via the NAL system.

## Package Layout: `internal/v3net/`

```
internal/v3net/
  protocol/               # Shared wire types, constants, validation
  keystore/               # ed25519 keypair generation and persistence
  dedup/                  # SQLite-backed UUID deduplication index
  hub/                    # Hub HTTP server
  leaf/                   # Leaf client (polls hub, maintains SSE)
  nal/                    # NAL fetch, verify, sign, cache
  registry/               # Central registry fetch + parse
  events/                 # SSE event types and broadcaster
cmd/
  v3net-hub/              # standalone hub binary (optional later)
```

Reuses existing `internal/jam/` for JAM message base I/O.

## All Files

### Existing Files (Phases 1–12, complete)

| File                                       | Purpose                                                                      |
| ------------------------------------------ | ---------------------------------------------------------------------------- |
| `internal/v3net/protocol/message.go`       | `Message` struct with JSON tags, `Validate() error`                          |
| `internal/v3net/protocol/event.go`         | `Event` struct and typed payloads (Logon, Logoff, NewMessage, Chat, Ping)    |
| `internal/v3net/protocol/network.go`       | `NetworkInfo`, `NetworkPolicy`, `RegistryNetwork`, `PresenceRequest` structs |
| `internal/v3net/protocol/message_test.go`  | Table-driven validation tests                                                |
| `internal/v3net/keystore/keystore.go`      | `Keystore` — Load/create keypair, NodeID, Sign, PubKeyBase64                 |
| `internal/v3net/keystore/keystore_test.go` | Round-trip, node ID stability, signature verification                        |
| `internal/v3net/dedup/dedup.go`            | `Index` — SQLite-backed UUID dedup (Seen, MarkSeen, LastSeen)                |
| `internal/v3net/dedup/dedup_test.go`       | Dedup index tests                                                            |
| `internal/v3net/hub/hub.go`                | `Hub` struct and constructor                                                 |
| `internal/v3net/hub/config.go`             | `Config` and `NetworkConfig` structs                                         |
| `internal/v3net/hub/server.go`             | HTTP router setup, stdlib `net/http` only                                    |
| `internal/v3net/hub/handlers.go`           | One handler per endpoint                                                     |
| `internal/v3net/hub/auth.go`               | Auth middleware — validate node ID + signature; `http.MaxBytesReader`        |
| `internal/v3net/hub/subscribers.go`        | SQLite-persisted subscriber registry                                         |
| `internal/v3net/hub/messages.go`           | SQLite message storage, cursor-based pagination                              |
| `internal/v3net/hub/events.go`             | SSE broadcaster with ping goroutine                                          |
| `internal/v3net/hub/ratelimit.go`          | Per-node token bucket rate limiter for chat (1 msg/sec)                      |
| `internal/v3net/hub/hub_test.go`           | Integration tests via `httptest.NewServer`                                   |
| `internal/v3net/leaf/leaf.go`              | `Leaf` struct and constructor                                                |
| `internal/v3net/leaf/config.go`            | `Config` struct                                                              |
| `internal/v3net/leaf/jam.go`               | `JAMWriter` interface (implementation injected from BBS)                     |
| `internal/v3net/leaf/poller.go`            | Polling goroutine — fetch, dedup, write to JAM                               |
| `internal/v3net/leaf/sse.go`               | SSE connection with exponential backoff reconnect                            |
| `internal/v3net/leaf/sender.go`            | SendMessage, SendChat, SendLogon, SendLogoff, sendPresence                   |
| `internal/v3net/leaf/leaf_test.go`         | Mock hub integration tests                                                   |
| `internal/v3net/registry/registry.go`      | `Fetch()` with 1-hour cache via `sync.Map`                                   |
| `internal/v3net/registry/registry_test.go` | Mock HTTP client tests                                                       |
| `internal/v3net/service.go`                | Wires keystore, dedup, hub, leaves; exposes SendMessage/SendLogon/SendLogoff |
| `internal/v3net/jam_adapter.go`            | Bridges V3Net messages to JAM via MessageManager.AddMessage                  |
| `internal/v3net/wire.go`                   | Builds wire messages from local posts, UUID v4 generation                    |
| `internal/v3net/integration_test.go`       | 5 end-to-end tests: round-trip, dedup, SSE, chat, presence                   |
| `internal/menu/v3net_status.go`            | `runV3NetStatus` handler, `V3NetStatusProvider` interface                    |

### New Files (Phase 13)

| File                                        | Purpose                                                            |
| ------------------------------------------- | ------------------------------------------------------------------ |
| `internal/v3net/protocol/nal.go`            | `NAL`, `Area`, `AreaAccess`, `AreaPolicy` structs                  |
| `internal/v3net/protocol/nal_test.go`       | NAL struct validation tests                                        |
| `internal/v3net/nal/nal.go`                 | `Fetch`, `Verify`, `Sign`, `Cache`, `NodeAllowed`                  |
| `internal/v3net/nal/nal_test.go`            | Sign/verify round-trip, tamper detection, access mode logic        |
| `internal/v3net/hub/nal_handler.go`         | `GET/POST /nal` handlers                                           |
| `internal/v3net/hub/proposal_handler.go`    | Propose, list, approve, reject area proposal handlers              |
| `internal/v3net/hub/access_handler.go`      | Area access mode, requests, approve, deny, remove handlers         |
| `internal/v3net/hub/coordinator_handler.go` | Coordinator transfer and accept handlers                           |
| `internal/v3net/leaf/nal.go`                | Startup NAL fetch, `nal_updated` handling, manager notifications   |
| `internal/menu/v3net_areas.go`              | Area Subscriptions menu screen                                     |
| `internal/menu/v3net_propose.go`            | Propose New Area form screen                                       |
| `internal/menu/v3net_access_requests.go`    | Area Access Requests screen (manager-only)                         |
| `internal/menu/v3net_coordinator.go`        | Coordinator Panel screen (coordinator-only)                        |
| `docs/v3net-nal.md`                         | Sysop-facing NAL documentation (plain language, no git references) |
| `docs/felonynet.md`                         | Already exists — update to describe area subscription flow         |

### Modified Files (Phase 13)

| File                                 | Change                                                                                                                                                                                                 |
| ------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `internal/v3net/protocol/event.go`   | Add NAL SSE event payload types: `NALUpdatedPayload`, `AreaProposedPayload`, `AreaAccessRequestedPayload`, `ProposalRejectedPayload`, `SubscriptionDeniedPayload`, `CoordinatorTransferPendingPayload` |
| `internal/v3net/protocol/network.go` | Add `AreaProposal`, `AccessRequest`, `AreaAccessConfig` structs                                                                                                                                        |
| `internal/v3net/hub/server.go`       | Register NAL, proposal, access, coordinator route groups                                                                                                                                               |
| `internal/v3net/hub/handlers.go`     | Update subscribe handler: validate area tags against NAL, return per-area status                                                                                                                       |
| `internal/v3net/hub/subscribers.go`  | Add `area_subscriptions` table; message distribution filters on active area subscriptions                                                                                                              |
| `internal/v3net/hub/hub_test.go`     | Add NAL, proposal, and access control integration tests                                                                                                                                                |
| `internal/v3net/leaf/sse.go`         | Handle `nal_updated`, `area_access_requested`, `proposal_rejected`, `subscription_denied`, `coordinator_transfer_pending` events                                                                       |
| `internal/v3net/leaf/sender.go`      | Add `ProposeArea`, `RequestAreaAccess` methods                                                                                                                                                         |
| `internal/v3net/dedup/dedup.go`      | Add `nal_cache` table (persist last-verified NAL per network)                                                                                                                                          |
| `internal/menu/v3net_status.go`      | Surface pending access requests badge; link to new menu screens                                                                                                                                        |
| `internal/v3net/integration_test.go` | Add NAL round-trip, area subscription, access approval tests                                                                                                                                           |
| `docs/felonynet.md`                  | Update to describe area subscription flow and access modes                                                                                                                                             |

## Database Schemas

### Dedup Index (`internal/v3net/dedup/`)

```sql
CREATE TABLE IF NOT EXISTS seen_messages (
  msg_uuid         TEXT PRIMARY KEY,
  network          TEXT NOT NULL,
  local_jam_msgnum INTEGER,
  seen_at          DATETIME DEFAULT (datetime('now'))
);

-- Phase 13
CREATE TABLE IF NOT EXISTS nal_cache (
  network     TEXT PRIMARY KEY,
  nal_json    TEXT NOT NULL,
  verified_at DATETIME DEFAULT (datetime('now'))
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

-- Phase 13
CREATE TABLE IF NOT EXISTS area_subscriptions (
  node_id       TEXT NOT NULL,
  network       TEXT NOT NULL,
  area_tag      TEXT NOT NULL,
  status        TEXT DEFAULT 'pending',  -- 'active' | 'pending' | 'denied'
  subscribed_at DATETIME DEFAULT (datetime('now')),
  PRIMARY KEY (node_id, network, area_tag)
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

### Hub NAL & Proposals (`hub/nal_handler.go`, `hub/proposal_handler.go`) — Phase 13

```sql
CREATE TABLE IF NOT EXISTS network_nal (
  network      TEXT PRIMARY KEY,
  nal_json     TEXT NOT NULL,       -- full signed NAL JSON
  verified_at  DATETIME DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS area_proposals (
  id          TEXT PRIMARY KEY,     -- UUID
  network     TEXT NOT NULL,
  tag         TEXT NOT NULL,
  name        TEXT NOT NULL,
  description TEXT,
  language    TEXT DEFAULT 'en',
  access_mode TEXT DEFAULT 'open',
  allow_ansi  INTEGER DEFAULT 1,
  from_node   TEXT NOT NULL,
  status      TEXT DEFAULT 'pending',  -- 'pending' | 'approved' | 'rejected'
  reason      TEXT,
  proposed_at DATETIME DEFAULT (datetime('now')),
  resolved_at DATETIME
);
```

### Hub Area Access (`hub/access_handler.go`) — Phase 13

```sql
CREATE TABLE IF NOT EXISTS area_access_requests (
  id           TEXT PRIMARY KEY,    -- UUID
  network      TEXT NOT NULL,
  area_tag     TEXT NOT NULL,
  node_id      TEXT NOT NULL,
  bbs_name     TEXT,
  status       TEXT DEFAULT 'pending',  -- 'pending' | 'approved' | 'denied'
  requested_at DATETIME DEFAULT (datetime('now')),
  resolved_at  DATETIME,
  UNIQUE(network, area_tag, node_id)
);
```

## NAL Wire Format (Phase 13)

```jsonc
{
  "v3net_nal": "1.0",
  "network": "felonynet",
  "coordinator_node_id": "a3f9e1b2c4d5e6f7",
  "coordinator_pubkey_b64": "<base64 ed25519 public key>",
  "updated": "2026-03-16",
  "signature_b64": "<base64url ed25519 signature over canonical payload>",
  "areas": [
    {
      "tag": "fel.general",
      "name": "General",
      "description": "General discussion. No warrants required.",
      "language": "en",
      "moderated": false,
      "manager_node_id": "a3f9e1b2c4d5e6f7",
      "manager_pubkey_b64": "...",
      "access": {
        "mode": "open",             // "open" | "approval" | "closed"
        "allow_list": [],           // node_ids explicitly permitted
        "deny_list": []             // always enforced regardless of mode
      },
      "policy": {
        "max_body_bytes": 32768,
        "allow_ansi": true,
        "require_tearline": false
      }
    }
  ]
}
```

### Area Tag Format

`{network_prefix}.{area_name}` — validated with regexp: `^[a-z0-9]{1,8}\.[a-z0-9-]{1,24}$`

### Access Mode Semantics

| Mode       | Any node can subscribe? | How nodes get access                    |
| ---------- | ----------------------- | --------------------------------------- |
| `open`     | Yes                     | Subscribe → immediately active          |
| `approval` | No                      | Manager approves from sysop menu        |
| `closed`   | No                      | Manager explicitly adds to `allow_list` |

`deny_list` is always enforced regardless of mode. Hub returns HTTP 403 for denied nodes without revealing the reason.

### NAL Signing

Canonical payload = NAL JSON marshalled with `signature_b64` set to `""`, fields in a fixed alphabetical order (documented in `nal/nal.go`). Signature is `ed25519.Sign(privkey, canonicalPayload)`.

Any modification to any field after signing causes `Verify` to return an error. Coordinator transfer re-signs the entire NAL with the new coordinator keypair.

## Sysop Menu Screens (Phase 13)

All area management happens natively inside Vision/3's sysop menus. No external tools, browser, or git interaction required.

### V3Net: Area Subscriptions

```
[ V3Net: felonynet — Area Subscriptions ]

  TAG                 NAME              STATUS     LOCAL BOARD
  fel.general         General           ACTIVE     FelonyNet General
  fel.phreaking       Phreaking         PENDING    —
  fel.art             ANSI/ASCII Art    —          —
  fel.wanted          Wanted            —          —

  [Space] subscribe/unsubscribe  [E]dit local board name
  [P]ropose new area             [Q]uit
```

### V3Net: Propose New Area

```
[ V3Net: Propose New Area — felonynet ]

  Area Tag     : fel.________________
  Display Name : ________________________________
  Description  : ________________________________________________
  Language     : en
  Access Mode  : [Open] / Approval / Closed
  Allow ANSI   : [Y]

  [S]ubmit  [Q]uit
```

### V3Net: Area Access Requests (manager-only)

```
[ V3Net: Area Access Requests ]

  NETWORK     AREA TAG          BBS NAME                  REQUESTED
  felonynet   fel.phreaking     The Underground BBS       2d ago
  felonynet   fel.phreaking     Sector 7 BBS              5h ago

  [A]pprove  [D]eny  [B]lacklist  [Q]uit
```

`[B]` denies and adds the node to the area's `deny_list` permanently.

### V3Net: Coordinator Panel (coordinator-only)

```
[ V3Net: Coordinator Panel — felonynet ]

  [P]ending area proposals  (2)
  [M]anage area managers
  [T]ransfer coordinator role
  [Q]uit
```

Pending proposals sub-screen allows approve, reject, or edit-before-approve (adjust access mode, assign area manager, tweak policy).

## JAM Integration

Map `protocol.Message` fields to JAM headers:
- `from` → JAM `From`
- `to` → JAM `To`
- `subject` → JAM `Subject`
- `date_utc` → JAM `DateWritten`
- `body` → JAM `Body`; prepend kludge line `\x01V3NETUUID: {msg_uuid}` for UUID recovery if dedup index is lost
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

| Phase | Description                                                         | Status |
| ----- | ------------------------------------------------------------------- | ------ |
| 1     | Protocol types and validation (`protocol/`)                         | ✅ DONE |
| 2     | Keystore and auth (`keystore/`)                                     | ✅ DONE |
| 3     | Deduplication index (`dedup/`)                                      | ✅ DONE |
| 4     | Hub server (`hub/`)                                                 | ✅ DONE |
| 5     | Leaf client (`leaf/`)                                               | ✅ DONE |
| 6     | Registry client (`registry/`)                                       | ✅ DONE |
| 7     | Vision/3 integration — config, startup, hooks                       | ✅ DONE |
| 8     | Logon/logoff hub endpoints                                          | ✅ DONE |
| 9     | Sysop V3Net Status menu                                             | ✅ DONE |
| 10    | End-to-end integration tests                                        | ✅ DONE |
| 11    | felonynet documentation                                             | ✅ DONE |
| 12    | Hardening — rate limiting, body limits, graceful shutdown           | ✅ DONE |
| 13    | Network Area List (NAL) — area management, access control, sysop UI | 🔲 TODO |

42 test cases across 6 packages passing after Phase 12. All tests must continue to pass throughout Phase 13.

## Phase 13 — Network Area List (NAL)

### Overview

The NAL is the signed canonical list of areas a network carries. It is the **source of truth for area definitions** — not any hub, not any git repository. The coordinator hub owns and signs it; all other hubs distribute it as verified mirrors.

All area management — proposing areas, approving proposals, subscribing to areas, managing per-area access — happens natively inside Vision/3's sysop menus over the V3Net protocol. **No external tools, browser, or git interaction is required for sysops.** Git (`v3net-registry`) is an optional archival export target only.

### Before Starting Phase 13

Read the existing codebase to understand:
1. How `hub/server.go` registers routes — follow the same pattern for new route groups.
2. How `hub/handlers.go` accesses the SQLite DB — use the same `*sql.DB` reference.
3. How `hub/events.go` broadcasts SSE events — use `Broadcaster.Publish` for all new events.
4. How `leaf/sse.go` dispatches events — add new event type cases to the existing switch.
5. How `internal/menu/v3net_status.go` is structured — follow the same pattern for new screens.

Do not start writing code until you have confirmed these patterns.

### Step 1 — Protocol Types (`protocol/nal.go`)

```go
type NAL struct {
    V3NetNAL       string `json:"v3net_nal"`
    Network        string `json:"network"`
    CoordNodeID    string `json:"coordinator_node_id"`
    CoordPubKeyB64 string `json:"coordinator_pubkey_b64"`
    Updated        string `json:"updated"`        // YYYY-MM-DD UTC
    SignatureB64   string `json:"signature_b64"`  // ed25519 over canonical payload
    Areas          []Area `json:"areas"`
}

type Area struct {
    Tag              string     `json:"tag"`
    Name             string     `json:"name"`
    Description      string     `json:"description"`
    Language         string     `json:"language"`
    Moderated        bool       `json:"moderated"`
    ManagerNodeID    string     `json:"manager_node_id"`
    ManagerPubKeyB64 string     `json:"manager_pubkey_b64"`
    Access           AreaAccess `json:"access"`
    Policy           AreaPolicy `json:"policy"`
}

type AreaAccess struct {
    Mode      string   `json:"mode"`                 // "open" | "approval" | "closed"
    AllowList []string `json:"allow_list,omitempty"`
    DenyList  []string `json:"deny_list,omitempty"`
}

type AreaPolicy struct {
    MaxBodyBytes    int  `json:"max_body_bytes"`
    AllowANSI       bool `json:"allow_ansi"`
    RequireTearline bool `json:"require_tearline"`
}
```

Also add to `protocol/network.go`:
```go
type AreaProposal struct {
    ID          string `json:"id"`
    Tag         string `json:"tag"`
    Name        string `json:"name"`
    Description string `json:"description"`
    Language    string `json:"language"`
    AccessMode  string `json:"access_mode"`
    AllowANSI   bool   `json:"allow_ansi"`
    FromNode    string `json:"from_node"`
    FromBBS     string `json:"from_bbs"`
    ProposedAt  string `json:"proposed_at"`
    Status      string `json:"status"`
}

type AccessRequest struct {
    NodeID      string `json:"node_id"`
    BBSName     string `json:"bbs_name"`
    BBSHost     string `json:"bbs_host"`
    RequestedAt string `json:"requested_at"`
}

type AreaAccessConfig struct {
    Mode      string   `json:"mode"`
    AllowList []string `json:"allow_list"`
    DenyList  []string `json:"deny_list"`
}
```

Also add to `protocol/event.go`:
```go
type NALUpdatedPayload          struct { Network string `json:"network"`; Updated string `json:"updated"`; AreaCount int `json:"area_count"` }
type AreaProposedPayload        struct { Network string `json:"network"`; Tag string `json:"tag"`; FromNode string `json:"from_node"`; ProposalID string `json:"proposal_id"` }
type AreaAccessRequestedPayload struct { Network string `json:"network"`; Tag string `json:"tag"`; NodeID string `json:"node_id"`; BBSName string `json:"bbs_name"` }
type ProposalRejectedPayload    struct { Network string `json:"network"`; Tag string `json:"tag"`; Reason string `json:"reason"` }
type SubscriptionDeniedPayload  struct { Network string `json:"network"`; Tag string `json:"tag"` }
type CoordTransferPendingPayload struct { Network string `json:"network"`; NewNodeID string `json:"new_node_id"` }
```

### Step 2 — NAL Package (`nal/nal.go`)

- `func Fetch(ctx context.Context, url string) (*protocol.NAL, error)` — HTTP GET, no verification.
- `func Verify(nal *protocol.NAL) error` — reconstruct canonical payload (NAL JSON with `SignatureB64` = `""`, fixed field order documented in a comment), verify with `crypto/ed25519`.
- `func Sign(nal *protocol.NAL, ks *keystore.Keystore) error` — set `CoordPubKeyB64`, `Updated`, compute `SignatureB64`.
- `type Cache struct` with `NewCache(ttl)`, `FetchAndVerify(ctx, url, network)`, `Area(network, tag)`, `NodeAllowed(network, tag, nodeID)`.
  - `NodeAllowed`: check `DenyList` first (always enforced), then `Mode` + `AllowList`.
  - On fetch failure with warm cache: return stale + log warning.
- Persist last-verified NAL in dedup SQLite `nal_cache` table.
- Sentinel errors: `ErrNetworkNotCached`, `ErrAreaNotFound`.

Tests (`nal/nal_test.go`):
- Sign → Verify round-trip succeeds.
- Verify fails on any field modification after signing.
- Verify fails with wrong keypair.
- `NodeAllowed` — open area: allowed unless on deny list.
- `NodeAllowed` — approval area: denied unless on allow list.
- `NodeAllowed` — deny list overrides allow list.
- Cache returns stale on fetch failure when warm.

### Step 3 — Hub NAL Handler (`hub/nal_handler.go`)

- `GET /v3net/v1/{network}/nal` — public, serve stored NAL from `network_nal` table.
- `POST /v3net/v1/{network}/nal` — coordinator-only (verify `X-V3Net-Node-ID` matches `coordinator_node_id` in stored NAL). Verify signature, persist, fan out `nal_updated` SSE event.

### Step 4 — Hub Proposal Handler (`hub/proposal_handler.go`)

- `POST /v3net/v1/{network}/areas/propose` — any authenticated subscriber. Validate tag format, store in `area_proposals`, fan out `area_proposed` SSE to coordinator.
- `GET /v3net/v1/{network}/areas/proposals` — coordinator-only. Return pending proposals.
- `POST /v3net/v1/{network}/areas/proposals/{id}/approve` — coordinator-only. Add area to NAL, re-sign, publish via `POST /nal` internally, update proposal status.
- `POST /v3net/v1/{network}/areas/proposals/{id}/reject` — coordinator-only. Update status, fan out `proposal_rejected` SSE to proposing node.

### Step 5 — Hub Access Handler (`hub/access_handler.go`)

Manager-only endpoints (verify `X-V3Net-Node-ID` matches `manager_node_id` for the area in the current NAL):

- `GET /areas/{tag}/access` — return current `AreaAccessConfig` from NAL.
- `POST /areas/{tag}/access/mode` — update mode in NAL, re-sign, publish.
- `GET /areas/{tag}/access/requests` — return pending rows from `area_access_requests`.
- `POST /areas/{tag}/access/approve` — add node_ids to `allow_list` in NAL area, set `area_access_requests` status to approved, re-sign NAL, activate `area_subscriptions` rows, publish NAL.
- `POST /areas/{tag}/access/deny` — add to `deny_list`, remove from `allow_list`, set request status to denied, deactivate subscription, fan out `subscription_denied` SSE, publish NAL.
- `POST /areas/{tag}/access/remove` — remove from `deny_list`, publish NAL.

### Step 6 — Hub Coordinator Handler (`hub/coordinator_handler.go`)

- `POST /coordinator/transfer` — coordinator-only. Validate `new_node_id` and `new_pubkey_b64`. Generate signed transfer token. Fan out `coordinator_transfer_pending` SSE to new coordinator node. Store pending transfer in hub DB.
- `POST /coordinator/accept` — new coordinator redeems token. Hub verifies token signature against old coordinator key. Re-signs NAL with new coordinator pubkey. Retires old key.

### Step 7 — Updated Subscribe Handler

Update `hub/handlers.go` subscribe handler:
1. Validate each tag in `area_tags` exists in current NAL. Unknown tags: HTTP 422 with list of valid tags.
2. Check `DenyList` per area. Denied: HTTP 403, no reason given.
3. Per access mode: `open` → insert `area_subscriptions` as `active`; `approval` → insert as `pending`, create `area_access_requests` row, fan out `area_access_requested` SSE to area manager; `closed` → HTTP 403 unless already on `AllowList`.
4. Message distribution in `hub/messages.go`: filter recipients by `area_subscriptions WHERE status = 'active' AND area_tag = ?`.

### Step 8 — Leaf NAL (`leaf/nal.go`)

- On startup: `FetchAndVerify` for each configured network. Log warning and use cached if fetch fails.
- On `nal_updated` SSE: schedule re-fetch within `60s * (1 ± 0.1 jitter)`.
- On `area_access_requested` SSE (this node is manager): queue notification for sysop menu.
- On `proposal_rejected` SSE: queue notification for proposing sysop.
- On `subscription_denied` SSE: log `Warn`, surface in V3Net status menu.
- On `coordinator_transfer_pending` SSE: queue notification for sysop menu.
- Expose `func (l *Leaf) Areas(network string) ([]protocol.Area, error)`.
- Expose `func (l *Leaf) PendingAccessRequests(network, tag string) ([]AccessRequest, error)`.

### Step 9 — Sysop Menu Screens

Follow the pattern established in `internal/menu/v3net_status.go` for all screens.

- `internal/menu/v3net_areas.go` — Area Subscriptions screen. Space toggles subscription (calls subscribe/unsubscribe endpoints). `E` prompts for local JAM board name; offer to create if not found. `P` opens proposal form.
- `internal/menu/v3net_propose.go` — Propose New Area form. Calls `leaf.ProposeArea`. Shows hub response.
- `internal/menu/v3net_access_requests.go` — Area Access Requests screen. Visible only if this node manages at least one area. `A`/`D`/`B` call access approve/deny endpoints.
- `internal/menu/v3net_coordinator.go` — Coordinator Panel. Visible only if this node is coordinator for at least one network. Shows pending proposal count. Sub-screens for proposal list, area manager assignment, coordinator transfer.

Register all new screens in the V3Net sysop menu. Gate visibility by role (manager, coordinator) checked against the cached NAL.

### Step 10 — Documentation

- `docs/v3net-nal.md` — sysop-facing, plain language:
  - What the NAL is and why it exists.
  - How to subscribe to areas from the V3Net sysop menu.
  - How to propose a new area.
  - The three access modes and when to use each.
  - How area managers approve/deny subscriptions.
  - How coordinator handoff works.
  - For developers only: the optional git archival export.
- Update `docs/felonynet.md` to describe the area subscription flow.

### Phase 13 Tests

In addition to unit tests for each new package/handler:

Add to `internal/v3net/integration_test.go`:
- `TestNAL_FetchAndVerify` — hub serves NAL, leaf fetches and verifies.
- `TestNAL_TamperDetected` — modified NAL fails verification.
- `TestAreaProposal_RoundTrip` — propose, coordinator approves, NAL updated, leaf sees new area.
- `TestAreaProposal_Rejected` — propose, reject, `proposal_rejected` SSE fires.
- `TestAreaAccess_ApprovalFlow` — subscribe to approval-mode area, manager approves, messages flow.
- `TestAreaAccess_DenyList` — denied node cannot subscribe or receive messages.
- `TestAreaAccess_ModeChange` — open → approval mode change propagates via NAL update.
- `TestCoordinator_Transfer` — transfer token generated, accepted, new NAL signed with new key.

## Code Style

- No global mutable state (except registry cache via `sync.Map`)
- All exported I/O functions accept `context.Context` as first argument
- Use `slog` for structured logging (Debug for polls, Info for connections, Warn for retries, Error for failures)
- Wrap errors at every boundary with `fmt.Errorf("...: %w", err)`
- No `panic` outside `main` or test helpers
- Hub and leaf shut down cleanly on `context.Done()`
- Files stay under 300 lines — split handlers into separate files if approaching limit

## Dependencies

- `modernc.org/sqlite` v1.46.2 (CGO-free SQLite) — already present
- No new dependencies for Phase 13. All crypto via `crypto/ed25519` (stdlib).
