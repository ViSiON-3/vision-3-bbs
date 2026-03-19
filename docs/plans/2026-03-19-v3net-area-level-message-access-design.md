# V3Net Area-Level Message Access Control

**Date:** 2026-03-19
**Status:** Design
**Scope:** Add `area_tag` to the message wire format and enforce area-level access control on both POST and GET message endpoints.

---

## Problem

Messages in V3Net are currently stored and served at the network level with no area awareness. The NAL defines areas with access modes (open, approval, closed) and allow/deny lists, and `area_subscriptions` tracks per-node per-area subscription status — but neither is consulted during message POST or GET.

This means a node that hasn't been approved for a restricted area can still post messages to it, and all nodes receive all messages regardless of their area subscriptions. The AGENTS.v3net.md spec (line 963) states: "Message distribution filters on `area_subscriptions` where `status = 'active'`."

## Design Decisions

- **`area_tag` is a new required field** on `protocol.Message`, alongside the existing `origin_board` (which remains for display/provenance).
- **Enforcement on both POST and GET** — nodes cannot post to areas they aren't subscribed to, and GET results are filtered to only include messages for areas where the node has active subscriptions.
- **Per-network cursor preserved** — the leaf continues to use a single `since=UUID` cursor per network. The hub silently skips messages for areas the requesting node isn't subscribed to. No per-area polling required.
- **No backward compatibility concerns** — this is experimental software with no production deployments. Schema changes are drop-and-recreate.

---

## Wire Format Change

Add `area_tag` to `protocol.Message`:

```go
type Message struct {
    // ... existing fields ...
    AreaTag     string         `json:"area_tag"`
    OriginBoard string         `json:"origin_board"`
    // ... remaining fields ...
}
```

`area_tag` is the network-wide area identifier (e.g., `fel.general`) used for routing and access control. `origin_board` is the human-readable local board name at the originating BBS (e.g., "General Discussion") used for display.

### Validation

`Message.Validate()` rejects messages with a missing or invalid `area_tag` using the existing `protocol.AreaTagRegexp` (defined in `protocol/nal.go`): `^[a-z0-9]{1,8}\.[a-z0-9-]{1,24}$`.

The hub performs additional validation on POST: the `area_tag` must exist in the network's NAL. This check lives in the handler, not in `Validate()`, because the wire-level validator has no NAL context.

---

## Hub POST /messages Enforcement

After existing validation in `handlePostMessage`, two new checks are added:

1. **NAL must exist** — load the NAL via `h.nalStore.Get(network)`. If `nil` (no NAL published yet), return HTTP 422 with `"no NAL published for this network"`. This forces operators to set up the NAL before messages can flow.
2. **Area exists in NAL** — call `currentNAL.FindArea(msg.AreaTag)`. If the area doesn't exist, return HTTP 422 (`unknown area_tag`).
3. **Node has active subscription** — read `nodeID` from `r.Header.Get(headerNodeID)` (set by auth middleware). Call `h.areaSubscriptions.IsActive(nodeID, network, msg.AreaTag)`. If not active, return HTTP 403 (`area access denied`).

Check order: wire validation → network match → NAL exists → area exists in NAL → node authorized for area → dedup → store.

Note: the `h.messages.Store` call signature changes to include `areaTag`, so the call in `handlePostMessage` must be updated accordingly.

---

## Hub GET /messages Filtering

### Schema Change

The `messages` table gains an `area_tag` column and a composite index. Since this is experimental software with no production data, the migration strategy is: `NewMessageStore` attempts to add the column via `ALTER TABLE messages ADD COLUMN area_tag TEXT NOT NULL DEFAULT ''`. If this fails (column already exists), it's a no-op. The `CREATE TABLE IF NOT EXISTS` statement is updated for fresh databases. Note: `CREATE TABLE IF NOT EXISTS` does not alter existing tables — it only applies to new databases.

```sql
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
```

### Store Method

`MessageStore.Store` gains an `areaTag` parameter:

```go
func (ms *MessageStore) Store(msgUUID, network, areaTag, data string) (bool, error)
```

The INSERT statement adds the `area_tag` column.

### Fetch Method

`MessageStore.Fetch` gains a `[]string` parameter for allowed area tags:

```go
func (ms *MessageStore) Fetch(network, sinceUUID string, limit int, areaTags []string) ([]string, bool, error)
```

**Empty areaTags short-circuit:** If `areaTags` is empty, return `(nil, false, nil)` immediately without querying the database. Building an `IN ()` clause with zero parameters produces invalid SQL.

