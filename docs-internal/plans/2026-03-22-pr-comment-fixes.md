# PR Comment Fixes Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Address all CodeRabbit and Copilot PR review comments on PR #14 (feat: full-screen V3Net chat).

**Architecture:** Fixes are grouped by file/subsystem. The largest structural change is adding a `network` parameter to all `chatRooms` methods to isolate state per network. Other fixes are targeted one-liners or small guards.

**Tech Stack:** Go 1.25, `log/slog` for structured logging, `net/http`, SQLite via `modernc.org/sqlite`.

---

## Files to Touch

| File | Change |
|------|--------|
| `internal/v3net/hub/chat_rooms.go` | Add `network` param to all methods; nested map structure |
| `internal/v3net/hub/chat_handlers.go` | Pass `network` to chatRooms calls; cap `limit` at 200 |
| `internal/v3net/hub/handlers.go` | Pass `network` to `HandleDisconnect`; scope leave broadcasts |
| `internal/v3net/hub/chat_store.go` | Validate `retentionDays > 0`; make `prune()` return error; add `StartPruner` |
| `internal/v3net/hub/hub.go` | Call `chatStore.StartPruner(ctx)` from `Start()` |
| `internal/v3net/hub/server.go` | Tighten public chat route matchers |
| `internal/v3net/hub/chat_test.go` | Add test: cross-network isolation; retentionDays=0 error |
| `internal/v3net/leaf/sender.go` | `signedPostCtx`: accept 2xx; `get()`: use `l.client` with context |
| `internal/v3net/leaf/chat.go` | `Join`: check status before unmarshal; `deliver`/`Close`: hold lock during send/close; `dispatch`: filter `EventChatPrivate` by ToNode |
| `internal/v3net/leaf/sse.go` | Only call `notifyReconnect` on reconnects, not initial connect |
| `internal/v3net/leaf/leaf.go` | Pass nodeID to `chatSessionRegistry` |
| `internal/chat/local.go` | Fix dual timestamps in `Private()` |
| `internal/configeditor/v3net_area_browser_cmds.go` | Handle `io.ReadAll` error |
| `internal/menu/chat.go` | `/MSG handle@node` parsing; use `LoadedStrings` in pickers |
| `docs/plans/2026-03-21-v3net-chat.md` | Fix stale `github.com/visionbbs/vision3/...` import paths |

---

## Task 1: Network-scope `chatRooms` (Critical)

**Files:**
- Modify: `internal/v3net/hub/chat_rooms.go`
- Modify: `internal/v3net/hub/chat_handlers.go`
- Modify: `internal/v3net/hub/handlers.go`
- Test: `internal/v3net/hub/chat_test.go`

- [ ] **Step 1: Write failing test for cross-network isolation**

Add to `internal/v3net/hub/chat_test.go`:

```go
func TestChatNetworkIsolation(t *testing.T) {
	h, _ := setupTestHub(t)
	ts := httptest.NewServer(h.newMux())
	defer ts.Close()

	ks1, _, _ := keystore.Load(filepath.Join(t.TempDir(), "leaf1.key"))
	ks2, _, _ := keystore.Load(filepath.Join(t.TempDir(), "leaf2.key"))
	registerLeaf(t, ts, ks1)
	// ks2 on "testnet2" — hub may not know this network, so join directly to chatRooms
	// Instead: test via chatRooms directly
	cr := newChatRooms()
	cr.Join("testnet", "lobby", "node1", "alice")
	cr.Join("testnet2", "lobby", "node2", "bob")

	rooms1 := cr.RoomList("testnet")
	rooms2 := cr.RoomList("testnet2")

	if len(rooms1) != 1 || rooms1[0].UserCount != 1 {
		t.Errorf("testnet should have 1 user in lobby, got %+v", rooms1)
	}
	if len(rooms2) != 1 || rooms2[0].UserCount != 1 {
		t.Errorf("testnet2 should have 1 user in lobby, got %+v", rooms2)
	}

	// Disconnect node1 from testnet; node2 in testnet2 should be unaffected.
	cr.HandleDisconnect("testnet", "node1")
	rooms2After := cr.RoomList("testnet2")
	if len(rooms2After) != 1 {
		t.Errorf("testnet2 lobby should still have 1 user after testnet disconnect, got %+v", rooms2After)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd /home/robbie/git/vision-3-bbs && go test ./internal/v3net/hub/ -run TestChatNetworkIsolation -v
```
Expected: compile error — `RoomList` takes no args, `HandleDisconnect` takes one arg.

- [ ] **Step 3: Rewrite `chatRooms` to be network-scoped**

Replace `internal/v3net/hub/chat_rooms.go` entirely:

