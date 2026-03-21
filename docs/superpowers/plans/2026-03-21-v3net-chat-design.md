# V3Net Chat — Design Specification

**Date:** 2026-03-21
**Status:** Approved for implementation planning

---

## Overview

V3Net Chat is a first-class real-time chat system for Vision/3. It replaces the existing single-room local teleconference (`internal/chat`) with a multi-room, multi-BBS chat backed by the V3Net protocol. When a BBS has V3Net enabled, all users connected to that network — from any BBS — share the same room space. When V3Net is not configured, the system degrades gracefully to a local-only BBS chat.

The feature is inspired by uMRC (Multi-Relay Chat) in functionality: ephemeral named rooms, room topics, private messages, join/leave announcements, and scrollback history. The implementation is native to Vision/3 and V3Net, with no dependency on the MRC protocol.

---

## Goals

- Replace `internal/chat` with a new multi-room chat system
- Network-wide rooms: all BBSes on the same V3Net network share the same room space
- Ephemeral rooms: created on first join, deleted when last user leaves
- Default "lobby" room all users auto-join
- Room topics settable by any user
- Private messages between users on any node on the network
- Scrollback history persisted to SQLite (7-day window)
- Local-only fallback when V3Net is not configured
- Same ANSI split-screen UI for both local and networked modes

## Non-Goals (v1)

- Custom chat nicks / color customization (users appear as their BBS handle)
- Moderation tools (kick, mute, ban)
- Persistent rooms (rooms always ephemeral)
- Per-room access control
- Chat logging beyond scrollback history

---

## Architecture

### Two Modes, One Interface

The chat system is driven by a `ChatService` interface. Mode is selected once at startup: if V3Net is configured and the hub is reachable, a `leaf.ChatSession` satisfies the interface. If V3Net is not configured, a `local.ChatService` does. If V3Net is configured but the hub is unreachable at startup, an error is shown and the BBS falls back to local mode for that session.

```
┌─────────────────────────────────┐
│     internal/menu/chat.go       │  (UI — unchanged shape)
│     works against ChatService   │
└───────────────┬─────────────────┘
                │
        ┌───────┴────────┐
        │                │
┌───────▼──────┐  ┌──────▼──────────────┐
│ local.Chat   │  │ leaf.ChatSession     │
│ Service      │  │ (V3Net-backed)       │
│ (SQLite)     │  │ (hub HTTP + SSE)     │
└──────────────┘  └──────────────────────┘
```

### ChatService Interface

Defined in `internal/chat/chat.go`. Both `local.ChatService` and `leaf.ChatSession` satisfy this interface.

```go
type ChatService interface {
    // Join subscribes the user to a room. Returns current room list and
    // recent history (up to limit messages) for the joined room.
    Join(room string) ([]RoomInfo, []ChatMessage, error)

    // Leave unsubscribes the user from their current room.
    Leave(room string) error

    // Post sends a message to the user's current room.
    Post(room, text string) error

    // Private sends a direct message to a specific user.
    // node is the target's node ID (empty string in local mode).
    Private(handle, node, text string) error

    // SetTopic sets the topic for a room.
    SetTopic(room, topic string) error

    // Rooms returns the current list of active rooms.
    Rooms() ([]RoomInfo, error)

    // History returns up to limit recent messages for a room (max 200).
    History(room string, limit int) ([]ChatMessage, error)

    // Users returns the current list of handles in the active room.
    Users() []string

    // Events returns a channel that receives incoming chat events.
    // The channel is closed when the session ends.
    Events() <-chan ChatEvent

    // Close tears down the session cleanly.
    Close() error
}
```

### Core Types (internal/chat/chat.go)

