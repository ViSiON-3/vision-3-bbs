package admin

import (
	"context"
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
func TestSubscribeCancelRace(t *testing.T) {
	reg := &fakeRegistry{
		sessions: nil,
	}
	srv := NewServer(ServerConfig{
		Reg: reg, SystemName: "race-test", StartedAt: time.Now(),
		Refresh: time.Hour, MaxEvents: 10, CallsToday: func() int { return -1 },
	})

	ctx, cancel := context.WithCancel(context.Background())
	ch := srv.Subscribe(ctx)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			srv.tick(time.Now())
		}
	}()

	cancel()
	wg.Wait()

	// Drain the channel (closed on cancel); must not panic.
	for range ch {
	}
}