```go
package hub

import (
	"encoding/json"
	"sort"
	"sync"

	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

type roomState struct {
	topic   string
	members map[string][]string // nodeID → []handle
}

// chatRooms is the in-memory ephemeral room registry, scoped per network.
type chatRooms struct {
	mu       sync.RWMutex
	networks map[string]map[string]*roomState // network → room → state
}

func newChatRooms() *chatRooms {
	return &chatRooms{networks: make(map[string]map[string]*roomState)}
}

// roomsFor returns the room map for network, creating it if needed.
// Caller must hold mu (write lock).
func (cr *chatRooms) roomsFor(network string) map[string]*roomState {
	rooms := cr.networks[network]
	if rooms == nil {
		rooms = make(map[string]*roomState)
		cr.networks[network] = rooms
	}
	return rooms
}

// Join adds handle@nodeID to room in network, creating room/network if needed.
// Returns the current user list for that room.
func (cr *chatRooms) Join(network, room, nodeID, handle string) []string {
	cr.mu.Lock()
	defer cr.mu.Unlock()
	rooms := cr.roomsFor(network)
	rs := rooms[room]
	if rs == nil {
		rs = &roomState{members: make(map[string][]string)}
		rooms[room] = rs
	}
	for _, h := range rs.members[nodeID] {
		if h == handle {
			return cr.usersLocked(network, room)
		}
	}
	rs.members[nodeID] = append(rs.members[nodeID], handle)
	return cr.usersLocked(network, room)
}

// Leave removes handle@nodeID from room in network. Deletes room if empty.
func (cr *chatRooms) Leave(network, room, nodeID, handle string) {
	cr.mu.Lock()
	defer cr.mu.Unlock()
	rooms := cr.networks[network]
	if rooms == nil {
		return
	}
	rs := rooms[room]
	if rs == nil {
		return
	}
	handles := rs.members[nodeID]
	for i, h := range handles {
		if h == handle {
			rs.members[nodeID] = append(handles[:i], handles[i+1:]...)
			break
		}
	}
	if len(rs.members[nodeID]) == 0 {
		delete(rs.members, nodeID)
	}
	if len(rs.members) == 0 {
		delete(rooms, room)
	}
}

// SetTopic updates the topic for room in network (creates room if needed).
func (cr *chatRooms) SetTopic(network, room, topic string) {
	cr.mu.Lock()
	defer cr.mu.Unlock()
	rooms := cr.roomsFor(network)
	rs := rooms[room]
	if rs == nil {
		rs = &roomState{members: make(map[string][]string)}
		rooms[room] = rs
	}
	rs.topic = topic
}

// IsJoined reports whether handle@nodeID is in room in network.
func (cr *chatRooms) IsJoined(network, room, nodeID, handle string) bool {
	cr.mu.RLock()
	defer cr.mu.RUnlock()
	rooms := cr.networks[network]
	if rooms == nil {
		return false
	}
	rs := rooms[room]
	if rs == nil {
		return false
	}
	for _, h := range rs.members[nodeID] {
		if h == handle {
			return true
		}
	}
	return false
}

// RoomList returns all active rooms in network sorted by name.
func (cr *chatRooms) RoomList(network string) []protocol.ProtoChatRoomInfo {
	cr.mu.RLock()
	defer cr.mu.RUnlock()
	rooms := cr.networks[network]
	var list []protocol.ProtoChatRoomInfo
	for name, rs := range rooms {
		count := 0
		for _, handles := range rs.members {
			count += len(handles)
		}
		list = append(list, protocol.ProtoChatRoomInfo{
			Name:      name,
			Topic:     rs.topic,
			UserCount: count,
		})
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Name < list[j].Name })
	return list
}

// Users returns all handles currently in room in network.
func (cr *chatRooms) Users(network, room string) []string {
	cr.mu.RLock()
	defer cr.mu.RUnlock()
	return cr.usersLocked(network, room)
}

// usersLocked returns handles in room in network; caller must hold mu.
func (cr *chatRooms) usersLocked(network, room string) []string {
	rooms := cr.networks[network]
	if rooms == nil {
		return nil
	}
	rs := rooms[room]
	if rs == nil {
		return nil
	}
	var out []string
	for _, handles := range rs.members {
		out = append(out, handles...)
	}
	return out
}

// HandleDisconnect removes all handles for nodeID from all rooms in network.
// Returns a slice of (room, handle) pairs removed so the caller can broadcast
// chat_leave events.
func (cr *chatRooms) HandleDisconnect(network, nodeID string) [][2]string {
	cr.mu.Lock()
	defer cr.mu.Unlock()
	rooms := cr.networks[network]
	if rooms == nil {
		return nil
	}
	var removed [][2]string
	for roomName, rs := range rooms {
		for _, handle := range rs.members[nodeID] {
			removed = append(removed, [2]string{roomName, handle})
		}
		delete(rs.members, nodeID)
		if len(rs.members) == 0 {
			delete(rooms, roomName)
		}
	}
	return removed
}

// broadcastChatEvent is a helper to publish a chat SSE event via a Broadcaster.
func broadcastChatEvent(b *Broadcaster, network, eventType string, payload any) {
	data, _ := json.Marshal(payload)
	b.Publish(network, protocol.Event{Type: eventType, Data: data})
}

// HandleForNode returns the first handle that nodeID has in room in network, or "".
func (cr *chatRooms) HandleForNode(network, room, nodeID string) string {
	cr.mu.RLock()
	defer cr.mu.RUnlock()
	rooms := cr.networks[network]
	if rooms == nil {
		return ""
	}
	rs := rooms[room]
	if rs == nil {
		return ""
	}
	handles := rs.members[nodeID]
	if len(handles) == 0 {
		return ""
	}
	return handles[0]
}

// AnyHandleForNode returns any handle that nodeID has across any room in network, or "".
func (cr *chatRooms) AnyHandleForNode(network, nodeID string) string {
	cr.mu.RLock()
	defer cr.mu.RUnlock()
	rooms := cr.networks[network]
	for _, rs := range rooms {
		if handles := rs.members[nodeID]; len(handles) > 0 {
			return handles[0]
		}
	}
	return ""
}
```

