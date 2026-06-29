# WFC Sysop Console Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build `wfc`, a read-only "Waiting For Caller" sysop console that connects to a running ViSiON/3 daemon over an SSH subsystem channel and renders live node/event state.

**Architecture:** A transport-agnostic Bubble Tea TUI (`internal/wfcui`) renders against an `admin.AdminClient` interface. The daemon hosts an `admin.Server` that reads `SessionRegistry` and produces snapshots + diff-synthesized events. Two clients satisfy the interface: an in-process client (daemon-embedded) and an SSH-channel client (`cmd/wfc`). Auth is per-user SSH public key, additive to the existing SSH server.

**Tech Stack:** Go, `gliderlabs/ssh` (server), `golang.org/x/crypto/ssh` + `.../knownhosts` (client), Bubble Tea + Lip Gloss (TUI), stdlib `encoding/json`, `log/slog`.

## Global Constraints

- Module path: `github.com/ViSiON-3/vision-3-bbs`.
- Pure Go, **no CGO**. Must `go build` for: `windows/386`, `windows/amd64`, `linux/amd64`, `linux/arm64`, `darwin/amd64`, `darwin/amd64`.
- Logging: stdlib `log/slog` only. Audit every admin session open/close and command.
- TUI stack: `github.com/charmbracelet/bubbletea` + `lipgloss` (already in `go.mod`). Do not add other TUI libs.
- Keep files under 300 lines; Go file names `snake_case`.
- `internal/wfcui` imports `internal/admin` (interface + types) ONLY — never `internal/session`, `internal/menu`, or daemon internals.
- `internal/admin/server.go` is the only WFC code that touches `SessionRegistry`, read-only, under the existing `BbsSession.Mutex` RLock.
- v1 is **read-only**: the only implemented command is `system.refresh`. No mutations.
- Run `gofmt -w`, `go vet`, and the package's `go test` before each commit.

Spec: `docs/superpowers/specs/2026-06-29-wfc-sysop-console-design.md`.

---

### Task 1: Admin contract — types + `AdminClient` interface

**Files:**
- Create: `internal/admin/client.go`
- Test: `internal/admin/client_test.go`

**Interfaces:**
- Produces: all shared types (`NodeStatus`, `NodeState`, `Counters`, `SystemSnapshot`, `EventType`, `Event`, `CommandType`, `AdminCommand`, `Result`) and the `AdminClient` interface consumed by every later task.

- [ ] **Step 1: Write the failing test**

```go
package admin

import (
	"encoding/json"
	"testing"
	"time"
)

func TestSnapshotJSONRoundTrip(t *testing.T) {
	in := SystemSnapshot{
		Time:       time.Unix(1700000000, 0).UTC(),
		SystemName: "The Misfit Node",
		UptimeSecs: 12345,
		Nodes: []NodeState{{
			NodeID: 1, Status: StatusOnline, Handle: "RobbieW",
			UserID: 7, AccessLevel: 255, RemoteAddr: "1.2.3.4:55",
			CurrentMenu: "MAIN", Activity: "Reading messages",
			TimeLeftMins: 42,
		}},
		Counters: Counters{ActiveNodes: 1, CallsToday: 14},
	}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out SystemSnapshot
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.SystemName != in.SystemName || len(out.Nodes) != 1 || out.Nodes[0].Handle != "RobbieW" {
		t.Fatalf("round trip mismatch: %+v", out)
	}
	if out.Counters.CallsToday != 14 {
		t.Fatalf("counters lost: %+v", out.Counters)
	}
}

func TestEventJSONRoundTrip(t *testing.T) {
	in := Event{Time: time.Unix(1700000000, 0).UTC(), Type: EventMenuChanged, NodeID: 2, Handle: "GUEST", Message: "DOORS"}
	b, _ := json.Marshal(in)
	var out Event
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.Type != EventMenuChanged || out.Message != "DOORS" {
		t.Fatalf("mismatch: %+v", out)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/admin/ -run TestSnapshotJSONRoundTrip -v`
Expected: FAIL — package/types do not compile (undefined: `SystemSnapshot`).

- [ ] **Step 3: Write the implementation**

```go
// Package admin defines the WFC admin contract (data types and the
// AdminClient interface) plus the daemon-side server and client
// implementations. Types here are serialization-safe projections of live
// session state — no net.Conn, mutex, or channel fields.
package admin

import (
	"context"
	"time"
)

// NodeStatus is a coarse, display-oriented status derived from session state.
type NodeStatus string

const (
	NodeStatusIdle   NodeStatus = "idle"
	StatusLogin      NodeStatus = "login"
	StatusOnline     NodeStatus = "online"
	StatusInMenu     NodeStatus = "menu"
	StatusInChat     NodeStatus = "chat"
)

// NodeState is one node/caller row in a snapshot.
type NodeState struct {
	NodeID       int        `json:"nodeId"`
	Status       NodeStatus `json:"status"`
	Handle       string     `json:"handle"`
	UserID       int        `json:"userId"`
	AccessLevel  int        `json:"accessLevel"`
	RemoteAddr   string     `json:"remoteAddr"`
	CurrentMenu  string     `json:"currentMenu"`
	Activity     string     `json:"activity"`
	Invisible    bool       `json:"invisible"`
	ConnectedAt  time.Time  `json:"connectedAt"`
	LastActivity time.Time  `json:"lastActivity"`
	TimeLeftMins int        `json:"timeLeftMins"` // best-effort; -1 if unknown
}

// Counters holds header counters populated only from existing data sources.
type Counters struct {
	ActiveNodes int `json:"activeNodes"`
	CallsToday  int `json:"callsToday"` // -1 if unavailable
}

// SystemSnapshot is a point-in-time view of the whole system.
type SystemSnapshot struct {
	Time       time.Time   `json:"time"`
	SystemName string      `json:"systemName"`
	UptimeSecs int64       `json:"uptimeSecs"`
	Nodes      []NodeState `json:"nodes"`
	Counters   Counters    `json:"counters"`
}

// EventType enumerates diff-synthesized event kinds.
type EventType string

const (
	EventCallerConnected    EventType = "caller.connected"
	EventCallerDisconnected EventType = "caller.disconnected"
	EventMenuChanged        EventType = "menu.changed"
	EventActivityChanged    EventType = "activity.changed"
)

// Event is a single entry in the live event feed.
type Event struct {
	Time    time.Time `json:"time"`
	Type    EventType `json:"type"`
	NodeID  int       `json:"nodeId"`
	Handle  string    `json:"handle"`
	Message string    `json:"message"`
}

// CommandType enumerates admin commands. v1 implements only CommandRefresh.
type CommandType string

const (
	CommandRefresh CommandType = "system.refresh"
)

// AdminCommand is a request to the server to perform an action.
type AdminCommand struct {
	Command CommandType    `json:"command"`
	NodeID  int            `json:"nodeId,omitempty"`
	Payload map[string]any `json:"payload,omitempty"`
}

// Result is the outcome of an AdminCommand.
type Result struct {
	OK      bool   `json:"ok"`
	Message string `json:"message,omitempty"`
}

// AdminClient is the transport-agnostic contract the TUI consumes.
type AdminClient interface {
	Snapshot(ctx context.Context) (*SystemSnapshot, error)
	Subscribe(ctx context.Context) (<-chan Event, error)
	Execute(ctx context.Context, cmd AdminCommand) (*Result, error)
	Close() error
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/admin/ -v`
Expected: PASS (both round-trip tests).

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/admin/
go vet ./internal/admin/
git add internal/admin/client.go internal/admin/client_test.go
git commit -m "feat(admin): add WFC admin types and AdminClient interface"
```

---

### Task 2: Snapshot builder

**Files:**
- Create: `internal/admin/snapshot.go`
- Test: `internal/admin/snapshot_test.go`

**Interfaces:**
- Consumes: `session.BbsSession` (fields: `NodeID`, `User`, `CurrentMenu`, `Activity`, `Invisible`, `StartTime`, `LastActivity`, `RemoteAddr`, `Mutex`); `user.User` (`Handle`, `ID`, `AccessLevel`, `TimeLimit`).
- Produces: `type RegistrySource interface { ListActive() []*session.BbsSession }` and `func BuildSnapshot(reg RegistrySource, systemName string, startedAt, now time.Time, callsToday int) *SystemSnapshot`. `*session.SessionRegistry` already satisfies `RegistrySource`.

- [ ] **Step 1: Write the failing test**

```go
package admin

