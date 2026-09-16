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
	NodeStatusIdle NodeStatus = "idle"
	StatusLogin    NodeStatus = "login"
	StatusOnline   NodeStatus = "online"
	StatusInMenu   NodeStatus = "menu"
	StatusInChat   NodeStatus = "chat"
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
// Every counter other than ActiveNodes uses -1 to mean "unavailable".
type Counters struct {
	ActiveNodes int `json:"activeNodes"`
	TotalUsers  int `json:"totalUsers"`  // registered (not deleted) accounts; -1 if unavailable
	CallsToday  int `json:"callsToday"`  // logins since local midnight; -1 if unavailable
	NewUsers    int `json:"newUsers"`    // accounts awaiting validation; -1 if unavailable
	MailWaiting int `json:"mailWaiting"` // unread private mail for the SysOp; -1 if unavailable
}

// SnapshotSchema is the schema version a current daemon stamps on every
// snapshot. A console compares it to decide which fields the daemon actually
// reports: an older daemon leaves Schema at zero, and the console then shows
// "-" for the newer counters instead of a misleading zero.
const SnapshotSchema = 2

// SystemSnapshot is a point-in-time view of the whole system.
type SystemSnapshot struct {
	Schema     int       `json:"schema,omitempty"`
	Time       time.Time `json:"time"`
	SystemName string    `json:"systemName"`
	UptimeSecs int64     `json:"uptimeSecs"`
	// MaxNodes is the configured node limit, so the console can draw a slot
	// for every node the board answers. Zero when the daemon does not report it.
	MaxNodes int         `json:"maxNodes,omitempty"`
	Nodes    []NodeState `json:"nodes"`
	Counters Counters    `json:"counters"`
	// PendingReloads names structural config files whose reload is queued
	// for the next idle window (empty when nothing is pending).
	PendingReloads []string `json:"pendingReloads,omitempty"`
	// ScheduledEvents is the event scheduler's configured events with their
	// run history, for the console's Events tab. Nil when the daemon has no
	// scheduler.
	ScheduledEvents []ScheduledEvent `json:"scheduledEvents,omitempty"`
}

// ScheduledEvent is one event-scheduler entry as shown on the console.
type ScheduledEvent struct {
	ID             string    `json:"id"`
	Name           string    `json:"name"`
	Schedule       string    `json:"schedule,omitempty"` // cron spec; empty for startup-only
	Enabled        bool      `json:"enabled"`
	RunAtStartup   bool      `json:"runAtStartup,omitempty"`
	Running        bool      `json:"running"`
	NextRun        time.Time `json:"nextRun,omitzero"`
	LastRun        time.Time `json:"lastRun,omitzero"`
	LastStatus     string    `json:"lastStatus,omitempty"` // success | failure | timeout
	LastDurationMs int64     `json:"lastDurationMs,omitempty"`
	RunCount       int       `json:"runCount"`
	FailureCount   int       `json:"failureCount"`
}

// EventType enumerates diff-synthesized event kinds.
type EventType string

const (
	EventCallerConnected    EventType = "caller.connected"
	EventCallerDisconnected EventType = "caller.disconnected"
	// EventCallerLoggedIn fires when a connection acquires a user: the point
	// at which an anonymous connection becomes a caller.
	EventCallerLoggedIn  EventType = "caller.login"
	EventMenuChanged     EventType = "menu.changed"
	EventActivityChanged EventType = "activity.changed"
	// EventNodeKicked is emitted by the server when an admin command
	// disconnects a caller, so every console sees who was dropped and why.
	EventNodeKicked EventType = "node.kicked"
)

// Event is a single entry in the live event feed.
type Event struct {
	Time    time.Time `json:"time"`
	Type    EventType `json:"type"`
	NodeID  int       `json:"nodeId"`
	Handle  string    `json:"handle"`
	Addr    string    `json:"addr,omitempty"` // remote address; names an anonymous connection
	Message string    `json:"message"`
}

// CommandType enumerates admin commands.
type CommandType string

const (
	// CommandRefresh asks the server to rebuild its snapshot now.
	CommandRefresh CommandType = "system.refresh"
	// CommandKick disconnects the caller on AdminCommand.NodeID.
	CommandKick CommandType = "node.kick"
)

// AdminCommand is a request to the server to perform an action.
type AdminCommand struct {
	Command CommandType `json:"command"`
	NodeID  int         `json:"nodeId,omitempty"`
	// ConnectedAt identifies the session a node-scoped command targets. Node
	// numbers are reused, so a kick names the caller it was issued against
	// by their connect time; if a different caller now holds the slot the
	// command is refused rather than dropping the wrong person.
	ConnectedAt time.Time      `json:"connectedAt,omitzero"`
	Payload     map[string]any `json:"payload,omitempty"`
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

// Liveness is optionally implemented by clients whose transport can die
// underneath them. Done is closed once the connection is unusable; Err then
// reports why. The TUI uses it to notice a dropped link immediately instead
// of waiting for the next request to fail.
type Liveness interface {
	Done() <-chan struct{}
	Err() error
}
