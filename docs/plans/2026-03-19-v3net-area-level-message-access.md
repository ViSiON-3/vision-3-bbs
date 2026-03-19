# V3Net Area-Level Message Access Control — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `area_tag` to the message wire format and enforce area-level access control on hub POST and GET message endpoints.

**Architecture:** Messages gain a required `area_tag` field validated against the NAL. The hub enforces area subscriptions on POST (reject unauthorized) and GET (filter to subscribed areas). The leaf sends area tags at subscribe time and routes inbound messages to per-area JAM adapters via a new JAMRouter.

**Tech Stack:** Go stdlib, SQLite (modernc.org/sqlite), ed25519 auth, existing hub/leaf/protocol packages.

**Spec:** `docs/plans/2026-03-19-v3net-area-level-message-access-design.md`

---

## File Structure

| File | Action | Responsibility |
|------|--------|----------------|
| `internal/v3net/protocol/message.go` | Modify | Add `AreaTag` field, update `Validate()` |
| `internal/v3net/protocol/message_test.go` | Modify | Add `AreaTag` to `validMessage()`, add area_tag validation tests |
| `internal/v3net/hub/messages.go` | Modify | Add `area_tag` column, update `Store`/`Fetch` signatures |
| `internal/v3net/hub/handlers.go` | Modify | POST: NAL+area+subscription checks; GET: area filtering; update `Store`/`Fetch` calls |
| `internal/v3net/wire.go` | Modify | Add `areaTag` parameter to `BuildWireMessage` |
| `internal/v3net/wire_test.go` | Modify | Update 3 `BuildWireMessage` call sites |
| `internal/config/config.go` | Modify | `Board string` → `Boards []string` |
| `internal/v3net/leaf/config.go` | Modify | Add `AreaTags []string` |
| `internal/v3net/leaf/sender.go` | Modify | Set `req.AreaTags` in `subscribe()` |
| `internal/v3net/jam_router.go` | **Create** | `JAMRouter` dispatches by `area_tag` |
| `internal/v3net/service.go` | Modify | Pass `AreaTags` to leaf, build router |
| `internal/v3net/area_sync.go` | Modify | Iterate `Boards` instead of `Board` |
| `internal/v3net/area_sync_test.go` | Modify | Update `Board:` to `Boards:` in test fixtures |
| `internal/menu/v3net_areas.go` | Modify | Update `.Board` references to work with `Boards` |
| `internal/configeditor/fields_v3net.go` | Modify | Update Board field editor to Boards list |
| `internal/configeditor/view_list.go` | Modify | Update v3netleaf list display |
| `internal/configeditor/update_wizard_form.go` | Modify | `Board:` → `Boards: []string{...}` |
| `internal/configeditor/update_v3net_wizard_hub.go` | Modify | `Board:` → `Boards: []string{...}` |
| `cmd/vision3/main.go` | Modify | Iterate `lcfg.Boards`, build `JAMRouter`, update `BuildWireMessage` call |
| `internal/v3net/hub/hub_test.go` | Modify | Add `AreaTag`, seed NAL, subscribe with areas in message tests |
| `internal/v3net/hub/hub_nal_test.go` | Modify | Add `"area_tag"` to raw JSON messages, seed NAL, subscribe with areas |
| `internal/v3net/leaf/leaf_test.go` | Modify | Add `AreaTag` to `testMessage()` |
| `internal/v3net/integration_test.go` | Modify | Add `AreaTag`, seed NAL, subscribe with areas, add multi-area test |
| `internal/v3net/hub/hub_area_msg_test.go` | **Create** | Area-level POST/GET enforcement tests |
| `internal/v3net/jam_router_test.go` | **Create** | JAM router dispatch tests |

---

### Task 1: Add `AreaTag` to Wire Format, Validation, and All Test Fixtures

This task adds the field, updates validation, and fixes **all** existing test fixtures in one atomic commit so no intermediate state breaks tests.

**Files:**
- Modify: `internal/v3net/protocol/message.go:19-36` (Message struct), `44-82` (Validate)
- Modify: `internal/v3net/protocol/message_test.go:8-24` (validMessage + new tests)
- Modify: `internal/v3net/hub/hub_test.go` (all message structs/JSON)
- Modify: `internal/v3net/hub/hub_nal_test.go` (pagination test JSON)
- Modify: `internal/v3net/leaf/leaf_test.go:128-143` (testMessage helper)
- Modify: `internal/v3net/integration_test.go` (all message structs)

- [ ] **Step 1: Add `AreaTag` field to Message struct**

In `message.go`, add the field between `Network` and `MsgUUID`:

```go
type Message struct {
    V3Net       string         `json:"v3net"`
    Network     string         `json:"network"`
    AreaTag     string         `json:"area_tag"`
    MsgUUID     string         `json:"msg_uuid"`
    // ... rest unchanged
}
```

- [ ] **Step 2: Add area_tag validation to Validate()**

In `Validate()`, add after the network check (after line 50):

```go
if !AreaTagRegexp.MatchString(m.AreaTag) {
    return fmt.Errorf("invalid area_tag: %q", m.AreaTag)
}
```

Note: `AreaTagRegexp` is already defined in `protocol/nal.go`.

- [ ] **Step 3: Update protocol test fixtures and add area_tag tests**

Update `validMessage()` in `message_test.go` to include `AreaTag: "fel.general"`.

Add two test cases to the `TestValidate_InvalidFields` table:

```go
{
    name:    "empty area_tag",
    modify:  func(m *Message) { m.AreaTag = "" },
    wantErr: "invalid area_tag",
},
{
    name:    "invalid area_tag format",
    modify:  func(m *Message) { m.AreaTag = "INVALID" },
    wantErr: "invalid area_tag",
},
```

- [ ] **Step 4: Update hub_test.go test fixtures**

Add `AreaTag: "gen.general"` to the `protocol.Message` struct in `TestPostAndGetMessage` (line 163).

Add `"area_tag":"gen.general"` to the raw JSON strings in:
- `TestDuplicatePostReturns200` (line 227)
- `TestSSEReceivesNewMessageEvent` (line 286)

Add `"area_tag":"gen.general"` to each raw JSON message in `TestGetMessages_Pagination` in `hub_nal_test.go` (line 287).

These fixture updates only add the field so `Validate()` passes. The tests still use the pre-enforcement POST path. POST enforcement and NAL seeding happen in Task 3.

- [ ] **Step 5: Update leaf_test.go and integration_test.go fixtures**

Add `AreaTag: "gen.general"` to `testMessage()` in `leaf/leaf_test.go`.