```go
type RoomInfo struct {
    Name      string
    Topic     string
    UserCount int
}

type ChatMessage struct {
    Room      string
    Handle    string
    Node      string    // empty in local mode
    BBS       string    // empty in local mode
    Text      string
    Timestamp time.Time
    IsSystem  bool
}

// ChatEvent is the sum type delivered on the Events() channel.
type ChatEvent struct {
    Type      ChatEventType
    Message   *ChatMessage   // set for TypeMessage, TypePrivate
    Room      *RoomInfo      // set for TypeRoomUpdate
    Join      *ChatJoin      // set for TypeJoin
    Leave     *ChatLeave     // set for TypeLeave
    Topic     *ChatTopic     // set for TypeTopic
    Reconnect bool           // set for TypeSystem when reconnecting
    Text      string         // set for TypeSystem messages
}

type ChatEventType int
const (
    TypeMessage   ChatEventType = iota
    TypePrivate
    TypeJoin
    TypeLeave
    TypeTopic
    TypeSystem    // reconnecting, errors, announcements
)

type ChatJoin  struct { Room, Handle, BBS string }
type ChatLeave struct { Room, Handle, BBS string }
type ChatTopic struct { Room, Topic, SetBy string }
```

### Package Layout

```
internal/chat/
  chat.go         ChatMessage, RoomInfo, ChatEvent, ChatService interface
  local.go        Local-only implementation (SQLite-backed)

internal/v3net/protocol/
  chat.go         New: wire types for chat (requests, SSE event payloads)

internal/v3net/hub/
  chat.go         New: room registry, presence tracking, history store, HTTP handlers

internal/v3net/leaf/
  chat.go         New: ChatSession — satisfies ChatService, backed by hub

internal/menu/
  chat.go         Rewritten: multi-room UI using ChatService interface
```

---

## Protocol Extensions

### New file: `internal/v3net/protocol/chat.go`

#### Hub-bound request bodies (leaf → hub, all authenticated)

```go
ChatJoinRequest    { Room string `json:"room"` }
ChatLeaveRequest   { Room string `json:"room"` }
ChatPostRequest    { Room string `json:"room"`;  Text string `json:"text"` }
ChatPrivateRequest { ToHandle string `json:"to_handle"`; ToNode string `json:"to_node"`; Text string `json:"text"` }
ChatTopicRequest   { Room string `json:"room"`;  Topic string `json:"topic"` }
```

#### Wire types defined in `internal/v3net/protocol/chat.go`

To avoid import cycles (`protocol` must not import `internal/chat`), the protocol package defines its own wire-level types. The leaf converts between wire types and domain types at the boundary.

```go
// ProtoChatRoomInfo is the wire representation of a room (used in responses and SSE).
ProtoChatRoomInfo struct {
    Name      string `json:"name"`
    Topic     string `json:"topic"`
    UserCount int    `json:"user_count"`
}

// ChatMsgPayload is a single chat message as returned by history and SSE events.
ChatMsgPayload struct {
    Room       string `json:"room"`
    FromHandle string `json:"from_handle"`
    FromNode   string `json:"from_node"`
    FromBBS    string `json:"from_bbs"`
    Text       string `json:"text"`
    Timestamp  string `json:"timestamp"`  // RFC3339
}
```

#### `POST rooms/join` response body

The join response returns the current room list directly in the HTTP response body — no SSE event is used for this. This avoids the awkwardness of a request-response pattern over a push-only stream.

```go
ChatJoinResponse {
    Rooms   []ProtoChatRoomInfo `json:"rooms"`
    History []ChatMsgPayload    `json:"history"`  // up to 50 recent messages
    Users   []string            `json:"users"`    // handles currently in the joined room
}
```

`leaf.ChatSession.Join()` converts `ProtoChatRoomInfo` → `chat.RoomInfo` and `ChatMsgPayload` → `chat.ChatMessage` before returning to the caller. `Users` is stored in `ChatSession.currentUsers`.

#### SSE event payloads (hub → leaf)

All carried as SSE events on the existing `/v3net/v1/{network}/events` stream. The hub broadcasts **all chat events to all connected leaves** for the network; leaf-side filtering (described below) drops irrelevant events.

