package main

import (
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/session"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

type fakeSessions []*session.BbsSession

func (f fakeSessions) ListActive() []*session.BbsSession { return f }

type fakeUsers struct {
	byID  map[int]*user.User
	calls []user.CallRecord
}

func (f *fakeUsers) GetUserByID(id int) (*user.User, bool) {
	u, ok := f.byID[id]
	if !ok {
		return nil, false
	}
	c := *u
	return &c, true
}
func (f *fakeUsers) GetLastCallers() []user.CallRecord { return f.calls }

// Only accounts that opted in are published, as they stand now, and never
// a session or call made invisible, a deleted account, or a call from
// before the caller agreed.
func TestCentralActivitySourceAppliesConsent(t *testing.T) {
	now := time.Now()
	agreed := now.Add(-3 * time.Hour)
	users := &fakeUsers{byID: map[int]*user.User{
		1: {ID: 1, Handle: "|12Alice", CentralPresence: true, CentralAvailability: protocol.CentralBusy, CentralLastCallsSince: &agreed},
		2: {ID: 2, Handle: "Bob"}, // declined both
		3: {ID: 3, Handle: "Carol", CentralPresence: true, CentralAvailability: "napping"},
		4: {ID: 4, Handle: "Dave", CentralPresence: true, CentralLastCallsSince: &agreed, DeletedUser: true},
	}}
	sess := func(node int, id int, invisible bool) *session.BbsSession {
		return &session.BbsSession{NodeID: node, StartTime: now.Add(-time.Duration(node) * time.Minute), User: &user.User{ID: id}, Invisible: invisible}
	}
	sessions := fakeSessions{
		sess(1, 1, false),
		sess(2, 1, false), // Alice on a second node
		sess(3, 2, false),
		sess(4, 3, false),
		sess(5, 3, true), // Carol hidden on this one
		sess(6, 4, false),
		{NodeID: 7, StartTime: now}, // still logging in
	}
	src := newCentralActivitySource(sessions, users)

	got := src.CentralPresence()
	if len(got) != 3 {
		t.Fatalf("presence %+v, want Alice twice and Carol once", got)
	}
	if got[0].Handle != "Alice" || got[0].Availability != protocol.CentralBusy || got[2].Handle != "Carol" || got[2].Availability != "" {
		t.Errorf("entries %+v", got)
	}
	if got[0].Session == got[1].Session {
		t.Error("two sessions share a session ID")
	}
	if src.CentralPresence()[0].Session != got[0].Session {
		t.Error("a session's ID changed between snapshots")
	}
	if other := newCentralActivitySource(sessions, users).CentralPresence(); other[0].Session == got[0].Session {
		t.Error("session IDs are not keyed per process")
	}

	// The account changes its mind: gone from the next snapshot.
	users.byID[1].CentralPresence = false
	if got := src.CentralPresence(); len(got) != 1 || got[0].Handle != "Carol" {
		t.Errorf("after opting out: %+v", got)
	}

	users.calls = []user.CallRecord{
		{UserID: 1, ConnectTime: now.Add(-4 * time.Hour), DisconnectTime: now.Add(-230 * time.Minute)}, // before she agreed
		{UserID: 1, ConnectTime: now.Add(-2 * time.Hour), DisconnectTime: now.Add(-110 * time.Minute)},
		{UserID: 1, ConnectTime: now.Add(-time.Hour), Invisible: true},
		{UserID: 2, ConnectTime: now.Add(-time.Hour)},
		{UserID: 4, ConnectTime: now.Add(-time.Hour)},
		{UserID: 9, ConnectTime: now.Add(-time.Hour)}, // no such account
	}
	calls := src.CentralLastCalls(now.Add(-24 * time.Hour))
	if len(calls) != 1 || calls[0].Handle != "Alice" || calls[0].Until == "" {
		t.Fatalf("calls %+v, want only Alice's visible call since she agreed", calls)
	}
	if calls := src.CentralLastCalls(now.Add(-90 * time.Minute)); len(calls) != 0 {
		t.Errorf("since not applied: %+v", calls)
	}
	// Withdrawing takes every call of hers out at once.
	users.byID[1].CentralLastCallsSince = nil
	if calls := src.CentralLastCalls(now.Add(-24 * time.Hour)); len(calls) != 0 {
		t.Errorf("after withdrawing: %+v", calls)
	}
}