Add `AreaTag: "gen.general"` to all `protocol.Message` structs in `integration_test.go`:
- `TestIntegration_PostAndPoll` (line 138)
- `TestIntegration_DedupPreventsDoubleWrite` (line 181)
- `TestIntegration_SSEReceivesEvents` (line 244)

- [ ] **Step 6: Run all tests to verify everything passes**

Run: `go test ./internal/v3net/... -v -count=1`
Expected: All PASS. The `area_tag` field is on the wire but no enforcement exists yet — existing tests pass because POST doesn't check areas yet.

- [ ] **Step 7: Commit**

```bash
git add internal/v3net/protocol/message.go internal/v3net/protocol/message_test.go \
  internal/v3net/hub/hub_test.go internal/v3net/hub/hub_nal_test.go \
  internal/v3net/leaf/leaf_test.go internal/v3net/integration_test.go
git commit -m "feat(protocol): add area_tag field to Message with validation

Update all test fixtures across hub, leaf, and integration tests to
include area_tag so Validate() passes."
```

---

### Task 2: Hub Message Store — Schema and Store Method

This task only changes the `Store` method and schema. The `Fetch` method and `handleGetMessages` are unchanged here — those move to Task 3 together with test fixes to avoid breaking existing GET tests.

**Files:**
- Modify: `internal/v3net/hub/messages.go:8-17` (schema), `25-30` (NewMessageStore), `33-46` (Store)
- Modify: `internal/v3net/hub/handlers.go:123` (Store call)
- Create: `internal/v3net/hub/hub_area_msg_test.go` (Store test)

- [ ] **Step 1: Write failing test for Store with areaTag**

Create `internal/v3net/hub/hub_area_msg_test.go`:

```go
package hub

import (
    "testing"
)

func TestMessageStore_StoreWithAreaTag(t *testing.T) {
    h, _ := setupTestHub(t)

    ok, err := h.messages.Store("uuid-001", "testnet", "gen.general", `{"test":"data"}`)
    if err != nil {
        t.Fatalf("Store: %v", err)
    }
    if !ok {
        t.Fatal("expected new message to be stored")
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/v3net/hub/ -run TestMessageStore_StoreWithAreaTag -v`
Expected: Compilation error — `Store` takes 3 args, not 4.

- [ ] **Step 3: Update schema, migration, and Store method**

In `messages.go`, update the schema:

