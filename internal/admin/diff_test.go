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