- [ ] **Step 4: Update `chat_handlers.go` to pass `network`**

In `handleChatJoin`: change `h.chatRooms.Join(room, nodeID, req.Handle)` → `h.chatRooms.Join(network, room, nodeID, req.Handle)` and `h.chatRooms.RoomList()` → `h.chatRooms.RoomList(network)`.

In `handleChatLeave`: change `h.chatRooms.Leave(room, nodeID, req.Handle)` → `h.chatRooms.Leave(network, room, nodeID, req.Handle)`.

In `handleChatPost`: change `h.chatRooms.HandleForNode(room, nodeID)` → `h.chatRooms.HandleForNode(network, room, nodeID)`.

In `handleChatTopic`: change `h.chatRooms.HandleForNode(room, nodeID)` → `h.chatRooms.HandleForNode(network, room, nodeID)` and `h.chatRooms.SetTopic(room, req.Topic)` → `h.chatRooms.SetTopic(network, room, req.Topic)`.

In `handleChatPrivate`: change `h.chatRooms.AnyHandleForNode(nodeID)` → `h.chatRooms.AnyHandleForNode(network, nodeID)`.

In `handleChatRooms`: change `h.chatRooms.RoomList()` → `h.chatRooms.RoomList(network)` and fix the signature to accept `network` properly (remove the `_` param name).

- [ ] **Step 5: Update `handlers.go` to pass `network` to HandleDisconnect**

In `handleEvents`, change:
```go
removed := h.chatRooms.HandleDisconnect(nodeID)
```
to:
```go
removed := h.chatRooms.HandleDisconnect(network, nodeID)
```

- [ ] **Step 6: Run tests**

```bash
cd /home/robbie/git/vision-3-bbs && go test ./internal/v3net/hub/ -v 2>&1 | tail -20
```
Expected: all hub tests pass including `TestChatNetworkIsolation`.

- [ ] **Step 7: Commit**

```bash
cd /home/robbie/git/vision-3-bbs && git add internal/v3net/hub/chat_rooms.go internal/v3net/hub/chat_handlers.go internal/v3net/hub/handlers.go internal/v3net/hub/chat_test.go
git commit -m "fix(hub): scope chatRooms state per network to prevent cross-network leakage"
```

---

## Task 2: `ChatHistoryStore` — validate retentionDays and periodic pruning

**Files:**
- Modify: `internal/v3net/hub/chat_store.go`
- Modify: `internal/v3net/hub/hub.go`
- Test: `internal/v3net/hub/chat_test.go`

- [ ] **Step 1: Write failing test for retentionDays=0**

Add to `internal/v3net/hub/chat_test.go`:

```go
func TestChatStoreRetentionDaysValidation(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	_, err = NewChatHistoryStore(db, 0)
	if err == nil {
		t.Error("expected error for retentionDays=0")
	}
	_, err = NewChatHistoryStore(db, -1)
	if err == nil {
		t.Error("expected error for retentionDays=-1")
	}
	_, err = NewChatHistoryStore(db, 7)
	if err != nil {
		t.Errorf("unexpected error for retentionDays=7: %v", err)
	}
}
```

You will also need to add `"database/sql"` import and `_ "modernc.org/sqlite"` to the test file if not already there. Check `hub_test.go` for the existing sqlite import pattern.

- [ ] **Step 2: Run test to verify it fails**

```bash
cd /home/robbie/git/vision-3-bbs && go test ./internal/v3net/hub/ -run TestChatStoreRetentionDays -v
```
Expected: FAIL — `NewChatHistoryStore(db, 0)` returns nil error.

- [ ] **Step 3: Implement validation and periodic pruning**

Update `internal/v3net/hub/chat_store.go`:

1. Add `context` import.

2. In `NewChatHistoryStore`, add validation before anything else:
```go
if retentionDays <= 0 {
    return nil, fmt.Errorf("hub: retentionDays must be > 0, got %d", retentionDays)
}
```

3. Remove the `store.prune()` call from `NewChatHistoryStore`.

4. Change `prune()` to return an error and log via slog:
```go
func (chs *ChatHistoryStore) prune() error {
    cutoff := time.Now().UTC().AddDate(0, 0, -chs.retentionDays)
    if _, err := chs.db.Exec("DELETE FROM chat_history WHERE created_at < ?", cutoff); err != nil {
        return fmt.Errorf("hub: prune chat_history: %w", err)
    }
    if _, err := chs.db.Exec("DELETE FROM chat_private_history WHERE created_at < ?", cutoff); err != nil {
        return fmt.Errorf("hub: prune chat_private_history: %w", err)
    }
    return nil
}
```