| Event type      | Payload type (all `protocol/chat.go`)                                         |
|-----------------|--------------------------------------------------------------------------------|
| `chat_message`  | `ChatMsgPayload` — `{room, from_handle, from_node, from_bbs, text, timestamp}` |
| `chat_private`  | `ChatMsgPayload` + `{to_handle, to_node}` — full fields listed above          |
| `chat_join`     | `{room, handle, bbs}`                                                          |
| `chat_leave`    | `{room, handle, bbs}`                                                          |
| `chat_topic`    | `{room, topic, set_by}`                                                        |

Note: `chat_private` carries both `to_handle` and `to_node` so the receiving leaf can filter it. A leaf drops any `chat_private` event where `to_node` does not match its own node ID.

#### Leaf-side filtering

The leaf SSE dispatcher in `leaf/sse.go` applies the following rules before delivering events to a `ChatSession`:

- `chat_message`, `chat_join`, `chat_leave`, `chat_topic`: deliver only to sessions whose `currentRoom` matches the event's `room` field.
- `chat_private`: deliver only to sessions whose `handle` matches `to_handle` AND the leaf's node ID matches `to_node`.
- All other `chat_*` events not listed above: drop silently.

This approach requires no changes to the `Broadcaster` API.

#### Hub endpoints

All under `/v3net/v1/{network}/chat/`. The hub router (`newMux()` in `hub/server.go`) is a single `http.HandlerFunc` closure with a `switch` statement — public endpoints are handled at the top before auth fires. The two `GET` chat endpoints must be added to that top-level `switch` block alongside the existing public cases (`/v3net/v1/networks`, `/v3net/v1/subscribe`, `*/info`, `*/nal`). All `POST` endpoints fall through to the auth middleware as normal.

```
POST  rooms/join              → ChatJoinResponse (room list + history)
POST  rooms/leave             → 204
POST  rooms/post              → 204
POST  rooms/private           → 204
POST  rooms/topic             → 204
GET   rooms                   → []RoomInfo{name, topic, user_count}
GET   rooms/{room}/history    → []ChatMsgPayload  (?limit=N, max 200, default 50)
```

The existing flat `POST /v3net/v1/{network}/chat` endpoint is removed.

### Room Name Rules

- Lowercase alphanumeric and hyphens only
- Max 32 characters
- Spaces auto-converted to hyphens
- "lobby" is reserved as the default room
- Validated client-side before sending; invalid names show an inline error

---

## Hub Implementation

### `internal/v3net/hub/chat.go`

#### In-memory room registry

```go
type roomState struct {
    topic   string
    members map[string][]string  // nodeID (string) → list of handles from that leaf
}

type chatRooms struct {
    mu    sync.RWMutex
    rooms map[string]*roomState  // room name → state
}
```

Rooms are created on first join, deleted when `members` is empty after a leave.

#### SSE disconnect → room cleanup

The `handleEvents` HTTP handler in `hub/handlers.go` is a thin wrapper around `broadcaster.ServeSSE`. The actual `ServeSSE` signature is `ServeSSE(w http.ResponseWriter, r *http.Request, network string)`. The handler is extended to capture `nodeID` before blocking and clean up chat presence after `ServeSSE` returns:

```go
func (h *Hub) handleEvents(w http.ResponseWriter, r *http.Request, network string) {
    nodeID := r.Header.Get(headerNodeID)
    h.broadcaster.ServeSSE(w, r, network)
    // ServeSSE blocks until disconnect; clean up chat presence on return
    h.chatRooms.handleDisconnect(network, nodeID, h.broadcaster)
}
```

`handleDisconnect` removes all of that node's handles from all rooms and broadcasts a `chat_leave` event for each removed handle.

#### SQLite schema (added to existing hub database)

