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
		// Same NodeID but ConnectedAt changed: one caller left and another arrived
		// on the same node between polls. Emit disconnect (old handle) + connect
		// (new handle) and skip menu/activity diff for this node to avoid
		// misattributing changes to the prior caller.
		if !old.ConnectedAt.IsZero() && !n.ConnectedAt.IsZero() && !old.ConnectedAt.Equal(n.ConnectedAt) {
			events = append(events, Event{Time: cur.Time, Type: EventCallerDisconnected, NodeID: old.NodeID, Handle: old.Handle, Message: "disconnected"})
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