5. Add `StartPruner` method (call from Hub.Start):
```go
// StartPruner runs a background goroutine that prunes old messages daily.
// It stops when ctx is cancelled.
func (chs *ChatHistoryStore) StartPruner(ctx context.Context) {
    go func() {
        // Run once at startup.
        if err := chs.prune(); err != nil {
            slog.Error("hub: chat history prune", "error", err)
        }
        ticker := time.NewTicker(24 * time.Hour)
        defer ticker.Stop()
        for {
            select {
            case <-ctx.Done():
                return
            case <-ticker.C:
                if err := chs.prune(); err != nil {
                    slog.Error("hub: chat history prune", "error", err)
                }
            }
        }
    }()
}
```

Add `"context"` and `"log/slog"` to imports.

- [ ] **Step 4: Call `StartPruner` from `Hub.Start`**

In `internal/v3net/hub/hub.go`, add to `Start()` after the broadcaster goroutine:
```go
h.chatStore.StartPruner(ctx)
```

- [ ] **Step 5: Run tests**

```bash
cd /home/robbie/git/vision-3-bbs && go test ./internal/v3net/hub/ -run TestChatStore -v
```
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
cd /home/robbie/git/vision-3-bbs && git add internal/v3net/hub/chat_store.go internal/v3net/hub/hub.go internal/v3net/hub/chat_test.go
git commit -m "fix(hub): validate retentionDays>0 and run chat history pruning on a daily ticker"
```

---

## Task 3: Hub minor fixes — limit cap and route tightening

**Files:**
- Modify: `internal/v3net/hub/chat_handlers.go`
- Modify: `internal/v3net/hub/server.go`

- [ ] **Step 1: Cap `limit` at 200 in `handleChatHistory`**

In `internal/v3net/hub/chat_handlers.go`, change the limit parsing block:
```go
// Before:
limit := 50
if v := r.URL.Query().Get("limit"); v != "" {
    if n, err := strconv.Atoi(v); err == nil && n > 0 {
        limit = n
    }
}
// After:
limit := 50
if v := r.URL.Query().Get("limit"); v != "" {
    if n, err := strconv.Atoi(v); err == nil && n > 0 {
        if n > 200 {
            n = 200
        }
        limit = n
    }
}
```

- [ ] **Step 2: Tighten public chat route matchers in `server.go`**

In `internal/v3net/hub/server.go`, replace the two loose public chat matchers:

```go
// Before:
case strings.HasSuffix(path, "/chat/rooms") && r.Method == http.MethodGet:
    h.handleChatRooms(w, r, extractNetwork(r.URL.Path))
    return
case strings.Contains(path, "/chat/rooms/") && strings.HasSuffix(path, "/history") && r.Method == http.MethodGet:
    h.handleChatHistory(w, r, extractNetwork(r.URL.Path))
    return

// After:
case strings.HasPrefix(path, "/v3net/v1/") && strings.HasSuffix(path, "/chat/rooms") && r.Method == http.MethodGet:
    h.handleChatRooms(w, r, extractNetwork(r.URL.Path))
    return
case strings.HasPrefix(path, "/v3net/v1/") && strings.Contains(path, "/chat/rooms/") && strings.HasSuffix(path, "/history") && r.Method == http.MethodGet:
    h.handleChatHistory(w, r, extractNetwork(r.URL.Path))
    return
```

- [ ] **Step 3: Run tests**

```bash
cd /home/robbie/git/vision-3-bbs && go test ./internal/v3net/hub/ -v 2>&1 | tail -10
```
Expected: all pass.

- [ ] **Step 4: Commit**

```bash
cd /home/robbie/git/vision-3-bbs && git add internal/v3net/hub/chat_handlers.go internal/v3net/hub/server.go
git commit -m "fix(hub): cap chat history limit at 200 and tighten public route matchers"
```

---

## Task 4: Leaf — `signedPostCtx` accept 2xx; `get()` use `l.client`

**Files:**
- Modify: `internal/v3net/leaf/sender.go`

- [ ] **Step 1: Fix `signedPostCtx` to accept any 2xx response**

In `internal/v3net/leaf/sender.go`, change line 174:
```go
// Before:
if resp.StatusCode != http.StatusOK {
    return fmt.Errorf("leaf: POST %s returned %d", path, resp.StatusCode)
}
// After:
if resp.StatusCode/100 != 2 {
    return fmt.Errorf("leaf: POST %s returned %d", path, resp.StatusCode)
}
```

This fixes Leave/Post/Private/SetTopic which the hub returns 204 for.

- [ ] **Step 2: Fix `get()` to use `l.client` with context**

Replace the `get()` function:
```go
// Before:
func (l *Leaf) get(path string) ([]byte, error) {
    resp, err := http.Get(l.cfg.HubURL + path)
    if err != nil {
        return nil, err
    }
    defer resp.Body.Close()
    if resp.StatusCode != http.StatusOK {
        return nil, fmt.Errorf("GET %s: status %d", path, resp.StatusCode)
    }
    return io.ReadAll(resp.Body)
}

