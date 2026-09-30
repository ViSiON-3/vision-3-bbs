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

	snap := BuildSnapshot(reg, "Test BBS", start, now, Counters{CallsToday: 14, NewUsers: -1, MailWaiting: -1}, nil)

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

// With a TimeLimit hook the snapshot shows the limit the BBS enforces: a
// sysop's stored limit is ignored, and a caller with none is unlimited rather
// than unknown.
func TestBuildSnapshotEffectiveTimeLimit(t *testing.T) {
	start := time.Unix(1700000000, 0).UTC()
	now := start.Add(10 * time.Minute)
	reg := &fakeRegistry{sessions: []*session.BbsSession{
		{NodeID: 1, User: &user.User{Handle: "Sysop", AccessLevel: 255, TimeLimit: 60}, StartTime: start},
		{NodeID: 2, User: &user.User{Handle: "Caller", AccessLevel: 10, TimeLimit: 60}, StartTime: start},
		{NodeID: 3, User: &user.User{Handle: "Free", AccessLevel: 10}, StartTime: start},
	}}
	exemptSysops := func(u *user.User) int {
		if u.AccessLevel >= 250 {
			return 0
		}
		return u.TimeLimit
	}

	snap := BuildSnapshot(reg, "Test BBS", start, now, Counters{}, exemptSysops)
	for i, want := range []struct {
		left      int
		unlimited bool
	}{{-1, true}, {50, false}, {-1, true}} {
		n := snap.Nodes[i]
		if n.TimeLeftMins != want.left || n.TimeUnlimited != want.unlimited {
			t.Errorf("%s: TimeLeftMins=%d TimeUnlimited=%v, want %d/%v",
				n.Handle, n.TimeLeftMins, n.TimeUnlimited, want.left, want.unlimited)
		}
	}
}
