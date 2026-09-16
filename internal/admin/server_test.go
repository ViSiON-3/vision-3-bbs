package admin

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/session"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

func TestServerPollProducesSnapshotAndEvents(t *testing.T) {
	reg := &fakeRegistry{}
	srv := NewServer(ServerConfig{
		Reg: reg, SystemName: "T", StartedAt: time.Now(),
		Refresh: time.Hour, MaxEvents: 10, CallsToday: func() int { return -1 },
	})

	// Manual tick API for deterministic tests.
	reg.sessions = []*session.BbsSession{{NodeID: 1, User: &user.User{Handle: "A"}, CurrentMenu: "MAIN"}}
	srv.tick(time.Now()) // first tick: seed, no events
	reg.sessions[0].CurrentMenu = "DOORS"
	srv.tick(time.Now()) // menu change → one event

	snap := srv.Snapshot()
	if snap == nil || len(snap.Nodes) != 1 || snap.Nodes[0].CurrentMenu != "DOORS" {
		t.Fatalf("snapshot wrong: %+v", snap)
	}

	ch := srv.Subscribe(context.Background())
	select {
	case e := <-ch:
		if e.Type != EventMenuChanged {
			t.Fatalf("expected replayed menu.changed, got %+v", e)
		}
	case <-time.After(time.Second):
		t.Fatal("expected a replayed event from ring buffer")
	}
}

func TestServerExecuteRefreshOnly(t *testing.T) {
	srv := NewServer(ServerConfig{Reg: &fakeRegistry{}, MaxEvents: 4, CallsToday: func() int { return -1 }})
	if r, err := srv.Execute(AdminCommand{Command: CommandRefresh}); err != nil || !r.OK {
		t.Fatalf("refresh should succeed: %v %+v", err, r)
	}
	if _, err := srv.Execute(AdminCommand{Command: "node.disconnect"}); err == nil {
		t.Fatal("non-refresh command must be rejected in v1")
	}
}

// TestSubscribeCancelRace is a regression test for the send-on-closed-channel
// panic that occurred when tick() fanned out to a subscriber channel that was
// concurrently closed by Subscribe's cancel goroutine. Without the fix this
// panics under -race; with the fix it passes cleanly.
//
// The test seeds fakeRegistry with one active session so that DiffSnapshots
// produces a menu.changed event on every tick after the first. This means
// tick() actually reaches the fan-out send path (select { case c <- e: default: })
// while the cancel goroutine may concurrently close the channel — the exact
// interleaving the fix guards against.
func TestSubscribeCancelRace(t *testing.T) {
	sess := &session.BbsSession{
		NodeID:      1,
		User:        &user.User{Handle: "racer"},
		CurrentMenu: "MAIN",
		Activity:    "idle",
	}
	reg := &fakeRegistry{sessions: []*session.BbsSession{sess}}
	srv := NewServer(ServerConfig{
		Reg: reg, SystemName: "race-test", StartedAt: time.Now(),
		Refresh: time.Hour, MaxEvents: 10, CallsToday: func() int { return -1 },
	})

	// Seed prev so the first tick in the goroutine sees a change.
	srv.tick(time.Now())

	ctx, cancel := context.WithCancel(context.Background())
	ch := srv.Subscribe(ctx)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		menus := [2]string{"DOORS", "MAIN"}
		for i := 0; i < 200; i++ {
			// Mutate under the session's write lock so BuildSnapshot's RLock
			// sees a consistent value and the race detector stays clean.
			sess.Mutex.Lock()
			sess.CurrentMenu = menus[i%2]
			sess.Mutex.Unlock()

			srv.tick(time.Now())
		}
	}()

	// Cancel partway through to race the channel close against the fan-out.
	cancel()
	wg.Wait()

	// Drain the channel (closed on cancel); must not panic.
	for range ch {
	}
}

func TestServerExecuteKick(t *testing.T) {
	reg := &fakeRegistry{sessions: []*session.BbsSession{{NodeID: 3, User: &user.User{Handle: "Scanner"}}}}
	var kicked []int
	srv := NewServer(ServerConfig{Reg: reg, MaxEvents: 4, Kick: func(nodeID int) error {
		if nodeID == 9 {
			return errNoSuchNode
		}
		kicked = append(kicked, nodeID)
		return nil
	}})
	srv.tick(time.Now())
	ch := srv.Subscribe(context.Background())

	res, err := srv.Execute(AdminCommand{Command: CommandKick, NodeID: 3})
	if err != nil || !res.OK || len(kicked) != 1 || kicked[0] != 3 {
		t.Fatalf("kick: res=%+v err=%v kicked=%v", res, err, kicked)
	}
	select {
	case e := <-ch:
		if e.Type != EventNodeKicked || e.NodeID != 3 || e.Handle != "Scanner" {
			t.Fatalf("kick event = %+v", e)
		}
	case <-time.After(time.Second):
		t.Fatal("expected a node.kicked event")
	}
	if _, err := srv.Execute(AdminCommand{Command: CommandKick}); err == nil {
		t.Fatal("kick without a node id must fail")
	}
	if _, err := srv.Execute(AdminCommand{Command: CommandKick, NodeID: 9}); err == nil || !errors.Is(err, errNoSuchNode) {
		t.Fatalf("kick hook error must be wrapped, got %v", err)
	}

	noHook := NewServer(ServerConfig{Reg: reg, MaxEvents: 4})
	if _, err := noHook.Execute(AdminCommand{Command: CommandKick, NodeID: 3}); !errors.Is(err, ErrKickUnsupported) {
		t.Fatalf("kick without hook: %v", err)
	}
}

var errNoSuchNode = errors.New("no such node")

func TestSnapshotCarriesCountersMaxNodesAndEvents(t *testing.T) {
	srv := NewServer(ServerConfig{
		Reg:         &fakeRegistry{},
		TotalUsers:  func() int { return 250 },
		CallsToday:  func() int { return 12 },
		NewUsers:    func() int { return 3 },
		MailWaiting: func() int { return 7 },
		MaxNodes:    func() int { return 10 },
		ScheduledEvents: func() []ScheduledEvent {
			return []ScheduledEvent{{ID: "nightly", Name: "Nightly", Schedule: "0 3 * * *", Enabled: true}}
		},
	})
	srv.tick(time.Now())
	snap := srv.Snapshot()
	c := snap.Counters
	if c.TotalUsers != 250 || c.CallsToday != 12 || c.NewUsers != 3 || c.MailWaiting != 7 || snap.MaxNodes != 10 {
		t.Fatalf("snapshot header fields: %+v max=%d", c, snap.MaxNodes)
	}
	if len(snap.ScheduledEvents) != 1 || snap.ScheduledEvents[0].ID != "nightly" {
		t.Fatalf("scheduled events: %+v", snap.ScheduledEvents)
	}
	// Getters left nil read as unavailable, not zero.
	bare := NewServer(ServerConfig{Reg: &fakeRegistry{}})
	bare.tick(time.Now())
	if c := bare.Snapshot().Counters; c.TotalUsers != -1 || c.CallsToday != -1 || c.NewUsers != -1 || c.MailWaiting != -1 {
		t.Fatalf("nil getters must read -1: %+v", c)
	}
}
