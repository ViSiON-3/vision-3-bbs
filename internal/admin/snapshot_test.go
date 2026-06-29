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