```sql
CREATE TABLE chat_history (
    id          INTEGER PRIMARY KEY,
    network     TEXT NOT NULL,
    room        TEXT NOT NULL,
    from_handle TEXT NOT NULL,
    from_node   TEXT NOT NULL,
    from_bbs    TEXT NOT NULL,
    text        TEXT NOT NULL,
    created_at  DATETIME NOT NULL
);

CREATE TABLE chat_private_history (
    id          INTEGER PRIMARY KEY,
    network     TEXT NOT NULL,
    from_handle TEXT NOT NULL,
    from_node   TEXT NOT NULL,
    to_handle   TEXT NOT NULL,
    to_node     TEXT NOT NULL,
    text        TEXT NOT NULL,
    created_at  DATETIME NOT NULL
);

CREATE INDEX idx_chat_history_room_time ON chat_history(network, room, created_at);
CREATE INDEX idx_chat_private_history_node ON chat_private_history(network, to_node, created_at);
```

History pruned on hub startup and every 24 hours. Default retention: 7 days (configurable).

#### Rate limiting

The existing per-node `chatLimiter` (1 message/second) is extended to cover the new `rooms/post` and `rooms/private` endpoints. `rooms/join`, `rooms/leave`, and `rooms/topic` are not rate-limited (low-frequency, not spam vectors).

#### Routing logic

| Request           | Action                                                                              |
|-------------------|-------------------------------------------------------------------------------------|
| `rooms/join`      | Add leaf+handle to registry; broadcast `chat_join` to all leaves; return `ChatJoinResponse` with room list + history |
| `rooms/leave`     | Remove from registry; broadcast `chat_leave`; delete room if now empty             |
| `rooms/post`      | Rate-limit; validate leaf is joined; write to `chat_history`; broadcast `chat_message` to all leaves |
| `rooms/private`   | Rate-limit; validate target node is a known subscriber; write to `chat_private_history`; broadcast `chat_private` to all leaves (filtered client-side) |
| `rooms/topic`     | Update in-memory topic; broadcast `chat_topic` to all leaves                       |
| SSE disconnect    | `handleEvents` calls `chatRooms.handleDisconnect` after `ServeSSE` returns         |

---

## Leaf Implementation

### `internal/v3net/leaf/chat.go`

`ChatSession` wraps one user's participation in networked chat and satisfies `ChatService`.

```go
type ChatSession struct {
    leaf         *Leaf
    handle       string
    currentRoom  string
    currentUsers []string           // handles in current room, updated from join/leave events
    events       chan chat.ChatEvent // fed by SSE dispatcher
}
```

`currentUsers` is populated from `ChatJoinResponse` on join and updated incrementally as `chat_join` and `chat_leave` events arrive for the current room. The `/users` command in the menu handler calls `session.Users()` which returns a snapshot of this slice.

#### Session registry on the Leaf

`Leaf` gains a `chatSessions` registry:

```go
type chatSessionRegistry struct {
    mu       sync.RWMutex
    sessions map[string]*ChatSession  // handle → session
}
```

- `NewChatSession(handle string) *ChatSession` — creates a session, registers it, returns it.
- `ChatSession.Close()` — deregisters from the registry and closes the events channel.

#### SSE dispatcher extension (`leaf/sse.go`)

The `connectSSE` loop in `leaf/sse.go` calls `l.onEvent(ev)` for every received event. A second call to `leaf.chatSessions.dispatch(ev)` is added immediately after `l.onEvent(ev)` in `sse.go` — it is a second call site at the same point in the loop, not a modification to the `onEvent` callback itself. This keeps the existing `onEvent` path intact for all consumers (JAM polling, presence) while also routing all events through the chat dispatcher.

`chatSessions.dispatch` applies the leaf-side filtering rules (see Protocol section) and writes matching events to the appropriate session's `events` channel. Events are dropped (not queued) if no matching session exists or the channel is full.

#### Reconnect behavior

The existing SSE reconnect loop in `leaf/sse.go` fires a reconnect notification. On reconnect, the leaf's `chatSessions` registry iterates all active sessions and:
1. Re-sends `POST rooms/join` for each session's `currentRoom`.
2. Pushes a `TypeSystem` reconnect event to each session's channel so the UI can show "reconnected".