When `areaTags` is non-empty, the query adds an `area_tag IN (...)` filter. Both query branches (with and without `sinceUUID` cursor) must include this filter:

```sql
-- Without cursor (sinceUUID is empty):
SELECT data FROM messages
WHERE network = ? AND area_tag IN (?, ?, ...)
ORDER BY id ASC LIMIT ?

-- With cursor:
SELECT data FROM messages
WHERE network = ? AND area_tag IN (?, ?, ...)
AND id > (SELECT COALESCE((SELECT id FROM messages WHERE msg_uuid = ?), 0))
ORDER BY id ASC LIMIT ?
```

### Handler Changes

`handleGetMessages` reads `nodeID` from `r.Header.Get(headerNodeID)` (currently this function does not use node ID — this is a new addition). It calls `areaSubscriptions.ListForNode(nodeID, network)`, filters to entries with `status = "active"`, extracts the tag list, and passes it to `Fetch`. If a node has no active subscriptions, the empty-slice short-circuit returns an empty array.

### Pagination

The existing cursor model is unchanged from the leaf's perspective. The leaf passes `since=<last_uuid>&limit=100` and gets back the next batch of messages it's allowed to see. The hub silently skips messages for areas the node isn't subscribed to. Gaps in the sequential ID space are invisible to the client.

The composite index on `(network, area_tag, id)` keeps filtered queries efficient.

---

## Leaf-Side Changes

### Config Change

`V3NetLeafConfig` currently has a single `Board string` field — one area per leaf config. This changes to support multiple areas per network subscription:

```go
type V3NetLeafConfig struct {
    HubURL       string   `json:"hubUrl"`
    Network      string   `json:"network"`
    Boards       []string `json:"boards"`           // V3Net area tags (e.g., ["fel.general", "fel.phreaking"])
    PollInterval string   `json:"pollInterval"`
    Origin       string   `json:"origin,omitempty"`
}
```

The old `Board string` field is replaced by `Boards []string`. Each entry is a V3Net area tag that also serves as the local message area tag (consistent with how `SyncAreas` already works — it uses the area tag as both the V3Net identifier and the local `MessageArea.Tag`).

### Wire Message Construction

`BuildWireMessage` in `wire.go` (not "NewMessage" — that was a naming error) gains an `areaTag` parameter. When a BBS user posts a message to a V3Net area, the outbound path looks up the area tag from the local message area's config and sets it on the wire message.

### JAM Router

The `JAMWriter` interface is unchanged:

```go
type JAMWriter interface {
    WriteMessage(msg protocol.Message) (int64, error)
}
```

A new `JAMRouter` in `jam_router.go` implements `leaf.JAMWriter` and dispatches by area tag:

```go
type JAMRouter struct {
    adapters map[string]*JAMAdapter // area_tag → adapter
}

func (r *JAMRouter) WriteMessage(msg protocol.Message) (int64, error) {
    adapter, ok := r.adapters[msg.AreaTag]
    if !ok {
        slog.Warn("v3net: no local area for tag, skipping", "area_tag", msg.AreaTag)
        return 0, nil
    }
    return adapter.WriteMessage(msg)
}
```

Each `JAMAdapter` still holds a single `areaID` and writes to one JAM message base. The router dispatches based on `msg.AreaTag`.

### Service Integration

`Service.AddLeaf` receives the router (which satisfies `JAMWriter`). The router is built during startup by iterating the leaf config's `Boards` slice:

```go
for _, tag := range lcfg.Boards {
    area, ok := mgr.GetAreaByTag(tag)
    if !ok {
        // SyncAreas should have created it; skip if missing
        continue
    }
    router.adapters[tag] = NewJAMAdapter(mgr, area.ID)
}
```

`SyncAreas` signature is unchanged — it still accepts `[]config.V3NetLeafConfig`. The internal loop changes to iterate `lcfg.Boards` instead of reading the old `lcfg.Board` field. Its return type remains `int` (count of areas created) — the router builds its own mapping separately using `mgr.GetAreaByTag`.

The startup code that calls `service.RegisterArea(areaID, network)` must also be updated to loop over all boards in each leaf config, calling `RegisterArea` for each one. This ensures `NetworkForArea` returns the correct network for outbound message routing.

### Subscribe with Area Tags

The leaf's subscribe request already supports `area_tags` via `protocol.SubscribeRequest.AreaTags`. Currently the leaf sends no area tags. With this change:

