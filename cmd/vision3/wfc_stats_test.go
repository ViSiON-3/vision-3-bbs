package main

import (
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	gossh "golang.org/x/crypto/ssh"

	"github.com/ViSiON-3/vision-3-bbs/internal/session"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

func TestCountCallsToday(t *testing.T) {
	now := time.Date(2026, 9, 16, 14, 0, 0, 0, time.Local)
	yesterday := now.Add(-20 * time.Hour) // 18:00 the day before
	records := []user.CallRecord{
		{ConnectTime: now.Add(-time.Hour)},
		{ConnectTime: now.Add(-13 * time.Hour)}, // 01:00 today
		{ConnectTime: yesterday},
	}
	active := []*session.BbsSession{
		{NodeID: 1, User: &user.User{Handle: "A"}, StartTime: now.Add(-5 * time.Minute)},
		{NodeID: 2, StartTime: now.Add(-time.Minute)},                                   // bot at the login prompt
		{NodeID: 3, User: &user.User{Handle: "B"}, StartTime: now.Add(-15 * time.Hour)}, // connected yesterday
	}
	if got := countCallsToday(records, active, now); got != 3 {
		t.Fatalf("countCallsToday = %d, want 3", got)
	}
	if got := countCallsToday(nil, nil, now); got != 0 {
		t.Fatalf("empty = %d", got)
	}
}

func TestCountNewUsers(t *testing.T) {
	users := []*user.User{
		{Handle: "pending", Validated: false, AccessLevel: 10},
		{Handle: "pending2", Validated: false, AccessLevel: 10},
		{Handle: "validated", Validated: true, AccessLevel: 10},
		{Handle: "banned", Validated: false, AccessLevel: 0},
		{Handle: "deleted", Validated: false, AccessLevel: 10, DeletedUser: true},
		nil,
	}
	if got := countNewUsers(users); got != 2 {
		t.Fatalf("countNewUsers = %d, want 2", got)
	}
	if got := countTotalUsers(users); got != 4 {
		t.Fatalf("countTotalUsers = %d, want 4 (deleted and nil excluded)", got)
	}
}

// fakeChannel records what kickNode does to a caller's SSH channel.
type fakeChannel struct {
	gossh.Channel
	wrote  strings.Builder
	closed bool
}

func (f *fakeChannel) Write(p []byte) (int, error) { return f.wrote.Write(p) }
func (f *fakeChannel) Close() error                { f.closed = true; return nil }

func TestKickNode(t *testing.T) {
	reg := session.NewSessionRegistry()
	ch := &fakeChannel{}
	started := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)
	reg.Register(&session.BbsSession{NodeID: 2, Channel: ch, StartTime: started})
	reg.Register(&session.BbsSession{NodeID: 3, StartTime: started}) // no channel wired

	// A stale connect time means the slot has been reused: refuse.
	if err := kickNode(reg, 2, started.Add(-time.Hour)); err == nil || ch.closed {
		t.Fatalf("kick with a different connect time must be refused: err=%v closed=%v", err, ch.closed)
	}
	if err := kickNode(reg, 2, started); err != nil {
		t.Fatalf("kick node 2: %v", err)
	}
	if !ch.closed || !strings.Contains(ch.wrote.String(), "disconnected by the SysOp") {
		t.Fatalf("channel closed=%v wrote=%q", ch.closed, ch.wrote.String())
	}
	if err := kickNode(reg, 3, time.Time{}); err == nil {
		t.Fatal("node without a channel must report an error")
	}
	if err := kickNode(reg, 9, time.Time{}); err == nil {
		t.Fatal("unknown node must report an error")
	}
	if err := kickNode(nil, 2, time.Time{}); err == nil {
		t.Fatal("nil registry must report an error")
	}
}

// stalledChannel never completes a write, like a caller whose flow-control
// window is exhausted.
type stalledChannel struct {
	gossh.Channel
	closed  chan struct{}
	closeMu sync.Once
}

func (c *stalledChannel) Write(p []byte) (int, error) {
	<-c.closed // released by Close, as x/crypto/ssh does for a closed channel
	return 0, io.EOF
}
func (c *stalledChannel) Close() error { c.closeMu.Do(func() { close(c.closed) }); return nil }

func TestKickNodeDoesNotHangOnStalledNotice(t *testing.T) {
	reg := session.NewSessionRegistry()
	ch := &stalledChannel{closed: make(chan struct{})}
	reg.Register(&session.BbsSession{NodeID: 1, Channel: ch})
	done := make(chan error, 1)
	go func() { done <- kickNode(reg, 1, time.Time{}) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("kick: %v", err)
		}
	case <-time.After(kickNoticeTimeout + 3*time.Second):
		t.Fatal("kick blocked behind a stalled notice write")
	}
	select {
	case <-ch.closed:
	default:
		t.Fatal("channel must be closed even though the notice never drained")
	}
}

func TestSchedulerEventsNilScheduler(t *testing.T) {
	if got := schedulerEvents(nil, time.Now()); got != nil {
		t.Fatalf("nil scheduler must yield nil, got %v", got)
	}
}
