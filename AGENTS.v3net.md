# V3Net Implementation — Claude Code Agent Instructions

## Project Context

You are implementing **V3Net**, a native message networking protocol for
[ViSiON/3 BBS](https://github.com/ViSiON-3/vision-3-bbs/), a Go-based bulletin
board system.

**V3Net is an additive feature. Vision/3 will continue to support FTN-based
echomail networks (FidoNet, fsxNet, etc.) alongside V3Net. Do not modify,
remove, or deprecate any existing FTN/echomail code. V3Net is a parallel,
independent networking stack.**

V3Net is a REST+SSE protocol that is firewall-friendly, trivial to set up, and
supports real-time events (logon/logoff, inter-BBS chat) that FTN echomail
cannot provide. It is designed for sysops who want a native Vision/3 network
without the overhead of mailer software and nodelist management — not as a
replacement for those who are already on FidoNet or other FTN networks.

The first named network running on V3Net is **felonynet**.

The codebase is Go. Use idiomatic Go: interfaces over concrete types, `context`
everywhere async, `errors.Is`/`errors.As`, table-driven tests.

---

## Repository Layout

Work inside a new top-level package:

```
v3net/
  cmd/
    v3net-hub/          # standalone hub binary (optional later)
  internal/
    jam/                # JAM message base read/write (reuse existing if present)
    dedup/              # SQLite-backed UUID deduplication index
    keystore/           # ed25519 keypair generation and persistence
  hub/                  # Hub HTTP server (serves leaf nodes)
  leaf/                 # Leaf client (polls hub, maintains SSE connection)
  protocol/             # Shared wire types, constants, validation
  registry/             # Central registry fetch + parse
  events/               # SSE event types and broadcaster
```

If the repo already has a `jam/` or `msgbase/` package, reuse it rather than
creating a parallel one. Confirm by reading the directory tree before writing
any JAM-related code.

---

## Protocol Specification

### Wire Format — Message Object

Every networked message is a JSON object with these fields. All fields are
**required** unless marked optional.

```jsonc
{
  // Protocol version. Currently always "1.0".
  "v3net": "1.0",

  // Short name of the network this message belongs to, e.g. "felonynet".
  "network": "felonynet",

  // Stable UUID v4 assigned at origin. Used for deduplication everywhere.
  "msg_uuid": "550e8400-e29b-41d4-a716-446655440000",

  // UUID of the root message in this thread.
  // Equal to msg_uuid for the first post in a thread.
  "thread_uuid": "550e8400-e29b-41d4-a716-446655440000",

  // UUID of the direct parent message, or null for a new thread.
  "parent_uuid": null,

  // Hostname or stable identifier of the originating BBS node.
  "origin_node": "bbs.example.net",

  // Human-readable name of the message board/area on the origin node.
  "origin_board": "General",

  // Author handle (not necessarily a registered user on every node).
  "from": "Darkstar",

  // Recipient handle, or "All" for public messages.
  "to": "All",

  "subject": "Hello from the underground",

  // RFC 3339 UTC timestamp.
  "date_utc": "2026-03-16T04:20:00Z",

  // Message body. UTF-8. CRLF or LF both accepted; normalize to LF on ingest.
  "body": "...",

  // Optional tearline displayed at end of message.
  "tearline": "Vision/3 BBS * Somewhere, Earth",  // optional

  // JAM-compatible attribute bitmask. 0 for normal public messages.
  "attributes": 0,

  // Open map for forward-compatible extension fields. Ignore unknown keys.
  "kludges": {}
}
```

**Validation rules** (enforce in `protocol/` package):

- `v3net` must be `"1.0"` (reject others with HTTP 422)
- `msg_uuid`, `thread_uuid` must parse as valid UUID v4
- `parent_uuid` is null or a valid UUID v4
- `network` must be `[a-z0-9_-]{1,32}`
- `date_utc` must parse with `time.Parse(time.RFC3339, ...)`
- `from` and `to`: 1–64 printable ASCII characters
- `subject`: 1–128 characters
- `body`: non-empty, ≤ 32,768 bytes (accept body but truncate with a kludge
  `"v3net_truncated": true` if over limit)

---

### Hub REST API

Base path: `/v3net/v1`

All responses are `application/json`. All requests must include the auth
header described in the Authentication section below.

#### `GET /v3net/v1/networks`

Returns an array of network descriptors this hub serves.

```jsonc
[
  {
    "name": "felonynet",
    "description": "General discussion. No warrants required.",
    "hub_node_id": "a3f9e1b2c4d5e6f7",  // first 16 hex chars of SHA-256(pubkey)
    "message_count": 4207,
    "created_at": "2026-01-01T00:00:00Z"
  }
]
```

No auth required for this endpoint — it is public discovery.

#### `GET /v3net/v1/{network}/info`

Returns full metadata for a single network.

```jsonc
{
  "name": "felonynet",
  "description": "...",
  "hub_node_id": "a3f9e1b2c4d5e6f7",
  "hub_pubkey_b64": "<base64-encoded ed25519 public key>",
  "leaf_count": 12,
  "message_count": 4207,
  "policy": {
    "max_body_bytes": 32768,
    "poll_interval_min": 60,
    "require_tearline": false
  }
}
```

No auth required.

#### `GET /v3net/v1/{network}/messages`

Fetch messages newer than a cursor.

Query params:
- `since` — `msg_uuid` of the last received message. If omitted or `"0"`,
  return from the beginning (hub may cap the response).
- `limit` — integer 1–500, default 100.

Returns a JSON array of message objects (see Wire Format), ordered oldest
first. An empty array means no new messages.

Response header `X-V3Net-Has-More: true` indicates additional pages are
available; repeat the request with `since` set to the last UUID received.

Auth required.

#### `POST /v3net/v1/{network}/messages`

Submit a new message from a leaf node to the hub.

Request body: a single message object (Wire Format).

The hub:
1. Validates the message.
2. Checks the submitting leaf node is subscribed to this network.
3. Deduplicates by `msg_uuid` — silently accepts (HTTP 200) if already seen.
4. Stores the message and enqueues it for distribution to all other subscribers.
5. Fans out a `new_message` SSE event on the network's event stream.

Response:
```jsonc
{ "ok": true, "msg_uuid": "550e8400-..." }
```

Auth required.

#### `GET /v3net/v1/{network}/events`

Server-Sent Events stream. Content-Type: `text/event-stream`.

The connection is long-lived. The hub sends a `ping` event every 30 seconds to
keep the connection alive through NAT/proxies. Leaf nodes must reconnect with
exponential backoff (base 5s, max 5min) on disconnect.

Event types:

```
event: ping
data: {}

event: logon
data: {"handle":"Darkstar","node":"bbs.example.net","timestamp":"2026-03-16T04:20:00Z"}

event: logoff
data: {"handle":"Darkstar","node":"bbs.example.net","timestamp":"2026-03-16T04:21:00Z"}

event: new_message
data: {"network":"felonynet","msg_uuid":"550e8400-...","from":"Darkstar","subject":"Hello"}

event: chat
data: {"from":"Darkstar","node":"bbs.example.net","text":"anyone alive out there?","timestamp":"..."}
```

Auth required. Hub closes the stream with an error event if the leaf's auth
is revoked while connected.

#### `POST /v3net/v1/{network}/chat`

Send an inter-BBS chat message. The hub fans it out to all SSE subscribers as
a `chat` event. Does not persist to JAM storage.

```jsonc
{ "from": "Darkstar", "text": "anyone alive out there?" }
```

Auth required. Rate-limited to 1 message per second per node.

#### `POST /v3net/v1/subscribe`

Register a new leaf node. Called once during initial setup.

```jsonc
{
  "network": "felonynet",
  "node_id": "a3f9e1b2c4d5e6f7",
  "pubkey_b64": "<base64-encoded ed25519 public key>",
  "bbs_name": "The Underground BBS",
  "bbs_host": "bbs.example.net"
}
```

The hub sysop may need to approve subscriptions depending on hub config
(`auto_approve: true/false`). Response:

```jsonc
{ "ok": true, "status": "active" }        // auto-approved
{ "ok": true, "status": "pending" }       // awaiting sysop approval
```

No auth required for this endpoint (it is the bootstrap step).

---

### Authentication

Every authenticated request must include two HTTP headers:

```
X-V3Net-Node-ID: a3f9e1b2c4d5e6f7
X-V3Net-Signature: <base64url(ed25519_sign(privkey, canonical_string))>
```

The **canonical string** to sign is:

```
{METHOD}\n{PATH}\n{DATE_UTC}\n{SHA256_HEX(body)}
```

Where:
- `METHOD` is uppercase HTTP verb (`GET`, `POST`, etc.)
- `PATH` is the full request path including query string
- `DATE_UTC` is the value of the `Date` header (RFC 1123), which must also be
  present in the request
- `SHA256_HEX(body)` is the lowercase hex SHA-256 of the raw request body, or
  the SHA-256 of an empty string for requests with no body
- `\n` is a literal newline byte

The hub validates:
1. `X-V3Net-Node-ID` exists in its subscriber list for this network
2. The `Date` header is within ±5 minutes of hub time (replay prevention)
3. The signature verifies against the stored public key for that node ID

Implement signing in `internal/keystore/` and verification in `hub/`.

---

### Node Identity

`node_id` = lowercase hex of the first 16 bytes of `SHA-256(raw_ed25519_public_key)`.

Example: `a3f9e1b2c4d5e6f7` (16 hex chars = 8 bytes of the hash).

On first run, the leaf generates a keypair and persists it at the path
configured by `v3net.keystore_path` in Vision/3's config. The node ID is
derived from the keypair and never changes. If the keystore file is lost, a
new identity must be registered with all hubs.

---

### Central Registry

The registry is a static JSON file fetchable by any node for network discovery.
It is not required for operation — nodes can connect to a hub directly if they
know its URL.

Canonical registry URL (hardcoded default, overridable in config):
```
https://raw.githubusercontent.com/ViSiON-3/v3net-registry/main/registry.json
```

Format:

```jsonc
{
  "v3net_registry": "1.0",
  "updated": "2026-03-16",
  "networks": [
    {
      "name": "felonynet",
      "description": "General discussion. No warrants required.",
      "hub_url": "https://bbs.felonynet.org",
      "hub_node_id": "a3f9e1b2c4d5e6f7",
      "tags": ["general", "tech", "bbs"]
    }
  ]
}
```

Implement `registry.Fetch(ctx, url)` returning `[]registry.Network`. Cache the
result in memory for 1 hour; do not fail startup if the registry is unreachable.

---

## Implementation Phases

Work through these phases in order. Do not start a later phase until the
current one has passing tests.

---

### Phase 1 — Protocol Types and Validation (`protocol/`)

**Files to create:**

- `protocol/message.go` — `Message` struct with JSON tags matching the wire
  format, plus `Validate() error`.
- `protocol/event.go` — `Event` struct and typed event payload structs
  (`LogonPayload`, `LogoffPayload`, `NewMessagePayload`, `ChatPayload`,
  `PingPayload`).
- `protocol/network.go` — `NetworkInfo`, `NetworkPolicy`, `RegistryNetwork`
  structs.
- `protocol/message_test.go` — Table-driven tests for `Validate()` covering
  valid messages and each class of invalid field.

**Do not** add any HTTP or database code in this package.

---

### Phase 2 — Keystore and Auth (`internal/keystore/`)

**Files to create:**

- `internal/keystore/keystore.go`
  - `type Keystore struct` wrapping an ed25519 keypair
  - `Load(path string) (*Keystore, error)` — loads from disk; creates and
    saves a new keypair if the file does not exist
  - `NodeID() string` — returns the 16-char hex node ID
  - `Sign(method, path, dateUTC, bodySHA256 string) (string, error)` —
    returns base64url signature over the canonical string
  - `PubKeyBase64() string` — base64 standard encoding of the raw public key
- `internal/keystore/keystore_test.go` — test round-trip load/save, node ID
  stability, and that `Sign` output verifies with `ed25519.Verify`.

Keypair storage format: a JSON file containing:
```jsonc
{ "privkey_b64": "...", "pubkey_b64": "..." }
```
Both fields are base64 standard encoding of the raw 64-byte private key and
32-byte public key respectively. Use `os.WriteFile` with mode `0600`.

### Mnemonic Recovery (added post-Phase 2)

The keystore supports BIP39 mnemonic encoding for key recovery. The 32-byte
ed25519 seed is encoded as 24 words from the standard BIP39 English word list
(embedded in `wordlist.go`, no external dependency).

- `Mnemonic() (string, error)` — encode current key as 24 words
- `FromMnemonic(phrase string) (*Keystore, error)` — decode and reconstruct
- `RecoverToFile(phrase, path string) (*Keystore, error)` — decode and save

`Load()` returns `(ks *Keystore, created bool, err error)` to signal new key
generation. No protocol changes — recovery restores the identical keypair.

---

### Phase 3 — Deduplication Index (`internal/dedup/`)

**Files to create:**

- `internal/dedup/dedup.go`
  - Opens (or creates) a SQLite database at the given path.
  - Schema:

    ```sql
    CREATE TABLE IF NOT EXISTS seen_messages (
      msg_uuid   TEXT PRIMARY KEY,
      network    TEXT NOT NULL,
      local_jam_msgnum INTEGER,  -- null until written to JAM
      seen_at    DATETIME DEFAULT (datetime('now'))
    );
    ```

  - `type Index struct`
  - `func Open(path string) (*Index, error)`
  - `func (ix *Index) Seen(msgUUID string) (bool, error)`
  - `func (ix *Index) MarkSeen(msgUUID, network string, localMsgNum *int64) error`
  - `func (ix *Index) LastSeen(network string) (msgUUID string, err error)` —
    returns the `msg_uuid` of the most recently marked message for a network,
    or `""` if none.

- `internal/dedup/dedup_test.go`

Use the `modernc.org/sqlite` driver (no CGO required). If the project already
uses a different SQLite driver, use that instead.

---

### Phase 4 — Hub Server (`hub/`)

**Files to create:**

- `hub/hub.go` — `type Hub struct` and `func New(cfg Config) *Hub`
- `hub/config.go` — `type Config struct` with fields:
  ```go
  type Config struct {
      ListenAddr   string          // e.g. ":8765"
      TLSCertFile  string          // optional; plain HTTP if empty
      TLSKeyFile   string
      DataDir      string          // where hub stores its SQLite DB and messages
      Keystore     *keystore.Keystore
      AutoApprove  bool
      Networks     []NetworkConfig
  }

  type NetworkConfig struct {
      Name        string
      Description string
  }
  ```
- `hub/server.go` — sets up the `net/http` router and registers all handlers.
  Use only stdlib `net/http`; no external router dependency unless the project
  already uses one (check `go.mod` first).
- `hub/handlers.go` — one handler function per endpoint.
- `hub/auth.go` — middleware that validates `X-V3Net-Node-ID` and
  `X-V3Net-Signature` on protected routes.
- `hub/subscribers.go` — in-memory + SQLite-persisted subscriber registry.
  Schema:

  ```sql
  CREATE TABLE IF NOT EXISTS subscribers (
    node_id    TEXT NOT NULL,
    network    TEXT NOT NULL,
    pubkey_b64 TEXT NOT NULL,
    bbs_name   TEXT,
    bbs_host   TEXT,
    status     TEXT NOT NULL DEFAULT 'pending',  -- 'active' | 'pending' | 'banned'
    created_at DATETIME DEFAULT (datetime('now')),
    PRIMARY KEY (node_id, network)
  );
  ```

- `hub/messages.go` — message storage. Schema:

  ```sql
  CREATE TABLE IF NOT EXISTS messages (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    msg_uuid   TEXT UNIQUE NOT NULL,
    network    TEXT NOT NULL,
    data       TEXT NOT NULL,  -- full JSON wire object
    received_at DATETIME DEFAULT (datetime('now'))
  );
  CREATE INDEX IF NOT EXISTS idx_messages_network ON messages(network, id);
  ```

  The `since` cursor on `GET /messages` translates to:
  ```sql
  SELECT data FROM messages
  WHERE network = ? AND id > (SELECT id FROM messages WHERE msg_uuid = ?)
  ORDER BY id ASC
  LIMIT ?;
  ```
  If `since` is `""` or `"0"`, use `id > 0`.

- `hub/events.go` — SSE broadcaster.
  - `type Broadcaster struct` with a map of `network → []chan Event`
  - `Subscribe(network string) (<-chan Event, cancel func())`
  - `Publish(network string, ev Event)`
  - The ping goroutine fires `Event{Type: "ping"}` every 30s on all channels.
  - SSE handler writes:
    ```
    event: {ev.Type}\ndata: {ev.JSON}\n\n
    ```
    and calls `http.Flusher.Flush()` after each event.

- `hub/hub_test.go` — integration tests using `httptest.NewServer`. Cover:
  - Unauthenticated request to protected endpoint returns 401.
  - POST a valid message, then GET and receive it.
  - Duplicate POST returns 200 with no duplicate stored.
  - SSE stream receives `new_message` event after a message is posted.

---

### Phase 5 — Leaf Client (`leaf/`)

**Files to create:**

- `leaf/leaf.go` — `type Leaf struct` and `func New(cfg Config) *Leaf`
- `leaf/config.go`:
  ```go
  type Config struct {
      HubURL       string
      Network      string
      PollInterval time.Duration  // default 5 minutes; hub policy minimum enforced
      Keystore     *keystore.Keystore
      DedupIndex   *dedup.Index
      JAMWriter    JAMWriter       // interface — see below
      OnEvent      func(Event)     // called for each SSE event; may be nil
  }
  ```
- `leaf/jam.go` — `type JAMWriter interface`:
  ```go
  type JAMWriter interface {
      WriteMessage(msg protocol.Message) (localMsgNum int64, err error)
  }
  ```
  The concrete implementation lives in the main BBS package and is injected
  here. Do not implement JAM I/O in this package.
- `leaf/poller.go` — polling goroutine:
  1. Call `dedup.LastSeen(network)` to get the cursor.
  2. GET `{hub}/v3net/v1/{network}/messages?since={cursor}&limit=100`.
  3. For each message in the response, call `dedup.Seen()`. Skip if true.
  4. Call `JAMWriter.WriteMessage(msg)`.
  5. Call `dedup.MarkSeen(msg.UUID, network, &localNum)`.
  6. If `X-V3Net-Has-More: true`, immediately fetch the next page before
     sleeping.
  7. Sleep `PollInterval` (or the hub's minimum, whichever is larger).
- `leaf/sse.go` — SSE connection goroutine:
  1. GET `{hub}/v3net/v1/{network}/events` with streaming body.
  2. Use `bufio.Scanner` with `ScanLines` to read the stream.
  3. Accumulate `event:` and `data:` lines; dispatch complete events to
     `OnEvent`.
  4. On any error, reconnect with exponential backoff (base 5s, max 5min,
     jitter ±10%).
- `leaf/sender.go`:
  - `func (l *Leaf) SendMessage(msg protocol.Message) error` — signs and POSTs
    to the hub.
  - `func (l *Leaf) SendChat(text, handle string) error` — signs and POSTs to
    `POST /chat`.
  - `func (l *Leaf) SendLogon(handle string) error` — POSTs logon event.
  - `func (l *Leaf) SendLogoff(handle string) error` — POSTs logoff event.
- `leaf/leaf_test.go` — use `httptest.NewServer` to mock a hub. Cover:
  - Happy-path poll receives messages and calls JAMWriter.
  - Duplicate messages are skipped (JAMWriter not called for seen UUIDs).
  - Reconnect logic on SSE disconnect.
  - `SendMessage` signs correctly and hub mock verifies the signature.

---

### Phase 6 — Registry Client (`registry/`)

- `registry/registry.go`:
  - `type Network struct` (mirrors registry JSON)
  - `func Fetch(ctx context.Context, url string) ([]Network, error)`
  - Cache result in a package-level `sync.Map` with a 1-hour TTL.
  - Return cached data (not an error) if the fetch fails but cached data exists.

---

### Phase 6.5 — Network Area List (NAL)

The NAL is the signed canonical list of areas (message boards) that a network
carries. It is the **source of truth for area definitions** — not any hub, not
any git repository. The coordinator hub owns and signs it; all other hubs
distribute it as verified mirrors.

**All area management — proposing areas, approving proposals, subscribing to
areas, managing per-area access — happens natively inside Vision/3's sysop
menus over the V3Net protocol. No external tools, no browser, no git
interaction is required for sysops.** Git (`v3net-registry`) is an optional
archival export target only.

---

#### NAL Data Types (`protocol/nal.go`)

```go
type NAL struct {
    V3NetNAL       string `json:"v3net_nal"`             // always "1.0"
    Network        string `json:"network"`
    CoordNodeID    string `json:"coordinator_node_id"`
    CoordPubKeyB64 string `json:"coordinator_pubkey_b64"`
    Updated        string `json:"updated"`               // YYYY-MM-DD UTC
    SignatureB64   string `json:"signature_b64"`         // ed25519 over canonical payload
    Areas          []Area `json:"areas"`
}

type Area struct {
    Tag              string     `json:"tag"`               // e.g. "fel.general"
    Name             string     `json:"name"`
    Description      string     `json:"description"`
    Language         string     `json:"language"`          // BCP 47, default "en"
    Moderated        bool       `json:"moderated"`
    ManagerNodeID    string     `json:"manager_node_id"`   // defaults to coordinator
    ManagerPubKeyB64 string     `json:"manager_pubkey_b64"`
    Access           AreaAccess `json:"access"`
    Policy           AreaPolicy `json:"policy"`
}

// AreaAccess controls which leaf nodes may subscribe to this area.
type AreaAccess struct {
    // "open"     — any subscribed node may carry this area (default)
    // "approval" — manager must approve each node's subscription request
    // "closed"   — no new subscriptions accepted; manager explicitly adds nodes
    Mode string `json:"mode"`

    // AllowList is the set of node_ids explicitly permitted.
    // For "open" areas this is ignored.
    // For "approval" areas this is the set of approved nodes.
    // For "closed" areas this is the complete allowed set.
    AllowList []string `json:"allow_list,omitempty"`

    // DenyList is always enforced regardless of Mode.
    // A node on the deny list is rejected even if also on AllowList.
    DenyList []string `json:"deny_list,omitempty"`
}

type AreaPolicy struct {
    MaxBodyBytes    int  `json:"max_body_bytes"`    // default 32768
    AllowANSI       bool `json:"allow_ansi"`
    RequireTearline bool `json:"require_tearline"`
}
```

**Access mode semantics:**

| Mode | Any node can subscribe? | How nodes get access |
|---|---|---|
| `open` | Yes | Subscribe and immediately receive messages |
| `approval` | No — pending by default | Manager approves from their sysop menu |
| `closed` | No | Manager explicitly adds node IDs to `AllowList` |

When a node is on `DenyList`, the hub rejects its subscription to that area
with HTTP 403 regardless of mode. The hub does not reveal the reason beyond
"access denied" to avoid enumerating deny lists.

---

#### NAL Package (`nal/nal.go`)

- `func Fetch(ctx context.Context, url string) (*protocol.NAL, error)`

  Fetches the NAL JSON from a hub's `/nal` endpoint. Does not verify —
  call `Verify` separately.

- `func Verify(nal *protocol.NAL) error`

  Reconstructs the canonical payload (NAL JSON with `signature_b64` set to
  `""`, keys sorted alphabetically via `encoding/json` with a fixed field
  ordering enforced by a custom marshaller — document the field order in
  a comment at the top of the file) and verifies `SignatureB64` against
  `CoordPubKeyB64` using `crypto/ed25519`. Returns a descriptive typed error.

- `func Sign(nal *protocol.NAL, ks *keystore.Keystore) error`

  Populates `CoordPubKeyB64`, sets `Updated` to today's UTC date, computes
  and sets `SignatureB64`. The keystore must hold the coordinator keypair.

- `type Cache struct` with:
  - `func NewCache(ttl time.Duration) *Cache`
  - `func (c *Cache) FetchAndVerify(ctx context.Context, url, network string) (*protocol.NAL, error)`

    Cache-first. On miss or expiry: fetch, verify, store. On fetch failure
    with a warm cache: return stale with a logged warning — stale-but-verified
    beats unavailable.

  - `func (c *Cache) Area(network, tag string) (*protocol.Area, error)` —
    returns `ErrNetworkNotCached` or `ErrAreaNotFound` as typed sentinels.

  - `func (c *Cache) NodeAllowed(network, tag, nodeID string) (bool, error)` —
    evaluates `DenyList` first, then `Mode` + `AllowList`. Used by the hub
    before distributing messages to a subscriber.

- NAL persistence in the dedup SQLite DB:

  ```sql
  CREATE TABLE IF NOT EXISTS nal_cache (
    network     TEXT PRIMARY KEY,
    nal_json    TEXT NOT NULL,
    verified_at DATETIME DEFAULT (datetime('now'))
  );
  ```

  Nodes survive restarts without re-fetching immediately.

- `nal/nal_test.go` — cover:
  - `Sign` → `Verify` round-trip succeeds.
  - `Verify` fails on any body field modification after signing.
  - `Verify` fails with a different keypair.
  - `NodeAllowed` — open area allows any node not on deny list.
  - `NodeAllowed` — approval area denies node not on allow list.
  - `NodeAllowed` — deny list overrides allow list.
  - `Cache.FetchAndVerify` returns stale on fetch failure when cache is warm.

---

#### Hub NAL Endpoints (`hub/nal_handler.go`)

- `GET /v3net/v1/{network}/nal`

  Public (no auth). Returns the current signed NAL. Mirror hubs serve their
  last verified copy.

- `POST /v3net/v1/{network}/nal`

  Coordinator-only (auth required; `X-V3Net-Node-ID` must match
  `coordinator_node_id` in the stored NAL). Verifies signature before
  accepting. On success, persists and fans out:

  ```
  event: nal_updated
  data: {"network":"felonynet","updated":"2026-03-16","area_count":7}
  ```

  Leaf nodes receiving this event re-fetch the NAL within 60 seconds (with
  ±10% jitter to avoid thundering herd).

---

#### Area Proposal Endpoints (`hub/proposal_handler.go`)

- `POST /v3net/v1/{network}/areas/propose`

  Any authenticated subscribed node may submit a proposal. Body:

  ```jsonc
  {
    "tag":         "fel.phreaking",
    "name":        "Phreaking",
    "description": "Voice networks, DTMF, blue boxes, history.",
    "language":    "en",
    "access_mode": "open",
    "allow_ansi":  true
  }
  ```

  Hub stores the proposal and notifies the coordinator node via SSE:

  ```
  event: area_proposed
  data: {"network":"felonynet","tag":"fel.phreaking","from_node":"b9f1c2d3e4a5b6c7","proposal_id":"uuid"}
  ```

  Response: `{ "ok": true, "proposal_id": "uuid", "status": "pending" }`

- `GET /v3net/v1/{network}/areas/proposals`

  Coordinator-only. Returns pending proposals:

  ```jsonc
  [
    {
      "proposal_id": "uuid",
      "tag":         "fel.phreaking",
      "name":        "Phreaking",
      "from_node":   "b9f1c2d3....",
      "from_bbs":    "The Underground BBS",
      "proposed_at": "2026-03-16T04:20:00Z",
      "status":      "pending"
    }
  ]
  ```

- `POST /v3net/v1/{network}/areas/proposals/{id}/approve`

  Coordinator-only. Adds the area to the NAL, re-signs it, publishes it.
  Optionally accepts a body to override proposal defaults (access mode, policy)
  before approval:

  ```jsonc
  {
    "access_mode": "approval",
    "manager_node_id": "b9f1c2d3e4a5b6c7"  // assign manager; defaults to proposer
  }
  ```

- `POST /v3net/v1/{network}/areas/proposals/{id}/reject`

  Coordinator-only. Accepts an optional `{ "reason": "..." }` body. The
  proposing node receives a `proposal_rejected` SSE event with the reason.

Hub schema for proposals:

```sql
CREATE TABLE IF NOT EXISTS area_proposals (
  id          TEXT PRIMARY KEY,   -- UUID
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

---

#### Area Access Management Endpoints (`hub/access_handler.go`)

These endpoints are callable by the **area manager** of a specific area (not
necessarily the network coordinator). Auth is required; the hub verifies that
`X-V3Net-Node-ID` matches `manager_node_id` for the requested area tag.

- `GET /v3net/v1/{network}/areas/{tag}/access`

  Returns current access config for the area (mode, allow list, deny list).
  Manager-only.

- `POST /v3net/v1/{network}/areas/{tag}/access/mode`

  Set the access mode. Body: `{ "mode": "open" | "approval" | "closed" }`.
  Triggers a NAL re-sign and publish.

- `GET /v3net/v1/{network}/areas/{tag}/access/requests`

  For `approval` mode areas: list nodes that have requested subscription but
  are not yet approved.

  ```jsonc
  [
    {
      "node_id":    "b9f1c2d3e4a5b6c7",
      "bbs_name":   "The Underground BBS",
      "bbs_host":   "bbs.example.net",
      "requested_at": "2026-03-16T04:20:00Z"
    }
  ]
  ```

- `POST /v3net/v1/{network}/areas/{tag}/access/approve`

  Approve one or more pending subscription requests. Body:
  `{ "node_ids": ["b9f1c2d3..."] }`.
  Adds nodes to `AllowList`, triggers NAL re-sign and publish. Approved nodes
  begin receiving messages on next distribution cycle.

- `POST /v3net/v1/{network}/areas/{tag}/access/deny`

  Deny pending requests or add existing subscribers to `DenyList`. Body:
  `{ "node_ids": ["b9f1c2d3..."], "reason": "optional" }`.
  Removes from `AllowList` if present, adds to `DenyList`, triggers NAL
  re-sign. Denied nodes stop receiving messages immediately and receive a
  `subscription_denied` SSE event.

- `POST /v3net/v1/{network}/areas/{tag}/access/remove`

  Remove a node from `DenyList` (reinstate eligibility without approving).
  Body: `{ "node_ids": ["b9f1c2d3..."] }`.

Hub schema for pending access requests:

```sql
CREATE TABLE IF NOT EXISTS area_access_requests (
  id           TEXT PRIMARY KEY,  -- UUID
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

---

#### Updated Subscription Flow

`POST /v3net/v1/subscribe` body gains `area_tags`:

```jsonc
{
  "network":    "felonynet",
  "node_id":    "a3f9e1b2c4d5e6f7",
  "pubkey_b64": "...",
  "bbs_name":   "The Underground BBS",
  "bbs_host":   "bbs.example.net",
  "area_tags":  ["fel.general", "fel.phreaking"]
}
```

For each requested area tag, the hub:

1. Validates the tag exists in the current NAL. Unknown tags: HTTP 422.
2. Checks `DenyList`. Denied nodes: HTTP 403, no reason given.
3. Checks access mode:
   - `open` → immediately active.
   - `approval` → status set to `pending`; area manager notified via SSE
     `area_access_requested` event; node receives messages only after approval.
   - `closed` → rejected with HTTP 403 unless already on `AllowList`.

Response includes per-area status:

```jsonc
{
  "ok": true,
  "areas": [
    { "tag": "fel.general",   "status": "active"  },
    { "tag": "fel.phreaking", "status": "pending" }
  ]
}
```

The subscriber schema gains `area_subscriptions` as a separate table (not a
JSON column) for queryability:

```sql
CREATE TABLE IF NOT EXISTS area_subscriptions (
  node_id    TEXT NOT NULL,
  network    TEXT NOT NULL,
  area_tag   TEXT NOT NULL,
  status     TEXT DEFAULT 'pending',  -- 'active' | 'pending' | 'denied'
  subscribed_at DATETIME DEFAULT (datetime('now')),
  PRIMARY KEY (node_id, network, area_tag)
);
```

Message distribution filters on `area_subscriptions` where `status = 'active'`.

---

#### Area Tag Format

`{network_prefix}.{area_name}` — validated with regexp:
`^[a-z0-9]{1,8}\.[a-z0-9-]{1,24}$`

---

#### Leaf-Side NAL Handling (`leaf/nal.go`)

- On startup: fetch and verify the NAL for each configured network.
- On `nal_updated` SSE event: re-fetch within 60 seconds ±10% jitter.
- On `area_access_requested` SSE event (if this node is an area manager):
  queue a notification for the sysop's next menu visit.
- On `proposal_rejected` SSE event: queue notification for the proposing sysop.
- On `subscription_denied` SSE event: log and surface in V3Net status menu.
- Expose `func (l *Leaf) Areas(network string) ([]protocol.Area, error)` for
  the sysop subscription UI.
- Expose `func (l *Leaf) PendingAccessRequests(network, tag string) ([]AccessRequest, error)`
  for the area manager UI.

---

#### Coordinator Key Transfer (in-BBS, no external tools)

Implement `POST /v3net/v1/{network}/coordinator/transfer`:

1. Current coordinator calls this endpoint with `{ "new_node_id": "...", "new_pubkey_b64": "..." }`.
2. Hub generates a signed transfer token (ed25519 signature by current coord key over `new_node_id + new_pubkey_b64 + timestamp`).
3. Hub sends a `coordinator_transfer_pending` SSE event to the new coordinator's node.
4. New coordinator redeems via `POST /v3net/v1/{network}/coordinator/accept` with the token.
5. Hub re-signs the NAL with the new coordinator's pubkey, publishes. Old key retired.

All steps happen from the Vision/3 sysop menu. No key material is ever
displayed or entered manually.

---

#### `docs/v3net-nal.md`

Document the following for sysops (plain language, no git references):

- What the NAL is and why it exists.
- How to propose a new area from the V3Net sysop menu.
- How coordinators approve or reject proposals.
- The three access modes and when to use each.
- How area managers approve/deny node subscriptions.
- How coordinator handoff works.
- For developers only: the optional git archival export and registry format.

---

### Phase 7 — Vision/3 Integration

This phase wires V3Net into the actual BBS. Read the existing codebase before
writing any code here. Specifically understand:

- How configuration is loaded (likely a TOML or YAML config file; find it).
- How the message bases are accessed (find the JAM reader/writer).
- How background goroutines are started at BBS startup.
- How the sysop configuration menus work (for the hub admin UI).

**Tasks:**

1. Add a `[v3net]` section to Vision/3's config with these fields:
   ```toml
   [v3net]
   enabled        = false
   keystore_path  = "data/v3net.key"
   dedup_db_path  = "data/v3net_dedup.sqlite"
   registry_url   = "https://raw.githubusercontent.com/ViSiON-3/v3net-registry/main/registry.json"

   # Hub mode (optional — only if this node hosts a hub)
   [v3net.hub]
   enabled     = false
   listen_addr = ":8765"
   tls_cert    = ""
   tls_key     = ""
   data_dir    = "data/v3net_hub"
   auto_approve = true

   # One [[v3net.leaf]] block per subscribed network
   [[v3net.leaf]]
   hub_url      = "https://bbs.felonynet.org"
   network      = "felonynet"
   board        = "FelonyNet General"   # local JAM message base name
   poll_interval = "5m"
   ```

2. Implement the `JAMWriter` interface wrapping Vision/3's existing JAM write
   path. Map `protocol.Message` fields to JAM header fields:
   - `from` → JAM `From`
   - `to` → JAM `To`
   - `subject` → JAM `Subject`
   - `date_utc` → JAM `DateWritten` (convert to local time or store UTC)
   - `body` → JAM `Body`; prepend a kludge line `\x01V3NETUUID: {msg_uuid}` so
     the UUID is recoverable from the JAM base if the dedup index is lost.
   - `attributes` → JAM `Attributes` (pass through)

3. At BBS startup, if `[v3net] enabled = true`:
   - Load or generate the keystore.
   - Open the dedup index.
   - Start a `leaf.Leaf` for each `[[v3net.leaf]]` block.
   - If `[v3net.hub] enabled = true`, start the hub server.

4. Hook into the BBS logon/logoff events to fire `leaf.SendLogon` /
   `leaf.SendLogoff`.

5. Hook into the message post path: when a user posts to a networked board,
   call `leaf.SendMessage` after the local JAM write succeeds.

6. Add the following sysop menu screens under a **"V3Net"** top-level sysop
   menu option:

   **V3Net Status** — overview screen:
   - Node ID
   - Hub mode: active/inactive
   - Each leaf subscription: network name, hub URL, last poll time, message
     count received, connection status (polling / SSE connected)

   **V3Net: Area Subscriptions** — per-network area manager:

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

   Spacebar on an unsubscribed area submits a subscription request to the hub.
   Status reflects the hub's response: `ACTIVE` (open area or approved),
   `PENDING` (approval-mode area awaiting manager decision), `DENIED`.
   `[E]` prompts for the local JAM message base name to map this area to;
   if the named base doesn't exist, offer to create it.
   `[P]` opens the area proposal form (below).

   **V3Net: Propose New Area** — form screen, opened from Area Subscriptions:

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

   On submit, calls `POST /v3net/v1/{network}/areas/propose`. Displays the
   hub's response (pending / rejected with reason) and returns to Area
   Subscriptions.

   **V3Net: Area Access Requests** — visible only if this node is an area
   manager for at least one area on any subscribed network:

   ```
   [ V3Net: Area Access Requests ]

     NETWORK     AREA TAG          BBS NAME                  REQUESTED
     felonynet   fel.phreaking     The Underground BBS       2d ago
     felonynet   fel.phreaking     Sector 7 BBS              5h ago

     [A]pprove  [D]eny  [B]lacklist  [Q]uit
   ```

   `[A]` approves the selected request — calls the approve endpoint, updates
   the NAL, sends confirmation to the requesting node via SSE.
   `[D]` denies with an optional reason string — calls the deny endpoint.
   `[B]` denies and adds the node to the area's `DenyList` permanently.

   **V3Net: Coordinator Panel** — visible only if this node is a network
   coordinator. Accessible from the V3Net menu:

   ```
   [ V3Net: Coordinator Panel — felonynet ]

     [P]ending area proposals  (2)
     [M]anage area managers
     [T]ransfer coordinator role
     [Q]uit
   ```

   Pending Proposals sub-screen:

   ```
   [ felonynet — Pending Area Proposals ]

     TAG              NAME           PROPOSED BY             AGE
     fel.phreaking    Phreaking      bbs.example.net         2d
     fel.art          ANSI Art       bbs.darkside.net        5h

     [A]pprove  [R]eject  [E]dit before approving  [Q]uit
   ```

   `[E]` before approving allows the coordinator to adjust access mode,
   assign a different area manager node ID, and tweak policy fields before
   the area is added to the signed NAL.

---

## Testing Strategy

- Phase 1–3: pure unit tests, no external dependencies.
- Phase 4–5: `httptest.NewServer` integration tests. No real network calls.
- Phase 6: mock the HTTP client to avoid hitting GitHub.
- Phase 7: integration with the real BBS — test with a local hub and leaf
  both running in the same process in separate goroutines.

Run `go test ./v3net/...` after each phase. All tests must pass before
proceeding to the next phase.

---

## Code Style Rules

- No global mutable state outside of the registry cache (which uses `sync.Map`).
- All exported functions accept `context.Context` as the first argument where
  any I/O is involved.
- Use `slog` (stdlib, Go 1.21+) for structured logging. Log at `Debug` for
  poll cycles, `Info` for connections/subscriptions, `Warn` for retries,
  `Error` for failures.
- Errors: wrap with `fmt.Errorf("...: %w", err)` at every boundary.
- No `panic` outside of `main` or test helpers.
- The hub and leaf must shut down cleanly on `context.Done()`. Use
  `context.WithCancel` or `context.WithTimeout` at every blocking call.

---

## Dependencies

Before adding any new dependency, check `go.mod` to see if an equivalent is
already in use. Prefer stdlib. Permitted new dependencies if not already
present:

- `modernc.org/sqlite` — CGO-free SQLite (only if the project has no SQLite
  driver already)
- Nothing else. Do not add external HTTP routers, JWT libraries, or UUID
  libraries. Use `crypto/rand` + `encoding/hex` for UUID v4 generation:

  ```go
  func NewUUID() string {
      var b [16]byte
      _, _ = rand.Read(b[:])
      b[6] = (b[6] & 0x0f) | 0x40 // version 4
      b[8] = (b[8] & 0x3f) | 0x80 // variant bits
      return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
          b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
  }
  ```

---

## felonynet Network Bootstrap

Once Phase 7 is complete and stable, the agent should produce a companion
document `docs/felonynet.md` with:

- What felonynet is (a general-purpose public BBS message network running on V3Net, Vision/3's native networking protocol — separate from and coexisting with FTN echomail networks)
- How to join as a leaf node (5-minute setup instructions)
- How to host a hub
- The canonical registry entry to submit via PR

---

## Questions to Answer Before Writing Code

Before starting Phase 7, read these files in the vision-3-bbs repo and
summarize what you find:

1. The main config struct — what file format, what package?
2. The JAM message base writer — what function signature writes a new message?
3. The startup sequence — where are background services launched?
4. The logon/logoff hooks — where are they fired?
5. The message post hook — where does a new message get committed to disk?

Output a short findings summary before writing any Phase 7 code.