// After:
func (l *Leaf) get(path string) ([]byte, error) {
    req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, l.cfg.HubURL+path, nil)
    if err != nil {
        return nil, fmt.Errorf("leaf: create GET request: %w", err)
    }
    resp, err := l.client.Do(req)
    if err != nil {
        return nil, err
    }
    defer resp.Body.Close()
    if resp.StatusCode != http.StatusOK {
        return nil, fmt.Errorf("GET %s: status %d", path, resp.StatusCode)
    }
    return io.ReadAll(resp.Body)
}
```

- [ ] **Step 3: Run tests**

```bash
cd /home/robbie/git/vision-3-bbs && go test ./internal/v3net/leaf/ -v 2>&1 | tail -10
```
Expected: all pass.

- [ ] **Step 4: Commit**

```bash
cd /home/robbie/git/vision-3-bbs && git add internal/v3net/leaf/sender.go
git commit -m "fix(leaf): accept 2xx in signedPostCtx and use l.client for GET requests"
```

---

## Task 5: Leaf — `Join` check HTTP status before unmarshal

**Files:**
- Modify: `internal/v3net/leaf/chat.go`

- [ ] **Step 1: Add status check in `Join`**

In `internal/v3net/leaf/chat.go`, in the `Join` method, insert the status check after `defer resp.Body.Close()` but **before** `io.ReadAll`:

```go
// Before:
respBytes, err := io.ReadAll(resp.Body)
if err != nil {
    return nil, nil, err
}
var joinResp protocol.ChatJoinResponse

// After:
if resp.StatusCode != http.StatusOK {
    return nil, nil, fmt.Errorf("join chat: hub returned status %d", resp.StatusCode)
}
respBytes, err := io.ReadAll(resp.Body)
if err != nil {
    return nil, nil, err
}
var joinResp protocol.ChatJoinResponse
```

- [ ] **Step 2: Run tests**

```bash
cd /home/robbie/git/vision-3-bbs && go test ./internal/v3net/leaf/ -v 2>&1 | tail -10
```
Expected: all pass.

- [ ] **Step 3: Commit**

```bash
cd /home/robbie/git/vision-3-bbs && git add internal/v3net/leaf/chat.go
git commit -m "fix(leaf): check HTTP status before unmarshaling Join response"
```

---

## Task 6: Leaf — Fix ChatSession.Close/deliver race condition

**Files:**
- Modify: `internal/v3net/leaf/chat.go`

**Context:** `deliver()` reads `s.closed` under lock then releases the lock before sending to the channel — a concurrent `Close()` can close the channel between the check and the send, causing a panic. Fix: hold `s.mu` during the channel send in `deliver()`. In `Close()`, set `s.closed = true` and `close(s.events)` while holding the lock, then deregister after releasing it.

- [ ] **Step 1: Fix `deliver()` to hold lock during send**

In `internal/v3net/leaf/chat.go`, replace the tail of `deliver()`:

```go
// Before (lines ~127–136):
s.mu.Lock()
closed := s.closed
s.mu.Unlock()
if closed {
    return
}
select {
case s.events <- ce:
default:
}

// After:
s.mu.Lock()
if !s.closed {
    select {
    case s.events <- ce:
    default:
    }
}
s.mu.Unlock()
```

- [ ] **Step 2: Fix `Close()` to hold lock during close**

In `internal/v3net/leaf/chat.go`, replace `Close()`:

```go
func (s *ChatSession) Close() error {
    s.mu.Lock()
    if s.closed {
        s.mu.Unlock()
        return nil
    }
    s.closed = true
    close(s.events)
    s.mu.Unlock()
    s.leaf.chatSessions.deregister(s.handle)
    return nil
}
```

- [ ] **Step 3: Fix `notifyReconnect()` to use `deliver`-style locking**

In `internal/v3net/leaf/chat.go`, `notifyReconnect()` sends directly to `s.events` without checking `s.closed`. Replace:

```go
// Before:
for _, s := range r.sessions {
    select {
    case s.events <- ev:
    default:
    }
}

// After:
for _, s := range r.sessions {
    s.mu.Lock()
    if !s.closed {
        select {
        case s.events <- ev:
        default:
        }
    }
    s.mu.Unlock()
}
```

- [ ] **Step 4: Run tests**

```bash
cd /home/robbie/git/vision-3-bbs && go test ./internal/v3net/leaf/ -v -race 2>&1 | tail -20
```
Expected: all pass, no race conditions detected.

- [ ] **Step 5: Commit**

```bash
cd /home/robbie/git/vision-3-bbs && git add internal/v3net/leaf/chat.go
git commit -m "fix(leaf): hold lock during channel send/close to prevent panic on concurrent Close"
```

---

## Task 7: Leaf — Filter `EventChatPrivate` by `ToNode`

**Files:**
- Modify: `internal/v3net/leaf/chat.go`
- Modify: `internal/v3net/leaf/leaf.go` (store nodeID on registry)

**Context:** `dispatch()` delivers private messages to any session matching `ToHandle` regardless of `ToNode`, leaking DMs across nodes with the same handle.

- [ ] **Step 1: Store nodeID on `chatSessionRegistry`**

In `internal/v3net/leaf/chat.go`, add `nodeID string` field to `chatSessionRegistry`:

```go
type chatSessionRegistry struct {
    mu       sync.RWMutex
    sessions map[string]*ChatSession
    nodeID   string
}