import (
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/session"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// fakeRegistry implements RegistrySource for tests.
type fakeRegistry struct{ sessions []*session.BbsSession }

func (f *fakeRegistry) ListActive() []*session.BbsSession { return f.sessions }

func TestBuildSnapshotMapsFields(t *testing.T) {
	start := time.Unix(1700000000, 0).UTC()
	now := start.Add(10 * time.Minute)
	reg := &fakeRegistry{sessions: []*session.BbsSession{
		{NodeID: 1, User: &user.User{Handle: "RobbieW", ID: 7, AccessLevel: 255, TimeLimit: 60},
			CurrentMenu: "MAIN", Activity: "Reading messages", StartTime: start, LastActivity: now},
		{NodeID: 2, User: nil, CurrentMenu: "", StartTime: now}, // pre-auth
	}}

	snap := BuildSnapshot(reg, "Test BBS", start, now, 14)

	if snap.SystemName != "Test BBS" || snap.UptimeSecs != 600 {
		t.Fatalf("header wrong: %+v", snap)
	}
	if len(snap.Nodes) != 2 || snap.Counters.ActiveNodes != 2 || snap.Counters.CallsToday != 14 {
		t.Fatalf("nodes/counters wrong: %+v", snap)
	}
	n1 := snap.Nodes[0]
	if n1.Handle != "RobbieW" || n1.Status != StatusOnline || n1.TimeLeftMins != 50 {
		t.Fatalf("node1 wrong: %+v", n1)
	}
	n2 := snap.Nodes[1]
	if n2.Status != StatusLogin || n2.TimeLeftMins != -1 {
		t.Fatalf("node2 (pre-auth) wrong: %+v", n2)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/admin/ -run TestBuildSnapshot -v`
Expected: FAIL (undefined: `BuildSnapshot`).

- [ ] **Step 3: Write the implementation**

```go
package admin

import (
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/session"
)

// RegistrySource is the read-only view of active sessions the builder needs.
// *session.SessionRegistry satisfies it via ListActive().
type RegistrySource interface {
	ListActive() []*session.BbsSession
}

// BuildSnapshot copies live session state into a serialization-safe snapshot.
// It reads each session under its RLock. callsToday may be -1 if unavailable.
func BuildSnapshot(reg RegistrySource, systemName string, startedAt, now time.Time, callsToday int) *SystemSnapshot {
	sessions := reg.ListActive()
	nodes := make([]NodeState, 0, len(sessions))
	for _, s := range sessions {
		s.Mutex.RLock()
		ns := NodeState{
			NodeID:       s.NodeID,
			CurrentMenu:  s.CurrentMenu,
			Activity:     s.Activity,
			Invisible:    s.Invisible,
			ConnectedAt:  s.StartTime,
			LastActivity: s.LastActivity,
			TimeLeftMins: -1,
		}
		if s.RemoteAddr != nil {
			ns.RemoteAddr = s.RemoteAddr.String()
		}
		if s.User != nil {
			ns.Handle = s.User.Handle
			ns.UserID = s.User.ID
			ns.AccessLevel = s.User.AccessLevel
			ns.Status = StatusOnline
			if s.Activity == "" && s.CurrentMenu != "" {
				ns.Status = StatusInMenu
			}
			if s.User.TimeLimit > 0 && !s.StartTime.IsZero() {
				used := int(now.Sub(s.StartTime).Minutes())
				left := s.User.TimeLimit - used
				if left < 0 {
					left = 0
				}
				ns.TimeLeftMins = left
			}
		} else {
			ns.Status = StatusLogin
		}
		s.Mutex.RUnlock()
		nodes = append(nodes, ns)
	}
	return &SystemSnapshot{
		Time:       now,
		SystemName: systemName,
		UptimeSecs: int64(now.Sub(startedAt).Seconds()),
		Nodes:      nodes,
		Counters:   Counters{ActiveNodes: len(nodes), CallsToday: callsToday},
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/admin/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/admin/
go vet ./internal/admin/
git add internal/admin/snapshot.go internal/admin/snapshot_test.go
git commit -m "feat(admin): build serialization-safe snapshots from SessionRegistry"
```

---

### Task 3: Diff engine (snapshot → events)

**Files:**
- Create: `internal/admin/diff.go`
- Test: `internal/admin/diff_test.go`

**Interfaces:**
- Produces: `func DiffSnapshots(prev, cur *SystemSnapshot) []Event`. `prev` may be nil (first poll → no events).

- [ ] **Step 1: Write the failing test**

```go
package admin

import (
	"testing"
	"time"
)

func node(id int, handle, menu, activity string) NodeState {
	return NodeState{NodeID: id, Handle: handle, CurrentMenu: menu, Activity: activity}
}

func TestDiffSnapshots(t *testing.T) {
	now := time.Unix(1700000000, 0).UTC()
	prev := &SystemSnapshot{Nodes: []NodeState{node(1, "Ann", "MAIN", "idle")}}
	cur := &SystemSnapshot{Time: now, Nodes: []NodeState{
		node(1, "Ann", "DOORS", "LORD"), // menu + activity changed
		node(2, "Bob", "LOGIN", ""),      // connected
	}}

	events := DiffSnapshots(prev, cur)

	types := map[EventType]int{}
	for _, e := range events {
		types[e.Type]++
	}
	if types[EventMenuChanged] != 1 || types[EventActivityChanged] != 1 || types[EventCallerConnected] != 1 {
		t.Fatalf("unexpected events: %+v", events)
	}

	// Node 1 disconnects on the next diff.
	cur2 := &SystemSnapshot{Time: now, Nodes: []NodeState{node(2, "Bob", "LOGIN", "")}}
	ev2 := DiffSnapshots(cur, cur2)
	if len(ev2) != 1 || ev2[0].Type != EventCallerDisconnected || ev2[0].NodeID != 1 {
		t.Fatalf("expected one disconnect for node 1: %+v", ev2)
	}
}

func TestDiffNilPrevNoEvents(t *testing.T) {
	cur := &SystemSnapshot{Nodes: []NodeState{node(1, "Ann", "MAIN", "")}}
	if got := DiffSnapshots(nil, cur); len(got) != 0 {
		t.Fatalf("expected no events for nil prev, got %+v", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/admin/ -run TestDiff -v`
Expected: FAIL (undefined: `DiffSnapshots`).

- [ ] **Step 3: Write the implementation**

```go
package admin

// DiffSnapshots synthesizes events by comparing the previous snapshot to the
// current one. A nil prev (first poll) yields no events.
func DiffSnapshots(prev, cur *SystemSnapshot) []Event {
	var events []Event
	prevByNode := make(map[int]NodeState)
	if prev != nil {
		for _, n := range prev.Nodes {
			prevByNode[n.NodeID] = n
		}
	}
	curByNode := make(map[int]NodeState, len(cur.Nodes))
	for _, n := range cur.Nodes {
		curByNode[n.NodeID] = n
		old, existed := prevByNode[n.NodeID]
		if !existed {
			if prev == nil {
				continue // first poll: seed state, do not emit
			}
			events = append(events, Event{Time: cur.Time, Type: EventCallerConnected, NodeID: n.NodeID, Handle: n.Handle, Message: "connected"})
			continue
		}
		if old.CurrentMenu != n.CurrentMenu {
			events = append(events, Event{Time: cur.Time, Type: EventMenuChanged, NodeID: n.NodeID, Handle: n.Handle, Message: n.CurrentMenu})
		}
		if old.Activity != n.Activity {
			events = append(events, Event{Time: cur.Time, Type: EventActivityChanged, NodeID: n.NodeID, Handle: n.Handle, Message: n.Activity})
		}
	}
	for id, old := range prevByNode {
		if _, ok := curByNode[id]; !ok {
			events = append(events, Event{Time: cur.Time, Type: EventCallerDisconnected, NodeID: id, Handle: old.Handle, Message: "disconnected"})
		}
	}
	return events
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/admin/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/admin/
go vet ./internal/admin/
git add internal/admin/diff.go internal/admin/diff_test.go
git commit -m "feat(admin): synthesize events by diffing consecutive snapshots"
```

---

### Task 4: Admin `Server` — polling, ring buffer, fan-out

**Files:**
- Create: `internal/admin/server.go`
- Test: `internal/admin/server_test.go`

**Interfaces:**
- Consumes: `RegistrySource`, `BuildSnapshot`, `DiffSnapshots`.
- Produces:
  - `type ServerConfig struct { Reg RegistrySource; SystemName string; StartedAt time.Time; Refresh time.Duration; MaxEvents int; CallsToday func() int }`
  - `func NewServer(cfg ServerConfig) *Server`
  - `func (*Server) Run(ctx context.Context)` — polls until ctx done.
  - `func (*Server) Snapshot() *SystemSnapshot`
  - `func (*Server) Subscribe(ctx context.Context) <-chan Event` — buffered, closed on ctx done; replays current ring buffer first.
  - `func (*Server) Execute(cmd AdminCommand) (*Result, error)`

- [ ] **Step 1: Write the failing test**

```go
package admin

import (
	"context"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/session"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

func TestServerPollProducesSnapshotAndEvents(t *testing.T) {
	reg := &fakeRegistry{}
	srv := NewServer(ServerConfig{
		Reg: reg, SystemName: "T", StartedAt: time.Now(),
		Refresh: time.Hour, MaxEvents: 10, CallsToday: func() int { return -1 },
	})

	// Manual tick API for deterministic tests.
	reg.sessions = []*session.BbsSession{{NodeID: 1, User: &user.User{Handle: "A"}, CurrentMenu: "MAIN"}}
	srv.tick(time.Now()) // first tick: seed, no events
	reg.sessions[0].CurrentMenu = "DOORS"
	srv.tick(time.Now()) // menu change → one event

	snap := srv.Snapshot()
	if snap == nil || len(snap.Nodes) != 1 || snap.Nodes[0].CurrentMenu != "DOORS" {
		t.Fatalf("snapshot wrong: %+v", snap)
	}

	ch := srv.Subscribe(context.Background())
	select {
	case e := <-ch:
		if e.Type != EventMenuChanged {
			t.Fatalf("expected replayed menu.changed, got %+v", e)
		}
	case <-time.After(time.Second):
		t.Fatal("expected a replayed event from ring buffer")
	}
}

func TestServerExecuteRefreshOnly(t *testing.T) {
	srv := NewServer(ServerConfig{Reg: &fakeRegistry{}, MaxEvents: 4, CallsToday: func() int { return -1 }})
	if r, err := srv.Execute(AdminCommand{Command: CommandRefresh}); err != nil || !r.OK {
		t.Fatalf("refresh should succeed: %v %+v", err, r)
	}
	if _, err := srv.Execute(AdminCommand{Command: "node.disconnect"}); err == nil {
		t.Fatal("non-refresh command must be rejected in v1")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/admin/ -run TestServer -v`
Expected: FAIL (undefined: `NewServer`).

- [ ] **Step 3: Write the implementation**

```go
package admin

import (
	"context"
	"errors"
	"sync"
	"time"
)

// ServerConfig configures an admin Server.
type ServerConfig struct {
	Reg        RegistrySource
	SystemName string
	StartedAt  time.Time
	Refresh    time.Duration
	MaxEvents  int
	CallsToday func() int // returns -1 if unavailable; may be nil
}

// Server polls SessionRegistry, keeps the latest snapshot, and fans out
// diff-synthesized events to subscribers. Read-only; v1 implements no mutations.
type Server struct {
	cfg  ServerConfig
	mu   sync.RWMutex
	prev *SystemSnapshot
	ring []Event
	subs map[chan Event]struct{}
}

// NewServer creates a Server. Call Run to start polling, or tick() in tests.
func NewServer(cfg ServerConfig) *Server {
	if cfg.MaxEvents <= 0 {
		cfg.MaxEvents = 200
	}
	if cfg.Refresh <= 0 {
		cfg.Refresh = time.Second
	}
	return &Server{cfg: cfg, subs: make(map[chan Event]struct{})}
}

// Run polls until ctx is cancelled.
func (s *Server) Run(ctx context.Context) {
	t := time.NewTicker(s.cfg.Refresh)
	defer t.Stop()
	s.tick(time.Now())
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			s.tick(now)
		}
	}
}

// tick builds a snapshot, diffs it, stores events, and fans them out.
func (s *Server) tick(now time.Time) {
	calls := -1
	if s.cfg.CallsToday != nil {
		calls = s.cfg.CallsToday()
	}
	snap := BuildSnapshot(s.cfg.Reg, s.cfg.SystemName, s.cfg.StartedAt, now, calls)

	s.mu.Lock()
	events := DiffSnapshots(s.prev, snap)
	s.prev = snap
	for _, e := range events {
		s.ring = append(s.ring, e)
		if len(s.ring) > s.cfg.MaxEvents {
			s.ring = s.ring[len(s.ring)-s.cfg.MaxEvents:]
		}
	}
	subs := make([]chan Event, 0, len(s.subs))
	for c := range s.subs {
		subs = append(subs, c)
	}
	s.mu.Unlock()

	for _, e := range events {
		for _, c := range subs {
			select {
			case c <- e:
			default: // drop for slow subscribers; ring buffer holds history
			}
		}
	}
}

// Snapshot returns the most recent snapshot (nil before the first tick).
func (s *Server) Snapshot() *SystemSnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.prev
}

// Subscribe returns a channel that first replays the current ring buffer and
// then receives live events until ctx is cancelled.
func (s *Server) Subscribe(ctx context.Context) <-chan Event {
	ch := make(chan Event, 256)
	s.mu.Lock()
	for _, e := range s.ring {
		select {
		case ch <- e:
		default:
		}
	}
	s.subs[ch] = struct{}{}
	s.mu.Unlock()

	go func() {
		<-ctx.Done()
		s.mu.Lock()
		delete(s.subs, ch)
		s.mu.Unlock()
		close(ch)
	}()
	return ch
}

// Execute runs an admin command. v1 supports only CommandRefresh.
func (s *Server) Execute(cmd AdminCommand) (*Result, error) {
	switch cmd.Command {
	case CommandRefresh:
		s.tick(time.Now())
		return &Result{OK: true}, nil
	default:
		return nil, errors.New("admin: command not supported in read-only v1: " + string(cmd.Command))
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/admin/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/admin/
go vet ./internal/admin/
git add internal/admin/server.go internal/admin/server_test.go
git commit -m "feat(admin): polling server with event ring buffer and fan-out"
```

---

### Task 5: `InProcessClient`

**Files:**
- Create: `internal/admin/client_inproc.go`
- Test: `internal/admin/client_inproc_test.go`

**Interfaces:**
- Consumes: `*Server`.
- Produces: `func NewInProcessClient(srv *Server) *InProcessClient` implementing `AdminClient`.

- [ ] **Step 1: Write the failing test**

```go
package admin

import (
	"context"
	"testing"
	"time"
)

func TestInProcessClientImplementsAdminClient(t *testing.T) {
	srv := NewServer(ServerConfig{Reg: &fakeRegistry{}, SystemName: "T", StartedAt: time.Now(), MaxEvents: 4, CallsToday: func() int { return -1 }})
	srv.tick(time.Now())
	var c AdminClient = NewInProcessClient(srv)
	defer c.Close()

	snap, err := c.Snapshot(context.Background())
	if err != nil || snap == nil || snap.SystemName != "T" {
		t.Fatalf("snapshot: %v %+v", err, snap)
	}
	if _, err := c.Subscribe(context.Background()); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	if r, err := c.Execute(context.Background(), AdminCommand{Command: CommandRefresh}); err != nil || !r.OK {
		t.Fatalf("execute: %v %+v", err, r)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/admin/ -run TestInProcess -v`
Expected: FAIL (undefined: `NewInProcessClient`).

- [ ] **Step 3: Write the implementation**

```go
package admin

import "context"

// InProcessClient adapts a *Server to AdminClient for daemon-embedded use
// (the --wfc local console and the server-side SSH admin TUI). No serialization.
type InProcessClient struct{ srv *Server }

// NewInProcessClient wraps a running Server.
func NewInProcessClient(srv *Server) *InProcessClient { return &InProcessClient{srv: srv} }

func (c *InProcessClient) Snapshot(ctx context.Context) (*SystemSnapshot, error) {
	if snap := c.srv.Snapshot(); snap != nil {
		return snap, nil
	}
	c.srv.tick(timeNow())
	return c.srv.Snapshot(), nil
}

func (c *InProcessClient) Subscribe(ctx context.Context) (<-chan Event, error) {
	return c.srv.Subscribe(ctx), nil
}

func (c *InProcessClient) Execute(ctx context.Context, cmd AdminCommand) (*Result, error) {
	return c.srv.Execute(cmd)
}

func (c *InProcessClient) Close() error { return nil }
```

Add to `server.go` a tiny indirection so tests stay deterministic and the file builds:

```go
// timeNow is overridable in tests; defaults to time.Now.
var timeNow = time.Now
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/admin/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/admin/
go vet ./internal/admin/
git add internal/admin/client_inproc.go internal/admin/client_inproc_test.go internal/admin/server.go
git commit -m "feat(admin): in-process AdminClient over Server"
```

---

### Task 6: Wire framing (length-prefixed JSON)

**Files:**
- Create: `internal/admin/wire.go`
- Test: `internal/admin/wire_test.go`

**Interfaces:**
- Produces a small framed-message protocol used by both the SSH subsystem server and `SSHChannelClient`:
  - `type Frame struct { Kind string \`json:"kind"\`; Snapshot *SystemSnapshot \`json:"snapshot,omitempty"\`; Event *Event \`json:"event,omitempty"\`; Command *AdminCommand \`json:"command,omitempty"\`; Result *Result \`json:"result,omitempty"\`; Err string \`json:"err,omitempty"\` }`
  - Frame kinds (consts): `KindHello`, `KindSnapshot`, `KindEvent`, `KindCommand`, `KindResult`, `KindError`.
  - `func WriteFrame(w io.Writer, f *Frame) error` — writes 4-byte big-endian length prefix + JSON.
  - `func ReadFrame(r io.Reader) (*Frame, error)` — reads one frame; caps payload at 1 MiB.

- [ ] **Step 1: Write the failing test**

```go
package admin

import (
	"bytes"
	"testing"
	"time"
)

func TestWireRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	out := []*Frame{
		{Kind: KindSnapshot, Snapshot: &SystemSnapshot{SystemName: "T", Time: time.Unix(1, 0).UTC()}},
		{Kind: KindEvent, Event: &Event{Type: EventMenuChanged, NodeID: 3, Message: "X"}},
		{Kind: KindResult, Result: &Result{OK: true}},
	}
	for _, f := range out {
		if err := WriteFrame(&buf, f); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	for i := range out {
		got, err := ReadFrame(&buf)
		if err != nil {
			t.Fatalf("read %d: %v", i, err)
		}
		if got.Kind != out[i].Kind {
			t.Fatalf("frame %d kind mismatch: %q vs %q", i, got.Kind, out[i].Kind)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/admin/ -run TestWire -v`
Expected: FAIL (undefined: `WriteFrame`).

- [ ] **Step 3: Write the implementation**

```go
package admin

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
)

// Frame is one message on the admin RPC channel.
type Frame struct {
	Kind     string          `json:"kind"`
	Snapshot *SystemSnapshot `json:"snapshot,omitempty"`
	Event    *Event          `json:"event,omitempty"`
	Command  *AdminCommand   `json:"command,omitempty"`
	Result   *Result         `json:"result,omitempty"`
	Err      string          `json:"err,omitempty"`
}

const (
	KindHello    = "hello"
	KindSnapshot = "snapshot"
	KindEvent    = "event"
	KindCommand  = "command"
	KindResult   = "result"
	KindError    = "error"
)

const maxFrameBytes = 1 << 20 // 1 MiB

// WriteFrame writes a length-prefixed JSON frame.
func WriteFrame(w io.Writer, f *Frame) error {
	payload, err := json.Marshal(f)
	if err != nil {
		return err
	}
	if len(payload) > maxFrameBytes {
		return fmt.Errorf("admin: frame too large: %d bytes", len(payload))
	}
	var hdr [4]byte
	binary.BigEndian.PutUint32(hdr[:], uint32(len(payload)))
	if _, err := w.Write(hdr[:]); err != nil {
		return err
	}
	_, err = w.Write(payload)
	return err
}

// ReadFrame reads one length-prefixed JSON frame.
func ReadFrame(r io.Reader) (*Frame, error) {
	var hdr [4]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return nil, err
	}
	n := binary.BigEndian.Uint32(hdr[:])
	if n > maxFrameBytes {
		return nil, fmt.Errorf("admin: frame too large: %d bytes", n)
	}
	payload := make([]byte, n)
	if _, err := io.ReadFull(r, payload); err != nil {
		return nil, err
	}
	var f Frame
	if err := json.Unmarshal(payload, &f); err != nil {
		return nil, err
	}
	return &f, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/admin/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/admin/
go vet ./internal/admin/
git add internal/admin/wire.go internal/admin/wire_test.go
git commit -m "feat(admin): length-prefixed JSON frame protocol"
```

---

### Task 7: RPC serve loop + remote AdminClient over a stream

**Files:**
- Create: `internal/admin/rpc.go`
- Test: `internal/admin/rpc_test.go`

**Interfaces:**
- Consumes: `Frame`/`ReadFrame`/`WriteFrame`, `*Server`.
- Produces (both sides driven over any `io.ReadWriteCloser`, so they're testable with `net.Pipe`):
  - `func ServeRPC(ctx context.Context, rw io.ReadWriter, srv *Server, audit func(string)) error` — server side: sends an initial snapshot, streams events, and answers commands.
  - `type StreamClient struct{...}` with `func NewStreamClient(rwc io.ReadWriteCloser) *StreamClient` implementing `AdminClient`. This is the shared client engine the SSH client wraps.

- [ ] **Step 1: Write the failing test**

```go
package admin

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/session"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

func TestRPCStreamClientServer(t *testing.T) {
	reg := &fakeRegistry{sessions: []*session.BbsSession{
		{NodeID: 1, User: &user.User{Handle: "A"}, CurrentMenu: "MAIN"},
	}}
	srv := NewServer(ServerConfig{Reg: reg, SystemName: "T", StartedAt: time.Now(), Refresh: 20 * time.Millisecond, MaxEvents: 8, CallsToday: func() int { return -1 }})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go srv.Run(ctx)

	cliConn, srvConn := net.Pipe()
	go ServeRPC(ctx, srvConn, srv, func(string) {})

	var c AdminClient = NewStreamClient(cliConn)
	defer c.Close()

	snap, err := c.Snapshot(ctx)
	if err != nil || snap == nil || snap.SystemName != "T" {
		t.Fatalf("snapshot over RPC: %v %+v", err, snap)
	}

	events, err := c.Subscribe(ctx)
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	// Trigger a change and expect an event to arrive.
	reg.sessions[0].CurrentMenu = "DOORS"
	select {
	case e := <-events:
		_ = e // any event proves the stream works
	case <-time.After(2 * time.Second):
		t.Fatal("expected an event over the RPC stream")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/admin/ -run TestRPC -v`
Expected: FAIL (undefined: `ServeRPC`).

- [ ] **Step 3: Write the implementation**

```go
package admin

import (
	"context"
	"io"
	"sync"
)

// ServeRPC runs the server side of the admin protocol over rw. It sends an
// initial snapshot, then concurrently streams events and answers commands
// until ctx is cancelled or the stream errors. audit, if non-nil, is called
// with a short description of each command for slog auditing.
func ServeRPC(ctx context.Context, rw io.ReadWriter, srv *Server, audit func(string)) error {
	var writeMu sync.Mutex
	write := func(f *Frame) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		return WriteFrame(rw, f)
	}

	if err := write(&Frame{Kind: KindSnapshot, Snapshot: srv.Snapshot()}); err != nil {
		return err
	}

	subCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	events := srv.Subscribe(subCtx)
	go func() {
		for e := range events {
			ev := e
			if err := write(&Frame{Kind: KindEvent, Event: &ev}); err != nil {
				cancel()
				return
			}
		}
	}()

	for {
		f, err := ReadFrame(rw)
		if err != nil {
			return err
		}
		if f.Kind != KindCommand || f.Command == nil {
			continue
		}
		if audit != nil {
			audit(string(f.Command.Command))
		}
		res, err := srv.Execute(*f.Command)
		out := &Frame{Kind: KindResult}
		if err != nil {
			out.Kind = KindError
			out.Err = err.Error()
		} else {
			out.Result = res
		}
		if werr := write(out); werr != nil {
			return werr
		}
	}
}

// StreamClient is the client engine for the admin protocol over a stream.
// SSHChannelClient wraps it around an SSH channel.
type StreamClient struct {
	rwc     io.ReadWriteCloser
	mu      sync.Mutex
	snap    *SystemSnapshot
	results chan *Frame
	events  chan Event
	closeFn func() error
	once    sync.Once
}

// NewStreamClient starts the read loop over rwc.
func NewStreamClient(rwc io.ReadWriteCloser) *StreamClient {
	c := &StreamClient{
		rwc:     rwc,
		results: make(chan *Frame, 4),
		events:  make(chan Event, 256),
		closeFn: rwc.Close,
	}
	go c.readLoop()
	return c
}

func (c *StreamClient) readLoop() {
	for {
		f, err := ReadFrame(c.rwc)
		if err != nil {
			close(c.events)
			return
		}
		switch f.Kind {
		case KindSnapshot:
			c.mu.Lock()
			c.snap = f.Snapshot
			c.mu.Unlock()
		case KindEvent:
			if f.Event != nil {
				select {
				case c.events <- *f.Event:
				default:
				}
			}
		case KindResult, KindError:
			select {
			case c.results <- f:
			default:
			}
		}
	}
}

func (c *StreamClient) Snapshot(ctx context.Context) (*SystemSnapshot, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.snap, nil
}

func (c *StreamClient) Subscribe(ctx context.Context) (<-chan Event, error) {
	return c.events, nil
}

func (c *StreamClient) Execute(ctx context.Context, cmd AdminCommand) (*Result, error) {
	if err := WriteFrame(c.rwc, &Frame{Kind: KindCommand, Command: &cmd}); err != nil {
		return nil, err
	}
	select {
	case f := <-c.results:
		if f.Kind == KindError {
			return nil, errFromString(f.Err)
		}
		return f.Result, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (c *StreamClient) Close() error {
	var err error
	c.once.Do(func() { err = c.closeFn() })
	return err
}

func errFromString(s string) error { return &rpcError{s} }

type rpcError struct{ msg string }

func (e *rpcError) Error() string { return e.msg }
```

Note: the server's initial-snapshot send relies on `srv.Snapshot()` being non-nil; `srv.Run` ticks once immediately, and `Snapshot()`'s caller in the client tolerates nil. To be safe in tests, `srv.Run` is started before `ServeRPC` (the test does this).

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/admin/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/admin/
go vet ./internal/admin/
git add internal/admin/rpc.go internal/admin/rpc_test.go
git commit -m "feat(admin): RPC serve loop and streaming AdminClient engine"
```

---

### Task 8: `User.PublicKeys` + key lookup

**Files:**
- Modify: `internal/user/user.go` (add field to the `User` struct, after `Flags`)
- Modify: `internal/user/manager.go` (add `FindByAuthorizedKey`)
- Test: `internal/user/pubkey_test.go`

**Interfaces:**
- Produces: `User.PublicKeys []string` (each entry an OpenSSH `authorized_keys` line) and `func (m *UserMgr) FindByAuthorizedKey(marshaled []byte) (*User, bool)` where `marshaled` is `ssh.PublicKey.Marshal()` wire bytes.

- [ ] **Step 1: Write the failing test**

```go
package user

import (
	"testing"

	"golang.org/x/crypto/ssh"
)

func TestFindByAuthorizedKey(t *testing.T) {
	// A deterministic test key (ed25519 authorized_keys line).
	const line = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIGb9ECWmEzf6FdyLqHU2eXvUjEjzaQjqQVZsGZ3GqGZ7 sysop@test"
	pub, _, _, _, err := ssh.ParseAuthorizedKey([]byte(line))
	if err != nil {
		t.Fatalf("parse test key: %v", err)
	}

	// This test is package user, so it may set the unexported map directly.
	// UserMgr.users is map[string]*User keyed by handle (confirmed in manager.go).
	m := &UserMgr{users: map[string]*User{
		"nobody": {ID: 1, Handle: "nobody", AccessLevel: 10},
		"sysop":  {ID: 2, Handle: "sysop", AccessLevel: 255, PublicKeys: []string{line}},
	}}

	got, ok := m.FindByAuthorizedKey(pub.Marshal())
	if !ok || got.Handle != "sysop" {
		t.Fatalf("expected sysop, got %v %+v", ok, got)
	}

	// Unknown key → not found.
	const other = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA other@x"
	opub, _, _, _, _ := ssh.ParseAuthorizedKey([]byte(other))
	if _, ok := m.FindByAuthorizedKey(opub.Marshal()); ok {
		t.Fatal("unknown key should not match")
	}
}
```

Verified facts (from `internal/user/manager.go`): `UserMgr` has `users map[string]*User` (keyed by handle) and `mu sync.RWMutex`; receivers are named `um`; `GetUser(handle string) (*User, bool)` and `Authenticate(handle, password string) (*User, bool)` exist.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/user/ -run TestFindByAuthorizedKey -v`
Expected: FAIL (undefined: `FindByAuthorizedKey` / unknown field `PublicKeys`).

- [ ] **Step 3: Write the implementation**

In `internal/user/user.go`, add to the `User` struct (right after the `Flags` field):

```go
	PublicKeys []string `json:"publicKeys,omitempty"` // OpenSSH authorized_keys lines authorized for WFC admin
```

In `internal/user/manager.go`, add:

```go
import "golang.org/x/crypto/ssh" // add to the existing import block

// FindByAuthorizedKey returns the user whose registered PublicKeys include a
// key matching the given marshaled wire bytes (ssh.PublicKey.Marshal()).
// Matching is by exact key bytes; access-level authorization is enforced by
// the caller, not here.
func (um *UserMgr) FindByAuthorizedKey(marshaled []byte) (*User, bool) {
	um.mu.RLock()
	defer um.mu.RUnlock()
	for _, u := range um.users { // um.users is map[string]*User
		for _, line := range u.PublicKeys {
			pub, _, _, _, err := ssh.ParseAuthorizedKey([]byte(line))
			if err != nil {
				continue
			}
			if string(pub.Marshal()) == string(marshaled) {
				return u, true
			}
		}
	}
	return nil, false
}

// NewUserMgrForTest builds a UserMgr seeded with the given users, keyed by
// handle. Exported so tests in other packages (e.g. cmd/vision3) can seed a
// manager without touching the JSON load path.
func NewUserMgrForTest(users ...*User) *UserMgr {
	m := &UserMgr{users: make(map[string]*User, len(users))}
	for _, u := range users {
		m.users[u.Handle] = u
	}
	return m
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/user/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/user/
go vet ./internal/user/
git add internal/user/user.go internal/user/manager.go internal/user/pubkey_test.go
git commit -m "feat(user): add PublicKeys field and authorized-key lookup"
```

---

### Task 9: Extend `sshserver.Config` for pubkey + subsystems

**Files:**
- Modify: `internal/sshserver/server.go` (Config + NewServer wiring)
- Test: `internal/sshserver/pubkey_test.go`

**Interfaces:**
- Produces two new optional `Config` fields, wired to gliderlabs:
  - `PublicKeyHandler func(ctx ssh.Context, key ssh.PublicKey) bool`
  - `SubsystemHandlers map[string]func(ssh.Session)`
- gliderlabs types: `ssh.Server.PublicKeyHandler` is `ssh.PublicKeyHandler` (`func(ctx ssh.Context, key ssh.PublicKey) bool`); `ssh.Server.SubsystemHandlers` is `map[string]ssh.SubsystemHandler` (`func(ssh.Session)`).

- [ ] **Step 1: Write the failing test**

```go
package sshserver

import (
	"testing"

	"github.com/gliderlabs/ssh"
)

func TestNewServerWiresPubkeyAndSubsystems(t *testing.T) {
	// Generate a throwaway host key file for the constructor.
	dir := t.TempDir()
	keyPath := writeTestHostKey(t, dir) // helper defined in existing server_test.go

	called := false
	srv, err := NewServer(Config{
		HostKeyPath: keyPath, Host: "127.0.0.1", Port: 0,
		SessionHandler: func(ssh.Session) {},
		PublicKeyHandler: func(ssh.Context, ssh.PublicKey) bool { called = true; return true },
		SubsystemHandlers: map[string]func(ssh.Session){
			"wfc-admin": func(ssh.Session) {},
		},
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	if srv.inner.PublicKeyHandler == nil {
		t.Fatal("PublicKeyHandler not wired")
	}
	if _, ok := srv.inner.SubsystemHandlers["wfc-admin"]; !ok {
		t.Fatal("wfc-admin subsystem not wired")
	}
	_ = called
}
```

> If `server_test.go` has no `writeTestHostKey` helper, add a small one that generates an ed25519 key, marshals it to PEM, and writes it to `dir/host_key`; return the path. Confirm by reading `internal/sshserver/server_test.go` first.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/sshserver/ -run TestNewServerWiresPubkey -v`
Expected: FAIL (unknown field `PublicKeyHandler` in `Config`).

- [ ] **Step 3: Write the implementation**

In `internal/sshserver/server.go`, add to `Config`:

```go
	PublicKeyHandler  func(ctx ssh.Context, key ssh.PublicKey) bool
	SubsystemHandlers map[string]func(ssh.Session)
```

In `NewServer`, after the `srv := &ssh.Server{...}` block and before `ServerConfigCallback`, add:

```go
	if cfg.PublicKeyHandler != nil {
		srv.PublicKeyHandler = cfg.PublicKeyHandler
	}
	if len(cfg.SubsystemHandlers) > 0 {
		srv.SubsystemHandlers = make(map[string]ssh.SubsystemHandler, len(cfg.SubsystemHandlers))
		for name, h := range cfg.SubsystemHandlers {
			srv.SubsystemHandlers[name] = ssh.SubsystemHandler(h)
		}
	}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/sshserver/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/sshserver/
go vet ./internal/sshserver/
git add internal/sshserver/server.go internal/sshserver/pubkey_test.go
git commit -m "feat(sshserver): expose PublicKeyHandler and SubsystemHandlers"
```

---

### Task 10: `SSHChannelClient` (remote AdminClient)

**Files:**
- Create: `internal/admin/client_ssh.go`
- Test: `internal/admin/client_ssh_test.go`

**Interfaces:**
- Consumes: `NewStreamClient`, `golang.org/x/crypto/ssh`, `.../ssh/knownhosts`.
- Produces:
  - `type SSHDialConfig struct { Addr, User string; Signer ssh.Signer; KnownHostsPath string; Insecure bool }`
  - `func DialSSH(cfg SSHDialConfig) (*SSHChannelClient, error)` — dials, opens the `wfc-admin` subsystem, returns an `AdminClient`.
  - `SSHChannelClient` embeds `*StreamClient` and also closes the SSH conn.

- [ ] **Step 1: Write the failing test**

This test stands up a real in-memory SSH server (gliderlabs) exposing a `wfc-admin` subsystem backed by `ServeRPC`, then dials it with `DialSSH`.

```go
package admin

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"net"
	"testing"
	"time"

	gliderssh "github.com/gliderlabs/ssh"
	"golang.org/x/crypto/ssh"

	"github.com/ViSiON-3/vision-3-bbs/internal/session"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

func TestDialSSHEndToEnd(t *testing.T) {
	// Server-side admin Server.
	reg := &fakeRegistry{sessions: []*session.BbsSession{{NodeID: 1, User: &user.User{Handle: "A"}, CurrentMenu: "MAIN"}}}
	srv := NewServer(ServerConfig{Reg: reg, SystemName: "SSHT", StartedAt: time.Now(), Refresh: 20 * time.Millisecond, MaxEvents: 8, CallsToday: func() int { return -1 }})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go srv.Run(ctx)

	// Host key + client key.
	_, hostPriv, _ := ed25519.GenerateKey(rand.Reader)
	hostSigner, _ := ssh.NewSignerFromKey(hostPriv)
	_, cliPriv, _ := ed25519.GenerateKey(rand.Reader)
	cliSigner, _ := ssh.NewSignerFromKey(cliPriv)

	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	defer ln.Close()

	gs := &gliderssh.Server{
		HostSigners:      []gliderssh.Signer{hostSigner},
		PublicKeyHandler: func(gliderssh.Context, gliderssh.PublicKey) bool { return true },
		SubsystemHandlers: map[string]gliderssh.SubsystemHandler{
			"wfc-admin": func(s gliderssh.Session) { ServeRPC(ctx, s, srv, nil) },
		},
	}
	go gs.Serve(ln)

	c, err := DialSSH(SSHDialConfig{Addr: ln.Addr().String(), User: "sysop", Signer: cliSigner, Insecure: true})
	if err != nil {
		t.Fatalf("DialSSH: %v", err)
	}
	defer c.Close()

	snap, err := c.Snapshot(ctx)
	if err != nil || snap == nil || snap.SystemName != "SSHT" {
		t.Fatalf("snapshot: %v %+v", err, snap)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/admin/ -run TestDialSSH -v`
Expected: FAIL (undefined: `DialSSH`).

- [ ] **Step 3: Write the implementation**

```go
package admin

import (
	"fmt"
	"net"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// SSHDialConfig configures a remote admin connection.
type SSHDialConfig struct {
	Addr           string // host:port
	User           string // sysop account name
	Signer         ssh.Signer
	KnownHostsPath string // verified against this file unless Insecure
	Insecure       bool   // skip host-key verification (tests/first-run only)
}

// SSHChannelClient is an AdminClient over an SSH "wfc-admin" subsystem channel.
type SSHChannelClient struct {
	*StreamClient
	conn *ssh.Client
}

// DialSSH connects to the BBS SSH server, opens the wfc-admin subsystem, and
// returns a streaming AdminClient.
func DialSSH(cfg SSHDialConfig) (*SSHChannelClient, error) {
	hostKeyCb := ssh.InsecureIgnoreHostKey()
	if !cfg.Insecure {
		cb, err := knownhosts.New(cfg.KnownHostsPath)
		if err != nil {
			return nil, fmt.Errorf("known_hosts %s: %w", cfg.KnownHostsPath, err)
		}
		hostKeyCb = cb
	}

	clientCfg := &ssh.ClientConfig{
		User:            cfg.User,
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(cfg.Signer)},
		HostKeyCallback: hostKeyCb,
		Timeout:         10 * time.Second,
	}

	conn, err := ssh.Dial("tcp", cfg.Addr, clientCfg)
	if err != nil {
		return nil, fmt.Errorf("ssh dial %s: %w", cfg.Addr, err)
	}
	ch, reqs, err := conn.OpenChannel("session", nil)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("open session channel: %w", err)
	}
	go ssh.DiscardRequests(reqs)

	ok, err := ch.SendRequest("subsystem", true, ssh.Marshal(struct{ Name string }{"wfc-admin"}))
	if err != nil || !ok {
		ch.Close()
		conn.Close()
		return nil, fmt.Errorf("request wfc-admin subsystem: ok=%v err=%w", ok, err)
	}

	return &SSHChannelClient{StreamClient: NewStreamClient(ch), conn: conn}, nil
}

// Close closes the channel and the underlying SSH connection.
func (c *SSHChannelClient) Close() error {
	_ = c.StreamClient.Close()
	return c.conn.Close()
}

var _ = net.Dial // keep net import if trimmed by tooling
```

> Remove the trailing `var _ = net.Dial` and the `net` import if `gofmt`/`go vet` flag it as unused; it is only a guard for editors. Prefer deleting both.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/admin/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/admin/
go vet ./internal/admin/
go mod tidy
git add internal/admin/client_ssh.go internal/admin/client_ssh_test.go go.mod go.sum
git commit -m "feat(admin): SSH wfc-admin subsystem AdminClient (knownhosts verified)"
```

---

### Task 11: `wfcui` model + update logic

**Files:**
- Create: `internal/wfcui/model.go`
- Create: `internal/wfcui/messages.go`
- Test: `internal/wfcui/model_test.go`

**Interfaces:**
- Consumes: `admin.AdminClient`, `admin.SystemSnapshot`, `admin.Event`.
- Produces:
  - `type Options struct { ASCII, NoColor, ReadOnly bool; MaxEvents int }`
  - `func New(client admin.AdminClient, opts Options) Model`
  - `Model` implements `tea.Model` (`Init`, `Update`, `View`).
  - Internal messages `snapshotMsg`, `eventMsg`, `errMsg` (in `messages.go`).
  - View modes: list (default), details, disconnected, tooSmall.

- [ ] **Step 1: Write the failing test**

```go
package wfcui

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ViSiON-3/vision-3-bbs/internal/admin"
)

type stubClient struct{}

func (stubClient) Snapshot(_ ctxT) (*admin.SystemSnapshot, error) { return nil, nil }

func TestUpdateAppliesSnapshotAndSelection(t *testing.T) {
	m := New(nil, Options{MaxEvents: 50})
	m.width, m.height = 100, 30

	snap := &admin.SystemSnapshot{SystemName: "T", Time: time.Now(), Nodes: []admin.NodeState{
		{NodeID: 1, Handle: "A"}, {NodeID: 2, Handle: "B"},
	}}
	mi, _ := m.Update(snapshotMsg{snap})
	m = mi.(Model)
	if m.snapshot == nil || len(m.snapshot.Nodes) != 2 {
		t.Fatalf("snapshot not applied: %+v", m.snapshot)
	}

	// Down arrow selects node index 1.
	mi, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = mi.(Model)
	if m.selected != 1 {
		t.Fatalf("expected selected=1, got %d", m.selected)
	}

	// Enter opens details.
	mi, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = mi.(Model)
	if m.mode != modeDetails {
		t.Fatalf("expected details mode, got %v", m.mode)
	}

	// Esc returns to list.
	mi, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = mi.(Model)
	if m.mode != modeList {
		t.Fatalf("expected list mode, got %v", m.mode)
	}
}

func TestErrMsgEntersDisconnected(t *testing.T) {
	m := New(nil, Options{MaxEvents: 10})
	mi, _ := m.Update(errMsg{err: errStub{}})
	if mi.(Model).mode != modeDisconnected {
		t.Fatal("errMsg should enter disconnected mode")
	}
}

type errStub struct{}

func (errStub) Error() string { return "boom" }

// ctxT keeps the stub signature short; real code uses context.Context.
type ctxT = interface{}
```

> The `stubClient`/`ctxT` shim only exists to keep the test compiling without importing context for an unused stub; if simpler, pass `nil` as the client (the model must guard nil client in `Init`, returning no command). Keep whichever compiles cleanly.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/wfcui/ -run TestUpdate -v`
Expected: FAIL (undefined: `New`).

- [ ] **Step 3: Write the implementation**

`internal/wfcui/messages.go`:

```go
package wfcui

import "github.com/ViSiON-3/vision-3-bbs/internal/admin"

type snapshotMsg struct{ snap *admin.SystemSnapshot }
type eventMsg struct{ ev admin.Event }
type errMsg struct{ err error }
```

`internal/wfcui/model.go`:

```go
// Package wfcui is the transport-agnostic Bubble Tea TUI for the WFC console.
// It depends only on internal/admin (interface + types).
package wfcui

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ViSiON-3/vision-3-bbs/internal/admin"
)

type viewMode int

const (
	modeList viewMode = iota
	modeDetails
	modeDisconnected
	modeTooSmall
)

const (
	minWidth  = 80
	minHeight = 25
)

// Options configures rendering and behavior.
type Options struct {
	ASCII    bool
	NoColor  bool
	ReadOnly bool
	MaxEvents int
}

// Model is the WFC TUI model.
type Model struct {
	client   admin.AdminClient
	opts     Options
	snapshot *admin.SystemSnapshot
	events   []admin.Event
	selected int
	mode     viewMode
	width    int
	height   int
	lastErr  string
}

// New builds a Model. client may be nil in tests that drive Update directly.
func New(client admin.AdminClient, opts Options) Model {
	if opts.MaxEvents <= 0 {
		opts.MaxEvents = 200
	}
	return Model{client: client, opts: opts, mode: modeList}
}

// Init kicks off the first snapshot fetch and event subscription.
func (m Model) Init() tea.Cmd {
	if m.client == nil {
		return nil
	}
	return tea.Batch(m.fetchSnapshot(), m.waitEvent())
}

func (m Model) fetchSnapshot() tea.Cmd {
	client := m.client
	return func() tea.Msg {
		snap, err := client.Snapshot(context.Background())
		if err != nil {
			return errMsg{err}
		}
		return snapshotMsg{snap}
	}
}

// pollCmd re-fetches the snapshot after a delay so the screen stays live.
func pollCmd(client admin.AdminClient, d time.Duration) tea.Cmd {
	return tea.Tick(d, func(time.Time) tea.Msg {
		snap, err := client.Snapshot(context.Background())
		if err != nil {
			return errMsg{err}
		}
		return snapshotMsg{snap}
	})
}

func (m Model) waitEvent() tea.Cmd {
	client := m.client
	return func() tea.Msg {
		ch, err := client.Subscribe(context.Background())
		if err != nil {
			return errMsg{err}
		}
		ev, ok := <-ch
		if !ok {
			return errMsg{context.Canceled}
		}
		return eventMsg{ev}
	}
}

// Update handles messages.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		if m.width < minWidth || m.height < minHeight {
			m.mode = modeTooSmall
		} else if m.mode == modeTooSmall {
			m.mode = modeList
		}
		return m, nil

	case snapshotMsg:
		m.snapshot = msg.snap
		if m.snapshot != nil && m.selected >= len(m.snapshot.Nodes) {
			m.selected = max(0, len(m.snapshot.Nodes)-1)
		}
		if m.mode == modeDisconnected {
			m.mode = modeList
		}
		var cmd tea.Cmd
		if m.client != nil {
			cmd = pollCmd(m.client, time.Second)
		}
		return m, cmd

	case eventMsg:
		m.events = append(m.events, msg.ev)
		if len(m.events) > m.opts.MaxEvents {
			m.events = m.events[len(m.events)-m.opts.MaxEvents:]
		}
		return m, m.waitEvent()

	case errMsg:
		m.mode = modeDisconnected
		if msg.err != nil {
			m.lastErr = msg.err.Error()
		}
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
```

> `handleKey` is defined in Task 13 (`keys.go`). The package will not compile until Task 13 lands; run this task's tests together with Task 13, or stub `handleKey` here and replace it in Task 13. To keep each task independently testable, add a minimal `handleKey` stub in this task and flesh it out in Task 13:

```go
// minimal stub; replaced in keys.go (Task 13)
func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyDown:
		if m.snapshot != nil && m.selected < len(m.snapshot.Nodes)-1 {
			m.selected++
		}
	case tea.KeyUp:
		if m.selected > 0 {
			m.selected--
		}
	case tea.KeyEnter:
		if m.mode == modeList {
			m.mode = modeDetails
		}
	case tea.KeyEsc:
		if m.mode == modeDetails {
			m.mode = modeList
		}
	}
	return m, nil
}
```

> Define `View()` minimally here too so the model satisfies `tea.Model` for this task; Task 12 replaces it:

```go
func (m Model) View() string { return "" }
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/wfcui/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/wfcui/
go vet ./internal/wfcui/
git add internal/wfcui/model.go internal/wfcui/messages.go internal/wfcui/model_test.go
git commit -m "feat(wfcui): model, update logic, and live snapshot/event commands"
```

---

### Task 12: `wfcui` view + styles

**Files:**
- Create: `internal/wfcui/view.go` (replaces the stub `View()` from Task 11 — delete the stub there)
- Create: `internal/wfcui/styles.go`
- Test: `internal/wfcui/view_test.go`

**Interfaces:**
- Consumes: `Model`, `admin.SystemSnapshot`, `lipgloss`.
- Produces: full `func (m Model) View() string` rendering header / node table / event feed / command bar, honoring `modeTooSmall` and `modeDisconnected`; `styles.go` provides border/color sets switched by `opts.ASCII`/`opts.NoColor`.

- [ ] **Step 1: Write the failing test**

```go
package wfcui

import (
	"strings"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/admin"
)

func TestViewRendersHeaderAndNodes(t *testing.T) {
	m := New(nil, Options{MaxEvents: 50, ASCII: true, NoColor: true})
	m.width, m.height = 100, 30
	m.snapshot = &admin.SystemSnapshot{
		SystemName: "The Misfit Node", Time: time.Now(), UptimeSecs: 3600,
		Nodes: []admin.NodeState{{NodeID: 1, Handle: "RobbieW", Status: admin.StatusOnline, Activity: "Reading messages"}},
		Counters: admin.Counters{ActiveNodes: 1, CallsToday: 14},
	}
	out := m.View()
	for _, want := range []string{"The Misfit Node", "RobbieW", "Reading messages"} {
		if !strings.Contains(out, want) {
			t.Fatalf("view missing %q\n%s", want, out)
		}
	}
}

func TestViewTooSmall(t *testing.T) {
	m := New(nil, Options{})
	m.width, m.height, m.mode = 40, 10, modeTooSmall
	if !strings.Contains(m.View(), "Terminal too small") {
		t.Fatalf("expected too-small guard, got:\n%s", m.View())
	}
}

func TestViewDisconnected(t *testing.T) {
	m := New(nil, Options{})
	m.width, m.height, m.mode = 100, 30, modeDisconnected
	if !strings.Contains(m.View(), "Disconnected") {
		t.Fatalf("expected disconnected banner, got:\n%s", m.View())
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/wfcui/ -run TestView -v`
Expected: FAIL (view returns empty string / missing substrings).

- [ ] **Step 3: Write the implementation**

Delete the stub `func (m Model) View() string { return "" }` from `model.go`, then create `internal/wfcui/styles.go`:

```go
package wfcui

import "github.com/charmbracelet/lipgloss"

type styleSet struct {
	border   lipgloss.Border
	header   lipgloss.Style
	selected lipgloss.Style
	dim      lipgloss.Style
}

func newStyles(opts Options) styleSet {
	b := lipgloss.RoundedBorder()
	if opts.ASCII {
		b = lipgloss.Border{
			Top: "-", Bottom: "-", Left: "|", Right: "|",
			TopLeft: "+", TopRight: "+", BottomLeft: "+", BottomRight: "+",
		}
	}
	s := styleSet{border: b, header: lipgloss.NewStyle(), selected: lipgloss.NewStyle(), dim: lipgloss.NewStyle()}
	if !opts.NoColor {
		s.header = s.header.Bold(true).Foreground(lipgloss.Color("15"))
		s.selected = s.selected.Reverse(true)
		s.dim = s.dim.Foreground(lipgloss.Color("8"))
	}
	return s
}
```

Create `internal/wfcui/view.go`:

```go
package wfcui

import (
	"fmt"
	"strings"
	"time"
)

// View renders the WFC screen.
func (m Model) View() string {
	switch m.mode {
	case modeTooSmall:
		return fmt.Sprintf("Terminal too small for WFC display.\nMinimum size: %dx%d. Current: %dx%d.\n",
			minWidth, minHeight, m.width, m.height)
	case modeDisconnected:
		msg := "Disconnected from admin endpoint."
		if m.lastErr != "" {
			msg += " (" + m.lastErr + ")"
		}
		return "\n  " + msg + "\n\n  [R] retry   [Q] quit\n"
	case modeDetails:
		return m.detailsView()
	default:
		return m.listView()
	}
}

func (m Model) listView() string {
	st := newStyles(m.opts)
	var b strings.Builder

	name, uptime, calls := "ViSiON/3", "0s", -1
	if m.snapshot != nil {
		name = m.snapshot.SystemName
		uptime = (time.Duration(m.snapshot.UptimeSecs) * time.Second).String()
		calls = m.snapshot.Counters.CallsToday
	}
	header := fmt.Sprintf("VISION/3 WFC  -  System: %s   Uptime: %s", name, uptime)
	if calls >= 0 {
		header += fmt.Sprintf("   Calls: %d", calls)
	}
	b.WriteString(st.header.Render(header))
	b.WriteString("\n\n")

	b.WriteString(fmt.Sprintf("%-4s %-12s %-14s %-24s\n", "Node", "Status", "User", "Activity"))
	if m.snapshot != nil {
		for i, n := range m.snapshot.Nodes {
			activity := n.Activity
			if activity == "" {
				activity = n.CurrentMenu
			}
			row := fmt.Sprintf("%-4d %-12s %-14s %-24s", n.NodeID, n.Status, n.Handle, activity)
			if i == m.selected {
				row = st.selected.Render(row)
			}
			b.WriteString(row)
			b.WriteString("\n")
		}
	}

	b.WriteString("\n")
	b.WriteString(st.header.Render("Events"))
	b.WriteString("\n")
	start := 0
	if len(m.events) > 6 {
		start = len(m.events) - 6
	}
	for _, e := range m.events[start:] {
		b.WriteString(st.dim.Render(fmt.Sprintf("%s  Node %d  %s  %s",
			e.Time.Format("15:04"), e.NodeID, e.Type, e.Message)))
		b.WriteString("\n")
	}

	b.WriteString("\n")
	bar := "[Enter] View  [R] Refresh  [L] Logs  [?] Help  [Q] Quit"
	if !m.opts.ReadOnly {
		bar = "[C] Chat [M] Msg [D] Drop  " + bar
	} else {
		bar = st.dim.Render("[C][M][D] disabled (read-only)  ") + bar
	}
	b.WriteString(bar)
	b.WriteString("\n")
	return b.String()
}
```

> `detailsView()` is created in Task 13. To keep this task's package compiling and tests runnable, add a one-line forwarder here that Task 13 replaces:

```go
func (m Model) detailsView() string { return m.listView() }
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/wfcui/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/wfcui/
go vet ./internal/wfcui/
git add internal/wfcui/view.go internal/wfcui/styles.go internal/wfcui/model.go internal/wfcui/view_test.go
git commit -m "feat(wfcui): render header, node table, event feed, and guards"
```

---

### Task 13: `wfcui` keys + node details

**Files:**
- Create: `internal/wfcui/keys.go` (move/replace the `handleKey` stub from Task 11 — delete it there)
- Create: `internal/wfcui/details.go` (replaces the `detailsView` forwarder from Task 12 — delete it there)
- Test: `internal/wfcui/keys_test.go`

**Interfaces:**
- Produces the full `handleKey` (adds `R` refresh, `L` logs toggle, `?` help, `Q`/`Ctrl+C` quit) and `detailsView()` for the selected node.

- [ ] **Step 1: Write the failing test**

```go
package wfcui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ViSiON-3/vision-3-bbs/internal/admin"
)

func TestQuitKey(t *testing.T) {
	m := New(nil, Options{})
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if cmd == nil {
		t.Fatal("q should return a quit command")
	}
}

func TestDetailsViewShowsFields(t *testing.T) {
	m := New(nil, Options{ASCII: true, NoColor: true})
	m.width, m.height = 100, 30
	m.snapshot = &admin.SystemSnapshot{Nodes: []admin.NodeState{{
		NodeID: 3, Handle: "Anna", UserID: 9, RemoteAddr: "5.6.7.8:22",
		CurrentMenu: "FILES", Activity: "Browsing", Status: admin.StatusOnline,
		ConnectedAt: time.Now(), TimeLeftMins: 30,
	}}}
	m.selected = 0
	m.mode = modeDetails
	out := m.detailsView()
	for _, want := range []string{"Anna", "5.6.7.8:22", "FILES", "Browsing"} {
		if !strings.Contains(out, want) {
			t.Fatalf("details missing %q\n%s", want, out)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/wfcui/ -run "TestQuitKey|TestDetailsViewShowsFields" -v`
Expected: FAIL — `q` does not quit yet (stub) and details forwards to listView (no "FILES"/"Browsing" detail labels). If the stubs happen to satisfy a substring, the `q` quit assertion still fails.

- [ ] **Step 3: Write the implementation**

Delete the `handleKey` stub from `model.go` and the `detailsView` forwarder from `view.go`. Create `internal/wfcui/keys.go`:

```go
package wfcui

import tea "github.com/charmbracelet/bubbletea"

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyCtrlC:
		return m, tea.Quit
	case tea.KeyDown:
		if m.mode == modeList && m.snapshot != nil && m.selected < len(m.snapshot.Nodes)-1 {
			m.selected++
		}
		return m, nil
	case tea.KeyUp:
		if m.mode == modeList && m.selected > 0 {
			m.selected--
		}
		return m, nil
	case tea.KeyEnter:
		if m.mode == modeList && m.snapshot != nil && len(m.snapshot.Nodes) > 0 {
			m.mode = modeDetails
		}
		return m, nil
	case tea.KeyEsc:
		if m.mode == modeDetails {
			m.mode = modeList
		}
		return m, nil
	case tea.KeyRunes:
		switch msg.Runes[0] {
		case 'q', 'Q':
			return m, tea.Quit
		case 'r', 'R':
			if m.mode == modeDisconnected {
				m.mode = modeList
			}
			if m.client != nil {
				return m, m.fetchSnapshot()
			}
		case 'l', 'L':
			m.showLogs = !m.showLogs
		}
		return m, nil
	}
	return m, nil
}
```

Add the `showLogs bool` field to the `Model` struct in `model.go`. Create `internal/wfcui/details.go`:

```go
package wfcui

import (
	"fmt"
	"strings"
)

func (m Model) detailsView() string {
	if m.snapshot == nil || m.selected >= len(m.snapshot.Nodes) {
		return "No node selected.\n[Esc] back\n"
	}
	n := m.snapshot.Nodes[m.selected]
	timeLeft := "unknown"
	if n.TimeLeftMins >= 0 {
		timeLeft = fmt.Sprintf("%d min", n.TimeLeftMins)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Node %d details\n\n", n.NodeID)
	rows := [][2]string{
		{"Status", string(n.Status)},
		{"Handle", n.Handle},
		{"User ID", fmt.Sprintf("%d", n.UserID)},
		{"Access level", fmt.Sprintf("%d", n.AccessLevel)},
		{"Remote addr", n.RemoteAddr},
		{"Current menu", n.CurrentMenu},
		{"Activity", n.Activity},
		{"Connected at", n.ConnectedAt.Format("2006-01-02 15:04:05")},
		{"Last activity", n.LastActivity.Format("15:04:05")},
		{"Time left", timeLeft},
	}
	for _, r := range rows {
		fmt.Fprintf(&b, "  %-14s %s\n", r[0]+":", r[1])
	}
	b.WriteString("\n[Esc] back\n")
	return b.String()
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/wfcui/ -v`
Expected: PASS (whole package).

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/wfcui/
go vet ./internal/wfcui/
git add internal/wfcui/keys.go internal/wfcui/details.go internal/wfcui/model.go internal/wfcui/view.go internal/wfcui/keys_test.go
git commit -m "feat(wfcui): key handling and node details view"
```

---

### Task 14: Daemon wiring — admin server, pubkey auth, subsystem

**Files:**
- Modify: `cmd/vision3/main.go` (instantiate admin server near `sessionRegistry = session.NewSessionRegistry()` ~line 1736; pass new handlers to `startSSHServer`)
- Modify: `cmd/vision3/ssh_server.go` (add `PublicKeyHandler` + `SubsystemHandlers` to the `sshserver.Config`)
- Create: `cmd/vision3/wfc_admin.go` (pubkey handler, subsystem handler, sysop authz, audit)
- Test: `cmd/vision3/wfc_admin_test.go`

**Interfaces:**
- Consumes: `admin.NewServer`, `admin.ServeRPC`, `userMgr.FindByAuthorizedKey`, `userMgr.GetUser`, `sessionRegistry`, and the startup-local `serverConfig` (`BoardName`, `CoSysOpLevel`). Note: `cmd/vision3` has globals `userMgr`, `menuExecutor`, `sessionRegistry` (main.go:45-51) but **no** global `cfg`/`serverConfig` — `serverConfig` is a local in the startup function (main.go:1615), in scope where the admin server is wired (~main.go:1736).
- Produces: package-level `adminServer *admin.Server` and `adminMinLevel int`; `func wfcPublicKeyHandler(ctx ssh.Context, key ssh.PublicKey) bool`; `func wfcAdminSubsystem(sess ssh.Session)`; `func authorizeAdmin(handle string) bool`.

- [ ] **Step 1: Write the failing test**

```go
package main

import (
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

func TestAuthorizeAdminRequiresSysopLevel(t *testing.T) {
	// authorizeAdmin looks up the handle in userMgr and checks access level
	// against cfg.CoSysOpLevel. Seed both package globals.
	userMgr = user.NewUserMgrForTest( // exported test helper added in Task 8
		&user.User{ID: 1, Handle: "lowly", AccessLevel: 10},
		&user.User{ID: 2, Handle: "boss", AccessLevel: 255},
	)
	adminMinLevel = 250 // package var set from serverConfig.CoSysOpLevel at startup

	if authorizeAdmin("lowly") {
		t.Fatal("non-sysop must be denied")
	}
	if !authorizeAdmin("boss") {
		t.Fatal("sysop must be allowed")
	}
}
```

> `userMgr` is an existing package-level global in `cmd/vision3` (`main.go:45`, used in `ssh_server.go:36`). `adminMinLevel` is a new package var introduced by this task (below), set at startup; the test seeds it directly. This keeps `authorizeAdmin` free of any dependency on `menuExecutor`.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/vision3/ -run TestAuthorizeAdmin -v`
Expected: FAIL (undefined: `authorizeAdmin`).

- [ ] **Step 3: Write the implementation**

Create `cmd/vision3/wfc_admin.go`:

```go
package main

import (
	"context"
	"log/slog"

	"github.com/gliderlabs/ssh"

	"github.com/ViSiON-3/vision-3-bbs/internal/admin"
)

// adminServer is the running WFC admin server; nil until startup wires it.
var adminServer *admin.Server

// adminMinLevel is the minimum access level required for WFC admin access.
// Set at startup from serverConfig.CoSysOpLevel.
var adminMinLevel int

// adminAuthUserKey stores the sysop handle resolved during public-key auth.
type adminAuthUserKey struct{}

// wfcPublicKeyHandler authorizes a connection for the wfc-admin subsystem if
// the presented key maps to a known user with sysop/cosysop access. Returning
// false lets normal callers fall through to password/keyboard-interactive auth.
func wfcPublicKeyHandler(ctx ssh.Context, key ssh.PublicKey) bool {
	if userMgr == nil {
		return false
	}
	u, ok := userMgr.FindByAuthorizedKey(key.Marshal())
	if !ok || u == nil {
		return false
	}
	if !authorizeAdmin(u.Handle) {
		slog.Warn("wfc: key matched non-sysop user", "handle", u.Handle, "remote", ctx.RemoteAddr())
		return false
	}
	ctx.SetValue(adminAuthUserKey{}, u.Handle)
	slog.Info("wfc: admin key accepted", "handle", u.Handle, "remote", ctx.RemoteAddr())
	return true
}

// authorizeAdmin reports whether handle has at least the configured admin level.
func authorizeAdmin(handle string) bool {
	if userMgr == nil {
		return false
	}
	u, ok := userMgr.GetUser(handle)
	if !ok || u == nil {
		return false
	}
	return u.AccessLevel >= adminMinLevel
}

// wfcAdminSubsystem serves the admin RPC over an authorized SSH session.
func wfcAdminSubsystem(sess ssh.Session) {
	handle, _ := sess.Context().Value(adminAuthUserKey{}).(string)
	if handle == "" || !authorizeAdmin(handle) {
		slog.Warn("wfc: rejecting unauthorized admin subsystem", "remote", sess.RemoteAddr())
		return
	}
	if adminServer == nil {
		slog.Error("wfc: admin server not initialized")
		return
	}
	slog.Info("wfc: admin session opened", "sysop", handle, "remote", sess.RemoteAddr())
	defer slog.Info("wfc: admin session closed", "sysop", handle, "remote", sess.RemoteAddr())

	audit := func(cmd string) {
		slog.Info("wfc: sysop command", "sysop", handle, "command", cmd, "remote", sess.RemoteAddr())
	}
	_ = admin.ServeRPC(sess.Context(), sess, adminServer, audit)
}
```

> `userMgr` is an existing package-level global in `cmd/vision3` (`main.go:45`); `userMgr.GetUser` is the existing lookup used in `ssh_server.go:36`. `adminMinLevel` is the new package var this task introduces (set from `serverConfig.CoSysOpLevel` at startup) — there is no global `cfg` in this package.

In `cmd/vision3/main.go`, immediately after `sessionRegistry = session.NewSessionRegistry()` (~line 1736), add:

```go
	adminMinLevel = serverConfig.CoSysOpLevel
	adminServer = admin.NewServer(admin.ServerConfig{
		Reg:        sessionRegistry,
		SystemName: serverConfig.BoardName,
		StartedAt:  time.Now(),
		Refresh:    time.Second,
		MaxEvents:  200,
		CallsToday: func() int { return -1 }, // wired to callhistory.json in a follow-up
	})
	go adminServer.Run(context.Background())
```

> Add `"context"` to `main.go` imports (`"time"` is already used). `serverConfig` is the local from `main.go:1615`, in scope here and passed to the executor at `main.go:1750`, so `serverConfig.BoardName` and `serverConfig.CoSysOpLevel` are correct. Confirm both fields exist on `config.ServerConfig` (they do: `CoSysOpLevel` at `config.go`, used at `main.go:1273`; `BoardName` is the BBS name field).

In `cmd/vision3/ssh_server.go`, extend the `sshserver.Config` literal in `startSSHServer` with:

```go
		PublicKeyHandler: wfcPublicKeyHandler,
		SubsystemHandlers: map[string]func(ssh.Session){
			"wfc-admin": wfcAdminSubsystem,
		},
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./cmd/vision3/ -run TestAuthorizeAdmin -v` then `go build ./cmd/vision3/`
Expected: PASS and a clean build.

- [ ] **Step 5: Commit**

```bash
gofmt -w cmd/vision3/
go vet ./cmd/vision3/
git add cmd/vision3/wfc_admin.go cmd/vision3/wfc_admin_test.go cmd/vision3/main.go cmd/vision3/ssh_server.go
git commit -m "feat(vision3): host WFC admin server and wfc-admin SSH subsystem"
```

---

### Task 15: `cmd/wfc` binary

**Files:**
- Create: `cmd/wfc/main.go`
- Test: `cmd/wfc/main_test.go`

**Interfaces:**
- Consumes: `admin.DialSSH`, `admin.SSHDialConfig`, `wfcui.New`, `wfcui.Options`, `tea.NewProgram`.
- Produces: the CLI with flags `--connect`, `--readonly`, `--ascii`, `--no-color`, `--refresh`, `--max-events`, `--identity`, `--known-hosts`, `--insecure`, `--version`, `--help`; plus `func parseConnect(s string) (user, addr string, err error)`.

- [ ] **Step 1: Write the failing test**

```go
package main

import "testing"

func TestParseConnect(t *testing.T) {
	u, addr, err := parseConnect("ssh://sysop@bbs.example.com:6023")
	if err != nil || u != "sysop" || addr != "bbs.example.com:6023" {
		t.Fatalf("got u=%q addr=%q err=%v", u, addr, err)
	}
	if _, _, err := parseConnect("tcp://x"); err == nil {
		t.Fatal("non-ssh scheme must error")
	}
	if _, _, err := parseConnect("ssh://nohost"); err == nil {
		t.Fatal("missing user@host:port must error")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/wfc/ -run TestParseConnect -v`
Expected: FAIL (undefined: `parseConnect`).

- [ ] **Step 3: Write the implementation**

```go
// Command wfc is the ViSiON/3 Waiting-For-Caller sysop console. It connects to
// a running ViSiON/3 daemon over the wfc-admin SSH subsystem and renders live
// node and event state. Read-only in this version.
package main

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"golang.org/x/crypto/ssh"

	"github.com/ViSiON-3/vision-3-bbs/internal/admin"
	"github.com/ViSiON-3/vision-3-bbs/internal/wfcui"
)

const version = "wfc 0.1.0 (read-only)"

func parseConnect(s string) (string, string, error) {
	u, err := url.Parse(s)
	if err != nil {
		return "", "", err
	}
	if u.Scheme != "ssh" {
		return "", "", fmt.Errorf("only ssh:// endpoints are supported, got %q", u.Scheme)
	}
	if u.User == nil || u.User.Username() == "" || u.Host == "" || u.Port() == "" {
		return "", "", fmt.Errorf("endpoint must be ssh://user@host:port")
	}
	return u.User.Username(), u.Host, nil
}

func loadSigner(identity string) (ssh.Signer, error) {
	if identity == "" {
		home, _ := os.UserHomeDir()
		identity = filepath.Join(home, ".ssh", "id_ed25519")
	}
	keyBytes, err := os.ReadFile(identity)
	if err != nil {
		return nil, fmt.Errorf("read identity %s: %w", identity, err)
	}
	return ssh.ParsePrivateKey(keyBytes)
}

func main() {
	var (
		connect    string
		readonly   bool
		ascii      bool
		noColor    bool
		refreshMs  int
		maxEvents  int
		identity   string
		knownHosts string
		insecure   bool
		showVer    bool
	)
	parseFlags(&connect, &readonly, &ascii, &noColor, &refreshMs, &maxEvents, &identity, &knownHosts, &insecure, &showVer)

	if showVer {
		fmt.Println(version)
		return
	}
	if connect == "" {
		fail("missing --connect ssh://user@host:port")
	}
	user, addr, err := parseConnect(connect)
	if err != nil {
		fail(err.Error())
	}
	signer, err := loadSigner(identity)
	if err != nil {
		fail(err.Error())
	}
	if knownHosts == "" {
		home, _ := os.UserHomeDir()
		knownHosts = filepath.Join(home, ".ssh", "known_hosts")
	}

	client, err := admin.DialSSH(admin.SSHDialConfig{
		Addr: addr, User: user, Signer: signer,
		KnownHostsPath: knownHosts, Insecure: insecure,
	})
	if err != nil {
		fail("connect: " + err.Error())
	}
	defer client.Close()

	model := wfcui.New(client, wfcui.Options{
		ASCII: ascii, NoColor: noColor, ReadOnly: true, MaxEvents: maxEvents,
	})
	_ = readonly // v1 is always read-only; flag accepted for forward-compat
	_ = refreshMs
	_ = context.Background

	p := tea.NewProgram(model, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fail(err.Error())
	}
}

func fail(msg string) {
	fmt.Fprintln(os.Stderr, "wfc: "+strings.TrimSpace(msg))
	os.Exit(1)
}

var _ = time.Second // retained if a later flag uses durations
```

Create `cmd/wfc/flags.go` (keep `main.go` under 300 lines):

```go
package main

import "flag"

func parseFlags(connect *string, readonly, ascii, noColor *bool, refreshMs, maxEvents *int, identity, knownHosts *string, insecure, showVer *bool) {
	flag.StringVar(connect, "connect", "", "admin endpoint: ssh://user@host:port")
	flag.BoolVar(readonly, "readonly", true, "disable mutating actions (always true in v1)")
	flag.BoolVar(ascii, "ascii", false, "use ASCII borders instead of line drawing")
	flag.BoolVar(noColor, "no-color", false, "disable color")
	flag.IntVar(refreshMs, "refresh", 1000, "snapshot refresh interval (ms)")
	flag.IntVar(maxEvents, "max-events", 200, "max events held in the feed")
	flag.StringVar(identity, "identity", "", "SSH private key (default ~/.ssh/id_ed25519)")
	flag.StringVar(knownHosts, "known-hosts", "", "known_hosts file (default ~/.ssh/known_hosts)")
	flag.BoolVar(insecure, "insecure", false, "skip host-key verification (first-run only)")
	flag.BoolVar(showVer, "version", false, "print version and exit")
	flag.Parse()
}
```

> Remove the `var _ = time.Second` guard and the `time` import if `go vet` flags them; they are placeholders for a future duration flag. Keep `refreshMs`/`readonly` wired into `Options`/poll once mutations land.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./cmd/wfc/ -v` then `go build ./cmd/wfc/`
Expected: PASS and clean build.

- [ ] **Step 5: Commit**

```bash
gofmt -w cmd/wfc/
go vet ./cmd/wfc/
git add cmd/wfc/main.go cmd/wfc/flags.go cmd/wfc/main_test.go
git commit -m "feat(wfc): cmd/wfc remote sysop console binary"
```

---

### Task 16: Cross-platform build verification + full test sweep

**Files:**
- None created; verification + final docs touch-up if needed.

**Interfaces:** none.

- [ ] **Step 1: Run the full test suite**

Run: `go test ./...`
Expected: PASS across all packages.

- [ ] **Step 2: Vet and format the whole tree**

Run: `gofmt -l . && go vet ./...`
Expected: `gofmt -l` prints nothing; `go vet` is clean.

- [ ] **Step 3: Cross-compile all six targets (no CGO)**

Run:

```bash
for t in windows/386 windows/amd64 linux/amd64 linux/arm64 darwin/amd64 darwin/arm64; do
  GOOS=${t%/*} GOARCH=${t#*/} CGO_ENABLED=0 go build -o /dev/null ./cmd/wfc || { echo "FAIL $t"; exit 1; }
  echo "ok $t"
done
```

Expected: `ok` for all six targets. (On Windows hosts, build to a temp file instead of `/dev/null`.)

- [ ] **Step 4: Smoke-test the CLI surface**

Run: `go run ./cmd/wfc --version` and `go run ./cmd/wfc --help`
Expected: version string prints; help lists all flags; neither panics.

- [ ] **Step 5: Commit (if any formatting/doc changes were needed)**

```bash
git add -A
git commit -m "chore(wfc): cross-platform build verification and test sweep" || echo "nothing to commit"
```

---

## Self-Review

**Spec coverage:**
- §3 layering → Tasks 1–7, 10–13 (admin contract/server/clients; wfcui).
- §4 transport & auth (SSH subsystem, pubkey, authz, audit) → Tasks 8, 9, 10, 14.
- §5 snapshot+diff engine → Tasks 2, 3, 4.
- §6 counters from real sources only → Task 2 (`callsToday` param; daemon passes `-1` for now, Task 14 note flags callhistory follow-up).
- §7 TUI (layout, keys, ascii/no-color, guards) → Tasks 11, 12, 13.
- §8 cmd/wfc CLI & flags → Task 15.
- §9 cross-platform/no-CGO → Task 16.
- §11 error handling (disconnected banner, too-small, auth/connect failures) → Tasks 11, 12, 15.
- §12 testing → tests in every task.
- §13 non-goals → respected (only `system.refresh` implemented; no chat/mutations/sockets).

**Placeholder scan:** No `TODO`/`TBD` in code steps. The `var _ = ...` guard lines and the `cfg`/`userMgr`/field-name confirmations are explicitly called out with instructions to verify against the real source and delete if unused — not silent placeholders.

**Type consistency:** `AdminClient` (Snapshot/Subscribe/Execute/Close) is identical across `InProcessClient`, `StreamClient`, `SSHChannelClient`. `RegistrySource.ListActive()` matches `*session.SessionRegistry`. Frame kinds and `Frame` fields are shared by `ServeRPC` and `StreamClient`. `wfcui.Options` fields match usages in Task 15. `NodeStatus` constants (`StatusLogin`, `StatusOnline`, `StatusInMenu`) are used consistently in Tasks 2, 11–13.

**Known cross-task compile coupling (intentional, called out in-task):** Task 11 ships minimal `handleKey`/`View` stubs so the package compiles and is testable; Tasks 12 and 13 delete those stubs and replace them. Each task's own tests pass at its boundary.

## Execution Handoff

Two execution options:

1. **Subagent-Driven (recommended)** — fresh subagent per task, two-stage review between tasks, fast iteration.
2. **Inline Execution** — execute tasks in this session via executing-plans, with checkpoints.

Which approach?
