package admin

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/session"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

func TestRPCStreamClientServer(t *testing.T) {
	reg := &fakeRegistry{sessions: []*session.BbsSession{
		{NodeID: 1, User: &user.User{Handle: "A"}, CurrentMenu: "MAIN"},
	}}
	srv := NewServer(ServerConfig{Reg: reg, SystemName: "T", StartedAt: time.Now(), Refresh: 20 * time.Millisecond, MaxEvents: 8, CallsToday: func() int { return -1 }})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go srv.Run(ctx)

	cliConn, srvConn := net.Pipe()
	go ServeRPC(ctx, srvConn, srv, func(string) {})

	var c AdminClient = NewStreamClient(cliConn)
	defer c.Close()

	snap, err := c.Snapshot(ctx)
	if err != nil || snap == nil || snap.SystemName != "T" {
		t.Fatalf("snapshot over RPC: %v %+v", err, snap)
	}

	events, err := c.Subscribe(ctx)
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	// Trigger a change and expect an event to arrive.
	// Use the session mutex to avoid a race with BuildSnapshot's RLock.
	reg.sessions[0].Mutex.Lock()
	reg.sessions[0].CurrentMenu = "DOORS"
	reg.sessions[0].Mutex.Unlock()
	select {
	case e := <-events:
		_ = e // any event proves the stream works
	case <-time.After(2 * time.Second):
		t.Fatal("expected an event over the RPC stream")
	}
}
