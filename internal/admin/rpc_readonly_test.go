package admin

import (
	"context"
	"net"
	"sync/atomic"
	"testing"
	"time"
)

// readOnlyRPC serves one console whose read-only state is ro and counts the
// node-control hooks the server reaches.
func readOnlyRPC(t *testing.T, ro *atomic.Bool) (*StreamClient, *atomic.Int32) {
	t.Helper()
	var calls atomic.Int32
	srv := NewServer(ServerConfig{
		Reg: &fakeRegistry{}, Refresh: 20 * time.Millisecond, MaxEvents: 8,
		Kick:   func(int, time.Time) error { calls.Add(1); return nil },
		TypeIn: func(string, int, time.Time, bool) error { calls.Add(1); return nil },
		Chat:   func(string, int, time.Time, bool) (bool, error) { calls.Add(1); return false, nil },
	})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go srv.Run(ctx)
	cliConn, srvConn := net.Pipe()
	go func() { _ = ServeRPC(ctx, srvConn, srv, "sysop", ro.Load, nil) }()
	c := NewStreamClient(cliConn)
	t.Cleanup(func() { _ = c.Close() })
	return c, &calls
}

// grabCommands act on a node, so a read-only console may not send them.
var grabCommands = []AdminCommand{
	{Command: CommandKick, NodeID: 1},
	{Command: CommandTypeIn, NodeID: 1, Payload: map[string]any{"on": true}},
	{Command: CommandChat, NodeID: 1, Payload: map[string]any{"start": true}},
}

// releaseCommands only give the keyboard back, so a read-only console may.
var releaseCommands = []AdminCommand{
	{Command: CommandTypeIn, NodeID: 1, Payload: map[string]any{"on": false}},
	{Command: CommandChat, NodeID: 1, Payload: map[string]any{"start": false}},
	{Command: CommandTypeIn, NodeID: 1},
	{Command: CommandChat, NodeID: 1},
}

var nodeCommands = append(append([]AdminCommand{}, grabCommands...), releaseCommands...)

func TestReadOnlyConsoleIsRefusedNodeCommands(t *testing.T) {
	var ro atomic.Bool
	ro.Store(true)
	c, calls := readOnlyRPC(t, &ro)
	ctx := context.Background()
	snap, err := c.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !snap.ReadOnly {
		t.Fatal("snapshot does not tell a read-only console")
	}
	for _, cmd := range grabCommands {
		if _, err := c.Execute(ctx, cmd); err == nil || err.Error() != ErrReadOnly.Error() {
			t.Fatalf("%s %v: err = %v, want %q", cmd.Command, cmd.Payload, err, ErrReadOnly)
		}
	}
	if n := calls.Load(); n != 0 {
		t.Fatalf("%d node-control hooks ran for a read-only console", n)
	}
	for _, cmd := range releaseCommands {
		if _, err := c.Execute(ctx, cmd); err != nil {
			t.Fatalf("%s %v: %v", cmd.Command, cmd.Payload, err)
		}
	}
	if n := calls.Load(); n != int32(len(releaseCommands)) {
		t.Fatalf("release hooks ran %d times, want %d", n, len(releaseCommands))
	}
	if res, err := c.Execute(ctx, AdminCommand{Command: CommandRefresh}); err != nil || !res.OK {
		t.Fatalf("refresh: %+v %v", res, err)
	}
}

func TestNormalConsoleReachesNodeCommands(t *testing.T) {
	var ro atomic.Bool
	c, calls := readOnlyRPC(t, &ro)
	ctx := context.Background()
	snap, err := c.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if snap.ReadOnly {
		t.Fatal("normal console told it is read-only")
	}
	for _, cmd := range nodeCommands {
		if _, err := c.Execute(ctx, cmd); err != nil {
			t.Fatalf("%s: %v", cmd.Command, err)
		}
	}
	if n := calls.Load(); n != int32(len(nodeCommands)) {
		t.Fatalf("hooks ran %d times, want %d", n, len(nodeCommands))
	}
}

// The state is read for every command and snapshot, so a change made while
// the console is connected applies without a reconnect.
func TestReadOnlyChangeAppliesMidSession(t *testing.T) {
	var ro atomic.Bool
	c, calls := readOnlyRPC(t, &ro)
	ctx := context.Background()
	if _, err := c.Execute(ctx, nodeCommands[0]); err != nil {
		t.Fatal(err)
	}
	ro.Store(true)
	if _, err := c.Execute(ctx, nodeCommands[0]); err == nil {
		t.Fatal("kick allowed after the account became read-only")
	}
	if n := calls.Load(); n != 1 {
		t.Fatalf("hooks ran %d times, want 1", n)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		snap, err := c.Snapshot(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if snap.ReadOnly {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("snapshots never reported the change")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
