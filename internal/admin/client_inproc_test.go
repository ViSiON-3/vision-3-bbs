package admin

import (
	"context"
	"testing"
	"time"
)

func TestInProcessClientImplementsAdminClient(t *testing.T) {
	srv := NewServer(ServerConfig{Reg: &fakeRegistry{}, SystemName: "T", StartedAt: time.Now(), MaxEvents: 4, CallsToday: func() int { return -1 }})
	srv.tick(time.Now())
	var c AdminClient = NewInProcessClient(srv)
	defer c.Close()

	snap, err := c.Snapshot(context.Background())
	if err != nil || snap == nil || snap.SystemName != "T" {
		t.Fatalf("snapshot: %v %+v", err, snap)
	}
	if _, err := c.Subscribe(context.Background()); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	if r, err := c.Execute(context.Background(), AdminCommand{Command: CommandRefresh}); err != nil || !r.OK {
		t.Fatalf("execute: %v %+v", err, r)
	}
}