The menu handler fetches fresh history from the session's `History()` method on reconnect and re-renders the scroll region.

---

## Local Mode Implementation

### `internal/chat/local.go`

Same `ChatService` interface, backed by a local SQLite file in the BBS data directory.

```sql
CREATE TABLE chat_history (
    id          INTEGER PRIMARY KEY,
    room        TEXT NOT NULL,
    from_handle TEXT NOT NULL,
    text        TEXT NOT NULL,
    created_at  DATETIME NOT NULL
);

CREATE TABLE chat_private_history (
    id          INTEGER PRIMARY KEY,
    from_handle TEXT NOT NULL,
    to_handle   TEXT NOT NULL,
    text        TEXT NOT NULL,
    created_at  DATETIME NOT NULL
);
```

Room state is in-memory only (ephemeral on restart). No `from_node`/`from_bbs` columns since it's single-node. The `Private(handle, node, text)` call ignores the `node` parameter in local mode and routes by handle only. Same 7-day pruning, same commands, same UI.

---

## Chat UI

### `internal/menu/chat.go` (rewritten)

Retains the existing ANSI split-screen layout: scroll region for messages, input bar at bottom.

#### Entry flow

1. Connect to `ChatService`
2. Fetch `Rooms()` — display room list with topics and user counts
3. User picks a room or presses Enter to auto-join "lobby"
4. Call `Join(room)` — response includes room list and last 50 messages of history
5. Render history in scroll region, then start live chat loop

#### Chat loop

Two goroutines: receive (reads `Events()` channel, renders to scroll region) and input (reads keystrokes, buffers line). Input is submitted on Enter.

#### Commands

| Command                  | Action                                           |
|--------------------------|--------------------------------------------------|
| `/join <room>`           | Leave current room, join new room, render history |
| `/rooms`                 | Fetch and display room list inline               |
| `/topic <text>`          | Set topic for current room                       |
| `/msg <handle> <text>`   | Send private message                             |
| `/users`                 | List users in current room (from last join/join event state) |
| `/q` or `/quit`          | Leave room and exit chat                         |

#### Private message display

Private messages appear inline in the scroll region with a distinct prefix:
```
*** <fromhandle> → you: message text
```

#### Reconnect display

On `TypeSystem` reconnect event: show "*** reconnecting…" then "*** reconnected" once the SSE loop restores, and re-render the last few history messages.

#### Strings config

New entries in `templates/configs/strings.json`:
```
chatRoomListHeader, chatRoomListEntry, chatPrivateMsgFormat,
chatJoinMsg, chatLeaveMsg, chatTopicMsg, chatReconnecting, chatReconnected
```

---

## Error Handling

| Scenario                          | Behavior                                                                 |
|-----------------------------------|--------------------------------------------------------------------------|
| Hub unreachable at startup        | Show error message; fall back to local mode for this session             |
| SSE drops mid-chat                | Leaf reconnects with existing backoff; show "reconnecting…" inline; re-join room on restore; re-render history |
| Hub rejects post (rate limit etc) | Show inline error in scroll region; do not drop user                     |
| Private message to unknown node   | Hub returns 404; show "user not found" inline                            |
| Invalid room name                 | Validate client-side; show inline error, do not send                     |
| Subscriber channel full           | Drop oldest message (same behavior as current system)                    |

---

## Configuration

No new sysop configuration required for basic operation. Optional hub-level settings (added to existing hub config):

```yaml
chat:
  history_retention_days: 7   # default
```

---

## Migration

`internal/chat/room.go` is deleted. The `ChatRoom` type and `CHAT` menu command wiring in `cmd/vision3/main.go` are updated to construct a `ChatService` (local or networked) and pass it to the menu executor. The menu command key (`CHAT`) remains the same — no sysop reconfiguration needed.
