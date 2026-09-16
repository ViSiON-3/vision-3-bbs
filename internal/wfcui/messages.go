package wfcui

import (
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/admin"
)

// Every message produced on behalf of a connection carries the connID that
// was current when its command was issued. Update drops messages whose connID
// is stale, so a straggling result from a link that has since died can never
// corrupt the state of its replacement.

// tickMsg is the console heartbeat: it repaints the clock-driven fields,
// polls the snapshot while connected, and drives reconnect timing while not.
type tickMsg struct{ at time.Time }

// snapshotMsg carries the result of one Snapshot call.
type snapshotMsg struct {
	connID int
	snap   *admin.SystemSnapshot
	err    error
}

// subscribedMsg carries the result of one Subscribe call.
type subscribedMsg struct {
	connID int
	ch     <-chan admin.Event
	err    error
}

// eventMsg carries one event read from ch, or ok=false when ch was closed
// (the transport is gone).
type eventMsg struct {
	connID int
	ch     <-chan admin.Event
	ev     admin.Event
	ok     bool
}

// connLostMsg fires when the client's Liveness.Done channel closes.
type connLostMsg struct {
	connID int
	err    error
}

// dialResultMsg carries the outcome of a reconnect attempt.
type dialResultMsg struct {
	connID int
	client admin.AdminClient
	err    error
}

// kickResultMsg carries the outcome of a node.kick command.
type kickResultMsg struct {
	nodeID int
	handle string
	addr   string // names a bot, which has no handle
	res    *admin.Result
	err    error
}