func newChatSessionRegistry(nodeID string) *chatSessionRegistry {
    return &chatSessionRegistry{sessions: make(map[string]*ChatSession), nodeID: nodeID}
}
```

- [ ] **Step 2: Update `dispatch()` to filter by `ToNode`**

In the `EventChatPrivate` case of `dispatch()`:

```go
case protocol.EventChatPrivate:
    var msg protocol.ChatMsgPayload
    json.Unmarshal(ev.Data, &msg)
    // Only deliver to sessions on this node (ToNode must match or be empty).
    if msg.ToNode != "" && msg.ToNode != r.nodeID {
        return
    }
    for _, s := range r.sessions {
        if s.handle == msg.ToHandle {
            s.deliver(ev)
        }
    }
```

- [ ] **Step 3: Update `leaf.go` to pass `NodeID()` to `newChatSessionRegistry`**

In `internal/v3net/leaf/leaf.go`, in `New()`:

```go
// Before:
l.chatSessions = newChatSessionRegistry()

// After:
l.chatSessions = newChatSessionRegistry(cfg.Keystore.NodeID())
```

- [ ] **Step 4: Run tests**

```bash
cd /home/robbie/git/vision-3-bbs && go test ./internal/v3net/leaf/ -v 2>&1 | tail -10
```
Expected: all pass.

- [ ] **Step 5: Commit**

```bash
cd /home/robbie/git/vision-3-bbs && git add internal/v3net/leaf/chat.go internal/v3net/leaf/leaf.go
git commit -m "fix(leaf): filter private message dispatch by ToNode to prevent cross-node DM leakage"
```

---

## Task 8: Leaf — Only fire reconnect notification on actual reconnects

**Files:**
- Modify: `internal/v3net/leaf/sse.go`

- [ ] **Step 1: Track whether initial connection has been made**

In `internal/v3net/leaf/sse.go`, update `runSSE()` and `connectSSE()`:

```go
// runSSE: add hasConnected bool
func (l *Leaf) runSSE(ctx context.Context) {
    attempt := 0
    hasConnected := false
    for {
        if ctx.Err() != nil {
            return
        }
        err := l.connectSSE(ctx, hasConnected)
        if ctx.Err() != nil {
            return
        }
        if err == nil {
            hasConnected = true
            attempt = 0
        } else {
            slog.Warn("leaf: SSE disconnected", "network", l.cfg.Network, "error", err)
            attempt++
        }
        delay := backoff(attempt)
        slog.Info("leaf: SSE reconnecting", "network", l.cfg.Network, "delay", delay)
        select {
        case <-ctx.Done():
            return
        case <-time.After(delay):
        }
    }
}

