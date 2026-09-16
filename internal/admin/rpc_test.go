package admin

import (
	"context"
	"errors"
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

// TestStreamClientReportsDeadLink: once the peer goes away, Done fires,
// Snapshot stops handing out the stale cached snapshot, and Execute fails
// fast instead of blocking.
func TestStreamClientReportsDeadLink(t *testing.T) {
	srv := NewServer(ServerConfig{Reg: &fakeRegistry{}, SystemName: "T", Refresh: time.Hour, MaxEvents: 8})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cliConn, srvConn := net.Pipe()
	go func() { _ = ServeRPC(ctx, srvConn, srv, nil) }()

	c := NewStreamClient(cliConn)
	defer c.Close()
	if _, err := c.Snapshot(ctx); err != nil {
		t.Fatalf("initial snapshot: %v", err)
	}
	select {
	case <-c.Done():
		t.Fatal("Done fired while the link was healthy")
	default:
	}

	_ = srvConn.Close() // the daemon side vanishes

	select {
	case <-c.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("Done did not fire after the peer closed")
	}
	if c.Err() == nil {
		t.Fatal("Err must explain the loss")
	}
	if _, err := c.Snapshot(ctx); err == nil {
		t.Fatal("Snapshot must fail on a dead link rather than return the cached copy")
	}
	if _, err := c.Execute(ctx, AdminCommand{Command: CommandRefresh}); err == nil {
		t.Fatal("Execute must fail on a dead link")
	}
}

// TestExecuteDiscardsLateReplyOfTimedOutCommand: a command whose caller gave
// up still completes on the server; its reply must not be handed to the next
// command as if it were that command's result.
func TestExecuteDiscardsLateReplyOfTimedOutCommand(t *testing.T) {
	release := make(chan struct{})
	srv := NewServer(ServerConfig{
		Reg: &fakeRegistry{}, Refresh: time.Hour, MaxEvents: 8,
		Kick: func(int, time.Time) error { <-release; return nil },
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cliConn, srvConn := net.Pipe()
	go func() { _ = ServeRPC(ctx, srvConn, srv, nil) }()
	c := NewStreamClient(cliConn)
	defer c.Close()
	if _, err := c.Snapshot(ctx); err != nil {
		t.Fatalf("snapshot: %v", err)
	}

	short, cancelShort := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancelShort()
	if _, err := c.Execute(short, AdminCommand{Command: CommandKick, NodeID: 1}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("blocked kick should time out, got %v", err)
	}
	close(release) // the kick now completes and its reply arrives late

	res, err := c.Execute(ctx, AdminCommand{Command: CommandRefresh})
	if err != nil || res == nil || !res.OK || res.Message != "" {
		t.Fatalf("refresh got the kick's late reply or failed: res=%+v err=%v", res, err)
	}
}