1. `leaf.Config` gains an `AreaTags []string` field (populated from `V3NetLeafConfig.Boards`).
2. `leaf.subscribe()` in `leaf/sender.go` sets `req.AreaTags = l.cfg.AreaTags` before marshalling.
3. `service.AddLeaf` passes `lcfg.Boards` through to `leaf.Config.AreaTags`.

This ensures the hub creates `area_subscriptions` entries at subscribe time, which enables both POST authorization and GET filtering.

---

## Edge Cases

### No NAL published yet

If `nalStore.Get(network)` returns `(nil, nil)`, POST /messages returns HTTP 422: `"no NAL published for this network"`. GET /messages returns an empty array (since the node can have no active area subscriptions without a NAL). This forces the operator to publish a NAL before message flow begins.

### Area removed from NAL after subscription

If an area is removed from the NAL while nodes have active subscriptions: POST is rejected (area not in NAL). GET continues to serve stored messages for that area (the subscription is still active). This is acceptable — the area manager should revoke subscriptions when removing an area, or the hub can garbage-collect orphaned subscriptions in a future cleanup pass.

---

## Testing Strategy

### Hub Tests (`hub/hub_area_msg_test.go` — new file)

- POST message without `area_tag` → 422 (wire validation)
- POST message with `area_tag` not in NAL → 422
- POST message when no NAL exists → 422
- POST message to area node isn't subscribed to → 403
- POST message to area with active subscription → 200
- GET messages only returns messages for node's active area subscriptions
- GET messages with no active subscriptions → empty array

### Protocol Tests (`protocol/message_test.go`)

- `Message.Validate()` rejects missing `area_tag`
- `Message.Validate()` rejects invalid `area_tag` format

### Integration Tests (`integration_test.go`)

- End-to-end: leaf posts message with `area_tag`, hub stores, other leaf polls and receives only messages for its subscribed areas

### JAM Router Tests (`v3net/jam_router_test.go` — new file)

- Router dispatches to correct adapter by `area_tag`
- Router skips messages for unknown `area_tag` (returns 0, nil)

### Existing Test Updates

All existing tests that construct `protocol.Message` (both as Go structs and raw JSON strings) must add `AreaTag`/`"area_tag"` to avoid validation failures. This includes raw JSON literals in `hub_test.go`, `hub_nal_test.go`, `leaf_test.go`, and `integration_test.go`.

---

## Files Modified

| File | Change |
|------|--------|
| `internal/v3net/protocol/message.go` | Add `AreaTag` field, update `Validate()` |
| `internal/v3net/hub/messages.go` | Add `area_tag` column to schema, update `Store`/`Fetch` signatures and queries |
| `internal/v3net/hub/handlers.go` | Add NAL existence + area existence + subscription checks on POST; add node ID lookup and area filtering on GET; update `Store` call to pass `areaTag` |
| `internal/v3net/wire.go` | Add `areaTag` parameter to `BuildWireMessage` |
| `internal/v3net/jam_adapter.go` | No structural change (individual adapters unchanged) |
| `internal/v3net/service.go` | Build `JAMRouter` from config boards, pass to `AddLeaf`; update `RegisterArea` loop |
| `internal/v3net/area_sync.go` | Iterate `lcfg.Boards` instead of `lcfg.Board` |
| `internal/config/config.go` | Change `V3NetLeafConfig.Board` to `Boards []string` |
| `internal/v3net/leaf/config.go` | Add `AreaTags []string` field to `leaf.Config` |
| `internal/v3net/leaf/sender.go` | Set `req.AreaTags` from config in `subscribe()` |
| `internal/v3net/leaf/jam.go` | No change (interface unchanged) |

## Files Added

| File | Purpose |
|------|---------|
| `internal/v3net/jam_router.go` | `JAMRouter` struct implementing `leaf.JAMWriter` with area_tag dispatch |

## Test Files Modified

| File | Change |
|------|--------|
| `internal/v3net/hub/hub_test.go` | Add `AreaTag`/`"area_tag"` to all test messages (structs and raw JSON) |
| `internal/v3net/hub/hub_nal_test.go` | Add `"area_tag"` to pagination test raw JSON messages |
| `internal/v3net/protocol/message_test.go` | Add `area_tag` validation tests |
| `internal/v3net/leaf/leaf_test.go` | Add `AreaTag` to test messages |
| `internal/v3net/integration_test.go` | Add `AreaTag` to test messages, update `JAMWriter` for router, add area-filtered polling test |

## Test Files Added

| File | Purpose |
|------|---------|
| `internal/v3net/hub/hub_area_msg_test.go` | Hub area-level POST/GET enforcement tests |
| `internal/v3net/jam_router_test.go` | JAM router dispatch and skip tests |