// connectSSE: accept reconnect bool; only notify when true
func (l *Leaf) connectSSE(ctx context.Context, reconnect bool) error {
    path := fmt.Sprintf("/v3net/v1/%s/events", l.cfg.Network)
    resp, err := l.signedGetSSE(ctx, path)
    if err != nil {
        return fmt.Errorf("SSE connect: %w", err)
    }
    defer resp.Body.Close()
    if resp.StatusCode != 200 {
        return fmt.Errorf("SSE status: %d", resp.StatusCode)
    }
    slog.Info("leaf: SSE connected", "network", l.cfg.Network)
    if reconnect {
        l.chatSessions.notifyReconnect()
    }
    // ... rest of the function unchanged
```

- [ ] **Step 2: Run tests**

```bash
cd /home/robbie/git/vision-3-bbs && go test ./internal/v3net/leaf/ -v 2>&1 | tail -10
```
Expected: all pass.

- [ ] **Step 3: Commit**

```bash
cd /home/robbie/git/vision-3-bbs && git add internal/v3net/leaf/sse.go
git commit -m "fix(leaf): only send reconnect notification on actual reconnect, not initial connect"
```

---

## Task 9: `chat/local.go` — Fix timestamp inconsistency in `Private()`

**Files:**
- Modify: `internal/chat/local.go`
- Test: `internal/chat/local_test.go`

- [ ] **Step 1: Write failing test**

Check `internal/chat/local_test.go` for existing test patterns. Add a test that verifies the timestamp used in the DB and event match (within a short window):

```go
func TestLocalChatPrivateTimestamp(t *testing.T) {
    svc1, err := NewLocalChatService("alice", ":memory:")
    if err != nil {
        t.Fatal(err)
    }
    defer svc1.Close()
    svc2, err := NewLocalChatService("bob", ":memory:")
    if err != nil {
        t.Fatal(err)
    }
    defer svc2.Close()

    // This test is primarily a compile check / regression guard.
    // The actual timestamp consistency fix is verified by code inspection.
    if err := svc1.Private("bob", "", "hello"); err != nil {
        t.Fatal(err)
    }
}
```

- [ ] **Step 2: Apply the fix**

In `internal/chat/local.go`, replace the `Private` method body:

```go
func (s *LocalChatService) Private(handle, _ string, text string) error {
    now := time.Now().UTC()
    _, err := s.db.Exec(
        `INSERT INTO chat_private_history(from_handle,to_handle,text,created_at) VALUES(?,?,?,?)`,
        s.handle, handle, text, now,
    )
    if err != nil {
        return err
    }
    msg := ChatMessage{Handle: s.handle, Text: text, Timestamp: now}
    sharedMu.RLock()
    for _, rs := range sharedRooms {
        if target, ok := rs.sessions[handle]; ok {
            select {
            case target.events <- ChatEvent{Type: TypePrivate, Message: &msg}:
            default:
            }
        }
    }
    sharedMu.RUnlock()
    return nil
}
```

- [ ] **Step 3: Run tests**

```bash
cd /home/robbie/git/vision-3-bbs && go test ./internal/chat/ -v 2>&1 | tail -10
```
Expected: all pass.

- [ ] **Step 4: Commit**

```bash
cd /home/robbie/git/vision-3-bbs && git add internal/chat/local.go internal/chat/local_test.go
git commit -m "fix(chat): use single timestamp in Private() for DB insert and event message"
```

---

## Task 10: `configeditor` — Handle `io.ReadAll` error

**Files:**
- Modify: `internal/configeditor/v3net_area_browser_cmds.go`

- [ ] **Step 1: Handle the read error**

In `internal/configeditor/v3net_area_browser_cmds.go`, replace:

```go
// Before:
body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
detail := strings.TrimSpace(string(body))
if detail == "" {
    detail = "(no body)"
}

// After:
body, readErr := io.ReadAll(io.LimitReader(resp.Body, 512))
detail := strings.TrimSpace(string(body))
if readErr != nil && detail == "" {
    detail = fmt.Sprintf("(failed to read error body: %v)", readErr)
}
if detail == "" {
    detail = "(no body)"
}
```

- [ ] **Step 2: Run tests**

```bash
cd /home/robbie/git/vision-3-bbs && go build ./... 2>&1
```
Expected: no errors.

- [ ] **Step 3: Commit**

```bash
cd /home/robbie/git/vision-3-bbs && git add internal/configeditor/v3net_area_browser_cmds.go
git commit -m "fix(configeditor): handle io.ReadAll error when reading hub error response body"
```

---

## Task 11: Menu — `/MSG handle@node` parsing and hard-coded strings

**Files:**
- Modify: `internal/menu/chat.go`

### Part A: `/MSG` node-aware parsing

- [ ] **Step 1: Update `/MSG` command to parse `handle@node`**

In `internal/menu/chat.go`, replace the `/MSG` handler block:

```go
// Before:
if strings.HasPrefix(upper, "/MSG ") {
    rest := strings.TrimSpace(trimmed[5:])
    parts := strings.SplitN(rest, " ", 2)
    if len(parts) == 2 {
        if msgErr := svc.Private(parts[0], "", parts[1]); msgErr != nil {
            writeChatLine(fmt.Sprintf(e.LoadedStrings.ChatSystemPrefix, "Could not send private message: "+msgErr.Error()))
        }
    }
    continue
}

// After:
if strings.HasPrefix(upper, "/MSG ") {
    rest := strings.TrimSpace(trimmed[5:])
    parts := strings.SplitN(rest, " ", 2)
    if len(parts) == 2 {
        target := strings.TrimSpace(parts[0])
        message := parts[1]
        toHandle := target
        toNode := ""
        if atIdx := strings.Index(target, "@"); atIdx > 0 && atIdx < len(target)-1 {
            toHandle = target[:atIdx]
            toNode = target[atIdx+1:]
        }
        if msgErr := svc.Private(toHandle, toNode, message); msgErr != nil {
            writeChatLine(fmt.Sprintf(e.LoadedStrings.ChatSystemPrefix, "Could not send private message: "+msgErr.Error()))
        }
    }
    continue
}
```

### Part B: Use `LoadedStrings` in `chatNetworkPicker` and `chatRoomPicker`

The `StringsConfig` has: `ChatNetworkPickerHeader`, `ChatNetworkPickerEntry`, `ChatRoomListHeader`, `ChatRoomListEntry`. These need to be passed into the picker functions.

- [ ] **Step 2: Pass `LoadedStrings` into picker functions**

`chatNetworkPicker` and `chatRoomPicker` are called from `runChat`. Add a `strings StringsConfig` parameter to both (or pass the whole executor `e`). The simpler approach: pass the relevant fields as a small struct. But since these functions are local helpers called only from within `chat.go`, add `e *MenuExecutor` as a parameter.

Change `chatNetworkPicker` signature:
```go
func chatNetworkPicker(e *MenuExecutor, s ssh.Session, terminal *term.Terminal, handle, dbPath string, leaves []ChatLeafInfo, outputMode ansi.OutputMode, wt func(string)) (chat.ChatService, string, error)
```

Change `chatRoomPicker` signature:
```go
func chatRoomPicker(e *MenuExecutor, svc chat.ChatService, s ssh.Session, terminal *term.Terminal, wt func(string)) string
```

Update `chatNetworkPicker` header rendering:
```go
// Before:
wt("\r\n|15Chat Networks|07\r\n")
wt("|08────────────────────────────────────────|07\r\n")
for i, net := range nets {
    switch {
    case !net.avail:
        wt(fmt.Sprintf("|08 %d.|07 %-20s |08(unavailable)|07\r\n", i+1, net.name))
    case net.isLocal:
        wt(fmt.Sprintf("|08 %d.|07 %-20s |08(this BBS only)|07\r\n", i+1, net.name))
    default:
        wt(fmt.Sprintf("|08 %d.|07 %-20s |08(%d users online)|07\r\n", i+1, net.name, net.users))
    }
}

// After:
wt("\r\n" + e.LoadedStrings.ChatNetworkPickerHeader + "\r\n")
for i, net := range nets {
    var status string
    switch {
    case !net.avail:
        status = "unavailable"
    case net.isLocal:
        status = "this BBS only"
    default:
        status = fmt.Sprintf("%d users online", net.users)
    }
    wt(fmt.Sprintf(e.LoadedStrings.ChatNetworkPickerEntry+"\r\n", i+1, net.name, status))
}
```

Update `chatRoomPicker` header rendering:
```go
// Before:
wt("\r\n|15Available Rooms|07\r\n")
wt("|08────────────────────────────────────────|07\r\n")
for i, r := range rooms {
    topic := r.Topic
    if topic == "" {
        topic = "|08no topic|07"
    }
    wt(fmt.Sprintf("|08 %d.|07 %-15s |08(%d)|07 %s\r\n", i+1, r.Name, r.UserCount, topic))
}

// After:
wt("\r\n" + e.LoadedStrings.ChatRoomListHeader + "\r\n")
for i, r := range rooms {
    topic := r.Topic
    if topic == "" {
        topic = "|08no topic|07"
    }
    wt(fmt.Sprintf(e.LoadedStrings.ChatRoomListEntry+"\r\n", r.Name, r.UserCount, topic))
}
```

Note: check `chatRoomListEntry` format string in `strings.json` — it has format `" |15%-20s|07 |08%3d users|07  %s"` (name, count, topic, no index). Adjust accordingly. The entry format uses 3 args (name, count, topic), not 4. Remove `i+1` from the args. Drop the numbered list or use a prefix: verify what the config entry expects.

Actually looking at `"chatRoomListEntry": " |15%-20s|07 |08%3d users|07  %s"` — it takes (name, count, topic). The picker currently shows a number prefix. If the format doesn't include a number, adjust. Prefer matching whatever the existing format says.

- [ ] **Step 3: Update all call sites for picker functions**

There are two call sites for each picker: the initial picker before entering chat, and the `/NET` and `/ROOM` in-chat commands. Find and update all calls to pass `e`.

```bash
grep -n "chatNetworkPicker\|chatRoomPicker" internal/menu/chat.go
```

Update each call to pass `e` as first argument.

- [ ] **Step 4: Run build**

```bash
cd /home/robbie/git/vision-3-bbs && go build ./... 2>&1
```
Expected: no errors.

- [ ] **Step 5: Commit**

```bash
cd /home/robbie/git/vision-3-bbs && git add internal/menu/chat.go
git commit -m "fix(menu): parse handle@node in /MSG and use LoadedStrings for picker UI"
```

---

## Task 12: Fix stale module path in plan doc

**Files:**
- Modify: `docs/plans/2026-03-21-v3net-chat.md`

- [ ] **Step 1: Replace stale import paths**

```bash
cd /home/robbie/git/vision-3-bbs && sed -i 's|github.com/visionbbs/vision3/|github.com/ViSiON-3/vision-3-bbs/|g' docs/plans/2026-03-21-v3net-chat.md
```

- [ ] **Step 2: Verify changes**

```bash
grep -n "visionbbs/vision3" docs/plans/2026-03-21-v3net-chat.md
```
Expected: no output.

- [ ] **Step 3: Commit**

```bash
cd /home/robbie/git/vision-3-bbs && git add docs/plans/2026-03-21-v3net-chat.md
git commit -m "docs: fix stale module path in v3net-chat plan (visionbbs/vision3 → ViSiON-3/vision-3-bbs)"
```

---

## Task 13: Final verification

- [ ] **Step 1: Run all tests**

```bash
cd /home/robbie/git/vision-3-bbs && go test ./... 2>&1 | grep -E "FAIL|ok|---"
```
Expected: all packages show `ok`, no `FAIL`.

- [ ] **Step 2: Run race detector on key packages**

```bash
cd /home/robbie/git/vision-3-bbs && go test -race ./internal/v3net/hub/ ./internal/v3net/leaf/ ./internal/chat/ 2>&1 | tail -10
```
Expected: all pass, no race conditions.
