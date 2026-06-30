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
