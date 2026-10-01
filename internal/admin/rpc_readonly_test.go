package admin

import (
	"context"
	"net"
	"sync"
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

// ServeRPC returns only after its snapshot writer has finished asking
// readOnly, so the caller may tear down what readOnly reads.
func TestServeRPCWaitsForReadOnlyCallsBeforeReturning(t *testing.T) {
	srv := NewServer(ServerConfig{Reg: &fakeRegistry{}, Refresh: 5 * time.Millisecond, MaxEvents: 8})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go srv.Run(ctx)

	var (
		calls   atomic.Int32
		inside  atomic.Int32
		once    sync.Once
		entered = make(chan struct{})
		release = make(chan struct{})
	)
	readOnly := func() bool {
		if calls.Add(1) == 1 {
			return false // initial snapshot
		}
		inside.Add(1)
		defer inside.Add(-1)
		once.Do(func() { close(entered) })
		<-release
		return false
	}
	cliConn, srvConn := net.Pipe()
	returned := make(chan struct{})
	go func() {
		_ = ServeRPC(ctx, srvConn, srv, "sysop", readOnly, nil)
		close(returned)
	}()
	c := NewStreamClient(cliConn)
	if _, err := c.Snapshot(ctx); err != nil {
		t.Fatal(err)
	}
	<-entered // the snapshot writer is now blocked inside readOnly
	_ = c.Close()

	// Give a ServeRPC that does not wait time to return early.
	var early int32
	select {
	case <-returned:
		early = inside.Load()
	case <-time.After(200 * time.Millisecond):
	}
	close(release)
	<-returned
	if early != 0 {
		t.Fatalf("ServeRPC returned with %d readOnly call(s) still running", early)
	}
}