```go
const messagesSchema = `
CREATE TABLE IF NOT EXISTS messages (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    msg_uuid    TEXT UNIQUE NOT NULL,
    network     TEXT NOT NULL,
    area_tag    TEXT NOT NULL,
    data        TEXT NOT NULL,
    received_at DATETIME DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_messages_network ON messages(network, id);
CREATE INDEX IF NOT EXISTS idx_messages_area ON messages(network, area_tag, id);
`
```

Add migration in `NewMessageStore`:

```go
func NewMessageStore(db *sql.DB) (*MessageStore, error) {
    if _, err := db.Exec(messagesSchema); err != nil {
        return nil, fmt.Errorf("hub: create messages table: %w", err)
    }
    // Migration: add area_tag column to existing databases.
    db.Exec("ALTER TABLE messages ADD COLUMN area_tag TEXT NOT NULL DEFAULT ''")
    // Migration: add composite index for area filtering.
    db.Exec("CREATE INDEX IF NOT EXISTS idx_messages_area ON messages(network, area_tag, id)")
    return &MessageStore{db: db}, nil
}
```

Update `Store` signature and INSERT:

```go
func (ms *MessageStore) Store(msgUUID, network, areaTag, data string) (bool, error) {
    res, err := ms.db.Exec(
        "INSERT OR IGNORE INTO messages (msg_uuid, network, area_tag, data) VALUES (?, ?, ?, ?)",
        msgUUID, network, areaTag, data,
    )
    // ... rest unchanged
}
```

- [ ] **Step 4: Update Store call in handlePostMessage**

In `handlers.go:123`:

```go
isNew, err := h.messages.Store(msg.MsgUUID, network, msg.AreaTag, string(data))
```

- [ ] **Step 5: Run tests**

Run: `go test ./internal/v3net/hub/ -v -count=1`
Expected: All PASS. `Fetch` and `handleGetMessages` are unchanged, so existing GET tests still work.

- [ ] **Step 6: Commit**

```bash
git add internal/v3net/hub/messages.go internal/v3net/hub/handlers.go \
  internal/v3net/hub/hub_area_msg_test.go
git commit -m "feat(hub): add area_tag column to messages table, update Store signature"
```

---

### Task 3: Hub POST Enforcement + Fetch Filtering + Fix Existing Tests

This task adds all enforcement (POST and GET) and simultaneously updates all existing tests that POST or GET messages. Everything changes in one atomic commit.

**Files:**
- Modify: `internal/v3net/hub/messages.go:51-97` (Fetch method)
- Modify: `internal/v3net/hub/handlers.go:59-91` (handleGetMessages), `94-141` (handlePostMessage)
- Modify: `internal/v3net/hub/hub_test.go` (tests that POST/GET messages)
- Modify: `internal/v3net/hub/hub_nal_test.go` (pagination test)
- Modify: `internal/v3net/integration_test.go` (setup + message tests)
- Add to: `internal/v3net/hub/hub_area_msg_test.go` (enforcement + fetch filter tests)

- [ ] **Step 1: Update Fetch method with area filtering**

Add `"strings"` to the import block in `messages.go`. Replace the `Fetch` method:

```go
func (ms *MessageStore) Fetch(network, sinceUUID string, limit int, areaTags []string) ([]string, bool, error) {
    if len(areaTags) == 0 {
        return nil, false, nil
    }
    if limit <= 0 || limit > 500 {
        limit = 100
    }

    placeholders := make([]string, len(areaTags))
    args := make([]any, 0, len(areaTags)+3)
    args = append(args, network)
    for i, tag := range areaTags {
        placeholders[i] = "?"
        args = append(args, tag)
    }
    inClause := strings.Join(placeholders, ",")

    fetchLimit := limit + 1

    var query string
    if sinceUUID == "" || sinceUUID == "0" {
        query = fmt.Sprintf(
            "SELECT data FROM messages WHERE network = ? AND area_tag IN (%s) ORDER BY id ASC LIMIT ?",
            inClause,
        )
        args = append(args, fetchLimit)
    } else {
        query = fmt.Sprintf(
            `SELECT data FROM messages
             WHERE network = ? AND area_tag IN (%s)
             AND id > (SELECT COALESCE((SELECT id FROM messages WHERE msg_uuid = ?), 0))
             ORDER BY id ASC LIMIT ?`,
            inClause,
        )
        args = append(args, sinceUUID, fetchLimit)
    }

    rows, err := ms.db.Query(query, args...)
    if err != nil {
        return nil, false, fmt.Errorf("hub: fetch messages: %w", err)
    }
    defer rows.Close()

    var results []string
    for rows.Next() {
        var data string
        if err := rows.Scan(&data); err != nil {
            return nil, false, fmt.Errorf("hub: scan message: %w", err)
        }
        results = append(results, data)
    }
    if err := rows.Err(); err != nil {
        return nil, false, fmt.Errorf("hub: iterate messages: %w", err)
    }

    hasMore := len(results) > limit
    if hasMore {
        results = results[:limit]
    }
    return results, hasMore, nil
}
```

- [ ] **Step 2: Replace handleGetMessages to filter by area subscriptions**

Replace `handleGetMessages` in `handlers.go`:

```go
func (h *Hub) handleGetMessages(w http.ResponseWriter, r *http.Request) {
    network := extractNetwork(r.URL.Path)
    nodeID := r.Header.Get(headerNodeID)
    since := r.URL.Query().Get("since")
    limit := 100
    if l := r.URL.Query().Get("limit"); l != "" {
        if n, err := strconv.Atoi(l); err == nil && n >= 1 && n <= 500 {
            limit = n
        }
    }

    subs, err := h.areaSubscriptions.ListForNode(nodeID, network)
    if err != nil {
        slog.Error("list area subscriptions", "error", err)
        http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
        return
    }
    var areaTags []string
    for _, s := range subs {
        if s.Status == "active" {
            areaTags = append(areaTags, s.Tag)
        }
    }

    results, hasMore, err := h.messages.Fetch(network, since, limit, areaTags)
    if err != nil {
        slog.Error("fetch messages", "error", err)
        http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
        return
    }

    if hasMore {
        w.Header().Set("X-V3Net-Has-More", "true")
    }

    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(http.StatusOK)
    w.Write([]byte("["))
    for i, data := range results {
        if i > 0 {
            w.Write([]byte(","))
        }
        w.Write([]byte(data))
    }
    w.Write([]byte("]"))
}
```

- [ ] **Step 3: Add POST enforcement to handlePostMessage**

In `handlers.go`, after the network mismatch check (line 111) and before `NeedsTruncation`, add:

```go
// Area-level access control.
nodeID := r.Header.Get(headerNodeID)

currentNAL, nalErr := h.nalStore.Get(network)
if nalErr != nil || currentNAL == nil {
    writeJSON(w, http.StatusUnprocessableEntity, map[string]string{
        "error": "no NAL published for this network",
    })
    return
}

if currentNAL.FindArea(msg.AreaTag) == nil {
    writeJSON(w, http.StatusUnprocessableEntity, map[string]string{
        "error": "unknown area_tag: " + msg.AreaTag,
    })
    return
}

active, err := h.areaSubscriptions.IsActive(nodeID, network, msg.AreaTag)
if err != nil {
    slog.Error("check area subscription", "error", err)
    http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
    return
}
if !active {
    http.Error(w, `{"error":"area access denied"}`, http.StatusForbidden)
    return
}
```

- [ ] **Step 4: Write POST enforcement and GET filtering tests**

Add to `hub_area_msg_test.go` (adding necessary imports at the top):

```go
import (
    "encoding/json"
    "fmt"
    "net/http"
    "net/http/httptest"
    "path/filepath"
    "strings"
    "testing"

    "github.com/ViSiON-3/vision-3-bbs/internal/v3net/keystore"
    "github.com/ViSiON-3/vision-3-bbs/internal/v3net/nal"
    "github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

func seedAreaMsgNAL(t *testing.T, h *Hub, ks *keystore.Keystore) {
    t.Helper()
    testNAL := &protocol.NAL{
        V3NetNAL: "1.0", Network: "testnet",
        CoordNodeID: ks.NodeID(), CoordPubKeyB64: ks.PubKeyBase64(),
        Areas: []protocol.Area{
            {
                Tag: "gen.general", Name: "General", Language: "en",
                ManagerNodeID: ks.NodeID(), ManagerPubKeyB64: ks.PubKeyBase64(),
                Access: protocol.AreaAccess{Mode: protocol.AccessModeOpen},
                Policy: protocol.AreaPolicy{MaxBodyBytes: 65536, AllowANSI: true},
            },
            {
                Tag: "gen.restricted", Name: "Restricted", Language: "en",
                ManagerNodeID: ks.NodeID(), ManagerPubKeyB64: ks.PubKeyBase64(),
                Access: protocol.AreaAccess{Mode: protocol.AccessModeApproval},
                Policy: protocol.AreaPolicy{MaxBodyBytes: 65536, AllowANSI: true},
            },
        },
    }
    if err := nal.Sign(testNAL, ks); err != nil {
        t.Fatalf("sign NAL: %v", err)
    }
    if err := h.nalStore.Put("testnet", testNAL); err != nil {
        t.Fatalf("put NAL: %v", err)
    }
}

func registerLeafWithAreas(t *testing.T, ts *httptest.Server, leafKS *keystore.Keystore, areaTags []string) {
    t.Helper()
    req := protocol.SubscribeRequest{
        Network: "testnet", NodeID: leafKS.NodeID(),
        PubKeyB64: leafKS.PubKeyBase64(),
        BBSName: "Test BBS", BBSHost: "test.example.net",
        AreaTags: areaTags,
    }
    body, _ := json.Marshal(req)
    resp, err := http.Post(ts.URL+"/v3net/v1/subscribe", "application/json",
        strings.NewReader(string(body)))
    if err != nil {
        t.Fatalf("subscribe: %v", err)
    }
    resp.Body.Close()
    if resp.StatusCode != http.StatusOK {
        t.Fatalf("subscribe status: %d", resp.StatusCode)
    }
}

func testMsgJSON(areaTag, uuid string) string {
    return fmt.Sprintf(`{
        "v3net":"1.0","network":"testnet","area_tag":%q,
        "msg_uuid":%q,"thread_uuid":%q,
        "origin_node":"test.example.net","origin_board":"General",
        "from":"Tester","to":"All","subject":"Test",
        "date_utc":"2026-03-16T04:20:00Z","body":"Hello!","kludges":{}
    }`, areaTag, uuid, uuid)
}

func TestPostMessage_NoNAL_Returns422(t *testing.T) {
    h, _ := setupTestHub(t)
    ts := httptest.NewServer(h.newMux())
    defer ts.Close()

    leafKS, _, _ := keystore.Load(filepath.Join(t.TempDir(), "leaf.key"))
    registerLeaf(t, ts, leafKS)

    body := testMsgJSON("gen.general", "550e8400-e29b-41d4-a716-446655440000")
    req := signedRequest(t, leafKS, "POST", ts.URL+"/v3net/v1/testnet/messages", body)
    resp, err := http.DefaultClient.Do(req)
    if err != nil {
        t.Fatalf("POST: %v", err)
    }
    resp.Body.Close()
    if resp.StatusCode != http.StatusUnprocessableEntity {
        t.Errorf("expected 422, got %d", resp.StatusCode)
    }
}

func TestPostMessage_UnknownArea_Returns422(t *testing.T) {
    h, hubKS := setupTestHub(t)
    ts := httptest.NewServer(h.newMux())
    defer ts.Close()
    seedAreaMsgNAL(t, h, hubKS)

    leafKS, _, _ := keystore.Load(filepath.Join(t.TempDir(), "leaf.key"))
    registerLeafWithAreas(t, ts, leafKS, []string{"gen.general"})

    body := testMsgJSON("gen.nonexistent", "550e8400-e29b-41d4-a716-446655440000")
    req := signedRequest(t, leafKS, "POST", ts.URL+"/v3net/v1/testnet/messages", body)
    resp, err := http.DefaultClient.Do(req)
    if err != nil {
        t.Fatalf("POST: %v", err)
    }
    resp.Body.Close()
    if resp.StatusCode != http.StatusUnprocessableEntity {
        t.Errorf("expected 422, got %d", resp.StatusCode)
    }
}

func TestPostMessage_NotSubscribed_Returns403(t *testing.T) {
    h, hubKS := setupTestHub(t)
    ts := httptest.NewServer(h.newMux())
    defer ts.Close()
    seedAreaMsgNAL(t, h, hubKS)

    leafKS, _, _ := keystore.Load(filepath.Join(t.TempDir(), "leaf.key"))
    registerLeaf(t, ts, leafKS) // no area subscriptions

    body := testMsgJSON("gen.general", "550e8400-e29b-41d4-a716-446655440000")
    req := signedRequest(t, leafKS, "POST", ts.URL+"/v3net/v1/testnet/messages", body)
    resp, err := http.DefaultClient.Do(req)
    if err != nil {
        t.Fatalf("POST: %v", err)
    }
    resp.Body.Close()
    if resp.StatusCode != http.StatusForbidden {
        t.Errorf("expected 403, got %d", resp.StatusCode)
    }
}

func TestPostMessage_ActiveSubscription_Returns200(t *testing.T) {
    h, hubKS := setupTestHub(t)
    ts := httptest.NewServer(h.newMux())
    defer ts.Close()
    seedAreaMsgNAL(t, h, hubKS)

    leafKS, _, _ := keystore.Load(filepath.Join(t.TempDir(), "leaf.key"))
    registerLeafWithAreas(t, ts, leafKS, []string{"gen.general"})

    body := testMsgJSON("gen.general", "550e8400-e29b-41d4-a716-446655440000")
    req := signedRequest(t, leafKS, "POST", ts.URL+"/v3net/v1/testnet/messages", body)
    resp, err := http.DefaultClient.Do(req)
    if err != nil {
        t.Fatalf("POST: %v", err)
    }
    resp.Body.Close()
    if resp.StatusCode != http.StatusOK {
        t.Errorf("expected 200, got %d", resp.StatusCode)
    }
}

func TestGetMessages_FiltersToSubscribedAreas(t *testing.T) {
    h, hubKS := setupTestHub(t)
    ts := httptest.NewServer(h.newMux())
    defer ts.Close()
    seedAreaMsgNAL(t, h, hubKS)

    leafKS, _, _ := keystore.Load(filepath.Join(t.TempDir(), "leaf.key"))
    registerLeafWithAreas(t, ts, leafKS, []string{"gen.general"})

    // Store messages in both areas directly.
    h.messages.Store("uuid-a1", "testnet", "gen.general", testMsgJSON("gen.general", "uuid-a1"))
    h.messages.Store("uuid-b1", "testnet", "gen.restricted", testMsgJSON("gen.restricted", "uuid-b1"))
    h.messages.Store("uuid-a2", "testnet", "gen.general", testMsgJSON("gen.general", "uuid-a2"))

    req := signedRequest(t, leafKS, "GET", ts.URL+"/v3net/v1/testnet/messages", "")
    resp, err := http.DefaultClient.Do(req)
    if err != nil {
        t.Fatalf("GET: %v", err)
    }
    defer resp.Body.Close()

    var messages []json.RawMessage
    json.NewDecoder(resp.Body).Decode(&messages)
    if len(messages) != 2 {
        t.Errorf("expected 2 messages (gen.general only), got %d", len(messages))
    }
}

func TestGetMessages_NoSubscriptions_ReturnsEmpty(t *testing.T) {
    h, hubKS := setupTestHub(t)
    ts := httptest.NewServer(h.newMux())
    defer ts.Close()
    seedAreaMsgNAL(t, h, hubKS)

    leafKS, _, _ := keystore.Load(filepath.Join(t.TempDir(), "leaf.key"))
    registerLeaf(t, ts, leafKS) // no area subscriptions

    h.messages.Store("uuid-001", "testnet", "gen.general", testMsgJSON("gen.general", "uuid-001"))

    req := signedRequest(t, leafKS, "GET", ts.URL+"/v3net/v1/testnet/messages", "")
    resp, err := http.DefaultClient.Do(req)
    if err != nil {
        t.Fatalf("GET: %v", err)
    }
    defer resp.Body.Close()

    var messages []json.RawMessage
    json.NewDecoder(resp.Body).Decode(&messages)
    if len(messages) != 0 {
        t.Errorf("expected 0 messages for unsubscribed node, got %d", len(messages))
    }
}

func TestMessageStore_FetchFiltersByAreaTags(t *testing.T) {
    h, _ := setupTestHub(t)

    h.messages.Store("uuid-a1", "testnet", "gen.general", `{"area_tag":"gen.general","msg_uuid":"uuid-a1"}`)
    h.messages.Store("uuid-b1", "testnet", "gen.coding", `{"area_tag":"gen.coding","msg_uuid":"uuid-b1"}`)
    h.messages.Store("uuid-a2", "testnet", "gen.general", `{"area_tag":"gen.general","msg_uuid":"uuid-a2"}`)

    results, _, err := h.messages.Fetch("testnet", "", 100, []string{"gen.general"})
    if err != nil {
        t.Fatalf("Fetch: %v", err)
    }
    if len(results) != 2 {
        t.Fatalf("expected 2 messages for gen.general, got %d", len(results))
    }
}

func TestMessageStore_FetchEmptyAreaTagsReturnsNil(t *testing.T) {
    h, _ := setupTestHub(t)

    h.messages.Store("uuid-001", "testnet", "gen.general", `{"test":"data"}`)

    results, hasMore, err := h.messages.Fetch("testnet", "", 100, []string{})
    if err != nil {
        t.Fatalf("Fetch: %v", err)
    }
    if results != nil {
        t.Errorf("expected nil results for empty areaTags, got %v", results)
    }
    if hasMore {
        t.Error("expected hasMore=false for empty areaTags")
    }
}
```

- [ ] **Step 5: Update existing hub tests that POST/GET messages**

All tests that POST messages now need NAL seeded and area subscriptions. All tests that GET messages need area subscriptions for results to be returned.

In `hub_test.go`, update each test:

**`TestPostAndGetMessage`**: Add `seedTestNAL(t, h, hubKS)` after `setupTestHub`. Replace `registerLeaf(t, ts, leafKS)` with `registerLeafWithAreas(t, ts, leafKS, []string{"gen.general"})`.

**`TestDuplicatePostReturns200`**: Same pattern — seed NAL, use `registerLeafWithAreas`.

**`TestSSEReceivesNewMessageEvent`**: Same pattern — seed NAL, use `registerLeafWithAreas`.

In `hub_nal_test.go`, update **`TestGetMessages_Pagination`**: Seed NAL via `seedTestNAL(t, h, hubKS)` (change `h, _ := setupTestHub(t)` to `h, hubKS := setupTestHub(t)`). Replace `registerLeaf(t, ts, leafKS)` with `registerLeafWithAreas(t, ts, leafKS, []string{"gen.general"})`.

- [ ] **Step 6: Update integration_test.go**

Update `setupIntegration` to: (a) return `hubKS` as an additional return value (add `hubKS *keystore.Keystore` to the return signature), (b) seed NAL, and (c) subscribe with area tags. After `h, err = hub.New(hubCfg)`:

```go
// Seed NAL for area-level access control.
hubNAL := &protocol.NAL{
    V3NetNAL: "1.0", Network: "testnet",
    CoordNodeID: hubKS.NodeID(), CoordPubKeyB64: hubKS.PubKeyBase64(),
    Areas: []protocol.Area{{
        Tag: "gen.general", Name: "General", Language: "en",
        ManagerNodeID: hubKS.NodeID(), ManagerPubKeyB64: hubKS.PubKeyBase64(),
        Access: protocol.AreaAccess{Mode: protocol.AccessModeOpen},
        Policy: protocol.AreaPolicy{MaxBodyBytes: 65536, AllowANSI: true},
    }},
}
nal.Sign(hubNAL, hubKS)
h.NALStore().Put("testnet", hubNAL)
```

Update the subscribe request to include area tags:

```go
registerBody, _ := json.Marshal(protocol.SubscribeRequest{
    Network:   "testnet",
    NodeID:    leafKS.NodeID(),
    PubKeyB64: leafKS.PubKeyBase64(),
    BBSName:   "Integration Test BBS",
    BBSHost:   "test.example.net",
    AreaTags:  []string{"gen.general"},
})
```

Add import: `"github.com/ViSiON-3/vision-3-bbs/internal/v3net/nal"`.

`h.NALStore()` already exists in `hub.go:157`. Update all existing callers of `setupIntegration` in this file to unpack the new `hubKS` return value (use `_` where not needed).

- [ ] **Step 7: Run all tests**

Run: `go test ./internal/v3net/... -v -count=1`
Expected: All PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/v3net/hub/messages.go internal/v3net/hub/handlers.go \
  internal/v3net/hub/hub_area_msg_test.go \
  internal/v3net/hub/hub_test.go internal/v3net/hub/hub_nal_test.go \
  internal/v3net/integration_test.go
git commit -m "feat(hub): enforce area-level access control on POST/GET messages

- POST rejects messages when: no NAL exists (422), area_tag not in NAL
  (422), or node has no active area subscription (403).
- GET filters results to only include messages for the node's actively
  subscribed areas. Empty subscriptions returns empty array.
- Update all existing hub, integration tests to seed NAL and subscribe
  with area tags."
```

---

### Task 4: JAM Router

This task is independent of hub changes — it only depends on Task 1 (AreaTag field on Message).

**Files:**
- Create: `internal/v3net/jam_router.go`
- Create: `internal/v3net/jam_router_test.go`

- [ ] **Step 1: Write failing router tests**

Create `internal/v3net/jam_router_test.go`:

```go
package v3net

import (
    "testing"

    "github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

type mockWriter struct {
    messages []protocol.Message
}

func (m *mockWriter) WriteMessage(msg protocol.Message) (int64, error) {
    m.messages = append(m.messages, msg)
    return int64(len(m.messages)), nil
}

func TestJAMRouter_DispatchesToCorrectAdapter(t *testing.T) {
    general := &mockWriter{}
    coding := &mockWriter{}
    router := NewJAMRouter()
    router.Add("gen.general", general)
    router.Add("gen.coding", coding)

    msg := protocol.Message{AreaTag: "gen.general", MsgUUID: "uuid-1"}
    if _, err := router.WriteMessage(msg); err != nil {
        t.Fatalf("WriteMessage: %v", err)
    }

    if len(general.messages) != 1 {
        t.Errorf("expected 1 message to general, got %d", len(general.messages))
    }
    if len(coding.messages) != 0 {
        t.Errorf("expected 0 messages to coding, got %d", len(coding.messages))
    }
}

func TestJAMRouter_UnknownAreaTagReturnsZero(t *testing.T) {
    router := NewJAMRouter()

    msg := protocol.Message{AreaTag: "gen.unknown", MsgUUID: "uuid-1"}
    num, err := router.WriteMessage(msg)
    if err != nil {
        t.Fatalf("WriteMessage: %v", err)
    }
    if num != 0 {
        t.Errorf("expected 0 for unknown area, got %d", num)
    }
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/v3net/ -run TestJAMRouter -v`
Expected: Compilation error — `NewJAMRouter` undefined.

- [ ] **Step 3: Implement JAMRouter**

Create `internal/v3net/jam_router.go`:

```go
package v3net

import (
    "log/slog"

    "github.com/ViSiON-3/vision-3-bbs/internal/v3net/leaf"
    "github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

// JAMRouter implements leaf.JAMWriter and dispatches messages to the
// correct per-area JAMAdapter based on the message's AreaTag.
type JAMRouter struct {
    adapters map[string]leaf.JAMWriter // area_tag → writer
}

// NewJAMRouter creates an empty JAMRouter.
func NewJAMRouter() *JAMRouter {
    return &JAMRouter{
        adapters: make(map[string]leaf.JAMWriter),
    }
}

// Add registers a writer for the given area tag.
func (r *JAMRouter) Add(areaTag string, writer leaf.JAMWriter) {
    r.adapters[areaTag] = writer
}

// WriteMessage dispatches the message to the adapter matching msg.AreaTag.
// Returns (0, nil) if no adapter exists for the tag.
func (r *JAMRouter) WriteMessage(msg protocol.Message) (int64, error) {
    adapter, ok := r.adapters[msg.AreaTag]
    if !ok {
        slog.Warn("v3net: no local area for tag, skipping", "area_tag", msg.AreaTag)
        return 0, nil
    }
    return adapter.WriteMessage(msg)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/v3net/ -run TestJAMRouter -v`
Expected: All PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/v3net/jam_router.go internal/v3net/jam_router_test.go
git commit -m "feat(v3net): add JAMRouter for area-tag based message dispatch"
```

---

### Task 5: Leaf Config and Subscribe with Area Tags

**Files:**
- Modify: `internal/v3net/leaf/config.go:14-24`
- Modify: `internal/v3net/leaf/sender.go:19-26`

- [ ] **Step 1: Add AreaTags to leaf Config**

In `leaf/config.go`, add `AreaTags` after `Network`:

```go
type Config struct {
    HubURL       string
    Network      string
    AreaTags     []string // V3Net area tags to subscribe to
    PollInterval time.Duration
    Keystore     *keystore.Keystore
    DedupIndex   *dedup.Index
    JAMWriter    JAMWriter
    OnEvent      func(protocol.Event)
    BBSName      string
    BBSHost      string
}
```

- [ ] **Step 2: Update subscribe() to send area tags**

In `leaf/sender.go`, add `AreaTags: l.cfg.AreaTags` to the subscribe request:

```go
req := protocol.SubscribeRequest{
    Network:   l.cfg.Network,
    NodeID:    l.cfg.Keystore.NodeID(),
    PubKeyB64: l.cfg.Keystore.PubKeyBase64(),
    BBSName:   l.cfg.BBSName,
    BBSHost:   l.cfg.BBSHost,
    AreaTags:  l.cfg.AreaTags,
}
```

- [ ] **Step 3: Run leaf tests**

Run: `go test ./internal/v3net/leaf/ -v`
Expected: All PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/v3net/leaf/config.go internal/v3net/leaf/sender.go
git commit -m "feat(leaf): add AreaTags to config and send in subscribe request"
```

---

### Task 6: Config Change + All Downstream Consumers

This is an atomic commit that changes `Board` to `Boards` and updates **all** downstream consumers simultaneously. This prevents any intermediate broken state.

**Files:**
- Modify: `internal/config/config.go:929-935`
- Modify: `internal/v3net/area_sync.go:16-50`
- Modify: `internal/v3net/area_sync_test.go:35-38`
- Modify: `internal/v3net/wire.go:16-35`
- Modify: `internal/v3net/wire_test.go` (3 call sites)
- Modify: `internal/v3net/service.go:195-216` (AddLeaf)
- Modify: `internal/menu/v3net_areas.go:62,341,346-349,396`
- Modify: `internal/configeditor/fields_v3net.go:74-76`
- Modify: `internal/configeditor/view_list.go:281`
- Modify: `internal/configeditor/update_wizard_form.go:247`
- Modify: `internal/configeditor/update_v3net_wizard_hub.go:239`
- Modify: `cmd/vision3/main.go:1823-1841,1869`

- [ ] **Step 1: Update V3NetLeafConfig**

In `config.go:929-935`, replace `Board string` with `Boards []string`:

```go
type V3NetLeafConfig struct {
    HubURL       string   `json:"hubUrl"`
    Network      string   `json:"network"`
    Boards       []string `json:"boards"`             // V3Net area tags (e.g., ["fel.general", "fel.phreaking"])
    PollInterval string   `json:"pollInterval"`       // Duration string (e.g., "5m")
    Origin       string   `json:"origin,omitempty"`   // Origin line text
}
```

- [ ] **Step 2: Update area_sync.go**

Replace the loop body in `SyncAreas` to iterate `lcfg.Boards`:

```go
func SyncAreas(leaves []config.V3NetLeafConfig, mgr *message.MessageManager, confMgr *conference.ConferenceManager) int {
    created := 0
    for _, lcfg := range leaves {
        for _, board := range lcfg.Boards {
            if board == "" {
                continue
            }
            if _, ok := mgr.GetAreaByTag(board); ok {
                continue
            }

            area := message.MessageArea{
                Tag:          board,
                Name:         areaNameFromTag(board, lcfg.Network),
                AreaType:     "v3net",
                Network:      lcfg.Network,
                EchoTag:      board,
                ConferenceID: inferConferenceID(mgr, confMgr, lcfg.Network),
                AutoJoin:     true,
                ACSRead:      "s10",
                ACSWrite:     "s20",
            }

            id, err := mgr.AddArea(area)
            if err != nil {
                slog.Error("v3net: auto-create area failed",
                    "tag", board, "network", lcfg.Network, "error", err)
                continue
            }
            slog.Info("v3net: auto-created message area",
                "id", id, "tag", board, "network", lcfg.Network,
                "conference_id", area.ConferenceID)
            created++
        }
    }
    return created
}
```

- [ ] **Step 3: Update area_sync_test.go**

Change `Board:` to `Boards: []string{...}` in the test fixtures:

```go
leaves := []config.V3NetLeafConfig{
    {HubURL: "https://hub.example.com", Network: "felonynet", Boards: []string{"FELGEN"}},
    {HubURL: "https://hub.example.com", Network: "felonynet", Boards: []string{"GENERAL"}}, // already exists
    {HubURL: "https://hub.example.com", Network: "felonynet", Boards: []string{"FELTECH"}},
}
```

- [ ] **Step 4: Update BuildWireMessage to accept areaTag**

In `wire.go`, add `areaTag` as the second parameter:

```go
func BuildWireMessage(network, areaTag, originNode, originBoard, from, to, subject, body, origin string) protocol.Message {
    uuid := newUUID()
    return protocol.Message{
        V3Net:       protocol.ProtocolVersion,
        Network:     network,
        AreaTag:     areaTag,
        MsgUUID:     uuid,
        ThreadUUID:  uuid,
        // ... rest unchanged
    }
}
```

- [ ] **Step 5: Update wire_test.go call sites**

Update all 3 calls to `BuildWireMessage` to pass `areaTag` as the second argument:

```go
// line 9:
msg := BuildWireMessage("testnet", "gen.general", "abc123", "General", "Sysop", "All", "Hello", "body text", "My Cool BBS")

// line 16:
msg := BuildWireMessage("testnet", "gen.general", "abc123", "General", "Sysop", "All", "Hello", "body text", "My Cool BBS - bbs.example.com")

// line 23:
msg := BuildWireMessage("testnet", "gen.general", "abc123", "General", "Sysop", "All", "Hello", "body text", "")
```

- [ ] **Step 6: Update cmd/vision3/main.go — leaf setup**

Replace the leaf setup loop (lines 1823-1841) to iterate boards and build a `JAMRouter`:

```go
for _, lcfg := range v3netConfig.Leaves {
    router := v3net.NewJAMRouter()
    origin := lcfg.Origin
    if origin == "" {
        origin = serverConfig.BoardName
    }
    for _, tag := range lcfg.Boards {
        area, ok := messageMgr.GetAreaByTag(tag)
        if !ok {
            log.Printf("WARN: V3Net leaf %q: message area %q not found, skipping", lcfg.Network, tag)
            continue
        }
        router.Add(tag, v3net.NewJAMAdapter(messageMgr, area.ID))
        v3netAreaMap[area.ID] = v3netAreaInfo{Network: lcfg.Network, Origin: origin}
        svc.RegisterArea(area.ID, lcfg.Network)
    }
    if err := v3netService.AddLeaf(lcfg, router, nil); err != nil {
        log.Printf("ERROR: V3Net leaf %q: %v", lcfg.Network, err)
        continue
    }
}
```

- [ ] **Step 7: Update cmd/vision3/main.go — BuildWireMessage call**

Line 1869 calls `BuildWireMessage` with 8 args. Add `area.Tag` as the second argument. The area tag is available from `area *message.MessageArea` in the `OnMessagePosted` callback:

```go
msg := v3net.BuildWireMessage(info.Network, area.Tag, svc.NodeID(), serverConfig.BoardName, from, to, subject, body, info.Origin)
```

- [ ] **Step 8: Update service.go AddLeaf**

Pass `lcfg.Boards` to `leaf.Config.AreaTags`:

```go
l := leaf.New(leaf.Config{
    HubURL:       lcfg.HubURL,
    Network:      lcfg.Network,
    AreaTags:     lcfg.Boards,
    PollInterval: interval,
    // ... rest unchanged
})
```

- [ ] **Step 9: Update menu/v3net_areas.go**

Four changes:

**Line 62** — build subscribed set:
```go
for _, lcfg := range v3cfg.Leaves {
    for _, board := range lcfg.Boards {
        subSet[lcfg.Network+":"+board] = true
    }
}
```

**Line 341** — check if already subscribed. Add helper and update check:
```go
func containsBoard(boards []string, tag string) bool {
    for _, b := range boards {
        if b == tag {
            return true
        }
    }
    return false
}
```
Then replace: `l.Board == area.Tag` → `containsBoard(l.Boards, area.Tag)`

**Lines 346-349** — add subscription. Append to existing leaf for same network, or create new:
```go
found := false
for i, l := range cfg.Leaves {
    if l.Network == network {
        cfg.Leaves[i].Boards = append(cfg.Leaves[i].Boards, area.Tag)
        found = true
        break
    }
}
if !found {
    cfg.Leaves = append(cfg.Leaves, config.V3NetLeafConfig{
        HubURL:       hubURL,
        Network:      network,
        Boards:       []string{area.Tag},
        PollInterval: "5m",
    })
}
```

**Line 396** — unsubscribe. Remove the board from the Boards slice. If Boards becomes empty, remove the entire leaf config entry:
```go
for i, l := range cfg.Leaves {
    if l.Network == network {
        filtered := l.Boards[:0]
        for _, b := range l.Boards {
            if b != tag {
                filtered = append(filtered, b)
            }
        }
        if len(filtered) == 0 {
            cfg.Leaves = append(cfg.Leaves[:i], cfg.Leaves[i+1:]...)
        } else {
            cfg.Leaves[i].Boards = filtered
        }
        break
    }
}
```

- [ ] **Step 10: Update configeditor/fields_v3net.go**

Change the Board field to comma-separated Boards:

```go
{
    Label: "Boards", Help: "V3Net area tags, comma-separated (e.g. fel.general,fel.coding)", Type: ftString, Col: 3, Row: 3, Width: 49,
    Get: func() string { return strings.Join(l.Boards, ",") },
    Set: func(val string) error {
        if val == "" {
            l.Boards = nil
        } else {
            l.Boards = strings.Split(val, ",")
        }
        return nil
    },
},
```

Add `"strings"` to the import block if not already present.

- [ ] **Step 11: Update configeditor/view_list.go**

Line 281 — update the list display:

```go
boards := ""
if len(l.Boards) > 0 {
    boards = l.Boards[0]
    if len(l.Boards) > 1 {
        boards += fmt.Sprintf(" (+%d)", len(l.Boards)-1)
    }
}
content = fmt.Sprintf(" %3d  %-30s %-14s %s", idx+1, padRight(l.HubURL, 30), padRight(l.Network, 14), boards)
```

- [ ] **Step 12: Update configeditor/update_wizard_form.go**

Line 247 — change `Board:` to `Boards:`:

```go
leaf := config.V3NetLeafConfig{
    HubURL:       m.wizard.hubURL,
    Network:      m.wizard.networkName,
    Boards:       []string{m.wizard.boardTag},
    PollInterval: m.wizard.pollInterval,
    Origin:       m.wizard.origin,
}
```

- [ ] **Step 13: Update configeditor/update_v3net_wizard_hub.go**

Line 239 — change `Board:` to `Boards:`:

```go
m.configs.V3Net.Leaves = append(m.configs.V3Net.Leaves, config.V3NetLeafConfig{
    HubURL:       hubURL,
    Network:      network,
    Boards:       []string{network},
    PollInterval: "5m",
})
```

- [ ] **Step 14: Verify full compilation**

Run: `go build ./...`
Expected: Clean build.

- [ ] **Step 15: Run all tests**

Run: `go test ./... -count=1`
Expected: All PASS.

- [ ] **Step 16: Commit**

```bash
git add internal/config/config.go internal/v3net/area_sync.go \
  internal/v3net/area_sync_test.go \
  internal/v3net/wire.go internal/v3net/wire_test.go \
  internal/v3net/service.go internal/menu/v3net_areas.go \
  internal/configeditor/fields_v3net.go internal/configeditor/view_list.go \
  internal/configeditor/update_wizard_form.go \
  internal/configeditor/update_v3net_wizard_hub.go \
  cmd/vision3/main.go
git commit -m "feat(config): change V3NetLeafConfig.Board to Boards []string

Update all consumers: area_sync, BuildWireMessage, service AddLeaf,
menu area subscribe/unsubscribe, config editor, wizard forms, and
cmd/vision3/main.go startup."
```

---

### Task 7: Integration Test — Multi-Area Filtered Polling

Per the spec's testing strategy: "End-to-end: leaf posts message with area_tag, hub stores, other leaf polls and receives only messages for its subscribed areas."

**Files:**
- Modify: `internal/v3net/integration_test.go`

- [ ] **Step 1: Write integration test for area-filtered polling**

The integration test is in package `v3net_test` (external), so it cannot access `h.messages` directly. Instead, use the hub's HTTP API to store a message for a different area by registering a second leaf subscribed to a different area.

Since `setupIntegration` was updated in Task 3 to return `hubKS`, update all existing callers to unpack the new return value. Then add the test:

```go
func TestIntegration_AreaFilteredPolling(t *testing.T) {
    h, ts, l, leafKS, hubKS, _, writer := setupIntegration(t)
    _ = leafKS

    // The default leaf from setupIntegration is subscribed to gen.general.
    // Post a message to gen.general — it should be received.
    msg := protocol.Message{
        V3Net: "1.0", Network: "testnet", AreaTag: "gen.general",
        MsgUUID: "880e8400-e29b-41d4-a716-446655440010",
        ThreadUUID: "880e8400-e29b-41d4-a716-446655440010",
        OriginNode: "test.example.net", OriginBoard: "General",
        From: "Tester", To: "All", Subject: "Area filtered test",
        DateUTC: "2026-03-16T04:20:00Z", Body: "Should be received.",
        Kludges: map[string]any{},
    }
    if err := l.SendMessage(msg); err != nil {
        t.Fatalf("SendMessage gen.general: %v", err)
    }

    // Update NAL to include a second area (gen.coding).
    updatedNAL := &protocol.NAL{
        V3NetNAL: "1.0", Network: "testnet",
        CoordNodeID: hubKS.NodeID(), CoordPubKeyB64: hubKS.PubKeyBase64(),
        Areas: []protocol.Area{
            {
                Tag: "gen.general", Name: "General", Language: "en",
                ManagerNodeID: hubKS.NodeID(), ManagerPubKeyB64: hubKS.PubKeyBase64(),
                Access: protocol.AreaAccess{Mode: protocol.AccessModeOpen},
                Policy: protocol.AreaPolicy{MaxBodyBytes: 65536, AllowANSI: true},
            },
            {
                Tag: "gen.coding", Name: "Coding", Language: "en",
                ManagerNodeID: hubKS.NodeID(), ManagerPubKeyB64: hubKS.PubKeyBase64(),
                Access: protocol.AreaAccess{Mode: protocol.AccessModeOpen},
                Policy: protocol.AreaPolicy{MaxBodyBytes: 65536, AllowANSI: true},
            },
        },
    }
    nal.Sign(updatedNAL, hubKS)
    h.NALStore().Put("testnet", updatedNAL)

    // Register a second leaf subscribed to gen.coding and use it to POST a message.
    leaf2KS, _, err := keystore.Load(filepath.Join(t.TempDir(), "leaf2.key"))
    if err != nil {
        t.Fatalf("load leaf2 keystore: %v", err)
    }
    registerBody2, _ := json.Marshal(protocol.SubscribeRequest{
        Network: "testnet", NodeID: leaf2KS.NodeID(),
        PubKeyB64: leaf2KS.PubKeyBase64(),
        BBSName: "Leaf 2 BBS", BBSHost: "leaf2.example.net",
        AreaTags: []string{"gen.coding"},
    })
    resp2, _ := ts.Client().Post(ts.URL+"/v3net/v1/subscribe", "application/json",
        bytes.NewReader(registerBody2))
    resp2.Body.Close()

    // Create a second leaf instance to POST the gen.coding message through the hub API.
    leaf2Writer := &testJAMWriter{}
    leaf2Dedup, _ := dedup.Open(filepath.Join(t.TempDir(), "dedup2.sqlite"))
    t.Cleanup(func() { leaf2Dedup.Close() })
    l2 := leaf.New(leaf.Config{
        HubURL:       ts.URL,
        Network:      "testnet",
        AreaTags:     []string{"gen.coding"},
        PollInterval: 50 * time.Millisecond,
        Keystore:     leaf2KS,
        DedupIndex:   leaf2Dedup,
        JAMWriter:    leaf2Writer,
    })

    codingMsg := protocol.Message{
        V3Net: "1.0", Network: "testnet", AreaTag: "gen.coding",
        MsgUUID: "990e8400-e29b-41d4-a716-446655440011",
        ThreadUUID: "990e8400-e29b-41d4-a716-446655440011",
        OriginNode: "leaf2.example.net", OriginBoard: "Coding",
        From: "Leaf2 User", To: "All", Subject: "Coding msg",
        DateUTC: "2026-03-16T04:20:00Z", Body: "Should NOT be received by leaf 1.",
        Kludges: map[string]any{},
    }
    if err := l2.SendMessage(codingMsg); err != nil {
        t.Fatalf("SendMessage gen.coding: %v", err)
    }

    // Poll with the original leaf (subscribed to gen.general only).
    ctx := context.Background()
    count, err := l.Poll(ctx)
    if err != nil {
        t.Fatalf("poll: %v", err)
    }
    if count != 1 {
        t.Fatalf("expected 1 message from poll (gen.general only), got %d", count)
    }
    if writer.count() != 1 {
        t.Fatalf("expected 1 JAM write, got %d", writer.count())
    }
    got := writer.get(0)
    if got.AreaTag != "gen.general" {
        t.Errorf("expected area_tag gen.general, got %q", got.AreaTag)
    }
}
```

All callers of `setupIntegration` in this file must be updated to unpack the additional `hubKS` return value (use `_` where not needed). Update the existing test functions: `h, ts, l, leafKS, _, writer := setupIntegration(t)` → `h, ts, l, leafKS, _, _, writer := setupIntegration(t)`.

- [ ] **Step 2: Run the test**

Run: `go test ./internal/v3net/ -run TestIntegration_AreaFilteredPolling -v`
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add internal/v3net/integration_test.go
git commit -m "test(v3net): add integration test for area-filtered message polling"
```

---

### Task 8: Full Test Suite Verification

- [ ] **Step 1: Run all v3net tests**

Run: `go test ./internal/v3net/... -v -count=1`
Expected: All PASS.

- [ ] **Step 2: Run full project test suite**

Run: `go test ./... -count=1`
Expected: All PASS.

- [ ] **Step 3: Run linters**

Run: `gofmt -l ./internal/ && go vet ./...`
Expected: No output.

- [ ] **Step 4: Final commit if any fixes needed**

```bash
git add -A
git commit -m "fix: address remaining issues from area-level access control"
```
