package admin

import (
	"testing"
	"time"
)

// nodeAt is like node() but with an explicit ConnectedAt timestamp.
func nodeAt(id int, handle, menu, activity string, connectedAt time.Time) NodeState {
	ns := node(id, handle, menu, activity)
	ns.ConnectedAt = connectedAt
	return ns
}

func node(id int, handle, menu, activity string) NodeState {
	return NodeState{NodeID: id, Handle: handle, CurrentMenu: menu, Activity: activity}
}

func TestDiffSnapshots(t *testing.T) {
	now := time.Unix(1700000000, 0).UTC()
	prev := &SystemSnapshot{Nodes: []NodeState{node(1, "Ann", "MAIN", "idle")}}
	cur := &SystemSnapshot{Time: now, Nodes: []NodeState{
		node(1, "Ann", "DOORS", "LORD"), // menu + activity changed
		node(2, "Bob", "LOGIN", ""),     // connected
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

// TestDiffNodeIDTurnover is a regression test for A5: when a caller disconnects
// and a new caller connects on the same NodeID between polls, DiffSnapshots must
// emit a disconnected event for the old handle and a connected event for the new
// handle, and must NOT emit menu/activity change events misattributed to the
// prior caller.
func TestDiffNodeIDTurnover(t *testing.T) {
	t1 := time.Unix(1700000000, 0).UTC()
	t2 := t1.Add(5 * time.Minute)
	now := t2.Add(time.Second)

	prev := &SystemSnapshot{Nodes: []NodeState{nodeAt(1, "OldCaller", "MAIN", "idle", t1)}}
	cur := &SystemSnapshot{Time: now, Nodes: []NodeState{nodeAt(1, "NewCaller", "DOORS", "chatting", t2)}}

	events := DiffSnapshots(prev, cur)

	var disconnects, connects, menuChanges, activityChanges int
	var disconnectHandle, connectHandle string
	for _, e := range events {
		switch e.Type {
		case EventCallerDisconnected:
			disconnects++
			disconnectHandle = e.Handle
		case EventCallerConnected:
			connects++
			connectHandle = e.Handle
		case EventMenuChanged:
			menuChanges++
		case EventActivityChanged:
			activityChanges++
		}
	}

	if disconnects != 1 || disconnectHandle != "OldCaller" {
		t.Errorf("expected 1 disconnect for OldCaller, got %d disconnect(s) for %q", disconnects, disconnectHandle)
	}
	if connects != 1 || connectHandle != "NewCaller" {
		t.Errorf("expected 1 connect for NewCaller, got %d connect(s) for %q", connects, connectHandle)
	}
	if menuChanges != 0 || activityChanges != 0 {
		t.Errorf("expected no menu/activity change events for turnover node, got menu=%d activity=%d", menuChanges, activityChanges)
	}
}
