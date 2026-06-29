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
