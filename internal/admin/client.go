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
